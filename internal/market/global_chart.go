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

// GlobalChartClient provides historical and intraday series for the small set
// of overseas indices exposed by the Web client. The series deliberately use
// the existing chart domain types so the browser and A-share chart renderer
// keep the same serialization contract.
type GlobalChartClient interface {
	FetchGlobalDailyBars(context.Context, string) ([]domain.DailyBar, error)
	FetchGlobalMinutePoints(context.Context, string) ([]domain.MinutePoint, error)
}

// GlobalChartDefinition maps the stable Sina snapshot symbol to the symbols
// understood by historical data providers.
type GlobalChartDefinition struct {
	Symbol              string
	Region              string
	Name                string
	Timezone            string
	EastmoneySecurityID string
	YahooSymbol         string
	YahooProxySymbol    string
}

var globalChartDefinitions = []GlobalChartDefinition{
	{Symbol: "rt_hkHSI", Region: "港股", Name: "恒生指数", Timezone: "Asia/Hong_Kong", EastmoneySecurityID: "100.HSI", YahooSymbol: "^HSI"},
	// Yahoo exposes this index as a Hong Kong-listed index symbol rather than
	// the caret-prefixed US-style ticker used by its other indices.
	{Symbol: "rt_hkHSTECH", Region: "港股", Name: "恒生科技", Timezone: "Asia/Hong_Kong", EastmoneySecurityID: "100.HSTECH", YahooSymbol: "HSTECH.HK", YahooProxySymbol: "3033.HK"},
	{Symbol: "b_NKY", Region: "日本", Name: "日经225", Timezone: "Asia/Tokyo", EastmoneySecurityID: "100.N225", YahooSymbol: "^N225"},
	{Symbol: "b_KOSPI", Region: "韩国", Name: "KOSPI", Timezone: "Asia/Seoul", EastmoneySecurityID: "100.KS11", YahooSymbol: "^KS11"},
	{Symbol: "b_KOSDAQ", Region: "韩国", Name: "KOSDAQ", Timezone: "Asia/Seoul", EastmoneySecurityID: "100.KQ11", YahooSymbol: "^KQ11"},
	{Symbol: "gb_ixic", Region: "美国", Name: "纳斯达克", Timezone: "America/New_York", EastmoneySecurityID: "100.NDX", YahooSymbol: "^IXIC"},
	{Symbol: "gb_inx", Region: "美国", Name: "标普500", Timezone: "America/New_York", EastmoneySecurityID: "100.SPX", YahooSymbol: "^GSPC"},
	{Symbol: "gb_dji", Region: "美国", Name: "道琼斯", Timezone: "America/New_York", EastmoneySecurityID: "100.DJIA", YahooSymbol: "^DJI"},
}

// GlobalChartDefinitions returns a copy so callers cannot mutate the provider
// mapping shared by concurrent Web requests.
func GlobalChartDefinitions() []GlobalChartDefinition {
	return append([]GlobalChartDefinition(nil), globalChartDefinitions...)
}

func GlobalChartDefinitionFor(symbol string) (GlobalChartDefinition, bool) {
	normalized := strings.ToLower(strings.TrimSpace(symbol))
	for _, definition := range globalChartDefinitions {
		if strings.ToLower(definition.Symbol) == normalized {
			return definition, true
		}
	}
	return GlobalChartDefinition{}, false
}

// FallbackGlobalChartClient tries each provider independently for daily and
// intraday data. A provider can therefore be unavailable for one interval
// without hiding a valid series from another provider.
type FallbackGlobalChartClient struct {
	clients []GlobalChartClient
}

func NewFallbackGlobalChartClient(clients ...GlobalChartClient) FallbackGlobalChartClient {
	usable := make([]GlobalChartClient, 0, len(clients))
	for _, client := range clients {
		if client != nil {
			usable = append(usable, client)
		}
	}
	return FallbackGlobalChartClient{clients: usable}
}

func (client FallbackGlobalChartClient) FetchGlobalDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	if len(client.clients) == 0 {
		return nil, fmt.Errorf("外盘日 K 服务未初始化")
	}
	errors := make([]string, 0, len(client.clients))
	for _, source := range client.clients {
		bars, err := source.FetchGlobalDailyBars(ctx, symbol)
		if err == nil && len(bars) > 0 {
			return bars, nil
		}
		if err != nil {
			errors = append(errors, err.Error())
		}
	}
	if len(errors) == 0 {
		return nil, fmt.Errorf("%s 未返回有效外盘日 K", symbol)
	}
	return nil, fmt.Errorf("%s", strings.Join(errors, "；"))
}

func (client FallbackGlobalChartClient) FetchGlobalMinutePoints(ctx context.Context, symbol string) ([]domain.MinutePoint, error) {
	if len(client.clients) == 0 {
		return nil, fmt.Errorf("外盘分时服务未初始化")
	}
	errors := make([]string, 0, len(client.clients))
	for _, source := range client.clients {
		points, err := source.FetchGlobalMinutePoints(ctx, symbol)
		if err == nil && len(points) > 0 {
			return points, nil
		}
		if err != nil {
			errors = append(errors, err.Error())
		}
	}
	if len(errors) == 0 {
		return nil, fmt.Errorf("%s 未返回有效外盘分时", symbol)
	}
	return nil, fmt.Errorf("%s", strings.Join(errors, "；"))
}

// --------------------------------------------------------------------------
// Yahoo Finance chart provider

const yahooGlobalChartAPIURL = "https://query1.finance.yahoo.com/v8/finance/chart/{symbol}"

type YahooGlobalChartClient struct{}

type yahooChartPayload struct {
	Chart struct {
		Result []struct {
			Meta struct {
				ExchangeTimezoneName string `json:"exchangeTimezoneName"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					Close  []*float64 `json:"close"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Volume []*float64 `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error json.RawMessage `json:"error"`
	} `json:"chart"`
}

func yahooChartAddress(base string, definition GlobalChartDefinition, interval, rangeValue string) string {
	if strings.TrimSpace(base) == "" {
		base = yahooGlobalChartAPIURL
	}
	address := strings.ReplaceAll(base, "{symbol}", url.PathEscape(definition.YahooSymbol))
	address = strings.ReplaceAll(address, "{canonical}", url.PathEscape(definition.Symbol))
	address = strings.ReplaceAll(address, "{interval}", url.QueryEscape(interval))
	address = strings.ReplaceAll(address, "{range}", url.QueryEscape(rangeValue))
	values := url.Values{
		"range":          {rangeValue},
		"interval":       {interval},
		"includePrePost": {"false"},
		"events":         {"div,splits"},
	}
	separator := "?"
	if strings.Contains(address, "?") {
		separator = "&"
	}
	return address + separator + values.Encode()
}

func yahooGlobalPayload(raw string) (yahooChartPayload, error) {
	var payload yahooChartPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return payload, err
	}
	if len(payload.Chart.Result) == 0 {
		return payload, fmt.Errorf("Yahoo 未返回外盘图表数据")
	}
	return payload, nil
}

func yahooLocation(definition GlobalChartDefinition, sourceTimezone string) *time.Location {
	for _, candidate := range []string{sourceTimezone, definition.Timezone, "UTC"} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if location, err := time.LoadLocation(candidate); err == nil {
			return location
		}
	}
	return time.UTC
}

func yahooFloat(values []*float64, index int) (float64, bool) {
	if index < 0 || index >= len(values) || values[index] == nil {
		return 0, false
	}
	value := *values[index]
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func yahooSeries(payload yahooChartPayload) (GlobalChartDefinition, []int64, []*float64, []*float64, []*float64, []*float64, []*float64, *time.Location) {
	result := payload.Chart.Result[0]
	definition := GlobalChartDefinition{Timezone: result.Meta.ExchangeTimezoneName}
	quote := result.Indicators.Quote
	if len(quote) == 0 {
		return definition, result.Timestamp, nil, nil, nil, nil, nil, time.UTC
	}
	item := quote[0]
	return definition, result.Timestamp, item.Open, item.Close, item.High, item.Low, item.Volume, yahooLocation(definition, result.Meta.ExchangeTimezoneName)
}

// ParseYahooGlobalDailyPayload parses Yahoo's chart JSON. It is exported for
// fixture-based tests and for downstream embedders that use a custom endpoint.
func ParseYahooGlobalDailyPayload(raw, symbol string) []domain.DailyBar {
	payload, err := yahooGlobalPayload(raw)
	if err != nil || len(payload.Chart.Result) == 0 {
		return nil
	}
	_, timestamps, opens, closes, highs, lows, volumes, location := yahooSeries(payload)
	if resolved, ok := GlobalChartDefinitionFor(symbol); ok {
		location = yahooLocation(resolved, payload.Chart.Result[0].Meta.ExchangeTimezoneName)
	}
	bars := make([]domain.DailyBar, 0, len(timestamps))
	seen := make(map[string]bool, len(timestamps))
	for index, stamp := range timestamps {
		open, openOK := yahooFloat(opens, index)
		closePrice, closeOK := yahooFloat(closes, index)
		high, highOK := yahooFloat(highs, index)
		low, lowOK := yahooFloat(lows, index)
		if !openOK || !closeOK || !highOK || !lowOK || open <= 0 || closePrice <= 0 || high <= 0 || low <= 0 {
			continue
		}
		date := time.Unix(stamp, 0).In(location).Format("2006-01-02")
		if seen[date] {
			continue
		}
		seen[date] = true
		volume, volumeOK := yahooFloat(volumes, index)
		if !volumeOK || volume < 0 {
			volume = 0
		}
		bars = append(bars, domain.DailyBar{
			Symbol: symbol, Source: "Yahoo Finance", Date: date,
			Open: open, Close: closePrice, High: high, Low: low, Volume: volume,
			Amount: closePrice * volume, Turnover: math.NaN(),
		})
	}
	sort.SliceStable(bars, func(left, right int) bool { return bars[left].Date < bars[right].Date })
	return bars
}

// ParseYahooGlobalMinutePayload parses Yahoo's intraday chart JSON into the
// browser's minute-point shape. Yahoo commonly returns 5-minute bars for the
// one-day range; the source interval is included in Source for transparency.
func ParseYahooGlobalMinutePayload(raw, symbol string) []domain.MinutePoint {
	payload, err := yahooGlobalPayload(raw)
	if err != nil || len(payload.Chart.Result) == 0 {
		return nil
	}
	_, timestamps, opens, closes, _, _, volumes, location := yahooSeries(payload)
	if resolved, ok := GlobalChartDefinitionFor(symbol); ok {
		location = yahooLocation(resolved, payload.Chart.Result[0].Meta.ExchangeTimezoneName)
	}
	points := make([]domain.MinutePoint, 0, len(timestamps))
	cumulativeVolume := 0.0
	cumulativeAmount := 0.0
	for index, stamp := range timestamps {
		closePrice, closeOK := yahooFloat(closes, index)
		if !closeOK || closePrice <= 0 {
			continue
		}
		open, openOK := yahooFloat(opens, index)
		if !openOK || open <= 0 {
			open = closePrice
		}
		volume, volumeOK := yahooFloat(volumes, index)
		if !volumeOK || volume < 0 {
			volume = 0
		}
		amount := closePrice * volume
		cumulativeVolume += volume
		cumulativeAmount += amount
		at := time.Unix(stamp, 0).In(location)
		points = append(points, domain.MinutePoint{
			Symbol: symbol, Source: "Yahoo Finance · 5分钟", TradeDate: at.Format("2006-01-02"),
			Time: at.Format("15:04"), Price: closePrice, Average: (open + closePrice) / 2,
			Volume: volume, Amount: amount, CumulativeVolume: cumulativeVolume, CumulativeAmount: cumulativeAmount,
		})
	}
	sort.SliceStable(points, func(left, right int) bool {
		return points[left].TradeDate+" "+points[left].Time < points[right].TradeDate+" "+points[right].Time
	})
	return points
}

func yahooDefinitions(definition GlobalChartDefinition) []GlobalChartDefinition {
	result := []GlobalChartDefinition{definition}
	if strings.TrimSpace(definition.YahooProxySymbol) != "" && definition.YahooProxySymbol != definition.YahooSymbol {
		proxy := definition
		proxy.YahooSymbol = definition.YahooProxySymbol
		result = append(result, proxy)
	}
	return result
}

func annotateYahooProxyBars(bars []domain.DailyBar, proxy string) {
	for index := range bars {
		bars[index].Source = "Yahoo Finance · " + proxy + " 代理"
	}
}

func annotateYahooProxyPoints(points []domain.MinutePoint, proxy string) {
	for index := range points {
		points[index].Source = "Yahoo Finance · " + proxy + " 代理"
	}
}

func (YahooGlobalChartClient) FetchGlobalDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	definition, ok := GlobalChartDefinitionFor(symbol)
	if !ok {
		return nil, fmt.Errorf("不支持的外盘市场 %q", symbol)
	}
	base := os.Getenv("ASTOCK_GLOBAL_CHART_API_URL")
	requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var lastError error
	var bars []domain.DailyBar
	for _, candidate := range yahooDefinitions(definition) {
		raw, err := fetchDecoded(requestContext, yahooChartAddress(base, candidate, "1d", "2y"), nil)
		if err != nil {
			lastError = err
			continue
		}
		bars = ParseYahooGlobalDailyPayload(raw, symbol)
		if len(bars) == 0 {
			lastError = fmt.Errorf("%s 的 Yahoo 日 K 为空", candidate.YahooSymbol)
			continue
		}
		if candidate.YahooSymbol == definition.YahooSymbol && definition.YahooProxySymbol != "" && len(bars) < 30 {
			lastError = fmt.Errorf("%s 的 Yahoo 日 K 仅返回 %d 根，切换代理", candidate.YahooSymbol, len(bars))
			continue
		}
		if candidate.YahooSymbol != definition.YahooSymbol {
			annotateYahooProxyBars(bars, candidate.YahooSymbol)
		}
		break
	}
	if len(bars) == 0 {
		if lastError == nil {
			lastError = fmt.Errorf("%s 的 Yahoo 日 K 为空", symbol)
		}
		return nil, fmt.Errorf("Yahoo 日 K: %w", lastError)
	}
	return bars, nil
}

func (YahooGlobalChartClient) FetchGlobalMinutePoints(ctx context.Context, symbol string) ([]domain.MinutePoint, error) {
	definition, ok := GlobalChartDefinitionFor(symbol)
	if !ok {
		return nil, fmt.Errorf("不支持的外盘市场 %q", symbol)
	}
	base := os.Getenv("ASTOCK_GLOBAL_CHART_API_URL")
	requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var lastError error
	var points []domain.MinutePoint
	for _, candidate := range yahooDefinitions(definition) {
		raw, err := fetchDecoded(requestContext, yahooChartAddress(base, candidate, "5m", "1d"), nil)
		if err != nil {
			lastError = err
			continue
		}
		points = ParseYahooGlobalMinutePayload(raw, symbol)
		if len(points) == 0 {
			lastError = fmt.Errorf("%s 的 Yahoo 分时为空", candidate.YahooSymbol)
			continue
		}
		if candidate.YahooSymbol == definition.YahooSymbol && definition.YahooProxySymbol != "" && len(points) < 20 {
			lastError = fmt.Errorf("%s 的 Yahoo 分时仅返回 %d 个点，切换代理", candidate.YahooSymbol, len(points))
			continue
		}
		if candidate.YahooSymbol != definition.YahooSymbol {
			annotateYahooProxyPoints(points, candidate.YahooSymbol)
		}
		break
	}
	if len(points) == 0 {
		if lastError == nil {
			lastError = fmt.Errorf("%s 的 Yahoo 分时为空", symbol)
		}
		return nil, fmt.Errorf("Yahoo 分时: %w", lastError)
	}
	return points, nil
}

// --------------------------------------------------------------------------
// Eastmoney global index provider

type EastmoneyGlobalChartClient struct{}

type eastmoneyGlobalChartPayload struct {
	Data *struct {
		Klines []string `json:"klines"`
	} `json:"data"`
}

func eastmoneyGlobalChartAddress(base string, definition GlobalChartDefinition, klt, limit string) string {
	if strings.TrimSpace(base) == "" {
		base = klineHistoryAPIURL
	}
	address := strings.ReplaceAll(base, "{symbol}", url.QueryEscape(definition.EastmoneySecurityID))
	address = strings.ReplaceAll(address, "{canonical}", url.QueryEscape(definition.Symbol))
	values := url.Values{
		"secid":   {definition.EastmoneySecurityID},
		"klt":     {klt},
		"fqt":     {"0"},
		"lmt":     {limit},
		"beg":     {"0"},
		"end":     {"20500000"},
		"fields1": {"f1,f2,f3,f4,f5,f6"},
		"fields2": {"f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61"},
	}
	separator := "?"
	if strings.Contains(address, "?") {
		separator = "&"
	}
	return address + separator + values.Encode()
}

func parseEastmoneyGlobalPayload(raw string) []string {
	var payload eastmoneyGlobalChartPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Data == nil {
		return nil
	}
	return payload.Data.Klines
}

func parseGlobalBarNumber(parts []string, index int) (float64, bool) {
	if index < 0 || index >= len(parts) {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(parts[index]), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

// ParseEastmoneyGlobalDailyPayload parses the same comma-separated rows as
// the A-share Eastmoney adapter, while allowing index volumes/amounts to be
// absent.
func ParseEastmoneyGlobalDailyPayload(raw, symbol string) []domain.DailyBar {
	rows := parseEastmoneyGlobalPayload(raw)
	bars := make([]domain.DailyBar, 0, len(rows))
	for _, row := range rows {
		parts := strings.Split(row, ",")
		if len(parts) < 5 {
			continue
		}
		date := strings.TrimSpace(parts[0])
		if len(date) >= 10 {
			date = date[:10]
		}
		if len(date) != 10 {
			continue
		}
		open, openOK := parseGlobalBarNumber(parts, 1)
		closePrice, closeOK := parseGlobalBarNumber(parts, 2)
		high, highOK := parseGlobalBarNumber(parts, 3)
		low, lowOK := parseGlobalBarNumber(parts, 4)
		if !openOK || !closeOK || !highOK || !lowOK || open <= 0 || closePrice <= 0 || high <= 0 || low <= 0 {
			continue
		}
		volume, volumeOK := parseGlobalBarNumber(parts, 5)
		if !volumeOK || volume < 0 {
			volume = 0
		}
		amount, amountOK := parseGlobalBarNumber(parts, 6)
		if !amountOK || amount < 0 {
			amount = closePrice * volume
		}
		bars = append(bars, domain.DailyBar{Symbol: symbol, Source: "东方财富全球指数", Date: date, Open: open, Close: closePrice, High: high, Low: low, Volume: volume, Amount: amount, Turnover: math.NaN()})
	}
	sort.SliceStable(bars, func(left, right int) bool { return bars[left].Date < bars[right].Date })
	return bars
}

func normalizeGlobalClock(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 5 && value[2] == ':' {
		return value[:5]
	}
	if len(value) == 4 {
		if _, err := strconv.Atoi(value); err == nil {
			return value[:2] + ":" + value[2:]
		}
	}
	return value
}

// ParseEastmoneyGlobalMinutePayload parses 1/5-minute rows. It intentionally
// does not apply A-share session filtering because Hong Kong, Japan and US
// sessions have different trading hours.
func ParseEastmoneyGlobalMinutePayload(raw, symbol string) []domain.MinutePoint {
	rows := parseEastmoneyGlobalPayload(raw)
	points := make([]domain.MinutePoint, 0, len(rows))
	cumulativeVolume := 0.0
	cumulativeAmount := 0.0
	for _, row := range rows {
		parts := strings.Split(row, ",")
		if len(parts) < 5 {
			continue
		}
		stamp := strings.TrimSpace(parts[0])
		fields := strings.Fields(stamp)
		if len(fields) < 2 || len(fields[0]) < 10 {
			continue
		}
		tradeDate := fields[0][:10]
		clock := normalizeGlobalClock(fields[1])
		if len(clock) < 5 {
			continue
		}
		open, openOK := parseGlobalBarNumber(parts, 1)
		closePrice, closeOK := parseGlobalBarNumber(parts, 2)
		_, highOK := parseGlobalBarNumber(parts, 3)
		_, lowOK := parseGlobalBarNumber(parts, 4)
		if !openOK || !closeOK || !highOK || !lowOK || closePrice <= 0 {
			continue
		}
		volume, volumeOK := parseGlobalBarNumber(parts, 5)
		if !volumeOK || volume < 0 {
			volume = 0
		}
		amount, amountOK := parseGlobalBarNumber(parts, 6)
		if !amountOK || amount < 0 {
			amount = closePrice * volume
		}
		cumulativeVolume += volume
		cumulativeAmount += amount
		points = append(points, domain.MinutePoint{Symbol: symbol, Source: "东方财富全球指数 · 5分钟", TradeDate: tradeDate, Time: clock, Price: closePrice, Average: (open + closePrice) / 2, Volume: volume, Amount: amount, CumulativeVolume: cumulativeVolume, CumulativeAmount: cumulativeAmount})
	}
	sort.SliceStable(points, func(left, right int) bool {
		return points[left].TradeDate+" "+points[left].Time < points[right].TradeDate+" "+points[right].Time
	})
	return points
}

func (EastmoneyGlobalChartClient) FetchGlobalDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	definition, ok := GlobalChartDefinitionFor(symbol)
	if !ok {
		return nil, fmt.Errorf("不支持的外盘市场 %q", symbol)
	}
	base := os.Getenv("ASTOCK_GLOBAL_EASTMONEY_API_URL")
	requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	raw, err := fetchDecoded(requestContext, eastmoneyGlobalChartAddress(base, definition, "101", "300"), nil)
	if err != nil {
		return nil, fmt.Errorf("东方财富日 K: %w", err)
	}
	bars := ParseEastmoneyGlobalDailyPayload(raw, symbol)
	if len(bars) == 0 {
		return nil, fmt.Errorf("%s 的东方财富日 K 为空", symbol)
	}
	return bars, nil
}

func (EastmoneyGlobalChartClient) FetchGlobalMinutePoints(ctx context.Context, symbol string) ([]domain.MinutePoint, error) {
	definition, ok := GlobalChartDefinitionFor(symbol)
	if !ok {
		return nil, fmt.Errorf("不支持的外盘市场 %q", symbol)
	}
	base := os.Getenv("ASTOCK_GLOBAL_EASTMONEY_API_URL")
	requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	raw, err := fetchDecoded(requestContext, eastmoneyGlobalChartAddress(base, definition, "5", "500"), nil)
	if err != nil {
		return nil, fmt.Errorf("东方财富分时: %w", err)
	}
	points := ParseEastmoneyGlobalMinutePayload(raw, symbol)
	if len(points) == 0 {
		return nil, fmt.Errorf("%s 的东方财富分时为空", symbol)
	}
	return points, nil
}
