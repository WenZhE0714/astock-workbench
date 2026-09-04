package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

// THSSignalClient reads the public Tonghuashun signal endpoints used by the
// sentiment cockpit. These endpoints are independent of QuantAPI token quotas.
type THSSignalClient struct {
	HTTPClient    *http.Client
	NorthboundURL string
	HotStocksURL  string
}

func NewTHSSignalClientFromEnv() THSSignalClient {
	return THSSignalClient{NorthboundURL: os.Getenv("ASTOCK_THS_NORTHBOUND_URL"), HotStocksURL: os.Getenv("ASTOCK_THS_HOT_STOCKS_URL")}
}

func (client THSSignalClient) httpClient() *http.Client {
	if client.HTTPClient != nil {
		return client.HTTPClient
	}
	return &http.Client{Timeout: 4 * time.Second}
}

func (client THSSignalClient) FetchNorthbound(ctx context.Context) (domain.NorthboundFlowSnapshot, error) {
	address := client.NorthboundURL
	if strings.TrimSpace(address) == "" {
		address = "https://data.hexin.cn/market/hsgtApi/method/dayChart/"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return domain.NorthboundFlowSnapshot{}, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0")
	request.Header.Set("Referer", "https://data.hexin.cn/")
	response, err := client.httpClient().Do(request)
	if err != nil {
		return domain.NorthboundFlowSnapshot{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.NorthboundFlowSnapshot{}, fmt.Errorf("北向资金 HTTP %s", response.Status)
	}
	var payload struct {
		Time []string          `json:"time"`
		HGT  []json.RawMessage `json:"hgt"`
		SGT  []json.RawMessage `json:"sgt"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return domain.NorthboundFlowSnapshot{}, err
	}
	if len(payload.Time) == 0 {
		return domain.NorthboundFlowSnapshot{}, fmt.Errorf("北向资金暂无盘中数据")
	}
	last := len(payload.Time) - 1
	shanghai := signalNumber(payload.HGT, last)
	shenzhen := signalNumber(payload.SGT, last)
	if !finiteSignal(shanghai) && !finiteSignal(shenzhen) {
		return domain.NorthboundFlowSnapshot{}, fmt.Errorf("北向资金返回空值")
	}
	return domain.NorthboundFlowSnapshot{At: time.Now(), Shanghai: shanghai, Shenzhen: shenzhen, Total: safeSignal(shanghai) + safeSignal(shenzhen), Available: true, Source: "同花顺 hsgtApi"}, nil
}

func (client THSSignalClient) FetchHotStocks(ctx context.Context, tradeDate string) (domain.HotStockSnapshot, error) {
	if strings.TrimSpace(tradeDate) == "" {
		tradeDate = time.Now().Format("2006-01-02")
	}
	address := client.HotStocksURL
	if strings.TrimSpace(address) == "" {
		address = "http://zx.10jqka.com.cn/event/api/getharden/date/%s/orderby/date/orderway/desc/charset/GBK/"
	}
	address = strings.ReplaceAll(address, "%s", tradeDate)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return domain.HotStockSnapshot{}, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0")
	response, err := client.httpClient().Do(request)
	if err != nil {
		return domain.HotStockSnapshot{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.HotStockSnapshot{}, fmt.Errorf("同花顺热点 HTTP %s", response.Status)
	}
	var payload struct {
		ErrorCode any `json:"errocode"`
		Data      []struct {
			Code      string          `json:"code"`
			Name      string          `json:"name"`
			Percent   json.RawMessage `json:"zhangfu"`
			Turnover  json.RawMessage `json:"huanshou"`
			Amount    json.RawMessage `json:"chengjiaoe"`
			MainRatio json.RawMessage `json:"ddejingliang"`
			Reason    string          `json:"reason"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return domain.HotStockSnapshot{}, err
	}
	stocks := make([]domain.HotStockSignal, 0, len(payload.Data))
	type themeAggregate struct {
		count      int
		rise       float64
		amount     float64
		leader     string
		leaderRise float64
	}
	themeStats := map[string]*themeAggregate{}
	for _, item := range payload.Data {
		if len(item.Code) != 6 || strings.TrimSpace(item.Name) == "" {
			continue
		}
		reason := strings.TrimSpace(item.Reason)
		for _, theme := range strings.Split(reason, "+") {
			if theme = strings.TrimSpace(theme); theme != "" {
				stat := themeStats[theme]
				if stat == nil {
					stat = &themeAggregate{}
					themeStats[theme] = stat
				}
				stat.count++
				if finiteSignal(signalRawNumber(item.Percent)) {
					stat.rise += signalRawNumber(item.Percent)
					if stat.leader == "" || signalRawNumber(item.Percent) > stat.leaderRise {
						stat.leader, stat.leaderRise = strings.TrimSpace(item.Name), signalRawNumber(item.Percent)
					}
				}
				stat.amount += signalRawNumber(item.Amount)
			}
		}
		stocks = append(stocks, domain.HotStockSignal{Symbol: normalizePlainSymbol(item.Code), Name: strings.TrimSpace(item.Name), Percent: signalRawNumber(item.Percent), Turnover: signalRawNumber(item.Turnover), Amount: signalRawNumber(item.Amount), MainRatio: signalRawNumber(item.MainRatio), Reason: reason})
	}
	if len(stocks) == 0 {
		return domain.HotStockSnapshot{}, fmt.Errorf("同花顺热点暂无数据")
	}
	themes := make([]domain.HotTheme, 0, len(themeStats))
	for name, stat := range themeStats {
		average := 0.0
		if stat.count > 0 {
			average = stat.rise / float64(stat.count)
		}
		themes = append(themes, domain.HotTheme{Name: name, Count: stat.count, Leader: stat.leader, AverageRise: average, Amount: stat.amount})
	}
	sort.Slice(themes, func(i, j int) bool {
		if themes[i].Count == themes[j].Count {
			if themes[i].AverageRise == themes[j].AverageRise {
				return themes[i].Name < themes[j].Name
			}
			return themes[i].AverageRise > themes[j].AverageRise
		}
		return themes[i].Count > themes[j].Count
	})
	if len(themes) > 10 {
		themes = themes[:10]
	}
	return domain.HotStockSnapshot{TradeDate: tradeDate, Stocks: stocks, Themes: themes, Available: true, Source: "同花顺热点事件"}, nil
}

func signalNumber(values []json.RawMessage, index int) float64 {
	if index < 0 || index >= len(values) {
		return math.NaN()
	}
	text := strings.TrimSpace(strings.Trim(string(values[index]), `"`))
	text = strings.TrimSuffix(strings.ReplaceAll(text, ",", ""), "%")
	number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return math.NaN()
	}
	return number
}
func signalRawNumber(value json.RawMessage) float64 {
	text := strings.TrimSpace(strings.Trim(string(value), `"`))
	text = strings.TrimSuffix(strings.ReplaceAll(text, ",", ""), "%")
	multiplier := 1.0
	if strings.HasSuffix(text, "亿") {
		multiplier, text = 1e8, strings.TrimSuffix(text, "亿")
	} else if strings.HasSuffix(text, "万") {
		multiplier, text = 1e4, strings.TrimSuffix(text, "万")
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return 0
	}
	return number * multiplier
}
func finiteSignal(value float64) bool { return value == value && value < 1e308 && value > -1e308 }
func safeSignal(value float64) float64 {
	if finiteSignal(value) {
		return value
	}
	return 0
}
func normalizePlainSymbol(code string) string {
	if strings.HasPrefix(code, "6") {
		return "sh" + code
	}
	return "sz" + code
}
