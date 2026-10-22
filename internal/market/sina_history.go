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
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

// SinaDailyHistoryClient uses the original-price daily series. It intentionally
// implements recent history only; it cannot stand in for a full backtest range.
type SinaDailyHistoryClient struct{}

func sinaDailyHistoryAddress(base, symbol string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("symbol", symbol)
	q.Set("scale", "240")
	q.Set("ma", "no")
	q.Set("datalen", strconv.Itoa(dailyHistoryLimit))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func ParseSinaDailyHistoryPayload(raw, symbol string) ([]domain.DailyBar, error) {
	var payload struct {
		Result struct {
			Status struct {
				Code int `json:"code"`
			} `json:"status"`
			Data []struct {
				Day                            string `json:"day"`
				Open, Close, High, Low, Volume json.RawMessage
			} `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("新浪日K响应格式无效: %w", err)
	}
	if payload.Result.Status.Code != 0 {
		return nil, fmt.Errorf("新浪日K错误 %d", payload.Result.Status.Code)
	}
	bars := make([]domain.DailyBar, 0, len(payload.Result.Data))
	seen := make(map[string]bool)
	for _, row := range payload.Result.Data {
		if _, err := time.Parse(time.DateOnly, row.Day); err != nil || seen[row.Day] {
			continue
		}
		o, c, h, l, v := rawNumber(row.Open), rawNumber(row.Close), rawNumber(row.High), rawNumber(row.Low), rawNumber(row.Volume)
		valid := true
		for _, value := range []float64{o, c, h, l, v} {
			valid = valid && !math.IsNaN(value) && !math.IsInf(value, 0)
		}
		if !valid || o <= 0 || c <= 0 || h < math.Max(o, c) || l > math.Min(o, c) || l <= 0 || v < 0 {
			continue
		}
		seen[row.Day] = true
		// The feed has volume but no amount/turnover; never derive cash turnover from close*volume.
		bars = append(bars, domain.DailyBar{Symbol: symbol, Source: "新浪未复权", Date: row.Day, Open: o, Close: c, High: h, Low: l, Volume: v, Turnover: math.NaN()})
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Date < bars[j].Date })
	return bars, nil
}

func (SinaDailyHistoryClient) FetchDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	if !ValidPrefixedSymbol(symbol) {
		return nil, fmt.Errorf("无效股票代码 %q", symbol)
	}
	base := os.Getenv("ASTOCK_DAILY_HISTORY_SINA_API_URL")
	if base == "" {
		base = sinaIndexKLineAPIURL
	}
	address, err := sinaDailyHistoryAddress(base, symbol)
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	raw, err := fetchDecodedWithHeaders(requestCtx, address, nil, map[string]string{"Referer": "https://finance.sina.com.cn/"})
	if err != nil {
		return nil, fmt.Errorf("新浪日K: %w", err)
	}
	bars, err := ParseSinaDailyHistoryPayload(raw, symbol)
	if err != nil {
		return nil, err
	}
	if len(bars) < 60 {
		return nil, fmt.Errorf("%s 新浪日K仅返回%d根有效数据", symbol, len(bars))
	}
	return bars, nil
}
