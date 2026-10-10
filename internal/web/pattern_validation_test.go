package web

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

func patternValidationServerFixture(root string) *Server {
	s := chartPatternServerFixture(root)
	s.patternValidations = storage.NewPatternValidationStore(filepath.Join(root, "validations"))
	history := s.history.(patternHistoryFixture)
	for symbol, bars := range history {
		for i := range bars {
			bars[i].Turnover = math.NaN()
			date, _ := time.Parse(time.DateOnly, bars[i].Date)
			bars[i].Date = date.AddDate(0, 0, -28).Format(time.DateOnly)
		}
		direction := 1.0
		if symbol == "sh600000" || symbol == "sh600002" || symbol == "sh600005" || symbol == "sh600007" {
			direction = -1
		}
		base := bars[79].Close
		date, _ := time.Parse(time.DateOnly, bars[79].Date)
		for len(bars) < 115 {
			date = date.AddDate(0, 0, 1)
			if date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
				continue
			}
			price := base * (1 + direction*.002*float64(len(bars)-79))
			bars = append(bars, domain.DailyBar{Symbol: symbol, Date: date.Format(time.DateOnly), Open: price, Close: price, High: price + .5, Low: price - .5, Volume: 1000, Source: "隔离形态验证数据"})
		}
		history[symbol] = bars
	}
	benchmark := append([]domain.DailyBar(nil), history["sh600004"]...)
	for i := range benchmark {
		price := 3000 + float64(i)*2
		benchmark[i].Symbol = "sh000300"
		benchmark[i].Open, benchmark[i].Close, benchmark[i].High, benchmark[i].Low = price, price, price+1, price-1
	}
	history["sh000300"] = benchmark
	s.now = func() time.Time { return time.Date(2026, 10, 9, 16, 0, 0, 0, realtimeWebLocation) }
	return s
}

func validationHTTPRequest(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRecorder()
	s.Handler().ServeHTTP(r, httptest.NewRequest(method, path, strings.NewReader(body)))
	return r
}

func TestPatternValidationAPIArchivesAndReadsWithoutMarketFetch(t *testing.T) {
	root := t.TempDir()
	s := patternValidationServerFixture(root)
	body := `{"symbols":["600004","600007"],"start":"2026-06-01","end":"2026-09-25"}`
	r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-validation", body)
	var result strategy.PatternValidationView
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || result.Report.RunID == "" || result.Totals.Mature < 2 || result.Report.Inputs != nil {
		t.Fatalf("run failed: %d %s", r.Code, r.Body.String())
	}
	plans, err := storage.NewTradePlanStore(filepath.Join(root, "plans")).All(100)
	if err != nil || len(plans) != 0 {
		t.Fatalf("validation plan-store check: %v, plans=%d", err, len(plans))
	}
	restarted := patternValidationServerFixture(root)
	restarted.history = nil
	restarted.quotes = nil
	restarted.resolver = nil
	path := "/api/pattern-validation?id=" + result.Report.RunID
	r = validationHTTPRequest(restarted, http.MethodGet, path+"&pattern=head-shoulders-bottom&horizon=20", "")
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || result.Total != 1 || result.Totals.Mature != 1 {
		t.Fatalf("archived filter failed: %s", r.Body.String())
	}
	r = validationHTTPRequest(restarted, http.MethodGet, path+"&download=1", "")
	var archived domain.PatternValidationReport
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &archived) != nil || len(archived.Inputs) != 3 || archived.InputHash == "" || !strings.Contains(r.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("export lost input evidence")
	}
	for _, test := range []struct {
		method, path, body string
		status             int
	}{
		{http.MethodGet, path + "&horizon=6", "", 400},
		{http.MethodGet, path + "&offset=bad", "", 400},
		{http.MethodGet, path + "&limit=101", "", 400},
		{http.MethodGet, "/api/pattern-validation?id=../x", "", 404},
		{http.MethodDelete, "/api/pattern-validation", "", 405},
		{http.MethodPost, "/api/pattern-validation", `{"symbols":[],"start":"2026-01-01","end":"2026-09-25"}`, 400},
		{http.MethodPost, "/api/pattern-validation", body + ` {}`, 400},
	} {
		r = validationHTTPRequest(s, test.method, test.path, test.body)
		if r.Code != test.status {
			t.Fatalf("%s %s: %d %s", test.method, test.path, r.Code, r.Body.String())
		}
	}
	s.patternValidationMu.Lock()
	r = validationHTTPRequest(s, http.MethodPost, "/api/pattern-validation", body)
	s.patternValidationMu.Unlock()
	if r.Code != 409 {
		t.Fatal("overlapping validation run accepted")
	}
}

func TestPatternValidationBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_PATTERN_VALIDATION_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	s := patternValidationServerFixture(t.TempDir())
	if err := http.ListenAndServe(address, s.Handler()); err != nil {
		t.Fatal(err)
	}
}
