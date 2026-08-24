package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

type realtimeScannerStub struct {
	result realtime.ScanResult
	calls  *int
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
	latestCalls *int
}

func (stub realtimeArchiveStub) List(int) ([]realtime.Signal, error) {
	if stub.listCalls != nil {
		(*stub.listCalls)++
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
