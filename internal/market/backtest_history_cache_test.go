package market

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
)

type rangeAwareHistoryMock struct {
	dailyBars  []domain.DailyBar
	rangeBars  []domain.DailyBar
	dailyErr   error
	rangeErr   error
	rangeCalls int
}

func (mock *rangeAwareHistoryMock) FetchDailyBars(context.Context, string) ([]domain.DailyBar, error) {
	return append([]domain.DailyBar(nil), mock.dailyBars...), mock.dailyErr
}

func (mock *rangeAwareHistoryMock) FetchDailyBarsRange(
	_ context.Context,
	_ string,
	_, _ time.Time,
	_ backtest.PriceAdjustment,
) ([]domain.DailyBar, error) {
	mock.rangeCalls++
	return append([]domain.DailyBar(nil), mock.rangeBars...), mock.rangeErr
}

func rangeHistoryBars(symbol, source string, start time.Time, count int) []domain.DailyBar {
	bars := make([]domain.DailyBar, count)
	for index := range bars {
		price := 10 + float64(index)/100
		bars[index] = domain.DailyBar{
			Symbol: symbol, Source: source, Date: start.AddDate(0, 0, index).Format("2006-01-02"),
			Open: price, Close: price, High: price + .2, Low: price - .2, Volume: 1000,
			Turnover: math.NaN(),
		}
	}
	return bars
}

func TestFallbackDailyHistoryRangeUsesNextSourceAfterFailure(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	first := &rangeAwareHistoryMock{rangeErr: errors.New("primary offline")}
	second := &rangeAwareHistoryMock{rangeBars: rangeHistoryBars("sh600519", "备用不复权", start, 120)}
	client := NewFallbackDailyHistoryClient(first, second)
	bars, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, start.AddDate(0, 0, 119), backtest.AdjustmentNone)
	if err != nil {
		t.Fatal(err)
	}
	if first.rangeCalls != 1 || second.rangeCalls != 1 || len(bars) != 120 || bars[0].Source != "备用不复权" {
		t.Fatalf("range fallback did not use the next source: first=%d second=%d bars=%#v", first.rangeCalls, second.rangeCalls, bars)
	}
}

func TestFallbackDailyHistoryRangeSkipsSourceThatDoesNotCoverRequestedWindow(t *testing.T) {
	requestedStart := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	partial := &rangeAwareHistoryMock{rangeBars: rangeHistoryBars("sh600519", "最新片段", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), 120)}
	complete := &rangeAwareHistoryMock{rangeBars: rangeHistoryBars("sh600519", "完整区间", requestedStart, 120)}
	client := NewFallbackDailyHistoryClient(partial, complete)
	bars, err := client.FetchDailyBarsRange(t.Context(), "sh600519", requestedStart, requestedStart.AddDate(0, 0, 119), backtest.AdjustmentNone)
	if err != nil {
		t.Fatal(err)
	}
	if partial.rangeCalls != 1 || complete.rangeCalls != 1 || len(bars) != 120 || bars[0].Source != "完整区间" {
		t.Fatalf("partial range source was accepted: partial=%d complete=%d bars=%#v", partial.rangeCalls, complete.rangeCalls, bars)
	}
}

func TestCachedDailyHistoryRangeUsesCoveringCacheBeforeOnlineSource(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	primary := &rangeAwareHistoryMock{rangeErr: errors.New("HTTP 501")}
	client := NewCachedDailyHistoryClient(primary, t.TempDir())
	client.now = func() time.Time { return time.Date(2026, 8, 28, 17, 0, 0, 0, time.Local) }
	if err := client.save("sh600519", rangeHistoryBars("sh600519", "东方财富不复权", start, 120)); err != nil {
		t.Fatal(err)
	}
	bars, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, start.AddDate(0, 0, 119), backtest.AdjustmentNone)
	if err != nil {
		t.Fatal(err)
	}
	if primary.rangeCalls != 0 || len(bars) != 120 || !strings.Contains(bars[0].Source, "缓存") {
		t.Fatalf("covering cache was not preferred: calls=%d bars=%#v", primary.rangeCalls, bars)
	}
}

func TestCachedDailyHistoryRangeUsesOnlineSourceWhenCacheDoesNotCover(t *testing.T) {
	requestedStart := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	primary := &rangeAwareHistoryMock{rangeBars: rangeHistoryBars("sh600519", "腾讯不复权", requestedStart, 120)}
	client := NewCachedDailyHistoryClient(primary, t.TempDir())
	client.now = func() time.Time { return time.Date(2026, 8, 28, 17, 0, 0, 0, time.Local) }
	recentStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := client.save("sh600519", rangeHistoryBars("sh600519", "东方财富不复权", recentStart, 120)); err != nil {
		t.Fatal(err)
	}
	bars, err := client.FetchDailyBarsRange(t.Context(), "sh600519", requestedStart, requestedStart.AddDate(0, 0, 119), backtest.AdjustmentNone)
	if err != nil {
		t.Fatal(err)
	}
	if primary.rangeCalls != 1 || len(bars) != 120 || bars[0].Source != "腾讯不复权" {
		t.Fatalf("incomplete cache blocked the online range source: calls=%d bars=%#v", primary.rangeCalls, bars)
	}
}
