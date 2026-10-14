package market

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type rankingFunc func(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error)

func (fn rankingFunc) FetchMarketRanking(ctx context.Context, kind domain.MarketRankingKind, limit int) ([]domain.MarketRankingItem, error) {
	return fn(ctx, kind, limit)
}

func TestRankingFallbackReservesTimeForIndependentSource(t *testing.T) {
	primary := rankingFunc(func(ctx context.Context, _ domain.MarketRankingKind, _ int) ([]domain.MarketRankingItem, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	backup := rankingFunc(func(ctx context.Context, kind domain.MarketRankingKind, limit int) ([]domain.MarketRankingItem, error) {
		if ctx.Err() != nil || kind != domain.MarketRankingGainers || limit != 2 {
			t.Fatal("incorrect backup request or exhausted deadline")
		}
		return []domain.MarketRankingItem{{Symbol: "sh600001", Percent: 2, Source: THSMarketRankingSource}, {Symbol: "sz000001", Percent: 1, Source: THSMarketRankingSource}}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	items, err := NewFallbackMarketRankingClient(primary, backup).FetchMarketRanking(ctx, domain.MarketRankingGainers, 2)
	if err != nil || len(items) != 2 || items[0].Source != THSMarketRankingSource {
		t.Fatalf("fallback failed: %v %v", items, err)
	}
}

func TestRankingFallbackUsesWholeSnapshotAndSkipsBackupWhenComplete(t *testing.T) {
	for _, complete := range []bool{true, false} {
		calls := 0
		primary := rankingFunc(func(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error) {
			items := []domain.MarketRankingItem{{Symbol: "sh600001", Percent: 20}}
			if complete {
				items = append(items, domain.MarketRankingItem{Symbol: "sz000001", Percent: 19})
			}
			return items, nil
		})
		backup := rankingFunc(func(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error) {
			calls++
			return []domain.MarketRankingItem{{Symbol: "sz300001", Percent: 10}, {Symbol: "sz300002", Percent: 9}}, nil
		})
		items, err := NewFallbackMarketRankingClient(primary, backup).FetchMarketRanking(context.Background(), domain.MarketRankingGainers, 2)
		if err != nil || len(items) != 2 {
			t.Fatalf("fetch failed: %+v %v", items, err)
		}
		if complete && calls != 0 {
			t.Fatal("unneeded fallback request")
		}
		if !complete && (calls != 1 || items[0].Symbol != "sz300001") {
			t.Fatal("ranking snapshots were mixed")
		}
	}
}

func TestRankingFallbackRejectsUnknownKindAndPropagatesCancellation(t *testing.T) {
	client := NewFallbackMarketRankingClient(rankingFunc(func(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error) {
		t.Fatal("unexpected request")
		return nil, nil
	}))
	if _, err := client.FetchMarketRanking(context.Background(), "invalid", 20); err == nil {
		t.Fatal("accepted unknown kind")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.FetchMarketRanking(ctx, domain.MarketRankingGainers, 20); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestRankingFailureNamesSourceAndOmitsRequestURL(t *testing.T) {
	err := fmt.Errorf("request failed: %w", &url.Error{Op: "Get", URL: "https://ranking.test/?fields=" + strings.Repeat("f22,", 100), Err: io.EOF})
	for _, test := range []struct {
		source MarketRankingClient
		name   string
	}{
		{EastmoneyClient{}, "东方财富"},
		{&EastmoneyClient{}, "东方财富"},
		{THSMarketRankingClient{}, "同花顺"},
		{&THSMarketRankingClient{}, "同花顺"},
	} {
		if got := rankingFailure(test.source, 0, err); got != test.name+": EOF" {
			t.Fatalf("verbose or unnamed error: %s", got)
		}
	}
}

func TestRankingFallbackReportsEachFailureOnSeparateLine(t *testing.T) {
	primary := rankingFunc(func(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error) {
		return nil, &url.Error{Op: "Get", URL: "https://ranking.test/?fields=f22", Err: io.EOF}
	})
	backup := rankingFunc(func(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error) {
		return nil, fmt.Errorf("%w: 排序与请求不符", errTHSRankingSnapshot)
	})
	_, err := NewFallbackMarketRankingClient(primary, backup).FetchMarketRanking(context.Background(), domain.MarketRankingRapidRise, 20)
	want := "榜单数据源均不可用:\n数据源1: EOF\n数据源2: 同花顺榜单快照无效: 排序与请求不符"
	if err == nil || err.Error() != want {
		t.Fatalf("source failures lost: %v", err)
	}
}

func TestRankingFallbackKeepsFailuresWhenCanceled(t *testing.T) {
	for _, cancelAt := range []int{0, 1} {
		t.Run(fmt.Sprintf("source=%d", cancelAt), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			source := rankingFunc(func(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error) {
				if calls == cancelAt {
					cancel()
				}
				calls++
				return nil, io.EOF
			})
			_, err := NewFallbackMarketRankingClient(source, source).FetchMarketRanking(ctx, domain.MarketRankingRapidRise, 20)
			if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "数据源1: EOF") || calls != cancelAt+1 {
				t.Fatalf("cancellation or failures lost: calls=%d err=%v", calls, err)
			}
			if cancelAt == 1 && !strings.Contains(err.Error(), "数据源2: EOF") {
				t.Fatalf("last failure lost: %v", err)
			}
		})
	}
}
