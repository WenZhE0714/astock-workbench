package market

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func thsRankingRow(code, name, percent, speed, turnover, amount string) string {
	return fmt.Sprintf(`<tr><td>1</td><td><a>%s</a></td><td><a>%s</a></td><td>30.47</td><td>%s</td><td>1.00</td><td>%s</td><td>%s</td><td>1.2</td><td>3.4</td><td>%s</td></tr>`, code, name, percent, speed, turnover, amount)
}

func thsRankingHTML(page, pages int, rows ...string) string {
	return fmt.Sprintf(`<table><tbody>%s</tbody></table><span class="page_info">%d/%d</span>`, strings.Join(rows, ""), page, pages)
}

func TestTHSRankingParsesUnitsScopeAndMissingFields(t *testing.T) {
	raw := thsRankingHTML(1, 2,
		thsRankingRow("920001", "北交所", "30", "1", "2", "3亿"),
		thsRankingRow("301052", "果麦&amp;文化", "20.01", "0.5", "25.41", "5.84亿"),
		thsRankingRow("688004", "博汇科技", "20", "--", "--", "1200.5万"),
		thsRankingRow("600abc", "无效", "10", "1", "2", "3亿"),
	)
	page, err := parseTHSMarketRankingPage(raw, domain.MarketRankingGainers)
	if err != nil {
		t.Fatal(err)
	}
	if page.page != 1 || page.pages != 2 || len(page.items) != 2 {
		t.Fatalf("bad page: %+v", page)
	}
	first, second := page.items[0], page.items[1]
	if first.Symbol != "sz301052" || first.Name != "果麦&文化" || first.Price != 30.47 || first.Percent != 20.01 || first.Speed != 0.5 || first.Turnover != 25.41 || first.Amount != 5.84e8 || first.Source != THSMarketRankingSource || first.Industry != "--" {
		t.Fatalf("bad ranking values: %+v", first)
	}
	if second.Symbol != "sh688004" || second.Amount != 12005000 || !math.IsNaN(second.Speed) || !math.IsNaN(second.Turnover) {
		t.Fatalf("missing fields or units changed: %+v", second)
	}
}

func TestTHSRankingFetchUsesCorrectSortAndLimit(t *testing.T) {
	for _, test := range []struct {
		kind          domain.MarketRankingKind
		metric, order string
	}{
		{domain.MarketRankingGainers, "zdf", "desc"}, {domain.MarketRankingLosers, "zdf", "asc"},
		{domain.MarketRankingRapidRise, "zs", "desc"}, {domain.MarketRankingAmount, "cje", "desc"},
		{domain.MarketRankingTurnover, "hs", "desc"},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			client := THSMarketRankingClient{BaseURL: "https://ths.test/{metric}/{order}/{page}", HTTPClient: &http.Client{Transport: rankingRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/"+test.metric+"/"+test.order+"/1" || r.Header.Get("Referer") == "" {
					t.Fatalf("bad request: %s", r.URL)
				}
				first, second := "20", "10"
				if test.kind == domain.MarketRankingLosers {
					first, second = "-20", "-10"
				}
				body := thsRankingHTML(1, 1, thsRankingRow("600001", "甲", first, "2", "3", "2亿"), thsRankingRow("000001", "乙", second, "1", "2", "1亿"))
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			items, err := client.FetchMarketRanking(context.Background(), test.kind, 1)
			if err != nil || len(items) != 1 || items[0].Symbol != "sh600001" {
				t.Fatalf("bad result: %+v %v", items, err)
			}
		})
	}
}

func TestTHSRankingPaginatesAfterFilteringAndDeduplicates(t *testing.T) {
	calls := 0
	client := THSMarketRankingClient{BaseURL: "https://ths.test/{page}", HTTPClient: &http.Client{Transport: rankingRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body := thsRankingHTML(1, 2, thsRankingRow("920001", "北交所", "30", "2", "3", "2亿"), thsRankingRow("600001", "甲", "20", "2", "3", "2亿"))
		if r.URL.Path == "/2" {
			body = thsRankingHTML(2, 2, thsRankingRow("600001", "甲", "20", "2", "3", "2亿"), thsRankingRow("000001", "乙", "10", "1", "2", "1亿"))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	items, err := client.FetchMarketRanking(context.Background(), domain.MarketRankingGainers, 2)
	if err != nil || calls != 2 || len(items) != 2 || items[1].Symbol != "sz000001" {
		t.Fatalf("pagination failed: calls=%d items=%+v err=%v", calls, items, err)
	}
}

func TestTHSRankingRejectsBlockedWrongOrderAndRepeatedPage(t *testing.T) {
	for _, raw := range []string{`<html>blocked</html>`, thsRankingHTML(1, 1), thsRankingHTML(1, 1, thsRankingRow("600001", "甲", "1", "1", "1", "1亿"), thsRankingRow("000001", "乙", "2", "2", "2", "2亿"))} {
		if _, err := parseTHSMarketRankingPage(raw, domain.MarketRankingGainers); err == nil {
			t.Fatalf("accepted invalid ranking: %s", raw)
		}
	}
	client := THSMarketRankingClient{BaseURL: "https://ths.test/{page}", HTTPClient: &http.Client{Transport: rankingRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(thsRankingHTML(1, 2, thsRankingRow("600001", "甲", "1", "1", "1", "1亿"))))}, nil
	})}}
	if _, err := client.FetchMarketRanking(context.Background(), domain.MarketRankingGainers, 2); err == nil || !strings.Contains(err.Error(), "页码") {
		t.Fatalf("repeated page was accepted: %v", err)
	}
}
