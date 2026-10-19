package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type candidateObservationEngineStub struct {
	request backtest.Request
}

func rejectedCandidateFixture() backtest.ContinuousOptimizationResult {
	parameters := backtest.DefaultTechnicalParameters()
	folds := make([]backtest.WalkForwardFold, 4)
	results := make([]backtest.WalkForwardFoldResult, 4)
	for index := range folds {
		start := time.Date(2021+index, 7, 20, 0, 0, 0, 0, strategyLocation)
		folds[index] = backtest.WalkForwardFold{ID: fmt.Sprintf("F%02d", index+1), Validate: backtest.Period{Start: start, End: start.AddDate(1, 0, -1)}}
		results[index] = backtest.WalkForwardFoldResult{Fold: folds[index], Validate: backtest.Metrics{TotalReturn: float64(index*3 - 4), MaxDrawdown: -8, Trades: 25}}
	}
	return backtest.ContinuousOptimizationResult{
		ID: "AUTO-diagnostics", GeneratedAt: time.Date(2026, 10, 19, 16, 0, 0, 0, strategyLocation), DataCutoff: "2026-10-19", Stage: backtest.ContinuousStageResearch,
		Request:     backtest.ContinuousOptimizationRequest{Folds: folds, BaseRequest: backtest.Request{Tickers: []string{"sh600000"}, Names: map[string]string{"sh600000": "浦发银行"}}},
		GateReasons: []string{"所有候选均未通过滚动验证门禁"},
		Candidates: []backtest.ContinuousCandidateResult{
			{Proposal: backtest.StrategyProposal{ID: "P009", Agent: "deterministic", Parameters: parameters}, Folds: results, Rejected: true, PositiveFoldRatio: .5, ValidationTrades: 100, AverageValidation: 3, WorstDrawdown: -8, Reasons: []string{"正收益窗口 50% 低于门槛 67%"}},
			{Proposal: backtest.StrategyProposal{ID: "P008", Agent: "deterministic", Parameters: parameters}, Folds: results, Rejected: true, PositiveFoldRatio: .75, ValidationTrades: 110, AverageValidation: 5, WorstDrawdown: -18.29, Reasons: []string{"最差验证回撤 18.29% 超过门槛 15.00%"}},
			{Proposal: backtest.StrategyProposal{ID: "P003", Parameters: parameters}, Folds: results[:1], Rejected: true, Reasons: []string{"F02验证失败：数据不足"}},
		},
	}
}

func TestStrategyCandidateDiagnosticsDoNotLockRejectedParameters(t *testing.T) {
	store := storage.NewContinuousOptimizationStore(t.TempDir())
	saved, err := store.Save(rejectedCandidateFixture())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(saved.Directory, "summary.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(nil, nil, nil, nil, "", WithStrategyCandidateLifecycle(&candidateObservationEngineStub{}, store))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/strategy/candidates", nil))
	var payload strategyCandidateListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || response.Code != 200 {
		t.Fatalf("diagnostics: %s %v", response.Body.String(), err)
	}
	item := payload.Items[0]
	if item.Parameters != nil || item.SelectedID != "" || item.Lifecycle.ExperimentID != "" || item.PassedCount != 0 || item.CandidateCount != 3 {
		t.Fatalf("rejected candidate promoted: %+v", item)
	}
	if len(item.Diagnostics) != 3 || item.Diagnostics[0].Parameters.FastMA == 0 || item.Diagnostics[0].Selected || *item.Diagnostics[0].PositiveFoldRatio != .5 {
		t.Fatalf("diagnostics incomplete: %+v", item.Diagnostics)
	}
	if item.Diagnostics[2].PositiveFoldRatio != nil || item.Diagnostics[2].ValidationTrades != nil {
		t.Fatal("incomplete evaluation advertised zero metrics")
	}
	if !strings.Contains(item.SelectionSummary, "3 组候选均未通过") {
		t.Fatal(item.SelectionSummary)
	}
	post := httptest.NewRecorder()
	server.Handler().ServeHTTP(post, httptest.NewRequest("POST", "/api/strategy/candidates?id="+saved.ID, strings.NewReader(`{"action":"start-observation"}`)))
	if post.Code != http.StatusConflict {
		t.Fatalf("rejected parameters became observable: %d", post.Code)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("diagnostics changed original experiment")
	}
}

func TestStrategyCandidateDiagnosticsKeepLockedButBlockedSelection(t *testing.T) {
	store := storage.NewContinuousOptimizationStore(t.TempDir())
	result := rejectedCandidateFixture()
	selected := result.Candidates[0]
	selected.Rejected = false
	selected.Reasons = nil
	result.Selected = &selected
	result.Candidates[0] = selected
	result.GateReasons = []string{"最终留出未通过"}
	if _, err := store.Save(result); err != nil {
		t.Fatal(err)
	}
	server := NewServer(nil, nil, nil, nil, "", WithStrategyCandidateLifecycle(&candidateObservationEngineStub{}, store))
	item, err := server.strategyCandidateSummary(result)
	if err != nil {
		t.Fatal(err)
	}
	if item.Parameters == nil || item.SelectedID != "P009" || !item.Diagnostics[0].Selected || !strings.Contains(item.NextAction, "参数已锁定") {
		t.Fatalf("existing selection lost: %+v", item)
	}
}

func TestStrategyCandidateDiagnosticsBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_CANDIDATE_TEST_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	store := storage.NewContinuousOptimizationStore(t.TempDir())
	if _, err := store.Save(rejectedCandidateFixture()); err != nil {
		t.Fatal(err)
	}
	server := NewServer(nil, nil, nil, nil, "600000", WithStrategyCandidateLifecycle(&candidateObservationEngineStub{}, store))
	if err := http.ListenAndServe(address, server.Handler()); err != nil {
		t.Fatal(err)
	}
}

func (stub *candidateObservationEngineStub) Run(_ context.Context, request backtest.Request) (backtest.Result, error) {
	stub.request = request
	equity := make([]backtest.EquityPoint, 60)
	for index := range equity {
		equity[index] = backtest.EquityPoint{Date: time.Date(2026, 8, 26+index, 0, 0, 0, 0, strategyLocation).Format("2006-01-02"), Equity: 1_020_000}
	}
	return backtest.Result{
		Request: request,
		Metrics: backtest.Metrics{
			TotalReturn: 2, ExcessReturn: 1, BenchmarkAvailable: true, MaxDrawdown: -4,
			Trades: 10, FinalEquity: 1_020_000,
		},
		Equity: equity, DataSources: map[string]string{"sh600519": "test"},
		DataCoverage: map[string]backtest.DataCoverage{"sh600519": {CoverageRatio: 1}},
	}, nil
}

func TestStrategyCandidateLifecycleAPIRequiresForwardObservationAndManualApproval(t *testing.T) {
	store := storage.NewContinuousOptimizationStore(t.TempDir())
	parameters := backtest.DefaultTechnicalParameters()
	selected := backtest.ContinuousCandidateResult{Proposal: backtest.StrategyProposal{ID: "P001", Parameters: parameters}}
	_, err := store.Save(backtest.ContinuousOptimizationResult{
		ID: "AUTO-web-lifecycle", Cycle: 1, DataCutoff: "2026-08-24", GeneratedAt: time.Date(2026, 8, 25, 9, 0, 0, 0, strategyLocation),
		Stage:    backtest.ContinuousStageShadow,
		Manifest: backtest.ExperimentManifest{CandidateSetHash: "candidate", ConfigurationHash: "config"},
		Quality:  backtest.DataQualitySummary{Grade: "A", Passed: true},
		Request: backtest.ContinuousOptimizationRequest{BaseRequest: backtest.Request{
			Strategy: "technical-breakout", StrategyVersion: "v1", Tickers: []string{"sh600519"}, Names: map[string]string{"sh600519": "贵州茅台"},
			InitialCash: 1_000_000, CommissionRate: .0003, MinimumCommission: 5, StampDutyRate: .0005, TransferFeeRate: .00001,
			SlippageBPS: 5, Adjustment: backtest.AdjustmentNone, Benchmark: "sh000300", NoFutureData: true, Technical: parameters,
		}}, Selected: &selected,
		Holdout: &backtest.Result{Metrics: backtest.Metrics{TotalReturn: 3, Trades: 12, Sharpe: 1, MaxDrawdown: -5}},
		Stress:  backtest.StressResult{DoubleCost: &backtest.Result{Metrics: backtest.Metrics{TotalReturn: 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine := &candidateObservationEngineStub{}
	server := NewServer(nil, nil, nil, nil, "600519", WithStrategyCandidateLifecycle(engine, store))
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, strategyLocation)
	server.now = func() time.Time { return now }

	list := httptest.NewRecorder()
	server.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/strategy/candidates", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), backtest.CandidateLifecycleAwaitingObservation) {
		t.Fatalf("candidate list missing awaiting lifecycle: %d %s", list.Code, list.Body.String())
	}

	start := httptest.NewRecorder()
	server.Handler().ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/api/strategy/candidates?id=AUTO-web-lifecycle", strings.NewReader(`{"action":"start-observation","note":""}`)))
	if start.Code != http.StatusOK {
		t.Fatalf("start observation failed: %d %s", start.Code, start.Body.String())
	}
	var started strategyCandidateSummary
	if err := json.Unmarshal(start.Body.Bytes(), &started); err != nil || started.Lifecycle.ObservationStart != "2026-08-26" {
		t.Fatalf("observation did not start on next trading day: %#v %v", started, err)
	}

	now = time.Date(2026, 11, 30, 16, 0, 0, 0, strategyLocation)
	refresh := httptest.NewRecorder()
	server.Handler().ServeHTTP(refresh, httptest.NewRequest(http.MethodPost, "/api/strategy/candidates?id=AUTO-web-lifecycle", strings.NewReader(`{"action":"refresh-observation","note":""}`)))
	if refresh.Code != http.StatusOK {
		t.Fatalf("refresh observation failed: %d %s", refresh.Code, refresh.Body.String())
	}
	var refreshed strategyCandidateSummary
	if err := json.Unmarshal(refresh.Body.Bytes(), &refreshed); err != nil || refreshed.Lifecycle.Status != backtest.CandidateLifecycleApprovalReady || !refreshed.Lifecycle.Assessment.Passed {
		t.Fatalf("complete observation did not become approval-ready: %#v %v", refreshed, err)
	}
	if engine.request.Start.Format("2006-01-02") != "2026-08-26" || engine.request.LiquidateAtEnd {
		t.Fatalf("forward observation request leaked old data or forced liquidation: %#v", engine.request)
	}

	approve := httptest.NewRecorder()
	server.Handler().ServeHTTP(approve, httptest.NewRequest(http.MethodPost, "/api/strategy/candidates?id=AUTO-web-lifecycle", strings.NewReader(`{"action":"approve-baseline","note":"前向观察门禁全部通过"}`)))
	if approve.Code != http.StatusOK || !strings.Contains(approve.Body.String(), backtest.CandidateLifecycleApproved) {
		t.Fatalf("manual approval failed: %d %s", approve.Code, approve.Body.String())
	}
	loaded, err := store.LoadLifecycle("AUTO-web-lifecycle")
	if err != nil || loaded.Status != backtest.CandidateLifecycleApproved || loaded.DecisionNote == "" {
		t.Fatalf("approved lifecycle was not archived: %#v %v", loaded, err)
	}
}
