package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

func TestContinuousOptimizationStoreIsIndependentAndRecoversStressArtifacts(t *testing.T) {
	root := t.TempDir()
	store := NewContinuousOptimizationStore(root)
	parameters := backtest.DefaultTechnicalParameters()
	proposal := backtest.StrategyProposal{ID: "P001", Agent: "test", Thesis: "stable", Parameters: parameters}
	candidate := backtest.ContinuousCandidateResult{Proposal: proposal, Score: 2, PositiveFoldRatio: 1, ValidationTrades: 30}
	result := backtest.ContinuousOptimizationResult{
		ID: "AUTO-20260813T120000", Cycle: 1, DataCutoff: "2026-08-12", GeneratedAt: time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC),
		Request:    backtest.ContinuousOptimizationRequest{BaseRequest: backtest.Request{Tickers: []string{"sh600519"}}, Proposals: []backtest.StrategyProposal{proposal}},
		Candidates: []backtest.ContinuousCandidateResult{candidate}, Selected: &candidate, Stage: backtest.ContinuousStageShadow,
		Holdout: &backtest.Result{RunID: "holdout", Request: backtest.Request{Strategy: "technical-breakout"}, Metrics: backtest.Metrics{TotalReturn: 3}},
		Stress:  backtest.StressResult{DoubleCost: &backtest.Result{RunID: "stress", Request: backtest.Request{Strategy: "technical-breakout"}, Metrics: backtest.Metrics{TotalReturn: 1}}, BestTradeProfitShare: .4},
	}
	saved, err := store.Save(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"summary.json", "manifest.json", "report.md", "agents.jsonl", "candidates.jsonl", "holdout/summary.json", "stress-double-cost/summary.json"} {
		if _, err := os.Stat(filepath.Join(saved.Directory, name)); err != nil {
			t.Fatalf("continuous artifact missing %s: %v", name, err)
		}
	}
	loaded, err := store.Load(saved.ID)
	if err != nil || loaded.Holdout == nil || loaded.Stress.DoubleCost == nil || loaded.Stress.BestTradeProfitShare != .4 {
		t.Fatalf("continuous archive not recoverable: %#v %v", loaded, err)
	}
	items, err := store.List(10)
	if err != nil || len(items) != 1 || items[0].Stage != backtest.ContinuousStageShadow {
		t.Fatalf("continuous index incomplete: %#v %v", items, err)
	}
}

func TestContinuousOptimizationStorePersistsRejectedCandidate(t *testing.T) {
	store := NewContinuousOptimizationStore(t.TempDir())
	proposal := backtest.StrategyProposal{ID: "P001", Parameters: backtest.DefaultTechnicalParameters()}
	result := backtest.ContinuousOptimizationResult{
		ID: "AUTO-rejected", GeneratedAt: time.Now(), Stage: backtest.ContinuousStageResearch,
		Candidates: []backtest.ContinuousCandidateResult{{
			Proposal: proposal, Score: -1e12, Rejected: true, Reasons: []string{"行情不可用"},
		}},
	}
	saved, err := store.Save(result)
	if err != nil {
		t.Fatalf("rejected candidate should remain serializable: %v", err)
	}
	loaded, err := store.Load(saved.ID)
	if err != nil || len(loaded.Candidates) != 1 || !loaded.Candidates[0].Rejected {
		t.Fatalf("rejected candidate archive incomplete: %#v %v", loaded, err)
	}
}
