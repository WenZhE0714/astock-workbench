package market

import (
	"strings"
	"testing"
)

func TestParseStockNewsPayloadStripsJSONPAndHTML(t *testing.T) {
	raw := `astockNews({"result":{"cmsArticleWebOld":[{"date":"2026-08-11 10:20:00","mediaName":"测试媒体","code":"202608111234","title":"贵州<em>茅台</em>发布公告"},{"date":"2026-08-11","mediaName":"测试媒体","code":"2","title":"贵州<em>茅台</em>发布公告"}]}})`
	items := ParseStockNewsPayload(raw)
	if len(items) != 1 || items[0].Title != "贵州茅台发布公告" || items[0].Source != "测试媒体" || items[0].URL == "" {
		t.Fatalf("unexpected news items: %#v", items)
	}
}

func TestStockNewsAddressContainsStructuredSearchParameter(t *testing.T) {
	address := stockNewsAddress("https://example.test/search", "600519", 8)
	for _, value := range []string{"cb=astockNews", "600519", "pageSize%22%3A8"} {
		if !strings.Contains(address, value) {
			t.Fatalf("address missing %q: %s", value, address)
		}
	}
}
