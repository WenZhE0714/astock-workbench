package market

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseBrokerResearchPayloadKeepsOpinionMetadata(t *testing.T) {
	raw := `{"data":[{"title":"需求根基稳固","stockName":"贵州茅台","stockCode":"600519","orgName":"中邮证券","publishDate":"2026-07-23 00:00:00","infoCode":"AP20260723","researcher":"蔡雪昱,张子健","emRatingName":"买入","lastEmRatingName":"增持","ratingChange":3}]}`
	items := ParseBrokerResearchPayload(raw)
	if len(items) != 1 {
		t.Fatalf("unexpected research items: %#v", items)
	}
	item := items[0]
	if item.Symbol != "sh600519" || item.Organization != "中邮证券" || item.Rating != "买入" || item.Author != "蔡雪昱,张子健" || item.RatingChange != "3" || !strings.Contains(item.URL, "AP20260723") {
		t.Fatalf("research metadata was not preserved: %#v", item)
	}
}

func TestBrokerResearchAddressUsesExactCodeFilter(t *testing.T) {
	begin := time.Date(2026, 5, 15, 0, 0, 0, 0, time.Local)
	end := time.Date(2026, 8, 13, 0, 0, 0, 0, time.Local)
	address := brokerResearchAddress(brokerResearchAPIURL, "600519", begin, end, 5)
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("code") != "600519" || query.Get("stockCode") != "" || query.Get("beginTime") != "2026-05-15" || query.Get("pageSize") != "5" {
		t.Fatalf("unexpected research query: %s", address)
	}
}
