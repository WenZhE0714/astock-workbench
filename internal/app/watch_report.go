package app

import (
	"fmt"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type watchMarketReport struct {
	generating bool
	viewing    bool
	unread     bool
	progress   string
	error      string
	report     domain.GeneratedMarketReport
}

func (state *watchMarketReport) begin() {
	state.generating = true
	state.unread = false
	state.progress = "采集市场数据"
	state.error = ""
}

func (state *watchMarketReport) complete(report domain.GeneratedMarketReport) {
	state.generating = false
	state.unread = true
	state.progress = ""
	state.error = ""
	state.report = report
}

func (state *watchMarketReport) fail(err error) {
	state.generating = false
	state.progress = ""
	if err != nil {
		state.error = err.Error()
	}
}

func (state *watchMarketReport) open(report domain.GeneratedMarketReport) {
	state.report = report
	state.viewing = true
	state.unread = false
	state.error = ""
}

func (state *watchMarketReport) close() {
	state.viewing = false
}

func (state watchMarketReport) status(moyu bool) string {
	if state.generating {
		progress := strings.TrimSpace(state.progress)
		if progress == "" {
			progress = "处理中"
		}
		if moyu {
			return "AI MARKET REPORT: " + strings.ToUpper(progress) + " | LIVE QUOTES CONTINUE"
		}
		return "智能报告生成中：" + progress + " · 行情继续刷新"
	}
	if state.unread {
		if moyu {
			return "AI MARKET REPORT READY | PRESS R TO OPEN"
		}
		if state.report.AIUsed {
			return "智能报告已生成，按 r 查看"
		}
		return "量化报告已生成（Codex综合未完成），按 r 查看"
	}
	if state.error != "" {
		if moyu {
			return "AI MARKET REPORT FAILED: " + state.error
		}
		return fmt.Sprintf("智能报告生成失败：%s；按 s 重试", state.error)
	}
	return ""
}

func marketReportViewControls(moyu bool) string {
	if moyu {
		return "UP/DOWN SCROLL  [/] PAGE  G/G ENDPOINTS  ESC BACK  Q QUIT"
	}
	return "↑/↓滚动  [/]翻页  g/G首尾  Esc返回  q退出"
}
