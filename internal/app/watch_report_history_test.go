package app

import (
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/storage"
)

func TestJoinWatchStatusesKeepsFailedAndRunningTasksVisible(t *testing.T) {
	status := joinWatchStatuses(
		"AI问答失败：超时",
		"策略研究后台运行中：候选 2/30",
		"个股研判生成中：600519 贵州茅台",
	)
	want := "AI问答失败：超时\n策略研究后台运行中：候选 2/30\n个股研判生成中：600519 贵州茅台"
	if status != want {
		t.Fatalf("unexpected aggregated status:\n%s", status)
	}
}

func TestReportHistoryGroupsDatesAndKeepsNewestTimesFirst(t *testing.T) {
	state := watchReportHistory{}
	state.openMarket([]storage.MarketReportIndexEntry{
		{ID: "third", GeneratedAt: time.Date(2026, 8, 11, 15, 31, 0, 0, shanghaiLocation), AIUsed: true},
		{ID: "second", GeneratedAt: time.Date(2026, 8, 11, 10, 1, 0, 0, shanghaiLocation)},
		{ID: "first", GeneratedAt: time.Date(2026, 8, 10, 15, 30, 0, 0, shanghaiLocation)},
	})
	dates := state.dates()
	if len(dates) != 2 || dates[0] != "2026-08-11" || dates[1] != "2026-08-10" {
		t.Fatalf("unexpected dates: %#v", dates)
	}
	if !state.openSelectedDate() {
		t.Fatal("expected newest date to open")
	}
	items := state.reportsForDate(state.selectedDate)
	if len(items) != 2 || items[0].id != "third" || items[1].id != "second" {
		t.Fatalf("unexpected reports for date: %#v", items)
	}
	state.move(1)
	selected, ok := state.selectedReport()
	if !ok || selected.id != "second" {
		t.Fatalf("unexpected selected report: %#v, %v", selected, ok)
	}
	if !state.back() || state.view != reportHistoryDates || state.selected != 0 {
		t.Fatalf("unexpected back state: %#v", state)
	}
}

func TestReportHistoryFrameShowsDateAndStockContext(t *testing.T) {
	state := watchReportHistory{}
	state.openStock("sh600519", "贵州茅台", []storage.StockReportIndexEntry{
		{ID: "one", GeneratedAt: time.Date(2026, 8, 11, 15, 31, 0, 0, shanghaiLocation), AIUsed: true},
	})
	state.openSelectedDate()
	frame := state.frame(79, false, false)
	for _, expected := range []string{"ASTOCK 报告历史", "600519 贵州茅台", "日期  2026-08-11", "15:31:00", "Codex综合"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("history frame missing %q:\n%s", expected, frame)
		}
	}
}
