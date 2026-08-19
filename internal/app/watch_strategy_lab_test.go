package app

import (
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

func TestStrategyLabDefaultPeriodsAreOrderedAndNonOverlapping(t *testing.T) {
	end := time.Date(2026, 8, 11, 0, 0, 0, 0, shanghaiLocation)
	state := newWatchStrategyLab()
	train, validate, oos := strategyLabOptimizationPeriods(end, strategyLabSplits[state.splitIndex])
	if !train.End.Before(validate.Start) || !validate.End.Before(oos.Start) || !oos.End.Equal(end) {
		t.Fatalf("strategy lab periods overlap or are unordered: %#v %#v %#v", train, validate, oos)
	}
	if train.Start.Format("2006-01-02") != "2018-08-12" || oos.Start.Format("2006-01-02") != "2024-08-12" {
		t.Fatalf("unexpected default 4/2/2 split: %#v %#v %#v", train, validate, oos)
	}
}

func TestStrategyLabSettingsAndTargetsUseFrozenScope(t *testing.T) {
	state := newWatchStrategyLab()
	state.updateContext("sh600519", "贵州茅台", "全部", []string{"sh600519", "sz000001"}, map[string]string{
		"sh600519": "贵州茅台", "sz000001": "平安银行",
	})
	symbols, names, err := state.targets()
	if err != nil || len(symbols) != 1 || names["sh600519"] != "贵州茅台" {
		t.Fatalf("unexpected current-stock targets: %#v %#v %v", symbols, names, err)
	}
	state.view = strategyLabSettings
	state.selected = 0
	state.cycleSetting()
	symbols, names, err = state.targets()
	if err != nil || len(symbols) != 2 || names["sz000001"] != "平安银行" {
		t.Fatalf("unexpected current-list targets: %#v %#v %v", symbols, names, err)
	}
	state.selected = 5
	state.cycleSetting()
	if state.useAI {
		t.Fatal("AI setting should toggle off")
	}
}

func TestStrategyLabCompletionOpensResultAndPreservesLoss(t *testing.T) {
	state := newWatchStrategyLab()
	state.begin("optimization", "训练/验证候选 1/3")
	if state.viewing || !state.running || !strings.Contains(state.status(false), "行情继续刷新") {
		t.Fatalf("background state is incomplete: %#v", state)
	}
	result := backtest.OptimizationResult{
		ID: "OPT-test", OutOfSample: &backtest.Result{Metrics: backtest.Metrics{TotalReturn: -4.06}},
	}
	state.completeOptimization(result)
	if !state.unread || !strings.Contains(state.status(false), "按 t 查看") {
		t.Fatalf("completion notice is missing: %#v", state)
	}
	state.open()
	if state.view != strategyLabOptimizationDetail || state.unread || state.optimization.OutOfSample.Metrics.TotalReturn != -4.06 {
		t.Fatalf("result did not open intact: %#v", state)
	}
}

func TestStrategyLabVisibleItemsKeepsSelectionOnScreen(t *testing.T) {
	items := make([]string, 30)
	for index := range items {
		items[index] = strings.Repeat("x", index+1)
	}
	visible, selected := strategyLabVisibleItems(items, 27, 15)
	if len(visible) != 15 || selected < 0 || selected >= len(visible) || visible[selected] != items[27] {
		t.Fatalf("selection was not kept visible: len=%d selected=%d", len(visible), selected)
	}
}

func TestStrategyLabBacktestPeriodUsesFullRequestedYears(t *testing.T) {
	end := time.Date(2026, 8, 11, 0, 0, 0, 0, shanghaiLocation)
	period := strategyLabBacktestPeriod(end, 3)
	if period.Start.Format("2006-01-02") != "2023-08-12" || !period.End.Equal(end) {
		t.Fatalf("unexpected backtest period: %#v", period)
	}
}

func TestStrategyLabShortcutDoesNotStealTextInput(t *testing.T) {
	event := terminalEvent{Text: "t"}
	if !isStrategyLabShortcut(event, false, false) {
		t.Fatal("t should open the strategy lab outside input mode")
	}
	if isStrategyLabShortcut(event, true, false) {
		t.Fatal("t must remain ordinary text while a command input is active")
	}
	if isStrategyLabShortcut(event, false, true) {
		t.Fatal("t should not recursively reopen the strategy lab")
	}
}

func TestStrategyLabContinuousDetailShowsResearchEvidence(t *testing.T) {
	state := newWatchStrategyLab()
	state.view = strategyLabContinuousDetail
	state.continuousRun = &backtest.ContinuousOptimizationResult{
		ID: "AUTO-test", Cycle: 2, DataCutoff: "2026-08-12", Stage: backtest.ContinuousStageResearch,
		Manifest:     backtest.ExperimentManifest{CandidateSetHash: "1234567890abcdef", ConfigurationHash: "fedcba0987654321"},
		Quality:      backtest.DataQualitySummary{Grade: "B", Passed: true, Checks: []backtest.DataQualityCheck{{Name: "点时股票池", Detail: "当前为静态池", Warning: true}}},
		Request:      backtest.ContinuousOptimizationRequest{Folds: make([]backtest.WalkForwardFold, 4)},
		PriorLessons: "上一轮回撤偏高",
	}
	frame := state.frame(time.Now(), 120, false, false)
	for _, expected := range []string{"数据质量 B", "1234567890ab", "fedcba098765", "点时股票池", "历史反思上下文", "上一轮回撤偏高"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("continuous detail missing %q:\n%s", expected, frame)
		}
	}
}
