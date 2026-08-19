package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type failingOptimizationAI struct{}

func (failingOptimizationAI) Synthesize(context.Context, string) (string, error) {
	return "", errors.New("test AI unavailable")
}

func backtestCommandFixture(t *testing.T) (*App, string) {
	t.Helper()
	root := t.TempDir()
	store := storage.NewBacktestStore(root)
	result, err := store.Save(backtest.Result{
		RunID: "20240812T120000", GeneratedAt: time.Date(2024, 8, 12, 12, 0, 0, 0, time.Local),
		Request: backtest.Request{
			Strategy: "technical-breakout", StrategyVersion: "v1", Tickers: []string{"sh600519"},
			Start: time.Date(2023, 1, 1, 0, 0, 0, 0, time.Local), End: time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local),
			Adjustment: backtest.AdjustmentNone, Technical: backtest.DefaultTechnicalParameters(),
		},
		Metrics: backtest.Metrics{TotalReturn: 8.2, MaxDrawdown: -4.1, Trades: 1, WinRate: 100, FinalEquity: 108200},
		Trades: []backtest.Trade{{
			ID: "T0001", Symbol: "sh600519", Name: "贵州茅台", Strategy: "technical-breakout",
			EntrySignal: backtest.SignalSnapshot{Date: "2023-03-01", Close: 1800, FastMA: 1750, SlowMA: 1700, PriorHigh: 1790, VolumeRatio: 1.5, Reasons: []string{"突破前高"}},
			Entry:       backtest.Fill{Date: "2023-03-02", RawPrice: 1810, Price: 1811, Quantity: 100, Amount: 181100, TotalFee: 55},
			ExitSignal:  backtest.SignalSnapshot{Date: "2023-03-10", Reasons: []string{"跌破MA20"}},
			Exit:        backtest.Fill{Date: "2023-03-13", RawPrice: 1850, Price: 1849, Amount: 184900, TotalFee: 150},
			NetProfit:   3595, ReturnPercent: 1.98, HoldingDays: 7, MaxFavorable: 4.2, MaxAdverse: -1.1, ExitReason: "跌破MA20",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	output := &bytes.Buffer{}
	app := &App{out: output, errOut: &bytes.Buffer{}, paths: storage.Paths{BacktestsDir: root}}
	return app, result.RunID
}

func TestBacktestQueryCommandsExposeTradesAndEvidence(t *testing.T) {
	app, runID := backtestCommandFixture(t)
	if err := app.runBacktestList(nil); err != nil {
		t.Fatal(err)
	}
	if err := app.runBacktestShow([]string{runID}); err != nil {
		t.Fatal(err)
	}
	if err := app.runBacktestTrades([]string{runID}); err != nil {
		t.Fatal(err)
	}
	if err := app.runBacktestTrade([]string{runID, "T0001"}); err != nil {
		t.Fatal(err)
	}
	frame := app.out.(*bytes.Buffer).String()
	for _, expected := range []string{runID, "收益率 +8.20%", "T0001", "贵州茅台", "买入信号", "突破前高", "实际成交", "最大浮亏"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("query output missing %q:\n%s", expected, frame)
		}
	}
}

func TestParseBacktestDateAndTickersRejectInvalidInput(t *testing.T) {
	if _, err := parseBacktestDate("2024/01/01", time.Now()); err == nil {
		t.Fatal("invalid date format should be rejected")
	}
	if _, err := parseBacktestTickers(t.Context(), nil, nil); err == nil {
		t.Fatal("empty stock input should be rejected before resolver use")
	}
}

func TestOptimizationQueryCommandsAndAIFailureArchive(t *testing.T) {
	root := t.TempDir()
	output := &bytes.Buffer{}
	app := &App{
		out: output, errOut: &bytes.Buffer{},
		paths:          storage.Paths{BacktestsDir: filepath.Dir(root), OptimizationsDir: root},
		marketReportAI: failingOptimizationAI{},
	}
	parameters := backtest.DefaultTechnicalParameters()
	candidate := backtest.CandidateResult{
		ID: "C001", Parameters: parameters, Score: 2.5,
		Validate: backtest.Metrics{TotalReturn: 3, MaxDrawdown: -2, Trades: 4},
	}
	result := backtest.OptimizationResult{
		ID: "OPT-20240812T120000", GeneratedAt: time.Date(2024, 8, 12, 12, 0, 0, 0, time.UTC),
		Request: backtest.OptimizationRequest{
			BaseRequest: backtest.Request{Tickers: []string{"sh600519"}},
			Train:       backtest.Period{Start: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2020, 12, 31, 0, 0, 0, 0, time.UTC)},
			Validate:    backtest.Period{Start: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2021, 12, 31, 0, 0, 0, 0, time.UTC)},
			OutOfSample: backtest.Period{Start: time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2022, 12, 31, 0, 0, 0, 0, time.UTC)},
		},
		Candidates: []backtest.CandidateResult{candidate}, Selected: &candidate,
		OutOfSample: &backtest.Result{Metrics: backtest.Metrics{TotalReturn: -2, MaxDrawdown: -5, Trades: 2}},
		AIError:     "test AI unavailable",
	}
	saved, err := app.optimizationStore().Save(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.runBacktestOptimizeList(nil); err != nil {
		t.Fatal(err)
	}
	if err := app.runBacktestOptimizeShow([]string{saved.ID}); err != nil {
		t.Fatal(err)
	}
	frame := output.String()
	for _, expected := range []string{saved.ID, "C001", "样本外收益 -2.00%", "AI复盘失败", "report.md"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("optimization output missing %q:\n%s", expected, frame)
		}
	}
	if _, err := os.Stat(filepath.Join(saved.Directory, "summary.json")); err != nil {
		t.Fatalf("deterministic archive lost after AI failure: %v", err)
	}
}

func TestOptimizationReviewPromptLocksFactsAndForbidsOOSReselection(t *testing.T) {
	prompt, err := optimizationReviewPrompt(backtest.OptimizationResult{
		ID: "OPT-test", Request: backtest.OptimizationRequest{},
		Candidates: []backtest.CandidateResult{{ID: "C001"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"不可变实验事实", "样本外是在参数锁定后一次性执行", "不得用样本外结果重新挑选", "不宣称稳定收益"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("optimization prompt missing %q:\n%s", expected, prompt)
		}
	}
}

func TestOptimizationAIFailurePreservesDeterministicFacts(t *testing.T) {
	app := &App{errOut: &bytes.Buffer{}, marketReportAI: failingOptimizationAI{}}
	candidate := backtest.CandidateResult{ID: "C001", Score: 2.5}
	result := backtest.OptimizationResult{
		ID: "OPT-test", Candidates: []backtest.CandidateResult{candidate}, Selected: &candidate,
		OutOfSample: &backtest.Result{Metrics: backtest.Metrics{TotalReturn: -4.06}},
	}
	reviewed := app.reviewOptimization(context.Background(), result)
	if reviewed.AIError == "" || reviewed.Selected == nil || reviewed.Selected.ID != "C001" ||
		reviewed.OutOfSample == nil || reviewed.OutOfSample.Metrics.TotalReturn != -4.06 {
		t.Fatalf("AI failure changed or discarded deterministic facts: %#v", reviewed)
	}
}
