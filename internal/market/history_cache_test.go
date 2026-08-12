package market

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type switchableHistoryMock struct {
	bars []domain.DailyBar
	err  error
}

func (mock *switchableHistoryMock) FetchDailyBars(context.Context, string) ([]domain.DailyBar, error) {
	return mock.bars, mock.err
}

func cachedHistoryBars() []domain.DailyBar {
	bars := make([]domain.DailyBar, 65)
	for index := range bars {
		bars[index] = domain.DailyBar{
			Symbol: "sh600519", Source: "腾讯", Date: fmt.Sprintf("2026-%02d-%02d", 1+index/28, 1+index%28),
			Open: 10, Close: 10, High: 11, Low: 9, Volume: 100, Turnover: math.NaN(),
		}
	}
	return bars
}

func TestCachedDailyHistoryFallsBackAfterOnlineFailure(t *testing.T) {
	primary := &switchableHistoryMock{bars: cachedHistoryBars()}
	client := NewCachedDailyHistoryClient(primary, t.TempDir())
	now := time.Date(2026, 8, 12, 9, 0, 0, 0, time.Local)
	client.now = func() time.Time { return now }
	if _, err := client.FetchDailyBars(context.Background(), "sh600519"); err != nil {
		t.Fatal(err)
	}
	primary.bars = nil
	primary.err = fmt.Errorf("HTTP 501")
	bars, err := client.FetchDailyBars(context.Background(), "sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 65 || !strings.Contains(bars[len(bars)-1].Source, "缓存") || !math.IsNaN(bars[0].Turnover) {
		t.Fatalf("unexpected cached bars: %#v", bars[len(bars)-1])
	}
}

func TestCachedDailyHistoryRejectsExpiredCache(t *testing.T) {
	primary := &switchableHistoryMock{bars: cachedHistoryBars()}
	client := NewCachedDailyHistoryClient(primary, t.TempDir())
	now := time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local)
	client.now = func() time.Time { return now }
	if _, err := client.FetchDailyBars(context.Background(), "sh600519"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(15 * 24 * time.Hour)
	primary.bars = nil
	primary.err = fmt.Errorf("offline")
	if _, err := client.FetchDailyBars(context.Background(), "sh600519"); err == nil || !strings.Contains(err.Error(), "缓存已过期") {
		t.Fatalf("unexpected expired cache error: %v", err)
	}
}
