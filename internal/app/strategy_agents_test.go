package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type structuredStrategyAgentMock struct {
	mu    sync.Mutex
	calls int
	fail  int
}

func (mock *structuredStrategyAgentMock) Synthesize(context.Context, string) (string, error) {
	return "# review", nil
}

func (mock *structuredStrategyAgentMock) SynthesizeJSON(_ context.Context, _ string, _ []byte, target any) error {
	mock.mu.Lock()
	mock.calls++
	call := mock.calls
	mock.mu.Unlock()
	if call == mock.fail {
		return errors.New("agent unavailable")
	}
	batch := strategyAgentWireBatch{SchemaVersion: 1, Candidates: []strategyAgentWireProposal{{
		Strategy: "technical-breakout", Hypothesis: "test candidate",
		Parameters: strategyAgentWireParameters{EntryMode: backtest.EntryModeReclaim, FastMA: 20 + call, SlowMA: 60, BreakoutDays: 20, VolumeRatioMin: 1, StopLoss: .08, TakeProfit: .2, MaxHoldingDays: 40},
	}}}
	data, _ := json.Marshal(batch)
	return json.Unmarshal(data, target)
}

func TestStrategyAgentCoordinatorIsolatesFailureAndKeepsDeterministicFallback(t *testing.T) {
	mock := &structuredStrategyAgentMock{fail: 2}
	app := &App{marketReportAI: mock}
	request := backtest.Request{Technical: backtest.DefaultTechnicalParameters()}
	runs, proposals := app.collectStrategyAgentProposals(context.Background(), request, "", true)
	if len(runs) != 3 || mock.calls != 3 || len(proposals) < 6 {
		t.Fatalf("unexpected multi-agent result: runs=%#v proposals=%#v calls=%d", runs, proposals, mock.calls)
	}
	failed := 0
	for _, run := range runs {
		if run.Status == "failed" {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("agent failure was not isolated: %#v", runs)
	}
	seen := make(map[string]bool)
	for _, proposal := range proposals {
		key := canonicalParameters(proposal.Parameters)
		if seen[key] {
			t.Fatalf("duplicate proposal was not removed: %#v", proposal)
		}
		seen[key] = true
		if proposal.Parameters.MaxPosition != request.Technical.MaxPosition {
			t.Fatal("agent changed coordinator-owned maximum position")
		}
	}
}

func TestStrategyAgentPromptForbidsMetricsAndAccountControls(t *testing.T) {
	prompt := strategyAgentPrompt(strategyAgentRoles[0], backtest.Request{Technical: backtest.DefaultTechnicalParameters()}, "上一轮留出回撤偏高")
	for _, expected := range []string{"不能写收益、评分", "不能修改股票池", "不能修改", "最大仓位", "只允许提出", "上一轮留出回撤偏高"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("strategy agent prompt missing %q:\n%s", expected, prompt)
		}
	}
}

func TestBuildWalkForwardPeriodsKeepsHoldoutUnseenAndOrdered(t *testing.T) {
	end := dateOnly(time.Date(2026, 8, 12, 0, 0, 0, 0, shanghaiLocation))
	folds, holdout := buildWalkForwardPeriods(end, defaultContinuousOptimizationOptions())
	if len(folds) != 4 || !holdout.End.Equal(end) {
		t.Fatalf("unexpected folds/holdout: %#v %#v", folds, holdout)
	}
	for index, fold := range folds {
		if !fold.Train.End.Before(fold.Validate.Start) || !fold.Validate.End.Before(holdout.Start) {
			t.Fatalf("fold %d leaks holdout: %#v", index, fold)
		}
		if index > 0 && !folds[index-1].Validate.End.Before(fold.Validate.End) {
			t.Fatalf("folds are not chronological: %#v", folds)
		}
	}
}

func TestLatestContinuousBaselineRejectsSameCutoffAndAdvancesOnNewDay(t *testing.T) {
	root := t.TempDir()
	app := &App{paths: storage.Paths{BacktestsDir: root, ContinuousOptimizationsDir: root + "/continuous"}}
	parameters := backtest.DefaultTechnicalParameters()
	parameters.FastMA = 30
	proposal := backtest.StrategyProposal{ID: "P001", Parameters: parameters}
	candidate := backtest.ContinuousCandidateResult{Proposal: proposal}
	_, err := app.continuousOptimizationStore().Save(backtest.ContinuousOptimizationResult{
		ID: "AUTO-old", Cycle: 2, DataCutoff: "2026-08-12", GeneratedAt: time.Now(), Stage: backtest.ContinuousStageShadow,
		Request: backtest.ContinuousOptimizationRequest{
			BaseRequest: backtest.Request{Tickers: []string{"sh600519"}},
			Holdout:     backtest.Period{Start: time.Date(2026, 5, 13, 0, 0, 0, 0, shanghaiLocation), End: time.Date(2026, 8, 12, 0, 0, 0, 0, shanghaiLocation)},
		},
		Selected: &candidate,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, err := app.latestContinuousBaseline([]string{"sh600519"}, time.Date(2026, 11, 11, 0, 0, 0, 0, shanghaiLocation), 3); err == nil {
		t.Fatal("overlapping holdout window must not be tuned repeatedly")
	}
	baseline, parent, cycle, lessons, previousEnd, err := app.latestContinuousBaseline([]string{"sh600519"}, time.Date(2026, 11, 12, 0, 0, 0, 0, shanghaiLocation), 3)
	if err != nil || baseline.FastMA != 30 || parent != "AUTO-old" || cycle != 3 || lessons == "" || previousEnd != "2026-08-12" {
		t.Fatalf("new non-overlapping cycle did not inherit audited baseline: %#v %s %d %q %q %v", baseline, parent, cycle, lessons, previousEnd, err)
	}
}

func TestLatestContinuousBaselineUsesDataCutoffForLegacyArchive(t *testing.T) {
	root := t.TempDir()
	app := &App{paths: storage.Paths{BacktestsDir: root, ContinuousOptimizationsDir: root + "/continuous"}}
	_, err := app.continuousOptimizationStore().Save(backtest.ContinuousOptimizationResult{
		ID: "AUTO-legacy", Cycle: 1, DataCutoff: "2026-08-12", GeneratedAt: time.Now(),
		Request: backtest.ContinuousOptimizationRequest{BaseRequest: backtest.Request{Tickers: []string{"sh600519"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, _, err := app.latestContinuousBaseline([]string{"sh600519"}, time.Date(2026, 11, 11, 0, 0, 0, 0, shanghaiLocation), 3); err == nil {
		t.Fatal("legacy archive cutoff must still protect the inspected holdout")
	}
}

func TestLatestContinuousBaselineScansBeyondTwentyArchives(t *testing.T) {
	root := t.TempDir()
	app := &App{paths: storage.Paths{BacktestsDir: root, ContinuousOptimizationsDir: root + "/continuous"}}
	parameters := backtest.DefaultTechnicalParameters()
	parameters.FastMA = 30
	candidate := backtest.ContinuousCandidateResult{Proposal: backtest.StrategyProposal{ID: "P001", Parameters: parameters}}
	_, err := app.continuousOptimizationStore().Save(backtest.ContinuousOptimizationResult{
		ID: "AUTO-target", Cycle: 4, DataCutoff: "2026-08-12", GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Stage: backtest.ContinuousStageShadow,
		Request: backtest.ContinuousOptimizationRequest{BaseRequest: backtest.Request{Tickers: []string{"sh600519"}}, Holdout: backtest.Period{
			Start: time.Date(2026, 5, 13, 0, 0, 0, 0, shanghaiLocation), End: time.Date(2026, 8, 12, 0, 0, 0, 0, shanghaiLocation),
		}}, Selected: &candidate,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 25; index++ {
		_, err = app.continuousOptimizationStore().Save(backtest.ContinuousOptimizationResult{
			ID: fmt.Sprintf("AUTO-other-%02d", index), GeneratedAt: time.Date(2026, 2, 1, 0, index, 0, 0, time.UTC),
			Manifest: backtest.ExperimentManifest{SchemaVersion: 1},
			Request:  backtest.ContinuousOptimizationRequest{BaseRequest: backtest.Request{Tickers: []string{fmt.Sprintf("sh60%04d", index)}}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	baseline, parent, cycle, _, _, err := app.latestContinuousBaseline([]string{"sh600519"}, time.Date(2026, 11, 12, 0, 0, 0, 0, shanghaiLocation), 3)
	if err != nil || baseline.FastMA != 30 || parent != "AUTO-target" || cycle != 5 {
		t.Fatalf("full lineage scan missed target archive: %#v %s %d %v", baseline, parent, cycle, err)
	}
}

type capturingContinuousAI struct {
	prompt string
}

func (ai *capturingContinuousAI) Synthesize(_ context.Context, prompt string) (string, error) {
	ai.prompt = prompt
	return "# review", nil
}

func TestContinuousSupervisorReceivesImmutableQualityAndHoldoutFacts(t *testing.T) {
	ai := &capturingContinuousAI{}
	app := &App{marketReportAI: ai}
	result := backtest.ContinuousOptimizationResult{
		ID: "AUTO-test", Stage: backtest.ContinuousStageResearch,
		Manifest: backtest.ExperimentManifest{CandidateSetHash: "candidate-hash", ConfigurationHash: "config-hash"},
		Quality:  backtest.DataQualitySummary{Grade: "B", Passed: true}, PriorLessons: "previous lesson",
		Holdout:     &backtest.Result{Metrics: backtest.Metrics{TotalReturn: -2}, DataSources: map[string]string{"sh600519": "eastmoney"}},
		Stress:      backtest.StressResult{DoubleCost: &backtest.Result{Metrics: backtest.Metrics{TotalReturn: -3}}, BestTradeProfitShare: .72},
		GateReasons: []string{"最终留出收益不为正"},
	}
	if _, err := app.reviewContinuousOptimization(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"candidate-hash", "config-hash", "previous lesson", "quality", "holdout", "stress", "best_trade_profit_share", "0.72", "最终留出收益不为正", "不可变事实"} {
		if !strings.Contains(ai.prompt, expected) {
			t.Fatalf("supervisor prompt missing %q:\n%s", expected, ai.prompt)
		}
	}
}
