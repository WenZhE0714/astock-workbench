package ui

import (
	"fmt"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func boardFundValue(value string, delta float64, color bool) string {
	return style(value, trendCode(delta, false), color)
}

func boardFundLabel(value string, width int) string {
	return padWidth(value, width, "left")
}

func boardFundLeaderLine(rank int, leader domain.MarketStockSnapshot, width int, color bool) string {
	code := leader.Symbol
	if len(code) == 8 {
		code = code[2:]
	}
	nameWidth := 12
	if width < 60 {
		nameWidth = 8
	}
	name := padWidth(truncateWidth(leader.Name, nameWidth), nameWidth, "left")
	return fmt.Sprintf(
		"  %d %s %s %s %s %s",
		rank,
		padWidth(code, 6, "left"),
		name,
		boardFundValue(padWidth(signedPercent(leader.Percent), 8, "right"), leader.Percent, color),
		boardFundValue(padWidth(signedPercent(leader.Speed), 8, "right"), leader.Speed, color),
		boardFundValue(padWidth(humanFundMovementAmount(leader.MainNet), 10, "right"), leader.MainNet, color),
	)
}

func boardFundSectionLines(title string, items []domain.BoardFundRankingItem, width int, color bool, positive bool, moyu bool) []string {
	if width < 32 {
		width = 32
	}
	titleCode := "1;31"
	if !positive {
		titleCode = "1;32"
	}
	if moyu {
		if positive {
			title = "MAIN NET INFLOW TOP 5"
		} else {
			title = "MAIN NET OUTFLOW TOP 5"
		}
	}
	lines := []string{style(title, titleCode, color && !moyu)}
	if len(items) == 0 {
		return append(lines, style("暂无数据", "33", color && !moyu))
	}
	// Keep the board summary and constituent columns visually distinct.
	lines = append(lines, style("    板块                    资金      涨幅    广度", "90", color && !moyu))
	lines = append(lines, style("      代码   名称          涨跌      涨速      主力净额", "90", color && !moyu))
	for index, item := range items {
		board := item.Board
		nameWidth := 18
		if width < 70 {
			nameWidth = 16
		}
		boardName := padWidth(truncateWidth(board.Name, nameWidth), nameWidth, "left")
		breadth := fmt.Sprintf("%d/%d/%d", board.RiseCount, board.FallCount, board.FlatCount)
		boardStyle := "1;31"
		if !positive {
			boardStyle = "1;32"
		}
		boardNameValue := style(boardName, boardStyle, color && !moyu)
		boardAmount := boardFundValue(padWidth(humanFundMovementAmount(board.MainNet), 10, "right"), board.MainNet, color && !moyu)
		boardPercent := boardFundValue(padWidth(signedPercent(board.Percent), 8, "right"), board.Percent, color && !moyu)
		boardLine := ""
		if width < 70 {
			boardLine = fmt.Sprintf("%02d %s  %s  %s  %s", index+1, boardNameValue, boardAmount, boardPercent, breadth)
		} else {
			boardLine = fmt.Sprintf(
				"%02d %s  %s %s  %s %s  %s %s",
				index+1, boardNameValue,
				style(boardFundLabel("资金", 4), "90", color && !moyu), boardAmount,
				style(boardFundLabel("涨幅", 4), "90", color && !moyu), boardPercent,
				style(boardFundLabel("广度", 4), "90", color && !moyu), style(breadth, "90", color && !moyu),
			)
		}
		lines = append(lines, boardLine)
		if len(item.Leaders) == 0 {
			lines = append(lines, style("   龙头数据暂不可用", "33", color && !moyu))
			continue
		}
		for leaderIndex, leader := range item.Leaders {
			lines = append(lines, boardFundLeaderLine(leaderIndex+1, leader, width, color && !moyu))
		}
	}
	return lines
}

func joinBoardFundColumns(left, right []string, width int) []string {
	rows := len(left)
	if len(right) > rows {
		rows = len(right)
	}
	result := make([]string, rows)
	for index := 0; index < rows; index++ {
		leftLine, rightLine := "", ""
		if index < len(left) {
			leftLine = left[index]
		}
		if index < len(right) {
			rightLine = right[index]
		}
		result[index] = padWidth(truncateWidth(leftLine, width), width, "left") + "  " + truncateWidth(rightLine, width)
	}
	return result
}

func BuildBoardFundDashboardFrame(
	dashboard domain.BoardFundDashboard,
	loading bool,
	status, controls string,
	terminalWidth int,
	moyu, color bool,
) string {
	if terminalWidth < 32 {
		terminalWidth = 32
	}
	refreshed := "--:--:--"
	if !dashboard.RefreshedAt.IsZero() {
		refreshed = dashboard.RefreshedAt.Format("15:04:05")
	}
	title := "ASTOCK BOARD FUND DASHBOARD  " + refreshed
	scope := "RANK: MAIN NET  ·  LEADERS: TOP 3 BY TURNOVER"
	if !moyu {
		title = "ASTOCK 板块资金看板  ·  " + refreshed
		scope = "板块口径：行业主力净额排名  ·  龙头口径：板块内成交额前3"
	}
	lines := []string{
		style(truncateWidth(title, terminalWidth), "1;36", color && !moyu),
		style(truncateWidth(controls, terminalWidth), "90", color && !moyu),
		"",
		style(truncateWidth(scope, terminalWidth), "90", color && !moyu),
	}
	if loading && dashboard.RefreshedAt.IsZero() {
		message := "LOADING; QUOTES KEEP REFRESHING"
		if !moyu {
			message = "正在加载板块与成交额龙头，行情继续在后台刷新…"
		}
		lines = append(lines, "", style(truncateWidth(message, terminalWidth), "33", color && !moyu))
	} else {
		lines = append(lines, "")
		if terminalWidth >= 112 {
			columnWidth := (terminalWidth - 2) / 2
			left := boardFundSectionLines("主力净流入 TOP 5", dashboard.Inflows, columnWidth, color, true, moyu)
			right := boardFundSectionLines("主力净流出 TOP 5", dashboard.Outflows, columnWidth, color, false, moyu)
			lines = append(lines, joinBoardFundColumns(left, right, columnWidth)...)
		} else {
			lines = append(lines, boardFundSectionLines("主力净流入 TOP 5", dashboard.Inflows, terminalWidth, color, true, moyu)...)
			lines = append(lines, "")
			lines = append(lines, boardFundSectionLines("主力净流出 TOP 5", dashboard.Outflows, terminalWidth, color, false, moyu)...)
		}
	}
	if status != "" {
		lines = append(lines, "")
		for _, line := range wrapReportMarkdown(status, terminalWidth) {
			lines = append(lines, style(line, "33", color && !moyu))
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
