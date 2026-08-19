package ui

import "strings"

type ReportHistoryFrame struct {
	Context  string
	Date     string
	Controls string
	Items    []string
	Selected int
	Error    string
}

func BuildReportHistoryFrame(data ReportHistoryFrame, terminalWidth int, moyu, color bool) string {
	if terminalWidth < 32 {
		terminalWidth = 32
	}
	useColor := color && !moyu
	title := "ASTOCK REPORT HISTORY"
	if !moyu {
		title = "ASTOCK 报告历史"
	}
	if data.Context != "" {
		title += "  ·  " + data.Context
	}
	lines := []string{
		style(truncateWidth(title, terminalWidth), "1;36", useColor),
		style(truncateWidth(data.Controls, terminalWidth), "90", useColor),
		style(strings.Repeat("─", min(terminalWidth, 72)), "90", useColor),
		"",
	}
	if data.Date != "" {
		label := "DATE  " + data.Date
		if !moyu {
			label = "日期  " + data.Date
		}
		lines = append(lines, style(truncateWidth(label, terminalWidth), "1;37", useColor), "")
	}
	if data.Error != "" {
		lines = append(lines, style(truncateWidth(data.Error, terminalWidth), "1;31", useColor), "")
	}
	if len(data.Items) == 0 {
		empty := "NO REPORT HISTORY"
		if !moyu {
			empty = "暂无报告历史"
		}
		lines = append(lines, style(empty, "90", useColor))
	}
	for index, item := range data.Items {
		marker := "  "
		code := "37"
		if index == data.Selected {
			marker = "> "
			code = "1;30;46"
		}
		lines = append(lines, style(truncateWidth(marker+item, terminalWidth), code, useColor))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
