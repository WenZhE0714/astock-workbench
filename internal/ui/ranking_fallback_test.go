package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
)

func TestRankingFallbackSourceFitsViewportAndKeepsSelection(t *testing.T) {
	items := make([]domain.MarketRankingItem, 20)
	for index := range items {
		items[index] = domain.MarketRankingItem{Symbol: fmt.Sprintf("sz%06d", 300000+index), Name: "测试股票", Percent: float64(20 - index), Source: market.THSMarketRankingSource, Industry: "出版", IndustrySource: "同花顺F10申万行业"}
	}
	for _, width := range []int{60, 79, 120} {
		frame := BuildLiveFrame(LiveData{RankingKind: domain.MarketRankingGainers, RankingItems: items, RankingSelected: 19}, ViewOptions{}, width, 24)
		for _, expected := range []string{market.THSMarketRankingSource, "涨幅榜 TOP 20", "> 20", "出版", "同花顺F10申万行业"} {
			if !strings.Contains(frame, expected) {
				t.Fatalf("missing %q:\n%s", expected, frame)
			}
		}
		if strings.Count(frame, "\n")+1 > 24 {
			t.Fatalf("ranking exceeds terminal height:\n%s", frame)
		}
		for _, line := range strings.Split(frame, "\n") {
			if displayWidth(line) > width {
				t.Fatalf("ranking exceeds width %d: %s", width, line)
			}
		}
	}
}

func TestRankingFailuresRemainVisibleInLiveStatus(t *testing.T) {
	status := "榜单加载失败: 榜单数据源均不可用:\n东方财富: EOF\n同花顺: HTTP 503 Service Unavailable"
	for _, width := range []int{60, 79, 120} {
		frame := BuildLiveFrame(LiveData{Status: status}, ViewOptions{}, width, 24)
		for _, expected := range []string{"东方财富: EOF", "同花顺: HTTP 503 Service Unavailable"} {
			if !strings.Contains(frame, expected) {
				t.Fatalf("missing failure %q at width %d:\n%s", expected, width, frame)
			}
		}
		if strings.Count(frame, "\n")+1 > 24 {
			t.Fatalf("failure status exceeds terminal height:\n%s", frame)
		}
		for _, line := range strings.Split(frame, "\n") {
			if displayWidth(line) > width {
				t.Fatalf("failure status exceeds width %d: %s", width, line)
			}
		}
	}
}
