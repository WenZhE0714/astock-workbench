package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type candidateObservationEngineStub struct {
	request backtest.Request
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
