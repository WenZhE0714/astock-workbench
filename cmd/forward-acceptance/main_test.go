package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func fixture(t *testing.T) (backtest.VolumeForwardPlan, backtest.VolumeForwardDay, []byte, []byte) {
	t.Helper()
	source, binary := []byte(`{"completed":true}`), []byte("frozen-test-program")
	sourceHash, binaryHash := sha256.Sum256(source), sha256.Sum256(binary)
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, shanghai)
	request := backtest.Request{Strategy: "technical-breakout", StrategyVersion: backtest.VolumeForwardVersion, Tickers: []string{"sh600110"}, Start: start, End: start, InitialCash: 1000000, Adjustment: backtest.AdjustmentNone, Benchmark: "sh000300", NoFutureData: true, Technical: backtest.DefaultTechnicalParameters(), CommissionRate: .0003, MinimumCommission: 5, StampDutyRate: .0005, TransferFeeRate: .00001, SlippageBPS: 5}
	request.Technical.EntryMode, request.Technical.MaxPosition = backtest.EntryModeReclaim, .10
	plan := backtest.VolumeForwardPlan{Version: backtest.VolumeForwardVersion, CreatedAt: start.Add(-time.Hour), SourceID: "fixture", CandidateID: "P008", SourceSHA256: hex.EncodeToString(sourceHash[:]), ExecutableSHA256: hex.EncodeToString(binaryHash[:]), Baseline: request, Candidate: request, Policy: backtest.DefaultCandidateObservationPolicy()}
	plan.Candidate.EntryVolumeFloor = 1.5
	var err error
	plan.Fingerprint, err = backtest.ForwardFingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	day := backtest.VolumeForwardDay{Date: start.Format(time.DateOnly), CapturedAt: start.Add(16 * time.Hour), PlanFingerprint: plan.Fingerprint, Bars: map[string][]domain.DailyBar{}}
	for _, symbol := range []string{"sh000300", "sh600110"} {
		for i := 61; i >= 0; i-- {
			day.Bars[symbol] = append(day.Bars[symbol], domain.DailyBar{Symbol: symbol, Date: start.AddDate(0, 0, -i).Format(time.DateOnly), Source: "fixture", Open: 10, Close: 10, High: 11, Low: 9, Volume: 100})
		}
	}
	for i := 0; i < 4; i++ {
		r := plan.Baseline
		if i%2 == 1 {
			r = plan.Candidate
		}
		if i >= 2 {
			r.CommissionRate *= 2
			r.MinimumCommission *= 2
			r.StampDutyRate *= 2
			r.TransferFeeRate *= 2
			r.SlippageBPS *= 2
		}
		run := backtest.Result{Request: r, Metrics: backtest.Metrics{FinalEquity: r.InitialCash}, Equity: []backtest.EquityPoint{{Date: day.Date, Cash: r.InitialCash, Equity: r.InitialCash}}}
		if i%2 == 0 {
			run.PendingOrders = []backtest.ForwardPendingOrder{{Symbol: "sh600110", Side: "buy", Signal: backtest.SignalSnapshot{Date: day.Date, VolumeRatio: 1.2}}}
		} else {
			run.VolumeBlockedEntries = []backtest.EntryVolumeBlock{{Symbol: "sh600110", SignalDate: day.Date, VolumeRatio: 1.2, Minimum: 1.5}}
		}
		day.Runs = append(day.Runs, run)
	}
	day.Fingerprint, err = backtest.ForwardFingerprint(day)
	if err != nil {
		t.Fatal(err)
	}
	return plan, day, source, binary
}

func TestFirstDaySignalsAndNoSignalAreBothValid(t *testing.T) {
	plan, day, _, _ := fixture(t)
	if err := verifyFirstDay(plan, day); err != nil {
		t.Fatal(err)
	}
	for i := range day.Runs {
		day.Runs[i].PendingOrders = nil
		day.Runs[i].VolumeBlockedEntries = nil
	}
	if err := verifyFirstDay(plan, day); err != nil {
		t.Fatal("no-signal first day rejected:", err)
	}
	for i := range day.Runs {
		day.Runs[i].PendingOrders = []backtest.ForwardPendingOrder{{Symbol: "sh600110", Side: "buy", Signal: backtest.SignalSnapshot{Date: day.Date, VolumeRatio: 1.5}}}
	}
	if err := verifyFirstDay(plan, day); err != nil {
		t.Fatal("inclusive volume boundary rejected:", err)
	}
}

func TestFirstDayRejectsPrematureExecutionAndRuleDrift(t *testing.T) {
	cases := map[string]func(*backtest.VolumeForwardDay){
		"premature-trade": func(d *backtest.VolumeForwardDay) { d.Runs[0].Trades = []backtest.Trade{{}} },
		"fees":            func(d *backtest.VolumeForwardDay) { d.Runs[1].Metrics.TotalFees = 5 },
		"cash":            func(d *backtest.VolumeForwardDay) { d.Runs[0].Equity[0].Cash-- },
		"double-cost":     func(d *backtest.VolumeForwardDay) { d.Runs[2].Request.CommissionRate /= 2 },
		"future-signal":   func(d *backtest.VolumeForwardDay) { d.Runs[0].PendingOrders[0].Signal.Date = "2026-10-21" },
		"missing-stock":   func(d *backtest.VolumeForwardDay) { delete(d.Bars, "sh600110") },
		"early-capture":   func(d *backtest.VolumeForwardDay) { d.CapturedAt = d.CapturedAt.Add(-time.Hour) },
		"filter-bypass":   func(d *backtest.VolumeForwardDay) { d.Runs[1].PendingOrders = d.Runs[0].PendingOrders },
		"filter-record":   func(d *backtest.VolumeForwardDay) { d.Runs[1].VolumeBlockedEntries = nil },
		"promotion":       func(d *backtest.VolumeForwardDay) { d.PromotionAllowed = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			plan, day, _, _ := fixture(t)
			mutate(&day)
			if err := verifyFirstDay(plan, day); err == nil {
				t.Fatal("invalid first day accepted")
			}
		})
	}
}

func TestInspectWaitsChecksDeadlineAndAcceptsSealedDay(t *testing.T) {
	plan, day, source, program := fixture(t)
	root := t.TempDir()
	archive, binary := filepath.Join(root, "archive"), filepath.Join(root, "collector")
	store := storage.NewVolumeForwardStore(archive)
	if err := store.Initialize(plan, source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, program, 0o600); err != nil {
		t.Fatal(err)
	}
	before := plan.CreatedAt
	result, err := inspect(archive, binary, plan.Fingerprint, before)
	if err != nil || result.Status != "waiting" || result.SealedDays != 0 {
		t.Fatalf("pre-start inspection: %+v %v", result, err)
	}
	result, err = inspect(archive, binary, plan.Fingerprint, plan.Baseline.Start.Add(19*time.Hour))
	if err != nil || result.Status != "attention" {
		t.Fatalf("missed window not reported: %+v %v", result, err)
	}
	if err := store.Append(plan, day); err != nil {
		t.Fatal(err)
	}
	if _, err := inspect(archive, binary, plan.Fingerprint, before); err == nil {
		t.Fatal("future capture accepted")
	}
	result, err = inspect(archive, binary, plan.Fingerprint, day.CapturedAt.Add(time.Minute))
	if err != nil || result.Status != "passed" || result.FirstFingerprint != day.Fingerprint || result.SealedDays != 1 || result.PromotionAllowed {
		t.Fatalf("sealed first day rejected: %+v %v", result, err)
	}
	if _, err := inspect(archive, binary, "different-plan", day.CapturedAt); err == nil {
		t.Fatal("changed plan accepted")
	}
	if err := os.WriteFile(binary, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := inspect(archive, binary, plan.Fingerprint, day.CapturedAt); err == nil {
		t.Fatal("changed collector accepted")
	}
	_, latest, count, err := store.Load()
	if err != nil || count != 1 || latest.Fingerprint != day.Fingerprint {
		t.Fatalf("inspection modified the archive: %v", err)
	}
}

func TestHistoricalRehearsalFirstDay(t *testing.T) {
	archive := os.Getenv("ASTOCK_ACCEPTANCE_REHEARSAL_ARCHIVE")
	if archive == "" {
		t.Skip("set ASTOCK_ACCEPTANCE_REHEARSAL_ARCHIVE to verify existing offline evidence")
	}
	plan, _, count, err := storage.NewVolumeForwardStore(archive).Load()
	if err != nil || count == 0 {
		t.Fatalf("load rehearsal: count=%d: %v", count, err)
	}
	entries, err := os.ReadDir(filepath.Join(archive, "days"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(archive, "days", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var day backtest.VolumeForwardDay
	if err := json.Unmarshal(data, &day); err != nil {
		t.Fatal(err)
	}
	if err := verifyFirstDay(plan, day); err != nil {
		t.Fatal(err)
	}
	t.Logf("historical first-day structure verified: %s; this is not live acceptance", day.Date)
}
