package backtest

import (
	"testing"
	"time"
)

func lifecycleResult() ContinuousOptimizationResult {
	parameters := DefaultTechnicalParameters()
	proposal := StrategyProposal{ID: "P001", Parameters: parameters}
	selected := ContinuousCandidateResult{Proposal: proposal}
	return ContinuousOptimizationResult{
		ID: "AUTO-test", Stage: ContinuousStageShadow,
		Manifest: ExperimentManifest{CandidateSetHash: "candidate", ConfigurationHash: "config"},
		Request:  ContinuousOptimizationRequest{BaseRequest: Request{Tickers: []string{"sh600519"}}},
		Selected: &selected,
	}
}

func observationResult(tradingDays, trades int, totalReturn, excess, drawdown float64) Result {
	equity := make([]EquityPoint, tradingDays)
	for index := range equity {
		equity[index] = EquityPoint{Date: time.Date(2026, 1, 1+index, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), Equity: 1_000_000}
	}
	return Result{
		Request: Request{Tickers: []string{"sh600519"}},
		Metrics: Metrics{Trades: trades, TotalReturn: totalReturn, ExcessReturn: excess, BenchmarkAvailable: true, MaxDrawdown: drawdown, FinalEquity: 1_000_000},
		Equity:  equity, DataSources: map[string]string{"sh600519": "test"},
		DataCoverage: map[string]DataCoverage{"sh600519": {CoverageRatio: 1}},
	}
}

func TestCandidateLifecycleRequiresForwardEvidenceBeforeApproval(t *testing.T) {
	lifecycle, err := NewCandidateLifecycle(lifecycleResult(), time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC), "2026-08-26")
	if err != nil || lifecycle.Status != CandidateLifecycleObserving {
		t.Fatalf("unexpected lifecycle: %#v %v", lifecycle, err)
	}
	UpdateCandidateObservation(&lifecycle, observationResult(20, 10, 3, 2, -4), "2026-09-20", time.Now())
	if lifecycle.Status != CandidateLifecycleObserving || lifecycle.Assessment.Passed {
		t.Fatalf("insufficient observation was marked ready: %#v", lifecycle)
	}
	UpdateCandidateObservation(&lifecycle, observationResult(60, 10, 3, 2, -4), "2026-11-20", time.Now())
	if lifecycle.Status != CandidateLifecycleApprovalReady || !lifecycle.Assessment.Passed {
		t.Fatalf("complete observation did not become approval-ready: %#v", lifecycle)
	}
	if err := ApproveCandidateLifecycle(&lifecycle, time.Now(), "前向观察门禁全部通过，批准进入下一轮研究基线"); err != nil || lifecycle.Status != CandidateLifecycleApproved {
		t.Fatalf("approved lifecycle invalid: %#v %v", lifecycle, err)
	}
	if len(lifecycle.Events) < 3 {
		t.Fatalf("lifecycle audit events missing: %#v", lifecycle.Events)
	}
}

func TestCandidateLifecycleApprovalRequiresNoteAndRejectRevokeAreAudited(t *testing.T) {
	lifecycle, err := NewCandidateLifecycle(lifecycleResult(), time.Now(), "2026-08-26")
	if err != nil {
		t.Fatal(err)
	}
	UpdateCandidateObservation(&lifecycle, observationResult(60, 10, 3, 2, -4), "2026-11-20", time.Now())
	if err := ApproveCandidateLifecycle(&lifecycle, time.Now(), ""); err == nil {
		t.Fatal("approval without evidence note should fail")
	}
	if err := RejectCandidateLifecycle(&lifecycle, time.Now(), "样本期行业暴露不稳定"); err != nil || lifecycle.Status != CandidateLifecycleRejected {
		t.Fatalf("rejected lifecycle invalid: %#v %v", lifecycle, err)
	}
	lifecycle, err = NewCandidateLifecycle(lifecycleResult(), time.Now(), "2026-08-26")
	if err != nil {
		t.Fatal(err)
	}
	UpdateCandidateObservation(&lifecycle, observationResult(60, 10, 3, 2, -4), "2026-11-20", time.Now())
	if err := ApproveCandidateLifecycle(&lifecycle, time.Now(), "通过观察门禁"); err != nil {
		t.Fatal(err)
	}
	if err := RevokeCandidateLifecycle(&lifecycle, time.Now(), "后续数据源质量下降"); err != nil || lifecycle.Status != CandidateLifecycleRevoked {
		t.Fatalf("revoked lifecycle invalid: %#v %v", lifecycle, err)
	}
}

func TestCandidateLifecycleRejectsHistoricalCandidateWithoutSelectedParameters(t *testing.T) {
	result := lifecycleResult()
	result.Stage = ContinuousStageResearch
	if _, err := NewAwaitingCandidateLifecycle(result); err == nil {
		t.Fatal("research candidate should not enter approval lifecycle")
	}
}
