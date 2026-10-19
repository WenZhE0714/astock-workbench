package backtest

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func forwardFixture(t *testing.T, ratio float64) (VolumeForwardPlan, forwardBars, []time.Time) {
	t.Helper()
	dates := []time.Time{}
	for d := time.Date(2026, 6, 1, 16, 0, 0, 0, forwardLocation); len(dates) < 90; d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			dates = append(dates, d)
		}
	}
	base := Request{Strategy: "technical-breakout", StrategyVersion: "v1", Tickers: []string{"sh600110"}, Names: map[string]string{"sh600110": "test"}, Start: dates[0], End: dates[70], InitialCash: 1000000, CommissionRate: .0003, MinimumCommission: 5, StampDutyRate: .0005, TransferFeeRate: .00001, SlippageBPS: 5, Adjustment: AdjustmentNone, Benchmark: "sh000300", NoFutureData: true, LiquidateAtEnd: true, Technical: DefaultTechnicalParameters()}
	base.Technical.EntryMode, base.Technical.MaxPosition = EntryModeReclaim, .10
	candidate := base
	candidate.EntryVolumeFloor = 1.5
	source := VolumeStudyResult{Completed: true, SourceID: "test", CandidateID: "P008", MaxPosition: .10, BaselineReproduced: true, Variants: []VolumeStudyVariant{
		{Minimum: 0, PositionStudyVariant: PositionStudyVariant{Windows: []PositionStudyWindow{{Normal: Result{Request: base}}, {Normal: Result{Request: base}}}}},
		{Minimum: 1.5, PositionStudyVariant: PositionStudyVariant{Windows: []PositionStudyWindow{{Normal: Result{Request: candidate}}, {Normal: Result{Request: candidate}}}, Normal: PositionStudySummary{Passed: true}, DoubleCost: PositionStudySummary{Passed: true}}},
	}}
	plan, err := NewVolumeForwardPlan(source, strings.Repeat("a", 64), strings.Repeat("b", 64), dates[80].AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	bars := forwardBars{}
	for _, symbol := range []string{"sh000300", "sh600110"} {
		for i, d := range dates {
			close := 10.0
			if i < 50 {
				close = 8
			}
			open, volume := close, 100.0
			switch i {
			case 79:
				close = 9.9
			case 80:
				close, volume = 11, ratio*100
			case 81:
				open, close = 11.1, 11
			case 82:
				open, close = 11, 9
			case 83:
				open, close = 9, 9
			}
			bars[symbol] = append(bars[symbol], domain.DailyBar{Symbol: symbol, Source: "fixture", Date: d.Format("2006-01-02"), Open: open, Close: close, High: math.Max(open, close) + .2, Low: math.Min(open, close) - .2, Volume: volume})
		}
	}
	return plan, bars, dates
}

func TestVolumeForwardSealsSignalBeforeNextOpenAndKeepsAccountsSeparate(t *testing.T) {
	plan, bars, dates := forwardFixture(t, 1.2)
	first, err := BuildVolumeForwardDay(t.Context(), plan, nil, bars, dates[80])
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Runs[0].PendingOrders) != 1 || len(first.Runs[1].PendingOrders) != 0 || len(first.Runs[1].VolumeBlockedEntries) != 1 {
		t.Fatalf("signal/filter mismatch: %+v", first.Runs)
	}
	for _, run := range first.Runs {
		if len(run.Trades) != 0 || len(run.OpenPositions) != 0 || len(run.Equity) != 1 || run.Metrics.FinalEquity != 1000000 || run.Request.LiquidateAtEnd {
			t.Fatal("first close invented fills or liquidation")
		}
	}
	second, err := BuildVolumeForwardDay(t.Context(), plan, &first, bars, dates[81])
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Runs[0].OpenPositions) != 1 || len(second.Runs[1].OpenPositions) != 0 || second.PreviousHash != first.Fingerprint {
		t.Fatal("independent cash/filter path lost")
	}
	entry := second.Runs[0].OpenPositions[0]
	if entry.EntrySignal.Date != first.Date || entry.Entry.Date != second.Date || entry.Entry.RawPrice != 11.1 || entry.Quantity%100 != 0 || entry.Entry.Amount+entry.Entry.TotalFee > 100000 {
		t.Fatalf("wrong opening execution: %+v", entry)
	}
	if second.Runs[2].OpenPositions[0].Entry.TotalFee <= entry.Entry.TotalFee || second.SampleReady || second.PromotionAllowed || second.Assessment.Passed {
		t.Fatal("stress fees or sample gate invalid")
	}
	third, err := BuildVolumeForwardDay(t.Context(), plan, &second, bars, dates[82])
	if err != nil {
		t.Fatal(err)
	}
	fourth, err := BuildVolumeForwardDay(t.Context(), plan, &third, bars, dates[83])
	if err != nil {
		t.Fatal(err)
	}
	if len(fourth.Runs[0].Trades) != 1 || fourth.NormalImpact.AvoidedLosses != 1 || math.Abs(fourth.NormalImpact.ReconciliationError) > .01 {
		t.Fatal("realized loss attribution or reconciliation incorrect")
	}
}

func TestVolumeForwardRejectsPastIntradayGapAndHistoricalRewrite(t *testing.T) {
	plan, bars, dates := forwardFixture(t, 1.5)
	for _, now := range []time.Time{plan.CreatedAt, dates[80].Add(-time.Hour)} {
		if _, err := BuildVolumeForwardDay(t.Context(), plan, nil, bars, now); err == nil {
			t.Fatal("pre-start or intraday capture allowed")
		}
	}
	if _, err := BuildVolumeForwardDay(t.Context(), plan, nil, bars, dates[81]); err == nil {
		t.Fatal("missed first close silently backfilled")
	}
	first, err := BuildVolumeForwardDay(t.Context(), plan, nil, bars, dates[80])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildVolumeForwardDay(t.Context(), plan, &first, bars, dates[82]); err == nil {
		t.Fatal("missing middle close backfilled")
	}
	bars["sh600110"][80].Volume++
	if _, err := BuildVolumeForwardDay(t.Context(), plan, &first, bars, dates[81]); err == nil {
		t.Fatal("historical signal rewritten")
	}
	bars["sh600110"][80].Volume--
	bars["sh600110"] = append(bars["sh600110"][:81], bars["sh600110"][82:]...)
	if _, err := BuildVolumeForwardDay(t.Context(), plan, &first, bars, dates[81]); err == nil {
		t.Fatal("missing stock session accepted")
	}
}

func TestVolumeForwardFrozenInputsIgnoreFutureBarsAndPreserveT1Deferral(t *testing.T) {
	plan, bars, dates := forwardFixture(t, 1.5)
	first, err := BuildVolumeForwardDay(t.Context(), plan, nil, bars, dates[80])
	if err != nil {
		t.Fatal(err)
	}
	bars["sh600110"][85].Volume = 1000000
	again, err := BuildVolumeForwardDay(t.Context(), plan, nil, bars, dates[80])
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatal("future data leaked into sealed day", err)
	}
	bar := &bars["sh600110"][81]
	bar.Open, bar.Close, bar.High, bar.Low = 11, 11, 11, 11
	second, err := BuildVolumeForwardDay(t.Context(), plan, &first, bars, dates[81])
	if err != nil || len(second.Runs[1].OpenPositions) != 0 || len(second.Runs[1].PendingOrders) != 1 {
		t.Fatal("one-price signal not deferred", err)
	}
	third, err := BuildVolumeForwardDay(t.Context(), plan, &second, bars, dates[82])
	if err != nil || len(third.Runs[1].OpenPositions) != 1 || len(third.Runs[1].Trades) != 0 || len(third.Runs[1].PendingOrders) != 1 {
		t.Fatal("T+1 or pending exit incorrect", err)
	}
	fourth, err := BuildVolumeForwardDay(t.Context(), plan, &third, bars, dates[83])
	if err != nil || len(fourth.Runs[1].Trades) != 1 || fourth.Runs[1].Trades[0].Entry.Date != third.Date || fourth.Runs[1].Trades[0].Exit.Date != fourth.Date {
		t.Fatal("deferred trade timing incorrect", err)
	}
	request := plan.Baseline
	request.End, request.LiquidateAtEnd = dates[81], true
	if _, err := NewDailyEngine(bars).RunForward(context.Background(), request); err == nil {
		t.Fatal("forward terminal liquidation accepted")
	}
}

func TestVolumeForwardRejectsConfigurationAndDayTampering(t *testing.T) {
	plan, bars, dates := forwardFixture(t, 1.5)
	changed := plan
	changed.Candidate.EntryVolumeFloor = 1.6
	if ValidateVolumeForwardPlan(changed) == nil {
		t.Fatal("changed threshold accepted")
	}
	day, err := BuildVolumeForwardDay(t.Context(), plan, nil, bars, dates[80])
	if err != nil {
		t.Fatal(err)
	}
	day.Runs[0].Metrics.FinalEquity++
	if ValidateVolumeForwardDay(plan, day) == nil {
		t.Fatal("changed equity accepted")
	}
}

func TestVolumeForwardMissingTurnoverSurvivesSealAndRestart(t *testing.T) {
	plan, bars, dates := forwardFixture(t, 1.5)
	for symbol := range bars {
		for i := range bars[symbol] {
			bars[symbol][i].Turnover = math.NaN()
		}
	}
	first, err := BuildVolumeForwardDay(t.Context(), plan, nil, bars, dates[80])
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(first)
	if err != nil || !strings.Contains(string(data), `"turnover_percent":null`) {
		t.Fatalf("missing turnover was not encoded as null: %v", err)
	}
	var restored VolumeForwardDay
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(restored.Bars["sh000300"][0].Turnover) {
		t.Fatal("missing turnover became a measured zero")
	}
	if err := ValidateVolumeForwardDay(plan, restored); err != nil {
		t.Fatal(err)
	}
	next, err := BuildVolumeForwardDay(t.Context(), plan, &restored, bars, dates[81])
	if err != nil || len(next.Runs[1].OpenPositions) != 1 || next.PreviousHash != first.Fingerprint {
		t.Fatalf("restart did not preserve the pending entry: %v", err)
	}
	bars["sh600110"][82].Volume = math.NaN()
	if _, err := BuildVolumeForwardDay(t.Context(), plan, &next, bars, dates[82]); err == nil {
		t.Fatal("invalid required volume was accepted")
	}
}

func TestVolumeForwardFiniteBarsKeepExistingJSON(t *testing.T) {
	_, bars, _ := forwardFixture(t, 1.5)
	bars["sh600110"][0].Turnover = 2.75
	for _, value := range []forwardBars{nil, {}, {"nil": nil, "empty": {}}, bars} {
		want, err := json.Marshal(map[string][]domain.DailyBar(value))
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(value)
		if err != nil || string(got) != string(want) {
			t.Fatalf("finite archive encoding changed: %v", err)
		}
		var restored forwardBars
		if err := json.Unmarshal(got, &restored); err != nil || !reflect.DeepEqual(value, restored) {
			t.Fatalf("finite bars changed on reload: %v", err)
		}
	}
}
