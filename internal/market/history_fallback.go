package market

import (
	"context"
	"fmt"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

// FallbackDailyHistoryClient keeps the execution history path deterministic:
// prefer the richer source, then use a lower-latency source when it is offline.
type FallbackDailyHistoryClient struct {
	clients []DailyHistoryClient
}

func NewFallbackDailyHistoryClient(clients ...DailyHistoryClient) *FallbackDailyHistoryClient {
	filtered := make([]DailyHistoryClient, 0, len(clients))
	for _, client := range clients {
		if client != nil {
			filtered = append(filtered, client)
		}
	}
	return &FallbackDailyHistoryClient{clients: filtered}
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
