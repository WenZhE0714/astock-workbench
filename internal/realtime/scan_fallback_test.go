package realtime

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type partialScanMarket struct {
	industryBoardMarketStub
	stamp string
}

func (m partialScanMarket) FetchStocks(context.Context, []string) ([]domain.MarketStockSnapshot, error) {
	return []domain.MarketStockSnapshot{{Symbol: "sh600000", Source: "同花顺公开快照（大单口径）", QuoteTime: m.stamp, Name: "测试股份", Industry: "银行", Price: 18, Percent: 2, VolumeRatio: 1.5, Amount: 1e8, MainNet: math.NaN(), MainRatio: math.NaN(), Speed: math.NaN()}}, errors.New("sh600001 unavailable")
}

func TestScannerKeepsPartialSnapshotsAndRejectsOldDates(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 30, 0, 0, marketLocation)
	history := outcomeHistoryStub{bars: map[string][]domain.DailyBar{"sh000300": risingBars(70), "sh600000": risingBars(70)}}
	for _, day := range []string{"2026-08-20", "2026-08-19"} {
		scanner := NewScanner(partialScanMarket{stamp: day + " 10:30:00"}, nil, history, nil, nil)
		scanner.now = func() time.Time { return now }
		result, err := scanner.Scan(context.Background(), []string{"sh600000", "sh600001"}, false)
		if day == "2026-08-19" {
			if err == nil || !strings.Contains(err.Error(), "行情日期未推进") {
				t.Fatalf("stale quote passed: %+v %v", result, err)
			}
			continue
		}
		if err != nil || len(result.Signals) != 1 || result.Signals[0].QuoteTime != day+" 10:30:00" {
			t.Fatalf("partial scan failed: %+v %v", result, err)
		}
		if !strings.Contains(strings.Join(result.Warnings, "|"), "部分扫描行情缺失") {
			t.Fatal("partial coverage was hidden")
		}
	}
}

func TestRealtimeQuoteDoesNotCarryOldSnapshotFundsForward(t *testing.T) {
	stock := domain.MarketStockSnapshot{Symbol: "sh600000", QuoteTime: "2026-08-19 15:00:00", MainNet: 1e8, MainRatio: 5, Speed: 1}
	merged := mergeRealtimeQuote(stock, domain.Quote{Symbol: "sh600000", Current: "18", QuoteTime: "2026-08-20 10:30:00"})
	if !math.IsNaN(merged.MainNet) || !math.IsNaN(merged.MainRatio) || !math.IsNaN(merged.Speed) || merged.Price != 18 {
		t.Fatalf("yesterday's funds mixed with today's quote: %+v", merged)
	}
}
