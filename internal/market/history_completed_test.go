package market

import (
	"context"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type completedHistoryStub struct {
	calls int
	bars  []domain.DailyBar
}

func (s *completedHistoryStub) FetchDailyBars(context.Context, string) ([]domain.DailyBar, error) {
	s.calls++
	return s.bars, nil
}

func TestCompletedHistoryCacheRequiresActualCloseFetch(t *testing.T) {
	bars := cachedHistoryBars()
	bars[len(bars)-1].Date = "2026-10-15"
	primary := &completedHistoryStub{bars: bars}
	client := NewCachedDailyHistoryClient(primary, t.TempDir())
	at := time.Date(2026, 10, 15, 14, 58, 0, 0, time.FixedZone("CST", 8*60*60))
	client.now = func() time.Time { return at }
	if _, err := client.FetchDailyBars(context.Background(), "sh600519"); err != nil {
		t.Fatal(err)
	}
	at = at.Add(40 * time.Minute)
	if _, err := client.FetchCompletedDailyBars(context.Background(), "sh600519"); err != nil {
		t.Fatal(err)
	}
	if primary.calls != 2 {
		t.Fatal("intraday cache used as completed close")
	}
	if _, err := client.FetchCompletedDailyBars(context.Background(), "sh600519"); err != nil {
		t.Fatal(err)
	}
	if primary.calls != 2 {
		t.Fatal("completed same-day cache was refetched")
	}
	at = at.AddDate(0, 0, 1)
	if _, err := client.FetchCompletedDailyBars(context.Background(), "sh600519"); err != nil {
		t.Fatal(err)
	}
	if primary.calls != 3 {
		t.Fatal("previous day's close reused as current")
	}
}
