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
	defaultLeaderLimit            = 20
	maximumUniverse               = 50
	maximumMinuteLoads            = 6
	historyMemoryTTL              = 15 * time.Minute
	minimumFactorCoverage         = 0.55
	minimumRealtimePortfolioScore = 55.0
	minimumRankingPool            = 5
	portfolioTopFraction          = 0.30
	portfolioIndustryFraction     = 0.40
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
	market        MarketClient
	quotes        QuoteClient
	history       HistoryClient
	minutes       MinuteClient
	store         Store
	strategies    []Strategy
	now           func() time.Time
	calendar      TradingCalendarProvider
	historyMu     sync.Mutex
	histories     map[string]historyCacheEntry
	calibrationMu sync.RWMutex
	calibration   ScoreCalibration
}

// SetTradingCalendarProvider injects the same exchange calendar used by the
// Web scheduler and shadow accounts. Passing nil restores benchmark-date
// fallback behavior.
func (scanner *Scanner) SetTradingCalendarProvider(provider TradingCalendarProvider) {
	if scanner == nil {
		return
	}
	scanner.calendar = provider
}

func NewScanner(market MarketClient, quotes QuoteClient, history HistoryClient, minutes MinuteClient, store Store) *Scanner {
	return &Scanner{
		market: market, quotes: quotes, history: history, minutes: minutes, store: store,
		strategies: DefaultStrategies(), now: time.Now, histories: make(map[string]historyCacheEntry),
	}
}

// SetCalibration installs a gated challenger configuration. An empty or
// invalid value clears the challenger and leaves the champion score untouched.
func (scanner *Scanner) SetCalibration(calibration ScoreCalibration) {
	if scanner == nil {
		return
	}
	calibration = normalizedScoreCalibration(calibration)
	scanner.calibrationMu.Lock()
	scanner.calibration = calibration
	scanner.calibrationMu.Unlock()
}

func (scanner *Scanner) calibrationSnapshot() ScoreCalibration {
	if scanner == nil {
		return ScoreCalibration{}
	}
	scanner.calibrationMu.RLock()
	defer scanner.calibrationMu.RUnlock()
	result := scanner.calibration
	result.ComponentWeights = cloneFloatMap(result.ComponentWeights)
	return result
}

func normalizedScoreCalibration(input ScoreCalibration) ScoreCalibration {
	input.ID = strings.TrimSpace(input.ID)
	if input.ID == "" || !finite(input.MinimumScore) || input.MinimumScore <= 0 {
		return ScoreCalibration{}
	}
	input.MinimumScore = clamp(input.MinimumScore, 45, 80)
	weights := make(map[string]float64)
	total := 0.0
	for key, weight := range input.ComponentWeights {
		key = strings.TrimSpace(key)
		if key == "" || !finite(weight) || weight <= 0 {
			continue
		}
		weights[key] = weight
		total += weight
	}
	if len(weights) < 2 || total <= 0 {
		return ScoreCalibration{}
	}
	for key := range weights {
		weights[key] /= total
	}
	input.ComponentWeights = weights
	return input
}

func cloneFloatMap(input map[string]float64) map[string]float64 {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]float64, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func (scanner *Scanner) Scan(ctx context.Context, watchlist []string, includeLeaders bool) (ScanResult, error) {
	if scanner == nil || scanner.market == nil || scanner.history == nil {
		return ScanResult{}, fmt.Errorf("实时策略扫描器未初始化")
	}
	now := scanner.now()
	warnings := make([]string, 0)
	// Establish the exchange session before requesting rankings, quotes or
	// minutes. Known holidays and make-up days should short-circuit the whole
	// scan instead of spending network capacity on a snapshot that cannot be
	// used.
	benchmark, benchmarkError := scanner.fetchHistory(ctx, "sh000300")
	if benchmarkError != nil {
		warnings = append(warnings, "沪深300历史数据不可用: "+benchmarkError.Error())
	}
	calendarDates := TradingDatesFromBars(benchmark)
	if scanner.calendar != nil {
		provided, calendarErr := scanner.calendar(ctx, now)
		if calendarErr != nil {
			warnings = append(warnings, "交易日历提供器不可用，回退沪深300日期: "+calendarErr.Error())
		} else if normalized := NormalizeTradingDates(provided); len(normalized) > 0 {
			calendarDates = normalized
		} else {
			warnings = append(warnings, "交易日历提供器未返回有效日期，回退沪深300日期")
		}
	}
	session := MarketSessionAtWithCalendar(now, calendarDates)
	if !session.TradingDay {
		return ScanResult{}, fmt.Errorf("当前日期不在交易日历中，暂停实时扫描")
	}
	if benchmarkError == nil && !session.CalendarKnown {
		// The history window may end before the current session. Keep the
		// weekday fallback, but leave an auditable warning instead of implying
		// that the provider supplied a complete future holiday calendar.
		warnings = append(warnings, "交易日历未覆盖当前日期，当前会话按工作日规则降级")
	}
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
		input := Snapshot{Now: now, Stock: stock, Bars: bars, Minutes: minutes[symbol], Board: board, Benchmark: benchmark, CalendarDates: calendarDates}
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
	signals = applyCrossSectionOverlay(signals)
	signals = applyCalibratedOverlay(signals)
	stale, current := quoteDateCoverage(signals, session.TradingDate)
	if stale > 0 && current == 0 {
		return ScanResult{}, fmt.Errorf("行情日期未推进到%s，暂停实时扫描（%d个信号仍为旧交易日）", session.TradingDate, stale)
	}
	if stale > 0 && current > 0 {
		warnings = append(warnings, fmt.Sprintf("行情日期混杂：%d个信号为当前交易日，%d个信号仍为旧交易日", current, stale))
	}
	result := ScanResult{GeneratedAt: now, Universe: "watchlist+leaders", MarketState: session.State, TradingDate: session.TradingDate, TradingDay: session.TradingDay, CalendarKnown: session.CalendarKnown, Signals: signals, Warnings: uniqueStrings(warnings, 30)}
	if scanner.store != nil {
		if saveError := scanner.store.Append(result); saveError != nil {
			result.Warnings = append(result.Warnings, "信号留痕失败: "+saveError.Error())
		}
	}
	return result, nil
}

func quoteDateCoverage(signals []Signal, tradingDate string) (stale, current int) {
	for _, signal := range signals {
		if len(signal.QuoteTime) < len(calendarDateLayout) {
			continue
		}
		date := signal.QuoteTime[:len(calendarDateLayout)]
		if date == tradingDate {
			current++
		} else {
			stale++
		}
	}
	return stale, current
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
	regime := marketregime.Insufficient
	if hasIndicators {
		regime = indicators.marketRegime
	}
	riskMultiplier, riskOverlayReason, riskAvailable := marketRiskOverlay(regime)
	riskAdjustedScore := math.Round(score*riskMultiplier*10) / 10
	state = StateWeak
	if riskAdjustedScore >= 72 {
		state = StateTriggered
	} else if riskAdjustedScore >= minimumRealtimePortfolioScore {
		state = StateWatching
	}
	if coverage < minimumFactorCoverage {
		state = StateInvalid
	}
	if !riskAvailable {
		state = StateInvalid
	}
	calibration := scanner.calibrationSnapshot()
	calibratedScore, calibratedRiskAdjustedScore := 0.0, 0.0
	calibratedState := SignalState("")
	if calibration.ID != "" {
		if value, calibratedCoverage, _ := compositeScoreWithWeights(components, calibration.ComponentWeights); calibratedCoverage >= minimumFactorCoverage {
			calibratedScore = math.Round(value*10) / 10
			calibratedRiskAdjustedScore = math.Round(value*riskMultiplier*10) / 10
			calibratedState = StateWeak
			if calibratedRiskAdjustedScore >= 72 {
				calibratedState = StateTriggered
			} else if calibratedRiskAdjustedScore >= calibration.MinimumScore {
				calibratedState = StateWatching
			}
			if !riskAvailable {
				calibratedState = StateInvalid
			}
		} else {
			calibratedState = StateInvalid
		}
	}
	price := input.Stock.Price
	triggerPrice, invalidationPrice, dataDate, source := 0.0, 0.0, "", ""
	entryShape, entryShapeLabel, entryShapeScore := "", "", 0.0
	entryShapeEvidence := []string(nil)
	entryShapeTriggerPrice, entryShapeInvalidationPrice := 0.0, 0.0
	if hasIndicators {
		if price <= 0 {
			price = indicators.latest.Close
		}
		entryShape, entryShapeLabel, entryShapeScore, entryShapeEvidence, entryShapeTriggerPrice, entryShapeInvalidationPrice = classifyEntryShape(input, indicators)
		// Keep the original generic references stable for outcome evaluation and
		// shadow execution. Shape-specific levels are exposed separately.
		triggerPrice = math.Max(indicators.prior20High, indicators.ma5)
		invalidationPrice = math.Max(indicators.ma20, indicators.prior20Low)
		dataDate, source = indicators.latest.Date, indicators.latest.Source
	}
	reasons := topReasons(components, contributions, 5)
	risks := signalRisks(input, indicators, hasIndicators)
	if riskOverlayReason != "" {
		risks = append(risks, riskOverlayReason)
	}
	quoteTime := ""
	if input.Quote != nil {
		quoteTime = input.Quote.QuoteTime
	}
	return Signal{
		ID:     input.Now.Format("20060102T150405") + "-" + input.Stock.Symbol,
		Symbol: input.Stock.Symbol, Name: input.Stock.Name, Industry: input.Stock.Industry,
		State: state, Score: math.Round(score*10) / 10,
		RiskAdjustedScore: riskAdjustedScore, RiskMultiplier: riskMultiplier, MarketRegime: string(regime), RiskOverlayReason: riskOverlayReason,
		CalibrationID: calibration.ID, CalibrationReadySamples: calibration.ReadySamples,
		CalibrationMinimumScore: calibration.MinimumScore, CalibratedScore: calibratedScore,
		CalibratedRiskAdjustedScore: calibratedRiskAdjustedScore, CalibratedState: calibratedState,
		Price: price, Percent: input.Stock.Percent, Speed: input.Stock.Speed,
		TriggerPrice: triggerPrice, InvalidationPrice: invalidationPrice,
		EntryShape: entryShape, EntryShapeLabel: entryShapeLabel, EntryShapeScore: entryShapeScore, EntryShapeEvidence: entryShapeEvidence,
		EntryShapeTriggerPrice: entryShapeTriggerPrice, EntryShapeInvalidationPrice: entryShapeInvalidationPrice,
		AsOf: input.Now, QuoteTime: quoteTime, DataDate: dataDate, DataSource: source,
		Components: components, Reasons: reasons, Risks: uniqueStrings(risks, 8), Warnings: uniqueStrings(warnings, 10),
	}
}

// classifyEntryShape adds a human-auditable setup label beside the composite
// score. It intentionally uses completed daily bars plus the current quote and
// never changes component weights or signal state.
func classifyEntryShape(input Snapshot, value indicators) (string, string, float64, []string, float64, float64) {
	price := currentPrice(input, value.latest.Close)
	if price <= 0 {
		return "", "", 0, nil, 0, 0
	}
	type candidate struct {
		id, label string
		score     float64
		evidence  []string
		trigger   float64
		invalid   float64
	}
	candidates := make([]candidate, 0, 6)
	add := func(item candidate) {
		if item.score > 0 {
			item.score = math.Round(math.Min(100, item.score)*10) / 10
			candidates = append(candidates, item)
		}
	}

	if finite(value.prior20High) && value.prior20High > 0 {
		distance := (price/value.prior20High - 1) * 100
		score := 0.0
		evidence := []string{fmt.Sprintf("现价距前20日高点 %+.2f%%", distance)}
		label := "结构突破（量能待确认）"
		if distance >= 0 {
			score += 60
		} else if distance >= -2 {
			score += 35
		}
		if finite(value.volumeRatio) {
			evidence = append(evidence, fmt.Sprintf("量比 %.2f", value.volumeRatio))
			if value.volumeRatio >= 1.2 && value.volumeRatio <= 4 {
				score += 25
				label = "放量突破"
			} else if value.volumeRatio > 4 {
				label = "结构突破（量能过热）"
			} else {
				label = "结构突破（量能偏弱）"
			}
		} else {
			// Without same-window volume evidence this is only a structural
			// breakout candidate, never a confirmed "放量突破" setup.
			score = math.Min(score, 60)
			evidence = append(evidence, "量能数据不足，不能确认放量")
		}
		if price >= value.ma20 {
			score += 15
			evidence = append(evidence, fmt.Sprintf("价格位于MA20 %.2f上方", value.ma20))
		}
		add(candidate{id: "breakout", label: label, score: score, evidence: evidence, trigger: value.prior20High, invalid: math.Max(value.ma20, value.prior20Low)})
	}

	if finite(value.ma20) && value.ma20 > 0 {
		distance := math.Abs(price/value.ma20-1) * 100
		score := 0.0
		evidence := []string{fmt.Sprintf("现价距MA20 %.2f%%", distance)}
		if price >= value.ma20 && distance <= 2 {
			score += 45
		}
		if value.previous.Close <= value.previousMA20 && price >= value.ma20 {
			score += 35
			evidence = append(evidence, "前一完整日收盘位于MA20下方，本次重新收复")
		}
		if value.ma20 > value.ma60 {
			score += 20
			evidence = append(evidence, "MA20高于MA60")
		}
		add(candidate{id: "trend-reclaim", label: "趋势收复", score: score, evidence: evidence, trigger: value.ma20, invalid: value.ma20})

		pullbackScore := 0.0
		pullbackEvidence := []string{fmt.Sprintf("日内低点相对MA20 %.2f%%", (value.latest.Low/value.ma20-1)*100)}
		if value.latest.Low <= value.ma20*1.015 && price >= value.ma20 {
			pullbackScore += 65
		}
		if value.ma20 > value.ma60 {
			pullbackScore += 25
			pullbackEvidence = append(pullbackEvidence, "中期均线保持多头")
		}
		if finite(value.volumeRatio) && value.volumeRatio <= 1.2 {
			pullbackScore += 10
			pullbackEvidence = append(pullbackEvidence, fmt.Sprintf("回踩量比 %.2f，抛压受控", value.volumeRatio))
		}
		add(candidate{id: "ma-pullback", label: "均线回踩", score: pullbackScore, evidence: pullbackEvidence, trigger: value.ma20, invalid: value.ma60})
	}

	if finite(value.return20) {
		score := 0.0
		evidence := []string{fmt.Sprintf("20日收益 %+.2f%%", value.return20)}
		if value.return20 >= 8 && value.return20 <= 50 {
			score += 55
		}
		if price > value.ma20 {
			score += 25
			evidence = append(evidence, "价格保持在MA20上方")
		}
		if !finite(value.rsi14) || value.rsi14 <= 78 {
			score += 20
		} else {
			evidence = append(evidence, fmt.Sprintf("RSI14 %.1f，接近过热区", value.rsi14))
		}
		add(candidate{id: "momentum-continuation", label: "动量延续", score: score, evidence: evidence, trigger: math.Max(value.prior20High, value.ma5), invalid: value.ma20})
	}

	if finite(value.bollingerLower) && value.bollingerLower > 0 && finite(value.rsi14) {
		score := 0.0
		evidence := []string{fmt.Sprintf("低点相对布林下轨 %.2f%%", (value.latest.Low/value.bollingerLower-1)*100), fmt.Sprintf("RSI14 %.1f", value.rsi14)}
		if value.ma20 > value.ma60 && value.latest.Low <= value.bollingerLower*1.02 && price > value.bollingerLower {
			score += 65
		}
		if value.rsi14 <= 45 {
			score += 25
		}
		if price > value.previous.Close {
			score += 10
			evidence = append(evidence, "收盘较前一完整日反弹")
		}
		add(candidate{id: "mean-reversion", label: "均值回归反弹", score: score, evidence: evidence, trigger: value.ma20, invalid: value.prior20Low})
	}

	if finite(value.rangeCompression) && value.rangeCompression > 0 {
		score := 0.0
		evidence := []string{fmt.Sprintf("10日/20日波动区间 %.0f%%", value.rangeCompression*100)}
		label := "波动收缩（量能待确认）"
		if value.rangeCompression <= .6 {
			score += 55
		}
		if price >= value.priorShortHigh {
			score += 30
			evidence = append(evidence, fmt.Sprintf("价格触及短周期高点 %.2f", value.priorShortHigh))
		}
		if finite(value.volumeRatio) && value.volumeRatio >= 1.1 && value.volumeRatio <= 4 {
			score += 15
			label = "波动收缩突破"
			evidence = append(evidence, fmt.Sprintf("量比 %.2f，未达极端爆量", value.volumeRatio))
		} else if finite(value.volumeRatio) {
			label = "波动收缩（量能未确认）"
		} else {
			evidence = append(evidence, "量能数据不足，不能确认突破")
		}
		add(candidate{id: "volatility-squeeze", label: label, score: score, evidence: evidence, trigger: value.priorShortHigh, invalid: value.ma20})
	}

	if len(candidates) == 0 {
		return "", "", 0, nil, 0, 0
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		if candidates[left].score != candidates[right].score {
			return candidates[left].score > candidates[right].score
		}
		return candidates[left].id < candidates[right].id
	})
	best := candidates[0]
	return best.id, best.label, best.score, best.evidence, best.trigger, best.invalid
}

func marketRiskOverlay(regime marketregime.Regime) (float64, string, bool) {
	switch regime {
	case marketregime.Bull:
		return 1, "市场风险覆盖：牛市，保留100%风险预算", true
	case marketregime.Range:
		return .85, "市场风险覆盖：震荡，风险预算降至85%", true
	case marketregime.Bear:
		return .60, "市场风险覆盖：熊市，风险预算降至60%", true
	case marketregime.HighVol:
		return .45, "市场风险覆盖：高波动，风险预算降至45%", true
	default:
		return 1, "市场风险覆盖：基准数据不足，暂停新增组合资格", false
	}
}

// applyCrossSectionOverlay keeps the complete audited signal set while adding
// deterministic same-scan ranking and a conservative portfolio-entry gate.
func applyCrossSectionOverlay(signals []Signal) []Signal {
	result := append([]Signal(nil), signals...)
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].Score != result[right].Score {
			return result[left].Score > result[right].Score
		}
		if result[left].Speed != result[right].Speed {
			return result[left].Speed > result[right].Speed
		}
		return result[left].Symbol < result[right].Symbol
	})
	total := len(result)
	tradableIndexes := make([]int, 0, total)
	for index, signal := range result {
		if comparableSignal(signal) {
			tradableIndexes = append(tradableIndexes, index)
		}
	}
	sort.SliceStable(tradableIndexes, func(left, right int) bool {
		leftSignal, rightSignal := result[tradableIndexes[left]], result[tradableIndexes[right]]
		leftScore, rightScore := leftSignal.RiskAdjustedScore, rightSignal.RiskAdjustedScore
		if leftScore <= 0 {
			leftScore = leftSignal.Score
		}
		if rightScore <= 0 {
			rightScore = rightSignal.Score
		}
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if leftSignal.Score != rightSignal.Score {
			return leftSignal.Score > rightSignal.Score
		}
		if leftSignal.Speed != rightSignal.Speed {
			return leftSignal.Speed > rightSignal.Speed
		}
		return leftSignal.Symbol < rightSignal.Symbol
	})
	tradableTotal := len(tradableIndexes)
	tradableRank := make(map[int]int, tradableTotal)
	for rank, index := range tradableIndexes {
		tradableRank[index] = rank + 1
	}
	for index := range result {
		signal := &result[index]
		signal.CrossSectionRank = index + 1
		signal.CrossSectionTotal = total
		if total <= 1 {
			signal.CrossSectionPercentile = 100
		} else {
			signal.CrossSectionPercentile = math.Round(float64(total-index-1)/float64(total-1)*1000) / 10
		}
		topPercent := math.Ceil(float64(signal.CrossSectionRank) / float64(total) * 100)
		rankingReason := fmt.Sprintf("横截面排名 %d/%d，处于全池前%.0f%%", signal.CrossSectionRank, total, topPercent)
		signal.Reasons = uniqueStrings(append([]string{rankingReason}, signal.Reasons...), 6)
		signal.TradableTotal = tradableTotal
		if rank, ok := tradableRank[index]; ok {
			signal.TradableRank = rank
			if tradableTotal <= 1 {
				signal.TradablePercentile = 100
			} else {
				signal.TradablePercentile = math.Round(float64(tradableTotal-rank)/float64(tradableTotal-1)*1000) / 10
			}
			signal.Reasons = uniqueStrings(append([]string{fmt.Sprintf("可交易池排名 %d/%d", rank, tradableTotal)}, signal.Reasons...), 6)
		}
	}
	for index := range result {
		signal := &result[index]
		signal.PortfolioEligible, signal.PortfolioReason = basePortfolioEligibility(*signal)
	}
	// Rank the complete audited set, but apply concentration limits only after
	// the score/risk gate. This keeps weak or incomplete signals visible in the
	// audit trail without allowing a single industry to consume the whole new
	// capital budget.
	eligibleIndustryCounts := make(map[string]int)
	eligibleIndustryTotal := 0
	for _, signal := range result {
		if !signal.PortfolioEligible {
			continue
		}
		industry := normalizedIndustry(signal.Industry)
		if industry == "" {
			continue
		}
		eligibleIndustryCounts[industry]++
		eligibleIndustryTotal++
	}
	industryCap := 0
	if eligibleIndustryTotal > 0 {
		industryCap = int(math.Ceil(float64(eligibleIndustryTotal) * portfolioIndustryFraction))
		if industryCap < 1 {
			industryCap = 1
		}
	}
	acceptedIndustryCounts := make(map[string]int)
	for index := range result {
		signal := &result[index]
		if !signal.PortfolioEligible || industryCap <= 0 {
			continue
		}
		industry := normalizedIndustry(signal.Industry)
		if industry != "" && eligibleIndustryCounts[industry] > industryCap && acceptedIndustryCounts[industry] >= industryCap {
			signal.PortfolioEligible = false
			signal.PortfolioReason = fmt.Sprintf("行业集中度保护：%s已有%d个更高排名组合候选，本信号不新增同业暴露", industry, industryCap)
			continue
		}
		if industry != "" {
			acceptedIndustryCounts[industry]++
		}
	}
	return result
}

func applyCalibratedOverlay(signals []Signal) []Signal {
	result := append([]Signal(nil), signals...)
	indexes := make([]int, 0, len(result))
	for index := range result {
		signal := &result[index]
		if signal.CalibrationID == "" || signal.CalibratedState == StateInvalid ||
			(signal.CalibratedState != StateTriggered && signal.CalibratedState != StateWatching) ||
			!finite(signal.CalibratedRiskAdjustedScore) || signal.CalibratedRiskAdjustedScore < signal.CalibrationMinimumScore {
			continue
		}
		indexes = append(indexes, index)
	}
	sort.SliceStable(indexes, func(left, right int) bool {
		leftSignal, rightSignal := result[indexes[left]], result[indexes[right]]
		if leftSignal.CalibratedRiskAdjustedScore != rightSignal.CalibratedRiskAdjustedScore {
			return leftSignal.CalibratedRiskAdjustedScore > rightSignal.CalibratedRiskAdjustedScore
		}
		if leftSignal.CalibratedScore != rightSignal.CalibratedScore {
			return leftSignal.CalibratedScore > rightSignal.CalibratedScore
		}
		return leftSignal.Symbol < rightSignal.Symbol
	})
	total := len(indexes)
	industryCounts := make(map[string]int)
	for _, index := range indexes {
		industry := normalizedIndustry(result[index].Industry)
		if industry != "" {
			industryCounts[industry]++
		}
	}
	industryCap := 0
	if total > 0 {
		industryCap = int(math.Ceil(float64(total) * portfolioIndustryFraction))
		if industryCap < 1 {
			industryCap = 1
		}
	}
	acceptedIndustry := make(map[string]int)
	for rank, index := range indexes {
		signal := &result[index]
		signal.CalibratedRank = rank + 1
		signal.CalibratedTotal = total
		if total >= minimumRankingPool {
			maximumRank := int(math.Ceil(float64(total) * portfolioTopFraction))
			if signal.CalibratedRank > maximumRank {
				signal.CalibratedPortfolioReason = fmt.Sprintf("校准池排名 %d/%d，未进入前%.0f%%", signal.CalibratedRank, total, portfolioTopFraction*100)
				continue
			}
		}
		industry := normalizedIndustry(signal.Industry)
		if industry != "" && industryCounts[industry] > industryCap && acceptedIndustry[industry] >= industryCap {
			signal.CalibratedPortfolioReason = fmt.Sprintf("校准行业集中度保护：%s已有%d个更高排名候选", industry, industryCap)
			continue
		}
		if industry != "" {
			acceptedIndustry[industry]++
		}
		signal.CalibratedPortfolioEligible = true
		signal.CalibratedPortfolioReason = fmt.Sprintf("校准分 %.1f、风险调整分 %.1f，校准池排名 %d/%d", signal.CalibratedScore, signal.CalibratedRiskAdjustedScore, signal.CalibratedRank, total)
	}
	return result
}

func portfolioEligibility(signal Signal) (bool, string) {
	return basePortfolioEligibility(signal)
}

func basePortfolioEligibility(signal Signal) (bool, string) {
	if signal.State == StateInvalid {
		return false, "关键因子或市场状态数据不足，不纳入新增仓位"
	}
	if signal.State != StateTriggered && signal.State != StateWatching {
		return false, "风险调整后的信号状态未达到观察门槛"
	}
	if signal.RiskAdjustedScore < minimumRealtimePortfolioScore {
		return false, fmt.Sprintf("风险调整分 %.1f 低于组合门槛 %.0f", signal.RiskAdjustedScore, minimumRealtimePortfolioScore)
	}
	if signal.TradableTotal > 0 {
		if signal.TradableRank == 0 {
			return false, "信号未进入可交易池：关键数据不足或风险覆盖不可用"
		}
		if signal.TradableTotal >= minimumRankingPool {
			maximumRank := int(math.Ceil(float64(signal.TradableTotal) * portfolioTopFraction))
			if signal.TradableRank > maximumRank {
				return false, fmt.Sprintf("可交易池排名 %d/%d，未进入前%.0f%%", signal.TradableRank, signal.TradableTotal, portfolioTopFraction*100)
			}
		}
	} else if signal.CrossSectionTotal >= minimumRankingPool {
		maximumRank := int(math.Ceil(float64(signal.CrossSectionTotal) * portfolioTopFraction))
		if signal.CrossSectionRank > maximumRank {
			return false, fmt.Sprintf("横截面排名 %d/%d，未进入前%.0f%%", signal.CrossSectionRank, signal.CrossSectionTotal, portfolioTopFraction*100)
		}
	}
	if signal.TradableRank > 0 {
		return true, fmt.Sprintf("原始分 %.1f、风险调整分 %.1f，可交易池排名 %d/%d，按风险预算进入组合候选", signal.Score, signal.RiskAdjustedScore, signal.TradableRank, signal.TradableTotal)
	}
	return true, fmt.Sprintf("原始分 %.1f、风险调整分 %.1f，横截面排名 %d/%d，可按风险预算进入组合候选", signal.Score, signal.RiskAdjustedScore, signal.CrossSectionRank, signal.CrossSectionTotal)
}

func comparableSignal(signal Signal) bool {
	if signal.State == StateInvalid || !finite(signal.Score) {
		return false
	}
	// Signals emitted before the risk overlay existed have no overlay fields;
	// preserve their ranking compatibility when their core score is valid.
	if signal.RiskMultiplier > 0 || signal.MarketRegime != "" || signal.RiskAdjustedScore > 0 {
		if !finite(signal.RiskAdjustedScore) || signal.RiskAdjustedScore < minimumRealtimePortfolioScore || signal.RiskMultiplier <= 0 {
			return false
		}
	}
	if len(signal.Components) > 0 {
		available := 0
		for _, component := range signal.Components {
			if component.State != "数据不足" && finite(component.Score) {
				available++
			}
		}
		if float64(available)/float64(len(signal.Components)) < minimumFactorCoverage {
			return false
		}
	}
	return signal.State == StateTriggered || signal.State == StateWatching
}

func normalizedIndustry(industry string) string {
	return strings.TrimSpace(industry)
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

func compositeScoreWithWeights(components []Component, weights map[string]float64) (float64, float64, map[string]float64) {
	byKey := make(map[string]Component, len(components))
	for _, item := range components {
		byKey[item.Key] = item
	}
	totalWeight, availableWeight, score := 0.0, 0.0, 0.0
	contributions := make(map[string]float64, len(weights))
	for key, weight := range weights {
		if weight <= 0 || !finite(weight) {
			continue
		}
		totalWeight += weight
		item, found := byKey[key]
		if !found || item.State == "数据不足" || !finite(item.Score) {
			continue
		}
		maximum := item.Maximum
		if maximum <= 0 || !finite(maximum) {
			maximum = 20
		}
		availableWeight += weight
		contribution := weight * clamp(item.Score/maximum, 0, 1) * 100
		score += contribution
		contributions[key] = contribution
	}
	if totalWeight <= 0 || availableWeight <= 0 {
		return 0, 0, contributions
	}
	return score / availableWeight, availableWeight / totalWeight, contributions
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
