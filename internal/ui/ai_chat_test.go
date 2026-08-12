package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestAIChatFrameWrapsConversationWithinTerminal(t *testing.T) {
	frame := BuildAIChatFrame("sh600519", "贵州茅台", []domain.AIChatTurn{{
		AskedAt:  time.Date(2026, 8, 12, 10, 30, 0, 0, time.Local),
		Question: "现在是否适合买入？",
		Answer:   "当前只能等待条件确认，因为价格位置、量能和资金承接需要同时观察，不能仅凭单一涨幅作出决定。",
	}}, "x继续追问  Esc返回", 79, false)
	for _, expected := range []string{"ASTOCK AI 实时咨询", "600519 贵州茅台", "现在是否适合买入", "等待条件确认", "x继续追问"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("AI chat frame missing %q:\n%s", expected, frame)
		}
	}
	for _, line := range strings.Split(frame, "\n") {
		if width := displayWidth(line); width > 79 {
			t.Fatalf("AI chat line width %d exceeds terminal:\n%s", width, line)
		}
	}
}
