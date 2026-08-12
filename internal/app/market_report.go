package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
)

const (
	marketReportCandidateLimit = 10
	marketReportHistoryLimit   = 18
)

type marketReportProgress func(string)

func reportProgress(callback marketReportProgress, value string) {
	if callback != nil {
		callback(value)
	}
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func averageLast(values []float64, count int) float64 {
	if count <= 0 || len(values) < count {
		return math.NaN()
	}
	total := 0.0
	for _, value := range values[len(values)-count:] {
		total += value
	}
	return total / float64(count)
}

func scanTechnical(bars []domain.DailyBar) (domain.MarketTechnicalSnapshot, bool) {
	if len(bars) < 61 {
		return domain.MarketTechnicalSnapshot{}, false
	}
	latest := bars[len(bars)-1]
	closes := make([]float64, len(bars))
	volumes := make([]float64, len(bars))
	for index, bar := range bars {
		closes[index] = bar.Close
		volumes[index] = bar.Volume
	}
	priorVolume20 := averageLast(volumes[:len(volumes)-1], 20)
	volumeRatio := math.NaN()
	if priorVolume20 > 0 {
		volumeRatio = latest.Volume / priorVolume20
	}
	prior := bars[len(bars)-21 : len(bars)-1]
	priorHigh, priorLow := prior[0].High, prior[0].Low
	for _, bar := range prior[1:] {
		priorHigh = math.Max(priorHigh, bar.High)
		priorLow = math.Min(priorLow, bar.Low)
	}
	closePosition := 50.0
	if latest.High > latest.Low {
		closePosition = (latest.Close - latest.Low) / (latest.High - latest.Low) * 100
	}
	result := domain.MarketTechnicalSnapshot{
		DataDate: latest.Date, Close: latest.Close,
		Return5:  (latest.Close/closes[len(closes)-6] - 1) * 100,
		Return20: (latest.Close/closes[len(closes)-21] - 1) * 100,
		Return60: (latest.Close/closes[len(closes)-61] - 1) * 100,
		MA5:      averageLast(closes, 5), MA20: averageLast(closes, 20), MA60: averageLast(closes, 60),
		VolumeRatio20: volumeRatio, ClosePosition: closePosition, Prior20High: priorHigh, Prior20Low: priorLow,
	}
	switch {
	case result.Close > result.MA5 && result.Close > result.MA20 && result.Close > result.MA60:
		result.Trend = "多头排列"
	case result.Close > result.MA5 && result.Close > result.MA20:
		result.Trend = "短线反弹，中期待确认"
	case result.Close < result.MA5 && result.Close < result.MA20:
		result.Trend = "短中期偏弱"
	default:
		result.Trend = "震荡分化"
	}
	return result, true
}

func normalizeScanIndustry(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, "ⅠⅡⅢIV")
	return strings.TrimSuffix(value, "设备")
}

func boardHeatScore(board domain.BoardFlow) float64 {
	total := board.RiseCount + board.FallCount + board.FlatCount
	if total < 4 || !finite(board.Percent) || !finite(board.MainNet) || board.Percent <= 0 || board.MainNet <= 0 || board.RiseCount <= board.FallCount {
		return math.Inf(-1)
	}
	breadth := float64(board.RiseCount-board.FallCount) / float64(total)
	score := board.Percent*1.8 + math.Min(board.MainNet/1e9, 3) + breadth*3
	if finite(board.Turnover) {
		score += math.Min(board.Turnover/5, 1.5)
		if board.Turnover > 15 {
			score -= 1
		}
	}
	return score
}

func selectHotBoards(lists ...[]domain.BoardFlow) []domain.MarketBoardAssessment {
	merged := make(map[string]domain.BoardFlow)
	for _, list := range lists {
		for _, board := range list {
			merged[board.Code] = board
		}
	}
	items := make([]domain.MarketBoardAssessment, 0, len(merged))
	for _, board := range merged {
		score := boardHeatScore(board)
		if math.IsInf(score, -1) {
			continue
		}
		items = append(items, domain.MarketBoardAssessment{BoardFlow: board, Score: score, FlowAvailable: true})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Score > items[j].Score })
	seen := make(map[string]bool)
	result := make([]domain.MarketBoardAssessment, 0, 5)
	for _, item := range items {
		key := normalizeScanIndustry(item.Name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
		if len(result) == 5 {
			break
		}
	}
	return result
}

func selectMomentumBoards(items []domain.BoardFlow) []domain.MarketBoardAssessment {
	assessments := make([]domain.MarketBoardAssessment, 0, len(items))
	for _, board := range items {
		total := board.RiseCount + board.FallCount + board.FlatCount
		if !finite(board.Percent) || board.Percent <= 0 {
			continue
		}
		if total > 0 && board.RiseCount <= board.FallCount {
			continue
		}
		breadth := 0.0
		if total > 0 {
			breadth = float64(board.RiseCount-board.FallCount) / float64(total)
		}
		score := board.Percent*2 + breadth*3
		if finite(board.Turnover) {
			score += math.Min(board.Turnover/5, 1.5)
		}
		assessments = append(assessments, domain.MarketBoardAssessment{
			BoardFlow: board, Score: score, FlowAvailable: finite(board.MainNet),
		})
	}
	sort.SliceStable(assessments, func(i, j int) bool { return assessments[i].Score > assessments[j].Score })
	seen := make(map[string]bool)
	result := make([]domain.MarketBoardAssessment, 0, 5)
	for _, item := range assessments {
		key := normalizeScanIndustry(item.Name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
		if len(result) == 5 {
			break
		}
	}
	return result
}

func selectWeakBoards(items []domain.BoardFlow) []domain.MarketBoardAssessment {
	result := make([]domain.MarketBoardAssessment, 0, 5)
	seen := make(map[string]bool)
	for _, board := range items {
		if len(result) == 5 {
			break
		}
		if !finite(board.Percent) || !finite(board.MainNet) || board.Percent >= 0 || board.MainNet >= 0 {
			continue
		}
		key := normalizeScanIndustry(board.Name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, domain.MarketBoardAssessment{
			BoardFlow: board, Score: math.Abs(board.Percent) + math.Min(math.Abs(board.MainNet)/1e9, 5),
		})
	}
	return result
}

func stockFromLeaderCode(code string) string {
	if len(code) != 6 {
		return ""
	}
	switch code[0] {
	case '6':
		return "sh" + code
	case '0', '3':
		return "sz" + code
	default:
		return ""
	}
}

func mergeStock(target domain.MarketStockSnapshot, source domain.MarketStockSnapshot) domain.MarketStockSnapshot {
	if target.Symbol == "" {
		return source
	}
	if source.Name != "" {
		target.Name = source.Name
	}
	if source.Industry != "" {
		target.Industry = source.Industry
	}
	values := []struct {
		from float64
		to   *float64
	}{
		{source.Price, &target.Price}, {source.Percent, &target.Percent}, {source.Amount, &target.Amount},
		{source.Turnover, &target.Turnover}, {source.VolumeRatio, &target.VolumeRatio}, {source.Speed, &target.Speed},
		{source.High, &target.High}, {source.Low, &target.Low}, {source.Open, &target.Open},
		{source.PreviousClose, &target.PreviousClose}, {source.MarketCap, &target.MarketCap},
		{source.MainNet, &target.MainNet}, {source.MainRatio, &target.MainRatio},
	}
	for _, value := range values {
		if finite(value.from) {
			*value.to = value.from
		}
	}
	return target
}

func riskyScanName(value string) bool {
	upper := strings.ToUpper(strings.TrimSpace(value))
	return strings.Contains(upper, "ST") || strings.HasPrefix(upper, "N") || strings.Contains(value, "退")
}

func preliminaryStockScore(stock domain.MarketStockSnapshot, leader bool, amountRank, flowRank int, relaxed bool) float64 {
	minimumAmount := 1e8
	if relaxed {
		minimumAmount = 5e7
	}
	if riskyScanName(stock.Name) || !finite(stock.Price) || stock.Price <= 0 || !finite(stock.Percent) ||
		!finite(stock.Amount) || stock.Amount < minimumAmount {
		return math.Inf(-1)
	}
	if !relaxed && (stock.Percent <= 0 || !finite(stock.MainNet) || stock.MainNet <= 0) {
		return math.Inf(-1)
	}
	score := math.Max(-5, math.Min(stock.Percent, 10))*0.35 + math.Log10(stock.Amount/1e8+1)*1.6
	if finite(stock.MainNet) {
		score += math.Max(-5, math.Min(stock.MainNet/1e8, 20)) * 0.12
	}
	if finite(stock.MainRatio) {
		score += math.Max(-2, math.Min(stock.MainRatio/5, 3))
	}
	if finite(stock.VolumeRatio) && stock.VolumeRatio > 1 {
		score += math.Min(stock.VolumeRatio-1, 2)
	}
	if leader {
		score += 6
	}
	if amountRank > 0 && amountRank <= 30 {
		score += 2 - float64(amountRank-1)/30
	}
	if flowRank > 0 && flowRank <= 30 {
		score += 2 - float64(flowRank-1)/30
	}
	if finite(stock.Turnover) && stock.Turnover > 25 {
		score -= 2
	}
	if stock.Percent >= 9.8 {
		// Near-limit-up names are useful for sentiment context, but should not
		// dominate a next-day observation pool merely because they are strong.
		score -= 3
	}
	if relaxed && stock.Percent < -3 && !leader {
		score -= 3
	}
	return score
}

func applyTechnicalScore(candidate *domain.MarketCandidateAssessment) {
	technical := candidate.Technical
	if technical.DataDate == "" {
		candidate.Risks = append(candidate.Risks, "日线数据不足，技术趋势未计分")
		return
	}
	if technical.Close > technical.MA5 {
		candidate.Score += 1
	}
	if technical.Close > technical.MA20 {
		candidate.Score += 1.5
	} else {
		candidate.Risks = append(candidate.Risks, "仍低于MA20，当前更像反弹")
	}
	if technical.Close > technical.MA60 {
		candidate.Score += 2
	} else {
		candidate.Risks = append(candidate.Risks, "仍低于MA60，中期趋势尚未扭转")
	}
	if technical.Return20 > 0 {
		candidate.Score += math.Min(technical.Return20/10, 2)
	}
	if technical.VolumeRatio20 > 1 {
		candidate.Score += math.Min(technical.VolumeRatio20-1, 1.5)
	}
	if technical.ClosePosition >= 70 {
		candidate.Score += 0.5
	} else if technical.ClosePosition <= 30 {
		candidate.Risks = append(candidate.Risks, "收盘靠近日内低位，承接偏弱")
	}
	if technical.Return60 > 80 {
		candidate.Score -= 2
		candidate.Risks = append(candidate.Risks, "60日累计涨幅过高，注意拥挤交易")
	}
	if candidate.Stock.Percent >= 9.8 {
		if candidate.BoardLeader && candidate.Stock.MainNet > 0 && technical.Close > technical.MA20 &&
			technical.Close > technical.MA60 && technical.VolumeRatio20 >= 1 {
			candidate.Score += 2
			candidate.Reasons = append(candidate.Reasons, "板块龙头且资金、均线和量能同步强势，仍不宜追高")
		} else {
			candidate.Risks = append(candidate.Risks, "接近涨停但未满足龙头资金与中期结构共振，追高惩罚加重")
		}
	}
}

func classifyMarketCandidate(candidate domain.MarketCandidateAssessment) (string, string) {
	if candidate.BoardLeader {
		return "A", "板块龙头/核心承接"
	}
	technical := candidate.Technical
	if technical.DataDate != "" && (technical.Close < technical.MA20 || technical.Close < technical.MA60 || technical.Return60 < 0) {
		return "C", "超跌反转观察"
	}
	return "B", "高波动情绪候选"
}

func marketCandidateIndustryKey(candidate domain.MarketCandidateAssessment) string {
	key := normalizeScanIndustry(candidate.Stock.Industry)
	if key == "" {
		return candidate.Stock.Symbol
	}
	return key
}

func selectMarketCandidates(candidates []domain.MarketCandidateAssessment, limit int) []domain.MarketCandidateAssessment {
	if limit <= 0 {
		return nil
	}
	ordered := append([]domain.MarketCandidateAssessment(nil), candidates...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Score > ordered[j].Score })
	selected := make([]domain.MarketCandidateAssessment, 0, min(limit, len(ordered)))
	selectedSymbols := make(map[string]bool, limit)
	industryCounts := make(map[string]int)
	appendCandidate := func(candidate domain.MarketCandidateAssessment) bool {
		if len(selected) >= limit || selectedSymbols[candidate.Stock.Symbol] {
			return false
		}
		industry := marketCandidateIndustryKey(candidate)
		if industryCounts[industry] >= 2 {
			return false
		}
		selectedSymbols[candidate.Stock.Symbol] = true
		industryCounts[industry]++
		selected = append(selected, candidate)
		return true
	}
	// Keep every available hot-board leader in its own seat before filling
	// the remaining slots from the scored pool.
	for _, candidate := range ordered {
		if candidate.Grade == "A" {
			appendCandidate(candidate)
		}
	}
	// A small oversold bucket prevents a report made entirely of chase names.
	oversoldCount := 0
	for _, candidate := range ordered {
		if len(selected) >= limit || oversoldCount >= 2 {
			break
		}
		if candidate.Grade == "C" {
			if appendCandidate(candidate) {
				oversoldCount++
			}
		}
	}
	for _, candidate := range ordered {
		if len(selected) >= limit {
			break
		}
		appendCandidate(candidate)
	}
	return selected
}

var positiveAnnouncementTerms = []string{"回购", "业绩预增", "分红", "员工持股", "激励计划"}
var riskAnnouncementTerms = []string{"减持", "问询", "更正", "延期", "风险提示", "异常波动", "质押", "担保", "H股"}

func attachAnnouncements(candidate *domain.MarketCandidateAssessment, items []domain.MarketAnnouncement) {
	for _, item := range items {
		if item.Symbol != candidate.Stock.Symbol || len(candidate.Announcements) >= 3 {
			continue
		}
		candidate.Announcements = append(candidate.Announcements, item)
		for _, term := range positiveAnnouncementTerms {
			if strings.Contains(item.Title, term) {
				candidate.Score += 0.4
				candidate.Reasons = append(candidate.Reasons, item.Date+"公告线索: "+item.Title)
				break
			}
		}
		for _, term := range riskAnnouncementTerms {
			if strings.Contains(item.Title, term) {
				candidate.Score -= 0.7
				candidate.Risks = append(candidate.Risks, item.Date+"公告待核: "+item.Title)
				break
			}
		}
	}
}

func marketAmountFromQuotes(quotes []domain.Quote) float64 {
	values := make(map[string]float64, len(quotes))
	for _, quote := range quotes {
		values[quote.Symbol] = quote.Amount
	}
	shanghai := values["sh000001"]
	shenzhen := values["sz399106"]
	if shenzhen <= 0 {
		shenzhen = values["sz399001"]
	}
	beijing := values["bj899050"]
	if shanghai <= 0 || shenzhen <= 0 {
		return 0
	}
	return shanghai + shenzhen + math.Max(beijing, 0)
}

type scanFetchResult struct {
	kind   string
	boards []domain.BoardFlow
	stocks []domain.MarketStockSnapshot
	quotes []domain.Quote
	flows  map[string]domain.FundFlow
	amount domain.MarketAmountSnapshot
	err    error
}

func (app *App) collectMarketScanFacts(ctx context.Context, progress marketReportProgress) (domain.MarketScanFacts, error) {
	if app.marketScan == nil || app.quotes == nil || app.scanHistory == nil {
		return domain.MarketScanFacts{}, fmt.Errorf("智能市场扫描依赖未初始化")
	}
	reportProgress(progress, "采集大盘、板块与全市场资金")
	results := make(chan scanFetchResult, 9)
	go func() {
		items, err := app.marketScan.FetchIndustryRanking(ctx, domain.MarketScanByPercent, true, 100)
		results <- scanFetchResult{kind: "boards_up", boards: items, err: err}
	}()
	go func() {
		items, err := app.marketScan.FetchIndustryRanking(ctx, domain.MarketScanByPercent, false, 30)
		results <- scanFetchResult{kind: "boards_down", boards: items, err: err}
	}()
	go func() {
		items, err := app.marketScan.FetchIndustryRanking(ctx, domain.MarketScanByMainNet, true, 100)
		results <- scanFetchResult{kind: "boards_flow", boards: items, err: err}
	}()
	go func() {
		items, err := app.marketScan.FetchStockRanking(ctx, domain.MarketScanByAmount, true, 100)
		results <- scanFetchResult{kind: "stocks_amount", stocks: items, err: err}
	}()
	go func() {
		items, err := app.marketScan.FetchStockRanking(ctx, domain.MarketScanByMainNet, true, 100)
		results <- scanFetchResult{kind: "stocks_flow", stocks: items, err: err}
	}()
	go func() {
		items, err := app.marketScan.FetchStockRanking(ctx, domain.MarketScanByPercent, true, 50)
		results <- scanFetchResult{kind: "stocks_up", stocks: items, err: err}
	}()
	go func() {
		items, err := app.quotes.Fetch(ctx, market.QuoteMarketSymbols)
		results <- scanFetchResult{kind: "quotes", quotes: items, err: err}
	}()
	go func() {
		items := map[string]domain.FundFlow{}
		var err error
		if app.flows != nil {
			items, err = app.flows.Fetch(ctx, market.BroadMarketSymbols)
		}
		results <- scanFetchResult{kind: "index_flows", flows: items, err: err}
	}()
	go func() {
		var item domain.MarketAmountSnapshot
		var err error
		if app.amounts != nil {
			item, err = app.amounts.FetchPreviousMarketAmount(ctx)
		}
		results <- scanFetchResult{kind: "previous_amount", amount: item, err: err}
	}()

	var boardsUp, boardsDown, boardsFlow []domain.BoardFlow
	var stocksAmount, stocksFlow, stocksUp []domain.MarketStockSnapshot
	var quotes []domain.Quote
	indexFlows := map[string]domain.FundFlow{}
	previousAmount := domain.MarketAmountSnapshot{}
	warnings := make([]string, 0)
	for range 9 {
		result := <-results
		if result.err != nil {
			warnings = append(warnings, result.kind+": "+result.err.Error())
		}
		switch result.kind {
		case "boards_up":
			boardsUp = result.boards
		case "boards_down":
			boardsDown = result.boards
		case "boards_flow":
			boardsFlow = result.boards
		case "stocks_amount":
			stocksAmount = result.stocks
		case "stocks_flow":
			stocksFlow = result.stocks
		case "stocks_up":
			stocksUp = result.stocks
		case "quotes":
			quotes = result.quotes
		case "index_flows":
			indexFlows = result.flows
		case "previous_amount":
			previousAmount = result.amount
		}
	}
	if len(boardsUp) == 0 || len(stocksAmount) == 0 {
		return domain.MarketScanFacts{}, fmt.Errorf("市场扫描基础数据不足: %s", strings.Join(warnings, "；"))
	}

	facts := domain.MarketScanFacts{
		SchemaVersion: 1, GeneratedAt: time.Now(), MarketStatus: marketSessionAt(time.Now()).Label,
		HotBoards: selectHotBoards(boardsUp, boardsFlow), WeakBoards: selectWeakBoards(boardsDown), Warnings: warnings,
	}
	strictIntraday := marketSessionAt(facts.GeneratedAt).Continuous
	if len(facts.HotBoards) == 0 {
		facts.HotBoards = selectMomentumBoards(boardsUp)
		if len(facts.HotBoards) > 0 {
			facts.Warnings = append(facts.Warnings, "行业主力资金暂不可用，热门板块按涨幅与广度降级筛选")
		}
	}
	if !strictIntraday {
		facts.Warnings = append(facts.Warnings, "当前非连续交易时段，候选池按成交活跃度与最近日K降级筛选；盘中资金需开盘后复核")
	}
	facts.CurrentAmount = marketAmountFromQuotes(quotes)
	if previousAmount.Shanghai > 0 && previousAmount.Shenzhen > 0 {
		facts.PreviousAmount = previousAmount.Shanghai + previousAmount.Shenzhen + math.Max(previousAmount.Beijing, 0)
		if facts.CurrentAmount > 0 {
			facts.AmountChange = (facts.CurrentAmount/facts.PreviousAmount - 1) * 100
		}
	}
	quoteMap := make(map[string]domain.Quote, len(quotes))
	for _, quote := range quotes {
		quoteMap[quote.Symbol] = quote
	}
	reportProgress(progress, "计算三大指数日线趋势与量能")
	type historyResult struct {
		symbol string
		bars   []domain.DailyBar
		err    error
	}
	historyResults := make(chan historyResult, len(market.BroadMarketSymbols))
	for _, symbol := range market.BroadMarketSymbols {
		symbol := symbol
		go func() {
			bars, err := app.scanHistory.FetchDailyBars(ctx, symbol)
			historyResults <- historyResult{symbol: symbol, bars: bars, err: err}
		}()
	}
	indexNames := map[string]string{"sh000001": "上证", "sz399001": "深证", "sz399006": "创业板"}
	for range market.BroadMarketSymbols {
		result := <-historyResults
		technical, ok := scanTechnical(result.bars)
		if result.err != nil || !ok {
			if result.err != nil {
				facts.Warnings = append(facts.Warnings, result.symbol+"日线: "+result.err.Error())
			}
			continue
		}
		facts.Indices = append(facts.Indices, domain.MarketIndexAssessment{
			Symbol: result.symbol, Name: indexNames[result.symbol], Percent: quoteMap[result.symbol].Percent,
			MainNet: indexFlows[result.symbol].MainNet, Technical: technical,
		})
	}
	sort.SliceStable(facts.Indices, func(i, j int) bool {
		order := map[string]int{"sh000001": 0, "sz399001": 1, "sz399006": 2}
		return order[facts.Indices[i].Symbol] < order[facts.Indices[j].Symbol]
	})

	stockMap := make(map[string]domain.MarketStockSnapshot)
	amountRank, flowRank := make(map[string]int), make(map[string]int)
	for index, stock := range stocksAmount {
		stockMap[stock.Symbol] = mergeStock(stockMap[stock.Symbol], stock)
		amountRank[stock.Symbol] = index + 1
		if stock.Percent > 0 {
			facts.TopAmountAdvancers++
		} else if stock.Percent < 0 {
			facts.TopAmountDecliners++
		}
		if finite(stock.MainNet) {
			facts.TopAmountMainNet += stock.MainNet
		}
	}
	for index, stock := range stocksFlow {
		stockMap[stock.Symbol] = mergeStock(stockMap[stock.Symbol], stock)
		flowRank[stock.Symbol] = index + 1
	}
	for _, stock := range stocksUp {
		stockMap[stock.Symbol] = mergeStock(stockMap[stock.Symbol], stock)
	}
	leaders := make(map[string]string)
	leaderSymbols := make([]string, 0, len(facts.HotBoards))
	for _, board := range facts.HotBoards {
		if symbol := stockFromLeaderCode(board.LeaderCode); symbol != "" {
			leaders[symbol] = board.Name
			leaderSymbols = append(leaderSymbols, symbol)
		}
	}
	if len(leaderSymbols) > 0 {
		leaderStocks, err := app.marketScan.FetchStocks(ctx, leaderSymbols)
		if err != nil {
			facts.Warnings = append(facts.Warnings, "板块龙头行情: "+err.Error())
		}
		for _, stock := range leaderStocks {
			stockMap[stock.Symbol] = mergeStock(stockMap[stock.Symbol], stock)
		}
	}
	boardContext := make(map[string]domain.BoardFlow)
	for _, list := range [][]domain.BoardFlow{boardsUp, boardsDown, boardsFlow} {
		for _, board := range list {
			key := normalizeScanIndustry(board.Name)
			if key != "" {
				boardContext[key] = board
			}
		}
	}

	preliminary := make([]domain.MarketCandidateAssessment, 0, len(stockMap))
	for symbol, stock := range stockMap {
		leaderBoard, leader := leaders[symbol]
		flowAvailable := finite(stock.MainNet)
		relaxed := !strictIntraday || !flowAvailable
		score := preliminaryStockScore(stock, leader, amountRank[symbol], flowRank[symbol], relaxed)
		if math.IsInf(score, -1) {
			continue
		}
		reasons := []string{fmt.Sprintf("当日%+.2f%%，成交%.2f亿元", stock.Percent, stock.Amount/1e8)}
		if flowAvailable {
			reasons = append(reasons, fmt.Sprintf("主力净额%+.2f亿元，净占比%+.2f%%", stock.MainNet/1e8, stock.MainRatio))
		} else {
			reasons = append(reasons, "当日主力资金字段暂不可用，未参与评分")
		}
		if leader {
			reasons = append(reasons, "热门板块“"+leaderBoard+"”领涨股")
		}
		risks := make([]string, 0)
		if !flowAvailable {
			risks = append(risks, "主力资金数据缺失，候选置信度降低")
		}
		if !strictIntraday {
			risks = append(risks, "当前非连续交易时段，需开盘后复核量价与资金承接")
		}
		matchedBoard := leaderBoard
		if board, ok := boardContext[normalizeScanIndustry(stock.Industry)]; ok {
			if matchedBoard == "" {
				matchedBoard = board.Name
			}
			if board.Percent > 0 && board.MainNet > 0 && board.RiseCount > board.FallCount {
				score += 2
				reasons = append(reasons, fmt.Sprintf("所属板块%s%+.2f%%、主力%+.2f亿元、广度%d/%d", board.Name, board.Percent, board.MainNet/1e8, board.RiseCount, board.FallCount))
			} else if board.Percent < 0 || board.RiseCount < board.FallCount {
				score -= 4
				risks = append(risks, fmt.Sprintf("所属板块%s%+.2f%%、广度%d/%d，个股强于板块但扩散不足", board.Name, board.Percent, board.RiseCount, board.FallCount))
			}
		}
		if stock.Percent >= 9.8 {
			risks = append(risks, "当日涨幅接近涨停，次日追高风险较高")
		}
		if stock.Turnover > 25 {
			risks = append(risks, fmt.Sprintf("换手率%.2f%%，筹码分歧较大", stock.Turnover))
		}
		preliminary = append(preliminary, domain.MarketCandidateAssessment{
			Stock: stock, MatchedBoard: matchedBoard, BoardLeader: leader, FlowAvailable: flowAvailable,
			Score: score, Reasons: reasons, Risks: risks,
		})
	}
	sort.SliceStable(preliminary, func(i, j int) bool { return preliminary[i].Score > preliminary[j].Score })
	if len(preliminary) > marketReportHistoryLimit {
		shortlisted := append([]domain.MarketCandidateAssessment(nil), preliminary[:marketReportHistoryLimit]...)
		for _, candidate := range preliminary[marketReportHistoryLimit:] {
			if candidate.BoardLeader {
				shortlisted = append(shortlisted, candidate)
			}
		}
		preliminary = shortlisted
	}
	reportProgress(progress, "计算候选股5/20/60日量价结构")
	stockHistoryResults := make(chan historyResult, len(preliminary))
	for _, candidate := range preliminary {
		symbol := candidate.Stock.Symbol
		go func() {
			bars, err := app.scanHistory.FetchDailyBars(ctx, symbol)
			stockHistoryResults <- historyResult{symbol: symbol, bars: bars, err: err}
		}()
	}
	technicals := make(map[string]domain.MarketTechnicalSnapshot, len(preliminary))
	for range preliminary {
		result := <-stockHistoryResults
		if technical, ok := scanTechnical(result.bars); ok {
			technicals[result.symbol] = technical
		} else if result.err != nil {
			facts.Warnings = append(facts.Warnings, result.symbol+"日线: "+result.err.Error())
		}
	}
	for index := range preliminary {
		preliminary[index].Technical = technicals[preliminary[index].Stock.Symbol]
		applyTechnicalScore(&preliminary[index])
	}
	reportProgress(progress, "核对候选股近期正式公告索引")
	preliminarySymbols := make([]string, 0, len(preliminary))
	for _, candidate := range preliminary {
		preliminarySymbols = append(preliminarySymbols, candidate.Stock.Symbol)
	}
	announcements, announcementError := app.marketScan.FetchAnnouncements(ctx, preliminarySymbols, 100)
	if announcementError != nil {
		facts.Warnings = append(facts.Warnings, "公告索引: "+announcementError.Error())
	}
	reportDate := facts.GeneratedAt.Format("2006-01-02")
	filteredAnnouncements := announcements[:0]
	for _, announcement := range announcements {
		if announcement.Date == "" || announcement.Date <= reportDate {
			filteredAnnouncements = append(filteredAnnouncements, announcement)
		}
	}
	announcements = filteredAnnouncements
	for index := range preliminary {
		attachAnnouncements(&preliminary[index], announcements)
		preliminary[index].Grade, preliminary[index].Category = classifyMarketCandidate(preliminary[index])
	}
	facts.Candidates = selectMarketCandidates(preliminary, marketReportCandidateLimit)
	if len(facts.Candidates) == 0 {
		return domain.MarketScanFacts{}, fmt.Errorf("未筛选到满足量价和资金条件的候选股")
	}
	sanitizeMarketScanFacts(&facts)
	return facts, nil
}

func zeroIfNotFinite(value float64) float64 {
	if !finite(value) {
		return 0
	}
	return value
}

func sanitizeTechnical(value *domain.MarketTechnicalSnapshot) {
	value.Close = zeroIfNotFinite(value.Close)
	value.Return5 = zeroIfNotFinite(value.Return5)
	value.Return20 = zeroIfNotFinite(value.Return20)
	value.Return60 = zeroIfNotFinite(value.Return60)
	value.MA5 = zeroIfNotFinite(value.MA5)
	value.MA20 = zeroIfNotFinite(value.MA20)
	value.MA60 = zeroIfNotFinite(value.MA60)
	value.VolumeRatio20 = zeroIfNotFinite(value.VolumeRatio20)
	value.ClosePosition = zeroIfNotFinite(value.ClosePosition)
	value.Prior20High = zeroIfNotFinite(value.Prior20High)
	value.Prior20Low = zeroIfNotFinite(value.Prior20Low)
}

func sanitizeBoard(value *domain.MarketBoardAssessment) {
	value.Percent = zeroIfNotFinite(value.Percent)
	value.MainNet = zeroIfNotFinite(value.MainNet)
	value.MainRatio = zeroIfNotFinite(value.MainRatio)
	value.Turnover = zeroIfNotFinite(value.Turnover)
	value.LeaderPercent = zeroIfNotFinite(value.LeaderPercent)
	value.Score = zeroIfNotFinite(value.Score)
}

func sanitizeStock(value *domain.MarketStockSnapshot) {
	value.Price = zeroIfNotFinite(value.Price)
	value.Percent = zeroIfNotFinite(value.Percent)
	value.Amount = zeroIfNotFinite(value.Amount)
	value.Turnover = zeroIfNotFinite(value.Turnover)
	value.VolumeRatio = zeroIfNotFinite(value.VolumeRatio)
	value.Speed = zeroIfNotFinite(value.Speed)
	value.High = zeroIfNotFinite(value.High)
	value.Low = zeroIfNotFinite(value.Low)
	value.Open = zeroIfNotFinite(value.Open)
	value.PreviousClose = zeroIfNotFinite(value.PreviousClose)
	value.MarketCap = zeroIfNotFinite(value.MarketCap)
	value.MainNet = zeroIfNotFinite(value.MainNet)
	value.MainRatio = zeroIfNotFinite(value.MainRatio)
}

func sanitizeMarketScanFacts(facts *domain.MarketScanFacts) {
	facts.CurrentAmount = zeroIfNotFinite(facts.CurrentAmount)
	facts.PreviousAmount = zeroIfNotFinite(facts.PreviousAmount)
	facts.AmountChange = zeroIfNotFinite(facts.AmountChange)
	facts.TopAmountMainNet = zeroIfNotFinite(facts.TopAmountMainNet)
	for index := range facts.Indices {
		facts.Indices[index].Percent = zeroIfNotFinite(facts.Indices[index].Percent)
		facts.Indices[index].MainNet = zeroIfNotFinite(facts.Indices[index].MainNet)
		sanitizeTechnical(&facts.Indices[index].Technical)
	}
	for index := range facts.HotBoards {
		sanitizeBoard(&facts.HotBoards[index])
	}
	for index := range facts.WeakBoards {
		sanitizeBoard(&facts.WeakBoards[index])
	}
	for index := range facts.Candidates {
		sanitizeStock(&facts.Candidates[index].Stock)
		sanitizeTechnical(&facts.Candidates[index].Technical)
		facts.Candidates[index].Score = zeroIfNotFinite(facts.Candidates[index].Score)
	}
}

func humanReportAmount(value float64) string {
	absolute := math.Abs(value)
	switch {
	case absolute >= 1e8:
		return fmt.Sprintf("%+.2f亿元", value/1e8)
	case absolute >= 1e4:
		return fmt.Sprintf("%+.0f万元", value/1e4)
	default:
		return fmt.Sprintf("%+.0f元", value)
	}
}

func marketReportBoardFlow(board domain.MarketBoardAssessment) string {
	if !board.FlowAvailable {
		return "主力暂不可用"
	}
	return "主力" + humanReportAmount(board.MainNet)
}

func marketReportCandidateFlow(candidate domain.MarketCandidateAssessment) string {
	if !candidate.FlowAvailable {
		return "主力暂不可用"
	}
	return "主力" + humanReportAmount(candidate.Stock.MainNet)
}

func renderDeterministicMarketReport(facts domain.MarketScanFacts, aiError string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# A股智能市场扫描\n\n生成时间：%s\n\n", facts.GeneratedAt.Format("2006-01-02 15:04:05"))
	if aiError != "" {
		fmt.Fprintf(&builder, "> Codex综合暂不可用，以下为确定性量价报告：%s\n\n", aiError)
	}
	builder.WriteString("## 大盘量价\n\n")
	for _, index := range facts.Indices {
		fmt.Fprintf(&builder, "- %s：%.2f，%+.2f%%；MA5/20/60 %.2f/%.2f/%.2f，20日量比 %.2f，%s。\n",
			index.Name, index.Technical.Close, index.Percent, index.Technical.MA5, index.Technical.MA20,
			index.Technical.MA60, index.Technical.VolumeRatio20, index.Technical.Trend)
	}
	if facts.CurrentAmount > 0 {
		fmt.Fprintf(&builder, "- 当前沪深京成交额 %.2f万亿元，较上一交易日 %+.2f%%。\n", facts.CurrentAmount/1e8, facts.AmountChange)
	}
	fmt.Fprintf(&builder, "- 成交额前100股：%d涨/%d跌，主力合计%s。\n\n",
		facts.TopAmountAdvancers, facts.TopAmountDecliners, humanReportAmount(facts.TopAmountMainNet))
	builder.WriteString("## 热门板块与龙头\n\n")
	for _, board := range facts.HotBoards {
		fmt.Fprintf(&builder, "- %s：%+.2f%%，%s，广度%d涨/%d跌，龙头%s(%s，%+.2f%%)。\n",
			board.Name, board.Percent, marketReportBoardFlow(board), board.RiseCount, board.FallCount,
			board.LeaderName, board.LeaderCode, board.LeaderPercent)
	}
	builder.WriteString("\n## 智能观察池\n\n")
	gradeLabels := map[string]string{
		"A": "A：板块龙头/核心承接",
		"B": "B：高波动情绪候选",
		"C": "C：超跌反转观察",
	}
	itemNumber := 0
	for _, grade := range []string{"A", "B", "C"} {
		hasGrade := false
		for _, candidate := range facts.Candidates {
			if candidate.Grade == grade {
				hasGrade = true
				break
			}
		}
		if !hasGrade {
			continue
		}
		fmt.Fprintf(&builder, "### %s\n\n", gradeLabels[grade])
		for _, candidate := range facts.Candidates {
			if candidate.Grade != grade {
				continue
			}
			itemNumber++
			fmt.Fprintf(&builder, "%d. %s %s：%+.2f%%，成交%.2f亿元，%s；%s。\n",
				itemNumber, candidate.Stock.Symbol[2:], candidate.Stock.Name, candidate.Stock.Percent,
				candidate.Stock.Amount/1e8, marketReportCandidateFlow(candidate), candidate.Technical.Trend)
			if len(candidate.Risks) > 0 {
				fmt.Fprintf(&builder, "   风险：%s。\n", strings.Join(candidate.Risks, "；"))
			}
		}
		builder.WriteString("\n")
	}
	builder.WriteString("## 使用边界\n\n- 观察池不是开盘买入清单；需结合次日板块广度、1/3/5分钟资金和价格承接复核。\n- 公告标题仅为索引线索，核心条款应回到交易所或公司正式PDF核验。\n- 本报告不触发自动交易，也不提供确定性买卖指令。\n")
	return builder.String()
}

func marketReportPrompt(facts domain.MarketScanFacts) (string, error) {
	data, err := json.MarshalIndent(facts, "", "  ")
	if err != nil {
		return "", err
	}
	prompt := `你是A股市场扫描报告编辑。不要运行命令，不要读取文件，不要搜索网络，只分析下面提供的结构化事实。

硬性要求：
1. 用中文输出Markdown，不使用表格，单行尽量不超过80个显示字符。
2. 先给一句话总判断，再分为“大盘量能与承接”“看涨/看跌条件”“热门板块与5只龙头”“智能观察池10只”“公告与舆情线索”“次日复核规则”。
3. 必须区分短线和中期；趋势必须引用MA5/20/60、20日量比、收盘位置或成交额变化。
4. 板块通常同时引用涨幅、主力净额和上涨/下跌家数；flow_available为false时必须明确写“资金暂不可用”，并只按涨幅与广度做低置信度判断。
5. 观察池必须按grade/category分为“A板块龙头/核心承接”“B高波动情绪候选”“C超跌反转观察”，沿用reasons、risks和公告标题；flow_available为false时不得把main_net=0描述成资金持平或净流入；不得添加数据中没有的公司事实、政策、新闻或公告数字。
6. 资金流和公告标题只是行为/事件线索，不得写成确定性买卖指令。公告核心条款未经PDF核验时必须说明。
7. 最后明确：报告不触发自动交易，观察池不是开盘直接买入清单。

结构化事实JSON：
` + string(data)
	return prompt, nil
}

func codexReportTimeout() time.Duration {
	seconds := 300
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ASTOCK_CODEX_TIMEOUT_SECONDS"))); err == nil && value >= 30 && value <= 1800 {
		seconds = value
	}
	return time.Duration(seconds) * time.Second
}

func (app *App) generateMarketReport(ctx context.Context, progress marketReportProgress) (domain.GeneratedMarketReport, error) {
	facts, err := app.collectMarketScanFacts(ctx, progress)
	if err != nil {
		return domain.GeneratedMarketReport{}, err
	}
	report := domain.GeneratedMarketReport{GeneratedAt: facts.GeneratedAt, Facts: facts}
	prompt, promptError := marketReportPrompt(facts)
	if promptError != nil {
		return report, promptError
	}
	if app.marketReportAI != nil {
		reportProgress(progress, "Codex正在后台综合市场报告")
		aiContext, cancel := context.WithTimeout(ctx, codexReportTimeout())
		markdown, aiError := app.marketReportAI.Synthesize(aiContext, prompt)
		cancel()
		if aiError == nil {
			report.AIUsed = true
			report.Markdown = markdown
		} else {
			report.AIError = aiError.Error()
		}
	}
	if strings.TrimSpace(report.Markdown) == "" {
		report.Markdown = renderDeterministicMarketReport(facts, report.AIError)
	}
	if app.marketReports == nil {
		return report, fmt.Errorf("智能市场报告存储未初始化")
	}
	reportProgress(progress, "保存报告与结构化快照")
	return app.marketReports.Save(report)
}
