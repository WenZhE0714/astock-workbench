package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const dailyHistoryLimit = 300
const dailyHistoryFetchAttempts = 2

const tencentDailyHistoryAPIURL = "https://web.ifzq.gtimg.cn/appstock/app/fqkline/get"

type klinePayload struct {
	Data *struct {
		Klines []string `json:"klines"`
	} `json:"data"`
}

type DailyHistoryClient interface {
	FetchDailyBars(context.Context, string) ([]domain.DailyBar, error)
}

func parseFiniteFloat(value string) (float64, bool) {
	result, err := strconv.ParseFloat(value, 64)
	return result, err == nil && !math.IsNaN(result) && !math.IsInf(result, 0)
}

// ParseDailyHistoryPayload parses Eastmoney's unadjusted (fqt=0) daily K-line
// response. Malformed rows are skipped so one bad upstream row cannot poison
// an otherwise usable history window.
func ParseDailyHistoryPayload(raw, symbol string) []domain.DailyBar {
	var payload klinePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Data == nil {
		return nil
	}
	bars := make([]domain.DailyBar, 0, len(payload.Data.Klines))
	for _, line := range payload.Data.Klines {
		parts := strings.Split(line, ",")
		if len(parts) < 11 || len(parts[0]) != len("2006-01-02") {
			continue
		}
		open, openOK := parseFiniteFloat(parts[1])
		closePrice, closeOK := parseFiniteFloat(parts[2])
		high, highOK := parseFiniteFloat(parts[3])
		low, lowOK := parseFiniteFloat(parts[4])
		volume, volumeOK := parseFiniteFloat(parts[5])
		amount, amountOK := parseFiniteFloat(parts[6])
		turnover, turnoverOK := parseFiniteFloat(parts[10])
		if !openOK || !closeOK || !highOK || !lowOK || !volumeOK || !amountOK ||
			open <= 0 || closePrice <= 0 || high <= 0 || low <= 0 || volume < 0 || amount < 0 {
			continue
		}
		if !turnoverOK {
			turnover = math.NaN()
		}
		bars = append(bars, domain.DailyBar{
			Symbol: symbol, Source: "东方财富", Date: parts[0], Open: open, Close: closePrice,
			High: high, Low: low, Volume: volume, Amount: amount, Turnover: turnover,
		})
	}
	sort.SliceStable(bars, func(i, j int) bool { return bars[i].Date < bars[j].Date })
	return bars
}

type tencentDailyHistoryPayload struct {
	Code int                        `json:"code"`
	Data map[string]json.RawMessage `json:"data"`
}

// ParseTencentDailyHistoryPayload parses Tencent's explicit `none` adjustment
// series. Each row is date/open/close/high/low/volume; amount and turnover are
// unavailable, but volume is sufficient for the strategy's liquidity checks.
func ParseTencentDailyHistoryPayload(raw, symbol string) []domain.DailyBar {
	var payload tencentDailyHistoryPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Code != 0 {
		return nil
	}
	itemRaw, ok := payload.Data[symbol]
	if !ok {
		return nil
	}
	var item struct {
		Day [][]json.RawMessage `json:"day"`
	}
	if err := json.Unmarshal(itemRaw, &item); err != nil {
		return nil
	}
	bars := make([]domain.DailyBar, 0, len(item.Day))
	for _, row := range item.Day {
		if len(row) < 6 {
			continue
		}
		values := make([]string, 6)
		valid := true
		for index := range values {
			if err := json.Unmarshal(row[index], &values[index]); err != nil {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		open, openOK := parseFiniteFloat(values[1])
		closePrice, closeOK := parseFiniteFloat(values[2])
		high, highOK := parseFiniteFloat(values[3])
		low, lowOK := parseFiniteFloat(values[4])
		volume, volumeOK := parseFiniteFloat(values[5])
		if !openOK || !closeOK || !highOK || !lowOK || !volumeOK ||
			open <= 0 || closePrice <= 0 || high <= 0 || low <= 0 || volume < 0 {
			continue
		}
		bars = append(bars, domain.DailyBar{
			Symbol: symbol, Source: "腾讯", Date: values[0], Open: open, Close: closePrice,
			High: high, Low: low, Volume: volume, Turnover: math.NaN(),
		})
	}
	sort.SliceStable(bars, func(i, j int) bool { return bars[i].Date < bars[j].Date })
	return bars
}

func dailyHistoryAddress(base, securityID string) string {
	values := url.Values{
		"secid":   {securityID},
		"klt":     {"101"},
		"fqt":     {"0"},
		"lmt":     {strconv.Itoa(dailyHistoryLimit)},
		"beg":     {"0"},
		"end":     {"20500000"},
		"fields1": {"f1,f2,f3,f4,f5,f6"},
		"fields2": {"f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61"},
	}
	if strings.Contains(base, "{secid}") {
		return strings.Replace(base, "{secid}", url.QueryEscape(securityID), 1)
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

func tencentDailyHistoryAddress(base, symbol string) string {
	if strings.Contains(base, "{symbol}") {
		return strings.Replace(base, "{symbol}", url.QueryEscape(symbol), 1)
	}
	values := url.Values{"param": {symbol + ",day,,," + strconv.Itoa(dailyHistoryLimit) + ",none"}}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

func fetchTencentDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	base := os.Getenv("ASTOCK_DAILY_HISTORY_TENCENT_API_URL")
	if base == "" {
		base = tencentDailyHistoryAPIURL
	}
	var lastError error
	for attempt := 0; attempt < dailyHistoryFetchAttempts; attempt++ {
		requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		raw, err := fetchDecoded(requestContext, tencentDailyHistoryAddress(base, symbol), nil)
		cancel()
		if err != nil {
			lastError = err
			continue
		}
		bars := ParseTencentDailyHistoryPayload(raw, symbol)
		if len(bars) >= 60 {
			return bars, nil
		}
		lastError = fmt.Errorf("%s 的腾讯日 K 仅返回 %d 根有效数据", symbol, len(bars))
	}
	return nil, lastError
}

// FetchDailyBars lets TencentClient serve as a direct, unadjusted history
// source for latency-sensitive market scans without waiting on other fallbacks.
func (TencentClient) FetchDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	return fetchTencentDailyBars(ctx, symbol)
}

func (EastmoneyClient) FetchDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	securityID := eastmoneySecurityID(symbol)
	if securityID == "" {
		return nil, fmt.Errorf("无效股票代码 %q", symbol)
	}
	configuredBase := os.Getenv("ASTOCK_DAILY_HISTORY_API_URL")
	bases := []string{configuredBase}
	if configuredBase == "" {
		bases = []string{klineHistoryAPIURL, klineAPIURL, klineFallbackAPIURL}
	}
	var lastError error
	for _, base := range bases {
		for attempt := 0; attempt < dailyHistoryFetchAttempts; attempt++ {
			requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
			raw, err := fetchDecoded(requestContext, dailyHistoryAddress(base, securityID), nil)
			cancel()
			if err != nil {
				lastError = err
				continue
			}
			bars := ParseDailyHistoryPayload(raw, symbol)
			if len(bars) < 60 {
				lastError = fmt.Errorf("%s 仅返回 %d 根有效日 K，至少需要 60 根", symbol, len(bars))
				continue
			}
			return bars, nil
		}
	}
	if bars, err := fetchTencentDailyBars(ctx, symbol); err == nil {
		return bars, nil
	} else if lastError == nil {
		lastError = err
	} else {
		lastError = fmt.Errorf("东方财富: %v；腾讯: %w", lastError, err)
	}
	if lastError == nil {
		lastError = fmt.Errorf("未获取到 %s 的历史日 K", symbol)
	}
	return nil, lastError
}
