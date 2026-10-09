package market

import (
	"context"
	"fmt"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

// FallbackBoardFlowClient tries independent related-board providers in order.
// Empty responses are not considered successful because callers cannot tell
// an empty membership from a provider outage.
type FallbackBoardFlowClient struct {
	clients []BoardFlowClient
}

func NewFallbackBoardFlowClient(clients ...BoardFlowClient) FallbackBoardFlowClient {
	usable := make([]BoardFlowClient, 0, len(clients))
	for _, client := range clients {
		if client != nil {
			usable = append(usable, client)
		}
	}
	return FallbackBoardFlowClient{clients: usable}
}

func (client FallbackBoardFlowClient) FetchBoards(ctx context.Context, symbol string) ([]domain.BoardFlow, error) {
	for index, source := range client.clients {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		requestContext, cancel := fallbackContext(ctx, len(client.clients)-index, 5*time.Second)
		items, err := source.FetchBoards(requestContext, symbol)
		cancel()
		if err == nil && len(items) > 0 {
			return items, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	if len(client.clients) == 0 {
		return nil, fmt.Errorf("关联板块数据源未初始化")
	}
	return nil, fmt.Errorf("关联板块数据源均不可用（已尝试东方财富、同花顺）")
}
