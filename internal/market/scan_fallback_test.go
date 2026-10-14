package market

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type stockScanFunc func(context.Context, []string) ([]domain.MarketStockSnapshot, error)

func (fn stockScanFunc) FetchStocks(ctx context.Context, symbols []string) ([]domain.MarketStockSnapshot, error) {
	return fn(ctx, symbols)
}

type scanPrimaryStub struct {
	EastmoneyClient
	stocks stockScanFunc
}

func TestStockScanFallbackReservesShortDeadline(t *testing.T) {
	primary := scanPrimaryStub{stocks: func(ctx context.Context, _ []string) ([]domain.MarketStockSnapshot, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	backup := stockScanFunc(func(ctx context.Context, _ []string) ([]domain.MarketStockSnapshot, error) {
		if ctx.Err() != nil {
			t.Fatal("primary consumed fallback deadline")
		}
		return []domain.MarketStockSnapshot{{Symbol: "sh600584", Price: 30}}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	items, err := NewFallbackMarketScanClient(primary, backup, nil).FetchStocks(ctx, []string{"sh600584"})
	if err != nil || len(items) != 1 {
		t.Fatalf("fallback failed with short budget: %+v %v", items, err)
	}
}

func (stub scanPrimaryStub) FetchStocks(ctx context.Context, symbols []string) ([]domain.MarketStockSnapshot, error) {
	return stub.stocks(ctx, symbols)
}
func (scanPrimaryStub) FetchStockRanking(context.Context, domain.MarketScanMetric, bool, int) ([]domain.MarketStockSnapshot, error) {
	return nil, errors.New("primary EOF")
}

func TestScanFallbackKeepsValidPrimaryAndReportsMissingSymbols(t *testing.T) {
	primary := scanPrimaryStub{stocks: func(context.Context, []string) ([]domain.MarketStockSnapshot, error) {
		return []domain.MarketStockSnapshot{{Symbol: "sh600584", Price: 10}, {Symbol: "sz001317", Price: math.NaN()}}, errors.New("EOF")
	}}
	backup := stockScanFunc(func(_ context.Context, symbols []string) ([]domain.MarketStockSnapshot, error) {
		if strings.Join(symbols, ",") != "sz001317,sz002080" {
			t.Fatalf("wrong fallback scope: %v", symbols)
		}
		return []domain.MarketStockSnapshot{{Symbol: "sz001317", Price: 20, Source: THSStockScanSource}, {Symbol: "sh600584", Price: 99}}, errors.New("sz002080 unavailable")
	})
	items, err := NewFallbackMarketScanClient(primary, backup, nil).FetchStocks(context.Background(), []string{"sh600584", "sz001317", "sz002080"})
	if err == nil || len(items) != 2 || items[0].Price != 10 || items[1].Source != THSStockScanSource || !strings.Contains(err.Error(), "sz002080") {
		t.Fatalf("bad partial fallback: %+v %v", items, err)
	}
}

func TestScanRankingFallbackDoesNotInventFundFields(t *testing.T) {
	backup := rankingFunc(func(_ context.Context, kind domain.MarketRankingKind, _ int) ([]domain.MarketRankingItem, error) {
		if kind != domain.MarketRankingGainers {
			t.Fatal(kind)
		}
		return []domain.MarketRankingItem{{Symbol: "sh600584", Source: THSMarketRankingSource, Price: 10, Percent: 2}}, nil
	})
	client := NewFallbackMarketScanClient(scanPrimaryStub{}, nil, backup)
	items, err := client.FetchStockRanking(context.Background(), domain.MarketScanByPercent, true, 1)
	if err != nil || len(items) != 1 || !math.IsNaN(items[0].MainNet) || !math.IsNaN(items[0].High) {
		t.Fatalf("invented optional metrics: %+v %v", items, err)
	}
	if _, err := client.FetchStockRanking(context.Background(), domain.MarketScanByMainNet, true, 1); err == nil {
		t.Fatal("main-net ranking replaced with a different ordering")
	}
}

func stockScanFixture(t *testing.T, changes map[string]any) string {
	t.Helper()
	items := map[string]any{"5": "600584", "marketid": "17", "name": "长电科技", "updateTime": "2026-10-14 10:30", "10": "30", "199112": "2", "19": "100000000", "1968584": "3", "1771976": "1.5", "8": "31", "9": "29", "7": "29.5", "6": "29.4", "3541450": "50000000000", "223": "10000000", "224": "3000000", "225": "6000000", "226": "2000000"}
	for key, value := range changes {
		items[key] = value
	}
	data, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}
	return "quotebridge_v6_realhead_hs_600584_last(" + string(data) + ")"
}

func TestTHSStockScanPreservesUnitsTimestampAndUnavailableFields(t *testing.T) {
	item, err := ParseTHSStockScanPayload(stockScanFixture(t, nil), "sh600584")
	if err != nil || item.Price != 30 || item.Amount != 1e8 || item.MarketCap != 5e10 || item.MainNet != 11e6 || item.MainRatio != 11 || item.QuoteTime != "2026-10-14 10:30:00" || item.Source != THSStockScanSource || !math.IsNaN(item.Speed) {
		t.Fatalf("bad snapshot: %+v %v", item, err)
	}
	item, err = ParseTHSStockScanPayload(stockScanFixture(t, map[string]any{"223": nil, "1968584": nil}), "sh600584")
	if err != nil || !math.IsNaN(item.MainNet) || !math.IsNaN(item.MainRatio) || !math.IsNaN(item.Turnover) {
		t.Fatalf("invented missing fields: %+v %v", item, err)
	}
	for _, changes := range []map[string]any{{"5": "600585"}, {"marketid": "33"}, {"updateTime": "bad"}, {"10": "0"}} {
		if _, err := ParseTHSStockScanPayload(stockScanFixture(t, changes), "sh600584"); err == nil {
			t.Fatalf("accepted mismatched/invalid quote: %v", changes)
		}
	}
}

func TestTHSStockScanKeepsOtherStocksWhenOneFails(t *testing.T) {
	client := THSStockScanClient{BaseURL: "https://ths.test/{code}", HTTPClient: &http.Client{Transport: rankingRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/600584" {
			return nil, errors.New("unavailable")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(stockScanFixture(t, nil)))}, nil
	})}}
	items, err := client.FetchStocks(context.Background(), []string{"sh600584", "sz001317"})
	if err == nil || len(items) != 1 || items[0].Symbol != "sh600584" {
		t.Fatalf("lost partial data: %+v %v", items, err)
	}
}
