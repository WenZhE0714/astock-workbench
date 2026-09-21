package paper

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func experimentFixture(t *testing.T) (PlanExperiment, domain.TradePlan, []domain.DailyBar) {
	t.Helper()
	created := time.Date(2026, 9, 21, 9, 29, 0, 0, shanghaiLocation)
	plan := domain.TradePlan{ID: strings.Repeat("a", 64), Version: 1, Symbol: "sh600519", CreatedAt: created.AddDate(0, 0, -3), ExpiresOn: "2026-09-28", Analysis: domain.ChartAnalysis{Version: "chart-v1", Symbol: "sh600519", Fingerprint: strings.Repeat("b", 64), DataDate: "2026-09-18"}, Structure: domain.ChartStructure{ID: "range-breakout", Name: "区间突破", Plan: &domain.ChartPlanLevels{EntryLow: 100, EntryHigh: 102, Invalidation: 95, Target1: 109, Target2: 116, Confirmation: "完整日K确认"}}}
	state, err := ConfigurePlanExperiment(PlanExperiment{}, true, created)
	if err != nil {
		t.Fatal(err)
	}
	state, err = SelectPlanExperiment(state, plan, true, created)
	if err != nil {
		t.Fatal(err)
	}
	bars := make([]domain.DailyBar, 0, 20)
	date := time.Date(2026, 8, 24, 0, 0, 0, 0, shanghaiLocation)
	for len(bars) < 20 {
		if date.Weekday() != time.Saturday && date.Weekday() != time.Sunday {
			bars = append(bars, domain.DailyBar{Symbol: plan.Symbol, Date: date.Format(time.DateOnly), Open: 100, High: 101, Low: 99, Close: 100, Volume: 100000, Amount: 1e8})
		}
		date = date.AddDate(0, 0, 1)
	}
	return state, plan, bars
}

func experimentInput(plan domain.TradePlan, bars []domain.DailyBar, at time.Time, price float64) PlanExperimentInput {
	phase := "confirmed"
	if price >= 100 && price <= 102 {
		phase = "in_zone"
	}
	return PlanExperimentInput{Symbol: plan.Symbol, Qualified: true, Quote: PositionQuote{Symbol: plan.Symbol, Price: price, PreviousClose: 100, LimitUp: 110, LimitDown: 90, Volume: 10000, Amount: 1000000, QuoteTime: at.Format("2006-01-02 15:04:05"), Source: "fixture"}, Bars: bars, CalendarDates: []string{"2026-09-18", "2026-09-21", "2026-09-22", "2026-09-23"}, Monitors: map[string]domain.PlanMonitor{plan.ID: {Version: 1, PlanID: plan.ID, Symbol: plan.Symbol, Enabled: true, Fingerprint: plan.Analysis.Fingerprint, Phase: phase, DataStatus: "healthy", ConfirmedOn: "2026-09-18", HistoryDate: "2026-09-18", LastQuoteAt: at}}}
}

func advanceExperimentTest(t *testing.T, state PlanExperiment, input PlanExperimentInput, now time.Time) PlanExperiment {
	t.Helper()
	next, err := AdvancePlanExperiment(state, []PlanExperimentInput{input}, now, "trading")
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func TestPlanExperimentNextQuoteTPlusOneAndFees(t *testing.T) {
	state, plan, bars := experimentFixture(t)
	at := time.Date(2026, 9, 21, 9, 30, 0, 0, shanghaiLocation)
	state = advanceExperimentTest(t, state, experimentInput(plan, bars, at, 101), at.Add(time.Second))
	if len(state.Arms[0].Report.Orders) != 0 || state.Arms[0].Trials[plan.ID].PendingEntry == nil {
		t.Fatal("same signal quote filled")
	}
	at = at.Add(30 * time.Second)
	state = advanceExperimentTest(t, state, experimentInput(plan, bars, at, 101), at.Add(time.Second))
	for _, arm := range state.Arms {
		if len(arm.Report.Orders) != 1 || arm.Report.Positions[0].AvailableQuantity != 0 || arm.Report.TotalFees <= 0 || arm.Report.Config.EnableIntradayT {
			t.Fatalf("entry/T+1/fees: %+v", arm.Report)
		}
		if arm.Report.Orders[0].ExecutionTime != at.Format("2006-01-02 15:04:05") {
			t.Fatal("fabricated opening timestamp")
		}
	}
	originalCash := state.Arms[0].Report.RemainingCash
	state = advanceExperimentTest(t, state, experimentInput(plan, bars, at, 101), at.Add(2*time.Second))
	if state.Arms[0].Report.RemainingCash != originalCash || len(state.Arms[0].Report.Orders) != 1 {
		t.Fatal("duplicate quote bought twice")
	}
	at = at.Add(30 * time.Second)
	state = advanceExperimentTest(t, state, experimentInput(plan, bars, at, 94), at.Add(time.Second))
	at = at.Add(30 * time.Second)
	state = advanceExperimentTest(t, state, experimentInput(plan, bars, at, 93), at.Add(time.Second))
	if len(state.Arms[0].Report.Trades) != 0 || state.Arms[0].Trials[plan.ID].PendingExit == nil {
		t.Fatal("T+1 risk exit was filled or lost")
	}
	state, err := ConfigurePlanExperiment(state, false, at.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	at = time.Date(2026, 9, 22, 9, 31, 0, 0, shanghaiLocation)
	input := experimentInput(plan, bars, at, 96)
	input.Monitors = nil // Existing positions remain protected even without a monitor.
	state = advanceExperimentTest(t, state, input, at.Add(time.Second))
	for _, arm := range state.Arms {
		if len(arm.Report.Positions) != 0 || len(arm.Report.Trades) != 1 || arm.Report.TotalProfit >= 0 || arm.Trials[plan.ID].Status != "closed" {
			t.Fatalf("next-day exit failed: %+v", arm)
		}
	}
}

func TestPlanExperimentArmsDifferOnlyByEntryFilter(t *testing.T) {
	state, plan, bars := experimentFixture(t)
	at := time.Date(2026, 9, 21, 9, 30, 0, 0, shanghaiLocation)
	for _, price := range []float64{103, 103.1, 101, 101.1} {
		state = advanceExperimentTest(t, state, experimentInput(plan, bars, at, price), at.Add(time.Second))
		at = at.Add(30 * time.Second)
	}
	if len(state.Arms[0].Report.Orders) != 1 || len(state.Arms[1].Report.Orders) != 1 {
		t.Fatal("paired experiment did not enter both arms")
	}
	if state.Arms[0].Report.Orders[0].RawPrice >= state.Arms[1].Report.Orders[0].RawPrice {
		t.Fatal("range filter had no effect")
	}
	if !sameExperimentJSON(state.Arms[0].Report.Config, state.Arms[1].Report.Config) || state.StartedAt.IsZero() {
		t.Fatal("comparison configuration/start diverged")
	}
}

func TestPlanExperimentCannotFillBeforeObservedSignal(t *testing.T) {
	state, plan, bars := experimentFixture(t)
	quoteAt := time.Date(2026, 9, 21, 9, 30, 0, 0, shanghaiLocation)
	state = advanceExperimentTest(t, state, experimentInput(plan, bars, quoteAt, 101), quoteAt.Add(time.Minute))
	state = advanceExperimentTest(t, state, experimentInput(plan, bars, quoteAt.Add(30*time.Second), 101), quoteAt.Add(70*time.Second))
	if len(state.Arms[0].Report.Orders) != 0 {
		t.Fatal("late-arriving historical quote filled before signal observation")
	}
	state = advanceExperimentTest(t, state, experimentInput(plan, bars, quoteAt.Add(61*time.Second), 101), quoteAt.Add(80*time.Second))
	if len(state.Arms[0].Report.Orders) != 1 {
		t.Fatal("later valid quote did not fill")
	}
}

func TestPlanExperimentRejectsCapacityBoundsAndStaleData(t *testing.T) {
	for _, kind := range []string{"amount", "limits", "halt", "stale", "paused"} {
		t.Run(kind, func(t *testing.T) {
			state, plan, bars := experimentFixture(t)
			at := time.Date(2026, 9, 21, 9, 30, 0, 0, shanghaiLocation)
			state = advanceExperimentTest(t, state, experimentInput(plan, bars, at, 101), at.Add(time.Second))
			at = at.Add(30 * time.Second)
			input := experimentInput(plan, bars, at, 101)
			switch kind {
			case "amount":
				input.Bars[0].Amount = 0
			case "limits":
				input.Quote.LimitUp = 0
			case "halt":
				input.Quote.Volume = 0
			case "stale":
				input.Quote.QuoteTime = at.Add(-3 * time.Minute).Format("2006-01-02 15:04:05")
			case "paused":
				monitor := input.Monitors[plan.ID]
				monitor.Enabled = false
				input.Monitors[plan.ID] = monitor
			}
			state = advanceExperimentTest(t, state, input, at.Add(time.Second))
			if len(state.Arms[0].Report.Orders) != 0 {
				t.Fatalf("%s generated fill", kind)
			}
		})
	}
}

func TestPlanExperimentValidationRejectsCashAndHistoryRewrite(t *testing.T) {
	state, plan, bars := experimentFixture(t)
	at := time.Date(2026, 9, 21, 9, 30, 0, 0, shanghaiLocation)
	state = advanceExperimentTest(t, state, experimentInput(plan, bars, at, 101), at.Add(time.Second))
	state = advanceExperimentTest(t, state, experimentInput(plan, bars, at.Add(30*time.Second), 101), at.Add(31*time.Second))
	changed, err := clonePlanExperiment(state)
	if err != nil {
		t.Fatal(err)
	}
	changed.Arms[0].Report.RemainingCash += 1
	if err := ValidatePlanExperimentTransition(state, changed); err == nil {
		t.Fatal("cash rewrite accepted")
	}
	changed, err = clonePlanExperiment(state)
	if err != nil {
		t.Fatal(err)
	}
	changed.Arms[0].Report.Orders[0].Reason = "changed"
	if err := ValidatePlanExperimentTransition(state, changed); err == nil {
		t.Fatal("settled order rewrite accepted")
	}
	if fmt.Sprint(PlanExperimentConfig().EnableIntradayT) != "false" {
		t.Fatal("intraday T is not disabled")
	}
}
