package backtest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type positionStudyEngine struct {
	requests []Request
	coverage bool
	failAt   int
}

func (engine *positionStudyEngine) Run(_ context.Context, request Request) (Result, error) {
	engine.requests = append(engine.requests, request)
	if engine.failAt > 0 && len(engine.requests) == engine.failAt {
		return Result{}, errors.New("missing diagnostic data")
	}
	value := request.Technical.MaxPosition * 100
	if request.CommissionRate > .0003 {
		value = -value
	}
	coverage := map[string]DataCoverage{}
	if engine.coverage {
		for _, symbol := range request.Tickers {
			coverage[symbol] = DataCoverage{Bars: 200, CoverageRatio: 1}
		}
	}
	return Result{Request: request, Metrics: Metrics{TotalReturn: value, MaxDrawdown: -request.Technical.MaxPosition * 100, Trades: 20, BenchmarkAvailable: true}, DataCoverage: coverage}, nil
}

func positionStudySource() ContinuousOptimizationResult {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := Request{Strategy: "technical-breakout", StrategyVersion: "v1", Tickers: []string{"sh600000"}, InitialCash: 1000000, CommissionRate: .0003, MinimumCommission: 5, StampDutyRate: .0005, TransferFeeRate: .00001, SlippageBPS: 5, Adjustment: AdjustmentNone, NoFutureData: true, LiquidateAtEnd: true, Technical: DefaultTechnicalParameters()}
	parameters := base.Technical
	parameters.EntryMode = EntryModeReclaim
	folds := []WalkForwardFold{{ID: "F01", Validate: Period{Start: at, End: at.AddDate(0, 1, 0)}}, {ID: "F02", Validate: Period{Start: at.AddDate(0, 1, 1), End: at.AddDate(0, 2, 0)}}}
	source := ContinuousOptimizationResult{ID: "source", Request: ContinuousOptimizationRequest{BaseRequest: base, Folds: folds, Holdout: Period{Start: at.AddDate(0, 2, 1), End: at.AddDate(0, 3, 0)}, MinimumValidationTrades: 20, MinimumPositiveFoldRatio: .67, MaximumValidationDrawdown: 15}}
	candidate := ContinuousCandidateResult{Proposal: StrategyProposal{ID: "P008", Parameters: parameters}}
	for _, fold := range folds {
		candidate.Folds = append(candidate.Folds, WalkForwardFoldResult{Fold: fold, Validate: Metrics{TotalReturn: 20, MaxDrawdown: -20, Trades: 20}})
	}
	source.Candidates = []ContinuousCandidateResult{candidate}
	return source
}

func TestPositionStudyOnlyChangesCapAndStressCosts(t *testing.T) {
	source := positionStudySource()
	engine := &positionStudyEngine{coverage: true}
	result, err := RunPositionStudy(context.Background(), engine, source, "P008", []float64{.2, .15, .1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed || !result.SourceBaselineReproduced || result.PromotionAllowed || len(engine.requests) != 18 {
		t.Fatalf("bad study: %+v", result)
	}
	for i := 0; i < len(engine.requests); i += 2 {
		normal, stress := engine.requests[i], engine.requests[i+1]
		if normal.Technical.EntryMode != EntryModeReclaim || !reflect.DeepEqual(normal.Tickers, source.Request.BaseRequest.Tickers) || normal.Technical.StopLoss != .08 {
			t.Fatal("non-cap parameter changed")
		}
		stress.CommissionRate /= 2
		stress.MinimumCommission /= 2
		stress.StampDutyRate /= 2
		stress.TransferFeeRate /= 2
		stress.SlippageBPS /= 2
		if !reflect.DeepEqual(normal, stress) {
			t.Fatal("stress changed strategy or dates")
		}
	}
	if result.Variants[0].Normal.Passed || !result.Variants[1].Normal.Passed || !result.Variants[2].Normal.Passed {
		t.Fatal("original drawdown gate not applied")
	}
	if result.Variants[1].DoubleCost.Passed {
		t.Fatal("losing stress windows passed")
	}
	if source.Request.BaseRequest.Technical.MaxPosition != .2 {
		t.Fatal("source mutated")
	}
}

func TestPositionStudyFlagsMissingDataAndBaselineDrift(t *testing.T) {
	source := positionStudySource()
	source.Candidates[0].Folds[0].Validate.Trades++
	result, err := RunPositionStudy(context.Background(), &positionStudyEngine{}, source, "P008", []float64{.2, .1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceBaselineReproduced || len(result.BaselineDifferences) == 0 || result.Variants[1].Normal.Passed {
		t.Fatal("missing data or drift hidden")
	}
	if _, err := RunPositionStudy(context.Background(), &positionStudyEngine{}, source, "P008", []float64{.15, .1}, nil); err == nil {
		t.Fatal("missing baseline accepted")
	}
}

func TestPositionStudyKeepsValidationWhenDiagnosticFails(t *testing.T) {
	result, err := RunPositionStudy(context.Background(), &positionStudyEngine{coverage: true, failAt: 9}, positionStudySource(), "P008", []float64{.2, .1}, nil)
	if err == nil || result.Completed || result.PromotionAllowed || !result.SourceBaselineReproduced || len(result.Variants) != 2 {
		t.Fatalf("partial result lost or reported complete: %+v %v", result, err)
	}
}
