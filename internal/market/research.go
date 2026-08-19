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

const brokerResearchAPIURL = "https://reportapi.eastmoney.com/report/list"

type BrokerResearchClient interface {
	FetchBrokerResearch(context.Context, string, time.Time, time.Time, int) ([]domain.BrokerResearchItem, error)
}

func ParseBrokerResearchPayload(raw string) []domain.BrokerResearchItem {
	var payload struct {
		Data []struct {
			Title          string          `json:"title"`
			StockName      string          `json:"stockName"`
			StockCode      string          `json:"stockCode"`
			OrgName        string          `json:"orgName"`
			OrgShortName   string          `json:"orgSName"`
			PublishDate    string          `json:"publishDate"`
			InfoCode       string          `json:"infoCode"`
			Researcher     string          `json:"researcher"`
			Rating         string          `json:"emRatingName"`
			PreviousRating string          `json:"lastEmRatingName"`
			RatingChange   json.RawMessage `json:"ratingChange"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	items := make([]domain.BrokerResearchItem, 0, len(payload.Data))
	seen := make(map[string]bool)
	for _, item := range payload.Data {
		code := strings.TrimSpace(item.StockCode)
		title := cleanNewsText(item.Title)
		organization := cleanNewsText(item.OrgName)
		if organization == "" {
			organization = cleanNewsText(item.OrgShortName)
		}
		key := strings.TrimSpace(item.InfoCode)
		if key == "" {
			key = code + "|" + title + "|" + item.PublishDate
		}
		if len(code) != 6 || title == "" || organization == "" || seen[key] {
			continue
		}
		seen[key] = true
		publishedAt := strings.TrimSpace(item.PublishDate)
		if len(publishedAt) > 19 {
			publishedAt = publishedAt[:19]
		}
		address := ""
		if item.InfoCode != "" {
			address = "https://data.eastmoney.com/report/zw_stock.jshtml?infocode=" + url.QueryEscape(item.InfoCode)
		}
		items = append(items, domain.BrokerResearchItem{
			Symbol: stockFromResearchCode(code), Name: cleanNewsText(item.StockName), Title: title,
			Organization: organization, Author: cleanNewsText(item.Researcher), PublishedAt: publishedAt,
			SourceID: strings.TrimSpace(item.InfoCode), Rating: cleanNewsText(item.Rating),
			PreviousRating: cleanNewsText(item.PreviousRating), RatingChange: researchPayloadText(item.RatingChange), URL: address,
		})
	}
	return items
}

func researchPayloadText(value json.RawMessage) string {
	if len(value) == 0 || string(value) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(value, &text) == nil {
		return cleanNewsText(text)
	}
	return strings.TrimSpace(string(value))
}

func stockFromResearchCode(code string) string {
	if len(code) != 6 {
		return ""
	}
	if strings.HasPrefix(code, "6") || strings.HasPrefix(code, "9") {
		return "sh" + code
	}
	return "sz" + code
}

func brokerResearchAddress(base, code string, begin, end time.Time, limit int) string {
	values := url.Values{
		"pageSize": {strconv.Itoa(limit)}, "pageNo": {"1"}, "qType": {"0"}, "code": {code},
		"industryCode": {"*"}, "beginTime": {begin.Format("2006-01-02")}, "endTime": {end.Format("2006-01-02")},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

func (EastmoneyClient) FetchBrokerResearch(ctx context.Context, symbol string, begin, end time.Time, limit int) ([]domain.BrokerResearchItem, error) {
	if len(symbol) == 8 {
		symbol = symbol[2:]
	}
	if len(symbol) != 6 {
		return nil, fmt.Errorf("无效股票代码 %q", symbol)
	}
	if begin.After(end) {
		return nil, fmt.Errorf("研报起始日期晚于结束日期")
	}
	if limit < 1 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	base := os.Getenv("ASTOCK_BROKER_RESEARCH_API_URL")
	if base == "" {
		base = brokerResearchAPIURL
	}
	requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	raw, err := fetchDecodedWithHeaders(requestContext, brokerResearchAddress(base, symbol, begin, end, limit), nil, map[string]string{
		"Accept": "application/json, text/plain, */*", "Referer": "https://data.eastmoney.com/report/stock.jshtml",
	})
	if err != nil {
		return nil, err
	}
	items := ParseBrokerResearchPayload(raw)
	if len(items) == 0 {
		return nil, fmt.Errorf("未解析到近期券商研报")
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
