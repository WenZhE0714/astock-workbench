package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
)

type historyRangeSnapshot struct {
	Start string `json:"start"`
	End   string `json:"end"`
	dailyHistoryCachePayload
}

type historyDateSpan struct{ start, end time.Time }

var historyCacheSymbol = regexp.MustCompile(`^(sh|sz|bj)[0-9]{6}$`)

func completedHistoryDate(now time.Time) string {
	local := now.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	if local.Hour() < 15 {
		local = local.AddDate(0, 0, -1)
	}
	return local.Format(time.DateOnly)
}

func validatedRangeBars(symbol string, bars []domain.DailyBar, start, end string) ([]domain.DailyBar, error) {
	result := []domain.DailyBar{}
	for _, bar := range sortedDailyBars(bars) {
		if bar.Date < start || bar.Date > end {
			continue
		}
		_, dateErr := time.Parse(time.DateOnly, bar.Date)
		valid := dateErr == nil && bar.Symbol == symbol && bar.Low > 0 && bar.Low <= math.Min(bar.Open, bar.Close) && bar.High >= math.Max(bar.Open, bar.Close)
		for _, value := range []float64{bar.Open, bar.Close, bar.High, bar.Low, bar.Volume, bar.Amount} {
			valid = valid && !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
		}
		if !valid {
			return nil, fmt.Errorf("%s %s 包含无效日K", symbol, bar.Date)
		}
		if len(result) > 0 && result[len(result)-1].Date == bar.Date {
			old := result[len(result)-1]
			if old.Open != bar.Open || old.Close != bar.Close || old.High != bar.High || old.Low != bar.Low || old.Volume != bar.Volume || old.Amount != bar.Amount {
				return nil, fmt.Errorf("%s %s 存在冲突日K", symbol, bar.Date)
			}
			continue
		}
		result = append(result, bar)
	}
	return result, nil
}

func (client *CachedDailyHistoryClient) rangeCacheDir(symbol string) string {
	return filepath.Join(client.dir, "ranges", "none", symbol)
}

func (client *CachedDailyHistoryClient) saveRangeSnapshot(symbol string, snapshot historyRangeSnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if err := writeFileAtomically(filepath.Join(client.rangeCacheDir(symbol), snapshot.Start+"_"+snapshot.End+".json"), data); err != nil {
		return fmt.Errorf("保存区间日K缓存: %w", err)
	}
	return nil
}

func (client *CachedDailyHistoryClient) loadRangeSnapshots(symbol string) ([]historyRangeSnapshot, error) {
	entries, err := os.ReadDir(client.rangeCacheDir(symbol))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	snapshots := []historyRangeSnapshot{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(client.rangeCacheDir(symbol), entry.Name()))
		if err != nil {
			return nil, err
		}
		var snapshot historyRangeSnapshot
		// Corrupt or expired cache entries are misses, never research evidence.
		if json.Unmarshal(data, &snapshot) != nil {
			continue
		}
		start, startErr := time.Parse(time.DateOnly, snapshot.Start)
		end, endErr := time.Parse(time.DateOnly, snapshot.End)
		age := client.now().Sub(snapshot.FetchedAt)
		if startErr != nil || endErr != nil || start.After(end) || snapshot.FetchedAt.IsZero() || age < 0 || age > dailyHistoryCacheMaxAge || snapshot.End > completedHistoryDate(snapshot.FetchedAt) {
			continue
		}
		bars, err := validatedRangeBars(symbol, decodeCachedBars(snapshot.Bars), snapshot.Start, snapshot.End)
		if err != nil || len(bars) == 0 {
			continue
		}
		snapshot.Bars = encodeCachedBars(bars)
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

func missingHistorySpans(start, end time.Time, snapshots []historyRangeSnapshot) []historyDateSpan {
	spans := []historyDateSpan{}
	for _, snapshot := range snapshots {
		left, _ := time.Parse(time.DateOnly, snapshot.Start)
		right, _ := time.Parse(time.DateOnly, snapshot.End)
		spans = append(spans, historyDateSpan{left, right})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start.Before(spans[j].start) })
	missing, cursor := []historyDateSpan{}, start
	for _, span := range spans {
		if span.end.Before(cursor) || span.start.After(end) {
			continue
		}
		if span.start.After(cursor) {
			missing = append(missing, historyDateSpan{cursor, span.start.AddDate(0, 0, -1)})
		}
		cursor = span.end.AddDate(0, 0, 1)
	}
	if !cursor.After(end) {
		missing = append(missing, historyDateSpan{cursor, end})
	}
	return missing
}

func mergeHistorySnapshots(snapshots []historyRangeSnapshot, start, end time.Time) []domain.DailyBar {
	sort.SliceStable(snapshots, func(i, j int) bool { return snapshots[i].FetchedAt.Before(snapshots[j].FetchedAt) })
	merged := map[string]domain.DailyBar{}
	for _, snapshot := range snapshots {
		// Replace the entire refreshed interval, including newly missing dates.
		for date := range merged {
			if date >= snapshot.Start && date <= snapshot.End {
				delete(merged, date)
			}
		}
		for i, bar := range decodeCachedBars(snapshot.Bars) {
			bar.Source = snapshot.Bars[i].Source
			merged[bar.Date] = bar
		}
	}
	bars := make([]domain.DailyBar, 0, len(merged))
	for _, bar := range merged {
		bars = append(bars, bar)
	}
	return dailyBarsInRange(sortedDailyBars(bars), start, end)
}

func historyOverlapChanged(old, fresh []domain.DailyBar) bool {
	byDate := map[string]domain.DailyBar{}
	for _, bar := range old {
		byDate[bar.Date] = bar
	}
	for _, bar := range fresh {
		previous, exists := byDate[bar.Date]
		if exists && (previous.Open != bar.Open || previous.Close != bar.Close || previous.High != bar.High || previous.Low != bar.Low || previous.Volume != bar.Volume) {
			return true
		}
	}
	return false
}

func (client *CachedDailyHistoryClient) fetchPersistentRange(ctx context.Context, symbol string, start, end time.Time, adjustment backtest.PriceAdjustment) ([]domain.DailyBar, error) {
	if client == nil || !historyCacheSymbol.MatchString(symbol) {
		return nil, fmt.Errorf("日K区间数据源或证券代码无效")
	}
	if adjustment != backtest.AdjustmentNone || start.IsZero() || end.IsZero() || start.After(end) {
		return nil, fmt.Errorf("日K区间仅支持有效日期及不复权口径")
	}
	start, _ = time.Parse(time.DateOnly, start.Format(time.DateOnly))
	end, _ = time.Parse(time.DateOnly, min(end.Format(time.DateOnly), completedHistoryDate(client.now())))
	if start.After(end) {
		return nil, fmt.Errorf("请求区间没有已收盘日K")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshots, err := client.loadRangeSnapshots(symbol)
	if err != nil {
		return nil, fmt.Errorf("读取区间缓存: %w", err)
	}
	if len(missingHistorySpans(start, end, snapshots)) > 0 {
		if cached, fetchedAt, err := client.loadWithTimestamp(symbol); err == nil {
			cached, err = validatedRangeBars(symbol, cached, start.Format(time.DateOnly), end.Format(time.DateOnly))
			if err == nil && len(cached) > 0 && !historyOverlapChanged(mergeHistorySnapshots(snapshots, start, end), cached) {
				snapshot := historyRangeSnapshot{Start: cached[0].Date, End: cached[len(cached)-1].Date, dailyHistoryCachePayload: dailyHistoryCachePayload{FetchedAt: fetchedAt, Bars: encodeCachedBars(cached)}}
				if err := client.saveRangeSnapshot(symbol, snapshot); err != nil {
					return nil, err
				}
				snapshots = append(snapshots, snapshot)
			}
		}
	}
	for _, span := range missingHistorySpans(start, end, snapshots) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Range providers require enough bars even for a one-day extension.
		left := max(start.Format(time.DateOnly), span.start.AddDate(0, 0, -180).Format(time.DateOnly))
		right := min(end.Format(time.DateOnly), span.end.AddDate(0, 0, 180).Format(time.DateOnly))
		fetchStart, _ := time.Parse(time.DateOnly, left)
		fetchEnd, _ := time.Parse(time.DateOnly, right)
		bars, err := client.fetchOnlineRange(ctx, symbol, fetchStart, fetchEnd, adjustment)
		if err != nil {
			return nil, fmt.Errorf("补取 %s 至 %s: %w", left, right, err)
		}
		bars, err = validatedRangeBars(symbol, bars, left, right)
		if err != nil {
			return nil, err
		}
		if historyOverlapChanged(mergeHistorySnapshots(snapshots, fetchStart, fetchEnd), bars) && (left != start.Format(time.DateOnly) || right != end.Format(time.DateOnly)) {
			// A corrected overlap cannot safely be spliced into an older series.
			left, right = start.Format(time.DateOnly), end.Format(time.DateOnly)
			bars, err = client.fetchOnlineRange(ctx, symbol, start, end, adjustment)
			if err != nil {
				return nil, fmt.Errorf("重叠日K变更，完整区间刷新失败: %w", err)
			}
			bars, err = validatedRangeBars(symbol, bars, left, right)
			if err != nil {
				return nil, err
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// An upstream that has not published the requested final day must remain
		// eligible for a tail fetch, even when its response passed holiday tolerance.
		cachedEnd := min(right, bars[len(bars)-1].Date)
		snapshot := historyRangeSnapshot{Start: left, End: cachedEnd, dailyHistoryCachePayload: dailyHistoryCachePayload{FetchedAt: client.now(), Bars: encodeCachedBars(bars)}}
		if err := client.saveRangeSnapshot(symbol, snapshot); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return mergeHistorySnapshots(snapshots, start, end), ctx.Err()
}
