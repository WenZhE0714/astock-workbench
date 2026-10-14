package market

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type StockScanClient interface {
	FetchStocks(context.Context, []string) ([]domain.MarketStockSnapshot, error)
}

type FallbackMarketScanClient struct {
	MarketScanClient
	stocks   StockScanClient
	rankings MarketRankingClient
}

func NewFallbackMarketScanClient(primary MarketScanClient, stocks StockScanClient, rankings MarketRankingClient) FallbackMarketScanClient {
	return FallbackMarketScanClient{MarketScanClient: primary, stocks: stocks, rankings: rankings}
}

func (client FallbackMarketScanClient) FetchStocks(ctx context.Context, symbols []string) ([]domain.MarketStockSnapshot, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	bySymbol := make(map[string]domain.MarketStockSnapshot)
	missing := append([]string(nil), symbols...)
	failures := make([]string, 0, 2)
	for index, source := range []StockScanClient{client.MarketScanClient, client.stocks} {
		if source == nil || len(missing) == 0 {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		timeout := 2 * time.Second
		if index == 1 {
			timeout = 10 * time.Second
		}
		requestCtx, cancel := fallbackContext(ctx, 2-index, timeout)
		items, err := source.FetchStocks(requestCtx, missing)
		cancel()
		if err != nil {
			failures = append(failures, err.Error())
		}
		wanted := make(map[string]bool, len(missing))
		for _, symbol := range missing {
			wanted[symbol] = true
		}
		for _, item := range items {
			if wanted[item.Symbol] && finiteTHS(item.Price) && item.Price > 0 {
				bySymbol[item.Symbol] = item
			}
		}
		remaining := make([]string, 0)
		for _, symbol := range missing {
			if _, ok := bySymbol[symbol]; !ok {
				remaining = append(remaining, symbol)
			}
		}
		missing = remaining
	}
	result := make([]domain.MarketStockSnapshot, 0, len(bySymbol))
	seen := make(map[string]bool)
	for _, symbol := range symbols {
		if item, ok := bySymbol[symbol]; ok && !seen[symbol] {
			result = append(result, item)
			seen[symbol] = true
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if len(missing) != 0 {
		return result, fmt.Errorf("扫描点查未覆盖 %s: %s", strings.Join(missing, ","), strings.Join(failures, "；"))
	}
	return result, nil
}

func (client FallbackMarketScanClient) FetchStockRanking(ctx context.Context, metric domain.MarketScanMetric, descending bool, limit int) ([]domain.MarketStockSnapshot, error) {
	primaryCtx, cancel := fallbackContext(ctx, 2, 2*time.Second)
	items, err := client.MarketScanClient.FetchStockRanking(primaryCtx, metric, descending, limit)
	cancel()
	if err == nil && len(items) > 0 {
		return items, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var kind domain.MarketRankingKind
	switch {
	case metric == domain.MarketScanByPercent && descending:
		kind = domain.MarketRankingGainers
	case metric == domain.MarketScanByPercent:
		kind = domain.MarketRankingLosers
	case metric == domain.MarketScanByAmount && descending:
		kind = domain.MarketRankingAmount
	default:
		return nil, fmt.Errorf("扫描榜单 %s 无可用备用源: %v", metric, err)
	}
	if client.rankings == nil {
		return nil, fmt.Errorf("扫描备用榜单未配置: %v", err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	ranked, err := client.rankings.FetchMarketRanking(requestCtx, kind, limit)
	if err != nil {
		return nil, err
	}
	items = make([]domain.MarketStockSnapshot, 0, len(ranked))
	for _, item := range ranked {
		items = append(items, domain.MarketStockSnapshot{
			Symbol: item.Symbol, Source: item.Source, Name: item.Name, Industry: item.Industry,
			Price: item.Price, Percent: item.Percent, Amount: item.Amount, Turnover: item.Turnover, Speed: item.Speed,
			VolumeRatio: math.NaN(), High: math.NaN(), Low: math.NaN(), Open: math.NaN(), PreviousClose: math.NaN(), MarketCap: math.NaN(), MainNet: math.NaN(), MainRatio: math.NaN(),
		})
	}
	return items, nil
}

func (client FallbackMarketScanClient) FetchIndustryRanking(ctx context.Context, metric domain.MarketScanMetric, descending bool, limit int) ([]domain.BoardFlow, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return client.MarketScanClient.FetchIndustryRanking(ctx, metric, descending, limit)
}

func (client FallbackMarketScanClient) FetchIndustryFlows(ctx context.Context) (map[string]domain.BoardFlow, error) {
	source, ok := client.MarketScanClient.(IndustryFlowClient)
	if !ok {
		return nil, fmt.Errorf("完整行业资金未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return source.FetchIndustryFlows(ctx)
}
