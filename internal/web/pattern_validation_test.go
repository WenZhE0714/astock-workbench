package web

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
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
	s.patternActions = validationActionFixture{}
	return s
}

type validationActionFixture struct {
	Events map[string][]domain.CorporateAction
	Err    error
}

func (fixture validationActionFixture) FetchCorporateActions(_ context.Context, symbol, start, end string) (domain.CorporateActionSnapshot, error) {
	events := append([]domain.CorporateAction{}, fixture.Events[symbol]...)
	return domain.CorporateActionSnapshot{Symbol: symbol, Start: start, End: end, Status: "checked", Scope: domain.CorporateActionScope, Source: "隔离分红送转数据", SourceURL: "https://data.eastmoney.com/yjfp/", FetchedAt: time.Date(2026, 10, 9, 16, 0, 0, 0, realtimeWebLocation), Events: events}, fixture.Err
}

type validationRangeFixture struct {
	patternHistoryFixture
	starts, ends []time.Time
	adjustments  []backtest.PriceAdjustment
	err          error
}

func (fixture *validationRangeFixture) FetchDailyBarsRange(_ context.Context, symbol string, start, end time.Time, adjustment backtest.PriceAdjustment) ([]domain.DailyBar, error) {
	fixture.starts = append(fixture.starts, start)
	fixture.ends = append(fixture.ends, end)
	fixture.adjustments = append(fixture.adjustments, adjustment)
	return fixture.patternHistoryFixture[symbol], fixture.err
}

func TestPatternValidationUsesRangeWithWarmupAndArchivesFetchEvidence(t *testing.T) {
	s := patternValidationServerFixture(t.TempDir())
	fixture := &validationRangeFixture{patternHistoryFixture: s.history.(patternHistoryFixture)}
	s.patternHistory = fixture
	s.history = nil
	r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-validation", `{"symbols":["600004"],"start":"2026-06-01","end":"2026-09-25"}`)
	var result strategy.PatternValidationView
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	start, _ := time.Parse(time.DateOnly, "2026-06-01")
	if len(fixture.starts) != 2 || !fixture.starts[0].Equal(start.AddDate(0, 0, -240)) || fixture.ends[0].Format(time.DateOnly) != "2026-09-25" || fixture.adjustments[0] != backtest.AdjustmentNone {
		t.Fatalf("incorrect acquisition bounds: %+v", fixture)
	}
	report, err := s.patternValidations.Load(result.Report.RunID)
	if err != nil {
		t.Fatal(err)
	}
	input := report.Inputs[0]
	if input.FetchMode != "range" || input.FetchStart != fixture.starts[0].Format(time.DateOnly) || input.FetchEnd != "2026-09-25" || input.Bars[len(input.Bars)-1].Date > input.FetchEnd || report.Coverage[0].FetchMode != "range" {
		t.Fatalf("fetch evidence missing: %+v", input)
	}
	// Existing archives remain readable without recomputation or new metadata.
	report.Version = "pattern-validation-v1"
	legacy, err := s.patternValidations.Save(report)
	if err != nil {
		t.Fatal(err)
	}
	r = validationHTTPRequest(s, http.MethodGet, "/api/pattern-validation?id="+legacy.RunID, "")
	if r.Code != 200 {
		t.Fatalf("legacy archive rejected: %s", r.Body.String())
	}
}

func TestPatternValidationSnapshotFallbackIsExplicitAndExcludesFuture(t *testing.T) {
	s := patternValidationServerFixture(t.TempDir())
	input := s.fetchPatternValidationHistory(t.Context(), "sh600004", "2026-06-01", "2026-08-20")
	if input.Error != "" || input.FetchMode != "snapshot" || input.FetchWarning == "" || len(input.Bars) == 0 || input.Bars[len(input.Bars)-1].Date > "2026-08-20" {
		t.Fatalf("silent fallback or future data: %+v", input)
	}
	s.patternHistory = &validationRangeFixture{err: errors.New("范围不足")}
	input = s.fetchPatternValidationHistory(t.Context(), "sh600004", "2026-06-01", "2026-08-20")
	if input.Error != "" || input.FetchMode != "snapshot" || !strings.Contains(input.FetchWarning, "范围不足") {
		t.Fatal("range failure fallback was not disclosed")
	}
}

func TestPatternValidationActionFailureDoesNotBecomeNoEvents(t *testing.T) {
	s := patternValidationServerFixture(t.TempDir())
	s.patternActions = validationActionFixture{Err: errors.New("分红接口暂不可用")}
	r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-validation", `{"symbols":["600004"],"start":"2026-06-01","end":"2026-09-25"}`)
	var result strategy.PatternValidationView
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || result.Totals.Mature != 0 || result.Totals.ActionUnverified != 1 || result.Totals.AveragePriceReturn != nil {
		t.Fatalf("failed event feed treated as empty: %d %+v", r.Code, result.Totals)
	}
	archive, err := s.patternValidations.Load(result.Report.RunID)
	if err != nil || archive.Inputs[0].Actions.Error == "" || archive.Samples[0].Outcomes[0].PriceReturn == nil {
		t.Fatalf("failure evidence or raw outcome missing: %v", err)
	}
	r = validationHTTPRequest(s, http.MethodGet, "/api/pattern-validation?id="+archive.RunID+"&action=unverified", "")
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || result.Total != 1 {
		t.Fatal("unverified archive filter failed")
	}
	archive.Version = "pattern-validation-v2"
	archive.ActionCoverage = nil
	for i := range archive.Inputs {
		archive.Inputs[i].Actions = nil
	}
	for i := range archive.Samples {
		archive.Samples[i].ActionCheck = nil
		for j := range archive.Samples[i].Outcomes {
			archive.Samples[i].Outcomes[j].ActionCheck = nil
		}
	}
	legacy, err := s.patternValidations.Save(archive)
	if err != nil {
		t.Fatal(err)
	}
	r = validationHTTPRequest(s, http.MethodGet, "/api/pattern-validation?id="+legacy.RunID+"&action=legacy", "")
	result = strategy.PatternValidationView{}
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || result.Total != 1 || result.Totals.Mature != 1 || result.Report.ActionCoverage != nil {
		t.Fatal("legacy archive was reinterpreted as audited")
	}
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
	history := s.history.(patternHistoryFixture)
	// A gap after all measured windows exercises diagnostics without changing returns.
	bars := history["sh600007"]
	history["sh600007"] = append(bars[:103:103], bars[104:]...)
	s.patternHistory = &validationRangeFixture{patternHistoryFixture: history}
	event := domain.CorporateAction{ID: "browser-dividend", Symbol: "sh600004", ExDate: history["sh600004"][85].Date, NoticeDate: history["sh600004"][80].Date, Kinds: []string{"cash-dividend"}, Description: "10派2.50元（隔离数据）", Source: "隔离分红送转数据", SourceURL: "https://data.eastmoney.com/yjfp/detail/600004.html"}
	event.Terms = validationTerms(2.5, 0, 0)
	shareEvent := domain.CorporateAction{ID: "browser-share", Symbol: "sh600007", ExDate: history["sh600007"][85].Date, Kinds: []string{"capitalization"}, Description: "10转5股（隔离数据）", Source: "隔离分红送转数据", Terms: validationTerms(0, 0, 5)}
	for i := range history["sh600007"] {
		bar := &history["sh600007"][i]
		if bar.Date >= shareEvent.ExDate {
			bar.Open /= 1.5
			bar.High /= 1.5
			bar.Low /= 1.5
			bar.Close /= 1.5
			bar.Volume *= 1.5
		}
	}
	s.patternActions = validationActionFixture{Events: map[string][]domain.CorporateAction{"sh600004": {event}, "sh600007": {shareEvent}}}
	previous := history["sh600004"][84].Close
	s.patternFactors = validationFactorFixture{Date: event.ExDate, Ratio: previous / (previous - .25), ShareDate: shareEvent.ExDate}
	if err := http.ListenAndServe(address, s.Handler()); err != nil {
		t.Fatal(err)
	}
}
