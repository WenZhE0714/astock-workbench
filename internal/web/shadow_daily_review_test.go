package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func shadowDailyFixture(t *testing.T) (*Server, *forwardClock, *storage.RealtimeSignalStore, string) {
	t.Helper()
	s, _, clock, _, _ := forwardServerFixture(t)
	root := t.TempDir()
	WithShadowExecution(paper.NewEvaluator(nil), storage.NewShadowStore(filepath.Join(root, "balanced.json")))(s)
	set := NewShadowTrialSet(clock.Now().Add(-30 * time.Minute))
	set.Hash = "daily-fixture"
	WithShadowTrials(set, filepath.Join(root, "trials"))(s)
	for _, profile := range s.shadowProfiles {
		cfg := profile.Config
		report := paper.Report{EngineVersion: paper.ShadowEngineVersion, Config: cfg, ConfigFingerprint: paper.ConfigFingerprint(cfg), AsOf: clock.Now().Format(time.DateOnly), GeneratedAt: clock.Now(), CheckpointPhase: paper.CheckpointOpen, InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash, TotalEquity: cfg.InitialCash}
		if err := profile.Archive.Save(report); err != nil {
			t.Fatal(err)
		}
	}
	scans := storage.NewRealtimeSignalStore(filepath.Join(root, "signals"))
	WithShadowDailyReviews(storage.NewShadowDailyReviewStore(filepath.Join(root, "reviews")), scans)(s)
	for _, offset := range []time.Duration{-10 * time.Minute, -10 * time.Minute, -9 * time.Minute, -24 * time.Hour} {
		at := clock.Now().Add(offset)
		scan := realtime.ScanResult{GeneratedAt: at, MarketState: realtime.MarketStateTrading, TradingDate: at.Format(time.DateOnly), TradingDay: true, Signals: []realtime.Signal{{ID: at.Format(time.RFC3339), Symbol: "sh600000", Name: "测试证券", AsOf: at, State: realtime.StateWeak, Score: 70, RiskAdjustedScore: 42, RiskMultiplier: .6, MarketRegime: "熊市", Monster: realtime.MonsterRadar{Score: 100, QuoteFresh: true}}}}
		if err := scans.Append(scan); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.captureShadowDailyReviews(clock.Now()); err != nil {
		t.Fatal(err)
	}
	return s, clock, scans, root
}

func readDailyResponse(t *testing.T, s *Server, url string) shadowDailyResponse {
	t.Helper()
	r := validationHTTPRequest(s, http.MethodGet, url, "")
	if r.Code != 200 {
		t.Fatalf("response %d: %s", r.Code, r.Body.String())
	}
	var result shadowDailyResponse
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestShadowDailyReviewCountsAndCacheInvalidation(t *testing.T) {
	s, clock, scans, root := shadowDailyFixture(t)
	before, _ := os.ReadFile(filepath.Join(root, "balanced.json"))
	response := readDailyResponse(t, s, "/api/strategy/shadow-daily")
	if response.Source.Records != 3 || len(response.Rows) != 7 {
		t.Fatalf("wrong response: %+v", response)
	}
	for _, row := range response.Rows {
		if row.Scans.Scans != 2 || row.Scans.Observations != 2 || row.Scans.Eligible != 0 || row.Scans.DuplicateScans != 1 || row.Ledger == nil || row.Ledger.OrderCount != 0 || row.ZeroTradeReason == "" {
			t.Fatalf("lost no-trade evidence: %+v", row)
		}
		if row.Snapshot == nil || row.Snapshot.Sealed {
			t.Fatal("live snapshot unavailable or incorrectly sealed")
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, "balanced.json"))
	if string(before) != string(after) {
		t.Fatal("GET changed the account")
	}
	at := clock.Now().Add(-time.Minute)
	if err := scans.Append(realtime.ScanResult{GeneratedAt: at, MarketState: realtime.MarketStateTrading, Signals: []realtime.Signal{{Symbol: "sh600000", AsOf: at, State: realtime.StateWatching, Score: 80}}}); err != nil {
		t.Fatal(err)
	}
	response = readDailyResponse(t, s, "/api/strategy/shadow-daily")
	if response.Source.Records != 4 || response.Rows[0].Scans.Eligible != 1 {
		t.Fatal("cached counts ignored archive append")
	}
	r := validationHTTPRequest(s, http.MethodGet, "/api/strategy/shadow-daily?download=1", "")
	if !strings.Contains(r.Header().Get("Content-Disposition"), "shadow-review-") {
		t.Fatal("export header missing")
	}
}

func TestShadowDailyReviewHistoricalIsolationAndMissingSnapshots(t *testing.T) {
	s, clock, _, _ := shadowDailyFixture(t)
	yesterday := clock.Now().AddDate(0, 0, -1).Format(time.DateOnly)
	response := readDailyResponse(t, s, "/api/strategy/shadow-daily?date="+yesterday)
	for _, row := range response.Rows {
		if row.Snapshot != nil || row.ReturnDifference != nil {
			t.Fatal("current equity leaked into historical date")
		}
		if row.TrialHash != "" && (row.Scans.Observations != 0 || row.RealizedDifference != nil) {
			t.Fatal("pre-start trial counted history")
		}
	}
	missing := clock.Now().AddDate(0, 0, -2).Format(time.DateOnly)
	response = readDailyResponse(t, s, "/api/strategy/shadow-daily?date="+missing)
	if response.Source.Available || len(response.Warnings) == 0 {
		t.Fatal("missing archive reported as zero observations")
	}
	for _, url := range []string{"/api/strategy/shadow-daily?date=../bad", "/api/strategy/shadow-daily?date=" + clock.Now().AddDate(0, 0, 1).Format(time.DateOnly)} {
		if r := validationHTTPRequest(s, http.MethodGet, url, ""); r.Code != 400 {
			t.Fatalf("invalid date accepted: %s", url)
		}
	}
	if r := validationHTTPRequest(s, http.MethodPost, "/api/strategy/shadow-daily", ""); r.Code != 405 {
		t.Fatal("daily GET endpoint accepted write")
	}
}

func TestShadowDailyReviewSuppressesMismatchedLedgerRevision(t *testing.T) {
	s, _, _, _ := shadowDailyFixture(t)
	archive := s.shadowProfiles["trial-balanced-no-add"].Archive
	report, _ := archive.Load()
	report.LedgerRevision++
	if err := archive.Save(report); err != nil {
		t.Fatal(err)
	}
	response := readDailyResponse(t, s, "/api/strategy/shadow-daily")
	for _, row := range response.Rows {
		if row.ID == "trial-balanced-no-add" && row.ReturnDifference != nil {
			t.Fatal("obsolete valuation used after correction")
		}
	}
}

func TestShadowDailyReviewDoesNotCompareDriftedTrialConfig(t *testing.T) {
	s, _, _, _ := shadowDailyFixture(t)
	archive := s.shadowProfiles["trial-balanced-no-add"].Archive
	report, _ := archive.Load()
	report.Config.DisableSignalAdditions = false
	report.ConfigFingerprint = paper.ConfigFingerprint(report.Config)
	if err := archive.Save(report); err != nil {
		t.Fatal(err)
	}
	response := readDailyResponse(t, s, "/api/strategy/shadow-daily")
	if len(response.Warnings) == 0 {
		t.Fatal("configuration drift not surfaced")
	}
	for _, row := range response.Rows {
		if row.ID == "trial-balanced-no-add" && (row.ReturnDifference != nil || row.RealizedDifference != nil) {
			t.Fatal("drifted account compared with frozen baseline")
		}
	}
}

func TestShadowDailyReviewIgnoresFutureScansAndClockRollback(t *testing.T) {
	s, clock, scans, _ := shadowDailyFixture(t)
	at := clock.Now().Add(time.Hour)
	if err := scans.Append(realtime.ScanResult{GeneratedAt: at, MarketState: realtime.MarketStateTrading, Signals: []realtime.Signal{{Symbol: "sh600000", AsOf: at, State: realtime.StateWatching, Score: 80}}}); err != nil {
		t.Fatal(err)
	}
	response := readDailyResponse(t, s, "/api/strategy/shadow-daily")
	if response.Source.FutureRecords != 1 || response.Rows[0].Scans.Eligible != 0 {
		t.Fatal("future scan leaked")
	}
	clock.Set(at)
	response = readDailyResponse(t, s, "/api/strategy/shadow-daily")
	if response.Source.FutureRecords != 0 || response.Rows[0].Scans.Eligible != 1 {
		t.Fatal("future scan not included once its time arrived")
	}
	clock.Set(at.Add(-time.Hour))
	response = readDailyResponse(t, s, "/api/strategy/shadow-daily")
	if response.Source.FutureRecords != 1 || response.Rows[0].Scans.Eligible != 0 {
		t.Fatal("cache ignored clock rollback")
	}
}

func TestShadowDailyBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_SHADOW_DAILY_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	s, _, _, _ := shadowDailyFixture(t)
	if err := http.ListenAndServe(address, s.Handler()); err != nil {
		t.Fatal(err)
	}
}
