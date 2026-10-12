package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const THSMinuteSpeedSource = "同花顺分时1分钟"
const thsFundIndexURL = "https://d.10jqka.com.cn/v6/realhead/hs_{code}/last.js"
const thsFundMinuteURL = "https://d.10jqka.com.cn/v6/time/hs_{code}/last.js"

func thsFundIndexCode(symbol string) string {
	switch symbol {
	case "sh000001":
		return "1A0001"
	case "sz399001":
		return "399001"
	case "sz399006":
		return "399006"
	}
	return ""
}

// These public responses wrap JSON in a fixed callback. Decode the JSON;
// never execute the upstream JavaScript.
func decodeTHSCallback(raw, callback string, target any) error {
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), ";"))
	if !strings.HasPrefix(raw, callback+"(") || !strings.HasSuffix(raw, ")") {
		return fmt.Errorf("同花顺响应回调不匹配")
	}
	return json.Unmarshal([]byte(raw[len(callback)+1:len(raw)-1]), target)
}

func ParseTHSIndexFundFlowPayload(raw, symbol string) (domain.FundFlow, error) {
	code := thsFundIndexCode(symbol)
	if code == "" {
		return domain.FundFlow{}, fmt.Errorf("不支持的指数资金代码 %s", symbol)
	}
	var payload struct {
		Items map[string]json.RawMessage `json:"items"`
	}
	if err := decodeTHSCallback(raw, "quotebridge_v6_realhead_hs_"+code+"_last", &payload); err != nil {
		return domain.FundFlow{}, err
	}
	text := func(key string) string {
		var value string
		_ = json.Unmarshal(payload.Items[key], &value)
		return value
	}
	marketID := "32"
	if symbol == "sh000001" {
		marketID = "16"
	}
	if text("5") != code || text("marketid") != marketID {
		return domain.FundFlow{}, fmt.Errorf("同花顺指数资金证券代码不匹配")
	}
	if _, err := time.ParseInLocation("2006-01-02 15:04", text("updateTime"), time.FixedZone("Asia/Shanghai", 8*60*60)); err != nil {
		return domain.FundFlow{}, fmt.Errorf("同花顺指数资金缺少有效数据时间")
	}
	// 223/224 are super-large buy/sell amounts, 225/226 large amounts.
	// Unlike realFunds, realhead already uses yuan.
	values := make([]float64, 4)
	for index, key := range []string{"223", "224", "225", "226"} {
		values[index] = rawNumber(payload.Items[key])
		if !finiteTHS(values[index]) || values[index] < 0 {
			return domain.FundFlow{}, fmt.Errorf("同花顺指数资金字段无效: %s", key)
		}
	}
	net := (values[0] - values[1]) + (values[2] - values[3])
	if !finiteTHS(net) {
		return domain.FundFlow{}, fmt.Errorf("同花顺指数大单净额无效")
	}
	ratio := math.NaN()
	if amount := rawNumber(payload.Items["19"]); finiteTHS(amount) && amount > 0 {
		ratio = net / amount * 100
		if !finiteTHS(ratio) {
			ratio = math.NaN()
		}
	}
	return domain.FundFlow{
		Symbol: symbol, Name: text("name"), Source: THSFundFlowSource, DataTime: text("updateTime"),
		Price: rawNumber(payload.Items["10"]), Percent: rawNumber(payload.Items["199112"]),
		Speed: math.NaN(), MainNet: net, MainRatio: ratio,
	}, nil
}

func (client THSFundFlowClient) fetchSnapshot(ctx context.Context, base, code string) (string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	address := strings.ReplaceAll(base, "{code}", code)
	headers := map[string]string{"Referer": "https://stockpage.10jqka.com.cn/"}
	if client.HTTPClient != nil {
		return fetchDecodedWithClient(requestCtx, client.HTTPClient, address, nil, headers)
	}
	return fetchTHSDecoded(requestCtx, address, nil, headers)
}

func (client THSFundFlowClient) fetchIndex(ctx context.Context, symbol string) (domain.FundFlow, error) {
	base := firstNonEmpty(client.IndexURL, os.Getenv("ASTOCK_THS_INDEX_FUND_FLOW_API_URL"), thsFundIndexURL)
	raw, err := client.fetchSnapshot(ctx, base, thsFundIndexCode(symbol))
	if err != nil {
		return domain.FundFlow{}, err
	}
	flow, err := ParseTHSIndexFundFlowPayload(raw, symbol)
	if err == nil {
		flow.FetchedAt = time.Now()
	}
	return flow, err
}

func ParseTHSMinuteSpeed(raw, symbol string) (float64, string, error) {
	if !supportsTHSFundFlow(symbol) {
		return math.NaN(), "", fmt.Errorf("不支持的涨速代码 %s", symbol)
	}
	code := symbol[2:]
	var payload map[string]struct {
		Date string `json:"date"`
		Data string `json:"data"`
	}
	if err := decodeTHSCallback(raw, "quotebridge_v6_time_hs_"+code+"_last", &payload); err != nil {
		return math.NaN(), "", err
	}
	item, ok := payload["hs_"+code]
	date, dateErr := time.Parse("20060102", item.Date)
	if !ok || dateErr != nil {
		return math.NaN(), "", fmt.Errorf("同花顺分时缺少有效日期/代码")
	}
	prices := make(map[int]float64)
	latest := -1
	for _, record := range strings.Split(item.Data, ";") {
		fields := strings.Split(record, ",")
		if len(fields) < 2 {
			continue
		}
		clock, err := time.Parse("1504", fields[0])
		if err != nil {
			continue
		}
		minute := clock.Hour()*60 + clock.Minute()
		// Exclude pre-open quotes and after-hours fixed-price trades from speed.
		if (minute < 570 || minute > 690) && (minute < 780 || minute > 900) {
			continue
		}
		if minute > latest {
			latest = minute
		}
		price, valid := parseFiniteFloat(fields[1])
		if !valid || price <= 0 {
			prices[minute] = math.NaN()
		} else {
			prices[minute] = price
		}
	}
	current, currentOK := prices[latest]
	previous, previousOK := prices[latest-1]
	if !currentOK || !previousOK || !finiteTHS(current) || !finiteTHS(previous) {
		return math.NaN(), "", fmt.Errorf("同花顺涨速缺少连续一分钟价格")
	}
	speed := (current/previous - 1) * 100
	if !finiteTHS(speed) {
		return math.NaN(), "", fmt.Errorf("同花顺涨速无效")
	}
	return speed, fmt.Sprintf("%s %02d:%02d", date.Format("2006-01-02"), latest/60, latest%60), nil
}

func (client THSFundFlowClient) fetchSpeed(ctx context.Context, symbol string) (float64, string, error) {
	base := firstNonEmpty(client.MinuteURL, os.Getenv("ASTOCK_THS_SPEED_API_URL"), thsFundMinuteURL)
	raw, err := client.fetchSnapshot(ctx, base, symbol[2:])
	if err != nil {
		return math.NaN(), "", err
	}
	return ParseTHSMinuteSpeed(raw, symbol)
}
