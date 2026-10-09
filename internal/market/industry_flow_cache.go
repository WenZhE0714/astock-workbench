package market

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const industryFlowCacheTTL = time.Minute
const industryFlowRetryInterval = 15 * time.Second

type IndustryFlowCacheError struct {
	FetchedAt time.Time
	Cause     error
}

func (err *IndustryFlowCacheError) Error() string {
	return fmt.Sprintf("行业扩散刷新失败，暂用 %s 的行业列表缓存", err.FetchedAt.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("15:04:05"))
}

func (err *IndustryFlowCacheError) Unwrap() error { return err.Cause }

// Share complete snapshots across cockpit, board-list and CLI requests.
// Expired data is never returned as current input to sentiment scoring.
type CachedIndustryFlowClient struct {
	primary   IndustryFlowClient
	now       func() time.Time
	mu        sync.Mutex
	inflight  chan struct{}
	flows     map[string]domain.BoardFlow
	fetchedAt time.Time
	failedAt  time.Time
	lastError error
}

func NewCachedIndustryFlowClient(primary IndustryFlowClient) *CachedIndustryFlowClient {
	return &CachedIndustryFlowClient{primary: primary, now: time.Now}
}

func copyIndustryFlows(flows map[string]domain.BoardFlow) map[string]domain.BoardFlow {
	result := make(map[string]domain.BoardFlow, len(flows))
	for name, flow := range flows {
		result[name] = flow
	}
	return result
}

func (client *CachedIndustryFlowClient) FetchIndustryFlows(ctx context.Context) (map[string]domain.BoardFlow, error) {
	if client == nil || client.primary == nil {
		return nil, fmt.Errorf("行业资金流数据源未初始化")
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		now := client.now()
		location := time.FixedZone("Asia/Shanghai", 8*60*60)
		client.mu.Lock()
		age := now.Sub(client.fetchedAt)
		if len(client.flows) > 0 && age >= 0 && age < industryFlowCacheTTL &&
			now.In(location).Format("2006-01-02") == client.fetchedAt.In(location).Format("2006-01-02") {
			flows := copyIndustryFlows(client.flows)
			client.mu.Unlock()
			return flows, nil
		}
		if client.lastError != nil && now.Sub(client.failedAt) >= 0 && now.Sub(client.failedAt) < industryFlowRetryInterval {
			flows, err := client.fallbackLocked(now, client.lastError)
			client.mu.Unlock()
			return flows, err
		}
		if inflight := client.inflight; inflight != nil {
			client.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-inflight:
				continue
			}
		}
		client.inflight = make(chan struct{})
		client.mu.Unlock()

		flows, err := client.primary.FetchIndustryFlows(ctx)
		if err == nil && len(flows) == 0 {
			err = fmt.Errorf("行业资金流暂无完整数据")
		}
		client.mu.Lock()
		if err == nil {
			client.flows, client.fetchedAt = copyIndustryFlows(flows), client.now()
			client.lastError = nil
		} else if ctx.Err() == nil {
			client.lastError, client.failedAt = err, client.now()
		}
		close(client.inflight)
		client.inflight = nil
		if err != nil {
			flows, err = client.fallbackLocked(client.now(), err)
		}
		client.mu.Unlock()
		return flows, err
	}
}

func (client *CachedIndustryFlowClient) fallbackLocked(now time.Time, cause error) (map[string]domain.BoardFlow, error) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	age := now.Sub(client.fetchedAt)
	if len(client.flows) > 0 && age >= 0 && age < 5*time.Minute &&
		now.In(location).Format("2006-01-02") == client.fetchedAt.In(location).Format("2006-01-02") {
		return copyIndustryFlows(client.flows), &IndustryFlowCacheError{FetchedAt: client.fetchedAt, Cause: cause}
	}
	return nil, cause
}
