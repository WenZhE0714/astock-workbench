package strategy

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func monitorFixture(t *testing.T) (domain.TradePlan, domain.PlanMonitor, []domain.DailyBar) {
	t.Helper()
	bars := chartTestBars(80)
	now := chartTestTime(bars[78].Date, 16)
	analysis, err := AnalyzeChart("sh600519", bars[:79], now, "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildTradePlan(analysis, "range-breakout", now.AddDate(0, 0, 7).Format(time.DateOnly), now)
	if err != nil {
		t.Fatal(err)
	}
	state, err := ConfigurePlanMonitor(plan, domain.PlanMonitor{}, true, now)
	if err != nil {
		t.Fatal(err)
	}
	bars[79].Close, bars[79].High, bars[79].Volume = 104, 105, 1600
	return plan, state, bars
}

func closingObservation(bars []domain.DailyBar) PlanMonitorObservation {
	return PlanMonitorObservation{Now: chartTestTime(bars[79].Date, 16), Session: "closing", ClosingSlot: bars[79].Date + ":primary", CalendarKnown: true, TradingDate: bars[79].Date, PreviousTradingDate: bars[78].Date, Bars: bars}
}

func liveMonitorObservation(bars []domain.DailyBar, now time.Time, price float64) PlanMonitorObservation {
	return PlanMonitorObservation{
		Now: now, Session: "trading", CalendarKnown: true, TradingDate: now.Format(time.DateOnly), PreviousTradingDate: bars[len(bars)-1].Date, Bars: bars,
		Quote: domain.Quote{Symbol: "sh600519", Current: fmt.Sprintf("%.2f", price), PreviousClose: fmt.Sprintf("%.2f", bars[len(bars)-1].Close), QuoteTime: now.Format("2006-01-02 15:04:05")},
	}
}

func countMonitorEvents(state domain.PlanMonitor, kind string) int {
	count := 0
	for _, event := range state.Events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func TestPlanMonitorConfirmationZoneCooldownAndInvalidation(t *testing.T) {
	_, state, bars := monitorFixture(t)
	state = AdvancePlanMonitor(state, closingObservation(bars))
	if state.Phase != "confirmed" || state.ConfirmedOn != bars[79].Date || countMonitorEvents(state, "confirmed") != 1 {
		t.Fatalf("daily confirmation failed: %+v", state)
	}
	// The low on the confirmation candle precedes the close and cannot be
	// replayed as a stop after that close.
	if !state.Enabled {
		t.Fatal("confirmation candle low retroactively invalidated plan")
	}
	now := chartTestTime(bars[79].Date, 10).AddDate(0, 0, 3)
	observation := liveMonitorObservation(bars, now, 102.5)
	state = AdvancePlanMonitor(state, observation)
	if state.Phase != "in_zone" || countMonitorEvents(state, "zone_entered") != 1 {
		t.Fatalf("zone entry missing: %+v", state)
	}
	sequence := state.Sequence
	observation.Now = now.Add(30 * time.Second)
	state = AdvancePlanMonitor(state, observation)
	if state.Sequence != sequence {
		t.Fatal("same quote duplicated event")
	}
	state = AdvancePlanMonitor(state, liveMonitorObservation(bars, now.Add(time.Minute), 104))
	state = AdvancePlanMonitor(state, liveMonitorObservation(bars, now.Add(2*time.Minute), 102.5))
	if countMonitorEvents(state, "zone_entered") != 1 {
		t.Fatal("cooldown did not suppress repeat entry")
	}
	state = AdvancePlanMonitor(state, liveMonitorObservation(bars, now.Add(5*time.Minute), 104))
	state = AdvancePlanMonitor(state, liveMonitorObservation(bars, now.Add(6*time.Minute), 102.5))
	if countMonitorEvents(state, "zone_entered") != 2 {
		t.Fatal("entry never rearmed after cooldown")
	}
	state = AdvancePlanMonitor(state, liveMonitorObservation(bars, now.Add(6*time.Minute+30*time.Second), 97))
	if state.Phase != "invalidated" || state.Enabled || countMonitorEvents(state, "invalidated") != 1 {
		t.Fatalf("critical invalidation suppressed: %+v", state)
	}
	finished := AdvancePlanMonitor(state, liveMonitorObservation(bars, now.Add(7*time.Minute), 96))
	if !reflect.DeepEqual(state, finished) {
		t.Fatal("ended monitor changed")
	}
}

func TestPlanMonitorWaitingDoesNotInvalidateBeforeConfirmation(t *testing.T) {
	_, state, bars := monitorFixture(t)
	now := chartTestTime(bars[79].Date, 10)
	observation := liveMonitorObservation(bars[:79], now, 95)
	observation.Bars = bars // Includes an unfinished breakout candle.
	state = AdvancePlanMonitor(state, observation)
	if state.Phase != "waiting" || state.ConfirmedOn != "" || countMonitorEvents(state, "invalidated") != 0 || state.DataStatus != "healthy" {
		t.Fatalf("future/intraday candle influenced plan: %+v", state)
	}
}

func TestPlanMonitorRefusesStaleOrUnqualifiedEvidence(t *testing.T) {
	_, initial, bars := monitorFixture(t)
	now := chartTestTime(bars[79].Date, 10).AddDate(0, 0, 3)
	for _, sample := range []struct {
		name, status string
		change       func(*PlanMonitorObservation)
	}{
		{"unknown-calendar", "calendar_unavailable", func(input *PlanMonitorObservation) { input.CalendarKnown = false }},
		{"old-quote", "stale_quote", func(input *PlanMonitorObservation) {
			input.Quote.QuoteTime = now.Add(-3 * time.Minute).Format(time.RFC3339)
		}},
		{"future-quote", "stale_quote", func(input *PlanMonitorObservation) { input.Quote.QuoteTime = now.Add(time.Minute).Format(time.RFC3339) }},
		{"old-history", "stale_history", func(input *PlanMonitorObservation) { input.Bars = input.Bars[:79] }},
		{"wrong-symbol", "unavailable", func(input *PlanMonitorObservation) { input.Quote.Symbol = "sz000001" }},
		{"missing-time", "unavailable", func(input *PlanMonitorObservation) { input.Quote.QuoteTime = "" }},
		{"missing-price", "unavailable", func(input *PlanMonitorObservation) { input.Quote.Current = "NaN" }},
		{"adjustment-mismatch", "price_basis_mismatch", func(input *PlanMonitorObservation) { input.Quote.PreviousClose = "90" }},
		{"provider-error", "unavailable", func(input *PlanMonitorObservation) { input.Error = "provider offline" }},
	} {
		t.Run(sample.name, func(t *testing.T) {
			input := liveMonitorObservation(bars, now, 102.5)
			sample.change(&input)
			state := AdvancePlanMonitor(initial, input)
			if state.DataStatus != sample.status || state.Phase != "waiting" || !state.LastQuoteAt.IsZero() || state.ConfirmedOn != "" {
				t.Fatalf("unqualified data advanced monitor: %+v", state)
			}
			for _, event := range state.Events {
				if event.Notify {
					t.Fatalf("bad data produced alert: %+v", event)
				}
			}
		})
	}
}

func TestPlanMonitorPauseResumeExpiryAndWatermark(t *testing.T) {
	plan, state, bars := monitorFixture(t)
	state = AdvancePlanMonitor(state, closingObservation(bars))
	now := chartTestTime(bars[79].Date, 10).AddDate(0, 0, 3)
	state = AdvancePlanMonitor(state, liveMonitorObservation(bars, now, 102.5))
	paused, err := ConfigurePlanMonitor(plan, state, false, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	unchanged := AdvancePlanMonitor(paused, liveMonitorObservation(bars, now.Add(2*time.Minute), 97))
	if !reflect.DeepEqual(unchanged, paused) {
		t.Fatal("paused monitor evaluated quotes")
	}
	resumed, err := ConfigurePlanMonitor(plan, paused, true, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	input := liveMonitorObservation(bars, now.Add(3*time.Minute), 97)
	input.Quote.QuoteTime = now.Add(2 * time.Minute).Format(time.RFC3339)
	resumed = AdvancePlanMonitor(resumed, input)
	if resumed.Phase != "confirmed" || resumed.DataStatus != "waiting" {
		t.Fatal("resume replayed a paused quote")
	}
	expiredAt := chartTestTime(plan.ExpiresOn, 10).AddDate(0, 0, 1)
	expired := AdvancePlanMonitor(resumed, PlanMonitorObservation{Now: expiredAt, Session: "closed"})
	if expired.Enabled || expired.Phase != "expired" || countMonitorEvents(expired, "expired") != 1 {
		t.Fatalf("expiry not durable: %+v", expired)
	}
	if _, err := ConfigurePlanMonitor(plan, expired, true, expiredAt); err == nil {
		t.Fatal("expired monitor restarted")
	}
}

func TestPlanMonitorRechecksRevisedDailyVolumeWithoutDuplicatingConfirmation(t *testing.T) {
	_, state, bars := monitorFixture(t)
	bars[79].Volume = 900
	input := closingObservation(bars)
	state = AdvancePlanMonitor(state, input)
	if state.Phase != "waiting" {
		t.Fatal("weak volume confirmed")
	}
	bars[79].Volume = 1600
	input.Now = input.Now.Add(time.Minute)
	state = AdvancePlanMonitor(state, input)
	if state.Phase != "confirmed" || countMonitorEvents(state, "confirmed") != 1 {
		t.Fatal("same-date revised bar not checked")
	}
	input.Now = input.Now.Add(time.Minute)
	state = AdvancePlanMonitor(state, input)
	if countMonitorEvents(state, "confirmed") != 1 {
		t.Fatal("daily confirmation duplicated")
	}
}

func TestPlanMonitorDoesNotRewindQuotesOrContinueAfterSessionBoundary(t *testing.T) {
	_, state, bars := monitorFixture(t)
	state = AdvancePlanMonitor(state, closingObservation(bars))
	now := chartTestTime(bars[79].Date, 10).AddDate(0, 0, 3)
	state = AdvancePlanMonitor(state, liveMonitorObservation(bars, now, 102.5))
	watermark, sequence := state.LastQuoteAt, state.Sequence
	old := liveMonitorObservation(bars, now.Add(30*time.Second), 97)
	old.Quote.QuoteTime = now.Add(-time.Second).Format(time.RFC3339)
	state = AdvancePlanMonitor(state, old)
	if state.Phase != "in_zone" || !state.LastQuoteAt.Equal(watermark) || *state.Price != 102.5 || countMonitorEvents(state, "invalidated") != 0 {
		t.Fatal("out-of-order quote changed observation")
	}
	if state.Sequence < sequence {
		t.Fatal("event sequence rewound")
	}
	lunch := liveMonitorObservation(bars, time.Date(now.Year(), now.Month(), now.Day(), 11, 30, 0, 0, chartLocation), 97)
	lunch.Quote.QuoteTime = lunch.Now.Add(-time.Second).Format(time.RFC3339)
	state = AdvancePlanMonitor(state, lunch)
	if state.Phase != "in_zone" || countMonitorEvents(state, "invalidated") != 0 {
		t.Fatal("slow request triggered after session ended")
	}
}

func TestPlanMonitorPullbackRequiresSixtyCompletedBars(t *testing.T) {
	bars := chartTestBars(80)
	for index := range bars {
		price := 100 + float64(index)*.1
		bars[index].Open, bars[index].Close, bars[index].High, bars[index].Low = price, price, price+2, price-2
	}
	now := chartTestTime(bars[78].Date, 16)
	analysis, err := AnalyzeChart("sh600519", bars[:79], now, "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildTradePlan(analysis, "ma-pullback", now.AddDate(0, 0, 7).Format(time.DateOnly), now)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := ConfigurePlanMonitor(plan, domain.PlanMonitor{}, true, now)
	if err != nil {
		t.Fatal(err)
	}
	input := closingObservation(bars)
	input.Bars = bars[40:]
	missing := AdvancePlanMonitor(initial, input)
	if missing.Phase != "waiting" || missing.DataStatus != "stale_history" {
		t.Fatal("missing MA60 history treated as valid")
	}
	input.Bars = bars
	confirmed := AdvancePlanMonitor(initial, input)
	if confirmed.Phase != "confirmed" {
		t.Fatalf("pullback did not confirm: %+v", confirmed)
	}
}

func TestAssistantPullbackWithoutTrendUsesOnlyItsPriceRule(t *testing.T) {
	bars := chartTestBars(80)
	now := chartTestTime(bars[79].Date, 16)
	analysis, err := AnalyzeChart("sh600519", bars, now, "")
	if err != nil {
		t.Fatal(err)
	}
	proposal := domain.AssistantRuleProposal{
		Kind: "pullback", Name: "价格回踩", Description: "完整日K进入区间并收盘守住下沿",
		EntryLow: 99, EntryHigh: 101, Invalidation: 97, ConfirmationPrice: 100,
		VolumeDays: 20, MinimumVolumeRatio: 1.2, RequireTrend: false,
	}
	draft, err := BuildAssistantRuleDraft(analysis, analysis.Structures[0], "只按价格回踩观察", now.AddDate(0, 0, 7).Format(time.DateOnly), proposal, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildAssistantTradePlan(analysis, draft, now.Add(-30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	state, err := ConfigurePlanMonitor(plan, domain.PlanMonitor{}, true, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	state = AdvancePlanMonitor(state, PlanMonitorObservation{
		Now: now, Session: "closing", ClosingSlot: bars[79].Date + ":primary", CalendarKnown: true,
		TradingDate: bars[79].Date, PreviousTradingDate: bars[78].Date, Bars: bars[79:],
	})
	if state.Phase != "confirmed" || state.DataStatus != "healthy" {
		t.Fatalf("bounded pullback rule incorrectly required MA history: %+v", state)
	}
}

func TestMonitorQuoteDayAttestationIsNotWeekdayGuessing(t *testing.T) {
	now := time.Date(2026, 9, 21, 10, 0, 0, 0, chartLocation)
	quote := domain.Quote{Symbol: "sh600519", Current: "100.00", QuoteTime: now.Format(time.RFC3339)}
	if !MonitorQuoteConfirmsSession(quote, now, "trading") {
		t.Fatal("fresh current-day quote rejected")
	}
	quote.QuoteTime = now.AddDate(0, 0, -3).Format(time.RFC3339)
	if MonitorQuoteConfirmsSession(quote, now, "trading") {
		t.Fatal("weekday fallback accepted an old quote")
	}
	now = time.Date(2026, 9, 21, 15, 10, 0, 0, chartLocation)
	quote.QuoteTime = time.Date(2026, 9, 21, 15, 0, 0, 0, chartLocation).Format(time.RFC3339)
	if !MonitorQuoteConfirmsSession(quote, now, "closing") {
		t.Fatal("closing quote rejected")
	}
	quote.QuoteTime = time.Date(2026, 9, 21, 14, 59, 0, 0, chartLocation).Format(time.RFC3339)
	if MonitorQuoteConfirmsSession(quote, now, "closing") {
		t.Fatal("unfinished session quote attested close")
	}
}
