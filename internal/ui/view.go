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
	Depth  bool
	Moyu   bool
	Pinyin bool
	Color  bool
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

func boardRankFraction(rank, total int) float64 {
	if rank <= 0 || total <= 0 || rank > total {
		return math.NaN()
	}
	return float64(rank) / float64(total)
}

func boardRiseRatio(board domain.BoardFlow) float64 {
	total := board.RiseCount + board.FallCount + board.FlatCount
	if total <= 0 {
		return math.NaN()
	}
	return float64(board.RiseCount) / float64(total)
}

func boardHeatLabel(board domain.BoardFlow) string {
	if board.UniverseSize <= 0 {
		return ""
	}
	changeRank := boardRankFraction(board.ChangeRank, board.UniverseSize)
	flowRank := boardRankFraction(board.FlowRank, board.UniverseSize)
	turnoverRank := boardRankFraction(board.TurnoverRank, board.UniverseSize)
	breadth := boardRiseRatio(board)
	strongChange := !math.IsNaN(changeRank) && board.Percent > 0 && changeRank <= 0.15
	strongFlow := !math.IsNaN(flowRank) && board.MainNet > 0 && flowRank <= 0.20
	activeTurnover := !math.IsNaN(turnoverRank) && turnoverRank <= 0.20
	broadRise := !math.IsNaN(breadth) && breadth >= 0.65
	if broadRise && ((strongChange && (strongFlow || activeTurnover)) || (strongFlow && activeTurnover && board.Percent > 0)) {
		if board.MainNet < 0 {
			return "热门分歧"
		}
		return "热门"
	}
	activeSignals := 0
	if !math.IsNaN(changeRank) && board.Percent > 0 && changeRank <= 0.30 {
		activeSignals++
	}
	if !math.IsNaN(flowRank) && board.MainNet > 0 && flowRank <= 0.30 {
		activeSignals++
	}
	if !math.IsNaN(turnoverRank) && turnoverRank <= 0.30 {
		activeSignals++
	}
	if !math.IsNaN(breadth) && breadth >= 0.55 {
		activeSignals++
	}
	if board.Percent > 0 && activeSignals >= 2 {
		return "活跃"
	}
	coldRank := (!math.IsNaN(changeRank) && changeRank >= 0.70) || (!math.IsNaN(flowRank) && flowRank >= 0.70)
	weakBreadth := !math.IsNaN(breadth) && breadth <= 0.35
	if board.Percent < 0 && board.MainNet < 0 && (coldRank || weakBreadth) {
		return "偏冷"
	}
	return "一般"
}

func boardHeatCode(label string) string {
	switch label {
	case "热门":
		return "1;31"
	case "热门分歧":
		return "1;33"
	case "活跃":
		return "1;36"
	case "偏冷":
		return "32"
	default:
		return "37"
	}
}

func boardHeatSummary(boards []domain.BoardFlow, color bool) string {
	counts := make(map[string]int)
	for _, board := range boards {
		if label := boardHeatLabel(board); label != "" {
			counts[label]++
		}
	}
	parts := make([]string, 0, len(counts))
	for _, label := range []string{"热门", "热门分歧", "活跃", "一般", "偏冷"} {
		if count := counts[label]; count > 0 {
			parts = append(parts, style(fmt.Sprintf("%s%d", label, count), boardHeatCode(label), color))
		}
	}
	return strings.Join(parts, "  ·  ")
}

func boardHeatLine(board domain.BoardFlow, color bool) string {
	label := boardHeatLabel(board)
	if label == "" {
		return ""
	}
	parts := []string{
		style("热度", "90", color) + " " + style(label, boardHeatCode(label), color),
	}
	if board.ChangeRank > 0 {
		parts = append(parts, fmt.Sprintf("涨幅 %d/%d", board.ChangeRank, board.UniverseSize))
	}
	if board.FlowRank > 0 {
		parts = append(parts, fmt.Sprintf("资金 %d/%d", board.FlowRank, board.UniverseSize))
	}
	if board.TurnoverRank > 0 && !math.IsNaN(board.Turnover) {
		parts = append(parts, fmt.Sprintf("换手 %d/%d(%.2f%%)", board.TurnoverRank, board.UniverseSize, board.Turnover))
	}
	if board.RiseCount+board.FallCount+board.FlatCount > 0 {
		breadth := fmt.Sprintf("涨%d/跌%d", board.RiseCount, board.FallCount)
		if board.FlatCount > 0 {
			breadth += fmt.Sprintf("/平%d", board.FlatCount)
		}
		parts = append(parts, breadth)
	}
	return "      " + strings.Join(parts, "  ")
}

type boardFlowRow struct {
	kind          string
	name          string
	percent       string
	flow          string
	ratio         string
	leader        string
	leaderPercent string
	percentValue  float64
	flowValue     float64
	leaderValue   float64
}

type boardFlowLayout struct {
	kind          int
	name          int
	percent       int
	flow          int
	ratio         int
	leaderLabel   int
	leader        int
	leaderPercent int
	gap           int
}

func (layout boardFlowLayout) width() int {
	return layout.kind + layout.name + layout.percent + layout.flow + layout.ratio +
		layout.leaderLabel + layout.leader + layout.leaderPercent + layout.gap*7
}

func boardFlowRows(boards []domain.BoardFlow) []boardFlowRow {
	rows := make([]boardFlowRow, 0, len(boards))
	for _, board := range boards {
		kind := "概念"
		if board.Kind == domain.BoardKindIndustry {
			kind = "行业"
		}
		flow := domain.FundFlow{MainNet: board.MainNet, MainRatio: board.MainRatio}
		leader := board.LeaderName
		leaderPercent := signedPercent(board.LeaderPercent)
		leaderValue := board.LeaderPercent
		if leader == "" || leader == "-" {
			leader = "--"
			leaderPercent = "--"
			leaderValue = math.NaN()
		}
		rows = append(rows, boardFlowRow{
			kind: kind, name: board.Name, percent: signedPercent(board.Percent),
			flow: directionalFundFlow(&flow), ratio: fundFlowRatio(&flow),
			leader: leader, leaderPercent: leaderPercent,
			percentValue: board.Percent, flowValue: board.MainNet, leaderValue: leaderValue,
		})
	}
	return rows
}

func boardFlowLines(boards []domain.BoardFlow, color bool, availableWidth int) []string {
	rows := boardFlowRows(boards)
	layout := boardFlowLayout{leaderLabel: displayWidth("领涨"), gap: 2}
	for _, row := range rows {
		layout.kind = maxInt(layout.kind, displayWidth(row.kind))
		layout.name = maxInt(layout.name, displayWidth(row.name))
		layout.percent = maxInt(layout.percent, displayWidth(row.percent))
		layout.flow = maxInt(layout.flow, displayWidth(row.flow))
		layout.ratio = maxInt(layout.ratio, displayWidth(row.ratio))
		layout.leader = maxInt(layout.leader, displayWidth(row.leader))
		layout.leaderPercent = maxInt(layout.leaderPercent, displayWidth(row.leaderPercent))
	}
	if layout.name > 16 {
		layout.name = 16
	}
	if layout.leader > 12 {
		layout.leader = 12
	}
	if availableWidth > 0 && layout.width() > availableWidth {
		layout.gap = 1
		for layout.width() > availableWidth && (layout.leader > 4 || layout.name > 4) {
			if layout.leader >= layout.name && layout.leader > 4 {
				layout.leader--
			} else if layout.name > 4 {
				layout.name--
			} else {
				layout.leader--
			}
		}
	}

	gap := strings.Repeat(" ", layout.gap)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		percent := trendValue(row.percent, row.percentValue, color)
		flow := row.flow
		ratio := row.ratio
		if color && !math.IsNaN(row.flowValue) {
			code := trendCode(row.flowValue, false)
			flow = style(flow, code, true)
			ratio = style(ratio, code, true)
		}
		leaderPercent := trendValue(row.leaderPercent, row.leaderValue, color)
		cells := []string{
			padWidth(style(row.kind, "90", color), layout.kind, "left"),
			padWidth(truncateWidth(row.name, layout.name), layout.name, "left"),
			padWidth(percent, layout.percent, "right"),
			padWidth(flow, layout.flow, "right"),
			padWidth(ratio, layout.ratio, "right"),
			padWidth(style("领涨", "90", color), layout.leaderLabel, "left"),
			padWidth(truncateWidth(row.leader, layout.leader), layout.leader, "left"),
			padWidth(leaderPercent, layout.leaderPercent, "right"),
		}
		lines = append(lines, strings.Join(cells, gap))
	}
	return lines
}

func humanYuanAmount(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	absolute := math.Abs(value)
	switch {
	case absolute >= 1e8:
		return fmt.Sprintf("%.2f亿", absolute/1e8)
	case absolute >= 1e4:
		return fmt.Sprintf("%.0f万", absolute/1e4)
	default:
		return fmt.Sprintf("%.0f元", absolute)
	}
}

func dragonTigerDateCount(entries []domain.DragonTigerEntry) int {
	dates := make(map[string]bool)
	for _, entry := range entries {
		dates[entry.TradeDate] = true
	}
	return len(dates)
}

func dragonTigerShortDate(value string) string {
	if len(value) >= 10 {
		return value[5:10]
	}
	return value
}

func dragonTigerFollowUp(entry domain.DragonTigerEntry, color bool) string {
	metrics := []struct {
		label string
		value float64
	}{
		{label: "1日", value: entry.Next1Percent},
		{label: "5日", value: entry.Next5Percent},
		{label: "10日", value: entry.Next10Percent},
	}
	parts := make([]string, 0, len(metrics))
	for _, metric := range metrics {
		if math.IsNaN(metric.value) || math.IsInf(metric.value, 0) {
			continue
		}
		parts = append(parts, metric.label+" "+trendValue(signedPercent(metric.value), metric.value, color))
	}
	return strings.Join(parts, "  ·  ")
}

func dragonTigerLines(snapshot *domain.DragonTigerSnapshot, color bool) []string {
	if snapshot == nil || !snapshot.Loaded {
		return nil
	}
	windowDays := snapshot.WindowDays
	if windowDays <= 0 {
		windowDays = 30
	}
	if len(snapshot.Entries) == 0 {
		return []string{style("龙虎榜", "1;36", color) + fmt.Sprintf("  近%d日无上榜记录", windowDays)}
	}
	lines := []string{
		style("龙虎榜", "1;36", color) + fmt.Sprintf("  近%d日上榜%d日 / %d条  ·  最近 %s", windowDays,
			dragonTigerDateCount(snapshot.Entries), len(snapshot.Entries), dragonTigerShortDate(snapshot.Entries[0].TradeDate)),
	}
	limit := len(snapshot.Entries)
	if limit > 3 {
		limit = 3
	}
	for index := 0; index < limit; index++ {
		entry := snapshot.Entries[index]
		change := trendValue(signedPercent(entry.ChangePercent), entry.ChangePercent, color)
		netFlow := domain.FundFlow{MainNet: entry.NetAmount, MainRatio: entry.NetRatio}
		net := directionalFundFlow(&netFlow) + " " + fundFlowRatio(&netFlow)
		if color && !math.IsNaN(entry.NetAmount) {
			net = style(net, trendCode(entry.NetAmount, false), true)
		}
		lines = append(lines, fmt.Sprintf("%s  %s  %s %s  %s %s  %s %s",
			dragonTigerShortDate(entry.TradeDate), change,
			style("净买入", "90", color), net,
			style("买入", "90", color), humanYuanAmount(entry.BuyAmount),
			style("卖出", "90", color), humanYuanAmount(entry.SellAmount)))
		if entry.Reason != "" {
			lines = append(lines, style("原因", "90", color)+"  "+entry.Reason)
		}
		details := []string{}
		if !math.IsNaN(entry.DealAmountRatio) {
			details = append(details, fmt.Sprintf("榜单成交占比 %.2f%%", entry.DealAmountRatio))
		}
		if !math.IsNaN(entry.Turnover) {
			details = append(details, fmt.Sprintf("换手 %.2f%%", entry.Turnover))
		}
		if entry.SeatSummary != "" {
			details = append(details, "席位标签 "+entry.SeatSummary)
		}
		if len(details) > 0 {
			lines = append(lines, strings.Join(details, "  ·  "))
		}
		if followUp := dragonTigerFollowUp(entry, color); followUp != "" {
			lines = append(lines, style("上榜后", "90", color)+"  "+followUp)
		}
	}
	return lines
}

func wrapPlainText(value string, width int) []string {
	if width <= 0 || displayWidth(value) <= width {
		return []string{value}
	}
	lines := make([]string, 0, 2)
	var current strings.Builder
	currentWidth := 0
	for _, character := range value {
		characterWidth := runeDisplayWidth(character)
		if currentWidth > 0 && currentWidth+characterWidth > width {
			lines = append(lines, current.String())
			current.Reset()
			currentWidth = 0
		}
		current.WriteRune(character)
		currentWidth += characterWidth
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func technicalTextSegments(value string) ([]string, string) {
	if strings.Contains(value, "  ·  ") {
		return strings.Split(value, "  ·  "), "  ·  "
	}
	segments := make([]string, 0, 2)
	start := 0
	runes := []rune(value)
	for index, character := range runes {
		if character == '，' || character == '；' || character == '。' {
			segments = append(segments, string(runes[start:index+1]))
			start = index + 1
		}
	}
	if start < len(runes) {
		segments = append(segments, string(runes[start:]))
	}
	if len(segments) == 0 {
		return []string{value}, ""
	}
	return segments, ""
}

func wrapTechnicalText(value string, width int) []string {
	segments, separator := technicalTextSegments(value)
	lines := make([]string, 0, 2)
	current := ""
	for _, segment := range segments {
		candidate := segment
		if current != "" {
			candidate = current + separator + segment
		}
		if current != "" && displayWidth(candidate) > width {
			lines = append(lines, current)
			current = segment
		} else {
			current = candidate
		}
		if displayWidth(current) > width {
			wrapped := wrapPlainText(current, width)
			lines = append(lines, wrapped[:len(wrapped)-1]...)
			current = wrapped[len(wrapped)-1]
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func labeledTechnicalLines(label, value string, width int, color bool) []string {
	plainPrefix := label + "  "
	available := width - displayWidth(plainPrefix)
	if available < 8 {
		lines := []string{style(label, "90", color)}
		return append(lines, wrapTechnicalText(value, width)...)
	}
	chunks := wrapTechnicalText(value, available)
	lines := make([]string, 0, len(chunks))
	for index, chunk := range chunks {
		prefix := strings.Repeat(" ", displayWidth(plainPrefix))
		if index == 0 {
			prefix = style(label, "90", color) + "  "
		}
		lines = append(lines, prefix+chunk)
	}
	return lines
}

func technicalBarIntraday(item domain.Quote, signal *domain.TechnicalSignal) bool {
	if signal == nil || signal.DataDate == "" || !strings.HasPrefix(item.QuoteTime, signal.DataDate) {
		return false
	}
	clock := quoteClock(item.QuoteTime)
	return clock >= "09:30:00" && clock < "15:00:00"
}

func technicalBiasCode(bias string) string {
	switch bias {
	case "看涨":
		return "1;31"
	case "看跌":
		return "1;32"
	default:
		return "1;33"
	}
}

func technicalContext(flow *domain.FundFlow, boards []domain.BoardFlow, dragonTiger *domain.DragonTigerSnapshot) string {
	parts := make([]string, 0, 3)
	if flow != nil && !math.IsNaN(flow.MainNet) {
		parts = append(parts, "个股主力 "+directionalFundFlow(flow)+" "+fundFlowRatio(flow))
	}
	if len(boards) > 0 {
		parts = append(parts, "关联板块"+boardFlowSummary(boards, nil))
	}
	if dragonTiger != nil && dragonTiger.Loaded {
		if len(dragonTiger.Entries) == 0 {
			parts = append(parts, fmt.Sprintf("近%d日无龙虎榜", dragonTiger.WindowDays))
		} else {
			parts = append(parts, fmt.Sprintf("近%d日龙虎榜上榜%d日", dragonTiger.WindowDays, dragonTigerDateCount(dragonTiger.Entries)))
		}
	}
	return strings.Join(parts, "  ·  ")
}

func technicalSignalLines(signal *domain.TechnicalSignal, flow *domain.FundFlow, boards []domain.BoardFlow, dragonTiger *domain.DragonTigerSnapshot, color, intraday bool, width int) []string {
	if signal == nil {
		return nil
	}
	if signal.Status == domain.TechnicalStatusLoading {
		return []string{style("交易信号", "1;36", color) + "  正在加载未复权日 K…"}
	}
	if signal.Status != domain.TechnicalStatusReady {
		message := "历史日 K 暂不可用"
		if signal.Error != "" {
			message += "：" + signal.Error
		}
		return labeledTechnicalLines("交易信号", message, width, color)
	}

	header := style("交易信号（日线波段）", "1;36", color) + "  " + style(signal.Bias, technicalBiasCode(signal.Bias), color) +
		"  ·  " + style(signal.Action, technicalBiasCode(signal.Bias), color)
	if signal.OptionLike != "" {
		header += "  ·  " + signal.OptionLike
	}
	header += fmt.Sprintf("  ·  强度 %d/100", signal.Strength)
	lines := []string{header}
	lines = append(lines, labeledTechnicalLines("趋势指标", fmt.Sprintf(
		"收盘 %.2f  ·  MA5 %.2f  ·  MA20 %.2f  ·  MA60 %.2f", signal.Price, signal.MA5, signal.MA20, signal.MA60,
	), width, color)...)
	volumeRatio := "--"
	if !math.IsNaN(signal.VolumeRatio) && !math.IsInf(signal.VolumeRatio, 0) {
		volumeRatio = fmt.Sprintf("%.2fx", signal.VolumeRatio)
	}
	lines = append(lines, labeledTechnicalLines("动量量价", fmt.Sprintf(
		"MACD柱 %+.3f  ·  RSI14 %.1f  ·  量能 %s  ·  前20日 %.2f–%.2f",
		signal.MACD, signal.RSI14, volumeRatio, signal.Low20, signal.High20,
	), width, color)...)
	if len(signal.Evidence) > 0 {
		lines = append(lines, labeledTechnicalLines("判断依据", strings.Join(signal.Evidence, "  ·  "), width, color)...)
	}
	lines = append(lines, labeledTechnicalLines("买入条件", signal.BuyTrigger, width, color)...)
	lines = append(lines, labeledTechnicalLines("卖出条件", signal.SellTrigger, width, color)...)
	lines = append(lines, labeledTechnicalLines("失效条件", signal.Invalidation, width, color)...)
	if signal.PositionPlan != "" {
		lines = append(lines, labeledTechnicalLines("仓位策略", signal.PositionPlan, width, color)...)
	}
	lines = append(lines, labeledTechnicalLines("关键位置", "支撑 "+signal.Support+"  ·  压力 "+signal.Resistance, width, color)...)
	if context := technicalContext(flow, boards, dragonTiger); context != "" {
		lines = append(lines, labeledTechnicalLines("短线侧证", context+"（不参与基础信号）", width, color)...)
	}
	dataBasis := signal.DataDate
	if signal.DataSource != "" {
		dataBasis += "  ·  " + signal.DataSource
	}
	dataBasis += "  ·  未复权日 K"
	if intraday {
		dataBasis += "（当日 K 线未收盘）"
	}
	dataBasis += "  ·  技术观察，不是自动交易指令"
	lines = append(lines, labeledTechnicalLines("数据口径", dataBasis, width, color)...)
	if signal.OptionLike != "" {
		lines = append(lines, labeledTechnicalLines("标签说明", "CALL/PUT-like仅作方向映射；PUT-like表示看跌或减仓，不代表普通A股账户可做空", width, color)...)
	}
	return lines
}

func dashboardCard(item domain.Quote, flow *domain.FundFlow, boards []domain.BoardFlow, dragonTiger *domain.DragonTigerSnapshot, technical *domain.TechnicalSignal, options ViewOptions, terminalWidth int) string {
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
	source := strings.TrimSpace(item.Source)
	if source == "" {
		source = "LEVEL-1"
	}
	headerRight := style(source, "1;36", color) + "  " + style(quoteClock(item.QuoteTime), "90", color)
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

	if signalLines := technicalSignalLines(technical, flow, boards, dragonTiger, color, technicalBarIntraday(item, technical), innerWidth); len(signalLines) > 0 {
		lines = append(lines, "\x00separator")
		lines = append(lines, signalLines...)
	}

	if len(boards) > 0 {
		lines = append(lines, "\x00separator")
		lines = append(lines, style("板块资金", "1;36", color)+"  "+boardFlowSummary(boards, flow))
		if heat := boardHeatSummary(boards, color); heat != "" {
			lines = append(lines, style("板块热度", "1;36", color)+"  "+heat)
		}
		flowLines := boardFlowLines(boards, color, innerWidth)
		for index, line := range flowLines {
			lines = append(lines, line)
			if index < len(boards) {
				if heatLine := boardHeatLine(boards[index], color); heatLine != "" {
					lines = append(lines, heatLine)
				}
			}
		}
	}

	if dragonLines := dragonTigerLines(dragonTiger, color); len(dragonLines) > 0 {
		lines = append(lines, "\x00separator")
		lines = append(lines, dragonLines...)
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
		builder.WriteString(dashboardCard(item, nil, nil, nil, nil, options, terminalWidth))
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
		frame := buildMoyuTable(visible, terminalWidth, options.Pinyin)
		if fetchError != "" && len(quotes) == 0 {
			frame += "\n数据暂不可用：" + fetchError
		}
		return frame
	}
	return buildStandardView(visible, options, terminalWidth, refreshed, fetchError)
}
