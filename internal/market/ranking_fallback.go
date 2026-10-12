package market

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func normalizedRankingLimit(limit int) int {
	if limit < 1 {
		return 20
	}
	return min(limit, 100)
}

func rankingMetric(item domain.MarketRankingItem, kind domain.MarketRankingKind) float64 {
	switch kind {
	case domain.MarketRankingRapidRise:
		return item.Speed
	case domain.MarketRankingAmount:
		return item.Amount
	case domain.MarketRankingTurnover:
		return item.Turnover
	default:
		return item.Percent
	}
}

func validRankingOrder(items []domain.MarketRankingItem, kind domain.MarketRankingKind) bool {
	for index, item := range items {
		value := rankingMetric(item, kind)
		if !finiteTHS(value) {
			return false
		}
		if index == 0 {
			continue
		}
		previous := rankingMetric(items[index-1], kind)
		if kind == domain.MarketRankingLosers {
			if value < previous {
				return false
			}
		} else if value > previous {
			return false
		}
	}
	return true
}

type FallbackMarketRankingClient struct{ clients []MarketRankingClient }

func NewFallbackMarketRankingClient(clients ...MarketRankingClient) FallbackMarketRankingClient {
	result := FallbackMarketRankingClient{}
	for _, client := range clients {
		if client != nil {
			result.clients = append(result.clients, client)
		}
	}
	return result
}

// Rankings are whole-provider snapshots. Combining providers would not
// preserve the ordering of the requested market-wide top N.
func (client FallbackMarketRankingClient) FetchMarketRanking(ctx context.Context, kind domain.MarketRankingKind, limit int) ([]domain.MarketRankingItem, error) {
	if _, _, err := marketRankingSort(kind); err != nil {
		return nil, err
	}
	limit = normalizedRankingLimit(limit)
	failures := make([]string, 0, len(client.clients))
	for index, source := range client.clients {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		requestCtx, cancel := fallbackContext(ctx, len(client.clients)-index, 2*time.Second)
		items, err := source.FetchMarketRanking(requestCtx, kind, limit)
		cancel()
		if err == nil && len(items) >= limit && validRankingOrder(items, kind) {
			return items[:limit], nil
		}
		if err != nil {
			failures = append(failures, err.Error())
		} else {
			failures = append(failures, fmt.Sprintf("榜单不完整或排序无效（%d/%d）", len(items), limit))
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(failures) == 0 {
		return nil, fmt.Errorf("榜单数据源未初始化")
	}
	return nil, fmt.Errorf("榜单数据源均不可用: %s", strings.Join(failures, "；"))
}
