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

type EastmoneyClient struct{}

type fundFlowPayload struct {
	Data *struct {
		Diff []struct {
			Price     json.RawMessage `json:"f2"`
			Percent   json.RawMessage `json:"f3"`
			Speed     json.RawMessage `json:"f22"`
			Code      string          `json:"f12"`
			Market    json.RawMessage `json:"f13"`
			Name      string          `json:"f14"`
			MainNet   json.RawMessage `json:"f62"`
			Industry  string          `json:"f100"`
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
			Symbol: symbol, Name: strings.TrimSpace(item.Name), Industry: strings.TrimSpace(item.Industry),
			Price: rawNumber(item.Price), Percent: rawNumber(item.Percent), Speed: rawNumber(item.Speed),
			MainNet: rawNumber(item.MainNet), MainRatio: rawNumber(item.MainRatio),
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
		"fields": {"f2,f3,f12,f13,f14,f22,f62,f100,f184"},
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
