package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type LiveData struct {
	Quotes          []domain.Quote
	Symbols         []string
	Indices         []domain.Quote
	Flows           map[string]domain.FundFlow
	Boards          []domain.BoardFlow
	DragonTiger     *domain.DragonTigerSnapshot
	PreviousAmounts map[string]float64
	RefreshedAt     time.Time
	MarketStatus    string
	FetchError      string
	FlowError       string
	Status          string
	Footer          string
	GroupName       string
	GroupCount      int
	Selected        int
	Detail          bool
}

func indexLabel(symbol string, moyu bool) string {
	if moyu {
		switch symbol {
		case "sh000001":
			return "SSE"
		case "sz399001":
			return "SZSE"
		case "sz399006":
			return "CHINEXT"
		}
	}
	switch symbol {
	case "sh000001":
		return "上证"
	case "sz399001":
		return "深证"
	case "sz399006":
		return "创业板"
	default:
		return symbol
	}
}

func marketStatusText(value string, moyu bool) string {
	if !moyu {
		return value
	}
	switch value {
	case "交易中":
		return "OPEN"
	case "午间休市":
		return "LUNCH BREAK"
	case "已收盘":
		return "CLOSED"
	case "休市":
		return "CLOSED"
	case "未开盘":
		return "PRE-OPEN"
	default:
		return strings.ToUpper(value)
	}
}

func refreshedText(value time.Time) string {
	if value.IsZero() {
		return "--:--:--"
	}
	return value.Format("01-02 15:04:05")
}

func marketOverview(indices []domain.Quote, moyu, color bool, width int) string {
	bySymbol := make(map[string]domain.Quote, len(indices))
	for _, item := range indices {
		bySymbol[item.Symbol] = item
	}
	parts := make([]string, 0, 3)
	for _, symbol := range []string{"sh000001", "sz399001", "sz399006"} {
		item, ok := bySymbol[symbol]
		current := "--"
		percent := "--"
		if ok {
			current = item.Current
			percent = signedPercent(item.Percent)
			if color && !moyu {
				current = trendValue(current, item.Delta, true)
				percent = trendValue(percent, item.Percent, true)
			}
		}
		parts = append(parts, fmt.Sprintf("%s %s %s", indexLabel(symbol, moyu), current, percent))
	}
	prefix := "大盘  "
	separator := "  ·  "
	if moyu {
		prefix = "MARKET  "
		separator = " | "
	}
	line := prefix + strings.Join(parts, separator)
	return truncateWidth(line, width)
}

func marketFlowOverview(flows map[string]domain.FundFlow, moyu, color bool, width int) string {
	parts := make([]string, 0, 3)
	for _, symbol := range []string{"sh000001", "sz399001", "sz399006"} {
		value := "--"
		if flow, ok := flows[symbol]; ok {
			value = directionalFundFlow(&flow)
			if color && !moyu && !math.IsNaN(flow.MainNet) {
				value = style(value, trendCode(flow.MainNet, false), true)
			}
		}
		parts = append(parts, indexLabel(symbol, moyu)+" "+value)
	}
	prefix := "指数资金  "
	separator := "  ·  "
	if moyu {
		prefix = "INDEX FLOW  "
		separator = " | "
	}
	return truncateWidth(prefix+strings.Join(parts, separator), width)
}

func marketTotalAmount(indices []domain.Quote) float64 {
	var total float64
	seenShanghai, seenShenzhen := false, false
	for _, item := range indices {
		if item.Amount <= 0 || math.IsNaN(item.Amount) || math.IsInf(item.Amount, 0) {
			continue
		}
		switch item.Symbol {
		case "sh000001":
			total += item.Amount
			seenShanghai = true
		case "sz399001":
			total += item.Amount
			seenShenzhen = true
		}
	}
	if !seenShanghai || !seenShenzhen {
		return math.NaN()
	}
	return total
}

func humanMarketAmount(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	switch {
	case value >= 1e8:
		return fmt.Sprintf("%.2f万亿", value/1e8)
	case value >= 1e4:
		return fmt.Sprintf("%.2f亿元", value/1e4)
	default:
		return fmt.Sprintf("%.0f万元", value)
	}
}

func marketAmountOverview(indices []domain.Quote, previous map[string]float64, moyu bool, width int) string {
	current := marketTotalAmount(indices)
	value := humanMarketAmount(current)
	change := ""
	previousShanghai, shanghaiOK := previous["sh000001"]
	previousShenzhen, shenzhenOK := previous["sz399001"]
	if shanghaiOK && shenzhenOK && previousShanghai > 0 && previousShenzhen > 0 && !math.IsNaN(current) {
		previous := previousShanghai + previousShenzhen
		delta := current - previous
		arrow := "→"
		if delta > 0 {
			arrow = "↑"
		} else if delta < 0 {
			arrow = "↓"
		}
		percent := delta / previous * 100
		change = fmt.Sprintf(" 较昨 %s %s (%+.2f%%)", arrow, humanMarketAmount(math.Abs(delta)), percent)
	}
	if moyu {
		return truncateWidth("TOTAL AMT  "+value+change, width)
	}
	return truncateWidth("沪深总成交额  "+value+change, width)
}

func liveHeader(data LiveData, options ViewOptions, width int) string {
	status := marketStatusText(data.MarketStatus, options.Moyu)
	var first string
	if options.Moyu {
		first = fmt.Sprintf("WORKMON  UPDATE %s  %s", refreshedText(data.RefreshedAt), status)
	} else {
		first = style("ASTOCK", "1;36", options.Color) + "  沪深 LEVEL-1  " +
			style("·", "90", options.Color) + "  更新 " + refreshedText(data.RefreshedAt) + "  " + status
	}
	if displayWidth(first) > width {
		first = truncateWidth(ansiPattern.ReplaceAllString(first, ""), width)
	}
	header := first + "\n" + marketOverview(data.Indices, options.Moyu, options.Color, width) +
		"\n" + marketFlowOverview(data.Flows, options.Moyu, options.Color, width) +
		"\n" + marketAmountOverview(data.Indices, data.PreviousAmounts, options.Moyu, width)
	if data.GroupName != "" {
		label := fmt.Sprintf("自选分组  %s  ·  %d只", data.GroupName, data.GroupCount)
		if options.Moyu {
			label = fmt.Sprintf("GROUP  %s  |  %d STOCKS", data.GroupName, data.GroupCount)
		}
		header += "\n" + truncateWidth(label, width)
	}
	return header
}

func visibleQuoteWindow(total, selected, limit int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if limit <= 0 || limit >= total {
		return 0, total
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= total {
		selected = total - 1
	}
	start := selected - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > total {
		start = total - limit
	}
	return start, start + limit
}

func liveQuotes(data LiveData) []domain.Quote {
	if len(data.Quotes) > 0 {
		return data.Quotes
	}
	return placeholderQuotes(data.Symbols)
}

func liveError(data LiveData, options ViewOptions, width int) string {
	message := ""
	if data.FetchError != "" {
		message = "行情暂不可用，保留上一帧并重试：" + data.FetchError
	} else if data.FlowError != "" {
		if options.Moyu {
			message = "FLOW N/A"
		} else {
			message = "资金流暂不可用，行情不受影响"
		}
	}
	return truncateWidth(message, width)
}

func liveFooter(data LiveData, options ViewOptions, width int) string {
	if data.Footer != "" {
		return truncateWidth(data.Footer, width)
	}
	if options.Moyu {
		if data.Detail {
			return truncateWidth("UP/DOWN SCROLL  PGUP/PGDN PAGE  ESC BACK  Q QUIT", width)
		}
		return truncateWidth("UP/DOWN SELECT  ENTER DETAIL  A ADD  D DELETE  I VIEW  E REORDER  F GROUP  Q QUIT", width)
	}
	if data.Detail {
		return truncateWidth("↑/↓ 滚动  PgUp/PgDn翻页  Esc返回  q退出", width)
	}
	return truncateWidth("↑/↓ 选择  Enter详情  a添加  d删除  i查看  e排序  f分组  q退出", width)
}

func liveStatus(data LiveData, width int) string {
	lines := strings.Split(data.Status, "\n")
	for index := range lines {
		lines[index] = truncateWidth(lines[index], width)
	}
	return strings.Join(lines, "\n")
}

func liveStatusRows(data LiveData) int {
	if data.Status == "" {
		return 1
	}
	return strings.Count(data.Status, "\n") + 1
}

func BuildLiveFrame(data LiveData, options ViewOptions, terminalWidth, terminalHeight int) string {
	quotes := liveQuotes(data)
	if data.Selected < 0 {
		data.Selected = 0
	}
	if data.Selected >= len(quotes) {
		data.Selected = len(quotes) - 1
	}
	header := liveHeader(data, options, terminalWidth)

	if data.Detail && data.Selected >= 0 && data.Selected < len(quotes) {
		flow, hasFlow := data.Flows[quotes[data.Selected].Symbol]
		var flowPointer *domain.FundFlow
		if hasFlow && !math.IsNaN(flow.MainNet) {
			flowPointer = &flow
		}
		detailOptions := options
		detailOptions.Moyu = false
		frame := header + "\n\n" + dashboardCard(quotes[data.Selected], flowPointer, data.Boards, data.DragonTiger, detailOptions, terminalWidth)
		if message := liveError(data, options, terminalWidth); message != "" {
			frame += "\n" + message
		}
		frame += "\n" + liveStatus(data, terminalWidth)
		frame += "\n" + liveFooter(data, options, terminalWidth)
		return frame
	}

	reservedRows := 11
	if options.Moyu {
		reservedRows = 10
	}
	if data.GroupName != "" {
		reservedRows++
	}
	reservedRows += liveStatusRows(data) - 1
	limit := terminalHeight - reservedRows
	if limit < 1 {
		limit = 1
	}
	start, end := visibleQuoteWindow(len(quotes), data.Selected, limit)
	visible := quotes[start:end]
	selected := data.Selected - start
	table := buildQuoteTable(visible, data.Flows, selected, terminalWidth, options.Moyu, options.Color)
	frame := header + "\n" + table
	if len(quotes) > len(visible) {
		frame += fmt.Sprintf("\n%d-%d/%d", start+1, end, len(quotes))
	}
	if message := liveError(data, options, terminalWidth); message != "" {
		frame += "\n" + message
	}
	frame += "\n" + liveStatus(data, terminalWidth)
	frame += "\n" + liveFooter(data, options, terminalWidth)
	return frame
}

// BuildSnapshotFrame keeps --once as a plain, complete snapshot while adding
// the same market status, index overview and fund-flow context as live mode.
func BuildSnapshotFrame(data LiveData, options ViewOptions, terminalWidth int) string {
	quotes := liveQuotes(data)
	header := liveHeader(data, options, terminalWidth)
	if options.Moyu {
		frame := header + "\n" + buildQuoteTable(quotes, data.Flows, -1, terminalWidth, true, false)
		if message := liveError(data, options, terminalWidth); message != "" {
			frame += "\n" + message
		}
		return frame
	}

	var builder strings.Builder
	builder.WriteString(header)
	builder.WriteString("\n\n")
	for index, item := range quotes {
		flow, ok := data.Flows[item.Symbol]
		var flowPointer *domain.FundFlow
		if ok {
			flowPointer = &flow
		}
		builder.WriteString(dashboardCard(item, flowPointer, nil, nil, options, terminalWidth))
		if index < len(quotes)-1 {
			builder.WriteString("\n\n")
		}
	}
	if message := liveError(data, options, terminalWidth); message != "" {
		builder.WriteByte('\n')
		builder.WriteString(message)
	}
	return builder.String()
}
