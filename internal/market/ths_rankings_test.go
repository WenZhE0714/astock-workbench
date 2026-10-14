package market

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

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

func TestTHSRankingRetriesTemporaryFailures(t *testing.T) {
	valid := thsRankingHTML(1, 1, thsRankingRow("600001", "甲", "1", "2", "3", "1亿"))
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"missing table", http.StatusOK, "<html>temporarily unavailable</html>"},
		{"wrong order", http.StatusOK, thsRankingHTML(1, 1, thsRankingRow("600001", "甲", "1", "1", "3", "1亿"), thsRankingRow("000001", "乙", "2", "2", "3", "1亿"))},
		{"server error", http.StatusServiceUnavailable, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := THSMarketRankingClient{BaseURL: "https://ths.test/{page}", HTTPClient: &http.Client{Transport: rankingRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				status, body := http.StatusOK, valid
				if calls == 1 {
					status, body = test.status, test.body
				}
				return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			items, err := client.FetchMarketRanking(context.Background(), domain.MarketRankingRapidRise, 1)
			if err != nil || calls != 2 || len(items) != 1 || items[0].Speed != 2 {
				t.Fatalf("retry failed: calls=%d items=%+v err=%v", calls, items, err)
			}
		})
	}
}

func TestTHSRankingRetryRestartsWholeSnapshot(t *testing.T) {
	paths := []string{}
	bodies := []string{
		thsRankingHTML(1, 2, thsRankingRow("600001", "旧快照", "1", "2", "3", "1亿")),
		thsRankingHTML(2, 2, thsRankingRow("000001", "跨页排序变化", "1", "3", "3", "1亿")),
		thsRankingHTML(1, 2, thsRankingRow("600002", "新快照甲", "1", "4", "3", "1亿")),
		thsRankingHTML(2, 2, thsRankingRow("000002", "新快照乙", "1", "3", "3", "1亿")),
	}
	client := THSMarketRankingClient{BaseURL: "https://ths.test/{page}", HTTPClient: &http.Client{Transport: rankingRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if len(paths) > len(bodies) {
			t.Fatal("unexpected extra retry")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(bodies[len(paths)-1]))}, nil
	})}}
	items, err := client.FetchMarketRanking(context.Background(), domain.MarketRankingRapidRise, 2)
	if err != nil || strings.Join(paths, ",") != "/1,/2,/1,/2" || len(items) != 2 || items[0].Symbol != "sh600002" || items[1].Symbol != "sz000002" {
		t.Fatalf("retry mixed snapshots: paths=%v items=%+v err=%v", paths, items, err)
	}
}

func TestTHSRankingRetryIsBounded(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable, http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			client := THSMarketRankingClient{BaseURL: "https://ths.test/{page}", HTTPClient: &http.Client{Transport: rankingRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Body: io.NopCloser(strings.NewReader("<html>unavailable</html>"))}, nil
			})}}
			items, err := client.FetchMarketRanking(context.Background(), domain.MarketRankingRapidRise, 1)
			wantCalls := 1
			if status == http.StatusOK || status >= 500 {
				wantCalls = 2
			}
			if err == nil || len(items) != 0 || calls != wantCalls {
				t.Fatalf("retry limit not respected: calls=%d want=%d items=%+v err=%v", calls, wantCalls, items, err)
			}
			if status == http.StatusOK && !errors.Is(err, errTHSRankingSnapshot) {
				t.Fatalf("snapshot failure lost: %v", err)
			}
		})
	}
}

func TestTHSRankingReservesRetryTimeAndHonorsCancellation(t *testing.T) {
	for _, cancelParent := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelParent), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			calls := 0
			client := THSMarketRankingClient{BaseURL: "https://ths.test/{page}", HTTPClient: &http.Client{Transport: rankingRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if cancelParent {
						cancel()
					}
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				if r.Context().Err() != nil {
					t.Fatal("retry has no time left")
				}
				body := thsRankingHTML(1, 1, thsRankingRow("600001", "甲", "1", "2", "3", "1亿"))
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			items, err := client.FetchMarketRanking(ctx, domain.MarketRankingRapidRise, 1)
			if cancelParent {
				if !errors.Is(err, context.Canceled) || calls != 1 || len(items) != 0 {
					t.Fatalf("cancellation ignored: calls=%d items=%+v err=%v", calls, items, err)
				}
			} else if err != nil || calls != 2 || len(items) != 1 {
				t.Fatalf("retry deadline exhausted: calls=%d items=%+v err=%v", calls, items, err)
			}
		})
	}
}
