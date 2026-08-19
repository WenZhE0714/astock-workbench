package backtest

import (
	"context"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type cachedBarRange struct {
	start time.Time
	end   time.Time
	bars  []domain.DailyBar
}

// CachingDailyBarProvider keeps fetched ranges in memory for one research
// process. Optimizing many parameter candidates must not multiply HTTP calls.
type CachingDailyBarProvider struct {
	upstream DailyBarProvider
	mu       sync.RWMutex
	ranges   map[string][]cachedBarRange
}

func NewCachingDailyBarProvider(upstream DailyBarProvider) *CachingDailyBarProvider {
	return &CachingDailyBarProvider{upstream: upstream, ranges: make(map[string][]cachedBarRange)}
}

func (provider *CachingDailyBarProvider) FetchDailyBarsRange(
	ctx context.Context,
	symbol string,
	start, end time.Time,
	adjustment PriceAdjustment,
) ([]domain.DailyBar, error) {
	key := string(adjustment) + ":" + symbol
	provider.mu.RLock()
	for _, item := range provider.ranges[key] {
		if !start.Before(item.start) && !end.After(item.end) {
			bars := barsWithin(item.bars, start, end)
			provider.mu.RUnlock()
			return bars, nil
		}
	}
	provider.mu.RUnlock()

	bars, err := provider.upstream.FetchDailyBarsRange(ctx, symbol, start, end, adjustment)
	if err != nil {
		return nil, err
	}
	stored := append([]domain.DailyBar(nil), bars...)
	provider.mu.Lock()
	provider.ranges[key] = append(provider.ranges[key], cachedBarRange{start: start, end: end, bars: stored})
	provider.mu.Unlock()
	return append([]domain.DailyBar(nil), bars...), nil
}

func barsWithin(bars []domain.DailyBar, start, end time.Time) []domain.DailyBar {
	startDate := start.Format("2006-01-02")
	endDate := end.Format("2006-01-02")
	result := make([]domain.DailyBar, 0, len(bars))
	for _, bar := range bars {
		if bar.Date >= startDate && bar.Date <= endDate {
			result = append(result, bar)
		}
	}
	return result
}
