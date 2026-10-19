package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

func forwardStoreFixture(t *testing.T) (*VolumeForwardStore, backtest.VolumeForwardPlan) {
	t.Helper()
	source := []byte(`{"completed":true}`)
	sum := sha256.Sum256(source)
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	request := backtest.Request{Strategy: "technical-breakout", Tickers: []string{"sh600110"}, Start: start, End: start, InitialCash: 1000000, Adjustment: backtest.AdjustmentNone, Technical: backtest.DefaultTechnicalParameters()}
	request.Technical.EntryMode = backtest.EntryModeReclaim
	plan := backtest.VolumeForwardPlan{Version: backtest.VolumeForwardVersion, CreatedAt: start.Add(-time.Hour), SourceID: "test", CandidateID: "P008", SourceSHA256: hex.EncodeToString(sum[:]), ExecutableSHA256: strings.Repeat("b", 64), Baseline: request, Candidate: request, Policy: backtest.DefaultCandidateObservationPolicy()}
	plan.Candidate.EntryVolumeFloor = 1.5
	var err error
	plan.Fingerprint, err = backtest.ForwardFingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	store := NewVolumeForwardStore(t.TempDir())
	if err := store.Initialize(plan, source); err != nil {
		t.Fatal(err)
	}
	return store, plan
}

func forwardStoreDay(t *testing.T, plan backtest.VolumeForwardPlan, previous *backtest.VolumeForwardDay) backtest.VolumeForwardDay {
	t.Helper()
	date := plan.Baseline.Start
	var equity []backtest.EquityPoint
	day := backtest.VolumeForwardDay{PlanFingerprint: plan.Fingerprint}
	if previous != nil {
		date = date.AddDate(0, 0, len(previous.Runs[0].Equity))
		equity = append(equity, previous.Runs[0].Equity...)
		day.PreviousHash = previous.Fingerprint
	}
	day.Date, day.CapturedAt = date.Format("2006-01-02"), date.Add(16*time.Hour)
	equity = append(equity, backtest.EquityPoint{Date: day.Date, Cash: 1000000, Equity: 1000000})
	for i := 0; i < 4; i++ {
		day.Runs = append(day.Runs, backtest.Result{Equity: equity})
	}
	var err error
	day.Fingerprint, err = backtest.ForwardFingerprint(day)
	if err != nil {
		t.Fatal(err)
	}
	return day
}

func TestVolumeForwardStoreAppendReloadAndNoOverwrite(t *testing.T) {
	store, plan := forwardStoreFixture(t)
	unlock, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if other, err := store.Acquire(); err == nil {
		other()
		t.Fatal("concurrent writer lock acquired")
	}
	first := forwardStoreDay(t, plan, nil)
	if err := store.Append(plan, first); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(plan, first); err == nil {
		t.Fatal("duplicate day overwritten")
	}
	if err := store.Initialize(plan, []byte(`{"completed":true}`)); err == nil {
		t.Fatal("frozen plan overwritten")
	}
	_, latest, count, err := NewVolumeForwardStore(store.root).Load()
	if err != nil || count != 1 || latest.Fingerprint != first.Fingerprint {
		t.Fatal("restart lost sealed evidence", err)
	}
	second := forwardStoreDay(t, plan, latest)
	if err := store.Append(plan, second); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(store.root, "days", second.Date+".json")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.Load(); err == nil {
		t.Fatal("deleted tail was not detected")
	}
}

func TestVolumeForwardStoreRejectsSourceAndDayCorruption(t *testing.T) {
	for _, name := range []string{"source-result.json", "config.json", "days/2026-10-20.json"} {
		t.Run(name, func(t *testing.T) {
			store, plan := forwardStoreFixture(t)
			if err := store.Append(plan, forwardStoreDay(t, plan, nil)); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(store.root, name), []byte(`{}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := store.Load(); err == nil {
				t.Fatal("corrupt archive accepted")
			}
		})
	}
}
