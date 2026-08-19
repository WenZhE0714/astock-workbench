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
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const sinaGlobalIndexAPIURL = "https://hq.sinajs.cn/list="

type GlobalIndexClient interface {
	FetchGlobalIndices(context.Context) ([]domain.GlobalIndex, error)
}

type SinaGlobalIndexClient struct{}

type globalIndexSpec struct {
	Symbol      string
	Region      string
	Name        string
	Format      string
	ProxySymbol string
	ProxyName   string
}

var globalIndexSpecs = []globalIndexSpec{
	{Symbol: "rt_hkHSI", Region: "港股", Name: "恒生指数", Format: "hk"},
	{Symbol: "rt_hkHSCEI", Region: "港股", Name: "恒生国企", Format: "hk"},
	{Symbol: "rt_hkHSTECH", Region: "港股", Name: "恒生科技", Format: "hk"},
	{Symbol: "b_NKY", Region: "日本", Name: "日经225", Format: "asia"},
	{Symbol: "b_TOPIX", Region: "日本", Name: "东证指数", Format: "asia"},
	{Symbol: "b_KOSPI", Region: "韩国", Name: "KOSPI", Format: "asia"},
	{Symbol: "b_KOSDAQ", Region: "韩国", Name: "KOSDAQ", Format: "asia"},
	{Symbol: "gb_dji", Region: "美国", Name: "道琼斯", Format: "us", ProxySymbol: "gb_dia", ProxyName: "DIA"},
	{Symbol: "gb_ixic", Region: "美国", Name: "纳斯达克", Format: "us", ProxySymbol: "gb_qqq", ProxyName: "QQQ"},
	{Symbol: "gb_inx", Region: "美国", Name: "标普500", Format: "us", ProxySymbol: "gb_spy", ProxyName: "SPY"},
}

var sinaGlobalRecordPattern = regexp.MustCompile(`(?m)var hq_str_([A-Za-z0-9_]+)="([^"]*)";`)

func globalNumber(fields []string, index int) float64 {
	if index < 0 || index >= len(fields) {
		return math.NaN()
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(fields[index]), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return math.NaN()
	}
	return value
}

func globalField(fields []string, index int) string {
	if index < 0 || index >= len(fields) {
		return ""
	}
	return strings.TrimSpace(fields[index])
}

func globalQuoteTime(date, clock string) string {
	date = strings.ReplaceAll(strings.TrimSpace(date), "/", "-")
	clock = strings.TrimSpace(clock)
	if date == "" {
		return clock
	}
	if clock == "" {
		return date
	}
	return date + " " + clock
}

func emptyGlobalIndex(spec globalIndexSpec) domain.GlobalIndex {
	return domain.GlobalIndex{
		Symbol: spec.Symbol, Region: spec.Region, Name: spec.Name,
		Current: math.NaN(), Delta: math.NaN(), Percent: math.NaN(),
		Open: math.NaN(), PreviousClose: math.NaN(), High: math.NaN(), Low: math.NaN(),
		Source: "新浪全球指数",
	}
}

func parseGlobalIndexRecord(spec globalIndexSpec, raw string) domain.GlobalIndex {
	result := emptyGlobalIndex(spec)
	if strings.TrimSpace(raw) == "" {
		return result
	}
	fields := strings.Split(raw, ",")
	switch spec.Format {
	case "hk":
		result.Open = globalNumber(fields, 2)
		result.PreviousClose = globalNumber(fields, 3)
		result.High = globalNumber(fields, 4)
		result.Low = globalNumber(fields, 5)
		result.Current = globalNumber(fields, 6)
		result.Delta = globalNumber(fields, 7)
		result.Percent = globalNumber(fields, 8)
		result.QuoteTime = globalQuoteTime(globalField(fields, 17), globalField(fields, 18))
	case "asia":
		result.Current = globalNumber(fields, 1)
		result.Delta = globalNumber(fields, 2)
		result.Percent = globalNumber(fields, 3)
		result.Open = globalNumber(fields, 8)
		result.PreviousClose = globalNumber(fields, 9)
		result.High = globalNumber(fields, 10)
		result.Low = globalNumber(fields, 11)
		clock := globalField(fields, 5)
		if clock == "" {
			clock = globalField(fields, 7)
		}
		result.QuoteTime = globalQuoteTime(globalField(fields, 6), clock)
	case "us":
		result.Current = globalNumber(fields, 1)
		result.Percent = globalNumber(fields, 2)
		result.QuoteTime = strings.TrimSpace(globalField(fields, 3))
		result.Delta = globalNumber(fields, 4)
		result.Open = globalNumber(fields, 5)
		result.High = globalNumber(fields, 6)
		result.Low = globalNumber(fields, 7)
		result.PreviousClose = globalNumber(fields, 26)
	}
	return result
}

func globalExtendedSession(value string) string {
	parsed, err := time.Parse("Jan 2 03:04PM MST", strings.TrimSpace(value))
	if err != nil {
		return "延长"
	}
	minutes := parsed.Hour()*60 + parsed.Minute()
	if minutes < 9*60+30 {
		return "盘前"
	}
	if minutes >= 16*60 {
		return "盘后"
	}
	return "延长"
}

func parseGlobalExtendedQuote(spec globalIndexSpec, raw string) *domain.GlobalExtendedQuote {
	if spec.ProxySymbol == "" || strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.Split(raw, ",")
	price := globalNumber(fields, 21)
	percent := globalNumber(fields, 22)
	delta := globalNumber(fields, 23)
	quoteTime := globalField(fields, 24)
	if math.IsNaN(price) || quoteTime == "" {
		return nil
	}
	return &domain.GlobalExtendedQuote{
		Session: globalExtendedSession(quoteTime), Symbol: strings.ToUpper(strings.TrimPrefix(spec.ProxySymbol, "gb_")),
		Name: spec.ProxyName, Price: price, Delta: delta, Percent: percent,
		Volume: globalNumber(fields, 27), QuoteTime: quoteTime, Source: "新浪美股ETF延长时段",
	}
}

// ParseSinaGlobalIndexPayload normalizes the three record layouts used by
// Sina for Hong Kong, East Asian and US indices into a stable display order.
func ParseSinaGlobalIndexPayload(raw string) []domain.GlobalIndex {
	records := make(map[string]string)
	for _, match := range sinaGlobalRecordPattern.FindAllStringSubmatch(raw, -1) {
		records[match[1]] = match[2]
	}
	result := make([]domain.GlobalIndex, 0, len(globalIndexSpecs))
	for _, spec := range globalIndexSpecs {
		item := parseGlobalIndexRecord(spec, records[spec.Symbol])
		item.Extended = parseGlobalExtendedQuote(spec, records[spec.ProxySymbol])
		result = append(result, item)
	}
	return result
}

func globalIndexAddress(base string) string {
	symbols := make([]string, 0, len(globalIndexSpecs))
	for _, spec := range globalIndexSpecs {
		symbols = append(symbols, spec.Symbol)
		if spec.ProxySymbol != "" {
			symbols = append(symbols, spec.ProxySymbol)
		}
	}
	query := strings.Join(symbols, ",")
	if strings.Contains(base, "{symbols}") {
		return strings.Replace(base, "{symbols}", url.QueryEscape(query), 1)
	}
	return base + query
}

func (SinaGlobalIndexClient) FetchGlobalIndices(ctx context.Context) ([]domain.GlobalIndex, error) {
	base := os.Getenv("ASTOCK_SINA_GLOBAL_INDEX_API_URL")
	if base == "" {
		base = sinaGlobalIndexAPIURL
	}
	raw, err := fetchDecodedWithHeaders(ctx, globalIndexAddress(base), simplifiedchinese.GB18030, map[string]string{
		"Referer": "https://finance.sina.com.cn/",
	})
	if err != nil {
		return nil, err
	}
	items := ParseSinaGlobalIndexPayload(raw)
	available := 0
	for _, item := range items {
		if !math.IsNaN(item.Current) {
			available++
		}
	}
	if available == 0 {
		return nil, fmt.Errorf("新浪未返回有效外盘指数")
	}
	return items, nil
}
