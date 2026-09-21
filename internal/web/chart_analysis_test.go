package web

import (
	"bytes"
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

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type chartHistoryStub struct{ bars []domain.DailyBar }

func (stub chartHistoryStub) FetchDailyBars(_ context.Context, symbol string) ([]domain.DailyBar, error) {
	if len(stub.bars) > 0 && stub.bars[0].Symbol != "" && stub.bars[0].Symbol != symbol {
		return nil, fmt.Errorf("隔离预览没有该证券的日K快照")
	}
	return stub.bars, nil
}

type chartQuoteStub struct{ quote domain.Quote }

func (stub chartQuoteStub) Fetch(_ context.Context, symbols []string) ([]domain.Quote, error) {
	for _, symbol := range symbols {
		if symbol == stub.quote.Symbol {
			return []domain.Quote{stub.quote}, nil
		}
	}
	return nil, fmt.Errorf("隔离预览没有该证券的行情快照")
}

type chartSnapshotResolver struct{ symbol, name string }

func (resolver chartSnapshotResolver) Resolve(_ context.Context, input string) (string, error) {
	if input == resolver.symbol || (len(resolver.symbol) == 8 && input == resolver.symbol[2:]) || input == resolver.name {
		return resolver.symbol, nil
	}
	return "", fmt.Errorf("隔离预览仅载入 %s %s", resolver.symbol, resolver.name)
}

type chartMinuteStub struct{ points []domain.MinutePoint }

func (stub chartMinuteStub) FetchMinutePoints(context.Context, string) ([]domain.MinutePoint, error) {
	if len(stub.points) == 0 {
		return nil, fmt.Errorf("预览快照未包含分时数据")
	}
	return stub.points, nil
}

func chartServerFixture(root string) *Server {
	date := time.Date(2026, 6, 1, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	bars := make([]domain.DailyBar, 0, 80)
	for len(bars) < 80 {
		if date.Weekday() != time.Saturday && date.Weekday() != time.Sunday {
			price := 100 + float64(len(bars))*.1
			bars = append(bars, domain.DailyBar{Symbol: "sh600519", Source: "fixture", Date: date.Format(time.DateOnly), Open: price, Close: price, High: price + 2, Low: price - 2, Volume: 1000})
		}
		date = date.AddDate(0, 0, 1)
	}
	quote := domain.Quote{Symbol: "sh600519", Name: "测试股份", Code: "600519", Current: "107.90", PreviousClose: "107.80", Open: "107.90", High: "109.90", Low: "105.90", QuoteTime: "2026-09-18 15:00:00", LimitUp: "118.58", LimitDown: "97.02"}
	server := NewServer(resolverStub{}, chartQuoteStub{quote: quote}, chartHistoryStub{bars: bars}, minuteStub{}, "600519", WithTradePlans(storage.NewTradePlanStore(root)))
	server.now = func() time.Time { return time.Date(2026, 9, 18, 16, 0, 0, 0, date.Location()) }
	return server
}

func chartRequest(t *testing.T, server *Server) domain.ChartAnalysis {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/chart-analysis?symbol=600519", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("chart request failed: %d %s", recorder.Code, recorder.Body.String())
	}
	var analysis domain.ChartAnalysis
	if err := json.Unmarshal(recorder.Body.Bytes(), &analysis); err != nil {
		t.Fatal(err)
	}
	return analysis
}

func planRequestBody(t *testing.T, analysis domain.ChartAnalysis) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]string{"symbol": analysis.Symbol, "through": analysis.DataDate, "fingerprint": analysis.Fingerprint, "structure_id": "range-breakout", "expires_on": "2026-09-25"})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func postTradePlan(server *Server, body []byte) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/trade-plans", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestChartEndpointAndStockUseSameUntruncatedHistory(t *testing.T) {
	server := chartServerFixture(t.TempDir())
	analysis := chartRequest(t, server)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/stock?symbol=600519&limit=21", nil))
	var payload stockResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ChartAnalysis == nil || payload.ChartAnalysis.Fingerprint != analysis.Fingerprint || !payload.PlansEnabled || analysis.BarsUsed != 80 {
		t.Fatalf("chart period affected analysis: %+v", payload)
	}
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/chart-analysis?symbol=600519&through=2026-08-28", nil))
	var historical domain.ChartAnalysis
	if err := json.Unmarshal(recorder.Body.Bytes(), &historical); err != nil {
		t.Fatal(err)
	}
	if historical.DataDate != "2026-08-28" || historical.BarsUsed >= analysis.BarsUsed || historical.Fingerprint == analysis.Fingerprint {
		t.Fatalf("historical cutoff ignored: %+v", historical)
	}
}

func TestTradePlanAPIRestartAndDuplicatePreserveSnapshot(t *testing.T) {
	root := t.TempDir()
	server := chartServerFixture(root)
	body := planRequestBody(t, chartRequest(t, server))
	if recorder := postTradePlan(server, body); recorder.Code != http.StatusCreated {
		t.Fatalf("save failed: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := postTradePlan(server, body); recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"created":false`) {
		t.Fatalf("duplicate not idempotent: %d %s", recorder.Code, recorder.Body.String())
	}
	restarted := chartServerFixture(root)
	recorder := httptest.NewRecorder()
	restarted.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/trade-plans?symbol=600519", nil))
	var payload struct {
		Items []domain.TradePlan `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || len(payload.Items) != 1 || payload.Items[0].Analysis.DataDate != "2026-09-18" {
		t.Fatalf("restart lost plan: %v %s", err, recorder.Body.String())
	}
}

func TestTradePlanAPIRejectsStaleSnapshotAndClientPrices(t *testing.T) {
	server := chartServerFixture(t.TempDir())
	analysis := chartRequest(t, server)
	analysis.Fingerprint = strings.Repeat("0", 64)
	if recorder := postTradePlan(server, planRequestBody(t, analysis)); recorder.Code != http.StatusConflict {
		t.Fatalf("stale snapshot accepted: %d %s", recorder.Code, recorder.Body.String())
	}
	for _, body := range []string{
		`{"symbol":"sh600519","entry_low":1}`,
		string(planRequestBody(t, chartRequest(t, server))) + `{}`,
	} {
		if recorder := postTradePlan(server, []byte(body)); recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid body accepted: %d %s", recorder.Code, recorder.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/trade-plans", bytes.NewReader(planRequestBody(t, chartRequest(t, server))))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://untrusted.example")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-site write accepted: %d", recorder.Code)
	}
}

func TestTradePlansOnlySupportAShareStocks(t *testing.T) {
	for _, symbol := range []string{"sh600519", "sh688001", "sz000001", "sz301001", "bj920001", "bj832001"} {
		if !supportsStockPlan(symbol) {
			t.Errorf("stock not supported: %s", symbol)
		}
	}
	for _, symbol := range []string{"sh000001", "sh000300", "sz399001", "bj899050", "sh510300", "sh113001", "../../a", "sh60051x"} {
		if supportsStockPlan(symbol) {
			t.Errorf("non-stock can save a stock plan: %s", symbol)
		}
	}
}

// This opt-in fixture serves the production page and API with an isolated
// temporary plan store. It never starts scanners or touches real accounts.
func TestChartBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_CHART_TEST_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	root := os.Getenv("ASTOCK_CHART_TEST_DATA_DIR")
	if root == "" {
		root = t.TempDir()
	}
	server := chartServerFixture(filepath.Join(root, "plans"))
	server.planMonitors = storage.NewPlanMonitorStore(filepath.Join(root, "monitors"))
	server.planExperiment = storage.NewPlanExperimentStore(filepath.Join(root, "plan-experiment.json"))
	server.tradingCalendarProvider = func(context.Context, time.Time) ([]string, error) {
		return []string{"2026-09-17", "2026-09-18", "2026-09-21"}, nil
	}
	if snapshot := os.Getenv("ASTOCK_CHART_TEST_SNAPSHOT"); snapshot != "" {
		data, err := os.ReadFile(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		var stock struct {
			Symbol  string               `json:"symbol"`
			Name    string               `json:"name"`
			Quote   domain.Quote         `json:"quote"`
			Bars    []domain.DailyBar    `json:"bars"`
			Minutes []domain.MinutePoint `json:"minutes"`
		}
		if err := json.Unmarshal(data, &stock); err != nil || len(stock.Bars) < 21 || stock.Symbol == "" {
			t.Fatalf("invalid preview snapshot: %v", err)
		}
		stock.Quote.Name += " (快照预览)"
		server.resolver = chartSnapshotResolver{symbol: stock.Symbol, name: stock.Name}
		server.defaultSymbol = stock.Symbol
		server.quotes = chartQuoteStub{quote: stock.Quote}
		server.history = chartHistoryStub{bars: stock.Bars}
		server.minutes = chartMinuteStub{points: stock.Minutes}
		server.now = time.Now
		server.tradingCalendarProvider = nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.runPlanMonitorLoop(ctx)
	if err := http.ListenAndServe(address, server.Handler()); err != nil {
		t.Fatal(err)
	}
}
