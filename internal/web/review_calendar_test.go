package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type calendarShadowStore struct {
	report       paper.Report
	loads, saves int
}

func (s *calendarShadowStore) Load() (paper.Report, error) { s.loads++; return s.report, nil }
func (s *calendarShadowStore) Save(paper.Report) error     { s.saves++; return nil }

func calendarServerFixture(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	s := chartServerFixture(filepath.Join(root, "plans"))
	reviews := storage.NewTradePlanReviewStore(filepath.Join(root, "reviews"))
	s.planReviews = reviews
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, 9, 22, 18, 0, 0, 0, location)
	s.now = func() time.Time { return now }
	for index, outcome := range []float64{2, -1, 0} {
		plan := domain.TradePlan{ID: strings.Repeat(string(rune('a'+index)), 64), Version: 1, Symbol: "sh600519", CreatedAt: time.Date(2026, 9, 16+index, 10, 0, 0, 0, location), ExpiresOn: "2026-09-30",
			Analysis:  domain.ChartAnalysis{Symbol: "sh600519", Fingerprint: strings.Repeat("f", 64), DataDate: "2026-09-18"},
			Structure: domain.ChartStructure{ID: "range-breakout", Name: "区间突破", Plan: &domain.ChartPlanLevels{EntryLow: 100, EntryHigh: 101, Invalidation: 98, Target2: 106}}}
		if _, _, err := s.tradePlans.Save(plan); err != nil {
			t.Fatal(err)
		}
		entry, exit := 100.0, 100+outcome*2
		input := domain.TradePlanReviewRevision{ExecutionStatus: "followed", Discipline: "followed", ActualEntry: &entry, ActualExit: &exit,
			EntryAt: time.Date(2026, 9, 18, 10, 0, 0, 0, location), ExitAt: time.Date(2026, 9, 21, 14+index%2, 0, 0, 0, location), Tags: []string{"放量"}}
		if index == 1 {
			input.ExecutionStatus = "deviated"
			input.Discipline = "deviated"
		}
		if index == 2 {
			input.EntryAt = time.Time{}
			input.ExitAt = time.Time{}
		}
		if _, err := reviews.Update(plan, input, now); err != nil {
			t.Fatal(err)
		}
	}
	s.shadowProfiles = map[string]shadowExecutionProfile{
		"balanced":     {ID: "balanced", Name: "均衡型", Archive: &calendarShadowStore{report: paper.Report{AsOf: "2026-09-22", CheckpointPhase: paper.CheckpointClose, Trades: []paper.ShadowTrade{{ID: "t1", Symbol: "sh600519", Name: "测试股份", EntryDate: "2026-09-18", ExitDate: "2026-09-21", ExitTime: "2026-09-21 10:00:00", Quantity: 100, EntryPrice: 100, ExitPrice: 101, TotalFee: 12, NetProfit: 88}}}}},
		"conservative": {ID: "conservative", Name: "稳健型", Archive: &calendarShadowStore{report: paper.Report{AsOf: "2026-09-22", CheckpointPhase: paper.CheckpointClose, Trades: []paper.ShadowTrade{{ID: "t2", Symbol: "sh600519", Name: "测试股份", EntryDate: "2026-09-18", ExitDate: "2026-09-21", Quantity: 100, EntryPrice: 100, ExitPrice: 99, TotalFee: 10, NetProfit: -110}}}}},
	}
	s.shadowEvaluator = &shadowAnalyzerStub{}
	for _, profile := range s.shadowProfiles {
		store := profile.Archive.(*calendarShadowStore)
		store.report.GeneratedAt = now
		store.report.InitialCash = 100000
		store.report.RealizedProfit = store.report.Trades[0].NetProfit
		store.report.TotalProfit = store.report.RealizedProfit
		store.report.TotalEquity = 100000 + store.report.RealizedProfit
	}
	return s
}

func TestReviewCalendarReadOnlyAndAccountIsolation(t *testing.T) {
	s := calendarServerFixture(t)
	for _, source := range []string{"manual", "shadow&profile=balanced", "shadow&profile=conservative"} {
		recorder := httptest.NewRecorder()
		s.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/review-calendar?month=2026-09&source="+source, nil))
		var response struct {
			Report domain.ReviewCalendarReport `json:"report"`
		}
		if recorder.Code != 200 || json.Unmarshal(recorder.Body.Bytes(), &response) != nil {
			t.Fatalf("bad response: %s", recorder.Body.String())
		}
		if source == "manual" {
			if response.Report.Totals.Exits != 2 || *response.Report.Totals.AverageR != .5 || response.Report.Totals.NetProfit != nil || len(response.Report.Undated) != 2 {
				t.Fatalf("manual totals: %+v", response.Report)
			}
		} else {
			want := 88.0
			if strings.Contains(source, "conservative") {
				want = -110
			}
			if response.Report.Totals.AverageR != nil || response.Report.Totals.NetProfit == nil || *response.Report.Totals.NetProfit != want {
				t.Fatalf("account mixed: %+v", response.Report.Totals)
			}
		}
	}
	for _, profile := range s.shadowProfiles {
		if profile.Archive.(*calendarShadowStore).saves != 0 || profile.Archive.(*calendarShadowStore).loads != 1 {
			t.Fatal("calendar mutated or loaded unrelated account")
		}
	}
	for _, path := range []string{"/api/review-calendar?month=2026-13", "/api/review-calendar?source=unknown", "/api/review-calendar?source=shadow&profile=missing"} {
		r := httptest.NewRecorder()
		s.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		if r.Code != 400 {
			t.Fatalf("invalid request accepted: %s", path)
		}
	}
}

func TestReviewCalendarBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_CALENDAR_TEST_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	s := calendarServerFixture(t)
	if err := http.ListenAndServe(address, s.Handler()); err != nil {
		t.Fatal(err)
	}
}
