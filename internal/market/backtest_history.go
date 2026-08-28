package market

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
)

const (
	backtestHistoryLimit        = 5000
	tencentBacktestHistoryLimit = 1000
	// Tencent returns at most 1000 daily bars and some deployments ignore a
	// range that is too wide. 900 calendar days stays comfortably below that
	// cap while still keeping the number of requests bounded for multi-year
	// walk-forward studies.
	tencentBacktestChunkCalendarDays = 900
)

func backtestHistoryAddress(base, securityID string, start, end time.Time) string {
	if strings.Contains(base, "{secid}") {
		base = strings.Replace(base, "{secid}", url.QueryEscape(securityID), 1)
	}
	values := url.Values{
		"secid":   {securityID},
		"klt":     {"101"},
		"fqt":     {"0"},
		"lmt":     {strconv.Itoa(backtestHistoryLimit)},
		"beg":     {start.Format("20060102")},
		"end":     {end.Format("20060102")},
		"fields1": {"f1,f2,f3,f4,f5,f6"},
		"fields2": {"f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61"},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

func tencentBacktestHistoryAddress(base, symbol string, start, end time.Time) string {
	if strings.Contains(base, "{symbol}") {
		base = strings.Replace(base, "{symbol}", url.QueryEscape(symbol), 1)
	}
	values := url.Values{
		"param": {strings.Join([]string{
			symbol, "day", start.Format("2006-01-02"), end.Format("2006-01-02"),
			strconv.Itoa(tencentBacktestHistoryLimit), "none",
		}, ",")},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

func fetchTencentDailyBarsRangeOnce(ctx context.Context, symbol string, start, end time.Time) ([]domain.DailyBar, error) {
	base := os.Getenv("ASTOCK_BACKTEST_HISTORY_TENCENT_API_URL")
	if base == "" {
		// Keep the backtest path aligned with the endpoint override used by
		// ordinary daily-history consumers. Deployments commonly configure one
		// proxy for both paths.
		base = os.Getenv("ASTOCK_DAILY_HISTORY_TENCENT_API_URL")
	}
	if base == "" {
		base = tencentDailyHistoryAPIURL
	}
	var lastError error
	for attempt := 0; attempt < dailyHistoryFetchAttempts; attempt++ {
		requestContext, cancel := context.WithTimeout(ctx, 12*time.Second)
		raw, fetchError := fetchDecoded(requestContext, tencentBacktestHistoryAddress(base, symbol, start, end), nil)
		cancel()
		if fetchError != nil {
			lastError = fetchError
			continue
		}
		bars := ParseTencentDailyHistoryPayload(raw, symbol)
		if len(bars) == 0 {
			lastError = fmt.Errorf("%s 未返回腾讯回测日K", symbol)
			continue
		}
		for index := range bars {
			bars[index].Source = "腾讯不复权"
		}
		return bars, nil
	}
	if lastError == nil {
		lastError = fmt.Errorf("%s 腾讯回测日K暂不可用", symbol)
	}
	return nil, lastError
}

// fetchTencentDailyBarsRange deliberately splits long requests. The endpoint's
// `lmt=1000` limit is easy to hit during a four-year training window; treating
// that response as a hard failure made one old/active ticker abort every
// candidate in a continuous optimization run. Each chunk is independently
// checked for truncation, then merged by trading date so the engine receives a
// single deterministic series.
func fetchTencentDailyBarsRange(ctx context.Context, symbol string, start, end time.Time) ([]domain.DailyBar, error) {
	if start.IsZero() || end.IsZero() || start.After(end) {
		return nil, fmt.Errorf("%s 腾讯回测日期区间无效", symbol)
	}
	merged := make(map[string]domain.DailyBar)
	chunkStart := start
	for !chunkStart.After(end) {
		chunkEnd := chunkStart.AddDate(0, 0, tencentBacktestChunkCalendarDays-1)
		if chunkEnd.After(end) {
			chunkEnd = end
		}
		bars, err := fetchTencentDailyBarsRangeOnce(ctx, symbol, chunkStart, chunkEnd)
		if err != nil {
			return nil, fmt.Errorf("%s 腾讯回测日K分段 %s~%s: %w", symbol,
				chunkStart.Format("2006-01-02"), chunkEnd.Format("2006-01-02"), err)
		}
		if tencentBacktestRangeTruncated(bars, chunkStart) {
			return nil, fmt.Errorf("%s 腾讯回测日K分段 %s~%s 仍超过%d根上限", symbol,
				chunkStart.Format("2006-01-02"), chunkEnd.Format("2006-01-02"), tencentBacktestHistoryLimit)
		}
		for _, bar := range bars {
			if bar.Date < start.Format("2006-01-02") || bar.Date > end.Format("2006-01-02") {
				continue
			}
			bar.Source = "腾讯不复权"
			merged[bar.Date] = bar
		}
		chunkStart = chunkEnd.AddDate(0, 0, 1)
	}
	if len(merged) == 0 {
		return nil, fmt.Errorf("%s 腾讯回测日K分段合并后为空", symbol)
	}
	result := make([]domain.DailyBar, 0, len(merged))
	for _, bar := range merged {
		result = append(result, bar)
	}
	sort.SliceStable(result, func(left, right int) bool { return result[left].Date < result[right].Date })
	return result, nil
}

func tencentBacktestRangeTruncated(bars []domain.DailyBar, start time.Time) bool {
	return len(bars) == tencentBacktestHistoryLimit && len(bars) > 0 && bars[0].Date > start.Format("2006-01-02")
}

// FetchDailyBarsRange fetches a fixed unadjusted series for auditable
// backtests. Adjusted modes stay rejected until their corporate-action basis
// can be persisted alongside each run.
func (EastmoneyClient) FetchDailyBarsRange(
	ctx context.Context,
	symbol string,
	start, end time.Time,
	adjustment backtest.PriceAdjustment,
) ([]domain.DailyBar, error) {
	if adjustment != backtest.AdjustmentNone {
		return nil, fmt.Errorf("当前回测仅支持固定的不复权口径")
	}
	securityID := eastmoneySecurityID(symbol)
	if securityID == "" {
		return nil, fmt.Errorf("无效股票代码 %q", symbol)
	}
	if start.IsZero() || end.IsZero() || start.After(end) {
		return nil, fmt.Errorf("无效回测日期区间")
	}
	// The strategy only needs OHLCV. Tencent's explicit `none` series is the
	// primary path here because it returns the requested range in one call;
	// Eastmoney remains a fallback with amount and turnover context.
	tencentBars, tencentError := fetchTencentDailyBarsRange(ctx, symbol, start, end)
	if tencentError == nil {
		return tencentBars, nil
	}
	bases := []string{klineHistoryAPIURL, klineAPIURL, klineFallbackAPIURL}
	if configured := os.Getenv("ASTOCK_BACKTEST_HISTORY_API_URL"); configured != "" {
		bases = []string{configured}
	} else if configured := os.Getenv("ASTOCK_DAILY_HISTORY_API_URL"); configured != "" {
		// A single configured Eastmoney-compatible proxy should be honored by
		// both the normal snapshot path and long-range research.
		bases = []string{configured}
	}
	var lastError error
	for _, base := range bases {
		for attempt := 0; attempt < dailyHistoryFetchAttempts; attempt++ {
			requestContext, cancel := context.WithTimeout(ctx, 12*time.Second)
			raw, fetchError := fetchDecoded(requestContext, backtestHistoryAddress(base, securityID, start, end), nil)
			cancel()
			if fetchError != nil {
				lastError = fetchError
				continue
			}
			bars := ParseDailyHistoryPayload(raw, symbol)
			if len(bars) == 0 {
				lastError = fmt.Errorf("%s 未返回回测日K", symbol)
				continue
			}
			for index := range bars {
				bars[index].Source = "东方财富不复权"
			}
			return bars, nil
		}
	}
	if lastError == nil {
		lastError = tencentError
	} else {
		lastError = fmt.Errorf("腾讯: %v；东方财富: %w", tencentError, lastError)
	}
	return nil, lastError
}
