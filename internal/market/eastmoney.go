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

const (
	fundFlowAPIURL         = "https://push2.eastmoney.com/api/qt/ulist.np/get"
	fundFlowFallbackAPIURL = "https://push2delay.eastmoney.com/api/qt/ulist.np/get"
	klineAPIURL            = "https://push2.eastmoney.com/api/qt/stock/kline/get"
	klineHistoryAPIURL     = "https://push2his.eastmoney.com/api/qt/stock/kline/get"
	klineFallbackAPIURL    = "https://push2delay.eastmoney.com/api/qt/stock/kline/get"
)

type FundFlowClient interface {
	Fetch(context.Context, []string) (map[string]domain.FundFlow, error)
}

type PreviousAmountClient interface {
	FetchPreviousAmounts(context.Context, []string) (map[string]float64, error)
}

type EastmoneyClient struct{}

type fundFlowPayload struct {
	Data *struct {
		Diff []struct {
			Code      string          `json:"f12"`
			Market    json.RawMessage `json:"f13"`
			MainNet   json.RawMessage `json:"f62"`
			MainRatio json.RawMessage `json:"f184"`
		} `json:"diff"`
	} `json:"data"`
}

func rawNumber(value json.RawMessage) float64 {
	text := strings.Trim(string(value), `"`)
	if text == "" || text == "-" || text == "null" {
		return math.NaN()
	}
	result, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return math.NaN()
	}
	return result
}

func marketNumber(value json.RawMessage, code string) int {
	text := strings.Trim(string(value), `"`)
	result, err := strconv.Atoi(text)
	if err == nil {
		return result
	}
	if strings.HasPrefix(code, "6") {
		return 1
	}
	return 0
}

func ParseFundFlowPayload(raw string) map[string]domain.FundFlow {
	var payload fundFlowPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Data == nil {
		return nil
	}
	result := make(map[string]domain.FundFlow, len(payload.Data.Diff))
	for _, item := range payload.Data.Diff {
		if len(item.Code) != 6 {
			continue
		}
		prefix := "sz"
		if marketNumber(item.Market, item.Code) == 1 {
			prefix = "sh"
		}
		symbol := prefix + item.Code
		result[symbol] = domain.FundFlow{
			Symbol: symbol, MainNet: rawNumber(item.MainNet), MainRatio: rawNumber(item.MainRatio),
		}
	}
	return result
}

func eastmoneySecurityID(symbol string) string {
	if len(symbol) != 8 {
		return ""
	}
	market := "0"
	if strings.HasPrefix(symbol, "sh") {
		market = "1"
	}
	return market + "." + symbol[2:]
}

func (EastmoneyClient) Fetch(ctx context.Context, symbols []string) (map[string]domain.FundFlow, error) {
	securityIDs := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		if securityID := eastmoneySecurityID(symbol); securityID != "" {
			securityIDs = append(securityIDs, securityID)
		}
	}
	if len(securityIDs) == 0 {
		return map[string]domain.FundFlow{}, nil
	}

	configuredBase := os.Getenv("ASTOCK_FUND_FLOW_API_URL")
	bases := []string{configuredBase}
	if configuredBase == "" {
		bases = []string{fundFlowAPIURL, fundFlowFallbackAPIURL}
	}
	values := url.Values{
		"fltt":   {"2"},
		"invt":   {"2"},
		"fields": {"f12,f13,f14,f62,f184"},
		"secids": {strings.Join(securityIDs, ",")},
		"ut":     {"b2884a393a59ad64002292a3e90d46a5"},
	}
	var lastError error
	for _, base := range bases {
		address := base
		if strings.Contains(base, "{secids}") {
			address = strings.Replace(base, "{secids}", url.QueryEscape(strings.Join(securityIDs, ",")), 1)
		} else {
			separator := "?"
			if strings.Contains(base, "?") {
				separator = "&"
			}
			address += separator + values.Encode()
		}

		requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
		raw, err := fetchDecoded(requestContext, address, nil)
		cancel()
		if err != nil {
			lastError = err
			continue
		}
		flows := ParseFundFlowPayload(raw)
		if flows == nil {
			lastError = fmt.Errorf("未解析到资金流数据")
			continue
		}
		return flows, nil
	}
	return nil, lastError
}

type klinePayload struct {
	Data *struct {
		Klines []string `json:"klines"`
	} `json:"data"`
}

// ParsePreviousAmountPayload returns the latest daily amount strictly before
// referenceDate. Eastmoney kline amounts are yuan; the result is normalized to
// the Tencent quote convention of ten-thousand yuan.
func ParsePreviousAmountPayload(raw, referenceDate string) (float64, bool) {
	var payload klinePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Data == nil {
		return math.NaN(), false
	}
	for index := len(payload.Data.Klines) - 1; index >= 0; index-- {
		parts := strings.Split(payload.Data.Klines[index], ",")
		if len(parts) < 7 || parts[0] >= referenceDate {
			continue
		}
		amount, err := strconv.ParseFloat(parts[6], 64)
		if err != nil || amount <= 0 {
			continue
		}
		return amount / 1e4, true
	}
	return math.NaN(), false
}

func klineAddress(base, securityID string) string {
	values := url.Values{
		"secid":   {securityID},
		"klt":     {"101"},
		"fqt":     {"0"},
		"lmt":     {"5"},
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

func (EastmoneyClient) FetchPreviousAmounts(ctx context.Context, symbols []string) (map[string]float64, error) {
	configuredBase := os.Getenv("ASTOCK_AMOUNT_HISTORY_API_URL")
	bases := []string{configuredBase}
	if configuredBase == "" {
		bases = []string{klineAPIURL, klineHistoryAPIURL, klineFallbackAPIURL}
	}
	referenceDate := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
	result := make(map[string]float64, len(symbols))
	var lastError error
	for _, symbol := range symbols {
		securityID := eastmoneySecurityID(symbol)
		if securityID == "" {
			continue
		}
		found := false
		for _, base := range bases {
			requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
			raw, err := fetchDecoded(requestContext, klineAddress(base, securityID), nil)
			cancel()
			if err != nil {
				lastError = err
				continue
			}
			amount, ok := ParsePreviousAmountPayload(raw, referenceDate)
			if !ok {
				lastError = fmt.Errorf("未解析到 %s 的历史成交额", symbol)
				continue
			}
			result[symbol] = amount
			found = true
			break
		}
		if !found {
			continue
		}
	}
	if len(result) == 0 && lastError != nil {
		if token := os.Getenv("ASTOCK_TUSHARE_TOKEN"); token != "" {
			if fallback, err := fetchTusharePreviousAmounts(ctx, symbols, token); err == nil && len(fallback) > 0 {
				return fallback, nil
			} else if err != nil {
				lastError = err
			}
		}
		return nil, lastError
	}
	if token := os.Getenv("ASTOCK_TUSHARE_TOKEN"); token != "" {
		if fallback, err := fetchTusharePreviousAmounts(ctx, symbols, token); err == nil {
			for symbol, amount := range fallback {
				result[symbol] = amount
			}
		}
	}
	return result, nil
}
