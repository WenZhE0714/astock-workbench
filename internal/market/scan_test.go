package market

import (
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestParseIndustryScanPayload(t *testing.T) {
	raw := `{"data":{"diff":[{"f3":3.1,"f8":4.03,"f12":"BK1320","f14":"逆变器","f62":1040000000,"f104":9,"f105":1,"f106":0,"f128":"德业股份","f136":6.58,"f140":"605117","f184":6.55}]}}`
	items := ParseIndustryScanPayload(raw)
	if len(items) != 1 {
		t.Fatalf("unexpected industry rows: %#v", items)
	}
	item := items[0]
	if item.Code != "BK1320" || item.Name != "逆变器" || item.MainNet != 1040000000 || item.RiseCount != 9 || item.LeaderCode != "605117" {
		t.Fatalf("unexpected industry item: %#v", item)
	}
}

func TestParseMarketStockScanPayload(t *testing.T) {
	raw := `{"data":{"diff":[{"f2":91.4,"f3":6.58,"f6":2344000000,"f8":2.04,"f10":1.8,"f12":"605117","f13":1,"f14":"德业股份","f15":92.3,"f16":84.8,"f17":85,"f18":85.75,"f20":116370000000,"f22":0.09,"f62":161000000,"f100":"光伏设备","f184":6.87}]}}`
	items := ParseMarketStockScanPayload(raw)
	if len(items) != 1 {
		t.Fatalf("unexpected stock rows: %#v", items)
	}
	item := items[0]
	if item.Symbol != "sh605117" || item.Name != "德业股份" || item.Amount != 2344000000 || item.VolumeRatio != 1.8 || item.MainRatio != 6.87 {
		t.Fatalf("unexpected stock item: %#v", item)
	}
}

func TestParseMarketAnnouncementPayload(t *testing.T) {
	raw := `{"data":{"list":[{"art_code":"AN1","notice_date":"2026-08-11 00:00:00","title":"老百姓:关于回购股份的公告","codes":[{"market_code":"1","short_name":"老百姓","stock_code":"603883"}]}]}}`
	items := ParseMarketAnnouncementPayload(raw)
	if len(items) != 1 || items[0].Symbol != "sh603883" || items[0].Date != "2026-08-11" || items[0].ArtCode != "AN1" {
		t.Fatalf("unexpected announcements: %#v", items)
	}
}

func TestScanRankingAddressUsesBoundedMetricAndOrder(t *testing.T) {
	address, err := scanRankingAddress("https://example.com/api", "universe", domain.MarketScanByMainNet, false, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"fid=f62", "po=0", "pz=100", "fs=universe"} {
		if !strings.Contains(address, expected) {
			t.Fatalf("scan address missing %q: %s", expected, address)
		}
	}
}
