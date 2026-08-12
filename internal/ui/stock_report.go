package ui

import (
	"fmt"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func BuildStockReportFrame(report domain.GeneratedStockReport, controls string, terminalWidth int, moyu bool) string {
	if terminalWidth < 32 {
		terminalWidth = 32
	}
	engine := "CODEX"
	if !report.AIUsed {
		engine = "RULE-BASED FALLBACK"
	}
	code := report.Symbol
	if len(code) == 8 {
		code = code[2:]
	}
	title := fmt.Sprintf("ASTOCK STOCK REPORT  %s %s  %s", code, report.Name, engine)
	if !moyu {
		engine = "Codex综合"
		if !report.AIUsed {
			engine = "量化回退版"
		}
		title = fmt.Sprintf("ASTOCK 个股多维研判  ·  %s %s  ·  %s", code, report.Name, engine)
	}
	lines := []string{truncateWidth(title, terminalWidth), truncateWidth(controls, terminalWidth), ""}
	lines = append(lines, wrapReportMarkdown(report.Markdown, terminalWidth)...)
	return strings.Join(lines, "\n")
}
