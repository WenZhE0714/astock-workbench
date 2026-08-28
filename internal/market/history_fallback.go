package market

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
)

// DailyBarRangeClient is the range-aware counterpart of DailyHistoryClient.
// Keeping the interface in the market package lets the application compose
// cache and provider fallbacks without coupling ordinary quote consumers to
// the backtest engine implementation.
type DailyBarRangeClient interface {
	FetchDailyBarsRange(context.Context, string, time.Time, time.Time, backtest.PriceAdjustment) ([]domain.DailyBar, error)
}

// FallbackDailyHistoryClient keeps the execution history path deterministic:
// prefer the richer source, then use a lower-latency source when it is offline.
type FallbackDailyHistoryClient struct {
	clients      []DailyHistoryClient
	rangeClients []DailyBarRangeClient
}

func NewFallbackDailyHistoryClient(clients ...DailyHistoryClient) *FallbackDailyHistoryClient {
	filtered := make([]DailyHistoryClient, 0, len(clients))
	rangeClients := make([]DailyBarRangeClient, 0, len(clients))
	for _, client := range clients {
		if client != nil {
			filtered = append(filtered, client)
			if rangeClient, ok := client.(DailyBarRangeClient); ok {
				rangeClients = append(rangeClients, rangeClient)
			}
		}
	}
	return &FallbackDailyHistoryClient{clients: filtered, rangeClients: rangeClients}
}

func (client *FallbackDailyHistoryClient) FetchDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	if client == nil || len(client.clients) == 0 {
		return nil, fmt.Errorf("日K回退源未初始化")
	}
	errors := make([]string, 0, len(client.clients))
	for _, source := range client.clients {
		bars, err := source.FetchDailyBars(ctx, symbol)
		if err == nil && len(bars) >= 60 {
			return bars, nil
		}
		if err != nil {
			errors = append(errors, err.Error())
		} else {
			errors = append(errors, fmt.Sprintf("仅返回%d根有效日K", len(bars)))
		}
	}
	return nil, fmt.Errorf("日K数据源均不可用: %s", strings.Join(errors, "；"))
}

// FetchDailyBarsRange applies the same source order as FetchDailyBars while
// requiring each source to cover the requested interval. A short or partial
// response is treated as a source failure so the next provider can be tried.
func (client *FallbackDailyHistoryClient) FetchDailyBarsRange(
	ctx context.Context,
	symbol string,
	start, end time.Time,
	adjustment backtest.PriceAdjustment,
) ([]domain.DailyBar, error) {
	if client == nil {
		return nil, fmt.Errorf("日K区间回退源未初始化")
	}
	rangeClients := client.rangeClients
	if len(rangeClients) == 0 {
		// Be tolerant of values assembled by older callers or package-local
		// tests that initialized only the original clients field.
		for _, source := range client.clients {
			if rangeSource, ok := source.(DailyBarRangeClient); ok {
				rangeClients = append(rangeClients, rangeSource)
			}
		}
	}
	if len(rangeClients) == 0 {
		return nil, fmt.Errorf("日K区间回退源未初始化")
	}
	failures := make([]string, 0, len(rangeClients))
	for _, source := range rangeClients {
		bars, err := source.FetchDailyBarsRange(ctx, symbol, start, end, adjustment)
		if err == nil {
			if covered, ok := dailyBarsCoverRange(bars, start, end); ok {
				return covered, nil
			}
			failures = append(failures, fmt.Sprintf("仅返回%d根或未覆盖请求区间", len(bars)))
			continue
		}
		if err != nil {
			failures = append(failures, err.Error())
		} else {
			failures = append(failures, fmt.Sprintf("仅返回%d根有效区间日K", len(bars)))
		}
	}
	return nil, fmt.Errorf("日K区间数据源均不可用: %s", strings.Join(failures, "；"))
}
