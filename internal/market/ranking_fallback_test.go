package market

import (
	"context"
	"errors"
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
