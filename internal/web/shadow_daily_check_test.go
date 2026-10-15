package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func dailyCheckFixture() shadowDailyResponse {
	at := time.Date(2026, 10, 15, 16, 0, 0, 0, realtimeWebLocation)
	zero, matches := 0.0, true
	return shadowDailyResponse{
		Date: "2026-10-15", Today: "2026-10-15", GeneratedAt: at,
		Source: storage.RealtimeDayRead{Available: true, Records: 3, Signature: "source-v1"},
		Runtime: AutomationStatus{Enabled: true, TaskStates: map[string]storage.AutomationTaskState{
			automationTaskShadow:   {Status: "waiting", LastSuccessAt: at.Add(-time.Minute)},
			automationTaskOutcomes: {Status: "waiting", Detail: "等待16:05日K重试", NextRunAt: at.Add(5 * time.Minute)},
		}},
		Rows: []shadowDailyRow{{
			ID: "trial-balanced-no-add", Name: "均衡不加仓", BaselineID: "trial-balanced", TrialHash: "frozen", ConfigHash: "config", FrozenConfigMatches: &matches,
			TradingStartAt:     at.AddDate(0, 0, -1),
			Scans:              paper.ShadowScanReview{Scans: 2, DuplicateScans: 1, Observations: 4, Gates: []paper.ShadowGateCount{{Code: "risk", Count: 4}}},
			Ledger:             &paper.ShadowDailyReview{Date: "2026-10-15"},
			Snapshot:           &storage.ShadowDailySnapshot{Date: "2026-10-15", ProfileID: "trial-balanced-no-add", TrialHash: "frozen", ConfigHash: "config", GeneratedAt: at.Add(-30 * time.Minute), Sealed: true, Fresh: true, Equity: 1_000_000, Cash: 1_000_000},
			RealizedDifference: &zero, ReturnDifference: &zero,
		}},
	}
}

func checkHasIssue(check paper.ShadowDailyCheck, code string) bool {
	for _, issue := range check.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func TestDailyCheckSeparatesSealedAccountsFromOutcomeRetry(t *testing.T) {
	response := dailyCheckFixture()
	check := checkShadowDaily(response)
	if check.Status != "pending" || check.AccountStatus != "passed" || check.SealedAccounts != 1 || check.Orders != 0 || check.TradedAccounts != 0 || !checkHasIssue(check, "outcomes-pending") {
		t.Fatalf("zero trading or retry misclassified: %+v", check)
	}
	task := response.Runtime.TaskStates[automationTaskOutcomes]
	task.LastSuccessAt = response.GeneratedAt.Add(-time.Minute)
	response.Runtime.TaskStates[automationTaskOutcomes] = task
	if check := checkShadowDaily(response); check.Status != "passed" {
		t.Fatalf("completed close not accepted: %+v", check)
	}
	response.Runtime.TaskStates[automationTaskOutcomes] = storage.AutomationTaskState{Status: "waiting", NextRunAt: response.GeneratedAt.Add(-time.Minute), Detail: "日K未就绪，验证窗口已结束"}
	if check := checkShadowDaily(response); check.Status != "warning" || check.AccountStatus != "passed" {
		t.Fatalf("expired retry not surfaced: %+v", check)
	}
}

func TestDailyCheckRejectsIncompleteOrInconsistentClose(t *testing.T) {
	for _, test := range []struct {
		name, code string
		change     func(*shadowDailyResponse)
	}{
		{"missing snapshot", "snapshot-missing", func(r *shadowDailyResponse) { r.Rows[0].Snapshot = nil }},
		{"unsealed snapshot", "snapshot-unsealed", func(r *shadowDailyResponse) { r.Rows[0].Snapshot.Sealed = false }},
		{"revised ledger", "snapshot-invalid", func(r *shadowDailyResponse) { r.Rows[0].LedgerRevision++ }},
		{"wrong trial", "snapshot-invalid", func(r *shadowDailyResponse) { r.Rows[0].Snapshot.TrialHash = "different" }},
		{"early close", "snapshot-invalid", func(r *shadowDailyResponse) { r.Rows[0].Snapshot.GeneratedAt = r.GeneratedAt.Add(-2 * time.Hour) }},
		{"config drift", "frozen-config-drift", func(r *shadowDailyResponse) { *r.Rows[0].FrozenConfigMatches = false }},
		{"count mismatch", "scan-count-mismatch", func(r *shadowDailyResponse) { r.Rows[0].Scans.Observations++ }},
		{"equity mismatch", "equity-mismatch", func(r *shadowDailyResponse) { r.Rows[0].Snapshot.Equity++ }},
		{"missing comparison", "comparison-unavailable", func(r *shadowDailyResponse) { r.Rows[0].ReturnDifference = nil }},
		{"missing ledger", "ledger-missing", func(r *shadowDailyResponse) { r.Rows[0].Ledger = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := dailyCheckFixture()
			test.change(&response)
			check := checkShadowDaily(response)
			if check.AccountStatus != "failed" || !checkHasIssue(check, test.code) {
				t.Fatalf("invalid close passed: %+v", check)
			}
		})
	}
}

func TestDailyCheckHistoricalAndPreStartIsolation(t *testing.T) {
	response := dailyCheckFixture()
	response.Today = "2026-10-16"
	response.Runtime.Enabled = false
	if check := checkShadowDaily(response); check.Status != "passed" {
		t.Fatalf("current runtime leaked into history: %+v", check)
	}
	response.Rows[0].TradingStartAt = response.GeneratedAt.Add(time.Hour)
	response.Rows[0].Snapshot, response.Rows[0].Ledger = nil, nil
	check := checkShadowDaily(response)
	if check.Status != "pending" || check.Accounts != 0 || check.NotStarted != 1 || checkHasIssue(check, "snapshot-missing") {
		t.Fatalf("pre-start account required a snapshot: %+v", check)
	}
	response = dailyCheckFixture()
	response.GeneratedAt = response.GeneratedAt.Add(-2 * time.Hour)
	response.Rows[0].Snapshot = nil
	if check := checkShadowDaily(response); check.AccountStatus != "pending" {
		t.Fatalf("intraday missing snapshot treated as failed close: %+v", check)
	}
}

func TestDailyCheckPersistsAutomaticResultAndGETStaysReadOnly(t *testing.T) {
	s, clock, _, root := shadowDailyFixture(t)
	checkPath := filepath.Join(root, "reviews", "checks", clock.Now().Format(time.DateOnly)+".json")
	readDailyResponse(t, s, "/api/strategy/shadow-daily")
	if _, err := os.Stat(checkPath); !os.IsNotExist(err) {
		t.Fatal("GET wrote the automatic check")
	}
	if err := s.saveShadowDailyCheck(context.Background(), clock.Now()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(checkPath)
	if err != nil {
		t.Fatal(err)
	}
	var check paper.ShadowDailyCheck
	if err := json.Unmarshal(data, &check); err != nil || check.Date == "" || check.Status == "" {
		t.Fatalf("invalid saved check: %s %v", data, err)
	}
	if err := os.WriteFile(filepath.Join(root, "reviews", check.Date, "broken.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.saveShadowDailyCheck(context.Background(), clock.Now().Add(time.Minute)); err == nil {
		t.Fatal("corrupt archive did not fail the check")
	}
	data, _ = os.ReadFile(checkPath)
	if err := json.Unmarshal(data, &check); err != nil || check.Status != "failed" || !checkHasIssue(check, "review-unreadable") {
		t.Fatalf("old success retained after read failure: %s", data)
	}
}

func TestDailyCheckRunsFromAutomationAndReportsWriteFailure(t *testing.T) {
	root := t.TempDir()
	watchlist := filepath.Join(root, "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 20, 15, 20)
	s := NewServer(resolverStub{}, nil, nil, nil, "", WithWatchlist(watchlist),
		WithRealtimeStrategy(realtimeScannerStub{result: realtime.ScanResult{GeneratedAt: now}}, realtimeArchiveStub{}),
		WithShadowDailyReviews(storage.NewShadowDailyReviewStore(filepath.Join(root, "reviews")), storage.NewRealtimeSignalStore(filepath.Join(root, "signals"))))
	s.now = func() time.Time { return now }
	s.runAutomationCycle(context.Background())
	file := filepath.Join(root, "reviews", "checks", "2026-08-20.json")
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("automatic close check ran before 15:30")
	}
	now = now.Add(20 * time.Minute)
	s.runAutomationCycle(context.Background())
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var check paper.ShadowDailyCheck
	if err := json.Unmarshal(data, &check); err != nil || !checkHasIssue(check, "accounts-missing") || !checkHasIssue(check, "scan-archive-missing") {
		t.Fatalf("missing records reported as passed: %s", data)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0o700); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	s.runAutomationCycle(context.Background())
	if s.automationStatus().LastError == "" || s.shadowDailyError == "" {
		t.Fatal("check write failure was hidden from automation")
	}
}
