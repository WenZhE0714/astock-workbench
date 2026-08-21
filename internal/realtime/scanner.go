package realtime

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/marketregime"
)

const (
	defaultLeaderLimit    = 20
	maximumUniverse       = 50
	maximumMinuteLoads    = 6
	historyMemoryTTL      = 15 * time.Minute
	minimumFactorCoverage = 0.55
)

type componentFamily struct {
	keys   []string
	weight float64
	useMax bool
}

// The composite keeps alternative entry setups in one family and gives the
// risk/quality layer a smaller share than alpha and market-context evidence.
// This mirrors the separation used by mature research and execution systems
// without hiding any component's raw 0-20 score from the user.
var realtimeComponentFamilies = []componentFamily{
	{keys: []string{"trend-breakout", "ma-pullback"}, weight: .20, useMax: true},
	{keys: []string{"relative-momentum"}, weight: .15},
	{keys: []string{"price-volume"}, weight: .15},
	{keys: []string{"fund-support"}, weight: .15},
	{keys: []string{"sector-rotation"}, weight: .10},
	{keys: []string{"market-regime"}, weight: .10},
	{keys: []string{"volatility-risk"}, weight: .075},
	{keys: []string{"liquidity-quality"}, weight: .075},
}

type historyCacheEntry struct {
	bars      []domain.DailyBar
	fetchedAt time.Time
}

type industryFlowClient interface {
	FetchIndustryFlows(context.Context) (map[string]domain.BoardFlow, error)
}

type Scanner struct {
	market     MarketClient
	quotes     QuoteClient
	history    HistoryClient
	minutes    MinuteClient
	store      Store
	strategies []Strategy
	now        func() time.Time
	historyMu  sync.Mutex
	histories  map[string]historyCacheEntry
}

func NewScanner(market MarketClient, quotes QuoteClient, history HistoryClient, minutes MinuteClient, store Store) *Scanner {
	return &Scanner{
		market: market, quotes: quotes, history: history, minutes: minutes, store: store,
		strategies: DefaultStrategies(), now: time.Now, histories: make(map[string]historyCacheEntry),
	}
}

func (scanner *Scanner) Scan(ctx context.Context, watchlist []string, includeLeaders bool) (ScanResult, error) {
	if scanner == nil || scanner.market == nil || scanner.history == nil {
		return ScanResult{}, fmt.Errorf("实时策略扫描器未初始化")
	}
	now := scanner.now()
	warnings := make([]string, 0)
	symbols := stockSymbols(watchlist)
	if includeLeaders {
		leaders, err := scanner.market.FetchStockRanking(ctx, domain.MarketScanByAmount, true, defaultLeaderLimit)
		if err != nil {
			warnings = append(warnings, "强势候选获取失败: "+err.Error())
		} else {
			for _, item := range leaders {
				if item.Percent > 0 || item.Speed > 0 || item.MainNet > 0 {
					symbols = append(symbols, item.Symbol)
				}
			}
		}
	}
	symbols = uniqueSymbols(symbols, maximumUniverse)
	if len(symbols) == 0 {
		return ScanResult{}, fmt.Errorf("实时扫描股票池为空")
	}

	stocks, err := scanner.market.FetchStocks(ctx, symbols)
	if err != nil {
		return ScanResult{}, fmt.Errorf("实时行情扫描失败: %w", err)
	}
	stockBySymbol := make(map[string]domain.MarketStockSnapshot, len(stocks))
	for _, stock := range stocks {
		stockBySymbol[stock.Symbol] = stock
	}
	quotes := scanner.fetchQuotes(ctx, symbols, &warnings)
	boards := scanner.fetchBoards(ctx, &warnings)
	benchmark, benchmarkError := scanner.fetchHistory(ctx, "sh000300")
	if benchmarkError != nil {
		warnings = append(warnings, "沪深300历史数据不可用: "+benchmarkError.Error())
	}

	type historyResult struct {
		symbol string
		bars   []domain.DailyBar
		err    error
	}
	results := make(chan historyResult, len(symbols))
	limit := make(chan struct{}, 6)
	var group sync.WaitGroup
	for _, symbol := range symbols {
		if _, ok := stockBySymbol[symbol]; !ok {
			continue
		}
		group.Add(1)
		go func(symbol string) {
			defer group.Done()
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				results <- historyResult{symbol: symbol, err: ctx.Err()}
				return
			}
			defer func() { <-limit }()
			bars, fetchError := scanner.fetchHistory(ctx, symbol)
			results <- historyResult{symbol: symbol, bars: bars, err: fetchError}
		}(symbol)
	}
	go func() { group.Wait(); close(results) }()

	histories := make(map[string][]domain.DailyBar)
	for result := range results {
		if result.err != nil {
			warnings = append(warnings, fmt.Sprintf("%s 日K不可用: %v", result.symbol, result.err))
			continue
		}
		histories[result.symbol] = result.bars
	}

	minutes := scanner.fetchMinutes(ctx, rankedMinuteSymbols(stocks, maximumMinuteLoads), &warnings)
	signals := make([]Signal, 0, len(histories))
	for _, symbol := range symbols {
		stock, ok := stockBySymbol[symbol]
		if !ok {
			continue
		}
		bars, ok := histories[symbol]
		if !ok {
			continue
		}
		var board *domain.BoardFlow
		if item, found := matchIndustryBoard(boards, stock.Industry); found {
			copied := item
			board = &copied
		}
		input := Snapshot{Now: now, Stock: stock, Bars: bars, Minutes: minutes[symbol], Board: board, Benchmark: benchmark}
		if quote, found := quotes[symbol]; found {
			copied := quote
			input.Quote = &copied
			if price := parseQuoteNumber(quote.Current); price > 0 {
				input.Stock.Price = price
			}
			if finite(quote.Percent) {
				input.Stock.Percent = quote.Percent
			}
		}
		signals = append(signals, scanner.evaluate(input))
	}
	sort.SliceStable(signals, func(left, right int) bool {
		if signals[left].Score == signals[right].Score {
			return signals[left].Speed > signals[right].Speed
		}
		return signals[left].Score > signals[right].Score
	})
	result := ScanResult{GeneratedAt: now, Universe: "watchlist+leaders", MarketState: MarketSessionAt(now).State, Signals: signals, Warnings: uniqueStrings(warnings, 30)}
	if scanner.store != nil {
		if saveError := scanner.store.Append(result); saveError != nil {
			result.Warnings = append(result.Warnings, "信号留痕失败: "+saveError.Error())
		}
	}
	return result, nil
}

func (scanner *Scanner) fetchHistory(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	now := scanner.now()
	scanner.historyMu.Lock()
	entry, found := scanner.histories[symbol]
	if found && now.Sub(entry.fetchedAt) >= 0 && now.Sub(entry.fetchedAt) < historyMemoryTTL {
		bars := append([]domain.DailyBar(nil), entry.bars...)
		scanner.historyMu.Unlock()
		return bars, nil
	}
	scanner.historyMu.Unlock()
	bars, err := scanner.history.FetchDailyBars(ctx, symbol)
	if err != nil {
		return nil, err
	}
	scanner.historyMu.Lock()
	scanner.histories[symbol] = historyCacheEntry{bars: append([]domain.DailyBar(nil), bars...), fetchedAt: now}
	scanner.historyMu.Unlock()
	return bars, nil
}

func (scanner *Scanner) evaluate(input Snapshot) Signal {
	components := make([]Component, 0, len(scanner.strategies))
	warnings := make([]string, 0)
	for _, item := range scanner.strategies {
		value := item.Evaluate(input)
		components = append(components, value)
		warnings = append(warnings, value.Warnings...)
	}
	score, coverage, contributions := compositeScore(components)
	state := StateWeak
	if score >= 72 {
		state = StateTriggered
	} else if score >= 55 {
		state = StateWatching
	}
	if coverage < minimumFactorCoverage {
		state = StateInvalid
	}
	indicators, hasIndicators := calculateIndicators(input)
	price := input.Stock.Price
	triggerPrice, invalidationPrice, dataDate, source := 0.0, 0.0, "", ""
	if hasIndicators {
		if price <= 0 {
			price = indicators.latest.Close
		}
		triggerPrice = math.Max(indicators.prior20High, indicators.ma5)
		invalidationPrice = math.Max(indicators.ma20, indicators.prior20Low)
		dataDate, source = indicators.latest.Date, indicators.latest.Source
	}
	reasons := topReasons(components, contributions, 5)
	risks := signalRisks(input, indicators, hasIndicators)
	quoteTime := ""
	if input.Quote != nil {
		quoteTime = input.Quote.QuoteTime
	}
	return Signal{
		ID:     input.Now.Format("20060102T150405") + "-" + input.Stock.Symbol,
		Symbol: input.Stock.Symbol, Name: input.Stock.Name, Industry: input.Stock.Industry,
		State: state, Score: math.Round(score*10) / 10, Price: price, Percent: input.Stock.Percent, Speed: input.Stock.Speed,
		TriggerPrice: triggerPrice, InvalidationPrice: invalidationPrice,
		AsOf: input.Now, QuoteTime: quoteTime, DataDate: dataDate, DataSource: source,
		Components: components, Reasons: reasons, Risks: risks, Warnings: uniqueStrings(warnings, 10),
	}
}

func (scanner *Scanner) fetchQuotes(ctx context.Context, symbols []string, warnings *[]string) map[string]domain.Quote {
	result := make(map[string]domain.Quote)
	if scanner.quotes == nil {
		return result
	}
	items, err := scanner.quotes.Fetch(ctx, symbols)
	if err != nil {
		*warnings = append(*warnings, "报价补充失败: "+err.Error())
		return result
	}
	for _, item := range items {
		result[item.Symbol] = item
	}
	return result
}

func (scanner *Scanner) fetchBoards(ctx context.Context, warnings *[]string) map[string]domain.BoardFlow {
	result := make(map[string]domain.BoardFlow)
	if client, ok := scanner.market.(industryFlowClient); ok {
		items, err := client.FetchIndustryFlows(ctx)
		if err == nil && len(items) > 0 {
			for name, board := range items {
				if strings.TrimSpace(board.Name) == "" {
					board.Name = strings.TrimSpace(name)
				}
				if board.Name != "" {
					result[board.Name] = board
				}
			}
			return result
		}
		if err != nil {
			*warnings = append(*warnings, "完整行业板块获取失败，已回退排行榜: "+err.Error())
		}
	}
	lists := [][]domain.BoardFlow{}
	for _, request := range []struct {
		metric     domain.MarketScanMetric
		descending bool
	}{{domain.MarketScanByPercent, true}, {domain.MarketScanByMainNet, true}} {
		items, err := scanner.market.FetchIndustryRanking(ctx, request.metric, request.descending, 100)
		if err != nil {
			*warnings = append(*warnings, "行业板块扫描失败: "+err.Error())
			continue
		}
		lists = append(lists, items)
	}
	for _, list := range lists {
		for _, board := range list {
			name := strings.TrimSpace(board.Name)
			if name == "" {
				continue
			}
			result[name] = board
		}
	}
	return result
}

// EnrichSectors repairs the sector component of an already archived snapshot.
// It does not rescan stocks, change the archived composite score, or persist a
// new signal record.
func (scanner *Scanner) EnrichSectors(ctx context.Context, result ScanResult) (ScanResult, error) {
	if scanner == nil || scanner.market == nil || result.GeneratedAt.IsZero() {
		return result, nil
	}
	warnings := make([]string, 0)
	boards := scanner.fetchBoards(ctx, &warnings)
	if len(boards) == 0 {
		if len(warnings) > 0 {
			return result, fmt.Errorf("%s", strings.Join(warnings, "; "))
		}
		return result, fmt.Errorf("未获取到行业板块数据")
	}

	updated := result
	updated.Signals = append([]Signal(nil), result.Signals...)
	for index := range updated.Signals {
		signal := updated.Signals[index]
		components := append([]Component(nil), signal.Components...)
		componentIndex := -1
		for cursor, item := range components {
			if item.Key == "sector-rotation" && item.State == "数据不足" {
				componentIndex = cursor
				break
			}
		}
		if componentIndex < 0 {
			continue
		}
		board, found := matchIndustryBoard(boards, signal.Industry)
		if !found {
			continue
		}
		components[componentIndex] = evaluateSectorRotation(Snapshot{
			Stock: domain.MarketStockSnapshot{Symbol: signal.Symbol, Industry: signal.Industry},
			Board: &board,
		})
		signal.Components = components
		signal.Warnings = removeString(signal.Warnings, "未匹配到实时行业板块")
		updated.Signals[index] = signal
	}
	return updated, nil
}

func matchIndustryBoard(boards map[string]domain.BoardFlow, industry string) (domain.BoardFlow, bool) {
	bestScore := 0
	best := domain.BoardFlow{}
	for name, board := range boards {
		if strings.TrimSpace(board.Name) == "" {
			board.Name = strings.TrimSpace(name)
		}
		score := industryMatchScore(industry, board.Name)
		if score > bestScore || (score == bestScore && score > 0 && (best.Name == "" || board.Name < best.Name)) {
			bestScore = score
			best = board
		}
	}
	return best, bestScore > 0
}

func industryMatchScore(industry, board string) int {
	industry = strings.TrimSpace(industry)
	board = strings.TrimSpace(board)
	if industry == "" || board == "" {
		return 0
	}
	if industry == board {
		return 3
	}
	if normalizeIndustryLevel(industry) == normalizeIndustryLevel(board) {
		return 2
	}
	if normalizeIndustryName(industry) == normalizeIndustryName(board) {
		return 1
	}
	return 0
}

func normalizeIndustryLevel(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "ⅠⅡⅢIV")
}

func normalizeIndustryName(value string) string {
	value = normalizeIndustryLevel(value)
	return strings.TrimSuffix(value, "设备")
}

func removeString(items []string, target string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item != target {
			result = append(result, item)
		}
	}
	return result
}

func (scanner *Scanner) fetchMinutes(ctx context.Context, symbols []string, warnings *[]string) map[string][]domain.MinutePoint {
	result := make(map[string][]domain.MinutePoint)
	if scanner.minutes == nil {
		return result
	}
	for _, symbol := range symbols {
		points, err := scanner.minutes.FetchMinutePoints(ctx, symbol)
		if err != nil {
			continue
		}
		result[symbol] = points
	}
	return result
}

func rankedMinuteSymbols(stocks []domain.MarketStockSnapshot, limit int) []string {
	items := append([]domain.MarketStockSnapshot(nil), stocks...)
	sort.SliceStable(items, func(left, right int) bool {
		leftValue := math.Abs(items[left].Speed)*2 + math.Abs(items[left].Percent) + math.Max(items[left].MainNet, 0)/1e8
		rightValue := math.Abs(items[right].Speed)*2 + math.Abs(items[right].Percent) + math.Max(items[right].MainNet, 0)/1e8
		return leftValue > rightValue
	})
	if len(items) > limit {
		items = items[:limit]
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Symbol)
	}
	return result
}

func compositeScore(components []Component) (float64, float64, map[string]float64) {
	byKey := make(map[string]Component, len(components))
	for _, item := range components {
		byKey[item.Key] = item
	}
	score, weightAvailable := 0.0, 0.0
	contributions := make(map[string]float64, len(components))
	for _, family := range realtimeComponentFamilies {
		bestKey, bestScore := "", 0.0
		available := false
		for _, key := range family.keys {
			item, found := byKey[key]
			if !found || item.State == "数据不足" {
				continue
			}
			if !available || (family.useMax && item.Score > bestScore) {
				bestKey, bestScore, available = key, item.Score, true
			}
			if !family.useMax {
				break
			}
		}
		if !available {
			continue
		}
		weightAvailable += family.weight
		contribution := family.weight * bestScore / 20 * 100
		score += contribution
		contributions[bestKey] += contribution
	}
	if weightAvailable <= 0 {
		return 0, 0, contributions
	}
	return score / weightAvailable, weightAvailable, contributions
}

func topReasons(components []Component, contributions map[string]float64, limit int) []string {
	items := append([]Component(nil), components...)
	sort.SliceStable(items, func(left, right int) bool {
		leftContribution, rightContribution := contributions[items[left].Key], contributions[items[right].Key]
		if leftContribution == rightContribution {
			return items[left].Score > items[right].Score
		}
		return leftContribution > rightContribution
	})
	result := make([]string, 0, limit)
	for _, includeQuality := range []bool{false, true} {
		for _, item := range items {
			quality := item.Key == "volatility-risk" || item.Key == "liquidity-quality"
			if quality != includeQuality || contributions[item.Key] <= 0 || len(item.Reasons) == 0 {
				continue
			}
			result = append(result, item.Name+": "+item.Reasons[0])
			if len(result) >= limit {
				return result
			}
		}
	}
	return result
}

func signalRisks(input Snapshot, value indicators, ok bool) []string {
	risks := make([]string, 0, 5)
	if input.Stock.MainNet < 0 {
		risks = append(risks, fmt.Sprintf("主力净流出 %.2f亿元", math.Abs(input.Stock.MainNet)/1e8))
	}
	if input.Board != nil && input.Board.MainNet < 0 {
		risks = append(risks, fmt.Sprintf("所属板块净流出 %.2f亿元", math.Abs(input.Board.MainNet)/1e8))
	}
	if ok {
		price := currentPrice(input, value.latest.Close)
		if price < value.ma20 {
			risks = append(risks, fmt.Sprintf("现价低于MA20 %.2f", value.ma20))
		}
		if value.return20 > 20 {
			risks = append(risks, fmt.Sprintf("20日涨幅 %+.2f%%，追高风险上升", value.return20))
		}
		if finite(value.volatility20) && value.volatility20 > 4 {
			risks = append(risks, fmt.Sprintf("20日波动率 %.2f%%偏高", value.volatility20))
		}
		if finite(value.drawdown20) && value.drawdown20 < -10 {
			risks = append(risks, fmt.Sprintf("较20日高点回撤 %.2f%%", value.drawdown20))
		}
		if finite(input.Stock.Turnover) && input.Stock.Turnover > 12 {
			risks = append(risks, fmt.Sprintf("换手率 %.2f%%偏高", input.Stock.Turnover))
		}
		if finite(input.Stock.VolumeRatio) && input.Stock.VolumeRatio > 4 {
			risks = append(risks, fmt.Sprintf("量比 %.2f异常放大", input.Stock.VolumeRatio))
		}
		if value.marketRegime == marketregime.Bear || value.marketRegime == marketregime.HighVol {
			risks = append(risks, fmt.Sprintf("市场处于%s状态", value.marketRegime))
		}
	}
	if len(risks) == 0 {
		risks = append(risks, "未发现显著量价风险，但信号仍可能失效")
	}
	return risks
}

func stockSymbols(input []string) []string {
	result := make([]string, 0, len(input))
	for _, symbol := range input {
		if len(symbol) == 8 && (strings.HasPrefix(symbol, "sh") || strings.HasPrefix(symbol, "sz") || strings.HasPrefix(symbol, "bj")) {
			result = append(result, symbol)
		}
	}
	return result
}

func uniqueSymbols(input []string, limit int) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))
	for _, item := range input {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func uniqueStrings(input []string, limit int) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))
	for _, item := range input {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func marketState(now time.Time) string {
	return MarketSessionAt(now).State
}

func parseQuoteNumber(value string) float64 {
	number, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return number
}
