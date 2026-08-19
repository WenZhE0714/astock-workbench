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
	} else if len(report.Agents) > 0 {
		engine = fmt.Sprintf("MULTI-AGENT %d/%d", successfulAgentRuns(report.Agents), len(report.Agents))
	}
	code := report.Symbol
	if len(code) == 8 {
		code = code[2:]
	}
	title := fmt.Sprintf("ASTOCK STOCK REPORT  %s %s  %s", code, report.Name, engine)
	if !moyu {
		engine = "Codex综合"
		if report.AIUsed && len(report.Agents) > 0 {
			engine = fmt.Sprintf("多Agent综合 %d/%d", successfulAgentRuns(report.Agents), len(report.Agents))
		}
		if !report.AIUsed {
			engine = "量化回退版"
		}
		title = fmt.Sprintf("ASTOCK 个股多维研判  ·  %s %s  ·  %s", code, report.Name, engine)
	}
	lines := []string{truncateWidth(title, terminalWidth), truncateWidth(controls, terminalWidth), ""}
	lines = append(lines, wrapReportMarkdown(report.Markdown, terminalWidth)...)
	return strings.Join(lines, "\n")
}

func successfulAgentRuns(runs []domain.AgentResearchRun) int {
	count := 0
	for _, run := range runs {
		if run.Status == "ok" {
			count++
		}
	}
	return count
}
