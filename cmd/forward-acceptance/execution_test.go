package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
)

func executionFixture(t *testing.T, ratio float64, deferEntry bool) (backtest.VolumeForwardPlan, []backtest.VolumeForwardDay) {
	t.Helper()
	plan, _, _, _ := fixture(t)
	var dates []time.Time
	for date := time.Date(2026, 6, 1, 16, 0, 0, 0, shanghai); len(dates) < 84; date = date.AddDate(0, 0, 1) {
		if date.Weekday() != time.Saturday && date.Weekday() != time.Sunday {
			dates = append(dates, date)
		}
	}
	start := dates[80].Add(-16 * time.Hour)
	plan.CreatedAt = dates[80].AddDate(0, 0, -1)
	for _, request := range []*backtest.Request{&plan.Baseline, &plan.Candidate} {
		request.Start, request.End = start, start
		request.Tickers = []string{"sh600110", "sz002413"}
	}
	plan.Fingerprint = ""
	plan.Fingerprint, _ = backtest.ForwardFingerprint(plan)
	bars := sealedExecutionBars{}
	for _, symbol := range []string{plan.Baseline.Benchmark, "sh600110", "sz002413"} {
		for i, date := range dates {
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
			bar := domain.DailyBar{Symbol: symbol, Date: date.Format(time.DateOnly), Source: "fixture", Open: open, Close: close, High: math.Max(open, close) + .2, Low: math.Min(open, close) - .2, Volume: volume}
			if deferEntry && i == 81 {
				bar.Open, bar.Close, bar.High, bar.Low = 11, 11, 11, 11
			}
			bars[symbol] = append(bars[symbol], bar)
		}
	}
	var days []backtest.VolumeForwardDay
	var previous *backtest.VolumeForwardDay
	for _, now := range dates[80:] {
		day, err := backtest.BuildVolumeForwardDay(t.Context(), plan, previous, bars, now)
		if err != nil {
			t.Fatal(err)
		}
		day = cloneDay(t, day)
		days = append(days, day)
		previous = &day
	}
	return plan, days
}

func cloneDay(t *testing.T, day backtest.VolumeForwardDay) backtest.VolumeForwardDay {
	t.Helper()
	data, err := json.Marshal(day)
	if err != nil {
		t.Fatal(err)
	}
	var result backtest.VolumeForwardDay
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func latestBars(day backtest.VolumeForwardDay) map[string]domain.DailyBar {
	bars := map[string]domain.DailyBar{}
	for symbol, items := range day.Bars {
		bars[symbol] = items[len(items)-1]
	}
	return bars
}

func TestExecutionAcceptsFourAccountsAndRoundTrip(t *testing.T) {
	for _, deferEntry := range []bool{false, true} {
		plan, days := executionFixture(t, 1.5, deferEntry)
		summary := newExecutionAcceptance()
		for i := 1; i < len(days); i++ {
			changes, err := verifyExecutionDay(t.Context(), plan, days[i-1], days[i])
			if err != nil {
				t.Fatal(err)
			}
			summary.record(days[i].Date, changes)
			if deferEntry && i == 1 && summary.Status != "waiting-fill" {
				t.Fatal("one-price deferral marked as a fill")
			}
		}
		if summary.Status != "passed" || summary.Transitions != 3 {
			t.Fatalf("unexpected progress: %+v", summary)
		}
		for _, account := range summary.Accounts {
			if account.Buys != 2 || account.Sells != 2 || account.FirstBuy >= account.FirstSell || deferEntry && account.DeferredOrders != 2 {
				t.Fatalf("wrong execution coverage: %+v", account)
			}
		}
	}
}

func TestExecutionKeepsUnfilledCandidatesPending(t *testing.T) {
	for _, ratio := range []float64{1.2, .5} {
		plan, days := executionFixture(t, ratio, false)
		summary := newExecutionAcceptance()
		for i := 1; i < len(days); i++ {
			changes, err := verifyExecutionDay(t.Context(), plan, days[i-1], days[i])
			if err != nil {
				t.Fatal(err)
			}
			summary.record(days[i].Date, changes)
		}
		want := "partial"
		if ratio == .5 {
			want = "waiting-fill"
		}
		if summary.Status != want || summary.Accounts[1].FirstBuy != "" || summary.Accounts[3].FirstBuy != "" {
			t.Fatalf("unfilled account counted as passed: %+v", summary)
		}
	}
}

func TestExecutionRejectsResealedBadData(t *testing.T) {
	plan, days := executionFixture(t, 1.5, false)
	cases := map[string]func(*backtest.VolumeForwardDay){
		"missing-bar": func(d *backtest.VolumeForwardDay) {
			d.Bars["sh600110"] = d.Bars["sh600110"][:len(d.Bars["sh600110"])-1]
		},
		"revised-history": func(d *backtest.VolumeForwardDay) { d.Bars["sh600110"][0].Volume++ },
		"missing-fill":    func(d *backtest.VolumeForwardDay) { d.Runs[0].OpenPositions = nil },
		"changed-cash":    func(d *backtest.VolumeForwardDay) { d.Runs[0].Equity[1].Cash++ },
		"changed-costs":   func(d *backtest.VolumeForwardDay) { d.Runs[2].Request.SlippageBPS /= 2 },
		"unsealed-signal": func(d *backtest.VolumeForwardDay) { d.Runs[0].OpenPositions[0].EntrySignal.Date = d.Date },
		"non-lot":         func(d *backtest.VolumeForwardDay) { d.Runs[0].OpenPositions[0].Entry.Quantity++ },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			day := cloneDay(t, days[1])
			mutate(&day)
			day.Fingerprint = ""
			day.Fingerprint, _ = backtest.ForwardFingerprint(day)
			if _, err := verifyExecutionDay(t.Context(), plan, days[0], day); err == nil {
				t.Fatal("a valid hash hid invalid execution evidence")
			}
		})
	}
	if _, err := verifyExecutionDay(t.Context(), plan, days[0], days[2]); err == nil {
		t.Fatal("missing intermediate seal accepted")
	}
}

func TestOpeningLedgerIndependentlyRejectsExecutionViolations(t *testing.T) {
	_, days := executionFixture(t, 1.5, false)
	cases := map[string]struct {
		mutate func(*backtest.Result, *backtest.Result)
		want   string
	}{
		"missing-prior-order": {func(p, c *backtest.Result) { p.PendingOrders = nil }, "待执行信号"},
		"incorrect-fee":       {func(p, c *backtest.Result) { c.OpenPositions[0].Entry.TotalFee++ }, "费用"},
		"position-limit":      {func(p, c *backtest.Result) { c.Request.Technical.MaxPosition = .01 }, "仓位上限"},
		"cash-reconciliation": {func(p, c *backtest.Result) { c.Equity[1].Cash++ }, "未对平"},
		"non-lot": {func(p, c *backtest.Result) {
			c.OpenPositions[0].Entry.Quantity++
			c.OpenPositions[0].Quantity++
		}, "整手"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			previous, current := cloneDay(t, days[0]), cloneDay(t, days[1])
			tc.mutate(&previous.Runs[0], &current.Runs[0])
			_, err := verifyOpeningLedger(previous.Runs[0], current.Runs[0], latestBars(current), current.Date)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %s, got %v", tc.want, err)
			}
		})
	}
	previous, current := cloneDay(t, days[2]), cloneDay(t, days[3])
	for i := range previous.Runs[0].OpenPositions {
		previous.Runs[0].OpenPositions[i].Entry.Date = current.Date
	}
	for i := range current.Runs[0].Trades {
		current.Runs[0].Trades[i].Entry.Date = current.Date
	}
	if _, err := verifyOpeningLedger(previous.Runs[0], current.Runs[0], latestBars(current), current.Date); err == nil || !strings.Contains(err.Error(), "T+1") {
		t.Fatalf("same-day sale accepted: %v", err)
	}
	_, deferred := executionFixture(t, 1.5, true)
	deferred[1].Runs[0].PendingOrders = nil
	if _, err := verifyOpeningLedger(deferred[0].Runs[0], deferred[1].Runs[0], latestBars(deferred[1]), deferred[1].Date); err == nil || !strings.Contains(err.Error(), "顺延") {
		t.Fatalf("lost one-price order accepted: %v", err)
	}
}
