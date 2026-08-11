package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const sinaIndexKLineAPIURL = "https://quotes.sina.cn/cn/api/openapi.php/CN_MarketDataService.getKLineData"

type MarketAmountClient interface {
	FetchPreviousMarketAmount(context.Context) (domain.MarketAmountSnapshot, error)
}

type SinaAmountClient struct{}

type sinaIndexKLinePayload struct {
	Result struct {
		Status struct {
			Code int `json:"code"`
		} `json:"status"`
		Data []struct {
			Day    string `json:"day"`
			Amount string `json:"amount"`
		} `json:"data"`
	} `json:"result"`
}

// ParseSinaPreviousAmountPayload sums all five-minute bars for the latest
// trading day before referenceDate. Sina amounts are yuan and are normalized
// to the Tencent quote convention of ten-thousand yuan.
func ParseSinaPreviousAmountPayload(raw, referenceDate string) (string, float64, bool) {
	var payload sinaIndexKLinePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Result.Status.Code != 0 {
		return "", math.NaN(), false
	}
	totals := make(map[string]float64)
	latest := ""
	for _, item := range payload.Result.Data {
		if len(item.Day) < 10 {
			continue
		}
		tradeDate := item.Day[:10]
		if tradeDate >= referenceDate {
			continue
		}
		amount, err := strconv.ParseFloat(item.Amount, 64)
		if err != nil || amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
			continue
		}
		totals[tradeDate] += amount
		if tradeDate > latest {
			latest = tradeDate
		}
	}
	amount := totals[latest]
	if latest == "" || amount <= 0 {
		return "", math.NaN(), false
	}
	return latest, amount / 1e4, true
}

func sinaAmountAddress(base, symbol string) string {
	if strings.Contains(base, "{symbol}") {
		return strings.Replace(base, "{symbol}", url.QueryEscape(symbol), 1)
	}
	values := url.Values{
		"symbol":  {symbol},
		"scale":   {"5"},
		"ma":      {"no"},
		"datalen": {"200"},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

type sinaAmountResult struct {
	symbol    string
	tradeDate string
	amount    float64
	err       error
}

func fetchSinaPreviousAmount(ctx context.Context, base, symbol, referenceDate string) (string, float64, error) {
	requestContext, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	raw, err := fetchDecoded(requestContext, sinaAmountAddress(base, symbol), nil)
	if err != nil {
		return "", math.NaN(), err
	}
	tradeDate, amount, ok := ParseSinaPreviousAmountPayload(raw, referenceDate)
	if !ok {
		return "", math.NaN(), fmt.Errorf("新浪未返回 %s 的上一交易日成交额", symbol)
	}
	return tradeDate, amount, nil
}

func (SinaAmountClient) FetchPreviousMarketAmount(ctx context.Context) (domain.MarketAmountSnapshot, error) {
	base := os.Getenv("ASTOCK_SINA_AMOUNT_HISTORY_API_URL")
	if base == "" {
		base = sinaIndexKLineAPIURL
	}
	referenceDate := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
	results := make(chan sinaAmountResult, len(MarketAmountSymbols))
	for _, symbol := range MarketAmountSymbols {
		symbol := symbol
		go func() {
			tradeDate, amount, err := fetchSinaPreviousAmount(ctx, base, symbol, referenceDate)
			results <- sinaAmountResult{symbol: symbol, tradeDate: tradeDate, amount: amount, err: err}
		}()
	}

	snapshot := domain.MarketAmountSnapshot{Source: "新浪指数5分钟行情"}
	for range MarketAmountSymbols {
		select {
		case result := <-results:
			if result.err != nil {
				return domain.MarketAmountSnapshot{}, result.err
			}
			if snapshot.TradeDate == "" {
				snapshot.TradeDate = result.tradeDate
			} else if snapshot.TradeDate != result.tradeDate {
				return domain.MarketAmountSnapshot{}, fmt.Errorf("沪深京成交额交易日不一致: %s / %s", snapshot.TradeDate, result.tradeDate)
			}
			switch result.symbol {
			case "sh000001":
				snapshot.Shanghai = result.amount
			case "sz399106":
				snapshot.Shenzhen = result.amount
			case "bj899050":
				snapshot.Beijing = result.amount
			}
		case <-ctx.Done():
			return domain.MarketAmountSnapshot{}, ctx.Err()
		}
	}
	if snapshot.Shanghai <= 0 || snapshot.Shenzhen <= 0 || snapshot.Beijing <= 0 {
		return domain.MarketAmountSnapshot{}, fmt.Errorf("沪深京上一交易日成交额不完整")
	}
	return snapshot, nil
}
