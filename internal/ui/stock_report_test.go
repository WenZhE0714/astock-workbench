package ui

import (
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestStockReportFrameWrapsAndNamesStock(t *testing.T) {
	report := domain.GeneratedStockReport{
		Symbol: "sh600519", Name: "贵州茅台", AIUsed: true,
		Markdown: "# 一句话结论\n\n- 这是包含技术、资金、板块、舆情、关键点位和风险理由的个股报告。",
	}
	frame := BuildStockReportFrame(report, "↑/↓滚动  [/]翻页  Esc返回", 79, false)
	for _, expected := range []string{"ASTOCK 个股多维研判", "600519", "贵州茅台", "Codex综合", "关键点位"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("frame missing %q:\n%s", expected, frame)
		}
	}
	for _, line := range strings.Split(frame, "\n") {
		if width := displayWidth(line); width > 79 {
			t.Fatalf("line width %d exceeds terminal: %s", width, line)
		}
	}
}
