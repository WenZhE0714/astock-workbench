package market

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
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
	cache NameCache
}

type AmbiguousNameError struct {
	Input      string
	Candidates []domain.Candidate
}

func (e *AmbiguousNameError) Error() string {
	return fmt.Sprintf("名称 '%s' 匹配到多只沪深 A 股，请输入完整名称或代码", e.Input)
}

func NewResolver(cache NameCache) *Resolver {
	return &Resolver{cache: cache}
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
		if len(fields) < 9 || fields[1] != "11" || fields[8] != "1" {
			continue
		}
		symbol := fields[3]
		if !ValidPrefixedSymbol(symbol) {
			continue
		}
		if !(strings.HasPrefix(symbol, "sh6") || strings.HasPrefix(symbol, "sz0") || strings.HasPrefix(symbol, "sz3")) {
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
		if len(fields) < 5 || fields[4] != "GP-A" {
			continue
		}
		symbol := fields[0] + fields[1]
		if (fields[0] == "sh" || fields[0] == "sz") && ValidPrefixedSymbol(symbol) {
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
	return result
}

func chooseCandidate(input string, items []domain.Candidate) (string, error) {
	items = uniqueCandidates(items)
	exact := make([]domain.Candidate, 0)
	for _, item := range items {
		if item.Name == input {
			exact = append(exact, item)
		}
	}
	if len(exact) == 1 {
		return exact[0].Symbol, nil
	}
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
	if symbol := r.cache.LookupSymbol(input); symbol != "" {
		return symbol, nil
	}
	var lastError error
	if items, err := searchSina(ctx, input); err == nil {
		if err := r.cache.Remember(items); err != nil {
			return "", err
		}
		if symbol, choiceError := chooseCandidate(input, items); choiceError != nil {
			return "", choiceError
		} else if symbol != "" {
			return symbol, nil
		}
	} else {
		lastError = err
	}

	if items, err := searchTencent(ctx, input); err == nil {
		if err := r.cache.Remember(items); err != nil {
			return "", err
		}
		if symbol, choiceError := chooseCandidate(input, items); choiceError != nil {
			return "", choiceError
		} else if symbol != "" {
			return symbol, nil
		}
	} else {
		lastError = err
	}
	if lastError != nil {
		return "", fmt.Errorf("股票名称 '%s' 搜索失败；请检查网络或直接输入六位代码: %w", input, lastError)
	}
	return "", fmt.Errorf("未找到沪深 A 股名称 '%s'", input)
}

func (r *Resolver) Resolve(ctx context.Context, input string) (string, error) {
	if symbol, status := InspectSymbol(input); status == "ok" {
		return symbol, nil
	} else if status == "unsupported" {
		return "", fmt.Errorf("暂不支持代码 '%s'；当前仅支持沪市和深市", input)
	}
	if strings.HasPrefix(strings.ToLower(input), "sh") || strings.HasPrefix(strings.ToLower(input), "sz") {
		return "", fmt.Errorf("无效股票代码或名称 '%s'", input)
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
