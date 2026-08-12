package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const stockNewsAPIURL = "https://search-api-web.eastmoney.com/search/jsonp"

type StockNewsClient interface {
	FetchStockNews(context.Context, string, int) ([]domain.StockNewsItem, error)
}

var newsHTMLPattern = regexp.MustCompile(`<[^>]+>`)

func cleanNewsText(value string) string {
	value = newsHTMLPattern.ReplaceAllString(value, "")
	return strings.Join(strings.Fields(value), " ")
}

func ParseStockNewsPayload(raw string) []domain.StockNewsItem {
	raw = strings.TrimSpace(raw)
	start := strings.IndexByte(raw, '(')
	end := strings.LastIndexByte(raw, ')')
	if start >= 0 && end > start {
		raw = raw[start+1 : end]
	}
	var payload struct {
		Result struct {
			Items []struct {
				Date   string `json:"date"`
				Title  string `json:"title"`
				Source string `json:"mediaName"`
				Code   string `json:"code"`
			} `json:"cmsArticleWebOld"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	result := make([]domain.StockNewsItem, 0, len(payload.Result.Items))
	seen := make(map[string]bool)
	for _, item := range payload.Result.Items {
		title := cleanNewsText(item.Title)
		if title == "" || seen[title] {
			continue
		}
		seen[title] = true
		date := strings.TrimSpace(item.Date)
		if len(date) > 19 {
			date = date[:19]
		}
		address := ""
		if item.Code != "" {
			address = "https://finance.eastmoney.com/a/" + item.Code + ".html"
		}
		result = append(result, domain.StockNewsItem{
			Date: date, Title: title, Source: cleanNewsText(item.Source), URL: address,
		})
	}
	return result
}

func stockNewsAddress(base, code string, limit int) string {
	if strings.Contains(base, "{code}") {
		return strings.ReplaceAll(base, "{code}", url.QueryEscape(code))
	}
	callback := "astockNews"
	parameter := map[string]any{
		"uid": "", "keyword": code, "type": []string{"cmsArticleWebOld"},
		"client": "web", "clientType": "web", "clientVersion": "curr",
		"param": map[string]any{"cmsArticleWebOld": map[string]any{
			"searchScope": "default", "sort": "default", "pageIndex": 1,
			"pageSize": limit, "preTag": "<em>", "postTag": "</em>",
		}},
	}
	encoded, _ := json.Marshal(parameter)
	values := url.Values{"cb": {callback}, "param": {string(encoded)}, "_": {strconv.FormatInt(time.Now().UnixMilli(), 10)}}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

func (EastmoneyClient) FetchStockNews(ctx context.Context, symbol string, limit int) ([]domain.StockNewsItem, error) {
	if len(symbol) == 8 {
		symbol = symbol[2:]
	}
	if len(symbol) != 6 {
		return nil, fmt.Errorf("无效股票代码 %q", symbol)
	}
	if limit < 1 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}
	base := os.Getenv("ASTOCK_STOCK_NEWS_API_URL")
	if base == "" {
		base = stockNewsAPIURL
	}
	requestContext, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	raw, err := fetchDecodedWithHeaders(requestContext, stockNewsAddress(base, symbol, limit), nil, map[string]string{
		"Accept":  "*/*",
		"Referer": "https://so.eastmoney.com/news/s?keyword=" + url.QueryEscape(symbol),
	})
	if err != nil {
		return nil, err
	}
	items := ParseStockNewsPayload(raw)
	if len(items) == 0 {
		return nil, fmt.Errorf("未解析到个股新闻线索")
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
