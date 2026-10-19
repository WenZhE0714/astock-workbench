package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type rehearsalBars map[string][]domain.DailyBar

func (bars rehearsalBars) FetchDailyBarsRange(_ context.Context, symbol string, start, end time.Time, adjustment backtest.PriceAdjustment) ([]domain.DailyBar, error) {
	if adjustment != backtest.AdjustmentNone {
		return nil, fmt.Errorf("rehearsal requires unadjusted prices")
	}
	result := []domain.DailyBar{}
	for _, bar := range bars[symbol] {
		if bar.Date >= start.Format("2006-01-02") && bar.Date <= end.Format("2006-01-02") {
			result = append(result, bar)
		}
	}
	return result, nil
}

func cloneRehearsalBars(bars rehearsalBars) rehearsalBars {
	copy := rehearsalBars{}
	for symbol, items := range bars {
		copy[symbol] = append([]domain.DailyBar(nil), items...)
	}
	return copy
}

// This opt-in integration test uses already-seen frozen data and a simulated
// clock. Its separate archive must never be counted as real-time observation.
func TestVolumeForwardFrozenRehearsal(t *testing.T) {
	sourceDir := os.Getenv("ASTOCK_FORWARD_REHEARSAL_SOURCE")
	if sourceDir == "" {
		t.Skip("set ASTOCK_FORWARD_REHEARSAL_SOURCE to a frozen volume study")
	}
	output := os.Getenv("ASTOCK_FORWARD_REHEARSAL_OUTPUT")
	if output == "" {
		output = filepath.Join(t.TempDir(), "rehearsal")
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatalf("rehearsal output must be a new directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(output, "NOT_FORWARD_EVIDENCE.md"), []byte("# Historical execution rehearsal\n\nThis directory uses already-seen data and a simulated clock. It is not prospective evidence. No day or trade here may be added to a live forward archive.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceBytes, err := os.ReadFile(filepath.Join(sourceDir, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var source backtest.VolumeStudyResult
	if err := json.Unmarshal(sourceBytes, &source); err != nil {
		t.Fatal(err)
	}
	planBytes, err := os.ReadFile(filepath.Join(sourceDir, "source-plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sourcePlan positionStudyPlan
	if err := json.Unmarshal(planBytes, &sourcePlan); err != nil {
		t.Fatal(err)
	}
	if !source.Completed || len(source.Variants) != 2 || sourcePlan.CreatedAt.IsZero() || len(sourcePlan.Files) == 0 {
		t.Fatal("incomplete frozen source")
	}
	if err := copyVerifiedStudyInputs(filepath.Join(sourceDir, "cache"), filepath.Join(output, "cache"), sourcePlan.Files); err != nil {
		t.Fatal(err)
	}
	period := source.Variants[0].Diagnostic.Normal.Request
	sum := sha256.Sum256(sourceBytes)
	plan, err := backtest.NewVolumeForwardPlan(source, hex.EncodeToString(sum[:]), strings.Repeat("0", 64), period.Start.AddDate(0, 0, -1).Add(16*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	provider := newOfflineStudyProvider(filepath.Join(output, "cache"), sourcePlan.CreatedAt)
	bars := rehearsalBars{}
	warmStart := plan.Baseline.Start.AddDate(0, 0, -180)
	for _, symbol := range append([]string{plan.Baseline.Benchmark}, plan.Baseline.Tickers...) {
		bars[symbol], err = provider.FetchDailyBarsRange(t.Context(), symbol, warmStart, period.End, backtest.AdjustmentNone)
		if err != nil {
			t.Fatalf("%s: %v", symbol, err)
		}
	}
	dates := []time.Time{}
	for _, bar := range bars[plan.Baseline.Benchmark] {
		if bar.Date >= plan.Baseline.Start.Format("2006-01-02") {
			date, err := time.ParseInLocation("2006-01-02", bar.Date, shanghaiLocation)
			if err != nil {
				t.Fatal(err)
			}
			dates = append(dates, date.Add(16*time.Hour))
		}
	}
	if len(dates) < 3 {
		t.Fatal("rehearsal needs multiple sessions")
	}
	archive := filepath.Join(output, "simulated-archive")
	store := storage.NewVolumeForwardStore(archive)
	if err := store.Initialize(plan, sourceBytes); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	var latest *backtest.VolumeForwardDay
	var stoppedAt string
	var missingSymbols []string
	restartReads := 0
	for i, now := range dates {
		// Reopen the store and deserialize the previous day on every step, as
		// the scheduled CLI does after a process exit.
		store = storage.NewVolumeForwardStore(archive)
		unlock, err := store.Acquire()
		if err != nil {
			t.Fatal(err)
		}
		storedPlan, previous, count, err := store.Load()
		restartReads++
		if err != nil || count != i {
			unlock()
			t.Fatalf("restart before %s: count=%d: %v", now.Format("2006-01-02"), count, err)
		}
		date := now.Format("2006-01-02")
		missing := []string{}
		for _, symbol := range plan.Baseline.Tickers {
			found := false
			for _, bar := range bars[symbol] {
				found = found || bar.Date == date
			}
			if !found {
				missing = append(missing, symbol)
			}
		}
		day, err := backtest.BuildVolumeForwardDay(t.Context(), storedPlan, previous, bars, now)
		if err != nil {
			if previous != nil && len(missing) > 0 && strings.Contains(err.Error(), missing[0]) && strings.Contains(err.Error(), "完整日K") {
				if appendErr := store.Append(storedPlan, day); appendErr == nil {
					t.Fatal("incomplete historical day was persisted")
				}
				_, unchanged, unchangedCount, loadErr := store.Load()
				if loadErr != nil || unchangedCount != i || unchanged == nil || unchanged.Fingerprint != previous.Fingerprint {
					t.Fatalf("historical gap changed archive: %v", loadErr)
				}
				unlock()
				stoppedAt, missingSymbols = date, missing
				t.Logf("correctly halted at frozen-data gap on %s: %v; %d seals retained", date, missing, count)
				break
			}
			unlock()
			t.Fatalf("rehearsal stopped on %s: %v", now.Format("2006-01-02"), err)
		}
		if len(missing) > 0 {
			unlock()
			t.Fatalf("missing source bars accepted on %s: %v", date, missing)
		}
		if err := store.Append(storedPlan, day); err != nil {
			unlock()
			t.Fatal(err)
		}
		unlock()
		latest = &day
		if due, _ := backtest.ForwardCollectionDue(storedPlan, latest, now); due {
			t.Fatal("sealed date scheduled again")
		}
		if i == 0 {
			rehearsalFailureChecks(t, store, plan, day, bars, dates)
		}
		if (i+1)%10 == 0 || i == len(dates)-1 {
			t.Logf("reopened and sealed %d/%d sessions through %s", i+1, len(dates), day.Date)
		}
	}
	_, restored, count, err := storage.NewVolumeForwardStore(archive).Load()
	restartReads++
	if err != nil || count < 3 || restored == nil || latest == nil || restored.Fingerprint != latest.Fingerprint || stoppedAt == "" && count != len(dates) {
		t.Fatalf("final archive verification: %v", err)
	}
	batchRuns := []backtest.Result{}
	for i, run := range restored.Runs {
		batch, err := backtest.NewDailyEngine(bars).Run(t.Context(), run.Request)
		if err != nil {
			t.Fatal(err)
		}
		// Empty optional lists are omitted in JSON and reload as nil.
		tradesMatch := slices.EqualFunc(run.Trades, batch.Trades, func(a, b backtest.Trade) bool { return reflect.DeepEqual(a, b) })
		positionsMatch := slices.EqualFunc(run.OpenPositions, batch.OpenPositions, func(a, b backtest.OpenPosition) bool { return reflect.DeepEqual(a, b) })
		if !tradesMatch || !positionsMatch || !slices.Equal(run.VolumeBlockedEntries, batch.VolumeBlockedEntries) || len(run.Equity) != len(batch.Equity) {
			t.Fatalf("account %d daily path differs from batch path", i)
		}
		for j, point := range run.Equity {
			other := batch.Equity[j]
			if point.Date != other.Date || math.Abs(point.Cash-other.Cash) > .000001 || math.Abs(point.Positions-other.Positions) > .000001 || math.Abs(point.Equity-other.Equity) > .000001 {
				t.Fatalf("account %d equity mismatch at %s", i, point.Date)
			}
		}
		if math.Abs(run.Metrics.FinalEquity-batch.Metrics.FinalEquity) > .000001 || math.Abs(run.Metrics.TotalFees-batch.Metrics.TotalFees) > .000001 {
			t.Fatalf("account %d final equity/fees mismatch", i)
		}
		batchRuns = append(batchRuns, batch)
	}
	result := map[string]any{
		"classification": "historical-execution-rehearsal", "real_time_evidence": false,
		"promotion_allowed": false, "completed": count == len(dates), "verification_passed": true, "source_id": source.SourceID,
		"source_sha256": plan.SourceSHA256, "frozen_files": len(sourcePlan.Files),
		"period_start": dates[0].Format("2006-01-02"), "period_end": restored.Date,
		"requested_period_end": dates[len(dates)-1].Format("2006-01-02"), "requested_sessions": len(dates),
		"stopped_at": stoppedAt, "missing_symbols": missingSymbols,
		"stock_count": len(plan.Baseline.Tickers), "sealed_simulated_days": count,
		"restart_reads": restartReads, "account_paths": len(restored.Runs),
		"failure_checks": []string{"missed-session", "missing-stock-bar", "revised-historical-volume", "future-bar-isolation", "duplicate-day"},
		"batch_parity":   true, "elapsed_seconds": time.Since(started).Seconds(),
		"final_day": restored, "batch_runs": batchRuns,
	}
	if err := writeStudyJSON(filepath.Join(output, "rehearsal-result.json"), result); err != nil {
		t.Fatal(err)
	}
	t.Logf("historical rehearsal evidence: %s", output)
}

func rehearsalFailureChecks(t *testing.T, store *storage.VolumeForwardStore, plan backtest.VolumeForwardPlan, first backtest.VolumeForwardDay, bars rehearsalBars, dates []time.Time) {
	t.Helper()
	unlock, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	assertUnchanged := func() {
		t.Helper()
		_, latest, count, err := store.Load()
		if err != nil || count != 1 || latest == nil || latest.Fingerprint != first.Fingerprint {
			t.Fatalf("rejected update changed archive: %v", err)
		}
	}
	assertRejected := func(name string, input rehearsalBars, now time.Time) {
		t.Helper()
		day, err := backtest.BuildVolumeForwardDay(t.Context(), plan, &first, input, now)
		if err == nil {
			t.Fatalf("%s accepted", name)
		}
		if err := store.Append(plan, day); err == nil {
			t.Fatalf("%s persisted", name)
		}
		assertUnchanged()
	}
	assertRejected("missing observation day", bars, dates[2])
	changed := cloneRehearsalBars(bars)
	symbol := plan.Baseline.Tickers[0]
	for i, bar := range changed[symbol] {
		if bar.Date == dates[1].Format("2006-01-02") {
			changed[symbol] = append(changed[symbol][:i], changed[symbol][i+1:]...)
			break
		}
	}
	assertRejected("missing stock session", changed, dates[1])
	changed = cloneRehearsalBars(bars)
	for i := range changed[symbol] {
		if changed[symbol][i].Date == first.Date {
			changed[symbol][i].Volume++
		}
	}
	assertRejected("historical volume rewrite", changed, dates[1])
	if err := store.Append(plan, first); err == nil {
		t.Fatal("duplicate day persisted")
	}
	assertUnchanged()
	changed = cloneRehearsalBars(bars)
	for i := range changed[symbol] {
		if changed[symbol][i].Date > first.Date {
			changed[symbol][i].Volume *= 100
		}
	}
	again, err := backtest.BuildVolumeForwardDay(t.Context(), plan, nil, changed, dates[0])
	if err != nil || again.Fingerprint != first.Fingerprint {
		t.Fatalf("future bars changed first seal: %v", err)
	}
}
