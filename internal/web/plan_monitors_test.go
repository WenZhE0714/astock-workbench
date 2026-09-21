package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type planCountingHistory struct {
	next  DailyHistoryClient
	calls atomic.Int32
}

func (client *planCountingHistory) FetchDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	client.calls.Add(1)
	return client.next.FetchDailyBars(ctx, symbol)
}

type planCountingQuotes struct {
	next  QuoteClient
	calls atomic.Int32
}

func (client *planCountingQuotes) Fetch(ctx context.Context, symbols []string) ([]domain.Quote, error) {
	client.calls.Add(1)
	return client.next.Fetch(ctx, symbols)
}

func monitorServerFixture(t *testing.T, root string) (*Server, domain.TradePlan) {
	t.Helper()
	server := chartServerFixture(root)
	server.now = func() time.Time { return time.Date(2026, 9, 18, 9, 0, 0, 0, realtimeWebLocation) }
	server.planMonitors = storage.NewPlanMonitorStore(filepath.Join(root, "monitors"))
	server.tradingCalendarProvider = func(context.Context, time.Time) ([]string, error) {
		return []string{"2026-09-17", "2026-09-18", "2026-09-21"}, nil
	}
	body := planRequestBody(t, chartRequest(t, server))
	recorder := postTradePlan(server, body)
	if recorder.Code != http.StatusCreated && recorder.Code != http.StatusOK {
		t.Fatalf("plan save: %s", recorder.Body.String())
	}
	var response struct {
		Plan domain.TradePlan `json:"plan"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return server, response.Plan
}

func setMonitor(t *testing.T, server *Server, plan domain.TradePlan, enabled bool) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"symbol": plan.Symbol, "plan_id": plan.ID, "enabled": enabled})
	request := httptest.NewRequest(http.MethodPost, "/api/trade-plan-monitors", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestPlanMonitorAPIConfigAndRestartKeepOriginalPlan(t *testing.T) {
	root := t.TempDir()
	server, plan := monitorServerFixture(t, root)
	for range 2 {
		if recorder := setMonitor(t, server, plan, true); recorder.Code != http.StatusOK {
			t.Fatalf("enable: %s", recorder.Body.String())
		}
	}
	state, err := server.planMonitors.Load(plan.ID)
	if err != nil || !state.Enabled || state.Sequence != 1 {
		t.Fatalf("duplicate enable: %v %+v", err, state)
	}
	restarted, _ := monitorServerFixture(t, root)
	loaded, err := restarted.planMonitors.Load(plan.ID)
	if err != nil || !reflect.DeepEqual(state, loaded) {
		t.Fatal("restart changed monitoring state")
	}
	if recorder := setMonitor(t, restarted, plan, false); recorder.Code != http.StatusOK {
		t.Fatalf("pause: %s", recorder.Body.String())
	}
	if recorder := setMonitor(t, restarted, plan, true); recorder.Code != http.StatusOK {
		t.Fatalf("resume: %s", recorder.Body.String())
	}
	stored, err := restarted.tradePlans.Load(plan.Symbol, plan.ID)
	if err != nil || !reflect.DeepEqual(plan, stored) {
		t.Fatal("monitor controls rewrote plan snapshot")
	}
	recorder := httptest.NewRecorder()
	restarted.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/trade-plan-monitors?symbol=600519", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("code lookup: %s", recorder.Body.String())
	}
	var response struct {
		Items   []domain.PlanMonitor `json:"items"`
		Runtime planMonitorRuntime   `json:"runtime"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || len(response.Items) != 1 || !response.Runtime.Supported || response.Runtime.Running {
		t.Fatalf("incorrect runtime: %v %s", err, recorder.Body.String())
	}
}

func TestPlanMonitorCycleDoesNotFetchOutsideHoursAndClosesTwice(t *testing.T) {
	server, plan := monitorServerFixture(t, t.TempDir())
	server.now = func() time.Time { return time.Date(2026, 9, 18, 9, 0, 0, 0, realtimeWebLocation) }
	if recorder := setMonitor(t, server, plan, true); recorder.Code != http.StatusOK {
		t.Fatal(recorder.Body.String())
	}
	history := &planCountingHistory{next: server.history}
	quotes := &planCountingQuotes{next: server.quotes}
	server.history, server.quotes = history, quotes
	server.historyCache = make(map[string]historyCacheEntry)
	server.now = func() time.Time { return time.Date(2026, 9, 18, 12, 0, 0, 0, realtimeWebLocation) }
	server.runPlanMonitorCycle(context.Background(), false)
	if history.calls.Load() != 0 || quotes.calls.Load() != 0 {
		t.Fatal("lunch triggered upstream requests")
	}
	server.now = func() time.Time { return time.Date(2026, 9, 18, 15, 10, 0, 0, realtimeWebLocation) }
	server.runPlanMonitorCycle(context.Background(), false)
	first, _ := server.planMonitors.Load(plan.ID)
	if history.calls.Load() != 1 || first.LastClosingSlot != "2026-09-18:primary" || first.DataStatus != "healthy" {
		t.Fatalf("first close check: %+v", first)
	}
	server.historyCache = make(map[string]historyCacheEntry)
	server.now = func() time.Time { return time.Date(2026, 9, 18, 15, 20, 0, 0, realtimeWebLocation) }
	server.runPlanMonitorCycle(context.Background(), false)
	if history.calls.Load() != 1 {
		t.Fatal("close check ran continuously")
	}
	server.now = func() time.Time { return time.Date(2026, 9, 18, 16, 5, 0, 0, realtimeWebLocation) }
	server.runPlanMonitorCycle(context.Background(), false)
	last, _ := server.planMonitors.Load(plan.ID)
	if history.calls.Load() != 2 || quotes.calls.Load() != 0 || last.LastClosingSlot != "2026-09-18:retry" {
		t.Fatalf("closing retry: %+v", last)
	}
}

func TestPlanMonitorUnknownCalendarPausesWithoutGuessingWeekday(t *testing.T) {
	server, plan := monitorServerFixture(t, t.TempDir())
	if recorder := setMonitor(t, server, plan, true); recorder.Code != http.StatusOK {
		t.Fatal(recorder.Body.String())
	}
	server.now = func() time.Time { return time.Date(2026, 9, 21, 10, 0, 0, 0, realtimeWebLocation) }
	server.history = nil
	server.tradingCalendarProvider = nil
	quotes := &planCountingQuotes{next: server.quotes}
	server.quotes = quotes
	server.runPlanMonitorCycle(context.Background(), false)
	state, err := server.planMonitors.Load(plan.ID)
	if err != nil || state.DataStatus != "calendar_unavailable" || state.Phase != "waiting" || quotes.calls.Load() != 0 {
		t.Fatalf("guessed trading date: %v %+v", err, state)
	}
}

func TestPlanMonitorLoopExpiresWithoutBrowserOrScanner(t *testing.T) {
	server, plan := monitorServerFixture(t, t.TempDir())
	if recorder := setMonitor(t, server, plan, true); recorder.Code != http.StatusOK {
		t.Fatal(recorder.Body.String())
	}
	server.now = func() time.Time { return time.Date(2026, 9, 26, 10, 0, 0, 0, realtimeWebLocation) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); server.runPlanMonitorLoop(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("monitor loop did not stop")
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state, err := server.planMonitors.Load(plan.ID)
		if err == nil && state.Phase == "expired" {
			alerts := server.planMonitorAlerts(plan.Symbol)
			if len(alerts) != 1 || alerts[0].Kind != "plan-monitor" || alerts[0].ID != state.Events[len(state.Events)-1].ID {
				t.Fatalf("durable alert missing: %+v", alerts)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("background monitoring did not advance expiry")
}

func TestPlanMonitorControlRequiresExplicitBooleanAndSameOrigin(t *testing.T) {
	server, plan := monitorServerFixture(t, t.TempDir())
	body, _ := json.Marshal(map[string]string{"symbol": plan.Symbol, "plan_id": plan.ID})
	request := httptest.NewRequest(http.MethodPost, "/api/trade-plan-monitors", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatal("missing boolean defaulted to pause")
	}
	body, _ = json.Marshal(map[string]any{"symbol": plan.Symbol, "plan_id": plan.ID, "enabled": true})
	request = httptest.NewRequest(http.MethodPost, "/api/trade-plan-monitors", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://untrusted.example")
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatal("cross-site monitor write accepted")
	}
}

func TestPlanMonitorDownloadIncludesCompleteEventHistory(t *testing.T) {
	server, plan := monitorServerFixture(t, t.TempDir())
	for index := 0; index < 35; index++ {
		_, err := server.planMonitors.Update(plan.ID, func(current domain.PlanMonitor) (domain.PlanMonitor, error) {
			return strategy.ConfigurePlanMonitor(plan, current, index%2 == 0, plan.CreatedAt.Add(time.Duration(index)*time.Second))
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, sample := range []struct {
		suffix string
		count  int
	}{{"", 30}, {"&download=1", 35}} {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/trade-plan-monitors?plan_id="+plan.ID+sample.suffix, nil))
		var response struct {
			Items []domain.PlanMonitor `json:"items"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || len(response.Items) != 1 || len(response.Items[0].Events) != sample.count || response.Items[0].Sequence != 35 {
			t.Fatalf("history response: %v %s", err, recorder.Body.String())
		}
		if sample.suffix != "" && recorder.Header().Get("Content-Disposition") == "" {
			t.Fatal("download response lacks filename")
		}
	}
	server.planMonitors = nil
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/trade-plan-monitors?plan_id=x&download=1", nil))
	if recorder.Code >= 500 {
		t.Fatalf("disabled monitor download failed: %s", recorder.Body.String())
	}
}

func TestPlanMonitorAdjacentEODCalendarRequiresCurrentQuote(t *testing.T) {
	for _, sample := range []struct {
		name                     string
		dates                    []string
		quoteTime, phase, status string
		quoteCalls, historyCalls int32
	}{
		{"adjacent-eod", []string{"2026-09-17", "2026-09-18"}, "2026-09-21 10:00:00", "in_zone", "healthy", 1, 1},
		{"old-quote", []string{"2026-09-17", "2026-09-18"}, "2026-09-18 15:00:00", "waiting", "calendar_unavailable", 1, 0},
		{"long-gap", []string{"2026-09-16", "2026-09-17"}, "2026-09-21 10:00:00", "waiting", "calendar_unavailable", 0, 0},
		{"known-holiday", []string{"2026-09-17", "2026-09-18", "2026-09-22"}, "2026-09-21 10:00:00", "waiting", "closed", 0, 0},
	} {
		t.Run(sample.name, func(t *testing.T) {
			server, plan := monitorServerFixture(t, t.TempDir())
			if recorder := setMonitor(t, server, plan, true); recorder.Code != http.StatusOK {
				t.Fatal(recorder.Body.String())
			}
			bars, err := server.history.FetchDailyBars(context.Background(), plan.Symbol)
			if err != nil {
				t.Fatal(err)
			}
			bars = append([]domain.DailyBar(nil), bars...)
			bars[len(bars)-1].Close, bars[len(bars)-1].High, bars[len(bars)-1].Volume = 111, 112, 1600
			history := &planCountingHistory{next: chartHistoryStub{bars: bars}}
			quotes := &planCountingQuotes{next: chartQuoteStub{quote: domain.Quote{Symbol: plan.Symbol, Current: "110.20", PreviousClose: "111.00", QuoteTime: sample.quoteTime}}}
			server.history, server.quotes = history, quotes
			server.historyCache = make(map[string]historyCacheEntry)
			server.now = func() time.Time { return time.Date(2026, 9, 21, 10, 0, 0, 0, realtimeWebLocation) }
			server.tradingCalendarProvider = func(context.Context, time.Time) ([]string, error) { return sample.dates, nil }
			server.runPlanMonitorCycle(context.Background(), false)
			state, err := server.planMonitors.Load(plan.ID)
			if err != nil || state.Phase != sample.phase || state.DataStatus != sample.status || history.calls.Load() != sample.historyCalls || quotes.calls.Load() != sample.quoteCalls {
				t.Fatalf("calendar qualification: %v %+v; quotes=%d history=%d", err, state, quotes.calls.Load(), history.calls.Load())
			}
			if sample.phase == "in_zone" && state.CalendarBasis == "" {
				t.Fatal("missing calendar evidence")
			}
		})
	}
}
