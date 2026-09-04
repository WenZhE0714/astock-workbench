package web

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
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
