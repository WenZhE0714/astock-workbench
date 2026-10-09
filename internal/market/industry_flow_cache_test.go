package market

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type industryFlowFunc func(context.Context) (map[string]domain.BoardFlow, error)

func (fn industryFlowFunc) FetchIndustryFlows(ctx context.Context) (map[string]domain.BoardFlow, error) {
	return fn(ctx)
}

func TestIndustryCacheFreshnessAndFailureCooldown(t *testing.T) {
	calls := 0
	fail := false
	client := NewCachedIndustryFlowClient(industryFlowFunc(func(context.Context) (map[string]domain.BoardFlow, error) {
		calls++
		if fail {
			return nil, fmt.Errorf("offline")
		}
		return map[string]domain.BoardFlow{"sample": {Name: "sample"}}, nil
	}))
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }
	first, _ := client.FetchIndustryFlows(context.Background())
	delete(first, "sample")
	second, err := client.FetchIndustryFlows(context.Background())
	if err != nil || len(second) != 1 || calls != 1 {
		t.Fatalf("cache was mutated or missed: %v %v calls=%d", second, err, calls)
	}
	now = now.Add(time.Minute)
	fail = true
	for index := 0; index < 2; index++ {
		flows, err := client.FetchIndustryFlows(context.Background())
		var cached *IndustryFlowCacheError
		if !errors.As(err, &cached) || len(flows) != 1 || calls != 2 || cached.FetchedAt != now.Add(-time.Minute) {
			t.Fatalf("missing dated fallback or cooldown: %v %v calls=%d", flows, err, calls)
		}
	}
	now = now.Add(industryFlowRetryInterval)
	fail = false
	if _, err := client.FetchIndustryFlows(context.Background()); err != nil || calls != 3 {
		t.Fatalf("source not retried after cooldown: calls=%d err=%v", calls, err)
	}
	now = now.Add(5 * time.Minute)
	fail = true
	if flows, err := client.FetchIndustryFlows(context.Background()); err == nil || len(flows) != 0 {
		t.Fatalf("cache older than five minutes reused: %+v %v", flows, err)
	}
}

func TestIndustryCacheCoalescesRequestsAndAllowsCanceledWaiter(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	client := NewCachedIndustryFlowClient(industryFlowFunc(func(context.Context) (map[string]domain.BoardFlow, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return map[string]domain.BoardFlow{"sample": {Name: "sample"}}, nil
	}))
	finished := make(chan error, 2)
	go func() { _, err := client.FetchIndustryFlows(context.Background()); finished <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := client.FetchIndustryFlows(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting caller did not cancel: %v", err)
	}
	go func() { _, err := client.FetchIndustryFlows(context.Background()); finished <- err }()
	close(release)
	for index := 0; index < 2; index++ {
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("duplicate upstream requests: %d", calls.Load())
	}
}
