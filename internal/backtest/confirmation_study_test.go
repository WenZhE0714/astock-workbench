package backtest

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestConfirmationBaselineAcceptsOmittedEmptyPositionsOnly(t *testing.T) {
	original := Result{Request: testRequest(), OpenPositions: []OpenPosition{}, Trades: []Trade{}, Equity: []EquityPoint{{Date: "2026-01-01", Equity: 1000}}}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var persisted Result
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if !sameConfirmationBaseline(original, persisted) {
		t.Fatal("JSON round trip changed baseline identity")
	}
	persisted.Equity[0].Equity++
	if sameConfirmationBaseline(original, persisted) {
		t.Fatal("actual equity change was accepted")
	}
}

func TestConfirmationStudyChangesOnlyConfirmationAndStressCosts(t *testing.T) {
	source := positionStudySource()
	engine := &positionStudyEngine{coverage: true}
	prior, err := RunPositionStudy(context.Background(), engine, source, "P008", []float64{.2, .1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine.requests = nil
	result, err := RunConfirmationStudy(context.Background(), engine, source, "P008", prior.Variants[1], nil)
	if err != nil || !result.Completed || !result.BaselineReproduced || result.PromotionAllowed || len(engine.requests) != 12 {
		t.Fatalf("invalid paired study: %+v %v", result, err)
	}
	for i := 0; i < 6; i++ {
		a, b := engine.requests[i], engine.requests[i+6]
		if a.EntryConfirmation != "" || b.EntryConfirmation != EntryConfirmReclaimTwoClose || a.Technical.MaxPosition != .1 {
			t.Fatal("wrong baseline or confirmation")
		}
		b.EntryConfirmation = ""
		if !reflect.DeepEqual(a, b) {
			t.Fatal("non-confirmation input changed")
		}
	}
	if result.Variants[1].DoubleCost.Passed {
		t.Fatal("original cost stress gate was bypassed")
	}
	prior.Variants[1].Windows[0].Normal.Equity = []EquityPoint{{Date: "changed", Equity: 42}}
	engine.requests = nil
	result, err = RunConfirmationStudy(context.Background(), engine, source, "P008", prior.Variants[1], nil)
	if err == nil || result.Completed || result.BaselineReproduced || len(result.BaselineDifferences) == 0 || len(engine.requests) != 6 {
		t.Fatal("equity drift did not stop before confirmation variant")
	}
}

func TestConfirmationImpactCountsReducedWinnersAndReconcilesOpenPositions(t *testing.T) {
	trade := func(symbol string, profit, ret float64) Trade {
		return Trade{Symbol: symbol, EntrySignal: SignalSnapshot{Date: "2026-01-01"}, NetProfit: profit, ReturnPercent: ret}
	}
	baseline := Result{Trades: []Trade{trade("loss", -20, -2), trade("missed-win", 10, 1), trade("big-win", 100, 25), trade("path-change", -5, -1)}, Metrics: Metrics{FinalEquity: 1085}}
	confirmed := Result{Trades: []Trade{trade("big-win", 60, 15), trade("new", 15, 2)}, OpenPositions: []OpenPosition{{Symbol: "open", EntrySignal: SignalSnapshot{Date: "2026-01-01"}, UnrealizedProfit: -7}}, Metrics: Metrics{FinalEquity: 1068}, EntryConfirmations: []EntryConfirmationEvent{{Symbol: "loss", SignalDate: "2026-01-01", Status: "rejected"}, {Symbol: "missed-win", SignalDate: "2026-01-01", Status: "rejected"}, {Symbol: "big-win", SignalDate: "2026-01-01", Status: "confirmed"}}}
	impact := confirmationImpact(baseline, confirmed, 20)
	if impact.AvoidedLosses != 1 || impact.AvoidedLossAmount != 20 || impact.MissedWins != 1 || impact.MissedProfitAmount != 10 || impact.OtherRemovedEntries != 1 || impact.NewEntries != 2 || impact.MatchedEntries != 1 {
		t.Fatalf("incorrect opportunity attribution: %+v", impact)
	}
	if impact.ReducedMatchedWins != 1 || impact.MatchedWinProfitDelta != -40 || impact.BaselineBigWins != 1 || impact.RetainedBigWinEntries != 1 || impact.BigWinProfitDelta != -40 {
		t.Fatalf("retained but reduced winner hidden: %+v", impact)
	}
	if impact.AttributedProfitDelta != -17 || math.Abs(impact.ReconciliationError) > 1e-8 {
		t.Fatalf("equity delta did not reconcile: %+v", impact)
	}
}
