package web

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func shadowComparisonFixture(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	at := time.Date(2026, 10, 15, 16, 0, 0, 0, realtimeWebLocation)
	set := NewShadowTrialSet(time.Date(2026, 10, 12, 9, 0, 0, 0, realtimeWebLocation))
	set.Hash = "comparison-fixture"
	store := storage.NewShadowDailyReviewStore(filepath.Join(root, "reviews"))
	s := NewServer(resolverStub{}, nil, nil, nil, "", WithShadowTrials(set, filepath.Join(root, "accounts")), WithShadowDailyReviews(store, storage.NewRealtimeSignalStore(filepath.Join(root, "scans"))))
	s.shadowEvaluator = paper.NewEvaluator(nil)
	s.now = func() time.Time { return at }
	calendar := struct {
		FetchedAt time.Time           `json:"fetched_at"`
		Bars      []map[string]string `json:"bars"`
	}{FetchedAt: at}
	for _, date := range []string{"2026-10-09", "2026-10-12", "2026-10-13", "2026-10-14", "2026-10-15"} {
		calendar.Bars = append(calendar.Bars, map[string]string{"symbol": "sh000300", "date": date})
		if date < "2026-10-12" {
			continue
		}
		day, _ := time.ParseInLocation(time.DateOnly, date, realtimeWebLocation)
		for _, profile := range s.shadowProfiles {
			report := paper.Report{AsOf: date, GeneratedAt: day.Add(16 * time.Hour), Config: profile.Config, ConfigFingerprint: paper.ConfigFingerprint(profile.Config), EngineVersion: paper.ShadowEngineVersion, CheckpointPhase: paper.CheckpointClose, InitialCash: 1_000_000, RemainingCash: 1_000_000, TotalEquity: 1_000_000}
			if err := profile.Archive.Save(report); err != nil {
				t.Fatal(err)
			}
			if err := store.Capture(profile.ID, profile.TrialHash, report, report.GeneratedAt); err != nil {
				t.Fatal(err)
			}
		}
	}
	data, _ := json.Marshal(calendar)
	s.shadowComparisonCalendarFile = filepath.Join(root, "calendar.json")
	if err := os.WriteFile(s.shadowComparisonCalendarFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return s, root
}

func readComparison(t *testing.T, s *Server, query string) shadowComparisonResponse {
	t.Helper()
	response := validationHTTPRequest(s, http.MethodGet, "/api/strategy/shadow-comparison"+query, "")
	if response.Code != http.StatusOK {
		t.Fatalf("response %d: %s", response.Code, response.Body.String())
	}
	var report shadowComparisonResponse
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestShadowComparisonUsesSealedDatesAndPairedBaselines(t *testing.T) {
	s, root := shadowComparisonFixture(t)
	file := filepath.Join(root, "accounts", "trial-balanced.json")
	before, _ := os.ReadFile(file)
	report := readComparison(t, s, "")
	if !report.CalendarKnown || len(report.Rows) != 6 {
		t.Fatalf("invalid comparison: %+v", report)
	}
	for _, row := range report.Rows {
		if row.SnapshotDays != 4 || row.ExpectedDays != 4 || row.Return == nil || *row.Return != 0 || row.MaxDrawdown == nil || *row.MaxDrawdown != 0 || row.Activity == nil || row.Activity.ClosedTrades != 0 || row.Activity.WinRate != nil {
			t.Fatalf("incorrect metrics: %+v", row)
		}
		if row.BaselineID != "" && (row.ReturnDifference == nil || *row.ReturnDifference != 0) {
			t.Fatal("missing baseline comparison")
		}
	}
	after, _ := os.ReadFile(file)
	if string(before) != string(after) {
		t.Fatal("read mutated ledger")
	}
	one := readComparison(t, s, "?start=2026-10-15&end=2026-10-15")
	for _, row := range one.Rows {
		if row.SnapshotDays != 1 || row.MaxDrawdown != nil {
			t.Fatal("single close presented as measured drawdown")
		}
	}
	r := validationHTTPRequest(s, http.MethodGet, "/api/strategy/shadow-comparison?download=1", "")
	if !strings.Contains(r.Header().Get("Content-Disposition"), "shadow-comparison-") {
		t.Fatal("missing download header")
	}
}

func TestShadowComparisonDoesNotBridgeMissingDaysOrUnknownCalendar(t *testing.T) {
	s, root := shadowComparisonFixture(t)
	if err := os.Remove(filepath.Join(root, "reviews", "2026-10-13", "trial-balanced-r0.json")); err != nil {
		t.Fatal(err)
	}
	report := readComparison(t, s, "")
	for _, row := range report.Rows {
		if row.ID == "trial-balanced" && (row.SnapshotDays != 3 || row.MaxDrawdown != nil || len(row.MissingDates) != 1 || row.Points[1].Equity != nil) {
			t.Fatalf("missing date hidden: %+v", row)
		}
	}
	s.shadowComparisonCalendarFile = ""
	report = readComparison(t, s, "")
	if report.CalendarKnown || len(report.Warnings) == 0 {
		t.Fatal("missing calendar silently guessed")
	}
	for _, row := range report.Rows {
		if row.MaxDrawdown != nil {
			t.Fatal("unknown date coverage used for drawdown")
		}
	}
}

func TestShadowComparisonRejectsDriftRevisionsAndInvalidDates(t *testing.T) {
	s, _ := shadowComparisonFixture(t)
	archive := s.shadowProfiles["trial-balanced-no-add"].Archive
	ledger, _ := archive.Load()
	ledger.LedgerRevision++
	if err := archive.Save(ledger); err != nil {
		t.Fatal(err)
	}
	response := readComparison(t, s, "")
	for _, row := range response.Rows {
		if row.ID == "trial-balanced-no-add" && (row.Return != nil || row.ReturnDifference != nil || row.MaxDrawdown != nil) {
			t.Fatal("obsolete snapshots remained comparable")
		}
	}
	for _, query := range []string{"?start=bad", "?start=2026-10-16&end=2026-10-15", "?end=2026-10-16", "?start=2024-01-01"} {
		if r := validationHTTPRequest(s, http.MethodGet, "/api/strategy/shadow-comparison"+query, ""); r.Code != 400 {
			t.Fatalf("invalid range accepted: %s", query)
		}
	}
	if r := validationHTTPRequest(s, http.MethodPost, "/api/strategy/shadow-comparison", ""); r.Code != 405 {
		t.Fatal("write method accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.buildShadowComparison(ctx, "2026-10-12", "2026-10-15", s.currentTime()); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestComparisonDrawdownUsesRunningPeak(t *testing.T) {
	points := []shadowComparisonPoint{}
	for _, value := range []float64{110, 99, 105} {
		equity := value
		points = append(points, shadowComparisonPoint{Equity: &equity})
	}
	if value := comparisonDrawdown(points, 100); math.Abs(value-10) > 1e-8 {
		t.Fatalf("drawdown=%v", value)
	}
	first := 80.0
	if value := comparisonDrawdown([]shadowComparisonPoint{{Equity: &first}}, 100); value != 20 {
		t.Fatalf("initial capital peak omitted: %v", value)
	}
}

func TestShadowComparisonBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_SHADOW_COMPARISON_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	s, root := shadowComparisonFixture(t)
	if err := os.Remove(filepath.Join(root, "reviews", "2026-10-13", "trial-balanced-bounded-r0.json")); err != nil {
		t.Fatal(err)
	}
	if err := http.ListenAndServe(address, s.Handler()); err != nil {
		t.Fatal(err)
	}
}
