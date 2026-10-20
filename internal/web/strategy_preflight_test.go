package web

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

type preflightEngineStub struct {
	calls, runs int
	err         error
}

func (e *preflightEngineStub) Run(context.Context, backtest.Request) (backtest.Result, error) {
	e.runs++
	return backtest.Result{}, nil
}
func (e *preflightEngineStub) InspectHistory(_ context.Context, request backtest.Request) (backtest.HistoryPreflight, error) {
	e.calls++
	return backtest.HistoryPreflight{CheckedAt: time.Now(), Unavailable: 1, Items: []backtest.HistoryInspection{{Symbol: request.Tickers[0], Detail: "source unavailable"}}}, e.err
}

func TestStrategyPreflightNeverRunsOrArchivesAndReleasesLock(t *testing.T) {
	engine, archive := &preflightEngineStub{}, &strategyArchiveStub{}
	s := NewServer(resolverStub{}, nil, nil, nil, "", WithStrategyResearch(engine, archive))
	request := func(method, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(method, "/api/strategy/preflight", strings.NewReader(body)))
		return w
	}
	body := `{"symbols":["sh688825"],"start":"2023-01-01","end":"2025-12-31"}`
	w := request("POST", body)
	var report backtest.HistoryPreflight
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil || w.Code != 200 || report.Ready || report.Unavailable != 1 {
		t.Fatalf("report: %d %s", w.Code, w.Body.String())
	}
	if engine.runs != 0 || archive.result.RunID != "" || s.strategyRunning {
		t.Fatal("preflight executed trades, saved results or retained lock")
	}
	engine.err = context.DeadlineExceeded
	if w = request("POST", body); w.Code != 408 || s.strategyRunning {
		t.Fatal("incomplete inspection should fail and release lock")
	}
	s.strategyRunning = true
	if w = request("POST", body); w.Code != 409 {
		t.Fatal("concurrent run accepted")
	}
	s.strategyRunning = false
	if w = request("GET", ""); w.Code != 405 {
		t.Fatal("GET inspection accepted")
	}
	if w = request("POST", `{"symbols":["sh516350"],"start":"2023-01-01","end":"2025-12-31"}`); w.Code != 400 {
		t.Fatal("ETF bypassed stock validation")
	}
	if engine.calls != 2 {
		t.Fatal("invalid request reached inspector")
	}
}

func TestStrategyPreflightUnsupportedEngineDoesNotRun(t *testing.T) {
	s := NewServer(resolverStub{}, nil, nil, nil, "", WithStrategyResearch(&strategyEngineStub{}, &strategyArchiveStub{}))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/strategy/preflight", strings.NewReader(`{}`)))
	if w.Code != 503 {
		t.Fatal("unsupported inspector not reported")
	}
}
