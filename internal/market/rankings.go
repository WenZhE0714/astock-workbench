package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const (
	marketRankingAPIURL      = "https://82.push2.eastmoney.com/api/qt/clist/get"
	marketRankingFallbackURL = "https://push2.eastmoney.com/api/qt/clist/get"
	marketRankingDelayURL    = "https://push2delay.eastmoney.com/api/qt/clist/get"
	marketRankingUniverse    = "m:0+t:6,m:0+t:80,m:1+t:2,m:1+t:23"
)

type MarketRankingClient interface {
	FetchMarketRanking(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error)
}

type marketRankingPayload struct {
	Data *struct {
		Diff []struct {
			Price    json.RawMessage `json:"f2"`
			Percent  json.RawMessage `json:"f3"`
			Code     string          `json:"f12"`
			Market   json.RawMessage `json:"f13"`
			Name     string          `json:"f14"`
			Speed    json.RawMessage `json:"f22"`
			Industry string          `json:"f100"`
		} `json:"diff"`
	} `json:"data"`
}

func ParseMarketRankingPayload(raw string) []domain.MarketRankingItem {
	var payload marketRankingPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Data == nil {
		return nil
	}
	result := make([]domain.MarketRankingItem, 0, len(payload.Data.Diff))
	for _, item := range payload.Data.Diff {
		if len(item.Code) != 6 || strings.TrimSpace(item.Name) == "" {
			continue
		}
		prefix := "sz"
		if marketNumber(item.Market, item.Code) == 1 {
			prefix = "sh"
		}
		symbol := prefix + item.Code
		if !ValidPrefixedSymbol(symbol) {
			continue
		}
		industry := strings.TrimSpace(item.Industry)
		if industry == "" || industry == "-" {
			industry = "--"
		}
		result = append(result, domain.MarketRankingItem{
			Symbol:   symbol,
			Name:     strings.TrimSpace(item.Name),
			Price:    rawNumber(item.Price),
			Percent:  rawNumber(item.Percent),
			Speed:    rawNumber(item.Speed),
			Industry: industry,
		})
	}
	return result
}

func marketRankingSort(kind domain.MarketRankingKind) (metric, order string, err error) {
	switch kind {
	case domain.MarketRankingGainers:
		return "f3", "1", nil
	case domain.MarketRankingLosers:
		return "f3", "0", nil
	case domain.MarketRankingRapidRise:
		return "f22", "1", nil
	default:
		return "", "", fmt.Errorf("未知榜单类型 %q", kind)
	}
}

func marketRankingAddress(base string, kind domain.MarketRankingKind, limit int) (string, error) {
	metric, order, err := marketRankingSort(kind)
	if err != nil {
		return "", err
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if strings.Contains(base, "{metric}") || strings.Contains(base, "{order}") || strings.Contains(base, "{limit}") {
		address := strings.ReplaceAll(base, "{metric}", url.QueryEscape(metric))
		address = strings.ReplaceAll(address, "{order}", url.QueryEscape(order))
		return strings.ReplaceAll(address, "{limit}", strconv.Itoa(limit)), nil
	}
	values := url.Values{
		"fields": {"f2,f3,f12,f13,f14,f22,f100"},
		"fid":    {metric},
		"fltt":   {"2"},
		"fs":     {marketRankingUniverse},
		"invt":   {"2"},
		"np":     {"1"},
		"pn":     {"1"},
		"po":     {order},
		"pz":     {strconv.Itoa(limit)},
		"ut":     {"bd1d9ddb04089700cf9c27f6f7426281"},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode(), nil
}

func marketRankingBases() []string {
	if configured := os.Getenv("ASTOCK_MARKET_RANK_API_URL"); configured != "" {
		return []string{configured}
	}
	return []string{marketRankingAPIURL, marketRankingFallbackURL, marketRankingDelayURL}
}

func (EastmoneyClient) FetchMarketRanking(ctx context.Context, kind domain.MarketRankingKind, limit int) ([]domain.MarketRankingItem, error) {
	var lastError error
	for _, base := range marketRankingBases() {
		address, err := marketRankingAddress(base, kind, limit)
		if err != nil {
			return nil, err
		}
		requestContext, cancel := context.WithTimeout(ctx, 4*time.Second)
		raw, fetchError := fetchDecoded(requestContext, address, nil)
		cancel()
		if fetchError != nil {
			lastError = fetchError
			continue
		}
		items := ParseMarketRankingPayload(raw)
		if len(items) == 0 {
			lastError = fmt.Errorf("未解析到个股榜单")
			continue
		}
		if limit > 0 && len(items) > limit {
			items = items[:limit]
		}
		return items, nil
	}
	if lastError == nil {
		lastError = fmt.Errorf("个股榜单暂不可用")
	}
	return nil, lastError
}
