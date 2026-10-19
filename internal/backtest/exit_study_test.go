package backtest

import (
	"context"
	"math"
	"reflect"
	"testing"
)

func TestExitStudyChangesOnlyProtectionAndPreservesBaseline(t *testing.T) {
	source := positionStudySource()
	engine := &positionStudyEngine{coverage: true}
	prior, err := RunPositionStudy(context.Background(), engine, source, "P008", []float64{.2, .1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine.requests = nil
	result, err := RunExitStudy(context.Background(), engine, historyRangeMock{}, source, "P008", prior.Variants[1], nil)
	if err != nil || !result.Completed || !result.BaselineReproduced || result.PromotionAllowed || len(engine.requests) != 12 || len(result.Windows) != 3 {
		t.Fatalf("invalid exit study: %+v %v", result, err)
	}
	for i := 0; i < 6; i++ {
		a, b := engine.requests[i], engine.requests[i+6]
		if a.ExitProtection != "" || b.ExitProtection != ExitProtectHalfProfit || a.Technical.MaxPosition != .1 {
			t.Fatal("unexpected study variant")
		}
		b.ExitProtection = ""
		if !reflect.DeepEqual(a, b) {
			t.Fatal("non-exit parameter changed")
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
			t.Fatal("double-cost scenario changed strategy")
		}
	}
	prior.Variants[1].Windows[0].Normal.Equity = []EquityPoint{{Equity: 42}}
	engine.requests = nil
	result, err = RunExitStudy(context.Background(), engine, historyRangeMock{}, source, "P008", prior.Variants[1], nil)
	if err == nil || result.Completed || result.BaselineReproduced || len(engine.requests) != 6 {
		t.Fatal("baseline drift failed to stop experiment")
	}
}

func TestExitEfficiencyProbePreservesEntryQuantityAndPendingExit(t *testing.T) {
	bars, request := exitProtectionFixture()
	request.ExitProtection = ""
	baseline, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(baseline.Trades) != 1 {
		t.Fatalf("missing baseline trade: %+v %v", baseline, err)
	}
	trade := baseline.Trades[0]
	row, err := reviewExitEfficiencyTrade(request, trade, bars)
	if err != nil || row.ProtectionFill == nil || row.ProtectionSignal.Date != bars[62].Date || row.ProtectionFill.Date != bars[63].Date || row.ProtectionFill.Quantity != trade.Entry.Quantity {
		t.Fatalf("invalid fixed-entry probe: %+v %v", row, err)
	}
	request.ExitProtection = ExitProtectHalfProfit
	protected, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || !reflect.DeepEqual(*row.ProtectionFill, protected.Trades[0].Exit) || math.Abs(*row.ProtectionProfit-protected.Trades[0].NetProfit) > 1e-8 {
		t.Fatal("fixed-entry probe disagrees with first portfolio exit")
	}
	// A pending original exit forbids a later overlay even if execution is delayed.
	trade.ExitSignal.Date, trade.ExitSignal.Close = bars[61].Date, bars[61].Close
	row, err = reviewExitEfficiencyTrade(request, trade, bars)
	if err != nil || row.ProtectionFill != nil {
		t.Fatal("probe replaced a pending original exit")
	}
}

func TestExitStudyAttributionIncludesNewEntriesAndOpenPositions(t *testing.T) {
	baseline := Result{Trades: []Trade{{Symbol: "winner", EntrySignal: SignalSnapshot{Date: "d"}, NetProfit: 100, ReturnPercent: 25}, {Symbol: "loss", EntrySignal: SignalSnapshot{Date: "d"}, NetProfit: -20}}, Metrics: Metrics{FinalEquity: 1080}}
	protected := Result{Trades: []Trade{{Symbol: "winner", EntrySignal: SignalSnapshot{Date: "d"}, NetProfit: 60, ExitSignal: SignalSnapshot{ProfitProtection: &ProfitProtectionEvidence{}}}, {Symbol: "new", EntrySignal: SignalSnapshot{Date: "n"}, NetProfit: 15}}, OpenPositions: []OpenPosition{{Symbol: "open", EntrySignal: SignalSnapshot{Date: "n"}, UnrealizedProfit: -5}}, Metrics: Metrics{FinalEquity: 1070}}
	impact := exitStudyImpact(baseline, protected, 20)
	if impact.ProtectionExits != 1 || impact.MatchedEntries != 1 || impact.NewEntries != 2 || impact.RemovedEntries != 1 || impact.BaselineWinnerDelta != -40 || impact.BaselineLossDelta != 20 || impact.BigWinProfitDelta != -40 || impact.EquityDifference != -10 || math.Abs(impact.ReconciliationError) > 1e-8 {
		t.Fatalf("invalid opportunity attribution: %+v", impact)
	}
}
