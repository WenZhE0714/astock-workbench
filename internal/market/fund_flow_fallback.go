package market

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type FallbackFundFlowClient struct {
	clients []FundFlowClient
}

func NewFallbackFundFlowClient(clients ...FundFlowClient) FallbackFundFlowClient {
	result := FallbackFundFlowClient{}
	for _, client := range clients {
		if client != nil {
			result.clients = append(result.clients, client)
		}
	}
	return result
}

func validFundFlow(flow domain.FundFlow) bool {
	return !math.IsNaN(flow.MainNet) && !math.IsInf(flow.MainNet, 0)
}

func missingFundFlowSymbols(symbols []string, flows map[string]domain.FundFlow) []string {
	missing := make([]string, 0)
	seen := make(map[string]bool)
	for _, symbol := range symbols {
		if seen[symbol] {
			continue
		}
		seen[symbol] = true
		if flow, ok := flows[symbol]; !ok || !validFundFlow(flow) {
			missing = append(missing, symbol)
		}
	}
	return missing
}

func fundFlowCoverageError(symbols []string, flows map[string]domain.FundFlow, failures []string) error {
	missing := missingFundFlowSymbols(symbols, flows)
	if len(missing) == 0 {
		return nil
	}
	if len(flows) > 0 {
		indicesOnly := true
		for _, symbol := range missing {
			indicesOnly = indicesOnly && IsBroadMarketSymbol(symbol)
		}
		if indicesOnly {
			return fmt.Errorf("指数资金暂不可用: %s", strings.Join(missing, ","))
		}
		return fmt.Errorf("部分资金流暂不可用: %s", strings.Join(missing, ","))
	}
	if len(failures) == 0 {
		failures = append(failures, "未返回有效资金流")
	}
	return fmt.Errorf("资金流数据源均不可用: %s", strings.Join(failures, "；"))
}

// Fetch fills missing symbols without replacing valid preferred-provider rows.
// Partial coverage remains visible even if indices or other stocks fail.
func (client FallbackFundFlowClient) Fetch(ctx context.Context, symbols []string) (map[string]domain.FundFlow, error) {
	flows := make(map[string]domain.FundFlow)
	missing := missingFundFlowSymbols(symbols, flows)
	failures := make([]string, 0, len(client.clients))
	for index, source := range client.clients {
		if len(missing) == 0 {
			break
		}
		if ctx.Err() != nil {
			return flows, ctx.Err()
		}
		requestCtx, cancel := fallbackContext(ctx, len(client.clients)-index, 3*time.Second)
		items, err := source.Fetch(requestCtx, missing)
		cancel()
		for _, symbol := range missing {
			if flow, ok := items[symbol]; ok && flow.Symbol == symbol && validFundFlow(flow) {
				flows[symbol] = flow
			}
		}
		if err != nil {
			failures = append(failures, err.Error())
		}
		missing = missingFundFlowSymbols(symbols, flows)
	}
	if ctx.Err() != nil {
		return flows, ctx.Err()
	}
	return flows, fundFlowCoverageError(symbols, flows, failures)
}
