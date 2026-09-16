package web

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type limitStatsStub struct{}

func (limitStatsStub) FetchLimitStats(context.Context, string) (domain.LimitStatsSnapshot, error) {
	return domain.LimitStatsSnapshot{TradeDate: "2026-09-03", LimitUpCount: 42, LimitDownCount: 8, BrokenCount: 6, BrokenRate: 12.5, HighestStreak: 7, StreakLadder: []domain.LimitStreakGroup{{Streak: 7, Count: 1}, {Streak: 3, Count: 4}}, Available: true, Source: "mock"}, nil
}

func TestCalculateMarketSentimentUsesCoverageAwareSignals(t *testing.T) {
	flows := map[string]domain.BoardFlow{
		"a": {Name: "行业A", Percent: 2, MainNet: 1e8, RiseCount: 80, FallCount: 10, FlatCount: 10},
		"b": {Name: "行业B", Percent: 1, MainNet: 1e8, RiseCount: 60, FallCount: 20, FlatCount: 20},
		"c": {Name: "行业C", Percent: -1, MainNet: -1e8, RiseCount: 20, FallCount: 70, FlatCount: 10},
	}
	indices := []domain.Quote{{Symbol: "sh000001", Percent: 1}, {Symbol: "sz399001", Percent: .5}, {Symbol: "sz399006", Percent: -.2}}
	snapshot := calculateMarketSentiment(time.Date(2026, 9, 3, 10, 0, 0, 0, time.Local), indices, flows, domain.MarketAmountSnapshot{Shanghai: 100, Shenzhen: 100, Beijing: 10})
	if snapshot.Phase == "数据不足" || snapshot.Score <= 50 || snapshot.IndustryBreadth <= 50 || snapshot.PositiveIndustryRate <= 50 {
		t.Fatalf("unexpected sentiment snapshot: %#v", snapshot)
	}
	if snapshot.LimitCountsAvailable || len(snapshot.Warnings) == 0 {
		t.Fatalf("missing limit coverage warning: %#v", snapshot)
	}
	if len(snapshot.StrongIndustries) != 3 || snapshot.StrongIndustries[0].Name != "行业A" {
		t.Fatalf("unexpected strong industries: %#v", snapshot.StrongIndustries)
	}
	if len(snapshot.WeakIndustries) != 3 || snapshot.WeakIndustries[0].Name != "行业C" {
		t.Fatalf("unexpected weak industries: %#v", snapshot.WeakIndustries)
	}
}

func TestMarketSentimentResponseMarshalsUnavailableNumbersAsNull(t *testing.T) {
	response := marketSentimentResponse{Snapshot: domain.MarketSentimentSnapshot{Score: math.NaN(), Phase: "数据不足", IndexSignal: math.Inf(1)}}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal sentiment response: %v", err)
	}
	if string(data) == "" || !bytes.Contains(data, []byte(`"score":null`)) || !bytes.Contains(data, []byte(`"index_signal":null`)) {
		t.Fatalf("unexpected JSON: %s", data)
	}
}

func TestSentimentEndpointIncludesLimitStructure(t *testing.T) {
	server := NewServer(resolverStub{}, marketQuoteStub{}, historyStub{}, minuteStub{}, "600519", WithLimitStats(limitStatsStub{}))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/sentiment", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response marketSentimentResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Snapshot.LimitStatsAvailable || response.Snapshot.LimitUpCount != 42 || response.Snapshot.BrokenCount != 6 || response.Snapshot.HighestStreak != 7 || len(response.Snapshot.StreakLadder) != 2 {
		t.Fatalf("unexpected limit structure: %+v", response.Snapshot)
	}
	for _, warning := range response.Snapshot.Warnings {
		if warning == "涨停/跌停、炸板率和连板梯队尚未接入" {
			t.Fatalf("stale limit warning remains: %+v", response.Snapshot.Warnings)
		}
	}
}

type hotStockSignalStub struct {
	dates []string
}

func (stub *hotStockSignalStub) FetchHotStocks(_ context.Context, date string) (domain.HotStockSnapshot, error) {
	stub.dates = append(stub.dates, date)
	return domain.HotStockSnapshot{
		Available: true, TradeDate: date,
		Stocks: []domain.HotStockSignal{{Symbol: "sh600519", Name: "贵州茅台", Percent: 5}},
		Themes: []domain.HotTheme{{Name: "消费", Count: 1, Leader: "贵州茅台", AverageRise: 5}},
	}, nil
}

func TestSentimentEndpointKeepsThemesWithoutRetiredScoreOverlay(t *testing.T) {
	ctx := context.Background()
	quotes, _ := (marketQuoteStub{}).Fetch(ctx, []string{"sh000001", "sz399001", "sz399006"})
	flows, _ := (boardRankingStub{}).FetchIndustryFlows(ctx)
	previous, _ := (marketAmountStub{}).FetchPreviousMarketAmount(ctx)
	want := calculateMarketSentiment(time.Now(), quotes, flows, previous)
	hot := &hotStockSignalStub{}
	server := NewServer(nil, marketQuoteStub{}, nil, nil, "", WithIndustryFlows(boardRankingStub{}), WithMarketAmount(marketAmountStub{}), WithSentimentSignals(hot))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/sentiment", nil))
	var response marketSentimentResponse
	if recorder.Code != http.StatusOK {
		t.Fatalf("sentiment status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Snapshot.Score != want.Score || response.Snapshot.CoveragePercent != want.CoveragePercent || response.Snapshot.Phase != want.Phase {
		t.Fatalf("retired overlay changed score or coverage: got=%+v want=%+v", response.Snapshot, want)
	}
	if !response.Snapshot.HotSignalAvailable || response.Snapshot.HotStockCount != 1 || len(response.Snapshot.HotThemes) != 1 || len(hot.dates) != 1 {
		t.Fatalf("hot-theme data was lost: %+v requests=%v", response.Snapshot, hot.dates)
	}
	if response.Snapshot.ScoreModel != domain.MarketSentimentScoreModel || bytes.Contains(recorder.Body.Bytes(), []byte("northbound")) {
		t.Fatalf("retired fields or wrong score model in API: %s", recorder.Body.String())
	}
}

func TestSentimentHistorySeparatesScoreModelsWithoutRewritingLegacyPoints(t *testing.T) {
	var legacy []domain.MarketSentimentPoint
	if err := json.Unmarshal([]byte(`[{"at":"2026-09-11T09:30:00+08:00","score":54,"phase":"修复","northbound_signal":0}]`), &legacy); err != nil {
		t.Fatal(err)
	}
	current := domain.MarketSentimentSnapshot{GeneratedAt: legacy[0].At.Add(time.Minute), Score: 60, ScoreModel: domain.MarketSentimentScoreModel, Phase: "强势"}
	history := appendSentimentHistory(legacy, current)
	before := append([]domain.MarketSentimentPoint(nil), history...)
	data, err := json.Marshal(marketSentimentResponse{Snapshot: current, History: history})
	if err != nil {
		t.Fatal(err)
	}
	var response marketSentimentResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.History) != 1 || response.History[0].Score != 60 || response.History[0].ScoreModel != domain.MarketSentimentScoreModel {
		t.Fatalf("old and current scores were mixed: %s", data)
	}
	if !reflect.DeepEqual(before, history) || len(history) != 2 || history[0].Score != 54 || history[0].LegacyNorthboundSignal == nil {
		t.Fatalf("legacy history was rewritten: %+v", history)
	}
	if bytes.Contains(data, []byte("northbound")) {
		t.Fatalf("retired factor exposed by API: %s", data)
	}
}

func TestHotThemeCacheIsPartitionedByTradeDate(t *testing.T) {
	provider := &hotStockSignalStub{}
	server := NewServer(nil, nil, nil, nil, "", WithSentimentSignals(provider))
	for _, date := range []string{"2026-09-10", "2026-09-10", "2026-09-11"} {
		snapshot, err := server.fetchSentimentExtras(context.Background(), date)
		if err != nil || snapshot.TradeDate != date {
			t.Fatalf("incorrect cached theme date: %+v %v", snapshot, err)
		}
	}
	if !reflect.DeepEqual(provider.dates, []string{"2026-09-10", "2026-09-11"}) {
		t.Fatalf("unexpected theme requests: %v", provider.dates)
	}
}
