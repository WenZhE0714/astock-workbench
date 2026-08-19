package market

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
)

const (
	backtestHistoryLimit        = 5000
	tencentBacktestHistoryLimit = 1000
)

func backtestHistoryAddress(base, securityID string, start, end time.Time) string {
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

func fetchTencentDailyBarsRange(ctx context.Context, symbol string, start, end time.Time) ([]domain.DailyBar, error) {
	base := os.Getenv("ASTOCK_BACKTEST_HISTORY_TENCENT_API_URL")
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
		if tencentBacktestRangeTruncated(tencentBars, start) {
			tencentError = fmt.Errorf("%s 腾讯回测日K达到%d根上限且未覆盖预热起点 %s", symbol, tencentBacktestHistoryLimit, start.Format("2006-01-02"))
		} else {
			return tencentBars, nil
		}
	}
	bases := []string{klineHistoryAPIURL, klineAPIURL, klineFallbackAPIURL}
	if configured := os.Getenv("ASTOCK_BACKTEST_HISTORY_API_URL"); configured != "" {
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
