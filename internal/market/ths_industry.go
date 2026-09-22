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
	"unicode/utf8"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	thsIndustryDirectoryURL = "https://q.10jqka.com.cn/thshy/"
	thsIndustryDetailURL    = "https://q.10jqka.com.cn/thshy/detail/code/{code}/"
	thsStockProfileURL      = "https://basic.10jqka.com.cn/{code}/"
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
	thsStockIndustryPattern   = regexp.MustCompile(`所属申万行业\s*[：:]\s*([^\s|，,；;]{1,40})`)
	thsStockIndustryFallback  = regexp.MustCompile(`所属行业\s*[：:]\s*([^\s|，,；;]{1,40})`)
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

func decodeTHSAuto(raw string) string {
	if utf8.ValidString(raw) {
		return raw
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().String(raw)
	if err != nil {
		return raw
	}
	return decoded
}

// fetchTHSDecoded makes the independent provider independent at the network
// layer too. Try the public mainland pages without the process-wide proxy, then
// retain the configured proxy as a fallback for environments that require it.
func fetchTHSDecoded(ctx context.Context, address string, decoder encoding.Encoding, headers map[string]string) (string, error) {
	result, directErr := fetchDecodedDirectWithHeaders(ctx, address, decoder, headers)
	if directErr == nil {
		return result, nil
	}
	if ctx.Err() != nil {
		return "", directErr
	}
	result, proxyErr := fetchDecodedWithHeaders(ctx, address, decoder, headers)
	if proxyErr == nil {
		return result, nil
	}
	return "", directErr
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
	return ParseTHSIndustryDetailLimit(raw, requestedCode, 10)
}

// ParseTHSIndustryDetailLimit parses the constituent table up to limit rows.
// A non-positive limit keeps every valid constituent returned by the source.
func ParseTHSIndustryDetailLimit(raw, requestedCode string, limit int) (domain.BoardFlow, []domain.MarketStockSnapshot, error) {
	heading := thsIndustryHeadingPattern.FindStringSubmatch(raw)
	if len(heading) != 3 || heading[2] != requestedCode {
		return domain.BoardFlow{}, nil, fmt.Errorf("未找到同花顺行业代码 %s", requestedCode)
	}
	flow := domain.BoardFlow{
		Code: "th" + requestedCode, Name: stripTHSHTML(heading[1]), Kind: domain.BoardKindIndustry, Source: "同花顺公开行业页",
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

	capacity := limit
	if capacity <= 0 {
		capacity = 64
	}
	leaders := make([]domain.MarketStockSnapshot, 0, capacity)
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
			if limit > 0 && len(leaders) >= limit {
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

func ParseTHSIndustryDirectory(raw string) []domain.Candidate {
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
		seen[code] = true
		result = append(result, domain.Candidate{Symbol: "th" + code, Name: name})
	}
	return result
}

func ParseTHSIndustryCandidates(raw, input string) []domain.Candidate {
	needle := strings.TrimSpace(input)
	if needle == "" {
		return nil
	}
	upperNeedle := strings.ToUpper(needle)
	result := make([]domain.Candidate, 0)
	for _, item := range ParseTHSIndustryDirectory(raw) {
		if !strings.Contains(item.Name, needle) && !strings.Contains(strings.ToUpper(item.Symbol), upperNeedle) {
			continue
		}
		result = append(result, item)
	}
	return result
}

// ParseTHSStockIndustry extracts the Shenwan industry label from a public F10
// page. Parsing text after stripping markup tolerates both linked and plain
// industry names used by different versions of the page.
func ParseTHSStockIndustry(raw string) string {
	plain := stripTHSHTML(raw)
	for _, pattern := range []*regexp.Regexp{thsStockIndustryPattern, thsStockIndustryFallback} {
		if match := pattern.FindStringSubmatch(plain); len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func normalizeTHSIndustryName(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), "")
	value = strings.TrimSuffix(value, "行业")
	for _, suffix := range []string{"Ⅳ", "Ⅲ", "Ⅱ", "Ⅰ", "IV", "III", "II", "I"} {
		value = strings.TrimSuffix(value, suffix)
	}
	return strings.ToLower(value)
}

func matchTHSIndustry(items []domain.Candidate, industry string) (domain.Candidate, bool) {
	wanted := normalizeTHSIndustryName(industry)
	if wanted == "" {
		return domain.Candidate{}, false
	}
	for _, item := range items {
		if normalizeTHSIndustryName(item.Name) == wanted {
			return item, true
		}
	}
	for _, item := range items {
		name := normalizeTHSIndustryName(item.Name)
		if strings.Contains(name, wanted) || strings.Contains(wanted, name) {
			return item, true
		}
	}
	return domain.Candidate{}, false
}

func thsStockProfileAddress(base, symbol string) string {
	code := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(symbol)), "sh"), "sz")
	if strings.Contains(base, "{code}") {
		return strings.Replace(base, "{code}", code, 1)
	}
	return strings.TrimRight(base, "/") + "/" + code + "/"
}

// THSStockBoardClient is the independent related-board fallback. It maps a
// stock to its Shenwan industry through Tonghuashun F10, resolves the matching
// Tonghuashun industry index, then fetches that index's live snapshot.
type THSStockBoardClient struct{}

func (THSStockBoardClient) FetchBoards(ctx context.Context, symbol string) ([]domain.BoardFlow, error) {
	if !ValidPrefixedSymbol(symbol) {
		return nil, fmt.Errorf("无效股票代码 %q", symbol)
	}
	profileBase := os.Getenv("ASTOCK_THS_STOCK_PROFILE_URL")
	if profileBase == "" {
		profileBase = thsStockProfileURL
	}
	profile, err := fetchTHSDecoded(ctx, thsStockProfileAddress(profileBase, symbol), nil, map[string]string{
		"Referer": "https://basic.10jqka.com.cn/",
	})
	if err != nil {
		return nil, fmt.Errorf("同花顺个股行业映射不可用")
	}
	industry := ParseTHSStockIndustry(decodeTHSAuto(profile))
	if industry == "" {
		return nil, fmt.Errorf("同花顺未返回所属行业")
	}

	directoryBase := os.Getenv("ASTOCK_THS_INDUSTRY_DIRECTORY_URL")
	if directoryBase == "" {
		directoryBase = thsIndustryDirectoryURL
	}
	directory, err := fetchTHSDecoded(ctx, directoryBase, simplifiedchinese.GB18030, map[string]string{
		"Referer": thsIndustryDirectoryURL,
	})
	if err != nil {
		return nil, fmt.Errorf("同花顺行业目录不可用")
	}
	matched, ok := matchTHSIndustry(ParseTHSIndustryDirectory(directory), industry)
	if !ok {
		return nil, fmt.Errorf("同花顺行业目录未匹配 %s", industry)
	}
	flow, _, err := (THSIndustryClient{}).FetchBoard(ctx, matched.Symbol)
	if err != nil {
		// The F10 page and industry directory already establish the related
		// industry. Keep that relationship when the optional live industry
		// snapshot is down, while leaving all unavailable numeric fields as NaN
		// so callers render "--" instead of inventing a quote.
		return []domain.BoardFlow{{
			Code:          matched.Symbol,
			Name:          matched.Name,
			Kind:          domain.BoardKindIndustry,
			Source:        "同花顺个股F10行业映射（行情暂不可用）",
			Percent:       math.NaN(),
			MainNet:       math.NaN(),
			MainRatio:     math.NaN(),
			Turnover:      math.NaN(),
			LeaderPercent: math.NaN(),
		}}, nil
	}
	flow.Source = "同花顺公开行业页（东方财富回退）"
	return []domain.BoardFlow{flow}, nil
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
	raw, err := fetchTHSDecoded(requestContext, base, simplifiedchinese.GB18030, map[string]string{
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
	raw, err := fetchTHSDecoded(requestContext, thsIndustryAddress(base, code), simplifiedchinese.GB18030, map[string]string{
		"Referer": "https://q.10jqka.com.cn/thshy/",
	})
	if err != nil {
		return domain.BoardFlow{}, nil, err
	}
	return ParseTHSIndustryDetailLimit(raw, code, 100)
}
