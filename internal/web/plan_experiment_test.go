package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func experimentRequest(t *testing.T, server *Server, input map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/plan-experiment", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestPlanExperimentAPIControlsRequireMonitorAndPersist(t *testing.T) {
	root := t.TempDir()
	server, plan := monitorServerFixture(t, root)
	file := filepath.Join(root, "experiment.json")
	server.planExperiment = storage.NewPlanExperimentStore(file)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/plan-experiment", nil))
	if recorder.Code != http.StatusOK {
		t.Fatal(recorder.Body.String())
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("GET initialized experiment")
	}
	selectInput := map[string]any{"action": "select", "symbol": plan.Symbol, "plan_id": plan.ID, "enabled": true}
	if response := experimentRequest(t, server, selectInput); response.Code != http.StatusConflict {
		t.Fatal("unmonitored plan was selected")
	}
	if response := setMonitor(t, server, plan, true); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if response := experimentRequest(t, server, selectInput); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if response := experimentRequest(t, server, map[string]any{"action": "entries", "enabled": true}); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	state, err := storage.NewPlanExperimentStore(file).Load()
	if err != nil || !state.EntriesEnabled || !state.Selections[plan.ID].Enabled {
		t.Fatalf("restart state: %v %+v", err, state)
	}
	if response := experimentRequest(t, server, map[string]any{"action": "entries", "enabled": false}); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	state, err = storage.NewPlanExperimentStore(file).Load()
	if err != nil || state.EntriesEnabled || len(state.Arms) != 2 || state.Arms[0].Report.InitialCash != state.Arms[1].Report.InitialCash {
		t.Fatal("paired configuration changed")
	}
}

func TestPlanExperimentCycleUsesLaterQuoteAndKeepsOldAccountsUntouched(t *testing.T) {
	root := t.TempDir()
	server, plan := monitorServerFixture(t, root)
	server.planExperiment = storage.NewPlanExperimentStore(filepath.Join(root, "experiment.json"))
	if response := setMonitor(t, server, plan, true); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	for _, input := range []map[string]any{{"action": "select", "symbol": plan.Symbol, "plan_id": plan.ID, "enabled": true}, {"action": "entries", "enabled": true}} {
		if response := experimentRequest(t, server, input); response.Code != http.StatusOK {
			t.Fatal(response.Body.String())
		}
	}
	bars, err := server.history.FetchDailyBars(context.Background(), plan.Symbol)
	if err != nil {
		t.Fatal(err)
	}
	for index := range bars {
		bars[index].Amount = 1e8
	}
	server.history = chartHistoryStub{bars: bars}
	server.historyCache = make(map[string]historyCacheEntry)
	at := time.Date(2026, 9, 21, 10, 0, 0, 0, realtimeWebLocation)
	for step := 0; step < 2; step++ {
		quoteAt := at.Add(time.Duration(step) * 30 * time.Second)
		server.now = func() time.Time { return quoteAt.Add(time.Second) }
		server.quotes = chartQuoteStub{quote: domain.Quote{Symbol: plan.Symbol, Current: "110.00", PreviousClose: "107.90", LimitUp: "118.69", LimitDown: "97.11", QuoteTime: quoteAt.Format("2006-01-02 15:04:05"), Volume: 10000, Amount: 1e7}}
		server.quoteCache = make(map[string]quoteCacheEntry)
		_, err = server.planMonitors.Update(plan.ID, func(state domain.PlanMonitor) (domain.PlanMonitor, error) {
			state.Phase = "in_zone"
			state.ConfirmedOn = "2026-09-18"
			state.DataStatus = "healthy"
			state.HistoryDate = "2026-09-18"
			state.LastQuoteAt = quoteAt
			state.UpdatedAt = quoteAt
			return state, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		server.runPlanExperimentCycle(context.Background())
		state, err := server.planExperiment.Load()
		if err != nil {
			t.Fatal(err)
		}
		if len(state.Arms[0].Report.Orders) != step {
			t.Fatalf("step %d orders=%d runtime=%+v", step, len(state.Arms[0].Report.Orders), server.experimentRuntime())
		}
	}
	state, _ := server.planExperiment.Load()
	if state.Arms[0].Report.Orders[0].PlanID != plan.ID || state.Arms[0].Report.Positions[0].AvailableQuantity != 0 {
		t.Fatal("plan attribution or T+1 missing")
	}
	if len(server.shadowProfiles) != 0 {
		t.Fatal("experiment added legacy shadow profiles")
	}
	if err := paper.ValidatePlanExperimentTransition(paper.PlanExperiment{}, state); err != nil {
		t.Fatal(err)
	}
}
