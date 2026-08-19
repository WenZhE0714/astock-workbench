package market

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	sinaSearchURL    = "https://suggest3.sinajs.cn/suggest/type=11,12,13,14,15"
	tencentSearchURL = "https://smartbox.gtimg.cn/s3/"
)

var unicodeEscapePattern = regexp.MustCompile(`\\u[0-9a-fA-F]{4}`)

type NameCache interface {
	LookupSymbol(string) string
	Remember([]domain.Candidate) error
}

type Resolver struct {
	cache       NameCache
	boardSearch BoardSearchClient
	thsSearch   BoardSearchClient
}

// BoardSearchClient supplies a lightweight name directory for industry and
// concept boards. It is intentionally optional so code-only resolution keeps
// working when the board ranking endpoint is unavailable.
type BoardSearchClient interface {
	SearchBoards(context.Context, string) ([]domain.Candidate, error)
}

type AmbiguousNameError struct {
	Input      string
	Candidates []domain.Candidate
}

func (e *AmbiguousNameError) Error() string {
	return fmt.Sprintf("名称 '%s' 匹配到多个证券或板块，请输入完整名称或代码", e.Input)
}

func NewResolver(cache NameCache) *Resolver {
	return &Resolver{cache: cache, boardSearch: EastmoneyClient{}, thsSearch: THSIndustryClient{}}
}

// AssetKindOf classifies normalized symbols before they reach an adapter.
func AssetKindOf(symbol string) domain.AssetKind {
	symbol = strings.ToLower(strings.TrimSpace(symbol))
	if (strings.HasPrefix(symbol, "bk") && len(symbol) >= 6) || IsTHSIndustrySymbol(symbol) {
		return domain.AssetKindSector
	}
	if len(symbol) == 8 && (strings.HasPrefix(symbol, "sh") || strings.HasPrefix(symbol, "sz")) {
		code := symbol[2:]
		if isConvertibleCode(code) {
			return domain.AssetKindConvertibleBond
		}
	}
	return domain.AssetKindStock
}

func isConvertibleCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	return strings.HasPrefix(code, "110") || strings.HasPrefix(code, "113") ||
		strings.HasPrefix(code, "118") || strings.HasPrefix(code, "123") ||
		strings.HasPrefix(code, "127") || strings.HasPrefix(code, "128")
}

// ValidAssetSymbol accepts stock, convertible-bond and board symbols used by
// the shared watchlist. ValidPrefixedSymbol remains stock-only for adapters
// whose upstream contract explicitly requires an A-share security.
func ValidAssetSymbol(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if IsTHSIndustrySymbol(value) {
		return true
	}
	if strings.HasPrefix(value, "bk") {
		if len(value) != 6 {
			return false
		}
		for _, character := range value[2:] {
			if character < '0' || character > '9' {
				return false
			}
		}
		return true
	}
	if !ValidPrefixedSymbol(value) {
		return false
	}
	code := value[2:]
	if !isConvertibleCode(code) {
		return true
	}
	if strings.HasPrefix(code, "110") || strings.HasPrefix(code, "113") || strings.HasPrefix(code, "118") {
		return strings.HasPrefix(value, "sh")
	}
	return strings.HasPrefix(value, "sz")
}

// InspectAsset normalizes an input into a symbol accepted by the relevant
// market adapter. Six-digit convertible-bond codes are routed to their real
// exchange instead of being mistaken for Shenzhen stocks.
func InspectAsset(input string) (string, string) {
	raw := strings.ToLower(strings.Join(strings.Fields(input), ""))
	if IsTHSIndustrySymbol(raw) {
		return "th" + THSIndustryCode(raw), "ok"
	}
	if strings.HasPrefix(raw, "bk") {
		if ValidAssetSymbol(raw) {
			return strings.ToUpper(raw), "ok"
		}
		return "", "invalid"
	}
	if ValidAssetSymbol(raw) {
		return raw, "ok"
	}
	if ValidPrefixedSymbol(raw) {
		return "", "invalid"
	}
	if len(raw) != 6 {
		return "", "invalid"
	}
	if strings.HasPrefix(raw, "88") {
		return "th" + raw, "ok"
	}
	for _, character := range raw {
		if character < '0' || character > '9' {
			return "", "invalid"
		}
	}
	if strings.HasPrefix(raw, "110") || strings.HasPrefix(raw, "113") || strings.HasPrefix(raw, "118") {
		return "sh" + raw, "ok"
	}
	if strings.HasPrefix(raw, "123") || strings.HasPrefix(raw, "127") || strings.HasPrefix(raw, "128") {
		return "sz" + raw, "ok"
	}
	symbol, status := InspectSymbol(raw)
	return symbol, status
}

func ValidPrefixedSymbol(value string) bool {
	if len(value) != 8 || (value[:2] != "sh" && value[:2] != "sz") {
		return false
	}
	for _, character := range value[2:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func InspectSymbol(input string) (string, string) {
	raw := strings.ToLower(strings.Join(strings.Fields(input), ""))
	if ValidPrefixedSymbol(raw) {
		return raw, "ok"
	}
	if len(raw) != 6 {
		return "", "invalid"
	}
	for _, character := range raw {
		if character < '0' || character > '9' {
			return "", "invalid"
		}
	}
	switch raw[0] {
	case '5', '6', '9':
		return "sh" + raw, "ok"
	case '0', '1', '2', '3':
		return "sz" + raw, "ok"
	default:
		return "", "unsupported"
	}
}

func MarketText(symbol string) string {
	if IsTHSIndustrySymbol(symbol) {
		return "同花顺行业"
	}
	if AssetKindOf(symbol) == domain.AssetKindSector {
		return "板块"
	}
	if strings.HasPrefix(symbol, "sh") {
		return "沪市"
	}
	return "深市"
}

func decodeUnicodeEscapes(value string) string {
	return unicodeEscapePattern.ReplaceAllStringFunc(value, func(match string) string {
		code, err := strconv.ParseInt(match[2:], 16, 32)
		if err != nil {
			return match
		}
		return string(rune(code))
	})
}

func ParseSinaCandidates(raw string) []domain.Candidate {
	payload := strings.TrimSpace(raw)
	payload = strings.TrimPrefix(payload, `var suggestvalue="`)
	payload = strings.TrimSuffix(payload, `";`)
	result := make([]domain.Candidate, 0)
	for _, record := range strings.Split(payload, ";") {
		fields := strings.Split(record, ",")
		if len(fields) < 9 || fields[8] != "1" {
			continue
		}
		symbol := fields[3]
		if !ValidAssetSymbol(symbol) || AssetKindOf(symbol) == domain.AssetKindSector {
			continue
		}
		if fields[1] != "11" && AssetKindOf(symbol) != domain.AssetKindConvertibleBond {
			continue
		}
		if !(strings.HasPrefix(symbol, "sh6") || strings.HasPrefix(symbol, "sh11") || strings.HasPrefix(symbol, "sh113") || strings.HasPrefix(symbol, "sh118") || strings.HasPrefix(symbol, "sz0") || strings.HasPrefix(symbol, "sz3") || strings.HasPrefix(symbol, "sz12")) {
			continue
		}
		result = append(result, domain.Candidate{Symbol: symbol, Name: fields[0]})
	}
	return result
}

func ParseTencentCandidates(raw string) []domain.Candidate {
	payload := strings.TrimSpace(decodeUnicodeEscapes(raw))
	payload = strings.TrimPrefix(payload, `v_hint="`)
	payload = strings.TrimSuffix(payload, `";`)
	result := make([]domain.Candidate, 0)
	for _, record := range strings.Split(payload, "^") {
		fields := strings.Split(record, "~")
		if len(fields) < 5 {
			continue
		}
		symbol := fields[0] + fields[1]
		if fields[4] != "GP-A" && AssetKindOf(symbol) != domain.AssetKindConvertibleBond {
			continue
		}
		if (fields[0] == "sh" || fields[0] == "sz") && ValidAssetSymbol(symbol) && AssetKindOf(symbol) != domain.AssetKindSector {
			result = append(result, domain.Candidate{Symbol: symbol, Name: fields[2]})
		}
	}
	return result
}

func uniqueCandidates(items []domain.Candidate) []domain.Candidate {
	seen := make(map[string]bool)
	result := make([]domain.Candidate, 0, len(items))
	for _, item := range items {
		if !seen[item.Symbol] {
			seen[item.Symbol] = true
			result = append(result, item)
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		return candidatePriority(result[left]) < candidatePriority(result[right])
	})
	return result
}

func candidatePriority(item domain.Candidate) int {
	if IsTHSIndustrySymbol(item.Symbol) {
		return 0
	}
	if AssetKindOf(item.Symbol) == domain.AssetKindSector {
		return 1
	}
	return 2
}

func chooseCandidate(input string, items []domain.Candidate) (string, error) {
	items = uniqueCandidates(items)
	if len(items) == 1 {
		return items[0].Symbol, nil
	}
	if len(items) > 1 {
		return "", &AmbiguousNameError{Input: input, Candidates: items}
	}
	return "", nil
}

func searchSina(ctx context.Context, input string) ([]domain.Candidate, error) {
	base := os.Getenv("ASTOCK_SINA_SEARCH_URL")
	if base == "" {
		base = sinaSearchURL
	}
	raw, err := fetchDecoded(ctx, base+"?key="+url.QueryEscape(input), simplifiedchinese.GB18030)
	if err != nil {
		return nil, err
	}
	return ParseSinaCandidates(raw), nil
}

func searchTencent(ctx context.Context, input string) ([]domain.Candidate, error) {
	base := os.Getenv("ASTOCK_TENCENT_SEARCH_URL")
	if base == "" {
		base = tencentSearchURL
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	raw, err := fetchDecoded(ctx, base+separator+"q="+url.QueryEscape(input)+"&t=all", nil)
	if err != nil {
		return nil, err
	}
	return ParseTencentCandidates(raw), nil
}

func (r *Resolver) resolveName(ctx context.Context, input string) (string, error) {
	candidates := make([]domain.Candidate, 0, 32)
	if symbol := r.cache.LookupSymbol(input); symbol != "" {
		candidates = append(candidates, domain.Candidate{Symbol: symbol, Name: input})
	}
	var lastError error
	if items, err := searchSina(ctx, input); err == nil {
		if err := r.cache.Remember(items); err != nil {
			return "", err
		}
		candidates = append(candidates, items...)
	} else {
		lastError = err
	}

	if items, err := searchTencent(ctx, input); err == nil {
		if err := r.cache.Remember(items); err != nil {
			return "", err
		}
		candidates = append(candidates, items...)
	} else {
		lastError = err
	}
	if r.boardSearch != nil {
		if items, err := r.boardSearch.SearchBoards(ctx, input); err == nil {
			if err := r.cache.Remember(items); err != nil {
				return "", err
			}
			candidates = append(candidates, items...)
		} else if lastError == nil {
			lastError = err
		}
	}
	if r.thsSearch != nil {
		if items, err := r.thsSearch.SearchBoards(ctx, input); err == nil {
			if err := r.cache.Remember(items); err != nil {
				return "", err
			}
			candidates = append(candidates, items...)
		} else if lastError == nil {
			lastError = err
		}
	}
	if symbol, choiceError := chooseCandidate(input, candidates); choiceError != nil {
		return "", choiceError
	} else if symbol != "" {
		return symbol, nil
	}
	if lastError != nil {
		return "", fmt.Errorf("证券或板块名称 '%s' 搜索失败；请检查网络或直接输入代码: %w", input, lastError)
	}
	return "", fmt.Errorf("未找到股票、转债或板块名称 '%s'", input)
}

func (r *Resolver) Resolve(ctx context.Context, input string) (string, error) {
	if symbol, status := InspectAsset(input); status == "ok" {
		return symbol, nil
	} else if status == "unsupported" {
		return "", fmt.Errorf("暂不支持代码 '%s'；当前支持沪深股票、转债、BK 板块和 88xxxx 同花顺行业", input)
	}
	lowerInput := strings.ToLower(strings.TrimSpace(input))
	if strings.HasPrefix(lowerInput, "sh") || strings.HasPrefix(lowerInput, "sz") || strings.HasPrefix(lowerInput, "bk") {
		return "", fmt.Errorf("无效证券或板块代码 '%s'", input)
	}
	allDigits := input != ""
	for _, character := range input {
		if character < '0' || character > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return "", fmt.Errorf("无效股票代码或名称 '%s'", input)
	}
	return r.resolveName(ctx, input)
}

func (r *Resolver) ResolveMany(ctx context.Context, inputs []string) ([]string, error) {
	result := make([]string, 0, len(inputs))
	seen := make(map[string]bool)
	for _, input := range inputs {
		symbol, err := r.Resolve(ctx, input)
		if err != nil {
			return nil, err
		}
		if !seen[symbol] {
			seen[symbol] = true
			result = append(result, symbol)
		}
	}
	return result, nil
}
