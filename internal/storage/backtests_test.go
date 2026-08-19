package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

func TestBacktestStorePersistsSummaryTradeLogAndEquity(t *testing.T) {
	store := NewBacktestStore(t.TempDir())
	result, err := store.Save(backtest.Result{
		RunID: "20240801T120000", GeneratedAt: time.Date(2024, 8, 1, 12, 0, 0, 0, time.UTC),
		Request: backtest.Request{Strategy: "test", Tickers: []string{"sh600519"}},
		Metrics: backtest.Metrics{TotalReturn: 4.2, Trades: 1},
		Trades:  []backtest.Trade{{ID: "T0001", Symbol: "sh600519", Entry: backtest.Fill{Date: "2024-01-02"}, Exit: backtest.Fill{Date: "2024-01-03"}}},
		Equity:  []backtest.EquityPoint{{Date: "2024-01-02", Equity: 100000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"summary.json", "strategy.json", "report.md", "trades.jsonl", "equity.csv"} {
		if _, err := os.Stat(filepath.Join(result.Directory, name)); err != nil {
			t.Fatalf("artifact %s unavailable: %v", name, err)
		}
	}
	loaded, err := store.Load(result.RunID)
	if err != nil || len(loaded.Trades) != 1 || loaded.Metrics.TotalReturn != 4.2 {
		t.Fatalf("stored run not recoverable: %#v %v", loaded, err)
	}
	items, err := store.List(10)
	if err != nil || len(items) != 1 || items[0].RunID != result.RunID {
		t.Fatalf("stored run missing from list: %#v %v", items, err)
	}
}

func TestOptimizationStorePersistsCandidatesAndAllSelectedSplits(t *testing.T) {
	root := t.TempDir()
	store := NewOptimizationStore(root)
	period := func(month int) backtest.Period {
		return backtest.Period{
			Start: time.Date(2024, time.Month(month), 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2024, time.Month(month+1), 1, 0, 0, 0, 0, time.UTC),
		}
	}
	parameters := backtest.DefaultTechnicalParameters()
	candidate := backtest.CandidateResult{
		ID: "C001", Parameters: parameters, Score: 3.14,
		Train:    backtest.Metrics{TotalReturn: 5, Trades: 3},
		Validate: backtest.Metrics{TotalReturn: 2, Trades: 2},
	}
	result := backtest.OptimizationResult{
		ID: "OPT-20240812T120000", GeneratedAt: time.Date(2024, 8, 12, 12, 0, 0, 0, time.UTC),
		Request: backtest.OptimizationRequest{
			BaseRequest: backtest.Request{Strategy: "technical-breakout", Tickers: []string{"sh600519"}},
			Train:       period(1), Validate: period(3), OutOfSample: period(5), MaxCandidates: 1,
		},
		Candidates: []backtest.CandidateResult{candidate}, Selected: &candidate,
		SelectedTrain: &backtest.Result{
			RunID: "train", Request: backtest.Request{Strategy: "technical-breakout"},
			Metrics: backtest.Metrics{TotalReturn: 5}, Trades: []backtest.Trade{{ID: "T0001"}},
		},
		SelectedValidation: &backtest.Result{
			RunID: "validation", Request: backtest.Request{Strategy: "technical-breakout"},
			Metrics: backtest.Metrics{TotalReturn: 2}, Trades: []backtest.Trade{{ID: "T0001"}},
		},
		OutOfSample: &backtest.Result{
			RunID: "oos", Request: backtest.Request{Strategy: "technical-breakout"},
			Metrics: backtest.Metrics{TotalReturn: -1}, Trades: []backtest.Trade{{ID: "T0001"}},
		},
		AIReview: "# 只读复盘\n\n没有收益保证。\n",
	}
	saved, err := store.Save(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"summary.json", "candidates.jsonl", "selected-strategy.json", "report.md", "ai-review.md",
		"train/trades.jsonl", "validation/equity.csv", "out-of-sample/summary.json",
	} {
		if _, err := os.Stat(filepath.Join(saved.Directory, name)); err != nil {
			t.Fatalf("optimization artifact %s unavailable: %v", name, err)
		}
	}
	loaded, err := store.Load(saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Selected == nil || loaded.Selected.ID != "C001" || loaded.SelectedTrain == nil ||
		loaded.SelectedValidation == nil || loaded.OutOfSample == nil || loaded.OutOfSample.Metrics.TotalReturn != -1 {
		t.Fatalf("optimization splits not recoverable: %#v", loaded)
	}
	items, err := store.List(10)
	if err != nil || len(items) != 1 || !items[0].OOSAvailable || !items[0].AIUsed {
		t.Fatalf("optimization list is incomplete: %#v %v", items, err)
	}
}
