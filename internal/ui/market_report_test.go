package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestMarketReportFrameWrapsLongMarkdownWithinTerminal(t *testing.T) {
	report := domain.GeneratedMarketReport{
		GeneratedAt: time.Date(2026, 8, 11, 15, 30, 0, 0, time.Local), AIUsed: true,
		Markdown: "# 一句话结论\n\n- 这是一段很长的中文市场分析内容，用来确认报告页面会完整换行而不是截断后丢失重要的风险和观察条件。",
	}
	frame := BuildMarketReportFrame(report, "↑/↓滚动  [/]翻页  Esc返回", 79, false)
	for _, expected := range []string{"ASTOCK 智能市场报告", "Codex综合", "一句话结论", "风险和观察条件", "Esc返回"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("report frame missing %q:\n%s", expected, frame)
		}
	}
	for _, line := range strings.Split(frame, "\n") {
		if width := displayWidth(line); width > 79 {
			t.Fatalf("report line width %d exceeds terminal:\n%s", width, line)
		}
	}
}
