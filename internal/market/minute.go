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

const tencentMinuteAPIURL = "https://web.ifzq.gtimg.cn/appstock/app/minute/query"

type MinuteClient interface {
	FetchMinutePoints(context.Context, string) ([]domain.MinutePoint, error)
}

type tencentMinutePayload struct {
	Code int `json:"code"`
	Data map[string]struct {
		Timeline struct {
			Rows []string `json:"data"`
			Date string   `json:"date"`
		} `json:"data"`
	} `json:"data"`
}

func validMinuteTime(value string) bool {
	if len(value) != 4 {
		return false
	}
	hour, hourError := strconv.Atoi(value[:2])
	minute, minuteError := strconv.Atoi(value[2:])
	if hourError != nil || minuteError != nil || minute < 0 || minute > 59 {
		return false
	}
	total := hour*60 + minute
	return (total >= 9*60+30 && total <= 11*60+30) || (total >= 13*60 && total <= 15*60)
}

func formatTradeDate(value string) string {
	if len(value) != 8 {
		return value
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return value
		}
	}
	return value[:4] + "-" + value[4:6] + "-" + value[6:]
}

func ParseTencentMinutePayload(raw, symbol string) []domain.MinutePoint {
	var payload tencentMinutePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Code != 0 {
		return nil
	}
	item, found := payload.Data[symbol]
	if !found {
		return nil
	}
	tradeDate := formatTradeDate(item.Timeline.Date)
	points := make([]domain.MinutePoint, 0, len(item.Timeline.Rows))
	previousVolume := 0.0
	previousAmount := 0.0
	for _, row := range item.Timeline.Rows {
		fields := strings.Fields(row)
		if len(fields) < 4 || !validMinuteTime(fields[0]) {
			continue
		}
		price, priceOK := parseFiniteFloat(fields[1])
		cumulativeVolume, volumeOK := parseFiniteFloat(fields[2])
		cumulativeAmount, amountOK := parseFiniteFloat(fields[3])
		if !priceOK || !volumeOK || !amountOK || price <= 0 || cumulativeVolume < 0 || cumulativeAmount < 0 {
			continue
		}
		if cumulativeVolume < previousVolume || cumulativeAmount < previousAmount {
			previousVolume = 0
			previousAmount = 0
		}
		volume := cumulativeVolume - previousVolume
		amount := cumulativeAmount - previousAmount
		average := price
		if cumulativeVolume > 0 {
			average = cumulativeAmount / (cumulativeVolume * 100)
		}
		if math.IsNaN(average) || math.IsInf(average, 0) || average <= 0 {
			average = price
		}
		points = append(points, domain.MinutePoint{
			Symbol: symbol, Source: "腾讯", TradeDate: tradeDate,
			Time: fields[0][:2] + ":" + fields[0][2:], Price: price, Average: average,
			Volume: volume, Amount: amount, CumulativeVolume: cumulativeVolume, CumulativeAmount: cumulativeAmount,
		})
		previousVolume = cumulativeVolume
		previousAmount = cumulativeAmount
	}
	return points
}

func minuteAddress(base, symbol string) string {
	if strings.Contains(base, "{symbol}") {
		return strings.Replace(base, "{symbol}", url.QueryEscape(symbol), 1)
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + "code=" + url.QueryEscape(symbol)
}

func (TencentClient) FetchMinutePoints(ctx context.Context, symbol string) ([]domain.MinutePoint, error) {
	if !ValidPrefixedSymbol(symbol) {
		return nil, fmt.Errorf("无效股票代码 %q", symbol)
	}
	base := os.Getenv("ASTOCK_MINUTE_API_URL")
	if base == "" {
		base = tencentMinuteAPIURL
	}
	requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	raw, err := fetchDecoded(requestContext, minuteAddress(base, symbol), nil)
	if err != nil {
		return nil, err
	}
	points := ParseTencentMinutePayload(raw, symbol)
	if len(points) == 0 {
		return nil, fmt.Errorf("%s 未返回有效分时行情", symbol)
	}
	return points, nil
}
