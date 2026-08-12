package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func fundMonitorTitle(source string, count int, refreshedAt, industryRefreshedAt time.Time, moyu bool) string {
	sampleTime := "--:--:--"
	if !refreshedAt.IsZero() {
		sampleTime = refreshedAt.Format("15:04:05")
	}
	industryTime := "--:--:--"
	if !industryRefreshedAt.IsZero() {
		industryTime = industryRefreshedAt.Format("15:04:05")
	}
	if moyu {
		return fmt.Sprintf("FUND RADAR  %s  %d STOCKS  1M↓  SAMPLE %s  INDUSTRY %s", source, count, sampleTime, industryTime)
	}
	return fmt.Sprintf("资金雷达  ·  %s  ·  %d只  ·  1分钟↓  ·  样本 %s  ·  行业 %s", source, count, sampleTime, industryTime)
}

func humanFundMovementAmount(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	return humanFundFlow(&domain.FundFlow{MainNet: value})
}

func fundMonitorIndustry(item domain.FundMovement) string {
	direction := ""
	switch {
	case !math.IsNaN(item.IndustryNet) && item.IndustryNet > 0:
		direction = "↑"
	case !math.IsNaN(item.IndustryNet) && item.IndustryNet < 0:
		direction = "↓"
	case !math.IsNaN(item.IndustryPercent) && item.IndustryPercent > 0:
		direction = "↑"
	case !math.IsNaN(item.IndustryPercent) && item.IndustryPercent < 0:
		direction = "↓"
	case !math.IsNaN(item.IndustryNet) || !math.IsNaN(item.IndustryPercent):
		direction = "→"
	}
	name := item.Industry
	if name == "" || name == "--" {
		name = "未分类"
	}
	if direction == "" {
		return name
	}
	return direction + " " + name
}

func compactFundMovementState(value string, moyu bool) string {
	if moyu {
		switch value {
		case "采样中":
			return "SAMPLE"
		case "流出转回流":
			return "REVERSAL IN"
		case "流入转流出":
			return "REVERSAL OUT"
		case "个股板块共振":
			return "IN RESONANCE"
		case "板块共振流出":
			return "OUT RESONANCE"
		case "价涨资出":
			return "PRICE/FLOW DIV"
		case "流入未涨":
			return "INFLOW LAG"
		case "加速流入":
			return "IN ACCEL"
		case "加速流出":
			return "OUT ACCEL"
		case "持续流入":
			return "IN CONTINUE"
		case "持续流出":
			return "OUT CONTINUE"
		case "资金回流":
			return "INFLOW"
		case "资金流出":
			return "OUTFLOW"
		default:
			return "UNCLEAR"
		}
	}
	switch value {
	case "流出转回流":
		return "回流反转"
	case "流入转流出":
		return "流出反转"
	case "个股板块共振":
		return "共振流入"
	case "板块共振流出":
		return "共振流出"
	default:
		return value
	}
}

func fundMovementStateCode(value string) string {
	if value == "流出转回流" {
		return trendCode(1, false)
	}
	if value == "流入转流出" {
		return trendCode(-1, false)
	}
	if strings.Contains(value, "流出") || strings.Contains(value, "资出") {
		return trendCode(-1, false)
	}
	if strings.Contains(value, "流入") || strings.Contains(value, "回流") || strings.Contains(value, "共振") {
		return trendCode(1, false)
	}
	return ""
}

func fundMovementTrendCode(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return ""
	}
	return trendCode(value, false)
}

func buildFundMonitorTable(items []domain.FundMovement, selected, terminalWidth int, moyu, color bool) string {
	kinds := []string{"code", "name", "main"}
	if terminalWidth >= 48 {
		kinds = append(kinds, "delta1")
	}
	if terminalWidth >= 60 {
		kinds = append(kinds, "state")
	}
	if terminalWidth >= 68 {
		kinds = append(kinds, "industry")
	}
	if terminalWidth >= 92 {
		kinds = append(kinds, "industry_net")
	}
	if terminalWidth >= 76 {
		kinds = append(kinds, "percent")
	}
	if terminalWidth >= 92 {
		kinds = append(kinds, "delta3")
	}
	if terminalWidth >= 104 {
		kinds = append(kinds, "delta5")
	}
	if terminalWidth >= 116 {
		kinds = append(kinds, "ratio")
	}
	if terminalWidth >= 130 {
		kinds = append(kinds, "price")
	}

	order := []string{"code", "name", "industry", "industry_net", "price", "percent", "main", "ratio", "delta1", "delta3", "delta5", "state"}
	present := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		present[kind] = true
	}
	kinds = kinds[:0]
	for _, kind := range order {
		if present[kind] {
			kinds = append(kinds, kind)
		}
	}

	title := func(kind string) string {
		if moyu {
			switch kind {
			case "code":
				return "CODE"
			case "name":
				return "NAME"
			case "industry":
				return "INDUSTRY"
			case "industry_net":
				return "BOARD NET"
			case "price":
				return "PRICE"
			case "percent":
				return "CHANGE"
			case "main":
				return "MAIN NET"
			case "ratio":
				return "RATIO"
			case "delta1":
				return "1M"
			case "delta3":
				return "3M"
			case "delta5":
				return "5M"
			default:
				return "STATE"
			}
		}
		switch kind {
		case "code":
			return "代码"
		case "name":
			return "名称"
		case "industry":
			return "行业/板块"
		case "industry_net":
			return "板块资金"
		case "price":
			return "最新"
		case "percent":
			return "涨幅"
		case "main":
			return "主力净额"
		case "ratio":
			return "净占比"
		case "delta1":
			return "1分钟"
		case "delta3":
			return "3分钟"
		case "delta5":
			return "5分钟"
		default:
			return "状态"
		}
	}

	header := make([]string, len(kinds))
	for index, kind := range kinds {
		header[index] = title(kind)
	}
	rows := make([][]string, 0, len(items))
	rowCodes := make([][]string, 0, len(items))
	for index, item := range items {
		row := make([]string, 0, len(kinds))
		codes := make([]string, 0, len(kinds))
		for _, kind := range kinds {
			switch kind {
			case "code":
				marker := "  "
				if index == selected {
					marker = "> "
				}
				code := item.Symbol
				if len(code) == 8 {
					code = code[2:]
				}
				row = append(row, marker+code)
				codes = append(codes, "")
			case "name":
				row = append(row, item.Name)
				codes = append(codes, "")
			case "industry":
				row = append(row, fundMonitorIndustry(item))
				value := item.IndustryNet
				if math.IsNaN(value) {
					value = item.IndustryPercent
				}
				codes = append(codes, fundMovementTrendCode(value))
			case "industry_net":
				row = append(row, humanFundMovementAmount(item.IndustryNet))
				codes = append(codes, fundMovementTrendCode(item.IndustryNet))
			case "price":
				row = append(row, marketRankingNumber(item.Price))
				codes = append(codes, fundMovementTrendCode(item.Percent))
			case "percent":
				row = append(row, signedPercent(item.Percent))
				codes = append(codes, fundMovementTrendCode(item.Percent))
			case "main":
				row = append(row, humanFundMovementAmount(item.MainNet))
				codes = append(codes, fundMovementTrendCode(item.MainNet))
			case "ratio":
				row = append(row, signedPercent(item.MainRatio))
				codes = append(codes, fundMovementTrendCode(item.MainRatio))
			case "delta1":
				row = append(row, humanFundMovementAmount(item.Delta1Minute))
				codes = append(codes, fundMovementTrendCode(item.Delta1Minute))
			case "delta3":
				row = append(row, humanFundMovementAmount(item.Delta3Minutes))
				codes = append(codes, fundMovementTrendCode(item.Delta3Minutes))
			case "delta5":
				row = append(row, humanFundMovementAmount(item.Delta5Minutes))
				codes = append(codes, fundMovementTrendCode(item.Delta5Minutes))
			case "state":
				row = append(row, compactFundMovementState(item.State, moyu))
				codes = append(codes, fundMovementStateCode(item.State))
			}
		}
		rows = append(rows, row)
		rowCodes = append(rowCodes, codes)
	}

	widths := make([]int, len(header))
	minimums := make([]int, len(header))
	for column, heading := range header {
		widths[column] = displayWidth(heading)
		minimums[column] = displayWidth(heading)
		for _, row := range rows {
			widths[column] = maxInt(widths[column], displayWidth(row[column]))
		}
		switch kinds[column] {
		case "code":
			widths[column] = min(widths[column], 8)
			minimums[column] = min(widths[column], 8)
		case "name":
			widths[column] = min(widths[column], 10)
			if terminalWidth >= 60 {
				minimums[column] = min(widths[column], 8)
			}
		case "industry":
			widths[column] = min(widths[column], 22)
			minimums[column] = min(widths[column], displayWidth(heading))
		case "state":
			widths[column] = min(widths[column], 14)
			if terminalWidth >= 68 {
				minimums[column] = min(widths[column], 8)
			}
		default:
			widths[column] = min(widths[column], 10)
		}
	}
	totalWidth := func() int {
		result := 1 + len(widths)*3
		for _, width := range widths {
			result += width
		}
		return result
	}
	shrinkKind := func(kind string) bool {
		for column, candidate := range kinds {
			if candidate == kind && widths[column] > minimums[column] {
				widths[column]--
				return true
			}
		}
		return false
	}
	for totalWidth() > terminalWidth {
		changed := shrinkKind("industry")
		if totalWidth() > terminalWidth {
			changed = shrinkKind("state") || changed
		}
		if totalWidth() > terminalWidth {
			changed = shrinkKind("name") || changed
		}
		if !changed {
			for column := len(widths) - 1; column >= 0 && totalWidth() > terminalWidth; column-- {
				if widths[column] > displayWidth(header[column]) {
					widths[column]--
					changed = true
				}
			}
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
			if heading || kinds[column] == "code" {
				alignment = "center"
			} else if kinds[column] == "name" || kinds[column] == "industry" || kinds[column] == "state" {
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
