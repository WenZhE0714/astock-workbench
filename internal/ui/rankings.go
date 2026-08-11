package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func marketRankingTitle(kind domain.MarketRankingKind, count int, refreshedAt time.Time, moyu bool) string {
	result := ""
	if moyu {
		switch kind {
		case domain.MarketRankingLosers:
			result = fmt.Sprintf("TOP LOSERS  %d", count)
		case domain.MarketRankingRapidRise:
			result = fmt.Sprintf("RAPID RISE  TOP %d", count)
		default:
			result = fmt.Sprintf("TOP GAINERS  %d", count)
		}
		if !refreshedAt.IsZero() {
			result += "  UPDATE " + refreshedAt.Format("15:04:05")
		}
		return result
	}
	switch kind {
	case domain.MarketRankingLosers:
		result = fmt.Sprintf("跌幅榜 TOP %d", count)
	case domain.MarketRankingRapidRise:
		result = fmt.Sprintf("快速涨幅榜 TOP %d", count)
	default:
		result = fmt.Sprintf("涨幅榜 TOP %d", count)
	}
	if !refreshedAt.IsZero() {
		result += "  ·  更新 " + refreshedAt.Format("15:04:05")
	}
	return result
}

func marketRankingNumber(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	return fmt.Sprintf("%.2f", value)
}

func buildMarketRankingTable(items []domain.MarketRankingItem, selected, offset int, kind domain.MarketRankingKind, terminalWidth int, moyu, color bool) string {
	showRank := terminalWidth >= 50
	showBothMetrics := terminalWidth >= 72
	showPrice := terminalWidth >= 96

	header := make([]string, 0, 7)
	columnKinds := make([]string, 0, 7)
	if showRank {
		header = append(header, "排名")
		columnKinds = append(columnKinds, "rank")
	}
	if moyu {
		header = append(header, "CODE", "NAME")
	} else {
		header = append(header, "代码", "名称")
	}
	columnKinds = append(columnKinds, "code", "name")
	if showPrice {
		if moyu {
			header = append(header, "PRICE")
		} else {
			header = append(header, "最新")
		}
		columnKinds = append(columnKinds, "price")
	}
	if showBothMetrics {
		if moyu {
			header = append(header, "CHANGE", "SPEED")
		} else {
			header = append(header, "涨幅", "涨速")
		}
		columnKinds = append(columnKinds, "percent", "speed")
	} else if kind == domain.MarketRankingRapidRise {
		if moyu {
			header = append(header, "SPEED")
		} else {
			header = append(header, "涨速")
		}
		columnKinds = append(columnKinds, "speed")
	} else {
		if moyu {
			header = append(header, "CHANGE")
		} else {
			header = append(header, "涨幅")
		}
		columnKinds = append(columnKinds, "percent")
	}
	if moyu {
		header = append(header, "INDUSTRY")
	} else {
		header = append(header, "行业板块")
	}
	columnKinds = append(columnKinds, "industry")

	rows := make([][]string, 0, len(items))
	rowCodes := make([][]string, 0, len(items))
	for index, item := range items {
		row := make([]string, 0, len(header))
		codes := make([]string, 0, len(header))
		marker := "  "
		if index == selected {
			marker = "> "
		}
		for _, column := range columnKinds {
			switch column {
			case "rank":
				row = append(row, fmt.Sprintf("%s%02d", marker, offset+index+1))
				codes = append(codes, "")
			case "code":
				code := item.Symbol
				if len(code) == 8 {
					code = code[2:]
				}
				row = append(row, code)
				codes = append(codes, "")
			case "name":
				name := item.Name
				if !showRank {
					name = marker + name
				}
				row = append(row, name)
				codes = append(codes, "")
			case "price":
				row = append(row, marketRankingNumber(item.Price))
				codes = append(codes, trendCode(item.Percent, false))
			case "percent":
				row = append(row, signedPercent(item.Percent))
				codes = append(codes, trendCode(item.Percent, false))
			case "speed":
				row = append(row, signedPercent(item.Speed))
				codes = append(codes, trendCode(item.Speed, false))
			case "industry":
				row = append(row, item.Industry)
				codes = append(codes, "")
			}
		}
		rows = append(rows, row)
		rowCodes = append(rowCodes, codes)
	}

	widths := make([]int, len(header))
	nameColumn, industryColumn := -1, -1
	for column, title := range header {
		widths[column] = displayWidth(title)
		for _, row := range rows {
			widths[column] = maxInt(widths[column], displayWidth(row[column]))
		}
		switch columnKinds[column] {
		case "rank":
			widths[column] = min(widths[column], 4)
		case "code":
			widths[column] = 6
		case "name":
			nameColumn = column
			widths[column] = min(widths[column], 12)
		case "industry":
			industryColumn = column
			widths[column] = min(widths[column], 18)
		case "price":
			widths[column] = min(widths[column], 10)
		default:
			widths[column] = min(widths[column], 9)
		}
	}
	totalWidth := func() int {
		result := 1 + len(widths)*3
		for _, width := range widths {
			result += width
		}
		return result
	}
	shrink := func(column int) bool {
		if column < 0 || widths[column] <= displayWidth(header[column]) {
			return false
		}
		widths[column]--
		return true
	}
	for totalWidth() > terminalWidth {
		changed := shrink(nameColumn)
		if totalWidth() > terminalWidth {
			changed = shrink(industryColumn) || changed
		}
		for column := len(widths) - 1; column >= 0 && totalWidth() > terminalWidth; column-- {
			if column == nameColumn || column == industryColumn {
				continue
			}
			changed = shrink(column) || changed
		}
		if !changed {
			break
		}
	}
	for row := range rows {
		for column := range rows[row] {
			rows[row][column] = truncateWidth(rows[row][column], widths[column])
		}
	}

	var builder strings.Builder
	border := "+"
	for _, width := range widths {
		border += strings.Repeat("-", width+2) + "+"
	}
	renderRow := func(row, codes []string, heading bool) {
		builder.WriteByte('|')
		for column, value := range row {
			if heading && color && !moyu {
				value = style(value, "1;36", true)
			} else if !heading && color && !moyu && codes[column] != "" {
				value = style(value, codes[column], true)
			}
			alignment := "right"
			if heading || columnKinds[column] == "rank" || columnKinds[column] == "code" {
				alignment = "center"
			} else if columnKinds[column] == "name" || columnKinds[column] == "industry" {
				alignment = "left"
			}
			builder.WriteByte(' ')
			builder.WriteString(padWidth(value, widths[column], alignment))
			builder.WriteString(" |")
		}
		builder.WriteByte('\n')
	}
	builder.WriteString(border + "\n")
	renderRow(header, nil, true)
	builder.WriteString(border + "\n")
	for index, row := range rows {
		renderRow(row, rowCodes[index], false)
	}
	builder.WriteString(border)
	return builder.String()
}
