package market

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const quoteAPIURL = "https://qt.gtimg.cn/q="

var BroadMarketSymbols = []string{"sh000001", "sz399001", "sz399006"}

// MarketAmountSymbols are quote-only helpers. The Shenzhen Component's quote
// amount currently represents the full Shenzhen market, but sz399106 is used
// explicitly so the real-time and historical definitions stay unambiguous.
var MarketAmountSymbols = []string{"sh000001", "sz399106", "bj899050"}

var QuoteMarketSymbols = []string{"sh000001", "sz399001", "sz399006", "sz399106", "bj899050"}

var quoteRecordPattern = regexp.MustCompile(`v_(sh|sz|bj)([0-9]{6})="([^"]*)";`)

type QuoteClient interface {
	Fetch(context.Context, []string) ([]domain.Quote, error)
}

type TencentClient struct{}

func IsBroadMarketSymbol(symbol string) bool {
	for _, candidate := range QuoteMarketSymbols {
		if symbol == candidate {
			return true
		}
	}
	return false
}

func field(fields []string, index int) string {
	if index < 0 || index >= len(fields) || fields[index] == "" {
		return "--"
	}
	return fields[index]
}

func numberField(fields []string, index int) float64 {
	if index < 0 || index >= len(fields) || fields[index] == "" {
		return math.NaN()
	}
	value, err := strconv.ParseFloat(fields[index], 64)
	if err != nil {
		return math.NaN()
	}
	return value
}

func formatQuoteTime(value string) string {
	if len(value) != 14 {
		if value == "" {
			return "--"
		}
		return value
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return value
		}
	}
	return fmt.Sprintf("%s-%s-%s %s:%s:%s",
		value[0:4], value[4:6], value[6:8],
		value[8:10], value[10:12], value[12:14])
}

func ParseQuotePayload(raw string) []domain.Quote {
	matches := quoteRecordPattern.FindAllStringSubmatch(raw, -1)
	result := make([]domain.Quote, 0, len(matches))
	for _, match := range matches {
		fields := strings.Split(match[3], "~")
		if len(fields) < 35 || fields[1] == "" {
			continue
		}
		bids := make([]domain.DepthLevel, 0, 5)
		asks := make([]domain.DepthLevel, 0, 5)
		for level := 0; level < 5; level++ {
			bids = append(bids, domain.DepthLevel{
				Level: level + 1, Price: field(fields, 9+level*2), Volume: field(fields, 10+level*2),
			})
			asks = append(asks, domain.DepthLevel{
				Level: level + 1, Price: field(fields, 19+level*2), Volume: field(fields, 20+level*2),
			})
		}
		result = append(result, domain.Quote{
			Symbol:         match[1] + match[2],
			Name:           fields[1],
			TaskName:       fields[1],
			Code:           field(fields, 2),
			Current:        field(fields, 3),
			PreviousClose:  field(fields, 4),
			Open:           field(fields, 5),
			QuoteTime:      formatQuoteTime(field(fields, 30)),
			Delta:          numberField(fields, 31),
			Percent:        numberField(fields, 32),
			High:           field(fields, 33),
			Low:            field(fields, 34),
			Volume:         numberField(fields, 36),
			Amount:         numberField(fields, 37),
			Turnover:       field(fields, 38),
			PETTM:          field(fields, 39),
			Amplitude:      field(fields, 43),
			MarketCap:      numberField(fields, 44),
			FloatMarketCap: numberField(fields, 45),
			PB:             field(fields, 46),
			LimitUp:        field(fields, 47),
			LimitDown:      field(fields, 48),
			VolumeRatio:    field(fields, 49),
			AveragePrice:   field(fields, 51),
			PEStatic:       field(fields, 52),
			Bids:           bids,
			Asks:           asks,
		})
	}
	return result
}

func (TencentClient) Fetch(ctx context.Context, symbols []string) ([]domain.Quote, error) {
	base := os.Getenv("ASTOCK_QUOTE_API_URL")
	if base == "" {
		base = quoteAPIURL
	}
	query := strings.Join(symbols, ",")
	address := base + query
	if strings.Contains(base, "{query}") {
		address = strings.Replace(base, "{query}", url.QueryEscape(query), 1)
	}
	raw, err := fetchDecoded(ctx, address, simplifiedchinese.GB18030)
	if err != nil {
		return nil, err
	}
	quotes := ParseQuotePayload(raw)
	if len(quotes) == 0 {
		return nil, fmt.Errorf("未解析到有效行情，请检查股票代码或稍后重试")
	}
	return quotes, nil
}
