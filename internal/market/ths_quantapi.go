package market

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const defaultTHSQuantAPIBaseURL = "https://quantapi.51ifind.com/api/v1"

// THSQuantOptions configures the HTTP Quant API adapter. The API uses the
// access_token header; refresh_token is retained for configuration/audit but
// is not exchanged automatically because the public data API does not expose
// a stable refresh endpoint (the web console refresh endpoints require its
// browser session).
type THSQuantOptions struct {
	AccessToken       string
	RefreshToken      string
	BaseURL           string
	HTTPClient        *http.Client
	MinRequestGap     time.Duration
	QuoteIndicators   string
	HistoryIndicators string
	MinuteIndicators  string
}

// THSQuantClient implements the project's quote/history/minute interfaces.
// Indicator names are configurable because iFinD permissions and account
// products expose different indicator catalogs.
type THSQuantClient struct {
	accessToken       string
	refreshToken      string
	baseURL           string
	httpClient        *http.Client
	minRequestGap     time.Duration
	quoteIndicators   string
	historyIndicators string
	minuteIndicators  string
	mu                sync.Mutex
	lastRequest       time.Time
}

func (client *THSQuantClient) AccessToken() string {
	if client == nil {
		return ""
	}
	return client.accessToken
}

func NewTHSQuantClient(options THSQuantOptions) *THSQuantClient {
	base := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if base == "" {
		base = defaultTHSQuantAPIBaseURL
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	gap := options.MinRequestGap
	if gap <= 0 {
		// The documented free/standard gateway limits are account-dependent;
		// 100ms is a conservative local guard and callers can raise it.
		gap = 100 * time.Millisecond
	}
	return &THSQuantClient{
		accessToken:       strings.TrimSpace(options.AccessToken),
		refreshToken:      strings.TrimSpace(options.RefreshToken),
		baseURL:           base,
		httpClient:        client,
		minRequestGap:     gap,
		quoteIndicators:   firstNonEmpty(options.QuoteIndicators, os.Getenv("ASTOCK_THS_QUOTE_INDICATORS"), "latest,changeRatio,open,high,low,preClose,volume,amount,turnoverRatio,limitUp,limitDown"),
		historyIndicators: firstNonEmpty(options.HistoryIndicators, os.Getenv("ASTOCK_THS_HISTORY_INDICATORS"), "open,close,high,low,volume,amount,turnoverRatio"),
		minuteIndicators:  firstNonEmpty(options.MinuteIndicators, os.Getenv("ASTOCK_THS_MINUTE_INDICATORS"), "latest,avgPrice,volume,amount"),
	}
}

func (client *THSQuantClient) Configured() bool {
	return client != nil && strings.TrimSpace(client.accessToken) != ""
}

func (client *THSQuantClient) do(ctx context.Context, path string, payload map[string]any) ([]byte, error) {
	if client == nil || !client.Configured() {
		return nil, errors.New("同花顺 Quant API 未配置 access token")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	client.mu.Lock()
	if wait := client.minRequestGap - time.Since(client.lastRequest); wait > 0 {
		timer := time.NewTimer(wait)
		client.mu.Unlock()
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
		client.mu.Lock()
	}
	client.lastRequest = time.Now()
	client.mu.Unlock()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/"+strings.TrimLeft(path, "/"), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "astock-workbench/0.21")
	request.Header.Set("access_token", client.accessToken)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("同花顺 Quant API HTTP %s: %s", response.Status, summarizeTHSError(body))
	}
	var envelope struct {
		ErrorCode  int    `json:"errorcode"`
		ErrorMsg   string `json:"errmsg"`
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("同花顺 Quant API 返回 JSON 无效: %w", err)
	}
	if envelope.ErrorCode != 0 {
		return nil, fmt.Errorf("同花顺 Quant API 错误 %d: %s", envelope.ErrorCode, firstNonEmpty(envelope.ErrorMsg, "unknown error"))
	}
	if envelope.StatusCode != 0 {
		return nil, fmt.Errorf("同花顺 Quant API 错误 %d: %s", envelope.StatusCode, firstNonEmpty(envelope.StatusMsg, "unknown error"))
	}
	return body, nil
}

func (client *THSQuantClient) Fetch(ctx context.Context, symbols []string) ([]domain.Quote, error) {
	codes := thsCodes(symbols)
	if len(codes) == 0 {
		return nil, nil
	}
	raw, err := client.do(ctx, "real_time_quotation", map[string]any{
		"codes": strings.Join(codes, ","), "indicators": client.quoteIndicators,
	})
	if err != nil {
		return nil, err
	}
	quotes := parseTHSQuotes(raw, symbols)
	if len(quotes) == 0 {
		return nil, errors.New("同花顺实时行情未返回可识别数据；请检查指标权限或指标名称")
	}
	return quotes, nil
}

func (client *THSQuantClient) FetchDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	code := thsCode(symbol)
	if code == "" {
		return nil, fmt.Errorf("无效股票代码 %q", symbol)
	}
	end := time.Now().Format("2006-01-02")
	start := time.Now().AddDate(0, 0, -500).Format("2006-01-02")
	raw, err := client.do(ctx, "date_sequence", map[string]any{
		"codes": code, "indicators": client.historyIndicators, "startdate": start, "enddate": end,
	})
	if err != nil {
		return nil, err
	}
	bars := parseTHSDailyBars(raw, symbol)
	if len(bars) < 60 {
		return nil, fmt.Errorf("同花顺日K仅返回%d根有效数据", len(bars))
	}
	return bars, nil
}

func (client *THSQuantClient) FetchMinutePoints(ctx context.Context, symbol string) ([]domain.MinutePoint, error) {
	code := thsCode(symbol)
	if code == "" {
		return nil, fmt.Errorf("无效股票代码 %q", symbol)
	}
	today := time.Now().Format("2006-01-02")
	raw, err := client.do(ctx, "high_frequency", map[string]any{
		"codes": code, "indicators": client.minuteIndicators, "startdate": today, "enddate": today,
	})
	if err != nil {
		return nil, err
	}
	points := parseTHSMinutePoints(raw, symbol)
	if len(points) == 0 {
		return nil, errors.New("同花顺高频接口未返回可识别分时数据；请检查高频权限或指标名称")
	}
	return points, nil
}

func thsCodes(symbols []string) []string {
	result := make([]string, 0, len(symbols))
	seen := make(map[string]bool)
	for _, symbol := range symbols {
		if code := thsCode(symbol); code != "" && !seen[code] {
			seen[code] = true
			result = append(result, code)
		}
	}
	return result
}

func thsCode(symbol string) string {
	symbol = strings.ToLower(strings.TrimSpace(symbol))
	if len(symbol) != 8 || (symbol[:2] != "sh" && symbol[:2] != "sz" && symbol[:2] != "bj") {
		return ""
	}
	market := "SZ"
	if symbol[:2] == "sh" {
		market = "SH"
	} else if symbol[:2] == "bj" {
		market = "BJ"
	}
	return symbol[2:] + "." + market
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func summarizeTHSError(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 300 {
		return text[:300]
	}
	return text
}

// The following parsers accept both the documented tables shape and the
// common flattened data shape returned by different Quant API products.
func parseTHSQuotes(raw []byte, symbols []string) []domain.Quote {
	rows := flattenTHSRows(raw)
	result := make([]domain.Quote, 0, len(rows))
	for _, row := range rows {
		code := firstNonEmpty(rowString(row, "thscode", "ths_code", "code", "symbol"))
		prefix := normalizeTHSSymbol(code)
		if prefix == "" {
			continue
		}
		current := rowNumber(row, "latest", "current", "price")
		previous := rowNumber(row, "preClose", "previousClose", "previous_close")
		delta := math.NaN()
		if finiteTHS(current) && finiteTHS(previous) {
			delta = current - previous
		}
		amount := rowNumber(row, "amount")
		if finiteTHS(amount) {
			amount /= 1e4 // Quote.Amount follows Tencent's ten-thousand-yuan unit.
		}
		result = append(result, domain.Quote{Symbol: prefix, Source: "同花顺QuantAPI", Code: prefix[2:], Name: rowString(row, "name", "security_name"), Current: rowNumberString(row, "latest", "current", "price"), PreviousClose: rowNumberString(row, "preClose", "previousClose", "previous_close"), Open: rowNumberString(row, "open"), High: rowNumberString(row, "high"), Low: rowNumberString(row, "low"), Turnover: rowNumberString(row, "turnoverRatio", "turnover"), VolumeRatio: rowNumberString(row, "volumeRatio", "volume_ratio"), LimitUp: rowNumberString(row, "limitUp", "limit_up"), LimitDown: rowNumberString(row, "limitDown", "limit_down"), AveragePrice: rowNumberString(row, "avgPrice", "averagePrice", "average_price"), QuoteTime: rowString(row, "time", "quoteTime", "quote_time"), Delta: delta, Percent: rowNumber(row, "changeRatio", "percent", "change_percent"), Amount: amount, Volume: rowNumber(row, "volume")})
	}
	return result
}

func parseTHSDailyBars(raw []byte, symbol string) []domain.DailyBar {
	rows := flattenTHSRows(raw)
	result := make([]domain.DailyBar, 0, len(rows))
	for _, row := range rows {
		date := firstNonEmpty(rowString(row, "date", "time", "tradeDate"))
		if len(date) >= 10 {
			date = date[:10]
		}
		if date == "" {
			continue
		}
		result = append(result, domain.DailyBar{Symbol: symbol, Source: "同花顺QuantAPI", Date: date, Open: rowNumber(row, "open"), Close: rowNumber(row, "close", "latest"), High: rowNumber(row, "high"), Low: rowNumber(row, "low"), Volume: rowNumber(row, "volume"), Amount: rowNumber(row, "amount"), Turnover: rowNumber(row, "turnoverRatio", "turnover")})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result
}

func parseTHSMinutePoints(raw []byte, symbol string) []domain.MinutePoint {
	rows := flattenTHSRows(raw)
	result := make([]domain.MinutePoint, 0, len(rows))
	for _, row := range rows {
		dateTime := firstNonEmpty(rowString(row, "time", "datetime", "date"))
		if dateTime == "" {
			continue
		}
		tradeDate, clock := dateTime, dateTime
		if len(dateTime) >= 10 {
			tradeDate = dateTime[:10]
		}
		if index := strings.IndexAny(dateTime, " T"); index >= 0 && index+1 < len(dateTime) {
			clock = dateTime[index+1:]
		}
		result = append(result, domain.MinutePoint{Symbol: symbol, Source: "同花顺QuantAPI", TradeDate: tradeDate, Time: clock, Price: rowNumber(row, "latest", "price", "close"), Average: rowNumber(row, "avgPrice", "average", "averagePrice"), Leading: rowNumber(row, "leading", "leadingIndex", "advanceDecline"), Volume: rowNumber(row, "volume"), Amount: rowNumber(row, "amount")})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Time < result[j].Time })
	return result
}

func flattenTHSRows(raw []byte) []map[string]any {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	rows := make([]map[string]any, 0)
	var collectTableRows func(map[string]any)
	collectTableRows = func(item map[string]any) {
		table, ok := item["table"].(map[string]any)
		if !ok {
			return
		}
		context := make(map[string]any)
		for _, key := range []string{"thscode", "ths_code", "code", "symbol", "date", "tradeDate", "trade_date", "time"} {
			if value, exists := item[key]; exists {
				context[key] = value
			}
		}
		length := 0
		for _, value := range table {
			if values, ok := value.([]any); ok && len(values) > length {
				length = len(values)
			}
		}
		for index := 0; index < length; index++ {
			row := make(map[string]any, len(context)+len(table))
			for key, value := range context {
				if values, ok := value.([]any); ok {
					if index < len(values) {
						row[key] = values[index]
					}
				} else {
					row[key] = value
				}
			}
			for key, value := range table {
				if values, ok := value.([]any); ok {
					if index < len(values) {
						row[key] = values[index]
					}
				} else {
					row[key] = value
				}
			}
			rows = append(rows, row)
		}
	}
	var walk func(any, map[string]any)
	walk = func(item any, inherited map[string]any) {
		switch typed := item.(type) {
		case map[string]any:
			collectTableRows(typed)
			if _, hasVectorTable := typed["table"].(map[string]any); hasVectorTable {
				return
			}
			context := inherited
			for _, key := range []string{"thscode", "ths_code", "code", "symbol", "date", "tradeDate", "trade_date"} {
				if value, ok := typed[key]; ok {
					if context == nil {
						context = make(map[string]any)
					} else {
						copyContext := make(map[string]any, len(context))
						for contextKey, contextValue := range context {
							copyContext[contextKey] = contextValue
						}
						context = copyContext
					}
					context[key] = value
				}
			}
			row := make(map[string]any, len(context)+len(typed))
			for key, value := range context {
				row[key] = value
			}
			for key, value := range typed {
				row[key] = value
			}
			if hasAnyKey(row, "thscode", "ths_code", "code", "symbol", "date", "time", "datetime") && hasAnyKey(row, "latest", "close", "price", "open") {
				rows = append(rows, row)
			}
			for _, child := range typed {
				walk(child, context)
			}
		case []any:
			for _, child := range typed {
				walk(child, inherited)
			}
		}
	}
	walk(value, nil)
	return rows
}

func hasAnyKey(row map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := row[key]; ok {
			return true
		}
	}
	return false
}

func rowString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			return fmt.Sprint(value)
		}
	}
	return ""
}

func rowNumber(row map[string]any, keys ...string) float64 {
	value := rowString(row, keys...)
	result, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return math.NaN()
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return math.NaN()
	}
	return result
}

func finiteTHS(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func rowNumberString(row map[string]any, keys ...string) string {
	value := rowNumber(row, keys...)
	if math.IsNaN(value) {
		return ""
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func normalizeTHSSymbol(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "SH")
	value = strings.TrimPrefix(value, "SZ")
	value = strings.TrimPrefix(value, "BJ")
	parts := strings.Split(value, ".")
	if len(parts) == 2 {
		value = parts[0]
		market := parts[1]
		if len(value) == 6 && (market == "SH" || market == "SZ" || market == "BJ") {
			return strings.ToLower(market) + value
		}
	}
	if len(value) == 6 {
		if strings.HasPrefix(value, "6") {
			return "sh" + value
		}
		return "sz" + value
	}
	return ""
}
