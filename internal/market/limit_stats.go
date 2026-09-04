package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const limitStatsAPIURL = "https://datacenter-web.eastmoney.com/api/data/v1/get"

const limitStatsPoolAPIURL = "https://push2ex.eastmoney.com"

type LimitStatsClient interface {
	FetchLimitStats(context.Context, string) (domain.LimitStatsSnapshot, error)
}

type limitPoolPayload struct {
	Success bool `json:"success"`
	Result  *struct {
		Data []map[string]any `json:"data"`
	} `json:"result"`
}

func limitPoolAddress(base, report, tradeDate string) string {
	columns := "SECURITY_CODE,SECURITY_NAME_ABBR,TRADE_DATE,CONTINUOUS_LIMIT_UP_NUM,LIMIT_UP_NUM,OPEN_NUM,CHANGE_RATE"
	filter := fmt.Sprintf("(TRADE_DATE='%s')", tradeDate)
	values := url.Values{
		"client": {"WEB"}, "source": {"WEB"}, "reportName": {report}, "columns": {columns},
		"filter": {filter}, "pageNumber": {"1"}, "pageSize": {"5000"},
		"sortColumns": {"CONTINUOUS_LIMIT_UP_NUM,CHANGE_RATE"}, "sortTypes": {"-1,-1"},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

func limitRowNumber(row map[string]any, keys ...string) float64 {
	for _, key := range keys {
		for actual, value := range row {
			if strings.EqualFold(strings.TrimSpace(actual), key) {
				text := strings.TrimSpace(fmt.Sprint(value))
				text = strings.Trim(text, `"`)
				if number, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", ""), 64); err == nil {
					return number
				}
			}
		}
	}
	return -1
}

func limitRowText(row map[string]any, keys ...string) string {
	for _, key := range keys {
		for actual, value := range row {
			if strings.EqualFold(strings.TrimSpace(actual), key) {
				text := strings.TrimSpace(fmt.Sprint(value))
				if text != "" && text != "<nil>" && text != "-" {
					return text
				}
			}
		}
	}
	return ""
}

func limitRowSymbol(row map[string]any) string {
	code := limitRowText(row, "c", "SECURITY_CODE", "CODE", "股票代码")
	code = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(code), "sh"), "sz"))
	if len(code) != 6 {
		return ""
	}
	market := int(limitRowNumber(row, "m", "MARKET"))
	if market == 1 || strings.HasPrefix(code, "6") {
		return "sh" + code
	}
	return "sz" + code
}

func decodeLimitPool(raw string) []map[string]any {
	var payload limitPoolPayload
	if json.Unmarshal([]byte(raw), &payload) == nil && payload.Result != nil {
		return payload.Result.Data
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal([]byte(raw), &envelope) == nil && len(envelope.Result) > 0 {
		var result struct {
			Data any `json:"data"`
		}
		if json.Unmarshal(envelope.Result, &result) == nil {
			if data, ok := result.Data.([]any); ok {
				rows := make([]map[string]any, 0, len(data))
				for _, item := range data {
					if row, ok := item.(map[string]any); ok {
						rows = append(rows, row)
					}
				}
				return rows
			}
		}
	}
	var generic struct {
		Data []map[string]any `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &generic) == nil {
		return generic.Data
	}
	return nil
}

func topicPoolAddress(base, endpoint, tradeDate string) string {
	// Eastmoney's topic-pool endpoints require YYYYMMDD while the rest of the
	// application consistently uses YYYY-MM-DD.
	poolDate := strings.ReplaceAll(strings.TrimSpace(tradeDate), "-", "")
	values := url.Values{
		"ut": {"7eea3edcaed734bea9cbfc24409ed989"}, "dpt": {"wz.ztzt"},
		"Pageindex": {"0"}, "page": {"1"}, "pagesize": {"5000"}, "limit": {"5000"}, "sort": {"fbt:asc"}, "date": {poolDate},
	}
	return strings.TrimRight(base, "/") + endpoint + "?" + values.Encode()
}

func decodeTopicPool(raw string) []map[string]any {
	var payload struct {
		Data *struct {
			Pool []map[string]any `json:"pool"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) == nil && payload.Data != nil {
		return payload.Data.Pool
	}
	var generic struct {
		Data any `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &generic) == nil {
		if rows, ok := generic.Data.([]any); ok {
			result := make([]map[string]any, 0, len(rows))
			for _, item := range rows {
				if row, ok := item.(map[string]any); ok {
					result = append(result, row)
				}
			}
			return result
		}
	}
	return nil
}

func fetchTopicPools(ctx context.Context, base, tradeDate string) ([][]map[string]any, error) {
	endpoints := []string{"/getTopicZTPool", "/getTopicDTPool", "/getTopicZBPool"}
	rows := make([][]map[string]any, len(endpoints))
	for index, endpoint := range endpoints {
		raw, err := fetchDecoded(ctx, topicPoolAddress(base, endpoint, tradeDate), nil)
		if err != nil {
			return nil, err
		}
		rows[index] = decodeTopicPool(raw)
		if rows[index] == nil {
			return nil, fmt.Errorf("东方财富 %s 返回格式不可识别", endpoint)
		}
	}
	if len(rows[0]) == 0 && len(rows[1]) == 0 && len(rows[2]) == 0 {
		return nil, fmt.Errorf("%s 暂无涨跌停池数据", tradeDate)
	}
	return rows, nil
}

func limitStatsSnapshotFromRows(rows [][]map[string]any, tradeDate, source string) domain.LimitStatsSnapshot {
	up, down, broken := rows[0], rows[1], rows[2]
	ladder := map[int][]domain.LimitStreakStock{}
	highest := 0
	for _, row := range up {
		streak := int(limitRowNumber(row, "CONTINUOUS_LIMIT_UP_NUM", "LIMIT_UP_NUM", "LBC", "连板数", "连板"))
		if streak < 1 {
			continue
		}
		stock := domain.LimitStreakStock{Symbol: limitRowSymbol(row), Name: limitRowText(row, "n", "SECURITY_NAME_ABBR", "NAME", "股票名称"), Percent: limitRowNumber(row, "zdp", "CHANGE_RATE", "涨跌幅")}
		if stock.Symbol == "" {
			continue
		}
		ladder[streak] = append(ladder[streak], stock)
		if streak > highest {
			highest = streak
		}
	}
	groups := make([]domain.LimitStreakGroup, 0, len(ladder))
	for streak := range ladder {
		stocks := ladder[streak]
		if len(stocks) > 8 {
			stocks = stocks[:8]
		}
		groups = append(groups, domain.LimitStreakGroup{Streak: streak, Count: len(ladder[streak]), Stocks: stocks})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Streak > groups[j].Streak })
	brokenRate := 0.0
	if len(up)+len(broken) > 0 {
		brokenRate = float64(len(broken)) / float64(len(up)+len(broken)) * 100
	}
	return domain.LimitStatsSnapshot{TradeDate: tradeDate, LimitUpCount: len(up), LimitDownCount: len(down), BrokenCount: len(broken), BrokenRate: brokenRate, HighestStreak: highest, StreakLadder: groups, Available: true, Source: source}
}

func (client EastmoneyClient) FetchLimitStats(ctx context.Context, tradeDate string) (domain.LimitStatsSnapshot, error) {
	if strings.TrimSpace(tradeDate) == "" {
		tradeDate = time.Now().Format("2006-01-02")
	}
	base := os.Getenv("ASTOCK_LIMIT_STATS_API_URL")
	if base == "" {
		base = limitStatsAPIURL
	}
	reports := []string{
		os.Getenv("ASTOCK_LIMIT_UP_REPORT"),
		os.Getenv("ASTOCK_LIMIT_DOWN_REPORT"),
		os.Getenv("ASTOCK_LIMIT_BROKEN_REPORT"),
	}
	if reports[0] == "" {
		reports[0] = "RPT_LIMIT_UP_POOL"
	}
	if reports[1] == "" {
		reports[1] = "RPT_LIMIT_DOWN_POOL"
	}
	if reports[2] == "" {
		reports[2] = "RPT_LIMIT_UP_BROKEN_POOL"
	}
	requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// The data-center reports were retired by Eastmoney. Query the current
	// topic-pool endpoints first; they expose the complete limit structure and
	// continuous-limit field used by the ladder.
	poolBase := os.Getenv("ASTOCK_LIMIT_STATS_POOL_API_URL")
	if poolBase == "" {
		poolBase = limitStatsPoolAPIURL
	}
	for _, base := range []string{poolBase, "https://push2.eastmoney.com"} {
		if rows, err := fetchTopicPools(requestContext, base, tradeDate); err == nil {
			return limitStatsSnapshotFromRows(rows, tradeDate, "东方财富涨跌停池"), nil
		}
	}
	rows := make([][]map[string]any, len(reports))
	primaryErr := error(nil)
	for index, report := range reports {
		raw, err := fetchDecoded(requestContext, limitPoolAddress(base, report, tradeDate), nil)
		if err != nil {
			primaryErr = err
			break
		}
		rows[index] = decodeLimitPool(raw)
		if rows[index] == nil {
			primaryErr = fmt.Errorf("东方财富 %s 返回格式不可识别", report)
			break
		}
	}
	if primaryErr != nil || (len(rows[0]) == 0 && len(rows[1]) == 0 && len(rows[2]) == 0) {
		poolBase := os.Getenv("ASTOCK_LIMIT_STATS_POOL_API_URL")
		if poolBase == "" {
			poolBase = limitStatsPoolAPIURL
		}
		endpoints := []string{"/getTopicZTPool", "/getTopicDTPool", "/getTopicZBPool"}
		bases := []string{poolBase}
		if strings.TrimRight(poolBase, "/") != "https://push2.eastmoney.com" {
			bases = append(bases, "https://push2.eastmoney.com")
		}
		for _, base := range bases {
			fallbackRows := make([][]map[string]any, len(endpoints))
			fallbackErr := error(nil)
			for index, endpoint := range endpoints {
				raw, err := fetchDecoded(requestContext, topicPoolAddress(base, endpoint, tradeDate), nil)
				if err != nil {
					fallbackErr = err
					break
				}
				fallbackRows[index] = decodeTopicPool(raw)
				if fallbackRows[index] == nil {
					fallbackErr = fmt.Errorf("东方财富 %s 返回格式不可识别", endpoint)
					break
				}
			}
			if fallbackErr == nil && (len(fallbackRows[0]) > 0 || len(fallbackRows[1]) > 0 || len(fallbackRows[2]) > 0) {
				rows, primaryErr = fallbackRows, nil
				break
			}
		}
	}
	if primaryErr != nil {
		return domain.LimitStatsSnapshot{}, primaryErr
	}
	if len(rows[0]) == 0 && len(rows[1]) == 0 && len(rows[2]) == 0 {
		return domain.LimitStatsSnapshot{}, fmt.Errorf("%s 暂无涨跌停池数据", tradeDate)
	}
	return limitStatsSnapshotFromRows(rows, tradeDate, "东方财富数据中心涨跌停池"), nil
}
