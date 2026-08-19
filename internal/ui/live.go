package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type LiveData struct {
	Quotes                  []domain.Quote
	Symbols                 []string
	Indices                 []domain.Quote
	Flows                   map[string]domain.FundFlow
	Boards                  []domain.BoardFlow
	DragonTiger             *domain.DragonTigerSnapshot
	Technical               *domain.TechnicalSignal
	PreviousAmounts         domain.MarketAmountSnapshot
	RefreshedAt             time.Time
	MarketStatus            string
	FetchError              string
	FlowError               string
	Status                  string
	Footer                  string
	GroupName               string
	GroupCount              int
	Selected                int
	Detail                  bool
	RankingKind             domain.MarketRankingKind
	RankingItems            []domain.MarketRankingItem
	RankingSelected         int
	RankingRefreshedAt      time.Time
	FundMonitorActive       bool
	FundMonitorSource       string
	FundMonitorCount        int
	FundMovements           []domain.FundMovement
	FundMonitorSelected     int
	FundMonitorRefreshedAt  time.Time
	FundIndustryRefreshedAt time.Time
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
	case "集合竞价":
		return "CALL AUCTION"
	case "开盘等待":
		return "PRE-OPEN"
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
		labelWidth := 6
		if moyu {
			labelWidth = 7
		}
		parts = append(parts,
			padWidth(indexLabel(symbol, moyu), labelWidth, "left")+" "+
				padWidth(current, 8, "left")+" "+padWidth(percent, 7, "left"))
	}
	prefix := padWidth("大盘", 10, "left")
	separator := "  ·  "
	if moyu {
		prefix = padWidth("MARKET", 10, "left")
		separator = " | "
	}
	line := prefix + strings.Join(parts, separator)
	return truncateWidth(line, width)
}

func marketFlowOverview(flows map[string]domain.FundFlow, moyu, color bool, width int) string {
	labelWidth := 6
	partWidth := 23
	prefix := padWidth("指数资金", 10, "left")
	separator := "  ·  "
	if moyu {
		labelWidth = 7
		partWidth = 24
		prefix = padWidth("INDEX FLOW", 10, "left")
		separator = " | "
	}
	alignedWidth := displayWidth(prefix) + 3*partWidth + 2*displayWidth(separator)
	parts := make([]string, 0, 3)
	for _, symbol := range []string{"sh000001", "sz399001", "sz399006"} {
		value := "--"
		if flow, ok := flows[symbol]; ok {
			value = directionalFundFlow(&flow)
			if color && !moyu && !math.IsNaN(flow.MainNet) {
				value = style(value, trendCode(flow.MainNet, false), true)
			}
		}
		part := padWidth(indexLabel(symbol, moyu), labelWidth, "left") + " " + padWidth(value, 10, "left")
		if width >= alignedWidth {
			part = padWidth(part, partWidth, "left")
		}
		parts = append(parts, part)
	}
	return truncateWidth(prefix+strings.Join(parts, separator), width)
}

func marketTotalAmount(indices []domain.Quote) float64 {
	shanghai, shenzhen, _ := marketAmountComponents(indices)
	if math.IsNaN(shanghai) || math.IsNaN(shenzhen) {
		return math.NaN()
	}
	return shanghai + shenzhen
}

func marketAmountComponents(indices []domain.Quote) (shanghai, shenzhen, beijing float64) {
	shanghai, shenzhen, beijing = math.NaN(), math.NaN(), math.NaN()
	seenShenzhenComposite := false
	for _, item := range indices {
		if item.Amount <= 0 || math.IsNaN(item.Amount) || math.IsInf(item.Amount, 0) {
			continue
		}
		switch item.Symbol {
		case "sh000001":
			shanghai = item.Amount
		case "sz399106":
			shenzhen = item.Amount
			seenShenzhenComposite = true
		case "sz399001":
			if !seenShenzhenComposite {
				shenzhen = item.Amount
			}
		case "bj899050":
			beijing = item.Amount
		}
	}
	return shanghai, shenzhen, beijing
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

func marketQuoteTradeDate(indices []domain.Quote) string {
	for _, item := range indices {
		if item.Symbol == "sh000001" && len(item.QuoteTime) >= 10 {
			return item.QuoteTime[:10]
		}
	}
	return ""
}

func marketAmountChange(current, previous float64, currentDate, previousDate string) string {
	if current <= 0 || previous <= 0 || math.IsNaN(current) || math.IsNaN(previous) ||
		currentDate == "" || previousDate == "" || currentDate <= previousDate {
		return ""
	}
	delta := current - previous
	arrow := "→"
	if delta > 0 {
		arrow = "↑"
	} else if delta < 0 {
		arrow = "↓"
	}
	percent := delta / previous * 100
	return fmt.Sprintf(" 较昨 %s %s (%+.2f%%)", arrow, humanMarketAmount(math.Abs(delta)), percent)
}

func marketAmountLine(label string, current, previous float64, currentDate, previousDate string, moyu, color bool, width int) string {
	value := humanMarketAmount(current)
	change := marketAmountChange(current, previous, currentDate, previousDate)
	if change != "" && color && !moyu {
		change = style(change, trendCode(current-previous, false), true)
	}
	return truncateWidth(padWidth(label, 12, "left")+value+change, width)
}

func marketAmountOverview(indices []domain.Quote, previous domain.MarketAmountSnapshot, moyu, color bool, width int) string {
	shanghai, shenzhen, beijing := marketAmountComponents(indices)
	currentDate := marketQuoteTradeDate(indices)
	previousDate := previous.TradeDate
	allCurrent := math.NaN()
	allPrevious := math.NaN()
	if !math.IsNaN(shanghai) && !math.IsNaN(shenzhen) {
		if !math.IsNaN(beijing) {
			allCurrent = shanghai + shenzhen + beijing
		}
	}
	if previous.Shanghai > 0 && previous.Shenzhen > 0 && previous.Beijing > 0 {
		allPrevious = previous.Shanghai + previous.Shenzhen + previous.Beijing
	}
	if moyu {
		return marketAmountLine("TOTAL AMT", allCurrent, allPrevious, currentDate, previousDate, moyu, color, width)
	}
	return marketAmountLine("沪深成交额", allCurrent, allPrevious, currentDate, previousDate, moyu, color, width)
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
		"\n" + marketAmountOverview(data.Indices, data.PreviousAmounts, options.Moyu, options.Color, width)
	if data.GroupName != "" && data.RankingKind == "" && !data.FundMonitorActive {
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
	truncateLines := func(value string) string {
		lines := strings.Split(value, "\n")
		for index := range lines {
			lines[index] = truncateWidth(lines[index], width)
		}
		return strings.Join(lines, "\n")
	}
	if data.Footer != "" {
		return truncateLines(data.Footer)
	}
	if options.Moyu {
		if data.Detail {
			return truncateLines("UP/DOWN SCROLL  [/]/PGUP/PGDN PAGE  ESC BACK  Q QUIT\nC STOCK REPORT  O OPEN  S MARKET REPORT  R OPEN")
		}
		return truncateLines("UP/DN  ENTER  A ADD  D DEL  I VIEW  H HISTORY  E SORT  F GROUP  Q QUIT\n1 GAINERS  2 LOSERS  3 RAPID RISE  V FUND RADAR  Y BOARD FUNDS\nC STOCK REPORT  O OPEN  S MARKET REPORT  R OPEN")
	}
	if data.Detail {
		return truncateLines("↑/↓ 滚动  [/]翻页  Esc返回  q退出\nc个股研判  o查看  s市场报告  r查看")
	}
	return truncateLines("↑/↓ 选择  Enter详情  a添加  d删除  i查看  h历史  e排序  f分组  q退出\n1涨幅前20  2跌幅前20  3快速涨幅前20  v资金雷达  y板块资金\nc个股研判  o查看  s市场报告  r查看")
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
	footer := liveFooter(data, options, terminalWidth)

	if data.Detail && data.Selected >= 0 && data.Selected < len(quotes) {
		flow, hasFlow := data.Flows[quotes[data.Selected].Symbol]
		var flowPointer *domain.FundFlow
		if hasFlow && !math.IsNaN(flow.MainNet) {
			flowPointer = &flow
		}
		detailOptions := options
		detailOptions.Moyu = false
		frame := header + "\n\n" + dashboardCard(quotes[data.Selected], flowPointer, data.Boards, data.DragonTiger, data.Technical, detailOptions, terminalWidth)
		if message := liveError(data, options, terminalWidth); message != "" {
			frame += "\n" + message
		}
		frame += "\n" + liveStatus(data, terminalWidth)
		frame += "\n" + footer
		return frame
	}

	if data.FundMonitorActive {
		statusRows := liveStatusRows(data)
		footerRows := strings.Count(footer, "\n") + 1
		errorData := data
		errorData.FlowError = ""
		errorMessage := liveError(errorData, options, terminalWidth)
		fixedRows := strings.Count(header, "\n") + 1 + 1 + 4 + statusRows + footerRows
		if errorMessage != "" {
			fixedRows++
		}
		limit := terminalHeight - fixedRows
		if limit < 1 {
			limit = 1
		}
		if len(data.FundMovements) > limit && limit > 1 {
			limit--
		}
		start, end := visibleQuoteWindow(len(data.FundMovements), data.FundMonitorSelected, limit)
		table := buildFundMonitorTable(
			data.FundMovements[start:end], data.FundMonitorSelected-start,
			terminalWidth, options.Moyu, options.Color,
		)
		monitorCount := data.FundMonitorCount
		if monitorCount <= 0 {
			monitorCount = len(data.FundMovements)
		}
		title := fundMonitorTitle(
			data.FundMonitorSource, monitorCount, data.FundMonitorRefreshedAt,
			data.FundIndustryRefreshedAt, options.Moyu,
		)
		frame := header + "\n" + truncateWidth(title, terminalWidth) + "\n" + table
		if len(data.FundMovements) > end-start {
			frame += fmt.Sprintf("\n%d-%d/%d", start+1, end, len(data.FundMovements))
		}
		if errorMessage != "" {
			frame += "\n" + errorMessage
		}
		frame += "\n" + liveStatus(data, terminalWidth)
		frame += "\n" + footer
		return frame
	}

	if data.RankingKind != "" && len(data.RankingItems) > 0 {
		statusRows := liveStatusRows(data)
		footerRows := strings.Count(footer, "\n") + 1
		errorMessage := liveError(data, options, terminalWidth)
		fixedRows := strings.Count(header, "\n") + 1 + 1 + 4 + statusRows + footerRows
		if errorMessage != "" {
			fixedRows++
		}
		limit := terminalHeight - fixedRows
		if limit < 1 {
			limit = 1
		}
		if len(data.RankingItems) > limit && limit > 1 {
			limit--
		}
		start, end := visibleQuoteWindow(len(data.RankingItems), data.RankingSelected, limit)
		table := buildMarketRankingTable(
			data.RankingItems[start:end], data.RankingSelected-start, start,
			data.RankingKind, terminalWidth, options.Moyu, options.Color,
		)
		frame := header + "\n" + marketRankingTitle(data.RankingKind, len(data.RankingItems), data.RankingRefreshedAt, options.Moyu) + "\n" + table
		if len(data.RankingItems) > end-start {
			frame += fmt.Sprintf("\n%d-%d/%d", start+1, end, len(data.RankingItems))
		}
		if errorMessage != "" {
			frame += "\n" + errorMessage
		}
		frame += "\n" + liveStatus(data, terminalWidth)
		frame += "\n" + footer
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
	reservedRows += strings.Count(footer, "\n")
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
	frame += "\n" + footer
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
		builder.WriteString(dashboardCard(item, flowPointer, nil, nil, nil, options, terminalWidth))
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
