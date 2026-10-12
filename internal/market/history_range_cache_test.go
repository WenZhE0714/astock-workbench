package market

import (
	"context"
	"errors"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
)

type incrementalHistorySource struct {
	bars  []domain.DailyBar
	calls []historyDateSpan
	err   error
}

func (source *incrementalHistorySource) FetchDailyBars(context.Context, string) ([]domain.DailyBar, error) {
	return source.bars, source.err
}

func (source *incrementalHistorySource) FetchDailyBarsRange(_ context.Context, _ string, start, end time.Time, _ backtest.PriceAdjustment) ([]domain.DailyBar, error) {
	source.calls = append(source.calls, historyDateSpan{start, end})
	return dailyBarsInRange(source.bars, start, end), source.err
}

func TestRangeCachePersistsExtendsAndSurvivesSnapshotRefresh(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	source := &incrementalHistorySource{bars: rangeHistoryBars("sh600519", "原始日K", start, 1000)}
	dir := t.TempDir()
	client := NewCachedDailyHistoryClient(source, dir)
	firstEnd := start.AddDate(0, 0, 699)
	if _, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, firstEnd, backtest.AdjustmentNone); err != nil {
		t.Fatal(err)
	}
	if err := client.save("sh600519", rangeHistoryBars("sh600519", "近期快照", start.AddDate(6, 0, 0), 60)); err != nil {
		t.Fatal(err)
	}
	client = NewCachedDailyHistoryClient(source, dir)
	source.err = errors.New("offline")
	bars, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, firstEnd, backtest.AdjustmentNone)
	if err != nil || len(bars) != 700 || len(source.calls) != 1 || !strings.Contains(bars[0].Source, "缓存") || !math.IsNaN(bars[0].Turnover) {
		t.Fatalf("restart lost history: len=%d calls=%d err=%v", len(bars), len(source.calls), err)
	}
	source.err = nil
	end := start.AddDate(0, 0, 799)
	bars, err = client.FetchDailyBarsRange(t.Context(), "sh600519", start, end, backtest.AdjustmentNone)
	if err != nil || len(bars) != 800 || len(source.calls) != 2 || !source.calls[1].start.Equal(start.AddDate(0, 0, 520)) || !source.calls[1].end.Equal(end) {
		t.Fatalf("extension fetched wrong interval: calls=%+v len=%d err=%v", source.calls, len(bars), err)
	}
	entries, err := os.ReadDir(client.rangeCacheDir("sh600519"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("range snapshots missing: %v %v", entries, err)
	}
}

func TestRangeCacheBackfillsInternalUncachedInterval(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	source := &incrementalHistorySource{bars: rangeHistoryBars("sh600519", "日K", start, 1000)}
	client := NewCachedDailyHistoryClient(source, t.TempDir())
	for _, span := range []historyDateSpan{{start, start.AddDate(0, 0, 299)}, {start.AddDate(0, 0, 600), start.AddDate(0, 0, 899)}} {
		if _, err := client.FetchDailyBarsRange(t.Context(), "sh600519", span.start, span.end, backtest.AdjustmentNone); err != nil {
			t.Fatal(err)
		}
	}
	bars, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, start.AddDate(0, 0, 899), backtest.AdjustmentNone)
	if err != nil || len(bars) != 900 || len(source.calls) != 3 || !source.calls[2].start.Equal(start.AddDate(0, 0, 120)) || !source.calls[2].end.Equal(start.AddDate(0, 0, 779)) {
		t.Fatalf("internal gap was treated as covered: calls=%+v len=%d err=%v", source.calls, len(bars), err)
	}
}

func TestRangeCacheDoesNotPromoteIntradayAndExpires(t *testing.T) {
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	source := &incrementalHistorySource{bars: rangeHistoryBars("sh600519", "日K", start, 130)}
	client := NewCachedDailyHistoryClient(source, t.TempDir())
	now := time.Date(2026, 10, 8, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	client.now = func() time.Time { return now }
	end := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	bars, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, end, backtest.AdjustmentNone)
	if err != nil || bars[len(bars)-1].Date != "2026-10-07" {
		t.Fatalf("intraday bar cached: %v", err)
	}
	now = now.Add(6 * time.Hour)
	source.bars[len(source.bars)-1].Close += .1
	bars, err = client.FetchDailyBarsRange(t.Context(), "sh600519", start, end, backtest.AdjustmentNone)
	if err != nil || len(source.calls) != 2 || bars[len(bars)-1].Date != "2026-10-08" || bars[len(bars)-1].Close != source.bars[len(source.bars)-1].Close {
		t.Fatalf("close did not refresh: %v calls=%d", err, len(source.calls))
	}
	now = now.Add(15 * 24 * time.Hour)
	source.err = errors.New("offline")
	if _, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, end, backtest.AdjustmentNone); err == nil {
		t.Fatal("expired cache accepted as fresh")
	}
}

func TestRangeCacheRefreshesWholeWindowWhenOverlapChanges(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	source := &incrementalHistorySource{bars: rangeHistoryBars("sh600519", "旧数据", start, 900)}
	client := NewCachedDailyHistoryClient(source, t.TempDir())
	if _, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, start.AddDate(0, 0, 699), backtest.AdjustmentNone); err != nil {
		t.Fatal(err)
	}
	for i := range source.bars {
		source.bars[i].Open *= 2
		source.bars[i].Close *= 2
		source.bars[i].High *= 2
		source.bars[i].Low *= 2
	}
	bars, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, start.AddDate(0, 0, 799), backtest.AdjustmentNone)
	if err != nil || len(source.calls) != 3 || !source.calls[2].start.Equal(start) || bars[0].Close != source.bars[0].Close {
		t.Fatalf("mixed corrected and old history: %v calls=%+v", err, source.calls)
	}
}

func TestRangeCacheRejectsInvalidAndConflictingBars(t *testing.T) {
	for _, kind := range []string{"conflict", "symbol", "ohlc", "nan"} {
		t.Run(kind, func(t *testing.T) {
			start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			source := &incrementalHistorySource{bars: rangeHistoryBars("sh600519", "日K", start, 120)}
			switch kind {
			case "conflict":
				bar := source.bars[20]
				bar.Volume++
				source.bars = append(source.bars, bar)
			case "symbol":
				source.bars[20].Symbol = "sh600000"
			case "ohlc":
				source.bars[20].High = 1
			case "nan":
				source.bars[20].Volume = math.NaN()
			}
			client := NewCachedDailyHistoryClient(source, t.TempDir())
			if _, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, start.AddDate(0, 0, 119), backtest.AdjustmentNone); err == nil {
				t.Fatal("invalid interval accepted")
			}
			if _, err := os.Stat(client.rangeCacheDir("sh600519")); !os.IsNotExist(err) {
				t.Fatalf("invalid evidence persisted: %v", err)
			}
		})
	}
}

func TestRangeCacheRetriesUnpublishedTailAndRejectsUnsafeSymbols(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	all := rangeHistoryBars("sh600519", "日K", start, 400)
	source := &incrementalHistorySource{bars: all[:399]}
	client := NewCachedDailyHistoryClient(source, t.TempDir())
	end := start.AddDate(0, 0, 399)
	if _, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, end, backtest.AdjustmentNone); err != nil {
		t.Fatal(err)
	}
	source.bars = all
	bars, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, end, backtest.AdjustmentNone)
	if err != nil || len(bars) != 400 || len(source.calls) != 2 || !source.calls[1].start.After(start) {
		t.Fatalf("unpublished final day marked complete: bars=%d calls=%+v err=%v", len(bars), source.calls, err)
	}
	for _, symbol := range []string{"../aaaaa", "sh../../", "xx600519"} {
		if _, err := client.FetchDailyBarsRange(t.Context(), symbol, start, end, backtest.AdjustmentNone); err == nil {
			t.Fatalf("unsafe symbol %q accepted", symbol)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.FetchDailyBarsRange(ctx, "sh600519", start, end, backtest.AdjustmentNone); err != context.Canceled {
		t.Fatalf("canceled cache read: %v", err)
	}
}

func TestRangeCachePersistsReusedRecentSnapshotWithoutRenewingItsAge(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	source := &incrementalHistorySource{bars: rangeHistoryBars("sh600519", "日K", start, 800)}
	client := NewCachedDailyHistoryClient(source, t.TempDir())
	now := time.Date(2026, 10, 10, 16, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }
	if _, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, start.AddDate(0, 0, 299), backtest.AdjustmentNone); err != nil {
		t.Fatal(err)
	}
	if err := client.save("sh600519", source.bars[300:]); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * 24 * time.Hour)
	end := start.AddDate(0, 0, 799)
	if _, err := client.FetchDailyBarsRange(t.Context(), "sh600519", start, end, backtest.AdjustmentNone); err != nil {
		t.Fatal(err)
	}
	// Evict the recent snapshot and go offline; both parts must survive restart.
	if err := client.save("sh600519", rangeHistoryBars("sh600519", "近期", start.AddDate(6, 0, 0), 100)); err != nil {
		t.Fatal(err)
	}
	restarted := NewCachedDailyHistoryClient(source, client.dir)
	restarted.now = client.now
	source.err = errors.New("offline")
	bars, err := restarted.FetchDailyBarsRange(t.Context(), "sh600519", start, end, backtest.AdjustmentNone)
	if err != nil || len(bars) != 800 || len(source.calls) != 1 {
		t.Fatalf("borrowed snapshot was lost: len=%d calls=%d err=%v", len(bars), len(source.calls), err)
	}
	now = now.Add(5 * 24 * time.Hour)
	if _, err := restarted.FetchDailyBarsRange(t.Context(), "sh600519", start, end, backtest.AdjustmentNone); err == nil {
		t.Fatal("reused snapshot age was renewed")
	}
}
