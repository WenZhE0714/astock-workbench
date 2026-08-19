package ui

import "strings"

type StrategyLabFrame struct {
	Context    string
	Status     string
	StatusTone string
	Controls   string
	Items      []string
	Selected   int
	Body       string
}

func strategyLabStatusCode(tone string) string {
	switch tone {
	case "running", "warning":
		return "1;33"
	case "error":
		return "1;31"
	case "success":
		return "1;36"
	default:
		return "36"
	}
}

func strategyLabBodyCode(line string) string {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "## 买入"):
		return "1;31"
	case strings.HasPrefix(trimmed, "## 卖出"):
		return "1;32"
	case strings.HasPrefix(trimmed, "## 样本外"), strings.HasPrefix(trimmed, "样本外"):
		return "1;35"
	case strings.HasPrefix(trimmed, "## AI"), strings.HasPrefix(trimmed, "入选"):
		return "1;36"
	case strings.HasPrefix(trimmed, "## "):
		return "1;37"
	case strings.Contains(trimmed, "拒绝："), strings.Contains(trimmed, "失败："):
		return "31"
	case strings.HasPrefix(trimmed, "报告 "):
		return "90"
	default:
		return ""
	}
}

func appendStrategyLabText(lines []string, text string, width int, code string, color bool) []string {
	for _, line := range wrapReportMarkdown(text, width) {
		lineCode := code
		if lineCode == "body" {
			lineCode = strategyLabBodyCode(line)
		}
		lines = append(lines, style(line, lineCode, color && lineCode != ""))
	}
	return lines
}

func BuildStrategyLabFrame(data StrategyLabFrame, terminalWidth int, moyu, color bool) string {
	if terminalWidth < 32 {
		terminalWidth = 32
	}
	useColor := color && !moyu
	title := "ASTOCK STRATEGY LAB"
	if !moyu {
		title = "ASTOCK 策略研究中心"
	}
	lines := []string{
		style(truncateWidth(title, terminalWidth), "1;36", useColor),
		style(truncateWidth(data.Controls, terminalWidth), "90", useColor),
		style(strings.Repeat("─", min(terminalWidth, 72)), "90", useColor),
		"",
	}
	if strings.TrimSpace(data.Context) != "" {
		contextLines := strings.Split(data.Context, "\n")
		codes := []string{"1;37", "36", "33"}
		for index, line := range contextLines {
			code := codes[min(index, len(codes)-1)]
			lines = appendStrategyLabText(lines, line, terminalWidth, code, useColor)
		}
		lines = append(lines, "")
	}
	if strings.TrimSpace(data.Status) != "" {
		lines = appendStrategyLabText(lines, data.Status, terminalWidth, strategyLabStatusCode(data.StatusTone), useColor)
		lines = append(lines, "")
	}
	if strings.TrimSpace(data.Body) != "" {
		lines = appendStrategyLabText(lines, data.Body, terminalWidth, "body", useColor)
		if len(data.Items) > 0 {
			lines = append(lines, "")
		}
	}
	for index, item := range data.Items {
		marker := "  "
		if index == data.Selected {
			marker = "> "
		}
		for _, line := range wrapReportMarkdown(marker+item, terminalWidth) {
			code := "37"
			if index == data.Selected {
				code = "1;30;46"
			}
			lines = append(lines, style(line, code, useColor))
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
