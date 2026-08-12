package app

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const (
	fundMonitorSampleInterval   = 10 * time.Second
	fundMonitorHistoryWindow    = 6 * time.Minute
	fundMonitorIndustryInterval = time.Minute
)

type fundFlowSample struct {
	at   time.Time
	flow domain.FundFlow
}

type watchFundMonitor struct {
	active              bool
	viewing             bool
	source              string
	rankingKind         domain.MarketRankingKind
	symbols             []string
	samples             map[string][]fundFlowSample
	industries          map[string]domain.BoardFlow
	rows                []domain.FundMovement
	selected            int
	refreshedAt         time.Time
	industryRefreshedAt time.Time
	refreshError        string
	industryError       string
}

func uniqueFundMonitorSymbols(symbols []string) []string {
	seen := make(map[string]bool)
	unique := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		if len(symbol) != 8 || seen[symbol] {
			continue
		}
		seen[symbol] = true
		unique = append(unique, symbol)
	}
	return unique
}

func (monitor *watchFundMonitor) begin(source string, symbols []string) {
	*monitor = watchFundMonitor{
		active:     true,
		viewing:    true,
		source:     source,
		symbols:    uniqueFundMonitorSymbols(symbols),
		samples:    make(map[string][]fundFlowSample),
		industries: make(map[string]domain.BoardFlow),
	}
}

func (monitor *watchFundMonitor) beginHidden(source string, symbols []string) {
	monitor.begin(source, symbols)
	monitor.viewing = false
}

func (monitor *watchFundMonitor) beginRanking(kind domain.MarketRankingKind, source string, symbols []string) {
	monitor.begin(source, symbols)
	monitor.rankingKind = kind
}

func (monitor watchFundMonitor) matches(kind domain.MarketRankingKind, source string, symbols []string) bool {
	if !monitor.active || monitor.rankingKind != kind || monitor.source != source {
		return false
	}
	expected := uniqueFundMonitorSymbols(symbols)
	if len(expected) != len(monitor.symbols) {
		return false
	}
	seen := make(map[string]bool, len(monitor.symbols))
	for _, symbol := range monitor.symbols {
		seen[symbol] = true
	}
	for _, symbol := range expected {
		if !seen[symbol] {
			return false
		}
	}
	return true
}

func (monitor *watchFundMonitor) syncSymbols(symbols []string) bool {
	if !monitor.active {
		return false
	}
	next := uniqueFundMonitorSymbols(symbols)
	if len(next) == len(monitor.symbols) {
		current := make(map[string]bool, len(monitor.symbols))
		for _, symbol := range monitor.symbols {
			current[symbol] = true
		}
		sameMembers := true
		for _, symbol := range next {
			if !current[symbol] {
				sameMembers = false
				break
			}
		}
		if sameMembers {
			monitor.symbols = next
			return false
		}
	}
	monitor.symbols = next
	monitor.rebuildRows()
	return true
}

func (monitor *watchFundMonitor) reset() {
	*monitor = watchFundMonitor{}
}

func fillFundFlowContext(current, previous domain.FundFlow) domain.FundFlow {
	if current.Name == "" {
		current.Name = previous.Name
	}
	if current.Industry == "" {
		current.Industry = previous.Industry
	}
	if math.IsNaN(current.Price) {
		current.Price = previous.Price
	}
	if math.IsNaN(current.Percent) {
		current.Percent = previous.Percent
	}
	if math.IsNaN(current.Speed) {
		current.Speed = previous.Speed
	}
	return current
}

func (monitor *watchFundMonitor) record(at time.Time, flows map[string]domain.FundFlow) {
	if !monitor.active {
		return
	}
	cutoff := at.Add(-fundMonitorHistoryWindow)
	for symbol, history := range monitor.samples {
		first := 0
		for first < len(history) && history[first].at.Before(cutoff) {
			first++
		}
		if first >= len(history) {
			delete(monitor.samples, symbol)
			continue
		}
		monitor.samples[symbol] = append([]fundFlowSample(nil), history[first:]...)
	}
	for _, symbol := range monitor.symbols {
		flow, ok := flows[symbol]
		if !ok || math.IsNaN(flow.MainNet) {
			continue
		}
		history := monitor.samples[symbol]
		if len(history) > 0 {
			flow = fillFundFlowContext(flow, history[len(history)-1].flow)
		}
		history = append(history, fundFlowSample{at: at, flow: flow})
		first := 0
		for first < len(history) && history[first].at.Before(cutoff) {
			first++
		}
		monitor.samples[symbol] = append([]fundFlowSample(nil), history[first:]...)
	}
	monitor.refreshedAt = at
	monitor.refreshError = ""
	monitor.rebuildRows()
}

func (monitor *watchFundMonitor) setIndustries(at time.Time, industries map[string]domain.BoardFlow) {
	if !monitor.active {
		return
	}
	monitor.industries = make(map[string]domain.BoardFlow, len(industries))
	for name, flow := range industries {
		monitor.industries[name] = flow
	}
	monitor.industryRefreshedAt = at
	monitor.industryError = ""
	monitor.rebuildRows()
}

func (monitor *watchFundMonitor) failRefresh(err error) {
	if monitor.active && err != nil {
		monitor.refreshError = err.Error()
	}
}

func (monitor *watchFundMonitor) failIndustryRefresh(err error) {
	if monitor.active && err != nil {
		monitor.industryError = err.Error()
	}
}

func (monitor watchFundMonitor) hasValidSample(flows map[string]domain.FundFlow) bool {
	for _, symbol := range monitor.symbols {
		if flow, ok := flows[symbol]; ok && !math.IsNaN(flow.MainNet) && !math.IsInf(flow.MainNet, 0) {
			return true
		}
	}
	return false
}

func sampleAtOrBefore(samples []fundFlowSample, target time.Time) (fundFlowSample, bool) {
	for index := len(samples) - 1; index >= 0; index-- {
		if !samples[index].at.After(target) {
			return samples[index], true
		}
	}
	return fundFlowSample{}, false
}

func fundSampleDelta(samples []fundFlowSample, window time.Duration, value func(domain.FundFlow) float64) float64 {
	if len(samples) == 0 {
		return math.NaN()
	}
	latest := samples[len(samples)-1]
	previous, ok := sampleAtOrBefore(samples, latest.at.Add(-window))
	if !ok {
		return math.NaN()
	}
	left, right := value(latest.flow), value(previous.flow)
	if math.IsNaN(left) || math.IsNaN(right) {
		return math.NaN()
	}
	return left - right
}

func previousFundMinuteDelta(samples []fundFlowSample) float64 {
	if len(samples) == 0 {
		return math.NaN()
	}
	latest := samples[len(samples)-1]
	oneMinuteAgo, ok := sampleAtOrBefore(samples, latest.at.Add(-time.Minute))
	if !ok {
		return math.NaN()
	}
	twoMinutesAgo, ok := sampleAtOrBefore(samples, latest.at.Add(-2*time.Minute))
	if !ok {
		return math.NaN()
	}
	return oneMinuteAgo.flow.MainNet - twoMinutesAgo.flow.MainNet
}

func fundPriceChange(samples []fundFlowSample, window time.Duration) float64 {
	if len(samples) == 0 {
		return math.NaN()
	}
	latest := samples[len(samples)-1]
	previous, ok := sampleAtOrBefore(samples, latest.at.Add(-window))
	if !ok || previous.flow.Price <= 0 || math.IsNaN(previous.flow.Price) || math.IsNaN(latest.flow.Price) {
		return math.NaN()
	}
	return (latest.flow.Price/previous.flow.Price - 1) * 100
}

func fundMovementDirection(netDelta, ratioDelta float64) int {
	positive := (!math.IsNaN(netDelta) && netDelta >= 1e7) || (!math.IsNaN(ratioDelta) && ratioDelta >= 0.8)
	negative := (!math.IsNaN(netDelta) && netDelta <= -1e7) || (!math.IsNaN(ratioDelta) && ratioDelta <= -0.8)
	if positive && !negative {
		return 1
	}
	if negative && !positive {
		return -1
	}
	return 0
}

func classifyFundMovement(current domain.FundFlow, delta1, delta3, previous1, ratioDelta1, priceChange1 float64, industry domain.BoardFlow) string {
	direction := fundMovementDirection(delta1, ratioDelta1)
	if direction == 0 && math.IsNaN(delta1) {
		return "采样中"
	}
	previousDirection := fundMovementDirection(previous1, math.NaN())
	if direction > 0 && previousDirection < 0 {
		return "流出转回流"
	}
	if direction < 0 && previousDirection > 0 {
		return "流入转流出"
	}
	if direction > 0 && industry.MainNet > 0 && industry.Percent > 0 {
		return "个股板块共振"
	}
	if direction < 0 && industry.MainNet < 0 && industry.Percent < 0 {
		return "板块共振流出"
	}
	if direction < 0 && !math.IsNaN(priceChange1) && priceChange1 >= 0.2 {
		return "价涨资出"
	}
	if direction > 0 && !math.IsNaN(priceChange1) && priceChange1 <= -0.2 {
		return "流入未涨"
	}
	if direction > 0 && !math.IsNaN(delta3) && delta3 > 0 && delta1 > delta3/3*1.5 {
		return "加速流入"
	}
	if direction < 0 && !math.IsNaN(delta3) && delta3 < 0 && math.Abs(delta1) > math.Abs(delta3/3)*1.5 {
		return "加速流出"
	}
	if direction > 0 && !math.IsNaN(delta3) && delta3 > 0 {
		return "持续流入"
	}
	if direction < 0 && !math.IsNaN(delta3) && delta3 < 0 {
		return "持续流出"
	}
	if direction > 0 {
		return "资金回流"
	}
	if direction < 0 {
		return "资金流出"
	}
	return "方向不明"
}

func trimIndustryLevel(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "ⅠⅡⅢIV")
}

func (monitor watchFundMonitor) industryFlow(name string) domain.BoardFlow {
	if flow, ok := monitor.industries[name]; ok {
		return flow
	}
	trimmed := trimIndustryLevel(name)
	for candidate, flow := range monitor.industries {
		if trimIndustryLevel(candidate) == trimmed {
			return flow
		}
	}
	return domain.BoardFlow{MainNet: math.NaN(), Percent: math.NaN(), MainRatio: math.NaN()}
}

func sortFundMovementsByOneMinuteFlow(rows []domain.FundMovement) {
	sort.SliceStable(rows, func(left, right int) bool {
		leftFlow := rows[left].Delta1Minute
		rightFlow := rows[right].Delta1Minute
		leftValid := !math.IsNaN(leftFlow) && !math.IsInf(leftFlow, 0)
		rightValid := !math.IsNaN(rightFlow) && !math.IsInf(rightFlow, 0)
		if leftValid != rightValid {
			return leftValid
		}
		if leftValid && leftFlow != rightFlow {
			return leftFlow > rightFlow
		}
		return false
	})
}

func (monitor *watchFundMonitor) rebuildRows() {
	selectedSymbol := ""
	if item, ok := monitor.selectedItem(); ok {
		selectedSymbol = item.Symbol
	}
	rows := make([]domain.FundMovement, 0, len(monitor.symbols))
	for _, symbol := range monitor.symbols {
		samples := monitor.samples[symbol]
		if len(samples) == 0 {
			continue
		}
		current := samples[len(samples)-1].flow
		delta1 := fundSampleDelta(samples, time.Minute, func(flow domain.FundFlow) float64 { return flow.MainNet })
		delta3 := fundSampleDelta(samples, 3*time.Minute, func(flow domain.FundFlow) float64 { return flow.MainNet })
		delta5 := fundSampleDelta(samples, 5*time.Minute, func(flow domain.FundFlow) float64 { return flow.MainNet })
		ratioDelta1 := fundSampleDelta(samples, time.Minute, func(flow domain.FundFlow) float64 { return flow.MainRatio })
		industry := monitor.industryFlow(current.Industry)
		rows = append(rows, domain.FundMovement{
			Symbol: symbol, Name: current.Name, Industry: current.Industry,
			Price: current.Price, Percent: current.Percent, MainNet: current.MainNet, MainRatio: current.MainRatio,
			Delta1Minute: delta1, Delta3Minutes: delta3, Delta5Minutes: delta5,
			IndustryNet: industry.MainNet, IndustryPercent: industry.Percent,
			State: classifyFundMovement(
				current, delta1, delta3, previousFundMinuteDelta(samples), ratioDelta1,
				fundPriceChange(samples, time.Minute), industry,
			),
		})
	}
	sortFundMovementsByOneMinuteFlow(rows)
	monitor.rows = rows
	monitor.selected = 0
	for index, item := range rows {
		if item.Symbol == selectedSymbol {
			monitor.selected = index
			break
		}
	}
}

func (monitor *watchFundMonitor) displayRows(quotes []domain.Quote, rankings []domain.MarketRankingItem) []domain.FundMovement {
	selectedSymbol := ""
	if item, ok := monitor.selectedItem(); ok {
		selectedSymbol = item.Symbol
	}
	result := append([]domain.FundMovement(nil), monitor.rows...)
	quoteBySymbol := make(map[string]domain.Quote, len(quotes))
	for _, quote := range quotes {
		quoteBySymbol[quote.Symbol] = quote
	}
	rankingBySymbol := make(map[string]domain.MarketRankingItem, len(rankings))
	for _, item := range rankings {
		rankingBySymbol[item.Symbol] = item
	}
	for index := range result {
		if quote, ok := quoteBySymbol[result[index].Symbol]; ok {
			if price, err := strconv.ParseFloat(quote.Current, 64); err == nil {
				result[index].Price = price
			}
			result[index].Percent = quote.Percent
			if quote.Name != "" {
				result[index].Name = quote.Name
			}
			continue
		}
		if item, ok := rankingBySymbol[result[index].Symbol]; ok {
			result[index].Price = item.Price
			result[index].Percent = item.Percent
			if item.Name != "" {
				result[index].Name = item.Name
			}
			if result[index].Industry == "" {
				result[index].Industry = item.Industry
			}
		}
	}
	sortFundMovementsByOneMinuteFlow(result)
	monitor.rows = append([]domain.FundMovement(nil), result...)
	monitor.selected = 0
	for index, item := range monitor.rows {
		if item.Symbol == selectedSymbol {
			monitor.selected = index
			break
		}
	}
	return result
}

func (monitor *watchFundMonitor) move(delta int) {
	if !monitor.active || len(monitor.rows) == 0 {
		return
	}
	monitor.selected += delta
	if monitor.selected < 0 {
		monitor.selected = 0
	}
	if monitor.selected >= len(monitor.rows) {
		monitor.selected = len(monitor.rows) - 1
	}
}

func (monitor *watchFundMonitor) selectIndex(index int) {
	if !monitor.active || len(monitor.rows) == 0 {
		return
	}
	monitor.selected = index
	monitor.move(0)
}

func (monitor watchFundMonitor) selectedItem() (domain.FundMovement, bool) {
	if !monitor.active || monitor.selected < 0 || monitor.selected >= len(monitor.rows) {
		return domain.FundMovement{}, false
	}
	return monitor.rows[monitor.selected], true
}

func (monitor watchFundMonitor) movementFor(symbol string) (domain.FundMovement, bool) {
	for _, item := range monitor.rows {
		if item.Symbol == symbol {
			return item, true
		}
	}
	return domain.FundMovement{}, false
}

func (monitor watchFundMonitor) controls(moyu bool) string {
	if moyu {
		return "UP/DOWN SELECT  [/] JUMP  ENTER DETAIL  V REFRESH  ESC BACK  Q QUIT\nY BOARD FUNDS  X ASK/OPEN AI  C STOCK REPORT  O OPEN  S MARKET REPORT  R OPEN"
	}
	return "↑/↓选择  [/]跳选  Enter详情  v刷新  Esc返回  q退出\ny板块资金  x咨询AI  c个股研判  o查看  s市场报告  r查看"
}

func (monitor watchFundMonitor) status(moyu bool) string {
	if monitor.refreshedAt.IsZero() && monitor.refreshError == "" {
		if moyu {
			return "LOADING FIRST FUND SNAPSHOT"
		}
		return "正在获取首个资金样本"
	}
	messages := make([]string, 0, 2)
	if monitor.refreshError != "" {
		if moyu && monitor.refreshedAt.IsZero() {
			messages = append(messages, "FUND SNAPSHOT FAILED: "+monitor.refreshError)
		} else if moyu {
			messages = append(messages, fmt.Sprintf("FLOW REFRESH FAILED; KEEPING %s: %s", monitor.refreshedAt.Format("15:04:05"), monitor.refreshError))
		} else if monitor.refreshedAt.IsZero() {
			messages = append(messages, "资金样本加载失败: "+monitor.refreshError)
		} else {
			messages = append(messages, fmt.Sprintf("资金刷新失败，保留 %s 数据: %s", monitor.refreshedAt.Format("15:04:05"), monitor.refreshError))
		}
	}
	if monitor.industryError != "" {
		if moyu && monitor.industryRefreshedAt.IsZero() {
			messages = append(messages, "INDUSTRY FLOW FAILED: "+monitor.industryError)
		} else if moyu {
			messages = append(messages, "INDUSTRY FLOW REFRESH FAILED: "+monitor.industryError)
		} else if monitor.industryRefreshedAt.IsZero() {
			messages = append(messages, "行业资金加载失败: "+monitor.industryError)
		} else {
			messages = append(messages, "行业资金刷新失败，保留上一份数据: "+monitor.industryError)
		}
	}
	return strings.Join(messages, "\n")
}

func fundMonitorRankingSource(kind domain.MarketRankingKind) string {
	switch kind {
	case domain.MarketRankingLosers:
		return "跌幅榜前20"
	case domain.MarketRankingRapidRise:
		return "快速涨幅榜前20"
	default:
		return "涨幅榜前20"
	}
}

func marketRankingSymbols(items []domain.MarketRankingItem) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Symbol)
	}
	return result
}
