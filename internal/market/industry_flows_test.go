package market

import (
	"net/url"
	"testing"
)

func TestParseIndustryFlowPayload(t *testing.T) {
	raw := `{"data":{"diff":[{"f12":"BK0475","f14":"银行","f3":1.25,"f62":2350000000,"f104":35,"f105":7,"f184":4.31},{"f12":"BK0896","f14":"白酒Ⅱ","f3":"-0.72","f62":"-854524336","f104":5,"f105":16,"f184":"-7.31"}]}}`
	flows := ParseIndustryFlowPayload(raw)
	if len(flows) != 2 {
		t.Fatalf("expected two industry flows, got %d", len(flows))
	}
	if flows["银行"].MainNet != 2350000000 || flows["银行"].RiseCount != 35 || flows["银行"].MainRatio != 4.31 {
		t.Fatalf("unexpected bank flow: %#v", flows["银行"])
	}
	if flows["白酒Ⅱ"].Percent != -0.72 || flows["白酒Ⅱ"].FallCount != 16 {
		t.Fatalf("unexpected liquor flow: %#v", flows["白酒Ⅱ"])
	}
}

func TestIndustryFlowAddressRequestsAllIndustryBoards(t *testing.T) {
	address := industryFlowAddress("https://example.test/api")
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("fs") != "m:90+t:2+f:!50" || query.Get("pz") != "500" || query.Get("fid") != "f3" {
		t.Fatalf("unexpected industry-flow query: %s", parsed.RawQuery)
	}
}
