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
	for _, expected := range []string{"ASTOCK AI 咨询历史", "600519 贵州茅台", "现在是否适合买入", "等待条件确认", "x继续追问"} {
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

func TestAIChatFrameShowsNewestTurnFirst(t *testing.T) {
	turns := []domain.AIChatTurn{
		{AskedAt: time.Date(2026, 8, 13, 10, 0, 0, 0, time.Local), Question: "第一问", Answer: "第一答"},
		{AskedAt: time.Date(2026, 8, 13, 10, 5, 0, 0, time.Local), Question: "第二问", Answer: "第二答"},
	}
	frame := BuildAIChatFrame("sh600519", "贵州茅台", turns, "Esc返回", 79, false)
	second := strings.Index(frame, "第二问")
	first := strings.Index(frame, "第一问")
	if second < 0 || first < 0 || second >= first {
		t.Fatalf("newest turn should be rendered first:\n%s", frame)
	}
	if turns[0].Question != "第一问" || turns[1].Question != "第二问" {
		t.Fatalf("rendering mutated stored chronological history: %#v", turns)
	}
}
