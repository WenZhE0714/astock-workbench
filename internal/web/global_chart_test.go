package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type globalChartEndpointStub struct{}

func (globalChartEndpointStub) FetchGlobalDailyBars(_ context.Context, symbol string) ([]domain.DailyBar, error) {
	return []domain.DailyBar{
		{Symbol: symbol, Source: "测试日K", Date: "2026-08-28", Open: 100, Close: 102, High: 104, Low: 99, Volume: 1000},
	}, nil
}

func (globalChartEndpointStub) FetchGlobalMinutePoints(_ context.Context, symbol string) ([]domain.MinutePoint, error) {
	return []domain.MinutePoint{
		{Symbol: symbol, Source: "测试分时", TradeDate: "2026-08-28", Time: "09:35", Price: 101, Average: 100.5, Volume: 10},
	}, nil
}

func TestGlobalChartEndpointReturnsSnapshotAndBothSeries(t *testing.T) {
	server := NewServer(
		resolverStub{}, marketQuoteStub{}, historyStub{}, minuteStub{}, "600519",
		WithGlobalMarkets(globalIndexStub{items: []domain.GlobalIndex{{
			Symbol: "gb_inx", Region: "美国", Name: "标普500", Current: 5600,
			PreviousClose: 5580, Open: 5590, High: 5610, Low: 5570, Percent: .36,
			QuoteTime: "2026-08-28 04:00:00", Source: "测试外盘",
		}}}),
		WithGlobalCharts(globalChartEndpointStub{}),
	)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/global/chart?symbol=gb_inx&mode=all", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response globalChartResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Symbol != "gb_inx" || response.Timezone != "America/New_York" || response.Market == nil {
		t.Fatalf("missing global metadata: %+v", response)
	}
	if len(response.Bars) != 1 || len(response.Minutes) != 1 || response.Bars[0].Close != 102 || response.Minutes[0].Time != "09:35" {
		t.Fatalf("missing global series: %+v", response)
	}
}

func TestGlobalChartEndpointRejectsUnknownMarket(t *testing.T) {
	server := NewServer(resolverStub{}, marketQuoteStub{}, historyStub{}, minuteStub{}, "600519", WithGlobalCharts(globalChartEndpointStub{}))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/global/chart?symbol=sh000001", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestGlobalChartEndpointSupportsKoreanMarket(t *testing.T) {
	server := NewServer(
		resolverStub{}, marketQuoteStub{}, historyStub{}, minuteStub{}, "600519",
		WithGlobalMarkets(globalIndexStub{items: []domain.GlobalIndex{{
			Symbol: "b_KOSPI", Region: "韩国", Name: "KOSPI", Current: 2700,
			PreviousClose: 2680, Open: 2690, High: 2710, Low: 2675,
			Percent: .75, QuoteTime: "2026-08-28 15:30:00", Source: "测试外盘",
		}}}),
		WithGlobalCharts(globalChartEndpointStub{}),
	)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/global/chart?symbol=b_KOSPI&mode=all", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response globalChartResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Symbol != "b_KOSPI" || response.Region != "韩国" || response.Timezone != "Asia/Seoul" || response.Market == nil {
		t.Fatalf("missing Korean market metadata: %+v", response)
	}
}

func TestNormalizeGlobalProxySeriesAlignsLatestPointToIndex(t *testing.T) {
	response := &globalChartResponse{
		Market: &globalMarketResponse{Current: "5000"},
		Bars: []chartBar{
			{Source: "Yahoo Finance · 3033.HK 代理", Open: 9, Close: 10, High: 11, Low: 8},
		},
		Minutes: []minutePointResponse{
			{Source: "Yahoo Finance · 3033.HK 代理", Price: 10, Average: 9, Amount: 100},
		},
	}
	normalizeGlobalProxySeries(response)
	if !response.Approximate || response.Proxy == "" || response.Bars[0].Close != 5000 || response.Minutes[0].Price != 5000 {
		t.Fatalf("proxy series was not normalized: %+v", response)
	}
}
