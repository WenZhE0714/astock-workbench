package app

import (
	"fmt"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type watchStockReport struct {
	generating bool
	viewing    bool
	unread     bool
	symbol     string
	name       string
	progress   string
	error      string
	report     domain.GeneratedStockReport
}

func (state *watchStockReport) begin(symbol, name string) {
	state.generating = true
	state.unread = false
	state.symbol = symbol
	state.name = name
	state.progress = "采集个股多维数据"
	state.error = ""
}

func (state *watchStockReport) complete(report domain.GeneratedStockReport) {
	state.generating = false
	state.unread = true
	state.progress = ""
	state.error = ""
	state.symbol = report.Symbol
	state.name = report.Name
	state.report = report
}

func (state *watchStockReport) fail(err error) {
	state.generating = false
	state.progress = ""
	if err != nil {
		state.error = err.Error()
	}
}

func (state *watchStockReport) open(report domain.GeneratedStockReport) {
	state.report = report
	state.symbol = report.Symbol
	state.name = report.Name
	state.viewing = true
	state.unread = false
	state.error = ""
}

func (state *watchStockReport) close() {
	state.viewing = false
}

func stockReportLabel(symbol, name string) string {
	if len(symbol) == 8 {
		symbol = symbol[2:]
	}
	if name != "" {
		return symbol + " " + name
	}
	return symbol
}

func (state watchStockReport) status(moyu bool) string {
	label := stockReportLabel(state.symbol, state.name)
	if state.generating {
		progress := strings.TrimSpace(state.progress)
		if progress == "" {
			progress = "处理中"
		}
		if moyu {
			return "AI STOCK REPORT " + label + ": " + strings.ToUpper(progress) + " | LIVE QUOTES CONTINUE"
		}
		return "个股研判生成中：" + label + " · " + progress + " · 行情继续刷新"
	}
	if state.unread {
		if moyu {
			return "AI STOCK REPORT READY " + label + " | PRESS O TO OPEN"
		}
		if state.report.AIUsed {
			return fmt.Sprintf("%s 多Agent个股研判已生成（%d/%d），按 o 查看", label, successfulAgentCount(state.report.Agents), len(state.report.Agents))
		}
		return label + " 量化研判已生成（Codex综合未完成），按 o 查看"
	}
	if state.error != "" {
		if moyu {
			return "AI STOCK REPORT FAILED " + label + ": " + state.error
		}
		return fmt.Sprintf("%s 个股研判失败：%s；按 c 重试", label, state.error)
	}
	return ""
}
