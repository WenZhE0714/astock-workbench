package market

import (
	"math"
	"net/url"
	"testing"
)

func TestParseDragonTigerPayload(t *testing.T) {
	raw := `{"success":true,"result":{"data":[
		{"SECURITY_CODE":"000603","SECURITY_NAME_ABBR":"盛达资源","TRADE_DATE":"2026-08-07 00:00:00","EXPLAIN":"4家机构买入，成功率40.14%","EXPLANATION":"连续三个交易日内，涨幅偏离值累计达到20%的证券","CLOSE_PRICE":34.43,"CHANGE_RATE":10,"BILLBOARD_NET_AMT":113039557.06,"BILLBOARD_BUY_AMT":1309617360.05,"BILLBOARD_SELL_AMT":1196577802.99,"BILLBOARD_DEAL_AMT":2506195163.04,"ACCUM_AMOUNT":8868652928,"DEAL_NET_RATIO":1.2746,"DEAL_AMOUNT_RATIO":28.259,"TURNOVERRATE":16.9158,"D1_CLOSE_ADJCHRATE":null,"D5_CLOSE_ADJCHRATE":null},
		{"SECURITY_CODE":"000603","SECURITY_NAME_ABBR":"盛达资源","TRADE_DATE":"2026-07-23 00:00:00","EXPLAIN":"3家机构买入","EXPLANATION":"日涨幅偏离值达到7%的前5只证券","CLOSE_PRICE":24.83,"CHANGE_RATE":10.0133,"BILLBOARD_NET_AMT":32940420.59,"BILLBOARD_BUY_AMT":365116241.22,"BILLBOARD_SELL_AMT":332175820.63,"BILLBOARD_DEAL_AMT":697292061.85,"ACCUM_AMOUNT":1656097547,"DEAL_NET_RATIO":1.989,"DEAL_AMOUNT_RATIO":42.1045,"TURNOVERRATE":10.3748,"D1_CLOSE_ADJCHRATE":-0.1208,"D5_CLOSE_ADJCHRATE":-3.0608}
	]}}`
	entries := ParseDragonTigerPayload(raw)
	if len(entries) != 2 {
		t.Fatalf("expected two entries, got %#v", entries)
	}
	latest := entries[0]
	if latest.Symbol != "sz000603" || latest.TradeDate != "2026-08-07" || latest.NetAmount != 113039557.06 || latest.DealAmountRatio != 28.259 {
		t.Fatalf("unexpected latest entry: %#v", latest)
	}
	if !math.IsNaN(latest.Next1Percent) || entries[1].Next5Percent != -3.0608 {
		t.Fatalf("unexpected follow-up returns: %#v", entries)
	}
}

func TestDragonTigerAddress(t *testing.T) {
	address := dragonTigerAddress("https://example.test/api", "sz000603", "2026-07-11", "2026-08-10")
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("reportName") != "RPT_DAILYBILLBOARD_DETAILSNEW" || query.Get("pageSize") != "20" {
		t.Fatalf("unexpected query: %s", address)
	}
	wantFilter := `(SECURITY_CODE="000603")(TRADE_DATE>='2026-07-11')(TRADE_DATE<='2026-08-10')`
	if query.Get("filter") != wantFilter {
		t.Fatalf("unexpected filter: %q", query.Get("filter"))
	}
}
