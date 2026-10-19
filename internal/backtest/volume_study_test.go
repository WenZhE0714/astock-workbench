package backtest

import (
	"context"
	"math"
	"reflect"
	"testing"
)

func TestVolumeStudyChangesOnlyActualFloorAndReproducesBaseline(t *testing.T) {
	source := positionStudySource()
	engine := &positionStudyEngine{coverage: true}
	prior, err := RunPositionStudy(context.Background(), engine, source, "P008", []float64{.2, .1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine.requests = nil
	result, err := RunVolumeStudy(context.Background(), engine, source, "P008", prior.Variants[1], nil)
	if err != nil || !result.Completed || !result.BaselineReproduced || result.PromotionAllowed || len(engine.requests) != 12 {
		t.Fatalf("invalid paired volume study: %v", err)
	}
	for i := 0; i < 6; i++ {
		a, b := engine.requests[i], engine.requests[i+6]
		if a.EntryVolumeFloor != 0 || b.EntryVolumeFloor != 1.5 || b.Technical.VolumeRatioMin != 1.2 || b.Technical.MaxPosition != .1 {
			t.Fatal("wrong study protocol")
		}
		b.EntryVolumeFloor = 0
		if !reflect.DeepEqual(a, b) {
			t.Fatal("non-volume parameter changed")
		}
	}
	for i := 0; i < len(engine.requests); i += 2 {
		a, b := engine.requests[i], engine.requests[i+1]
		b.CommissionRate /= 2
		b.MinimumCommission /= 2
		b.StampDutyRate /= 2
		b.TransferFeeRate /= 2
		b.SlippageBPS /= 2
		if !reflect.DeepEqual(a, b) {
			t.Fatal("cost stress changed strategy")
		}
	}
	if result.Variants[1].DoubleCost.Passed {
		t.Fatal("original validation gates bypassed")
	}
	prior.Variants[1].Windows[0].Normal.Equity = []EquityPoint{{Equity: 42}}
	engine.requests = nil
	result, err = RunVolumeStudy(context.Background(), engine, source, "P008", prior.Variants[1], nil)
	if err == nil || result.Completed || result.BaselineReproduced || len(engine.requests) != 6 {
		t.Fatal("baseline drift failed to stop study")
	}
}

func TestVolumeStudyAttributesRejectedAndChangedPortfolioPaths(t *testing.T) {
	trade := func(symbol string, profit, ret float64) Trade {
		return Trade{Symbol: symbol, EntrySignal: SignalSnapshot{Date: "2026-01-01"}, NetProfit: profit, ReturnPercent: ret}
	}
	baseline := Result{Trades: []Trade{trade("loss", -20, -2), trade("missed-win", 10, 1), trade("big-win", 100, 25), trade("path-change", -5, -1)}, Metrics: Metrics{FinalEquity: 1085}}
	filtered := Result{Trades: []Trade{trade("big-win", 60, 15), trade("new", 15, 2)}, OpenPositions: []OpenPosition{{Symbol: "open", EntrySignal: SignalSnapshot{Date: "2026-01-01"}, UnrealizedProfit: -7}}, Metrics: Metrics{FinalEquity: 1068}, VolumeBlockedEntries: []EntryVolumeBlock{{Symbol: "loss", SignalDate: "2026-01-01"}, {Symbol: "missed-win", SignalDate: "2026-01-01"}, {Symbol: "not-a-baseline-fill", SignalDate: "2026-01-01"}}}
	impact := volumeStudyImpact(baseline, filtered, 20)
	if impact.BlockedSignals != 3 || impact.AvoidedLosses != 1 || impact.AvoidedLossAmount != 20 || impact.MissedWins != 1 || impact.MissedProfitAmount != 10 || impact.RetainedEntries != 1 || impact.NewEntries != 2 || impact.OtherRemovedEntries != 1 {
		t.Fatalf("wrong opportunity attribution: %+v", impact)
	}
	if impact.BaselineBigWins != 1 || impact.RetainedBigWinEntries != 1 || impact.BigWinProfitDelta != -40 || impact.RetainedWinnerDelta != -40 || impact.EquityDifference != -17 || math.Abs(impact.ReconciliationError) > 1e-8 {
		t.Fatalf("winner or marked equity delta not reconciled: %+v", impact)
	}
}
