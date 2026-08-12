package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func splitReportLine(value string, maximum int) (string, string) {
	if displayWidth(value) <= maximum {
		return value, ""
	}
	width := 0
	cut := 0
	lastSpace := -1
	for index, character := range value {
		characterWidth := runeDisplayWidth(character)
		if width+characterWidth > maximum {
			break
		}
		width += characterWidth
		cut = index + utf8.RuneLen(character)
		if character == ' ' {
			lastSpace = cut
		}
	}
	if lastSpace > 0 && lastSpace >= cut-12 {
		cut = lastSpace
	}
	if cut <= 0 {
		_, size := utf8.DecodeRuneInString(value)
		cut = size
	}
	return strings.TrimRight(value[:cut], " "), strings.TrimLeft(value[cut:], " ")
}

func wrapReportMarkdown(markdown string, width int) []string {
	if width < 16 {
		width = 16
	}
	input := strings.Split(strings.ReplaceAll(strings.TrimSpace(markdown), "\t", "  "), "\n")
	result := make([]string, 0, len(input))
	for _, line := range input {
		if line == "" {
			result = append(result, "")
			continue
		}
		continuation := ""
		trimmed := strings.TrimLeft(line, " ")
		indent := line[:len(line)-len(trimmed)]
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "> ") {
			continuation = indent + "  "
		} else if len(trimmed) >= 3 && trimmed[0] >= '0' && trimmed[0] <= '9' && strings.Contains(trimmed[:min(len(trimmed), 4)], ". ") {
			continuation = indent + "   "
		}
		remaining := line
		first := true
		for remaining != "" {
			prefix := ""
			if !first {
				prefix = continuation
			}
			available := width - displayWidth(prefix)
			part, rest := splitReportLine(remaining, available)
			result = append(result, prefix+part)
			remaining = rest
			first = false
		}
	}
	return result
}

func BuildMarketReportFrame(report domain.GeneratedMarketReport, controls string, terminalWidth int, moyu bool) string {
	if terminalWidth < 32 {
		terminalWidth = 32
	}
	engine := "CODEX"
	if !report.AIUsed {
		engine = "RULE-BASED FALLBACK"
	}
	title := fmt.Sprintf("ASTOCK MARKET REPORT  %s  %s", report.GeneratedAt.Format("01-02 15:04:05"), engine)
	if !moyu {
		engine = "Codex综合"
		if !report.AIUsed {
			engine = "量化回退版"
		}
		title = fmt.Sprintf("ASTOCK 智能市场报告  ·  %s  ·  %s", report.GeneratedAt.Format("01-02 15:04:05"), engine)
	}
	lines := []string{truncateWidth(title, terminalWidth), truncateWidth(controls, terminalWidth), ""}
	lines = append(lines, wrapReportMarkdown(report.Markdown, terminalWidth)...)
	return strings.Join(lines, "\n")
}
