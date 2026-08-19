package market

import (
	"context"
	"fmt"
	"html"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	thsIndustryDirectoryURL = "https://q.10jqka.com.cn/thshy/"
	thsIndustryDetailURL    = "https://q.10jqka.com.cn/thshy/detail/code/{code}/"
)

var (
	thsIndustryHeadingPattern = regexp.MustCompile(`(?is)<h3>\s*([^<]+?)\s*<span>\s*(88[0-9]{4})\s*</span>\s*</h3>`)
	thsIndustryQuotePattern   = regexp.MustCompile(`(?is)<span[^>]*class=["'][^"']*board-xj[^"']*["'][^>]*>\s*([^<]+?)\s*</span>\s*<p[^>]*class=["'][^"']*board-zdf[^"']*["'][^>]*>\s*(.*?)\s*</p>`)
	thsIndustryLinkPattern    = regexp.MustCompile(`(?is)<a[^>]+href=["'][^"']*/thshy/detail/code/(88[0-9]{4})/?["'][^>]*>(.*?)</a>`)
	thsIndustryInfoPattern    = regexp.MustCompile(`(?is)<dt>\s*([^<]+?)\s*</dt>\s*<dd[^>]*>(.*?)</dd>`)
	thsIndustryBodyPattern    = regexp.MustCompile(`(?is)<tbody>(.*?)</tbody>`)
	thsIndustryRowPattern     = regexp.MustCompile(`(?is)<tr>(.*?)</tr>`)
	thsIndustryCellPattern    = regexp.MustCompile(`(?is)<td[^>]*>(.*?)</td>`)
	thsHTMLTagPattern         = regexp.MustCompile(`(?is)<[^>]+>`)
)

// IsTHSIndustrySymbol recognizes the 88xxxx industry-index namespace used by
// Tonghuashun/TDX. The canonical persisted form is th881155 so it cannot be
// confused with an exchange-listed stock.
func IsTHSIndustrySymbol(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(value, "th") {
		value = value[2:]
	}
	if len(value) != 6 || !strings.HasPrefix(value, "88") {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func THSIndustryCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(value, "th") {
		value = value[2:]
	}
	if !IsTHSIndustrySymbol(value) {
		return ""
	}
	return value
}

func thsIndustryAddress(base, code string) string {
	if strings.Contains(base, "{code}") {
		return strings.Replace(base, "{code}", code, 1)
	}
	return strings.TrimRight(base, "/") + "/" + code + "/"
}

func stripTHSHTML(value string) string {
	value = thsHTMLTagPattern.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	return strings.Join(strings.Fields(value), " ")
}

func parseTHSFloat(value string) float64 {
	value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	result, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(result) || math.IsInf(result, 0) {
		return math.NaN()
	}
	return result
}

func parseTHSMoney(value string) float64 {
	value = strings.TrimSpace(value)
	multiplier := 1.0
	switch {
	case strings.HasSuffix(value, "万亿"):
		multiplier = 1e12
		value = strings.TrimSuffix(value, "万亿")
	case strings.HasSuffix(value, "亿"):
		multiplier = 1e8
		value = strings.TrimSuffix(value, "亿")
	case strings.HasSuffix(value, "万"):
		multiplier = 1e4
		value = strings.TrimSuffix(value, "万")
	}
	result := parseTHSFloat(value)
	if math.IsNaN(result) {
		return result
	}
	return result * multiplier
}

func thsStockSymbol(code string) string {
	symbol, status := InspectSymbol(code)
	if status == "ok" {
		return symbol
	}
	return code
}

// ParseTHSIndustryDetail parses the public Tonghuashun industry page. The
// page provides the index snapshot, industry breadth, net flow and a sorted
// constituent table without requiring a browser runtime.
func ParseTHSIndustryDetail(raw, requestedCode string) (domain.BoardFlow, []domain.MarketStockSnapshot, error) {
	heading := thsIndustryHeadingPattern.FindStringSubmatch(raw)
	if len(heading) != 3 || heading[2] != requestedCode {
		return domain.BoardFlow{}, nil, fmt.Errorf("未找到同花顺行业代码 %s", requestedCode)
	}
	flow := domain.BoardFlow{
		Code: "th" + requestedCode, Name: stripTHSHTML(heading[1]), Kind: domain.BoardKindIndustry,
		Percent: math.NaN(), MainNet: math.NaN(), MainRatio: math.NaN(), Turnover: math.NaN(), LeaderPercent: math.NaN(),
		Quote: &domain.BoardQuoteSnapshot{
			Price: math.NaN(), Delta: math.NaN(), Open: math.NaN(), PreviousClose: math.NaN(),
			High: math.NaN(), Low: math.NaN(), Volume: math.NaN(), Amount: math.NaN(),
		},
	}
	if quote := thsIndustryQuotePattern.FindStringSubmatch(raw); len(quote) == 3 {
		flow.Quote.Price = parseTHSFloat(stripTHSHTML(quote[1]))
		quoteFields := strings.Fields(stripTHSHTML(quote[2]))
		if len(quoteFields) >= 1 {
			flow.Quote.Delta = parseTHSFloat(quoteFields[0])
		}
		if len(quoteFields) >= 2 {
			flow.Percent = parseTHSFloat(quoteFields[1])
		}
	}
	for _, match := range thsIndustryInfoPattern.FindAllStringSubmatch(raw, -1) {
		label := stripTHSHTML(match[1])
		value := stripTHSHTML(match[2])
		switch label {
		case "今开":
			flow.Quote.Open = parseTHSFloat(value)
		case "昨收":
			flow.Quote.PreviousClose = parseTHSFloat(value)
		case "最低":
			flow.Quote.Low = parseTHSFloat(value)
		case "最高":
			flow.Quote.High = parseTHSFloat(value)
		case "成交量(万手)":
			flow.Quote.Volume = parseTHSFloat(value)
		case "成交额(亿)":
			flow.Quote.Amount = parseTHSFloat(value) * 1e8
		case "板块涨幅":
			flow.Percent = parseTHSFloat(value)
		case "涨幅排名":
			fields := strings.Split(value, "/")
			if len(fields) == 2 {
				flow.ChangeRank, _ = strconv.Atoi(strings.TrimSpace(fields[0]))
				flow.UniverseSize, _ = strconv.Atoi(strings.TrimSpace(fields[1]))
			}
		case "涨跌家数":
			fields := strings.Fields(value)
			if len(fields) >= 2 {
				flow.RiseCount, _ = strconv.Atoi(fields[0])
				flow.FallCount, _ = strconv.Atoi(fields[1])
			}
		case "资金净流入(亿)":
			flow.MainNet = parseTHSFloat(value) * 1e8
		}
	}

	leaders := make([]domain.MarketStockSnapshot, 0, 10)
	body := thsIndustryBodyPattern.FindStringSubmatch(raw)
	if len(body) == 2 {
		for _, row := range thsIndustryRowPattern.FindAllStringSubmatch(body[1], -1) {
			cells := thsIndustryCellPattern.FindAllStringSubmatch(row[1], -1)
			if len(cells) < 11 {
				continue
			}
			values := make([]string, len(cells))
			for index := range cells {
				values[index] = stripTHSHTML(cells[index][1])
			}
			if len(values[1]) != 6 || values[2] == "" {
				continue
			}
			leaders = append(leaders, domain.MarketStockSnapshot{
				Symbol: thsStockSymbol(values[1]), Name: values[2], Industry: flow.Name,
				Price: parseTHSFloat(values[3]), Percent: parseTHSFloat(values[4]), Speed: parseTHSFloat(values[6]),
				Turnover: parseTHSFloat(values[7]), VolumeRatio: parseTHSFloat(values[8]), Amount: parseTHSMoney(values[10]),
				MainNet: math.NaN(), MainRatio: math.NaN(),
			})
			if len(leaders) == 10 {
				break
			}
		}
	}
	if len(leaders) > 0 {
		flow.LeaderCode = strings.TrimPrefix(strings.TrimPrefix(leaders[0].Symbol, "sh"), "sz")
		flow.LeaderName = leaders[0].Name
		flow.LeaderPercent = leaders[0].Percent
	}
	return flow, leaders, nil
}

type THSIndustryClient struct{}

func ParseTHSIndustryCandidates(raw, input string) []domain.Candidate {
	needle := strings.TrimSpace(input)
	if needle == "" {
		return nil
	}
	upperNeedle := strings.ToUpper(needle)
	result := make([]domain.Candidate, 0)
	seen := make(map[string]bool)
	for _, match := range thsIndustryLinkPattern.FindAllStringSubmatch(raw, -1) {
		if len(match) != 3 {
			continue
		}
		code := match[1]
		name := stripTHSHTML(match[2])
		if name == "" || seen[code] {
			continue
		}
		if !strings.Contains(name, needle) && !strings.Contains(code, upperNeedle) {
			continue
		}
		seen[code] = true
		result = append(result, domain.Candidate{Symbol: "th" + code, Name: name})
	}
	return result
}

// SearchBoards searches Tonghuashun's industry directory. 88xxxx symbols
// are a separate namespace from exchange stocks and Eastmoney BK boards, so
// they need their own directory lookup before being normalized to th881155.
func (THSIndustryClient) SearchBoards(ctx context.Context, input string) ([]domain.Candidate, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}
	base := os.Getenv("ASTOCK_THS_INDUSTRY_DIRECTORY_URL")
	if base == "" {
		base = thsIndustryDirectoryURL
	}
	requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	raw, err := fetchDecodedWithHeaders(requestContext, base, simplifiedchinese.GB18030, map[string]string{
		"Referer": thsIndustryDirectoryURL,
	})
	if err != nil {
		return nil, err
	}
	return ParseTHSIndustryCandidates(raw, input), nil
}

func (THSIndustryClient) FetchBoard(ctx context.Context, symbol string) (domain.BoardFlow, []domain.MarketStockSnapshot, error) {
	code := THSIndustryCode(symbol)
	if code == "" {
		return domain.BoardFlow{}, nil, fmt.Errorf("无效同花顺行业代码 %q", symbol)
	}
	base := os.Getenv("ASTOCK_THS_INDUSTRY_API_URL")
	if base == "" {
		base = thsIndustryDetailURL
	}
	requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	raw, err := fetchDecodedWithHeaders(requestContext, thsIndustryAddress(base, code), simplifiedchinese.GB18030, map[string]string{
		"Referer": "https://q.10jqka.com.cn/thshy/",
	})
	if err != nil {
		return domain.BoardFlow{}, nil, err
	}
	return ParseTHSIndustryDetail(raw, code)
}
