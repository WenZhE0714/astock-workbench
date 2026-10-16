package market

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

func TestOfflineHistoryReadsWithoutNetworkOrMutation(t *testing.T) {
	primary := &completedHistoryStub{bars: cachedHistoryBars()}
	client := NewCachedDailyHistoryClient(primary, t.TempDir())
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }
	if err := client.save("sh600519", primary.bars); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(client.cachePath("sh600519"))
	start, _ := time.Parse(time.DateOnly, "2026-01-01")
	end, _ := time.Parse(time.DateOnly, "2026-03-10")
	bars, err := client.ReadCachedDailyBarsRange(context.Background(), "sh600519", start, end, backtest.AdjustmentNone)
	if err != nil || len(bars) != 65 || primary.calls != 0 {
		t.Fatalf("offline read: %d %v %d", len(bars), err, primary.calls)
	}
	after, _ := os.ReadFile(client.cachePath("sh600519"))
	if string(before) != string(after) {
		t.Fatal("cache mutated")
	}
	if _, err := client.ReadCachedDailyBarsRange(context.Background(), "sh600000", start, end, backtest.AdjustmentNone); err == nil || primary.calls != 0 {
		t.Fatal("missing cache triggered network or passed")
	}
}
