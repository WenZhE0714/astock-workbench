package market

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
)

type stalledSource struct{}

func (stalledSource) Fetch(ctx context.Context, _ []string) ([]domain.Quote, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (stalledSource) FetchDailyBars(ctx context.Context, _ string) ([]domain.DailyBar, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (stalledSource) FetchDailyBarsRange(ctx context.Context, _ string, _, _ time.Time, _ backtest.PriceAdjustment) ([]domain.DailyBar, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (stalledSource) FetchMinutePoints(ctx context.Context, _ string) ([]domain.MinutePoint, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (stalledSource) FetchBoards(ctx context.Context, _ string) ([]domain.BoardFlow, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type availableSource struct{ calls int }

func (source *availableSource) check(ctx context.Context) error {
	source.calls++
	return ctx.Err()
}

func (source *availableSource) Fetch(ctx context.Context, symbols []string) ([]domain.Quote, error) {
	return []domain.Quote{{Symbol: symbols[0], Current: "10"}}, source.check(ctx)
}

func (source *availableSource) FetchDailyBars(ctx context.Context, _ string) ([]domain.DailyBar, error) {
	return cachedHistoryBars(), source.check(ctx)
}

func (source *availableSource) FetchDailyBarsRange(ctx context.Context, _ string, _, _ time.Time, _ backtest.PriceAdjustment) ([]domain.DailyBar, error) {
	return cachedHistoryBars(), source.check(ctx)
}

func (source *availableSource) FetchMinutePoints(ctx context.Context, _ string) ([]domain.MinutePoint, error) {
	return []domain.MinutePoint{{Price: 10, Leading: 10}}, source.check(ctx)
}

func (source *availableSource) FetchBoards(ctx context.Context, _ string) ([]domain.BoardFlow, error) {
	return []domain.BoardFlow{{Code: "BK0001"}}, source.check(ctx)
}

func TestFallbackReservesBudgetAndHonorsCancellation(t *testing.T) {
	for _, kind := range []string{"quote", "daily", "range", "minute", "index-minute", "boards"} {
		t.Run(kind, func(t *testing.T) {
			available := &availableSource{}
			fetch := func(ctx context.Context) error {
				var err error
				switch kind {
				case "quote":
					_, err = NewFallbackQuoteClient(stalledSource{}, available).Fetch(ctx, []string{"sh600519"})
				case "daily":
					_, err = NewFallbackDailyHistoryClient(stalledSource{}, available).FetchDailyBars(ctx, "sh600519")
				case "range":
					start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
					_, err = NewFallbackDailyHistoryClient(stalledSource{}, available).FetchDailyBarsRange(ctx, "sh600519", start, start.AddDate(0, 2, 8), backtest.AdjustmentNone)
				case "minute":
					_, err = NewFallbackMinuteClient(stalledSource{}, available).FetchMinutePoints(ctx, "sh600519")
				case "index-minute":
					_, err = NewMarketMinuteClient(available, stalledSource{}).FetchMinutePoints(ctx, "sh000001")
				case "boards":
					_, err = NewFallbackBoardFlowClient(stalledSource{}, available).FetchBoards(ctx, "sh600519")
				}
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
			defer cancel()
			if err := fetch(ctx); err != nil || ctx.Err() != nil || available.calls != 1 {
				t.Fatalf("fallback lost its budget: calls=%d err=%v parent=%v", available.calls, err, ctx.Err())
			}
			cancel()
			if err := fetch(ctx); !errors.Is(err, context.Canceled) || available.calls != 1 {
				t.Fatalf("canceled request reached fallback: calls=%d err=%v", available.calls, err)
			}
		})
	}
}
