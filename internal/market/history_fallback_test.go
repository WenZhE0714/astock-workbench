package market

import (
	"context"
	"fmt"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestFallbackDailyHistoryUsesNextSourceAfterFailure(t *testing.T) {
	first := &switchableHistoryMock{err: fmt.Errorf("offline")}
	second := &switchableHistoryMock{bars: cachedHistoryBars()}
	bars, err := NewFallbackDailyHistoryClient(first, second).FetchDailyBars(context.Background(), "sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) != 65 || bars[0].Source != "腾讯" {
		t.Fatalf("fallback source was not used: %#v", bars)
	}
}

func TestFallbackDailyHistoryRejectsShortSeries(t *testing.T) {
	short := &switchableHistoryMock{bars: []domain.DailyBar{{Symbol: "sh600519", Date: "2026-08-25"}}}
	_, err := NewFallbackDailyHistoryClient(short).FetchDailyBars(context.Background(), "sh600519")
	if err == nil {
		t.Fatal("short history was accepted")
	}
}
