package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type ViewOptions struct {
	Depth bool
	Moyu  bool
	Color bool
}

type metric struct {
	Label string
	Value string
}

func style(value, code string, enabled bool) string {
	if !enabled || value == "" {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func humanVolume(value float64) string {
	if math.IsNaN(value) {
		return "--"
	}
	if value >= 10000 {
		return fmt.Sprintf("%.2f万手", value/10000)
	}
	return fmt.Sprintf("%.0f手", value)
}

func humanAmount(value float64) string {
	if math.IsNaN(value) {
		return "--"
	}
	if value >= 10000 {
		return fmt.Sprintf("%.2f亿元", value/10000)
	}
	return fmt.Sprintf("%.0f万元", value)
}

func humanMarketCap(value float64) string {
	if math.IsNaN(value) || value <= 0 {
		return "--"
	}
	if value >= 10000 {
		return fmt.Sprintf("%.2f万亿", value/10000)
	}
	return fmt.Sprintf("%.2f亿", value)
}

func withUnit(value, unit string) string {
	if value == "" || value == "--" {
		return "--"
	}
	return value + unit
}

func trendCode(delta float64, bold bool) string {
	prefix := ""
	if bold {
		prefix = "1;"
	}
	if delta > 0 {
		return prefix + "31"
	}
	if delta < 0 {
		return prefix + "32"
	}
	return prefix + "33"
}

func trendValue(value string, delta float64, color bool) string {
	if !color || math.IsNaN(delta) {
		return value
	}
	return style(value, trendCode(delta, false), true)
}

func parsePrice(value string) (float64, bool) {
	result, err := strconv.ParseFloat(value, 64)
	return result, err == nil && !math.IsNaN(result) && !math.IsInf(result, 0)
}

func priceDelta(value, previousClose string) float64 {
	price, priceOK := parsePrice(value)
	previous, previousOK := parsePrice(previousClose)
	if !priceOK || !previousOK {
		return math.NaN()
	}
	return price - previous
}

func orderVolume(value string) string {
	volume, ok := parsePrice(value)
	if !ok {
		return "--"
	}
	if volume >= 10000 {
		return fmt.Sprintf("%.2f万手", volume/10000)
	}
	return fmt.Sprintf("%.0f手", volume)
}

func firstLevel(levels []domain.DepthLevel) domain.DepthLevel {
	if len(levels) == 0 {
		return domain.DepthLevel{Level: 1, Price: "--", Volume: "--"}
	}
	return levels[0]
}

func quoteClock(value string) string {
	if len(value) >= 8 {
		return value[len(value)-8:]
	}
	return value
}

func sideBySide(left, right string, width int) []string {
	gap := width - displayWidth(left) - displayWidth(right)
	if gap >= 3 {
		return []string{left + strings.Repeat(" ", gap) + right}
	}
	return []string{left, right}
}

func framedLine(content string, width int, color bool) string {
	if displayWidth(content) > width {
		content = truncateWidth(ansiPattern.ReplaceAllString(content, ""), width)
	}
	border := style("│", "90", color)
	return border + " " + padWidth(content, width, "left") + " " + border
}

func frameBorder(left, fill, right string, width int, color bool) string {
	return style(left+strings.Repeat(fill, width+2)+right, "90", color)
}

func metricText(item metric, color bool) string {
	return style(item.Label, "90", color) + " " + item.Value
}

func packMetrics(metrics []metric, width int, color bool) []string {
	const separator = "   "
	lines := make([]string, 0)
	current := ""
	for _, item := range metrics {
		value := metricText(item, color)
		candidate := value
		if current != "" {
			candidate = current + separator + value
		}
		if current != "" && displayWidth(candidate) > width {
			lines = append(lines, current)
			current = value
		} else {
			current = candidate
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func priceRail(item domain.Quote, width int, color bool) string {
	low, lowOK := parsePrice(item.Low)
	high, highOK := parsePrice(item.High)
	current, currentOK := parsePrice(item.Current)
	label := style("日内", "90", color)
	if !lowOK || !highOK || !currentOK {
		return fmt.Sprintf("%s  %s — %s", label, item.Low, item.High)
	}
	prefix := label + "  " + item.Low + "  "
	suffix := "  " + item.High
	railWidth := width - displayWidth(prefix) - displayWidth(suffix) - 2
	if railWidth < 8 {
		return fmt.Sprintf("%s  %s  ← %s →  %s", label, item.Low, item.Current, item.High)
	}
	if railWidth > 34 {
		railWidth = 34
	}
	position := railWidth / 2
	if high > low {
		ratio := (current - low) / (high - low)
		if ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}
		position = int(math.Round(ratio * float64(railWidth-1)))
	}
	rail := []rune(strings.Repeat("─", railWidth))
	rail[position] = '●'
	railText := "├" + string(rail) + "┤"
	if color {
		markerCode := trendCode(item.Delta, true)
		railText = style("├", "90", true) + style(string(rail[:position]), "90", true) +
			style("●", markerCode, true) + style(string(rail[position+1:]), "90", true) + style("┤", "90", true)
	}
	return prefix + railText + suffix
}

func marketTag(symbol string) string {
	if strings.HasPrefix(symbol, "sh") {
		return "SH"
	}
	return "SZ"
}

func boardFlowSummary(boards []domain.BoardFlow, stockFlow *domain.FundFlow) string {
	positive, negative, valid := 0, 0, 0
	for _, board := range boards {
		if math.IsNaN(board.MainNet) || math.IsInf(board.MainNet, 0) {
			continue
		}
		valid++
		if board.MainNet > 0 {
			positive++
		} else if board.MainNet < 0 {
			negative++
		}
	}
	if valid == 0 {
		return "板块资金暂不可用"
	}
	bias := 0
	label := fmt.Sprintf("资金分化（流入%d/流出%d）", positive, negative)
	if positive*3 >= valid*2 {
		bias = 1
		label = fmt.Sprintf("多数净流入（%d/%d）", positive, valid)
	} else if negative*3 >= valid*2 {
		bias = -1
		label = fmt.Sprintf("多数净流出（%d/%d）", negative, valid)
	}
	if stockFlow == nil || math.IsNaN(stockFlow.MainNet) || math.IsInf(stockFlow.MainNet, 0) || bias == 0 || stockFlow.MainNet == 0 {
		return label
	}
	switch {
	case bias > 0 && stockFlow.MainNet > 0, bias < 0 && stockFlow.MainNet < 0:
		return label + "，个股与板块同向"
	case bias > 0 && stockFlow.MainNet < 0:
		return label + "，个股弱于板块"
	default:
		return label + "，个股逆板块走强"
	}
}

func boardFlowLine(board domain.BoardFlow, color bool) string {
	kind := "概念"
	if board.Kind == domain.BoardKindIndustry {
		kind = "行业"
	}
	percent := signedPercent(board.Percent)
	if color && !math.IsNaN(board.Percent) {
		percent = trendValue(percent, board.Percent, true)
	}
	flow := domain.FundFlow{MainNet: board.MainNet, MainRatio: board.MainRatio}
	flowText := directionalFundFlow(&flow) + " " + fundFlowRatio(&flow)
	if color && !math.IsNaN(board.MainNet) {
		flowText = style(flowText, trendCode(board.MainNet, false), true)
	}
	line := style(kind, "90", color) + "  " + board.Name + "  " + percent + "  " + flowText
	if board.LeaderName != "" && board.LeaderName != "-" {
		leaderPercent := signedPercent(board.LeaderPercent)
		if color && !math.IsNaN(board.LeaderPercent) {
			leaderPercent = trendValue(leaderPercent, board.LeaderPercent, true)
		}
		line += "  " + style("领涨", "90", color) + " " + board.LeaderName + " " + leaderPercent
	}
	return line
}

func dashboardCard(item domain.Quote, flow *domain.FundFlow, boards []domain.BoardFlow, options ViewOptions, terminalWidth int) string {
	cardWidth := terminalWidth
	if cardWidth > 100 {
		cardWidth = 100
	}
	if cardWidth < 31 {
		cardWidth = 31
	}
	innerWidth := cardWidth - 4
	color := options.Color

	lines := make([]string, 0, 16)
	name := item.Name
	if name == "" {
		name = item.Code
	}
	headerLeft := style(name, "1;37", color) + "  " + style(item.Code+" · "+marketTag(item.Symbol), "36", color)
	headerRight := style("LEVEL-1", "1;36", color) + "  " + style(quoteClock(item.QuoteTime), "90", color)
	lines = append(lines, sideBySide(headerLeft, headerRight, innerWidth)...)
	lines = append(lines, "\x00separator")

	change := "--  --"
	if !math.IsNaN(item.Delta) && !math.IsNaN(item.Percent) {
		change = fmt.Sprintf("%+.2f  %+.2f%%", item.Delta, item.Percent)
	}
	priceLine := style("现价", "90", color) + "  " + style(item.Current, trendCode(item.Delta, true), color) +
		"   " + style(change, trendCode(item.Delta, false), color)
	bid := firstLevel(item.Bids)
	ask := firstLevel(item.Asks)
	bidLine := style("买一", "1;31", color) + "  " + trendValue(bid.Price, priceDelta(bid.Price, item.PreviousClose), color) + " × " + orderVolume(bid.Volume)
	askLine := style("卖一", "1;32", color) + "  " + trendValue(ask.Price, priceDelta(ask.Price, item.PreviousClose), color) + " × " + orderVolume(ask.Volume)
	lines = append(lines, sideBySide(priceLine, bidLine, innerWidth)...)
	referenceLine := style("昨收", "90", color) + " " + item.PreviousClose + "   " + style("今开", "90", color) + " " + item.Open
	lines = append(lines, sideBySide(referenceLine, askLine, innerWidth)...)
	lines = append(lines, priceRail(item, innerWidth, color))
	lines = append(lines, "\x00separator")

	metrics := []metric{
		{Label: "成交量", Value: humanVolume(item.Volume)},
		{Label: "成交额", Value: humanAmount(item.Amount)},
		{Label: "换手", Value: withUnit(item.Turnover, "%")},
		{Label: "振幅", Value: withUnit(item.Amplitude, "%")},
		{Label: "量比", Value: item.VolumeRatio},
		{Label: "均价", Value: item.AveragePrice},
		{Label: "PE(TTM)", Value: item.PETTM},
		{Label: "PB", Value: item.PB},
		{Label: "总市值", Value: humanMarketCap(item.MarketCap)},
		{Label: "流通值", Value: humanMarketCap(item.FloatMarketCap)},
		{Label: "涨停", Value: trendValue(item.LimitUp, 1, color)},
		{Label: "跌停", Value: trendValue(item.LimitDown, -1, color)},
	}
	if flow != nil {
		flowValue := directionalFundFlow(flow)
		if color && !math.IsNaN(flow.MainNet) {
			flowValue = style(flowValue, trendCode(flow.MainNet, false), true)
		}
		metrics = append(metrics,
			metric{Label: "主力资金", Value: flowValue},
			metric{Label: "主力净占比", Value: fundFlowRatio(flow)},
		)
	}
	lines = append(lines, packMetrics(metrics, innerWidth, color)...)

	if len(boards) > 0 {
		lines = append(lines, "\x00separator")
		lines = append(lines, style("板块资金", "1;36", color)+"  "+boardFlowSummary(boards, flow))
		for _, board := range boards {
			lines = append(lines, boardFlowLine(board, color))
		}
	}

	if options.Depth {
		lines = append(lines, "\x00separator")
		lines = append(lines, style("五档盘口", "1;36", color)+"  "+style("价格 × 手", "90", color))
		for index := 0; index < 5; index++ {
			bidLevel := domain.DepthLevel{Level: index + 1, Price: "--", Volume: "--"}
			askLevel := domain.DepthLevel{Level: index + 1, Price: "--", Volume: "--"}
			if index < len(item.Bids) {
				bidLevel = item.Bids[index]
			}
			if index < len(item.Asks) {
				askLevel = item.Asks[index]
			}
			left := style(fmt.Sprintf("买%d", bidLevel.Level), "31", color) + "  " +
				trendValue(bidLevel.Price, priceDelta(bidLevel.Price, item.PreviousClose), color) + " × " + orderVolume(bidLevel.Volume)
			right := style(fmt.Sprintf("卖%d", askLevel.Level), "32", color) + "  " +
				trendValue(askLevel.Price, priceDelta(askLevel.Price, item.PreviousClose), color) + " × " + orderVolume(askLevel.Volume)
			lines = append(lines, sideBySide(left, right, innerWidth)...)
		}
	}

	var builder strings.Builder
	builder.WriteString(frameBorder("╭", "─", "╮", innerWidth, color))
	builder.WriteByte('\n')
	for _, line := range lines {
		if line == "\x00separator" {
			builder.WriteString(frameBorder("├", "─", "┤", innerWidth, color))
		} else {
			builder.WriteString(framedLine(line, innerWidth, color))
		}
		builder.WriteByte('\n')
	}
	builder.WriteString(frameBorder("╰", "─", "╯", innerWidth, color))
	return builder.String()
}

func buildStandardView(quotes []domain.Quote, options ViewOptions, terminalWidth int, refreshed time.Time, fetchError string) string {
	var builder strings.Builder
	brand := style("ASTOCK", "1;36", options.Color) + "  沪深 LEVEL-1  " +
		style("·", "90", options.Color) + "  腾讯公开行情  " + style("·", "90", options.Color) +
		"  REFRESH " + refreshed.Format("15:04:05")
	if displayWidth(brand) > terminalWidth {
		brand = truncateWidth(ansiPattern.ReplaceAllString(brand, ""), terminalWidth)
	}
	builder.WriteString(brand)
	builder.WriteString("\n\n")
	for quoteIndex, item := range quotes {
		builder.WriteString(dashboardCard(item, nil, nil, options, terminalWidth))
		if quoteIndex < len(quotes)-1 {
			builder.WriteString("\n\n")
		}
	}
	if fetchError != "" {
		builder.WriteString("\n")
		builder.WriteString(style("行情暂不可用，保留上一帧并重试：", "33", options.Color) + fetchError)
	}
	return builder.String()
}

func placeholderQuotes(symbols []string) []domain.Quote {
	result := make([]domain.Quote, 0, len(symbols))
	for _, symbol := range symbols {
		result = append(result, domain.Quote{
			Symbol: symbol, Name: symbol[2:], TaskName: symbol[2:], Code: symbol[2:],
			Current: "--", PreviousClose: "--", Open: "--", High: "--", Low: "--",
			QuoteTime: "--", Turnover: "--", Amplitude: "--", PETTM: "--", PEStatic: "--",
			PB: "--", LimitUp: "--", LimitDown: "--", VolumeRatio: "--", AveragePrice: "--",
			Delta: math.NaN(), Percent: math.NaN(), Volume: math.NaN(), Amount: math.NaN(),
			MarketCap: math.NaN(), FloatMarketCap: math.NaN(),
		})
	}
	return result
}

func BuildFrame(quotes []domain.Quote, symbols []string, options ViewOptions, terminalWidth int, refreshed time.Time, fetchError string) string {
	visible := quotes
	if len(visible) == 0 {
		visible = placeholderQuotes(symbols)
	}
	if options.Moyu {
		frame := buildMoyuTable(visible, terminalWidth)
		if fetchError != "" && len(quotes) == 0 {
			frame += "\n数据暂不可用：" + fetchError
		}
		return frame
	}
	return buildStandardView(visible, options, terminalWidth, refreshed, fetchError)
}
