package market

import (
	"net/url"
	"testing"
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

func TestIndustryFlowAddressSupportsPagination(t *testing.T) {
	address := industryFlowPageAddress("https://example.test/api", 4)
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("fs") != "m:90+t:2+f:!50" || query.Get("pz") != "100" || query.Get("pn") != "4" || query.Get("fid") != "f3" {
		t.Fatalf("unexpected industry-flow query: %s", parsed.RawQuery)
	}
}
