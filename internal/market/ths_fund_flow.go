package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const THSFundFlowSource = "同花顺大单净额"
const thsFundFlowAPIURL = "https://stockpage.10jqka.com.cn/spService/{code}/Funds/realFunds"

type THSFundFlowClient struct {
	HTTPClient *http.Client
	BaseURL    string
	IndexURL   string
	MinuteURL  string
}

// The code-only endpoint is stock-specific. In particular, 000001 means the
// Shenzhen stock, never the Shanghai index. Do not send indices or ETFs here.
func supportsTHSFundFlow(symbol string) bool {
	if !ValidPrefixedSymbol(symbol) {
		return false
	}
	return strings.HasPrefix(symbol, "sh60") || strings.HasPrefix(symbol, "sh688") ||
		strings.HasPrefix(symbol, "sz00") || strings.HasPrefix(symbol, "sz30")
}

func ParseTHSFundFlowPayload(raw, symbol string) (domain.FundFlow, error) {
	if !supportsTHSFundFlow(symbol) {
		return domain.FundFlow{}, fmt.Errorf("同花顺个股资金流不支持 %s", symbol)
	}
	var payload struct {
		Flash []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"sr"`
		} `json:"flash"`
		Field struct {
			Industry string `json:"hyname"`
		} `json:"field"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return domain.FundFlow{}, fmt.Errorf("同花顺资金流返回无效 JSON: %w", err)
	}
	amounts := make(map[string]float64)
	for _, item := range payload.Flash {
		switch item.Name {
		case "大单流入", "大单流出", "中单流入", "中单流出", "小单流入", "小单流出":
		default:
			continue
		}
		value := rawNumber(item.Value)
		if _, duplicate := amounts[item.Name]; duplicate || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return domain.FundFlow{}, fmt.Errorf("同花顺资金流字段无效: %s", item.Name)
		}
		amounts[item.Name] = value
	}
	inflow, inOK := amounts["大单流入"]
	outflow, outOK := amounts["大单流出"]
	if !inOK || !outOK {
		return domain.FundFlow{}, fmt.Errorf("同花顺资金流缺少大单流入/流出")
	}
	// Public amounts are in ten-thousand yuan. The title.je total includes
	// medium/small orders and must never be interpreted as main-fund flow.
	net := (inflow - outflow) * 1e4
	if math.IsNaN(net) || math.IsInf(net, 0) {
		return domain.FundFlow{}, fmt.Errorf("同花顺大单净额无效")
	}
	ratio := math.NaN()
	if len(amounts) == 6 {
		total := 0.0
		for _, value := range amounts {
			total += value
		}
		if total > 0 && !math.IsInf(total, 0) {
			ratio = (inflow - outflow) / total * 100
		}
	}
	return domain.FundFlow{
		Symbol: symbol, Source: THSFundFlowSource, Industry: strings.TrimSpace(payload.Field.Industry),
		Price: math.NaN(), Percent: math.NaN(), Speed: math.NaN(), MainNet: net, MainRatio: ratio,
	}, nil
}

func (client THSFundFlowClient) fetchOne(ctx context.Context, symbol string) (domain.FundFlow, error) {
	if thsFundIndexCode(symbol) != "" {
		return client.fetchIndex(ctx, symbol)
	}
	base := firstNonEmpty(client.BaseURL, os.Getenv("ASTOCK_THS_FUND_FLOW_API_URL"), thsFundFlowAPIURL)
	address := strings.ReplaceAll(base, "{code}", url.PathEscape(symbol[2:]))
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	headers := map[string]string{"Referer": "https://stockpage.10jqka.com.cn/" + symbol[2:] + "/funds/"}
	var raw string
	var err error
	if client.HTTPClient != nil {
		raw, err = fetchDecodedWithClient(requestCtx, client.HTTPClient, address, nil, headers)
	} else {
		raw, err = fetchTHSDecoded(requestCtx, address, nil, headers)
	}
	if err != nil {
		return domain.FundFlow{}, err
	}
	flow, err := ParseTHSFundFlowPayload(raw, symbol)
	if err == nil {
		flow.FetchedAt = time.Now()
		if speed, sampledAt, speedErr := client.fetchSpeed(ctx, symbol); speedErr == nil {
			flow.Speed, flow.SpeedTime, flow.SpeedSource = speed, sampledAt, THSMinuteSpeedSource
		}
	}
	return flow, err
}

func (client THSFundFlowClient) Fetch(ctx context.Context, symbols []string) (map[string]domain.FundFlow, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	flows := make(map[string]domain.FundFlow)
	requested := missingFundFlowSymbols(symbols, flows)
	failures := make([]string, len(requested))
	jobs := make(chan int, len(requested))
	for index, symbol := range requested {
		if supportsTHSFundFlow(symbol) || thsFundIndexCode(symbol) != "" {
			jobs <- index
		} else {
			failures[index] = "同花顺个股资金流未覆盖 " + symbol
		}
	}
	close(jobs)
	var mu sync.Mutex
	var workers sync.WaitGroup
	for range min(4, len(jobs)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				symbol := requested[index]
				flow, err := client.fetchOne(ctx, symbol)
				mu.Lock()
				if err != nil {
					failures[index] = symbol + ": " + err.Error()
				} else {
					flows[symbol] = flow
				}
				mu.Unlock()
			}
		}()
	}
	workers.Wait()
	if ctx.Err() != nil {
		return flows, ctx.Err()
	}
	causes := make([]string, 0)
	for _, failure := range failures {
		if failure != "" {
			causes = append(causes, failure)
		}
	}
	return flows, fundFlowCoverageError(requested, flows, causes)
}
