package ui

import (
	"strings"
	"testing"
)

func TestBuildReportHistoryFrameUsesSelectionColorAndRespectsWidth(t *testing.T) {
	data := ReportHistoryFrame{
		Context: "600519 贵州茅台", Date: "2026-08-11",
		Controls: "↑/↓选择  Enter打开  Esc返回",
		Items:    []string{"15:31:00  Codex综合", "10:01:00  量化回退"}, Selected: 1,
	}
	colored := BuildReportHistoryFrame(data, 79, false, true)
	if !strings.Contains(colored, "\x1b[1;30;46m> 10:01:00") {
		t.Fatalf("selected history item is not highlighted:\n%q", colored)
	}
	plain := BuildReportHistoryFrame(data, 40, false, false)
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain history contains ANSI: %q", plain)
	}
	for _, line := range strings.Split(plain, "\n") {
		if width := displayWidth(line); width > 40 {
			t.Fatalf("history line width %d exceeds terminal: %s", width, line)
		}
	}
	moyu := BuildReportHistoryFrame(data, 79, true, true)
	if strings.Contains(moyu, "\x1b[") {
		t.Fatalf("moyu history contains ANSI: %q", moyu)
	}
}
