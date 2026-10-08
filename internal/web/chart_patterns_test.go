package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type patternHistoryFixture map[string][]domain.DailyBar

func (fixture patternHistoryFixture) FetchDailyBars(_ context.Context, symbol string) ([]domain.DailyBar, error) {
	if bars := fixture[symbol]; len(bars) > 0 {
		return bars, nil
	}
	return nil, fmt.Errorf("隔离测试未提供该股票的日K")
}

func patternWebBars(symbol string, triangle, bearish bool) []domain.DailyBar {
	type knot struct {
		index int
		price float64
	}
	knots := []knot{{0, 114}, {36, 114}, {42, 115}, {50, 100}, {58, 110}, {66, 100.4}, {71, 106}, {76, 108}, {79, 112}}
	if triangle {
		knots = []knot{{0, 100}, {36, 100}, {42, 110}, {48, 98}, {54, 110.2}, {60, 101}, {66, 110.1}, {72, 104}, {79, 112}}
	}
	date := time.Date(2026, 6, 1, 0, 0, 0, 0, realtimeWebLocation)
	bars := make([]domain.DailyBar, 0, 80)
	segment := 0
	for len(bars) < 80 {
		if date.Weekday() != time.Saturday && date.Weekday() != time.Sunday {
			index := len(bars)
			for segment < len(knots)-2 && index > knots[segment+1].index {
				segment++
			}
			left, right := knots[segment], knots[segment+1]
			price := left.price + (right.price-left.price)*float64(index-left.index)/float64(right.index-left.index)
			if bearish {
				price = 220 - price
			}
			bars = append(bars, domain.DailyBar{Symbol: symbol, Source: "隔离形态测试", Date: date.Format(time.DateOnly), Open: price - .1, Close: price, High: price + .5, Low: price - .5, Volume: 1000, Amount: 1e8})
		}
		date = date.AddDate(0, 0, 1)
	}
	bars[79].Volume = 2000
	return bars
}

func overlappingPatternWebBars(symbol string) []domain.DailyBar {
	bars := patternWebBars(symbol, true, false)
	knots := []struct {
		index int
		price float64
	}{{0, 100}, {36, 100}, {42, 110}, {48, 99}, {54, 110.2}, {60, 100.5}, {66, 110.1}, {72, 102}, {79, 112.5}}
	segment := 0
	for index := range bars {
		for segment < len(knots)-2 && index > knots[segment+1].index {
			segment++
		}
		left, right := knots[segment], knots[segment+1]
		price := left.price + (right.price-left.price)*float64(index-left.index)/float64(right.index-left.index)
		bars[index].Open, bars[index].Close = price-.1, price
		bars[index].High, bars[index].Low = price+1, price-1
	}
	return bars
}

func chartPatternServerFixture(root string) *Server {
	server := chartServerFixture(filepath.Join(root, "plans"))
	server.planMonitors = storage.NewPlanMonitorStore(filepath.Join(root, "monitors"))
	server.planReviews = storage.NewTradePlanReviewStore(filepath.Join(root, "reviews"))
	history := patternHistoryFixture{
		"sh600519": patternWebBars("sh600519", false, false),
		"sh600000": patternWebBars("sh600000", false, true),
		"sh600001": patternWebBars("sh600001", true, false),
		"sh600002": patternWebBars("sh600002", true, true),
		"sh600003": overlappingPatternWebBars("sh600003"),
	}
	names := map[string]string{"sh600519": "双底测试", "sh600000": "双顶测试", "sh600001": "上升三角测试", "sh600002": "下降三角测试", "sh600003": "多形态测试"}
	server.history = history
	server.quotes = quoteClientFunc(func(_ context.Context, symbols []string) ([]domain.Quote, error) {
		quotes := make([]domain.Quote, 0)
		for _, symbol := range symbols {
			bars := history[symbol]
			if len(bars) == 0 {
				continue
			}
			last := bars[len(bars)-1]
			previous := bars[len(bars)-2].Close
			quotes = append(quotes, domain.Quote{Symbol: symbol, Name: names[symbol], Code: symbol[2:], Current: fmt.Sprintf("%.2f", last.Close), PreviousClose: fmt.Sprintf("%.2f", previous), Open: fmt.Sprintf("%.2f", last.Open), High: fmt.Sprintf("%.2f", last.High), Low: fmt.Sprintf("%.2f", last.Low), QuoteTime: last.Date + " 15:00:00", LimitUp: fmt.Sprintf("%.2f", previous*1.1), LimitDown: fmt.Sprintf("%.2f", previous*.9), Volume: last.Volume, Amount: last.Amount})
		}
		return quotes, nil
	})
	return server
}

func TestChartPatternFixtureRecognizesConcurrentClassicPatterns(t *testing.T) {
	server := chartPatternServerFixture(t.TempDir())
	analysis, err := server.chartAnalysis(context.Background(), "sh600003", "")
	if err != nil {
		t.Fatal(err)
	}
	confirmed := make(map[string]bool)
	for _, structure := range analysis.Structures {
		confirmed[structure.ID] = structure.State == "confirmed"
	}
	if !confirmed["double-bottom"] || !confirmed["ascending-triangle"] {
		t.Fatalf("missing overlapping classic patterns: %+v", analysis.Structures)
	}
}

func TestClassicChartPlansPersistGeometryAndMonitorRules(t *testing.T) {
	root := t.TempDir()
	server := chartPatternServerFixture(root)
	analysis := chartRequest(t, server)
	body, err := json.Marshal(map[string]string{"symbol": analysis.Symbol, "through": analysis.DataDate, "fingerprint": analysis.Fingerprint, "structure_id": "double-bottom", "expires_on": "2026-09-25"})
	if err != nil {
		t.Fatal(err)
	}
	recorder := postTradePlan(server, body)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("classic plan save: %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Plan domain.TradePlan `json:"plan"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Plan.MonitorRule == nil || response.Plan.MonitorRule.PatternReadyOn == "" || len(response.Plan.Structure.Lines) < 3 {
		t.Fatalf("classic plan lost evidence: %+v", response.Plan)
	}
	if enabled := setMonitor(t, server, response.Plan, true); enabled.Code != http.StatusOK {
		t.Fatalf("classic plan monitoring: %d %s", enabled.Code, enabled.Body.String())
	}
	restarted := chartPatternServerFixture(root)
	loaded, err := restarted.tradePlans.Load(response.Plan.Symbol, response.Plan.ID)
	if err != nil || !reflect.DeepEqual(loaded, response.Plan) {
		t.Fatalf("restart changed frozen geometry: %v", err)
	}
	monitor, err := restarted.planMonitors.Load(response.Plan.ID)
	if err != nil || !monitor.Enabled || monitor.Rule.PatternReadyOn != loaded.Structure.Pattern.ReadyOn || monitor.Rule.BreakoutPrice != loaded.Structure.Pattern.TriggerPrice {
		t.Fatalf("restart lost pattern rule: %v %+v", err, monitor)
	}
	if duplicate := postTradePlan(restarted, body); duplicate.Code != http.StatusOK {
		t.Fatalf("classic plan duplicate was not idempotent: %d", duplicate.Code)
	}
}

func TestClassicChartAnalysisAndStockUseSameGeometry(t *testing.T) {
	server := chartPatternServerFixture(t.TempDir())
	analysis := chartRequest(t, server)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/stock?symbol=600519&limit=21", nil))
	var response stockResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ChartAnalysis == nil || !reflect.DeepEqual(response.ChartAnalysis.Structures, analysis.Structures) {
		t.Fatal("stock API changed classic geometry with chart range")
	}
}

func TestChartPatternsBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_CHART_PATTERNS_TEST_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	server := chartPatternServerFixture(t.TempDir())
	if err := http.ListenAndServe(address, server.Handler()); err != nil {
		t.Fatal(err)
	}
}
