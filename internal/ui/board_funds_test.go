package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func boardFundDashboardFixture() domain.BoardFundDashboard {
	build := func(prefix string, sign float64) []domain.BoardFundRankingItem {
		items := make([]domain.BoardFundRankingItem, 5)
		for index := range items {
			leaders := make([]domain.MarketStockSnapshot, 3)
			for leader := range leaders {
				leaders[leader] = domain.MarketStockSnapshot{
					Symbol: fmt.Sprintf("sh60%d00%d", index, leader), Name: fmt.Sprintf("核心股票%d%d", index, leader),
					Percent: sign * 2.1, Speed: sign * 0.18, MainNet: sign * 1.2e8,
				}
			}
			items[index] = domain.BoardFundRankingItem{
				Board: domain.BoardFlow{
					Code: fmt.Sprintf("BK%04d", index), Name: fmt.Sprintf("%s行业板块%d", prefix, index+1),
					MainNet: sign * 9e8, Percent: sign * 2.8, RiseCount: 24, FallCount: 3, FlatCount: 1,
				},
				Leaders: leaders,
			}
		}
		return items
	}
	return domain.BoardFundDashboard{
		RefreshedAt: time.Date(2026, 8, 12, 10, 31, 2, 0, time.Local),
		Inflows:     build("流入", 1), Outflows: build("流出", -1),
	}
}

func TestBoardFundDashboardShowsBothDirectionsAndLeaderBasis(t *testing.T) {
	dashboard := boardFundDashboardFixture()
	for _, width := range []int{79, 136} {
		frame := BuildBoardFundDashboardFrame(dashboard, false, "", "y刷新  Esc返回", width, false, false)
		for _, expected := range []string{
			"主力净流入 TOP 5", "主力净流出 TOP 5", "龙头口径：板块内成交额前3",
			"流入行业板块5", "流出行业板块5", "广度", "涨速", "主力",
		} {
			if !strings.Contains(frame, expected) {
				t.Fatalf("width %d missing %q:\n%s", width, expected, frame)
			}
		}
		if count := strings.Count(frame, "核心股票"); count != 30 {
			t.Fatalf("width %d rendered %d leaders, want 30:\n%s", width, count, frame)
		}
		for _, line := range strings.Split(frame, "\n") {
			if lineWidth := displayWidth(line); lineWidth > width {
				t.Fatalf("line width %d exceeds %d:\n%s", lineWidth, width, line)
			}
		}
	}
}

func TestBoardFundDashboardLoadingExplainsQuotesContinue(t *testing.T) {
	frame := BuildBoardFundDashboardFrame(domain.BoardFundDashboard{}, true, "正在加载板块资金看板，行情继续刷新", "Esc返回", 79, false, false)
	for _, expected := range []string{"正在加载", "行情继续", "Esc返回"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("loading frame missing %q:\n%s", expected, frame)
		}
	}
}

func TestBoardFundDashboardUsesDirectionalColorBands(t *testing.T) {
	frame := BuildBoardFundDashboardFrame(boardFundDashboardFixture(), false, "", "", 136, false, true)
	if !strings.Contains(frame, "\x1b[1;31m主力净流入 TOP 5\x1b[0m") {
		t.Fatalf("inflow heading should use the positive color:\n%q", frame)
	}
	if !strings.Contains(frame, "\x1b[1;32m主力净流出 TOP 5\x1b[0m") {
		t.Fatalf("outflow heading should use the negative color:\n%q", frame)
	}
	if strings.Contains(BuildBoardFundDashboardFrame(boardFundDashboardFixture(), false, "", "", 136, false, false), "\x1b[") {
		t.Fatal("color disabled frame should not contain ANSI escapes")
	}
}
