package ui

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func runeDisplayWidth(character rune) int {
	if unicode.Is(unicode.Mn, character) || unicode.Is(unicode.Me, character) {
		return 0
	}
	switch {
	case character >= 0x1100 && character <= 0x115f,
		character >= 0x2329 && character <= 0x232a,
		character >= 0x2e80 && character <= 0xa4cf,
		character >= 0xac00 && character <= 0xd7a3,
		character >= 0xf900 && character <= 0xfaff,
		character >= 0xfe10 && character <= 0xfe19,
		character >= 0xfe30 && character <= 0xfe6f,
		character >= 0xff00 && character <= 0xff60,
		character >= 0xffe0 && character <= 0xffe6,
		character >= 0x1f300 && character <= 0x1faff,
		character >= 0x20000 && character <= 0x3fffd:
		return 2
	default:
		return 1
	}
}

func displayWidth(value string) int {
	value = ansiPattern.ReplaceAllString(value, "")
	width := 0
	for _, character := range value {
		width += runeDisplayWidth(character)
	}
	return width
}

func truncateWidth(value string, maximum int) string {
	if displayWidth(value) <= maximum {
		return value
	}
	if maximum <= 0 {
		return ""
	}
	suffix := "…"
	target := maximum - 1
	if target < 0 {
		target = 0
		suffix = ""
	}
	var builder strings.Builder
	width := 0
	for len(value) > 0 {
		character, size := utf8.DecodeRuneInString(value)
		characterWidth := runeDisplayWidth(character)
		if width+characterWidth > target {
			break
		}
		builder.WriteRune(character)
		width += characterWidth
		value = value[size:]
	}
	return builder.String() + suffix
}

func padWidth(value string, width int, alignment string) string {
	remaining := width - displayWidth(value)
	if remaining < 0 {
		remaining = 0
	}
	switch alignment {
	case "right":
		return strings.Repeat(" ", remaining) + value
	case "center":
		left := remaining / 2
		return strings.Repeat(" ", left) + value + strings.Repeat(" ", remaining-left)
	default:
		return value + strings.Repeat(" ", remaining)
	}
}

func signedPercent(value float64) string {
	if math.IsNaN(value) {
		return "--"
	}
	return fmt.Sprintf("%+.2f%%", value)
}

func maxInt(values ...int) int {
	result := 0
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func humanFundFlow(flow *domain.FundFlow) string {
	if flow == nil || math.IsNaN(flow.MainNet) || math.IsInf(flow.MainNet, 0) {
		return "--"
	}
	value := flow.MainNet
	absolute := math.Abs(value)
	switch {
	case absolute >= 1e8:
		return fmt.Sprintf("%+.2f亿", value/1e8)
	case absolute >= 1e4:
		return fmt.Sprintf("%+.0f万", value/1e4)
	default:
		return fmt.Sprintf("%+.0f元", value)
	}
}

func fundFlowMagnitude(flow *domain.FundFlow) string {
	value := humanFundFlow(flow)
	return strings.TrimLeft(value, "+-")
}

func fundFlowRatio(flow *domain.FundFlow) string {
	if flow == nil || math.IsNaN(flow.MainRatio) || math.IsInf(flow.MainRatio, 0) {
		return "--"
	}
	return fmt.Sprintf("%+.2f%%", flow.MainRatio)
}

func directionalFundFlow(flow *domain.FundFlow) string {
	if humanFundFlow(flow) == "--" {
		return "--"
	}
	if flow.MainNet > 0 {
		return "↑ " + fundFlowMagnitude(flow)
	}
	if flow.MainNet < 0 {
		return "↓ " + fundFlowMagnitude(flow)
	}
	return "→ " + fundFlowMagnitude(flow)
}

func buildMoyuTable(quotes []domain.Quote, terminalWidth int, pinyin bool) string {
	return buildQuoteTable(quotes, nil, -1, terminalWidth, true, false, pinyin)
}

func buildQuoteTable(quotes []domain.Quote, flows map[string]domain.FundFlow, selected, terminalWidth int, moyu, color, pinyin bool) string {
	header := []string{"个股", "现价", "涨跌", "涨速", "资金"}
	if moyu {
		header = []string{"TASK", "VALUE", "DRIFT", "SPEED", "FLOW"}
		if terminalWidth < 44 {
			header = []string{"TASK", "VAL", "%", "SPD", "FLOW"}
		}
	} else {
		header = []string{"个股", "现价", "涨跌", "涨速", "资金", "涨停", "跌停"}
	}
	showRange := (moyu && terminalWidth >= 96) || (!moyu && terminalWidth >= 116)
	if showRange {
		if moyu {
			header = append(header, "FLOOR", "CEILING")
		} else {
			header = append(header, "最低", "最高")
		}
	}
	rows := make([][]string, 0, len(quotes))
	rowCodes := make([][]string, 0, len(quotes))
	for index, item := range quotes {
		task := item.TaskName
		if !moyu || task == "" {
			task = item.Name
		}
		if task == "" {
			task = item.Symbol
		}
		if market.AssetKindOf(item.Symbol) == domain.AssetKindSector {
			if pinyin {
				task = "BOARD·" + task
			} else {
				task = "板块·" + task
			}
		}
		if selected >= 0 {
			marker := "  "
			if index == selected {
				marker = "> "
			}
			task = marker + task
		}
		flow, hasFlow := flows[item.Symbol]
		speed := "--"
		flowText := "--"
		speedCode := ""
		flowCode := ""
		if hasFlow {
			speed = signedPercent(flow.Speed)
			flowText = directionalFundFlow(&flow)
			if !math.IsNaN(flow.Speed) {
				speedCode = trendCode(flow.Speed, false)
			}
			if !math.IsNaN(flow.MainNet) {
				flowCode = trendCode(flow.MainNet, false)
			}
		}
		row := []string{task, item.Current, signedPercent(item.Percent), speed, flowText}
		codes := []string{"", trendCode(item.Delta, true), trendCode(item.Percent, false), speedCode, flowCode}
		if !moyu {
			row = append(row, item.LimitUp, item.LimitDown)
			codes = append(codes, "31", "32")
		}
		if showRange {
			row = append(row, item.Low, item.High)
			codes = append(codes, "", "")
		}
		rows = append(rows, row)
		rowCodes = append(rowCodes, codes)
	}

	widths := make([]int, len(header))
	for column, title := range header {
		widths[column] = displayWidth(title)
		for _, row := range rows {
			widths[column] = maxInt(widths[column], displayWidth(row[column]))
		}
	}
	totalWidth := func() int {
		result := 1 + len(widths)*3
		for _, width := range widths {
			result += width
		}
		return result
	}
	if widths[0] > 28 {
		widths[0] = 28
	}
	for totalWidth() > terminalWidth && widths[0] > displayWidth(header[0]) {
		widths[0]--
	}
	for totalWidth() > terminalWidth {
		changed := false
		for column := len(widths) - 1; column >= 1 && totalWidth() > terminalWidth; column-- {
			minimum := displayWidth(header[column])
			if widths[column] > minimum {
				widths[column]--
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	for index := range rows {
		for column := range rows[index] {
			rows[index][column] = truncateWidth(rows[index][column], widths[column])
		}
	}

	var builder strings.Builder
	border := "+"
	for _, width := range widths {
		border += strings.Repeat("-", width+2) + "+"
	}
	renderRow := func(row []string, codes []string, heading bool) {
		builder.WriteByte('|')
		for column, value := range row {
			if heading && color && !moyu {
				value = style(value, "1;36", true)
			} else if !heading && color && !moyu && codes[column] != "" {
				value = style(value, codes[column], true)
			}
			alignment := "right"
			if heading {
				alignment = "center"
			} else if column == 0 {
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
