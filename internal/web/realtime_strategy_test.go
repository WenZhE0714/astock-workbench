package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type shadowAnalyzerStub struct {
	report paper.Report
	calls  *int
}

func (stub shadowAnalyzerStub) Evaluate(context.Context, []realtime.Signal, paper.Options) (paper.Report, error) {
	if stub.calls != nil {
		(*stub.calls)++
	}
	return stub.report, nil
}

type shadowRealtimeAnalyzerStub struct {
	report        paper.Report
	dailyCalls    *int
	realtimeCalls *int
}

func (stub shadowRealtimeAnalyzerStub) Evaluate(_ context.Context, _ []realtime.Signal, _ paper.Options) (paper.Report, error) {
	if stub.dailyCalls != nil {
		(*stub.dailyCalls)++
	}
	return stub.report, nil
}

func (stub shadowRealtimeAnalyzerStub) AdvanceRealtime(_ context.Context, previous paper.Report, _ []realtime.Signal, options paper.Options) (paper.Report, error) {
	if stub.realtimeCalls != nil {
		(*stub.realtimeCalls)++
	}
	previous.GeneratedAt = options.RealtimeAt
	previous.LastRealtimeAt = options.RealtimeAt.Format("2006-01-02 15:04:05")
	previous.ExecutionMode = paper.ExecutionModeLive
	previous.CheckpointPhase = paper.CheckpointOpen
	previous.AsOf = options.RealtimeAt.Format("2006-01-02")
	previous.EngineVersion = paper.ShadowEngineVersion
	previous.Config = options.Config
	previous.ConfigFingerprint = paper.OptionsFingerprint(options.Config, options.Limit)
	return previous, nil
}

type shadowProfileAnalyzerStub struct {
	configs *[]paper.Config
}

func (stub shadowProfileAnalyzerStub) Evaluate(_ context.Context, _ []realtime.Signal, options paper.Options) (paper.Report, error) {
	if stub.configs != nil {
		*stub.configs = append(*stub.configs, options.Config)
	}
	return paper.Report{
		EngineVersion:     paper.ShadowEngineVersion,
		ConfigFingerprint: paper.OptionsFingerprint(options.Config, options.Limit),
		CheckpointPhase:   paper.CheckpointOpen,
		GeneratedAt:       realtimeWebTime(2026, 8, 21, 10, 15),
		AsOf:              "2026-08-21",
		Config:            options.Config,
		InitialCash:       options.Config.InitialCash,
		RemainingCash:     options.Config.InitialCash,
		TotalEquity:       options.Config.InitialCash,
	}, nil
}

type shadowArchiveStub struct {
	report paper.Report
	saves  int
}

func (stub *shadowArchiveStub) Save(report paper.Report) error {
	stub.report = report
	stub.saves++
	return nil
}
func (stub *shadowArchiveStub) Load() (paper.Report, error) { return stub.report, nil }

type shadowQuoteStub struct {
	quotes []domain.Quote
}

func (stub shadowQuoteStub) Fetch(context.Context, []string) ([]domain.Quote, error) {
	return stub.quotes, nil
}

type quoteClientFunc func(context.Context, []string) ([]domain.Quote, error)

func (fn quoteClientFunc) Fetch(ctx context.Context, symbols []string) ([]domain.Quote, error) {
	return fn(ctx, symbols)
}

type realtimeScannerStub struct {
	result realtime.ScanResult
	calls  *int
}

type calibratableRealtimeScannerStub struct {
	calibration realtime.ScoreCalibration
}

func (stub *calibratableRealtimeScannerStub) Scan(context.Context, []string, bool) (realtime.ScanResult, error) {
	return realtime.ScanResult{}, nil
}

func (stub *calibratableRealtimeScannerStub) SetCalibration(value realtime.ScoreCalibration) {
	stub.calibration = value
}

type realtimeScannerFunc func(context.Context, []string, bool) (realtime.ScanResult, error)

func (fn realtimeScannerFunc) Scan(ctx context.Context, symbols []string, includeLeaders bool) (realtime.ScanResult, error) {
	return fn(ctx, symbols, includeLeaders)
}

type failingRealtimeScannerStub struct{}

func (failingRealtimeScannerStub) Scan(context.Context, []string, bool) (realtime.ScanResult, error) {
	return realtime.ScanResult{}, fmt.Errorf("测试扫描失败")
}

func (stub realtimeScannerStub) Scan(context.Context, []string, bool) (realtime.ScanResult, error) {
	if stub.calls != nil {
		(*stub.calls)++
	}
	return stub.result, nil
}

type realtimeEnrichingScannerStub struct {
	realtimeScannerStub
	enriched    realtime.ScanResult
	enrichCalls *int
}

func (stub realtimeEnrichingScannerStub) EnrichSectors(context.Context, realtime.ScanResult) (realtime.ScanResult, error) {
	if stub.enrichCalls != nil {
		(*stub.enrichCalls)++
	}
	return stub.enriched, nil
}

type realtimeArchiveStub struct {
	items       []realtime.Signal
	latest      realtime.ScanResult
	listCalls   *int
	listLimit   *int
	latestCalls *int
}

type calendarHistoryStub struct {
	bars []domain.DailyBar
}

func (stub calendarHistoryStub) FetchDailyBars(context.Context, string) ([]domain.DailyBar, error) {
	return append([]domain.DailyBar(nil), stub.bars...), nil
}

func (stub realtimeArchiveStub) List(limit int) ([]realtime.Signal, error) {
	if stub.listCalls != nil {
		(*stub.listCalls)++
	}
	if stub.listLimit != nil {
		*stub.listLimit = limit
	}
	return stub.items, nil
}

func (stub realtimeArchiveStub) Latest() (realtime.ScanResult, error) {
	if stub.latestCalls != nil {
		(*stub.latestCalls)++
	}
	return stub.latest, nil
}

type realtimeOutcomeStub struct {
	report        realtime.OutcomeReport
	evaluateCalls *int
	reportCalls   *int
	reportLimit   *int
	options       *realtime.OutcomeOptions
}

type realtimeOutcomeFunc struct {
	evaluate func(context.Context, []realtime.Signal, realtime.OutcomeOptions) (realtime.OutcomeReport, error)
	report   func(int, time.Time) (realtime.OutcomeReport, error)
}

func (fn realtimeOutcomeFunc) Evaluate(ctx context.Context, signals []realtime.Signal, options realtime.OutcomeOptions) (realtime.OutcomeReport, error) {
	return fn.evaluate(ctx, signals, options)
}

func (fn realtimeOutcomeFunc) Report(limit int, now time.Time) (realtime.OutcomeReport, error) {
	if fn.report != nil {
		return fn.report(limit, now)
	}
	return realtime.OutcomeReport{}, nil
}

func (stub realtimeOutcomeStub) Evaluate(_ context.Context, _ []realtime.Signal, options realtime.OutcomeOptions) (realtime.OutcomeReport, error) {
	if stub.evaluateCalls != nil {
		(*stub.evaluateCalls)++
	}
	if stub.options != nil {
		*stub.options = options
	}
	return stub.report, nil
}

func (stub realtimeOutcomeStub) Report(limit int, _ time.Time) (realtime.OutcomeReport, error) {
	if stub.reportCalls != nil {
		(*stub.reportCalls)++
	}
	if stub.reportLimit != nil {
		*stub.reportLimit = limit
	}
	return stub.report, nil
}

func TestRealtimeStrategyEndpointRunsAndReturnsHistory(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 10, 30, 0, 0, time.Local)
	result := realtime.ScanResult{GeneratedAt: now, Universe: "watchlist+leaders", Signals: []realtime.Signal{{ID: "signal", Symbol: "sh600519", Score: 80}}}
	scanCalls := 0
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithWatchlist(watchlist), WithRealtimeStrategy(realtimeScannerStub{result: result, calls: &scanCalls}, realtimeArchiveStub{items: result.Signals}))
	server.now = func() time.Time { return now }

	runRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(runRecorder, httptest.NewRequest(http.MethodPost, "/api/strategy/realtime", nil))
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected run status %d: %s", runRecorder.Code, runRecorder.Body.String())
	}
	var run realtimeStrategyResponse
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.Result == nil || len(run.Result.Signals) != 1 {
		t.Fatalf("unexpected run response: %+v", run)
	}
	if scanCalls != 1 || run.ScanAllowed == nil || !*run.ScanAllowed || run.Frozen == nil || *run.Frozen || run.MarketState != realtime.MarketStateTrading {
		t.Fatalf("unexpected active session response: calls=%d response=%+v", scanCalls, run)
	}

	historyRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(historyRecorder, httptest.NewRequest(http.MethodGet, "/api/strategy/realtime?view=history&limit=10", nil))
	if historyRecorder.Code != http.StatusOK {
		t.Fatalf("unexpected history status %d: %s", historyRecorder.Code, historyRecorder.Body.String())
	}
	var history realtimeStrategyResponse
	if err := json.Unmarshal(historyRecorder.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history.History) != 1 || history.History[0].ID != "signal" {
		t.Fatalf("unexpected history response: %+v", history)
	}
}

func TestRealtimeStrategyScopeIsForwardedToScanner(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 20, 10, 30)
	var includeLeaders []bool
	scanner := realtimeScannerFunc(func(_ context.Context, _ []string, include bool) (realtime.ScanResult, error) {
		includeLeaders = append(includeLeaders, include)
		universe := "watchlist"
		if include {
			universe = "watchlist+leaders"
		}
		return realtime.ScanResult{GeneratedAt: now, Universe: universe}, nil
	})
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithWatchlist(watchlist), WithRealtimeStrategy(scanner, realtimeArchiveStub{}))
	server.now = func() time.Time { return now }

	for _, scope := range []string{"watchlist", "leaders"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/strategy/realtime?scope="+scope, nil)
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("scope %s returned %d: %s", scope, recorder.Code, recorder.Body.String())
		}
	}
	if len(includeLeaders) != 2 || includeLeaders[0] || !includeLeaders[1] {
		t.Fatalf("scope was not forwarded correctly: %#v", includeLeaders)
	}
}

func TestRealtimeStrategyPausesOnKnownExchangeHoliday(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	calendar := []domain.DailyBar{
		{Date: "2026-08-20", Open: 1, Close: 1, High: 1, Low: 1},
		{Date: "2026-08-22", Open: 1, Close: 1, High: 1, Low: 1},
		{Date: "2026-08-24", Open: 1, Close: 1, High: 1, Low: 1},
	}
	calls := 0
	server := NewServer(
		resolverStub{}, nil, calendarHistoryStub{bars: calendar}, nil, "",
		WithWatchlist(watchlist),
		WithRealtimeStrategy(realtimeScannerStub{calls: &calls}, realtimeArchiveStub{}),
	)
	now := realtimeWebTime(2026, 8, 21, 10, 30)
	server.now = func() time.Time { return now }
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/strategy/realtime", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected holiday status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response realtimeStrategyResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || response.MarketState != realtime.MarketStateClosed || response.ScanAllowed == nil || *response.ScanAllowed || response.Frozen == nil || !*response.Frozen {
		t.Fatalf("holiday was not frozen: calls=%d response=%+v", calls, response)
	}
	next, err := time.Parse(time.RFC3339, response.NextScanAt)
	if err != nil || next.Day() != 22 || next.Hour() != 9 || next.Minute() != 15 {
		t.Fatalf("unexpected next exchange session: %q (%v)", response.NextScanAt, err)
	}
}

func TestServerAutomationRunsWithoutBrowserAndExposesStatus(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 20, 10, 30)
	result := realtime.ScanResult{GeneratedAt: now, Universe: "watchlist+leaders", Signals: []realtime.Signal{{ID: "auto", Symbol: "sh600519", Score: 80}}}
	scanCalls, outcomeCalls := 0, 0
	server := NewServer(resolverStub{}, nil, nil, nil, "",
		WithWatchlist(watchlist),
		WithRealtimeStrategy(realtimeScannerStub{result: result, calls: &scanCalls}, realtimeArchiveStub{items: result.Signals}),
		WithRealtimeOutcomes(realtimeOutcomeStub{evaluateCalls: &outcomeCalls}),
	)
	server.now = func() time.Time { return now }
	server.runAutomationCycle(context.Background())
	if scanCalls != 1 || outcomeCalls != 0 {
		t.Fatalf("intraday automation should scan without daily validation: scan=%d outcomes=%d", scanCalls, outcomeCalls)
	}
	status := server.automationStatus()
	if !status.Enabled || status.Running || status.LastSuccessAt.IsZero() || status.LastError != "" {
		t.Fatalf("unexpected automation status: %+v", status)
	}
	if strings.Join(status.TaskOrder, ",") != "scan,outcomes,shadow,research" {
		t.Fatalf("automation task order is not stable: %#v", status.TaskOrder)
	}
	if status.TaskStates[automationTaskScan].Status != "success" || status.TaskStates[automationTaskOutcomes].Status != "waiting" {
		t.Fatalf("automation task state missing: %+v", status.TaskStates)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/strategy/automation", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "交易时段实时扫描") {
		t.Fatalf("automation endpoint failed: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAutomaticOutcomeValidationPlanUsesCloseAndRetryWindows(t *testing.T) {
	beforeClose := realtimeWebTime(2026, 8, 20, 14, 0)
	plan := automaticOutcomeValidationPlan(beforeClose, realtime.MarketSessionAt(beforeClose), time.Time{}, storage.AutomationTaskState{})
	if plan.Run || plan.NextRunAt.Hour() != 15 || plan.NextRunAt.Minute() != 30 || !strings.Contains(plan.Detail, "15:30") {
		t.Fatalf("unexpected pre-close validation plan: %+v", plan)
	}
	primaryNow := realtimeWebTime(2026, 8, 20, 15, 35)
	plan = automaticOutcomeValidationPlan(primaryNow, realtime.MarketSessionAt(primaryNow), time.Time{}, storage.AutomationTaskState{})
	if !plan.Run || plan.RetryAttempt || plan.NextRunAt.Hour() != 16 || plan.NextRunAt.Minute() != 5 {
		t.Fatalf("unexpected primary validation plan: %+v", plan)
	}
	waitingTask := storage.AutomationTaskState{LastAttemptAt: primaryNow}
	between := realtimeWebTime(2026, 8, 20, 15, 50)
	plan = automaticOutcomeValidationPlan(between, realtime.MarketSessionAt(between), time.Time{}, waitingTask)
	if plan.Run || plan.NextRunAt.Hour() != 16 || plan.NextRunAt.Minute() != 5 {
		t.Fatalf("unexpected retry wait plan: %+v", plan)
	}
	retryNow := realtimeWebTime(2026, 8, 20, 16, 6)
	plan = automaticOutcomeValidationPlan(retryNow, realtime.MarketSessionAt(retryNow), time.Time{}, waitingTask)
	if !plan.Run || !plan.RetryAttempt || plan.NextRunAt.Day() != 21 || plan.NextRunAt.Hour() != 15 || plan.NextRunAt.Minute() != 30 {
		t.Fatalf("unexpected retry validation plan: %+v", plan)
	}
	completed := realtimeWebTime(2026, 8, 20, 15, 40)
	plan = automaticOutcomeValidationPlan(retryNow, realtime.MarketSessionAt(retryNow), completed, waitingTask)
	if plan.Run || !strings.Contains(plan.Detail, "已完成") || plan.NextRunAt.Day() != 21 {
		t.Fatalf("completed validation was scheduled twice: %+v", plan)
	}
}

func TestAutomationOutcomeValidationRetriesOnceWhenDailyDataLags(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 20, 15, 35)
	snapshot := realtime.ScanResult{GeneratedAt: realtimeWebTime(2026, 8, 20, 15, 0), Signals: []realtime.Signal{{ID: "signal", Symbol: "sh600519", DataDate: "2026-08-20"}}}
	calls := 0
	evaluator := realtimeOutcomeFunc{evaluate: func(context.Context, []realtime.Signal, realtime.OutcomeOptions) (realtime.OutcomeReport, error) {
		calls++
		dataThrough := "2026-08-19"
		if calls > 1 {
			dataThrough = "2026-08-20"
		}
		return realtime.OutcomeReport{DataThrough: dataThrough}, nil
	}}
	server := NewServer(resolverStub{}, nil, nil, nil, "",
		WithWatchlist(watchlist),
		WithRealtimeStrategy(realtimeScannerStub{}, realtimeArchiveStub{items: snapshot.Signals, latest: snapshot}),
		WithRealtimeOutcomes(evaluator),
	)
	server.now = func() time.Time { return now }
	server.runAutomationCycle(context.Background())
	status := server.automationStatus()
	if calls != 1 || !status.LastOutcomeAt.IsZero() || status.TaskStates[automationTaskOutcomes].Status != "waiting" || !strings.Contains(status.TaskStates[automationTaskOutcomes].Detail, "16:05重试") {
		t.Fatalf("lagging daily data did not schedule one retry: calls=%d status=%+v", calls, status)
	}
	if next := status.TaskStates[automationTaskOutcomes].NextRunAt; next.Hour() != 16 || next.Minute() != 5 {
		t.Fatalf("unexpected retry time: %s", next)
	}
	now = realtimeWebTime(2026, 8, 20, 15, 50)
	server.runAutomationCycle(context.Background())
	if calls != 1 {
		t.Fatalf("validation retried before 16:05: %d", calls)
	}
	now = realtimeWebTime(2026, 8, 20, 16, 6)
	server.runAutomationCycle(context.Background())
	status = server.automationStatus()
	if calls != 2 || !status.LastOutcomeAt.Equal(now) || status.TaskStates[automationTaskOutcomes].Status != "success" {
		t.Fatalf("16:05 retry did not complete validation: calls=%d status=%+v", calls, status)
	}
	if next := status.TaskStates[automationTaskOutcomes].NextRunAt; next.Day() != 21 || next.Hour() != 15 || next.Minute() != 30 {
		t.Fatalf("next validation was not moved to the next trading close: %s", next)
	}
}

func TestAutomationContinuesIndependentJobsWhenManualScanIsBusy(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 20, 10, 30)
	result := realtime.ScanResult{
		GeneratedAt: now,
		Universe:    "watchlist+leaders",
		Signals:     []realtime.Signal{{ID: "cached", Symbol: "sh600519", Score: 80}},
	}
	outcomeCalls := 0
	scanner := realtimeScannerStub{result: result}
	archive := realtimeArchiveStub{items: result.Signals, latest: result}
	server := NewServer(resolverStub{}, nil, nil, nil, "",
		WithWatchlist(watchlist),
		WithRealtimeStrategy(scanner, archive),
		WithRealtimeOutcomes(realtimeOutcomeStub{evaluateCalls: &outcomeCalls}),
	)
	server.now = func() time.Time { return now }
	server.realtimeMu.Lock()
	server.realtimeRunning = true
	server.realtimeCache = result
	server.realtimeMu.Unlock()

	server.runAutomationCycle(context.Background())
	status := server.automationStatus()
	if outcomeCalls != 0 {
		t.Fatalf("intraday scan lock should not trigger daily validation: %d", outcomeCalls)
	}
	if status.TaskStates[automationTaskScan].Status != "busy" {
		t.Fatalf("scan task should remain visibly busy: %+v", status.TaskStates[automationTaskScan])
	}
	if status.TaskStates[automationTaskOutcomes].Status != "waiting" {
		t.Fatalf("validation task should wait for the close window: %+v", status.TaskStates[automationTaskOutcomes])
	}
	server.realtimeMu.Lock()
	server.realtimeRunning = false
	server.realtimeMu.Unlock()
}

func TestAutomationBusyScanDoesNotRunIndependentJobsWithoutCurrentSnapshot(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 20, 10, 30)
	outcomeCalls := 0
	server := NewServer(resolverStub{}, nil, nil, nil, "",
		WithWatchlist(watchlist),
		WithRealtimeStrategy(realtimeScannerStub{}, realtimeArchiveStub{}),
		WithRealtimeOutcomes(realtimeOutcomeStub{evaluateCalls: &outcomeCalls}),
	)
	server.now = func() time.Time { return now }
	server.realtimeMu.Lock()
	server.realtimeRunning = true
	server.realtimeMu.Unlock()

	server.runAutomationCycle(context.Background())
	if outcomeCalls != 0 {
		t.Fatalf("an occupied scanner without a current snapshot must not run outcomes: %d", outcomeCalls)
	}
	status := server.automationStatus()
	if status.TaskStates[automationTaskScan].Status != "busy" || status.TaskStates[automationTaskOutcomes].Status != "waiting" {
		t.Fatalf("unexpected busy/stale task states: %+v", status.TaskStates)
	}
	server.realtimeMu.Lock()
	server.realtimeRunning = false
	server.realtimeMu.Unlock()
}

func TestAutomationUsesCurrentSnapshotWhenFreshScanFails(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 20, 10, 30)
	cached := realtime.ScanResult{
		GeneratedAt: now,
		Universe:    "watchlist+leaders",
		Signals:     []realtime.Signal{{ID: "cached", Symbol: "sh600519", Score: 80}},
	}
	outcomeCalls, shadowCalls := 0, 0
	archive := &shadowArchiveStub{}
	server := NewServer(resolverStub{}, nil, nil, nil, "",
		WithWatchlist(watchlist),
		WithRealtimeStrategy(failingRealtimeScannerStub{}, realtimeArchiveStub{items: cached.Signals, latest: cached}),
		WithRealtimeOutcomes(realtimeOutcomeStub{evaluateCalls: &outcomeCalls}),
		WithShadowExecution(shadowAnalyzerStub{
			report: paper.Report{EngineVersion: paper.ShadowEngineVersion, AsOf: cached.Signals[0].DataDate, Config: paper.DefaultConfig()},
			calls:  &shadowCalls,
		}, archive),
	)
	server.now = func() time.Time { return now }
	server.realtimeMu.Lock()
	server.realtimeCache = cached
	server.realtimeMu.Unlock()

	server.runAutomationCycle(context.Background())
	status := server.automationStatus()
	if outcomeCalls != 0 || shadowCalls != 1 {
		t.Fatalf("current cached snapshot should keep shadow running without intraday validation: outcomes=%d shadow=%d", outcomeCalls, shadowCalls)
	}
	if status.TaskStates[automationTaskScan].Status != "error" {
		t.Fatalf("scan failure was not exposed: %+v", status.TaskStates[automationTaskScan])
	}
	if status.TaskStates[automationTaskOutcomes].Status != "waiting" || status.TaskStates[automationTaskShadow].Status != "success" {
		t.Fatalf("independent task states were unexpected: %+v", status.TaskStates)
	}
	if !strings.Contains(status.LastError, "实时扫描失败") {
		t.Fatalf("scan error was not retained in cycle status: %+v", status)
	}
}

func TestAutomationManualTriggerDoesNotStartDuplicateCycle(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithWatchlist(watchlist), WithRealtimeStrategy(realtimeScannerStub{}, realtimeArchiveStub{}))
	server.now = func() time.Time { return realtimeWebTime(2026, 8, 20, 10, 30) }
	server.realtimeScanner = blockingRealtimeScannerStub{started: started, release: release}
	if !server.beginAutomationCycle() {
		t.Fatal("first automation cycle did not start")
	}
	defer close(release)
	if server.beginAutomationCycle() {
		t.Fatal("duplicate automation cycle started while first was running")
	}
	server.automationMu.Lock()
	server.automationRunning = false
	server.automationMu.Unlock()
}

func TestAutomationRunContextFollowsServerLifetime(t *testing.T) {
	server := NewServer(resolverStub{}, nil, nil, nil, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.automationMu.Lock()
	server.automationCtx = ctx
	server.automationMu.Unlock()
	if got := server.automationRunContext(); got == nil {
		t.Fatal("automation context must not be nil")
	} else {
		cancel()
		select {
		case <-got.Done():
		default:
			t.Fatal("automation context did not receive cancellation")
		}
	}
	server.automationMu.Lock()
	server.automationCtx = nil
	server.automationMu.Unlock()
	if got := server.automationRunContext(); got == nil {
		t.Fatal("missing automation context should fall back to background context")
	}
}

type blockingRealtimeScannerStub struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (stub blockingRealtimeScannerStub) Scan(context.Context, []string, bool) (realtime.ScanResult, error) {
	select {
	case stub.started <- struct{}{}:
	default:
	}
	<-stub.release
	return realtime.ScanResult{}, nil
}

func TestAutomationEndpointCanTriggerImmediateCycle(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 20, 10, 30)
	result := realtime.ScanResult{GeneratedAt: now, Signals: []realtime.Signal{{ID: "manual", Symbol: "sh600519", Score: 80}}}
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithWatchlist(watchlist),
		WithRealtimeStrategy(realtimeScannerStub{result: result}, realtimeArchiveStub{items: result.Signals}),
	)
	server.now = func() time.Time { return now }
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/strategy/automation?action=run", nil))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("unexpected manual automation status: %d %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "自动化任务未初始化") {
		t.Fatalf("manual automation was incorrectly disabled: %s", recorder.Body.String())
	}
}

func TestAutomationAdvancesShadowOnEachNewRealtimeSnapshot(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	calendar := []domain.DailyBar{
		{Date: "2026-08-27", Open: 1, Close: 1, High: 1, Low: 1},
	}
	first := realtime.ScanResult{
		GeneratedAt: realtimeWebTime(2026, 8, 27, 10, 0),
		Signals:     []realtime.Signal{{ID: "first", Symbol: "sh600519", QuoteTime: "2026-08-27 10:00:00", Score: 70}},
	}
	second := first
	second.GeneratedAt = realtimeWebTime(2026, 8, 27, 10, 1)
	second.Signals = []realtime.Signal{{ID: "second", Symbol: "sh600519", QuoteTime: "2026-08-27 10:01:00", Score: 71}}
	snapshot := first
	scanCalls, realtimeCalls := 0, 0
	archive := &shadowArchiveStub{report: paper.Report{
		EngineVersion:     paper.ShadowEngineVersion,
		Config:            paper.DefaultConfig(),
		ConfigFingerprint: paper.OptionsFingerprint(paper.DefaultConfig(), 0),
		AsOf:              "2026-08-27",
		CheckpointPhase:   paper.CheckpointOpen,
		ExecutionMode:     paper.ExecutionModeLive,
		LastRealtimeAt:    "2026-08-27 09:59:00",
		InitialCash:       1_000_000,
		RemainingCash:     1_000_000,
	}}
	scanner := realtimeScannerFunc(func(context.Context, []string, bool) (realtime.ScanResult, error) {
		scanCalls++
		return snapshot, nil
	})
	server := NewServer(
		resolverStub{}, quoteClientFunc(func(context.Context, []string) ([]domain.Quote, error) {
			return []domain.Quote{{Symbol: "sh600519", Current: "10", QuoteTime: snapshot.GeneratedAt.Format("2006-01-02 15:04:05"), Source: "test"}}, nil
		}), calendarHistoryStub{bars: calendar}, nil, "",
		WithWatchlist(watchlist),
		WithRealtimeStrategy(scanner, realtimeArchiveStub{items: snapshot.Signals, latest: snapshot}),
		WithShadowExecution(shadowRealtimeAnalyzerStub{realtimeCalls: &realtimeCalls}, archive),
	)
	server.now = func() time.Time { return snapshot.GeneratedAt }
	server.runAutomationCycle(context.Background())
	if scanCalls != 1 || realtimeCalls != 1 {
		t.Fatalf("first fresh snapshot did not advance shadow: scans=%d realtime=%d", scanCalls, realtimeCalls)
	}
	// A second cycle with the same generated timestamp must be idempotent.
	server.runAutomationCycle(context.Background())
	if realtimeCalls != 1 {
		t.Fatalf("same snapshot advanced shadow twice: %d", realtimeCalls)
	}
	snapshot = second
	server.now = func() time.Time { return snapshot.GeneratedAt }
	server.quoteMu.Lock()
	server.quoteCache = make(map[string]quoteCacheEntry)
	server.quoteMu.Unlock()
	server.runAutomationCycle(context.Background())
	if realtimeCalls != 2 {
		t.Fatalf("new snapshot did not advance shadow: %d", realtimeCalls)
	}
}

func TestAutomationSkipsOutcomeAndShadowForStaleSnapshot(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 20, 10, 30)
	stale := realtime.ScanResult{GeneratedAt: realtimeWebTime(2026, 8, 19, 15, 2), Signals: []realtime.Signal{{ID: "stale"}}}
	outcomeCalls := 0
	server := NewServer(resolverStub{}, nil, nil, nil, "",
		WithWatchlist(watchlist),
		WithRealtimeStrategy(realtimeScannerStub{result: stale}, realtimeArchiveStub{items: stale.Signals, latest: stale}),
		WithRealtimeOutcomes(realtimeOutcomeStub{evaluateCalls: &outcomeCalls}),
	)
	server.now = func() time.Time { return now }
	// A scanner result is returned for this cycle, but the archive's stale
	// snapshot must not make outcome evaluation run against yesterday twice.
	server.runAutomationCycle(context.Background())
	if outcomeCalls != 0 {
		t.Fatalf("stale snapshot should not trigger outcomes: %d", outcomeCalls)
	}
}

func TestAutomationSkipsStaleQuoteDateEvenWhenGeneratedAtIsCurrent(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 20, 10, 30)
	stale := realtime.ScanResult{GeneratedAt: now, Signals: []realtime.Signal{{ID: "stale", QuoteTime: "2026-08-19 15:00:00"}}}
	outcomeCalls := 0
	server := NewServer(resolverStub{}, nil, nil, nil, "",
		WithWatchlist(watchlist),
		WithRealtimeStrategy(realtimeScannerStub{result: stale}, realtimeArchiveStub{items: stale.Signals, latest: stale}),
		WithRealtimeOutcomes(realtimeOutcomeStub{evaluateCalls: &outcomeCalls}),
	)
	server.now = func() time.Time { return now }
	server.runAutomationCycle(context.Background())
	if outcomeCalls != 0 {
		t.Fatalf("stale quote date should not trigger outcomes: %d", outcomeCalls)
	}
}

func TestAutomationDoesNotStartResearchAfterFailedScan(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 24, 10, 30)
	researchCalls := 0
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithWatchlist(watchlist),
		WithRealtimeStrategy(failingRealtimeScannerStub{}, realtimeArchiveStub{}),
		WithAutomaticStrategyResearch(func(context.Context, []string, time.Time) (AutomaticResearchResult, error) {
			researchCalls++
			return AutomaticResearchResult{Ran: true}, nil
		}),
	)
	server.now = func() time.Time { return now }
	server.runAutomationCycle(context.Background())
	if researchCalls != 0 {
		t.Fatalf("research started after failed scan: %d", researchCalls)
	}
	if !strings.Contains(server.automationStatus().LastError, "实时扫描失败") {
		t.Fatalf("scan failure was not retained: %+v", server.automationStatus())
	}
}

func TestAutomaticResearchCutoffUsesLatestCompletedDataDate(t *testing.T) {
	now := realtimeWebTime(2026, 8, 24, 16, 0)
	snapshot := realtime.ScanResult{Signals: []realtime.Signal{
		{DataDate: "2026-08-21"},
		{DataDate: "2026-08-20"},
	}}
	cutoff := automaticResearchCutoff(snapshot, now)
	if cutoff.Format("2006-01-02") != "2026-08-21" {
		t.Fatalf("unexpected data cutoff: %s", cutoff.Format("2006-01-02"))
	}
}

func TestAutomationStateRestoresAfterServerRestart(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "automation", "state.json")
	store := storage.NewAutomationStore(stateFile)
	want := storage.AutomationState{
		LastRunAt:            realtimeWebTime(2026, 8, 24, 16, 0),
		LastSuccessAt:        realtimeWebTime(2026, 8, 24, 16, 1),
		LastOutcomeAt:        realtimeWebTime(2026, 8, 24, 16, 2),
		ResearchAttemptAt:    realtimeWebTime(2026, 8, 24, 16, 3),
		ResearchSuccessAt:    realtimeWebTime(2026, 8, 24, 16, 4),
		ResearchExperimentID: "AUTO-restart",
		ResearchMessage:      "已归档",
	}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithAutomationState(store))
	status := server.automationStatus()
	if !status.LastSuccessAt.Equal(want.LastSuccessAt) || !status.LastOutcomeAt.Equal(want.LastOutcomeAt) || status.ResearchExperimentID != want.ResearchExperimentID || status.ResearchMessage != want.ResearchMessage {
		t.Fatalf("automation state was not restored: %+v", status)
	}
}

func TestAutomationStateDoesNotRestoreTransientRunningState(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "automation", "state.json")
	store := storage.NewAutomationStore(stateFile)
	if err := store.Save(storage.AutomationState{Tasks: map[string]storage.AutomationTaskState{
		automationTaskScan:   {Status: "running", Detail: "旧进程执行中"},
		automationTaskShadow: {Status: "busy", Detail: "旧进程排队"},
	}}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithAutomationState(store))
	status := server.automationStatus()
	if status.TaskStates[automationTaskScan].Status != "waiting" || status.TaskStates[automationTaskShadow].Status != "waiting" {
		t.Fatalf("transient task state leaked across restart: %+v", status.TaskStates)
	}
}

func TestAutomaticResearchRunsOncePerTradingDate(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 24, 16, 0)
	result := realtime.ScanResult{GeneratedAt: now, Signals: []realtime.Signal{{ID: "signal", Symbol: "sh600519", DataDate: "2026-08-24", QuoteTime: "2026-08-24 15:00:00"}}}
	completed := make(chan struct{}, 2)
	calls := 0
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithWatchlist(watchlist), WithAutomaticStrategyResearch(func(_ context.Context, symbols []string, end time.Time) (AutomaticResearchResult, error) {
		calls++
		if len(symbols) != 1 || symbols[0] != "sh600519" || end.Format("2006-01-02") != "2026-08-24" {
			t.Errorf("unexpected automatic research inputs: %v %s", symbols, end.Format("2006-01-02"))
		}
		completed <- struct{}{}
		return AutomaticResearchResult{Ran: true, ExperimentID: "AUTO-once", Message: "完成"}, nil
	}))
	server.now = func() time.Time { return now }
	session := realtime.MarketSessionAt(now)
	server.maybeStartAutomaticResearch(context.Background(), now, session, result)
	server.maybeStartAutomaticResearch(context.Background(), now, session, result)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("automatic research did not start")
	}
	if calls != 1 {
		t.Fatalf("automatic research ran more than once: %d", calls)
	}
	status := server.automationStatus()
	if status.ResearchExperimentID != "AUTO-once" || status.ResearchMessage != "完成" || status.ResearchRunning {
		t.Fatalf("unexpected research status: %+v", status)
	}
}

func TestAutomaticResearchRetriesAfterFailureCooldown(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	baseNow := realtimeWebTime(2026, 8, 24, 16, 0)
	currentNow := baseNow
	completed := make(chan struct{}, 2)
	calls := 0
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithWatchlist(watchlist), WithAutomaticStrategyResearch(func(_ context.Context, _ []string, _ time.Time) (AutomaticResearchResult, error) {
		calls++
		completed <- struct{}{}
		if calls == 1 {
			return AutomaticResearchResult{}, fmt.Errorf("临时数据源失败")
		}
		return AutomaticResearchResult{Ran: true, ExperimentID: "AUTO-retry", Message: "完成"}, nil
	}))
	server.now = func() time.Time { return currentNow }
	snapshot := realtime.ScanResult{GeneratedAt: baseNow, Signals: []realtime.Signal{{ID: "signal", Symbol: "sh600519", DataDate: "2026-08-24", QuoteTime: "2026-08-24 15:00:00"}}}
	session := realtime.MarketSessionAt(baseNow)
	server.maybeStartAutomaticResearch(context.Background(), baseNow, session, snapshot)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("first research attempt did not finish")
	}
	currentNow = baseNow.Add(5 * time.Minute)
	server.maybeStartAutomaticResearch(context.Background(), currentNow, realtime.MarketSessionAt(currentNow), snapshot)
	if calls != 1 {
		t.Fatalf("research retried before cooldown: %d", calls)
	}
	currentNow = baseNow.Add(11 * time.Minute)
	server.maybeStartAutomaticResearch(context.Background(), currentNow, realtime.MarketSessionAt(currentNow), snapshot)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("retry research attempt did not finish")
	}
	if calls != 2 || server.automationStatus().ResearchExperimentID != "AUTO-retry" {
		t.Fatalf("unexpected retry state: calls=%d status=%+v", calls, server.automationStatus())
	}
}

func TestRealtimeStrategyPausesDuringBreakAndAfterClose(t *testing.T) {
	tests := []struct {
		name       string
		now        time.Time
		latest     time.Time
		state      string
		nextHour   int
		nextMinute int
	}{
		{name: "lunch break", now: realtimeWebTime(2026, 8, 20, 12, 0), latest: realtimeWebTime(2026, 8, 20, 11, 29), state: realtime.MarketStateBreak, nextHour: 13},
		{name: "after close", now: realtimeWebTime(2026, 8, 20, 16, 0), latest: realtimeWebTime(2026, 8, 20, 15, 2), state: realtime.MarketStateClosed, nextHour: 9, nextMinute: 15},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			watchlist := filepath.Join(t.TempDir(), "watchlist")
			if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			latest := realtime.ScanResult{GeneratedAt: test.latest, Signals: []realtime.Signal{{ID: "cached", Symbol: "sh600519"}}}
			server := NewServer(
				resolverStub{}, nil, nil, nil, "",
				WithWatchlist(watchlist),
				WithRealtimeStrategy(realtimeScannerStub{calls: &calls}, realtimeArchiveStub{latest: latest}),
			)
			server.now = func() time.Time { return test.now }

			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/strategy/realtime", nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
			}
			var response realtimeStrategyResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if calls != 0 || response.Result == nil || response.Result.Signals[0].ID != "cached" || !response.Cached || response.ScanAllowed == nil || *response.ScanAllowed || response.Frozen == nil || !*response.Frozen || response.MarketState != test.state {
				t.Fatalf("request was not frozen: calls=%d response=%+v", calls, response)
			}
			next, err := time.Parse(time.RFC3339, response.NextScanAt)
			if err != nil || next.Hour() != test.nextHour || next.Minute() != test.nextMinute {
				t.Fatalf("unexpected next scan time %q: %v", response.NextScanAt, err)
			}
		})
	}
}

func TestRealtimeStrategyFinalizesAfterCloseOnlyOnce(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 20, 15, 2)
	calls := 0
	latest := realtime.ScanResult{GeneratedAt: realtimeWebTime(2026, 8, 20, 14, 59), Signals: []realtime.Signal{{ID: "pre-close"}}}
	final := realtime.ScanResult{GeneratedAt: now, MarketState: realtime.MarketStateClosed, Signals: []realtime.Signal{{ID: "final"}}}
	server := NewServer(
		resolverStub{}, nil, nil, nil, "",
		WithWatchlist(watchlist),
		WithRealtimeStrategy(realtimeScannerStub{result: final, calls: &calls}, realtimeArchiveStub{latest: latest}),
	)
	server.now = func() time.Time { return now }

	first := httptest.NewRecorder()
	server.Handler().ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/api/strategy/realtime", nil))
	second := httptest.NewRecorder()
	server.Handler().ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/strategy/realtime", nil))
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("unexpected statuses first=%d second=%d", first.Code, second.Code)
	}
	var firstResponse, secondResponse realtimeStrategyResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstResponse); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondResponse); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || firstResponse.Result == nil || firstResponse.Result.Signals[0].ID != "final" || firstResponse.Cached || secondResponse.Result == nil || secondResponse.Result.Signals[0].ID != "final" || !secondResponse.Cached {
		t.Fatalf("final snapshot was not deduplicated: calls=%d first=%+v second=%+v", calls, firstResponse, secondResponse)
	}
	if firstResponse.ScanAllowed == nil || *firstResponse.ScanAllowed || firstResponse.Frozen == nil || !*firstResponse.Frozen {
		t.Fatalf("final snapshot should immediately freeze: %+v", firstResponse)
	}
}

func TestRealtimeStrategyGETRestoresLatestSnapshotAfterRestart(t *testing.T) {
	latestCalls, scanCalls := 0, 0
	latest := realtime.ScanResult{GeneratedAt: realtimeWebTime(2026, 8, 20, 15, 2), Universe: "watchlist+leaders", Signals: []realtime.Signal{{ID: "restored"}}}
	server := NewServer(
		resolverStub{}, nil, nil, nil, "",
		WithRealtimeStrategy(realtimeScannerStub{calls: &scanCalls}, realtimeArchiveStub{latest: latest, latestCalls: &latestCalls}),
	)
	server.now = func() time.Time { return realtimeWebTime(2026, 8, 20, 16, 0) }

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/strategy/realtime", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response realtimeStrategyResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if latestCalls != 1 || scanCalls != 0 || response.Result == nil || response.Result.Signals[0].ID != "restored" || !response.Cached || response.Frozen == nil || !*response.Frozen {
		t.Fatalf("latest snapshot was not restored: latest=%d scans=%d response=%+v", latestCalls, scanCalls, response)
	}
}

func TestRealtimeStrategyGETEnrichesSameDayClosedSnapshotOnce(t *testing.T) {
	enrichCalls, scanCalls := 0, 0
	generatedAt := realtimeWebTime(2026, 8, 20, 15, 0)
	latest := realtime.ScanResult{
		GeneratedAt: generatedAt,
		Signals: []realtime.Signal{
			{
				ID: "archived", Symbol: "sh688766", Industry: "半导体", Score: 57.2,
				Components: []realtime.Component{
					{Key: "sector-rotation", Name: "板块轮动", State: "数据不足", Warnings: []string{"未匹配到实时行业板块"}},
				},
			},
		},
	}
	enriched := latest
	enriched.Signals = append([]realtime.Signal(nil), latest.Signals...)
	enriched.Signals[0].Components = []realtime.Component{{Key: "sector-rotation", Name: "板块轮动", Score: 0.8, Maximum: 20, State: "弱", Reasons: []string{"板块涨幅 +0.39%"}}}
	server := NewServer(
		resolverStub{}, nil, nil, nil, "",
		WithRealtimeStrategy(realtimeEnrichingScannerStub{
			realtimeScannerStub: realtimeScannerStub{calls: &scanCalls},
			enriched:            enriched,
			enrichCalls:         &enrichCalls,
		}, realtimeArchiveStub{latest: latest}),
	)
	server.now = func() time.Time { return realtimeWebTime(2026, 8, 20, 16, 0) }

	for requestIndex := 0; requestIndex < 2; requestIndex++ {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/strategy/realtime", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
		}
		var response realtimeStrategyResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		component := response.Result.Signals[0].Components[0]
		if component.State == "数据不足" || component.Score != 0.8 || response.Result.Signals[0].Score != 57.2 {
			t.Fatalf("closed snapshot was not enriched without changing its score: %+v", response.Result.Signals[0])
		}
	}
	if enrichCalls != 1 || scanCalls != 0 {
		t.Fatalf("unexpected enrichment calls: enrich=%d scan=%d", enrichCalls, scanCalls)
	}
}

func realtimeWebTime(year, month, day, hour, minute int) time.Time {
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
}

func TestRealtimeOutcomeEndpointReturnsPerformanceReport(t *testing.T) {
	now := time.Date(2026, 8, 20, 16, 0, 0, 0, time.Local)
	report := realtime.OutcomeReport{GeneratedAt: now, Horizons: []int{1, 3}, Summaries: []realtime.OutcomeSummary{{Horizon: 1, Ready: 8, HitRatePercent: 62.5}}}
	archive := realtimeArchiveStub{items: []realtime.Signal{{ID: "signal", Symbol: "sh600519", AsOf: now}}}
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithRealtimeStrategy(realtimeScannerStub{}, archive), WithRealtimeOutcomes(realtimeOutcomeStub{report: report}))

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/strategy/realtime?view=outcomes&horizons=1,3&limit=100", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected outcome status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response realtimeStrategyResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Report == nil || len(response.Report.Summaries) != 1 || response.Report.Summaries[0].Ready != 8 {
		t.Fatalf("unexpected outcome response: %+v", response)
	}
}

func TestRealtimeOutcomeGETIsReadOnlyAndDoesNotRequireSignalArchive(t *testing.T) {
	now := time.Date(2026, 8, 20, 16, 0, 0, 0, time.Local)
	reportCalls, evaluateCalls, reportLimit := 0, 0, 0
	server := NewServer(
		resolverStub{}, nil, nil, nil, "",
		WithRealtimeOutcomes(realtimeOutcomeStub{
			report:        realtime.OutcomeReport{GeneratedAt: now},
			reportCalls:   &reportCalls,
			evaluateCalls: &evaluateCalls,
			reportLimit:   &reportLimit,
		}),
	)

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/strategy/realtime?view=outcomes&limit=7", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected outcome status %d: %s", recorder.Code, recorder.Body.String())
	}
	if reportCalls != 1 || evaluateCalls != 0 || reportLimit != 7 {
		t.Fatalf("GET crossed evaluation boundary: report=%d evaluate=%d limit=%d", reportCalls, evaluateCalls, reportLimit)
	}
}

func TestRealtimeOutcomeFullArchiveModeUsesNegativeSentinel(t *testing.T) {
	reportCalls, reportLimit := 0, 0
	server := NewServer(
		resolverStub{}, nil, nil, nil, "",
		WithRealtimeOutcomes(realtimeOutcomeStub{
			report:      realtime.OutcomeReport{GeneratedAt: realtimeWebTime(2026, 8, 20, 16, 0)},
			reportCalls: &reportCalls,
			reportLimit: &reportLimit,
		}),
	)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/strategy/realtime?view=outcomes&full=1", nil))
	if recorder.Code != http.StatusOK || reportCalls != 1 || reportLimit != -1 {
		t.Fatalf("full outcome report did not use archive sentinel: status=%d calls=%d limit=%d", recorder.Code, reportCalls, reportLimit)
	}
}

func TestRealtimeOutcomePartialReportCannotInstallCalibration(t *testing.T) {
	scanner := &calibratableRealtimeScannerStub{}
	archive := realtimeArchiveStub{items: []realtime.Signal{{ID: "signal", Symbol: "sh600519"}}}
	report := realtime.OutcomeReport{
		GeneratedAt: realtimeWebTime(2026, 8, 28, 16, 0),
		Assessment: realtime.OutcomeAssessment{
			Verdict:      "可作为下一轮候选",
			Horizon:      realtime.OutcomeHorizon5D,
			ReadySamples: 48,
			Threshold:    &realtime.ThresholdProposal{MinimumScore: 61},
			StrategyWeights: []realtime.StrategyWeightProposal{
				{Key: "relative-momentum", Weight: .6},
				{Key: "price-volume", Weight: .4},
			},
		},
	}
	server := NewServer(resolverStub{}, nil, nil, nil, "",
		WithRealtimeStrategy(scanner, archive),
		WithRealtimeOutcomes(realtimeOutcomeStub{report: report}),
	)
	partial := httptest.NewRecorder()
	server.Handler().ServeHTTP(partial, httptest.NewRequest(http.MethodGet, "/api/strategy/realtime?view=outcomes&limit=7", nil))
	if partial.Code != http.StatusOK || scanner.calibration.ID != "" {
		t.Fatalf("bounded outcome report installed a calibration: status=%d calibration=%+v", partial.Code, scanner.calibration)
	}
	full := httptest.NewRecorder()
	server.Handler().ServeHTTP(full, httptest.NewRequest(http.MethodGet, "/api/strategy/realtime?view=outcomes&full=1", nil))
	if full.Code != http.StatusOK || scanner.calibration.ID == "" {
		t.Fatalf("full outcome report did not install a gated calibration: status=%d calibration=%+v", full.Code, scanner.calibration)
	}
}

func TestRealtimeOutcomePOSTReadsSignalsAndEvaluates(t *testing.T) {
	evaluateCalls, archiveCalls := 0, 0
	options := realtime.OutcomeOptions{}
	archive := realtimeArchiveStub{items: []realtime.Signal{{ID: "signal", Symbol: "sh600519"}}, listCalls: &archiveCalls}
	server := NewServer(
		resolverStub{}, nil, nil, nil, "",
		WithRealtimeStrategy(realtimeScannerStub{}, archive),
		WithRealtimeOutcomes(realtimeOutcomeStub{evaluateCalls: &evaluateCalls, options: &options}),
	)

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/strategy/realtime?view=outcomes&horizons=1,3&target_percent=6&limit=9", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected outcome status %d: %s", recorder.Code, recorder.Body.String())
	}
	if archiveCalls != 1 || evaluateCalls != 1 || len(options.Horizons) != 2 || options.Horizons[0] != 1 || options.Horizons[1] != 3 || options.TargetReturn != 6 || options.SignalLimit != 9 {
		t.Fatalf("POST did not evaluate requested inputs: archive=%d evaluate=%d options=%+v", archiveCalls, evaluateCalls, options)
	}
}

func TestRealtimeOutcomePOSTFullArchiveModePassesThrough(t *testing.T) {
	evaluateCalls, archiveCalls, archiveLimit := 0, 0, 0
	options := realtime.OutcomeOptions{}
	archive := realtimeArchiveStub{
		items:     []realtime.Signal{{ID: "signal", Symbol: "sh600519"}},
		listCalls: &archiveCalls,
		listLimit: &archiveLimit,
	}
	server := NewServer(
		resolverStub{}, nil, nil, nil, "",
		WithRealtimeStrategy(realtimeScannerStub{}, archive),
		WithRealtimeOutcomes(realtimeOutcomeStub{evaluateCalls: &evaluateCalls, options: &options}),
	)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/strategy/realtime?view=outcomes&full=1", nil))
	if recorder.Code != http.StatusOK || archiveCalls != 1 || archiveLimit != -1 || evaluateCalls != 1 || options.SignalLimit != -1 {
		t.Fatalf("full outcome evaluation did not use archive sentinel: status=%d archive=%d limit=%d evaluate=%d options=%+v", recorder.Code, archiveCalls, archiveLimit, evaluateCalls, options)
	}
}

func TestShadowExecutionGETAndPOST(t *testing.T) {
	basePosition := paper.ShadowOpenPosition{
		Symbol: "sh600519", Quantity: 100, EntryPrice: 10,
		LastDate: "2026-08-20", LastPrice: 10, MarketValue: 1000,
	}
	archive := &shadowArchiveStub{report: paper.Report{CandidateCount: 2, Config: paper.DefaultConfig(), Positions: []paper.ShadowOpenPosition{basePosition}}}
	evaluateCalls := 0
	valuedAt := realtimeWebTime(2026, 8, 21, 10, 15)
	evaluated := paper.Report{CandidateCount: 3, CompletedTrades: 1, Config: paper.DefaultConfig(), Positions: []paper.ShadowOpenPosition{basePosition}}
	server := NewServer(
		resolverStub{}, shadowQuoteStub{quotes: []domain.Quote{{Symbol: "sh600519", Current: "12.34", QuoteTime: "2026-08-21 10:15:00", Source: "test-l1"}}}, nil, nil, "",
		WithRealtimeStrategy(realtimeScannerStub{}, realtimeArchiveStub{items: []realtime.Signal{{ID: "signal", Symbol: "sh600519"}}}),
		WithShadowExecution(shadowAnalyzerStub{report: evaluated, calls: &evaluateCalls}, archive),
	)
	server.now = func() time.Time { return valuedAt }

	getRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/strategy/shadow", nil))
	if getRecorder.Code != http.StatusOK || evaluateCalls != 0 {
		t.Fatalf("GET crossed evaluation boundary: status=%d calls=%d body=%s", getRecorder.Code, evaluateCalls, getRecorder.Body.String())
	}
	var getPayload shadowResponse
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &getPayload); err != nil {
		t.Fatal(err)
	}
	if getPayload.Report == nil || len(getPayload.Report.Positions) != 1 || getPayload.Report.Positions[0].LastPrice != 12.34 || !getPayload.Report.Positions[0].RealtimeValuation {
		t.Fatalf("GET did not mark positions with realtime quote: %+v", getPayload.Report)
	}
	if getPayload.Report.ValuedAt == nil || !getPayload.Report.ValuedAt.Equal(valuedAt) || getPayload.Report.Positions[0].ValuationSource != "test-l1" {
		t.Fatalf("GET valuation metadata mismatch: %+v", getPayload.Report)
	}

	postRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(postRecorder, httptest.NewRequest(http.MethodPost, "/api/strategy/shadow?limit=9&minimum_score=60&holding_days=3", nil))
	if postRecorder.Code != http.StatusOK || evaluateCalls != 1 || archive.saves != 1 || archive.report.CompletedTrades != 1 {
		t.Fatalf("unexpected POST result: status=%d calls=%d saves=%d report=%+v body=%s", postRecorder.Code, evaluateCalls, archive.saves, archive.report, postRecorder.Body.String())
	}
	var postPayload shadowResponse
	if err := json.Unmarshal(postRecorder.Body.Bytes(), &postPayload); err != nil {
		t.Fatal(err)
	}
	if postPayload.Report == nil || len(postPayload.Report.Positions) != 1 || postPayload.Report.Positions[0].LastPrice != 12.34 {
		t.Fatalf("POST response did not include realtime valuation: %+v", postPayload.Report)
	}
	if archive.report.Positions[0].LastPrice != 10 || archive.report.Positions[0].RealtimeValuation || archive.report.ValuedAt != nil {
		t.Fatalf("realtime valuation leaked into saved execution report: %+v", archive.report)
	}
}

func TestShadowExecutionProfilesUseIndependentLedgersAndConfigs(t *testing.T) {
	balanced := &shadowArchiveStub{}
	conservative := &shadowArchiveStub{}
	aggressive := &shadowArchiveStub{}
	configs := make([]paper.Config, 0, 3)
	server := NewServer(
		resolverStub{}, nil, nil, nil, "",
		WithRealtimeStrategy(realtimeScannerStub{}, realtimeArchiveStub{items: []realtime.Signal{{ID: "signal", Symbol: "sh600519"}}}),
		WithShadowExecutionProfiles(shadowProfileAnalyzerStub{configs: &configs}, balanced, conservative, aggressive),
	)
	server.now = func() time.Time { return realtimeWebTime(2026, 8, 21, 10, 15) }

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/strategy/shadow?profile=all&selected=conservative", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected profile sync status %d: %s", recorder.Code, recorder.Body.String())
	}
	if balanced.saves != 1 || conservative.saves != 1 || aggressive.saves != 1 || len(configs) != 3 {
		t.Fatalf("profiles did not save independently: balanced=%d conservative=%d aggressive=%d configs=%d", balanced.saves, conservative.saves, aggressive.saves, len(configs))
	}
	if balanced.report.Config.MinimumScore != 55 || conservative.report.Config.MinimumScore != 62 || aggressive.report.Config.MinimumScore != 52 {
		t.Fatalf("profile thresholds were not isolated: balanced=%+v conservative=%+v aggressive=%+v", balanced.report.Config, conservative.report.Config, aggressive.report.Config)
	}
	if balanced.report.Config.MaxOpenPositions != 8 || conservative.report.Config.MaxOpenPositions != 6 || aggressive.report.Config.MaxOpenPositions != 10 ||
		balanced.report.Config.MaxDailyRotations != 2 || conservative.report.Config.MaxDailyRotations != 1 || aggressive.report.Config.MaxDailyRotations != 3 {
		t.Fatalf("profile rotation controls were not isolated: balanced=%+v conservative=%+v aggressive=%+v", balanced.report.Config, conservative.report.Config, aggressive.report.Config)
	}
	if balanced.report.ConfigFingerprint == conservative.report.ConfigFingerprint || balanced.report.ConfigFingerprint == aggressive.report.ConfigFingerprint || conservative.report.ConfigFingerprint == aggressive.report.ConfigFingerprint {
		t.Fatalf("profile fingerprints must differ: %s %s %s", balanced.report.ConfigFingerprint, conservative.report.ConfigFingerprint, aggressive.report.ConfigFingerprint)
	}
	var payload shadowResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Account == nil || payload.Account.ID != shadowProfileConservative || payload.Report == nil || payload.Report.Config.MinimumScore != 62 || len(payload.Profiles) != 3 {
		t.Fatalf("selected profile response mismatch: %+v", payload)
	}
}

func TestMonsterShadowExecutionAddsIndependentRadarProfile(t *testing.T) {
	balanced := &shadowArchiveStub{}
	conservative := &shadowArchiveStub{}
	aggressive := &shadowArchiveStub{}
	monster := &shadowArchiveStub{}
	configs := make([]paper.Config, 0, 4)
	server := NewServer(
		resolverStub{}, nil, nil, nil, "",
		WithRealtimeStrategy(realtimeScannerStub{}, realtimeArchiveStub{items: []realtime.Signal{{ID: "signal", Symbol: "sh600519"}}}),
		WithShadowExecutionProfiles(shadowProfileAnalyzerStub{configs: &configs}, balanced, conservative, aggressive),
		WithMonsterShadowExecution(monster),
	)
	server.now = func() time.Time { return realtimeWebTime(2026, 8, 21, 10, 15) }

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/strategy/shadow?profile=all&selected=monster", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected monster profile sync status %d: %s", recorder.Code, recorder.Body.String())
	}
	if balanced.saves != 1 || conservative.saves != 1 || aggressive.saves != 1 || monster.saves != 1 || len(configs) != 4 {
		t.Fatalf("monster profile did not run as an independent ledger: saves=%d/%d/%d/%d configs=%d", balanced.saves, conservative.saves, aggressive.saves, monster.saves, len(configs))
	}
	if !monster.report.Config.UseMonsterRadar || monster.report.Config.MinimumScore != 58 || monster.report.Config.MaxOpenPositions != 6 {
		t.Fatalf("monster profile config mismatch: %+v", monster.report.Config)
	}
	if balanced.report.Config.UseMonsterRadar || conservative.report.Config.UseMonsterRadar || aggressive.report.Config.UseMonsterRadar {
		t.Fatal("monster radar leaked into a baseline profile")
	}
	var payload shadowResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Account == nil || payload.Account.ID != shadowProfileMonster || payload.Report == nil || !payload.Report.Config.UseMonsterRadar || len(payload.Profiles) != 4 {
		t.Fatalf("monster profile response mismatch: %+v", payload)
	}
	if payload.Profiles[3].ID != shadowProfileMonster {
		t.Fatalf("monster profile ordering mismatch: %+v", payload.Profiles)
	}
}

func TestShadowExecutionProfilesShareTradingCalendarRead(t *testing.T) {
	history := &countingHistoryStub{}
	balanced := &shadowArchiveStub{}
	conservative := &shadowArchiveStub{}
	aggressive := &shadowArchiveStub{}
	configs := make([]paper.Config, 0, 3)
	server := NewServer(
		resolverStub{}, nil, history, nil, "",
		WithRealtimeStrategy(realtimeScannerStub{}, realtimeArchiveStub{items: []realtime.Signal{{ID: "signal", Symbol: "sh600519"}}}),
		WithShadowExecutionProfiles(shadowProfileAnalyzerStub{configs: &configs}, balanced, conservative, aggressive),
	)
	now := realtimeWebTime(2026, 8, 21, 10, 15)
	server.now = func() time.Time { return now }

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/strategy/shadow?profile=all", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected profile sync status %d: %s", recorder.Code, recorder.Body.String())
	}
	if history.calls != 1 {
		t.Fatalf("three profiles should share one calendar read, got %d", history.calls)
	}
	if balanced.saves != 1 || conservative.saves != 1 || aggressive.saves != 1 || len(configs) != 3 {
		t.Fatalf("profiles did not all sync after shared calendar read: saves=%d/%d/%d configs=%d", balanced.saves, conservative.saves, aggressive.saves, len(configs))
	}
}

func TestShadowExecutionUsesInjectedCalendarForEvaluator(t *testing.T) {
	calendarCalls := 0
	history := &countingHistoryStub{}
	archive := &shadowArchiveStub{}
	configs := make([]paper.Options, 0, 1)
	server := NewServer(
		resolverStub{}, nil, history, nil, "",
		WithRealtimeStrategy(realtimeScannerStub{}, realtimeArchiveStub{items: []realtime.Signal{{ID: "signal", Symbol: "sh600519"}}}),
		WithShadowExecution(shadowOptionsCaptureStub{options: &configs}, archive),
		WithTradingCalendar(func(context.Context, time.Time) ([]string, error) {
			calendarCalls++
			return []string{"2026-08-20", "2026-08-22"}, nil
		}),
	)
	server.now = func() time.Time { return realtimeWebTime(2026, 8, 21, 10, 15) }
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/strategy/shadow", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	if calendarCalls != 1 || len(configs) != 1 || len(configs[0].CalendarDates) != 2 || history.calls != 0 {
		t.Fatalf("injected calendar was not shared with evaluator: calls=%d options=%+v history=%d", calendarCalls, configs, history.calls)
	}
}

type shadowOptionsCaptureStub struct {
	options *[]paper.Options
}

func (stub shadowOptionsCaptureStub) Evaluate(_ context.Context, _ []realtime.Signal, options paper.Options) (paper.Report, error) {
	if stub.options != nil {
		*stub.options = append(*stub.options, options)
	}
	return paper.Report{
		EngineVersion:     paper.ShadowEngineVersion,
		ConfigFingerprint: paper.OptionsFingerprint(options.Config, options.Limit),
		AsOf:              "2026-08-21", CheckpointPhase: paper.CheckpointOpen, Config: options.Config,
	}, nil
}

func TestShadowExecutionProfileValidationAndEmptyState(t *testing.T) {
	server := NewServer(
		resolverStub{}, nil, nil, nil, "",
		WithShadowExecution(shadowAnalyzerStub{}, &shadowArchiveStub{}),
	)

	empty := httptest.NewRecorder()
	server.Handler().ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/api/strategy/shadow", nil))
	if empty.Code != http.StatusOK {
		t.Fatalf("unexpected empty profile status %d: %s", empty.Code, empty.Body.String())
	}
	var payload shadowResponse
	if err := json.Unmarshal(empty.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Report != nil || payload.Account == nil || payload.Account.Available {
		t.Fatalf("empty account was presented as initialized: %+v", payload)
	}

	invalid := httptest.NewRecorder()
	server.Handler().ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/strategy/shadow?profile=unknown", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("unknown profile status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestShadowExecutionPOSTCachesCurrentDayUnlessRebuildRequested(t *testing.T) {
	history := &countingHistoryStub{}
	archive := &shadowArchiveStub{report: paper.Report{
		EngineVersion: paper.ShadowEngineVersion,
		AsOf:          "2026-08-21",
		Config:        paper.DefaultConfig(),
	}}
	archive.report.ConfigFingerprint = paper.OptionsFingerprint(archive.report.Config, 0)
	archive.report.CheckpointPhase = paper.CheckpointOpen
	evaluateCalls, listLimit := 0, -1
	server := NewServer(
		resolverStub{}, nil, history, nil, "",
		WithRealtimeStrategy(realtimeScannerStub{}, realtimeArchiveStub{listLimit: &listLimit}),
		WithShadowExecution(shadowAnalyzerStub{report: paper.Report{EngineVersion: paper.ShadowEngineVersion, AsOf: "2026-08-21"}, calls: &evaluateCalls}, archive),
	)
	server.now = func() time.Time { return realtimeWebTime(2026, 8, 21, 10, 15) }

	cached := httptest.NewRecorder()
	server.Handler().ServeHTTP(cached, httptest.NewRequest(http.MethodPost, "/api/strategy/shadow", nil))
	if cached.Code != http.StatusOK || evaluateCalls != 0 {
		t.Fatalf("same-day POST rebuilt account: status=%d calls=%d body=%s", cached.Code, evaluateCalls, cached.Body.String())
	}
	var cachedPayload shadowResponse
	if err := json.Unmarshal(cached.Body.Bytes(), &cachedPayload); err != nil {
		t.Fatal(err)
	}
	if !cachedPayload.Cached || cachedPayload.Report == nil || cachedPayload.Report.AsOf != "2026-08-21" {
		t.Fatalf("same-day POST did not return cached report: %+v", cachedPayload)
	}
	if history.calls != 1 {
		t.Fatalf("same checkpoint validation should read the shared trading calendar once: calls=%d", history.calls)
	}

	rebuilt := httptest.NewRecorder()
	server.Handler().ServeHTTP(rebuilt, httptest.NewRequest(http.MethodPost, "/api/strategy/shadow?rebuild=1", nil))
	if rebuilt.Code != http.StatusOK || evaluateCalls != 1 || archive.saves != 1 || listLimit != 0 {
		t.Fatalf("explicit rebuild was not honored: status=%d calls=%d saves=%d limit=%d body=%s", rebuilt.Code, evaluateCalls, archive.saves, listLimit, rebuilt.Body.String())
	}
}

func TestShadowExecutionPOSTReportsPreservedReason(t *testing.T) {
	cfg := paper.DefaultConfig()
	fee := cfg.MinimumCommission
	previous := paper.Report{
		EngineVersion: paper.ShadowEngineVersion, Config: cfg,
		ConfigFingerprint: paper.OptionsFingerprint(cfg, 0), AsOf: "2026-08-21",
		CheckpointPhase: paper.CheckpointOpen, ExecutionMode: paper.ExecutionModeLive,
		LastRealtimeAt: "2026-08-21 10:00:00", InitialCash: cfg.InitialCash,
		RemainingCash: cfg.InitialCash - 1000 - fee,
		Orders:        []paper.ShadowOrder{{ID: "old", Symbol: "sh600000", Side: "buy", AttemptDate: "2026-08-21", ExecutionTime: "2026-08-21 09:30:00", Quantity: 100, Price: 10, RawPrice: 10, Amount: 1000, Status: paper.OrderFilled}},
		Positions:     []paper.ShadowOpenPosition{{Symbol: "sh600000", Quantity: 100, EntryDate: "2026-08-21", EntryPrice: 10, EntryAmount: 1000, EntryFee: fee}},
	}
	archive := &shadowArchiveStub{report: previous}
	server := NewServer(
		resolverStub{}, nil, nil, nil, "",
		WithRealtimeStrategy(realtimeScannerStub{}, realtimeArchiveStub{items: []realtime.Signal{{ID: "signal", Symbol: "sh600000"}}}),
		WithShadowExecution(shadowAnalyzerStub{report: paper.Report{EngineVersion: paper.ShadowEngineVersion, Config: cfg, AsOf: "2026-08-21", CheckpointPhase: paper.CheckpointOpen}, calls: new(int)}, archive),
	)
	server.now = func() time.Time { return realtimeWebTime(2026, 8, 21, 10, 15) }
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/strategy/shadow?rebuild=1", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload shadowResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Preserved || payload.PreserveReason == "" {
		t.Fatalf("preserve reason was not returned: %+v", payload)
	}
}

func TestShadowHelpersRejectStaleQuotesAndMismatchedCache(t *testing.T) {
	now := realtimeWebTime(2026, 8, 24, 10, 0)
	if shadowQuoteFresh("2026-08-24 09:00:00", now) {
		t.Fatal("stale quote was accepted")
	}
	if !shadowQuoteFresh("2026-08-24 09:45:00", now) {
		t.Fatal("fresh quote was rejected")
	}
	reportWithWatermark := paper.Report{LastRealtimeAt: "2026-08-24 09:45:00"}
	if shadowRealtimeQuotesAdvance(reportWithWatermark, []paper.PositionQuote{{Symbol: "sh600519", Price: 10, QuoteTime: "2026-08-24 09:44:00"}}, now) {
		t.Fatal("quote older than the account watermark was accepted")
	}
	if shadowRealtimeQuotesAdvance(reportWithWatermark, []paper.PositionQuote{{Symbol: "sh600519", Price: 10, QuoteTime: "2026-08-24 09:49:00"}}, now.Add(20*time.Minute)) {
		t.Fatal("stale quote advanced the realtime account")
	}
	if !shadowRealtimeQuotesAdvance(reportWithWatermark, []paper.PositionQuote{{Symbol: "sh600519", Price: 10, QuoteTime: "2026-08-24 09:59:00"}}, now) {
		t.Fatal("fresh quote after the account watermark was rejected")
	}
	options := paper.Options{Config: paper.DefaultConfig(), Limit: 0}
	checkpoint := paper.Checkpoint{Date: "2026-08-24", Phase: paper.CheckpointOpen}
	report := paper.Report{EngineVersion: paper.ShadowEngineVersion, ConfigFingerprint: paper.OptionsFingerprint(options.Config, options.Limit), AsOf: checkpoint.Date, CheckpointPhase: checkpoint.Phase}
	if !shadowReportMatches(report, options, checkpoint) {
		t.Fatal("matching cache was rejected")
	}
	options.Config.HoldingDays++
	if shadowReportMatches(report, options, checkpoint) {
		t.Fatal("cache ignored execution config change")
	}
}

func TestShadowCanAdvanceLegacyLedgerIntoCurrentEngine(t *testing.T) {
	legacyConfig := paper.DefaultConfig()
	legacyConfig.MaxPortfolioPercent = 0
	legacyConfig.CashReservePercent = 0
	legacyConfig.MaxDailyDeploymentPercent = 0
	legacyConfig.InitialEntryPercent = 0
	legacyConfig.MaxEntryTranches = 0
	legacyConfig.AdditionScoreStep = 0
	report := paper.Report{
		EngineVersion:     "tplus1-v6",
		ConfigFingerprint: "legacy-fingerprint",
		Config:            legacyConfig,
		Orders:            []paper.ShadowOrder{{ID: "legacy-buy", Symbol: "sh600000", Side: "buy", Status: paper.OrderFilled}},
	}
	options := paper.Options{Config: paper.DefaultConfig(), Limit: 0}
	if !shadowCanAdvance(report, options) {
		t.Fatal("compatible legacy ledger was not allowed to migrate")
	}
	options.Config.HoldingDays++
	if shadowCanAdvance(report, options) {
		t.Fatal("legacy ledger ignored an incompatible holding-window change")
	}
}

func TestShadowCanAdvanceV8LedgerWithNewRotationControls(t *testing.T) {
	previousConfig := paper.DefaultConfig()
	previousConfig.MaxOpenPositions = 0
	previousConfig.MaxDailyRotations = 0
	previousConfig.RotationScoreGap = 0
	previousConfig.RotationMinimumHoldDays = 0
	report := paper.Report{
		EngineVersion: "tplus1-v8", Config: previousConfig,
		Orders: []paper.ShadowOrder{{ID: "v8-buy", Symbol: "sh600000", Side: "buy", Status: paper.OrderFilled}},
	}
	options := paper.Options{Config: paper.DefaultConfig(), Limit: 0}
	options.Config.MaxOpenPositions = 6
	options.Config.MaxDailyRotations = 1
	options.Config.RotationScoreGap = 10
	options.Config.RotationMinimumHoldDays = 3
	if !shadowCanAdvance(report, options) {
		t.Fatal("v8 ledger could not adopt forward-only v9 rotation controls")
	}
	options.Config.HoldingDays++
	if shadowCanAdvance(report, options) {
		t.Fatal("v8 migration ignored an incompatible pre-v9 execution change")
	}
}

func TestServerRestoresDurableRealtimeCalibration(t *testing.T) {
	scanner := &calibratableRealtimeScannerStub{}
	store := storage.NewRealtimeCalibrationStore(filepath.Join(t.TempDir(), "calibration.json"))
	active := realtime.ScoreCalibration{ID: "CAL-RESTORE", DataThrough: "2026-08-27", ReadySamples: 48, MinimumScore: 61, ComponentWeights: map[string]float64{"relative-momentum": .6, "price-volume": .4}}
	if err := store.Save(storage.RealtimeCalibrationState{Active: active}); err != nil {
		t.Fatal(err)
	}
	_ = NewServer(nil, nil, nil, nil, "600519", WithRealtimeStrategy(scanner, nil), WithRealtimeCalibrationStore(store))
	if scanner.calibration.ID != active.ID || scanner.calibration.MinimumScore != active.MinimumScore {
		t.Fatalf("durable calibration was not restored: %+v", scanner.calibration)
	}
}

func TestNewerRealtimeCalibrationRejectsStaleEvidence(t *testing.T) {
	current := realtime.ScoreCalibration{ID: "CAL-20260827-H5-N48", DataThrough: "2026-08-27", ReadySamples: 48}
	if newerRealtimeCalibration(realtime.ScoreCalibration{ID: "CAL-20260826-H5-N60", DataThrough: "2026-08-26", ReadySamples: 60}, current) {
		t.Fatal("older data cutoff must not replace the active calibration")
	}
	if !newerRealtimeCalibration(realtime.ScoreCalibration{ID: "CAL-20260828-H5-N48", DataThrough: "2026-08-28", ReadySamples: 48}, current) {
		t.Fatal("newer data cutoff should replace the active calibration")
	}
}

func TestRealtimeCalibrationRollsBackMateriallyWorseChallenger(t *testing.T) {
	scanner := &calibratableRealtimeScannerStub{}
	store := storage.NewRealtimeCalibrationStore(filepath.Join(t.TempDir(), "calibration.json"))
	active := realtime.ScoreCalibration{ID: "CAL-ROLLBACK", DataThrough: "2026-08-27", ReadySamples: 48, MinimumScore: 61, ComponentWeights: map[string]float64{"relative-momentum": .6, "price-volume": .4}}
	if err := store.Save(storage.RealtimeCalibrationState{Active: active, Status: "challenger-active"}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(nil, nil, nil, nil, "600519", WithRealtimeStrategy(scanner, nil), WithRealtimeCalibrationStore(store))
	server.now = func() time.Time { return realtimeWebTime(2026, 8, 28, 16, 0) }
	report := realtime.OutcomeReport{Calibration: realtime.CalibrationAnalysis{
		ID: "CAL-ROLLBACK", ReadySamples: 42, Status: "候选落后", Recommendation: "候选落后，暂停晋级",
	}}
	server.applyRealtimeCalibration(report)
	if scanner.calibration.ID != "" {
		t.Fatalf("rolled-back challenger remained active: %+v", scanner.calibration)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "challenger-rolled-back" || len(state.RejectedIDs) != 1 || state.RejectedIDs[0] != "CAL-ROLLBACK" {
		t.Fatalf("rollback decision was not persisted: %+v", state)
	}
}
