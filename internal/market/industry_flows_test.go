package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseIndustryFlowPayload(t *testing.T) {
	raw := `{"data":{"total":496,"diff":[{"f12":"BK0475","f14":"银行","f3":1.25,"f62":2350000000,"f104":35,"f105":7,"f106":1,"f128":"招商银行","f136":3.2,"f140":"600036","f184":4.31},{"f12":"BK0896","f14":"白酒Ⅱ","f3":"-0.72","f62":"-854524336","f104":5,"f105":16,"f184":"-7.31"}]}}`
	total, parsed := parseIndustryFlowPage(raw)
	if total != 496 || len(parsed) != 2 {
		t.Fatalf("unexpected industry page metadata: total=%d flows=%d", total, len(parsed))
	}
	flows := ParseIndustryFlowPayload(raw)
	if len(flows) != 2 {
		t.Fatalf("expected two industry flows, got %d", len(flows))
	}
	if flows["银行"].MainNet != 2350000000 || flows["银行"].RiseCount != 35 || flows["银行"].FlatCount != 1 || flows["银行"].MainRatio != 4.31 || flows["银行"].LeaderCode != "600036" || flows["银行"].LeaderPercent != 3.2 {
		t.Fatalf("unexpected bank flow: %#v", flows["银行"])
	}
	if flows["白酒Ⅱ"].Percent != -0.72 || flows["白酒Ⅱ"].FallCount != 16 {
		t.Fatalf("unexpected liquor flow: %#v", flows["白酒Ⅱ"])
	}
}

func TestIndustryFlowsReserveTimeForCompleteFallback(t *testing.T) {
	original, originalDirect := httpClient, directHTTPClient
	t.Cleanup(func() { httpClient, directHTTPClient = original, originalDirect })
	var pages atomic.Int32
	transport := httpRetryRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "slow.test" {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		pages.Add(1)
		page, _ := strconv.Atoi(r.URL.Query().Get("pn"))
		start := (page - 1) * 100
		count := min(100, 301-start)
		items := make([]map[string]any, count)
		for index := range items {
			items[index] = map[string]any{"f12": fmt.Sprintf("BK%04d", start+index), "f14": fmt.Sprintf("industry-%d", start+index)}
		}
		body, _ := json.Marshal(map[string]any{"data": map[string]any{"total": 301, "diff": items}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	httpClient, directHTTPClient = &http.Client{Transport: transport}, &http.Client{Transport: transport}
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	flows, err := fetchIndustryFlows(ctx, []string{"https://slow.test/flows", "https://available.test/flows"})
	if err != nil || ctx.Err() != nil || len(flows) != 301 || pages.Load() != 4 {
		t.Fatalf("fallback did not finish all pages: count=%d pages=%d err=%v", len(flows), pages.Load(), err)
	}
}

func TestIndustryFlowsRejectTruncatedPages(t *testing.T) {
	original := directHTTPClient
	t.Cleanup(func() { directHTTPClient = original })
	directHTTPClient = &http.Client{Transport: httpRetryRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":{"total":99,"diff":[{"f12":"BK0001","f14":"sample"}]}}`))}, nil
	})}
	flows, err := fetchIndustryFlows(context.Background(), []string{"https://example.test/flows"})
	if err == nil || len(flows) != 0 {
		t.Fatalf("truncated industry universe accepted: count=%d err=%v", len(flows), err)
	}
}

func TestIndustryFlowAddressSupportsPagination(t *testing.T) {
	address := industryFlowPageAddress("https://example.test/api", 4)
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("fs") != "m:90+t:2+f:!50" || query.Get("pz") != "100" || query.Get("pn") != "4" || query.Get("fid") != "f12" {
		t.Fatalf("unexpected industry-flow query: %s", parsed.RawQuery)
	}
}
