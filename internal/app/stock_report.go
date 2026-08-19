package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type stockReportProgress func(string)

func reportStockProgress(callback stockReportProgress, value string) {
	if callback != nil {
		callback(value)
	}
}

func parseStockReportNumber(value string) float64 {
	result, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || !finite(result) {
		return 0
	}
	return result
}

func stockQuoteSnapshot(quote domain.Quote) domain.StockQuoteSnapshot {
	source := strings.TrimSpace(quote.Source)
	if source == "" {
		source = "腾讯Level-1行情"
	}
	return domain.StockQuoteSnapshot{
		Source: source, Symbol: quote.Symbol, Name: quote.Name, Price: parseStockReportNumber(quote.Current), Percent: zeroIfNotFinite(quote.Percent),
		Open: parseStockReportNumber(quote.Open), High: parseStockReportNumber(quote.High), Low: parseStockReportNumber(quote.Low),
		PreviousClose: parseStockReportNumber(quote.PreviousClose), AveragePrice: parseStockReportNumber(quote.AveragePrice),
		VolumeRatio: parseStockReportNumber(quote.VolumeRatio), Turnover: parseStockReportNumber(quote.Turnover),
		Amount: zeroIfNotFinite(quote.Amount), PETTM: parseStockReportNumber(quote.PETTM), PB: parseStockReportNumber(quote.PB),
		MarketCap: zeroIfNotFinite(quote.MarketCap), FloatMarketCap: zeroIfNotFinite(quote.FloatMarketCap),
		LimitUp: parseStockReportNumber(quote.LimitUp), LimitDown: parseStockReportNumber(quote.LimitDown), QuoteTime: quote.QuoteTime,
	}
}

func stockPriceBoundary(quote domain.StockQuoteSnapshot, generatedAt time.Time) domain.StockPriceBoundary {
	tradeDate := ""
	if len(quote.QuoteTime) >= 10 {
		candidate := quote.QuoteTime[:10]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			tradeDate = candidate
		}
	}
	if tradeDate == "" {
		tradeDate = generatedAt.Format("2006-01-02")
	}
	available := quote.LimitUp > 0 && quote.LimitDown > 0 && quote.LimitDown < quote.LimitUp
	return domain.StockPriceBoundary{
		TradeDate: tradeDate, LimitUp: quote.LimitUp, LimitDown: quote.LimitDown,
		Available: available, Source: "腾讯Level-1行情涨跌停字段",
	}
}

type stockReportFetchResult struct {
	kind          string
	quotes        []domain.Quote
	flows         map[string]domain.FundFlow
	bars          []domain.DailyBar
	boards        []domain.BoardFlow
	dragonTiger   domain.DragonTigerSnapshot
	announcements []domain.MarketAnnouncement
	news          []domain.StockNewsItem
	research      []domain.BrokerResearchItem
	err           error
}

func (app *App) collectStockReportFacts(ctx context.Context, symbol string, movement *domain.FundMovement, progress stockReportProgress) (domain.StockReportFacts, error) {
	if !market.ValidPrefixedSymbol(symbol) || app.quotes == nil || app.history == nil {
		return domain.StockReportFacts{}, fmt.Errorf("个股研判依赖未初始化或股票代码无效")
	}
	reportStockProgress(progress, "采集行情、资金、板块与外部信息凭证")
	results := make(chan stockReportFetchResult, 8)
	go func() {
		items, err := app.quotes.Fetch(ctx, quoteRequestSymbols([]string{symbol}))
		results <- stockReportFetchResult{kind: "quotes", quotes: items, err: err}
	}()
	go func() {
		items := map[string]domain.FundFlow{}
		var err error
		if app.flows != nil {
			items, err = app.flows.Fetch(ctx, fundFlowRequestSymbols([]string{symbol}))
		}
		results <- stockReportFetchResult{kind: "flows", flows: items, err: err}
	}()
	go func() {
		items, err := app.history.FetchDailyBars(ctx, symbol)
		results <- stockReportFetchResult{kind: "history", bars: items, err: err}
	}()
	go func() {
		var items []domain.BoardFlow
		var err error
		if app.boards != nil {
			items, err = app.boards.FetchBoards(ctx, symbol)
		}
		results <- stockReportFetchResult{kind: "boards", boards: items, err: err}
	}()
	go func() {
		var item domain.DragonTigerSnapshot
		var err error
		if app.dragonTiger != nil {
			item, err = app.dragonTiger.FetchDragonTiger(ctx, symbol)
		}
		results <- stockReportFetchResult{kind: "dragon_tiger", dragonTiger: item, err: err}
	}()
	go func() {
		var items []domain.MarketAnnouncement
		var err error
		if app.marketScan != nil {
			items, err = app.marketScan.FetchAnnouncements(ctx, []string{symbol}, 30)
		}
		results <- stockReportFetchResult{kind: "announcements", announcements: items, err: err}
	}()
	go func() {
		var items []domain.StockNewsItem
		var err error
		if app.news != nil {
			items, err = app.news.FetchStockNews(ctx, symbol, 10)
		}
		results <- stockReportFetchResult{kind: "news", news: items, err: err}
	}()
	go func() {
		var items []domain.BrokerResearchItem
		var err error
		if app.research != nil {
			now := time.Now()
			items, err = app.research.FetchBrokerResearch(ctx, symbol, now.AddDate(0, 0, -90), now, 5)
		}
		results <- stockReportFetchResult{kind: "broker_research", research: items, err: err}
	}()

	var quotes []domain.Quote
	flows := map[string]domain.FundFlow{}
	var bars []domain.DailyBar
	var boards []domain.BoardFlow
	var dragonTiger domain.DragonTigerSnapshot
	var announcements []domain.MarketAnnouncement
	var news []domain.StockNewsItem
	var research []domain.BrokerResearchItem
	warnings := make([]string, 0)
	var historyError error
	for range 8 {
		result := <-results
		if result.err != nil {
			warnings = append(warnings, result.kind+": "+result.err.Error())
			if result.kind == "history" {
				historyError = result.err
			}
		}
		switch result.kind {
		case "quotes":
			quotes = result.quotes
		case "flows":
			flows = result.flows
		case "history":
			bars = result.bars
		case "boards":
			boards = result.boards
		case "dragon_tiger":
			dragonTiger = result.dragonTiger
		case "announcements":
			announcements = result.announcements
		case "news":
			news = result.news
		case "broker_research":
			research = result.research
		}
	}
	if len(bars) == 0 {
		if historyError != nil {
			return domain.StockReportFacts{}, fmt.Errorf("个股日 K 不可用，无法给出有依据的关键点位: %w", historyError)
		}
		return domain.StockReportFacts{}, fmt.Errorf("个股日 K 不可用，无法给出有依据的关键点位")
	}
	generatedAt := time.Now()
	var unfinishedWarning string
	bars, unfinishedWarning = completedDailyBars(bars, generatedAt)
	if unfinishedWarning != "" {
		warnings = append(warnings, unfinishedWarning)
	}
	reportStockProgress(progress, "计算未复权日K趋势与关键点位")
	technical, err := strategy.AnalyzeTechnical(symbol, bars)
	if err != nil {
		return domain.StockReportFacts{}, err
	}

	facts := domain.StockReportFacts{SchemaVersion: 1, GeneratedAt: generatedAt, Technical: technical, Warnings: warnings}
	quoteBySymbol := make(map[string]domain.Quote, len(quotes))
	for _, quote := range quotes {
		quoteBySymbol[quote.Symbol] = quote
	}
	if quote, ok := quoteBySymbol[symbol]; ok {
		facts.Quote = stockQuoteSnapshot(quote)
	} else {
		name := ""
		if app.names != nil {
			name = app.names.LookupName(symbol)
		}
		facts.Quote = domain.StockQuoteSnapshot{Source: technical.DataSource, Symbol: symbol, Name: name, Price: technical.Price}
		facts.Warnings = append(facts.Warnings, "实时行情缺失，当前价回退到最近日K收盘")
	}
	if facts.Quote.Name == "" {
		facts.Quote.Name = flows[symbol].Name
	}
	if warning := quoteFreshnessWarning(facts.Quote.QuoteTime, generatedAt); warning != "" {
		facts.Warnings = append(facts.Warnings, warning)
	}
	if warning := dailyFreshnessWarning(technical.DataDate, technical.DataSource, generatedAt); warning != "" {
		facts.Warnings = append(facts.Warnings, warning)
	}
	facts.PriceBoundary = stockPriceBoundary(facts.Quote, generatedAt)
	if !facts.PriceBoundary.Available {
		facts.Warnings = append(facts.Warnings, "报价交易日涨跌停价不可用，关键点位不能解释为当日可成交边界")
	}
	indexNames := map[string]string{"sh000001": "上证", "sz399001": "深证", "sz399006": "创业板"}
	for _, indexSymbol := range market.BroadMarketSymbols {
		quote, ok := quoteBySymbol[indexSymbol]
		if !ok {
			continue
		}
		facts.Market = append(facts.Market, domain.StockIndexContext{
			Symbol: indexSymbol, Name: indexNames[indexSymbol], Price: parseStockReportNumber(quote.Current),
			Percent: zeroIfNotFinite(quote.Percent), MainNet: zeroIfNotFinite(flows[indexSymbol].MainNet),
		})
	}
	facts.Fund = flows[symbol]
	if facts.Fund.Symbol == "" {
		facts.Fund.Symbol = symbol
		facts.Warnings = append(facts.Warnings, "个股主力资金快照缺失")
	} else {
		if !finite(facts.Fund.MainNet) {
			facts.Warnings = append(facts.Warnings, "个股当日累计主力净额缺失")
		}
		if !finite(facts.Fund.MainRatio) {
			facts.Warnings = append(facts.Warnings, "个股当日主力净占比缺失")
		}
	}
	if movement != nil && movement.Symbol == symbol {
		copyMovement := *movement
		movementAge := generatedAt.Sub(copyMovement.SampledAt)
		if !copyMovement.SampledAt.IsZero() && (movementAge < 0 || movementAge > 2*time.Minute) {
			facts.Warnings = append(facts.Warnings, fmt.Sprintf(
				"1/3/5分钟资金增量样本采于%s，距报告%.0f分钟，已排除过期增量",
				copyMovement.SampledAt.Format("2006-01-02 15:04:05"), math.Abs(movementAge.Minutes()),
			))
		} else {
			facts.FundMovement = &copyMovement
			if copyMovement.SampledAt.IsZero() {
				facts.Warnings = append(facts.Warnings, "1/3/5分钟资金增量缺少采样时间，时效无法核验")
			}
			if !finite(copyMovement.Delta1Minute) {
				facts.Warnings = append(facts.Warnings, "1分钟主力资金增量样本不足")
			}
			if !finite(copyMovement.Delta3Minutes) {
				facts.Warnings = append(facts.Warnings, "3分钟主力资金增量样本不足")
			}
			if !finite(copyMovement.Delta5Minutes) {
				facts.Warnings = append(facts.Warnings, "5分钟主力资金增量样本不足")
			}
		}
	}
	if len(boards) > 6 {
		boards = boards[:6]
	}
	facts.Boards = boards
	if len(dragonTiger.Entries) > 3 {
		dragonTiger.Entries = dragonTiger.Entries[:3]
	}
	facts.DragonTiger = dragonTiger
	facts.Announcements, _ = filterRecentAnnouncements(announcements, generatedAt, 5)
	var contextNews int
	facts.News, _, contextNews = classifyStockNews(news, symbol, facts.Quote.Name, facts.Boards, generatedAt, 5)
	for index := range facts.News {
		if facts.News[index].Symbol == "" {
			facts.News[index].Symbol = symbol
		}
		if facts.News[index].Name == "" {
			facts.News[index].Name = facts.Quote.Name
		}
	}
	filteredResearch, _ := filterRecentResearch(research, generatedAt, 5)
	facts.Evidence = buildEvidenceSnapshot(generatedAt, facts.Announcements, facts.News, filteredResearch)
	if contextNews > 0 {
		facts.Evidence.Warnings = append(facts.Evidence.Warnings,
			fmt.Sprintf("已过滤%d条既未直接提及%s、也未匹配所属行业的市场名单资讯", contextNews, facts.Quote.Name))
	}
	sanitizeStockReportFacts(&facts)
	facts.Warnings = sortedWarnings(facts.Warnings)
	return facts, nil
}

func sanitizeStockReportFacts(facts *domain.StockReportFacts) {
	quoteValues := []*float64{&facts.Quote.Price, &facts.Quote.Percent, &facts.Quote.Open, &facts.Quote.High,
		&facts.Quote.Low, &facts.Quote.PreviousClose, &facts.Quote.AveragePrice, &facts.Quote.VolumeRatio,
		&facts.Quote.Turnover, &facts.Quote.Amount, &facts.Quote.PETTM, &facts.Quote.PB,
		&facts.Quote.MarketCap, &facts.Quote.FloatMarketCap, &facts.Quote.LimitUp, &facts.Quote.LimitDown,
		&facts.PriceBoundary.LimitUp, &facts.PriceBoundary.LimitDown}
	for _, value := range quoteValues {
		*value = zeroIfNotFinite(*value)
	}
	facts.PriceBoundary.Available = facts.PriceBoundary.LimitUp > 0 && facts.PriceBoundary.LimitDown > 0 &&
		facts.PriceBoundary.LimitDown < facts.PriceBoundary.LimitUp
	technicalValues := []*float64{&facts.Technical.Price, &facts.Technical.MA5, &facts.Technical.MA20,
		&facts.Technical.MA60, &facts.Technical.MACD, &facts.Technical.RSI14, &facts.Technical.VolumeRatio,
		&facts.Technical.High20, &facts.Technical.Low20}
	for _, value := range technicalValues {
		*value = zeroIfNotFinite(*value)
	}
	facts.Fund.Price = zeroIfNotFinite(facts.Fund.Price)
	facts.Fund.Percent = zeroIfNotFinite(facts.Fund.Percent)
	facts.Fund.Speed = zeroIfNotFinite(facts.Fund.Speed)
	facts.Fund.MainNet = zeroIfNotFinite(facts.Fund.MainNet)
	facts.Fund.MainRatio = zeroIfNotFinite(facts.Fund.MainRatio)
	for index := range facts.Boards {
		board := &facts.Boards[index]
		board.Percent = zeroIfNotFinite(board.Percent)
		board.MainNet = zeroIfNotFinite(board.MainNet)
		board.MainRatio = zeroIfNotFinite(board.MainRatio)
		board.Turnover = zeroIfNotFinite(board.Turnover)
		board.LeaderPercent = zeroIfNotFinite(board.LeaderPercent)
	}
	for index := range facts.DragonTiger.Entries {
		entry := &facts.DragonTiger.Entries[index]
		values := []*float64{&entry.ClosePrice, &entry.ChangePercent, &entry.NetAmount, &entry.BuyAmount, &entry.SellAmount,
			&entry.DealAmount, &entry.MarketAmount, &entry.NetRatio, &entry.DealAmountRatio, &entry.Turnover,
			&entry.Next1Percent, &entry.Next2Percent, &entry.Next5Percent, &entry.Next10Percent}
		for _, value := range values {
			*value = zeroIfNotFinite(*value)
		}
	}
	if facts.FundMovement != nil {
		movement := facts.FundMovement
		values := []*float64{&movement.Price, &movement.Percent, &movement.MainNet, &movement.MainRatio,
			&movement.Delta1Minute, &movement.Delta3Minutes, &movement.Delta5Minutes,
			&movement.IndustryNet, &movement.IndustryPercent}
		for _, value := range values {
			*value = zeroIfNotFinite(*value)
		}
	}
}

var explicitYuanPricePattern = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)\s*元`)

var actionablePriceTerms = []string{
	"买入", "卖出", "加仓", "减仓", "止盈", "止损", "突破", "跌破", "站稳", "守住",
	"防守", "压力", "支撑", "目标", "观察位", "关键位", "触发", "确认", "收盘", "盘中",
}

type priceBoundaryViolation struct {
	message string
}

func (err *priceBoundaryViolation) Error() string { return err.message }

func boundarySentenceQualified(sentence string) bool {
	for _, marker := range []string{
		"跨交易日", "后续交易日", "非当日", "非今日", "当日不可", "今日不可",
		"当日无法", "今日无法", "不能作为当日", "不能作为今日", "不是当日", "不是今日",
		"高于当日涨停", "高于今日涨停", "低于当日跌停", "低于今日跌停",
	} {
		if strings.Contains(sentence, marker) {
			return true
		}
	}
	return false
}

func priceOutsideBoundary(value float64, boundary domain.StockPriceBoundary) bool {
	return value > boundary.LimitUp+0.005 || value < boundary.LimitDown-0.005
}

func sentenceUsesActionablePrice(sentence string) bool {
	for _, term := range actionablePriceTerms {
		if strings.Contains(sentence, term) {
			return true
		}
	}
	return false
}

func validatePriceBoundaryText(facts domain.StockReportFacts, text string) error {
	boundary := facts.PriceBoundary
	if !boundary.Available || strings.TrimSpace(text) == "" {
		return nil
	}
	knownLevels := []float64{
		facts.Technical.Price, facts.Technical.MA5, facts.Technical.MA20, facts.Technical.MA60,
		facts.Technical.High20, facts.Technical.Low20,
	}
	sentences := strings.FieldsFunc(text, func(r rune) bool {
		return r == '。' || r == '！' || r == '？' || r == '；' || r == ';' ||
			r == '\n'
	})
	for _, sentence := range sentences {
		if boundarySentenceQualified(sentence) {
			continue
		}
		clauses := strings.FieldsFunc(sentence, func(r rune) bool {
			return r == '，' || r == ',' || r == '：' || r == ':'
		})
		for _, clause := range clauses {
			for _, match := range explicitYuanPricePattern.FindAllStringSubmatch(clause, -1) {
				value, err := strconv.ParseFloat(match[1], 64)
				if err == nil && priceOutsideBoundary(value, boundary) && sentenceUsesActionablePrice(clause) {
					return &priceBoundaryViolation{message: fmt.Sprintf(
						"价位 %.2f 超出 %s 可交易区间 %.2f-%.2f，但未标明为跨交易日结构位",
						value, boundary.TradeDate, boundary.LimitDown, boundary.LimitUp,
					)}
				}
			}
			for _, value := range knownLevels {
				if value <= 0 || !priceOutsideBoundary(value, boundary) {
					continue
				}
				formatted := strconv.FormatFloat(value, 'f', 2, 64)
				if sentenceUsesActionablePrice(clause) && strings.Contains(clause, formatted) {
					return &priceBoundaryViolation{message: fmt.Sprintf(
						"技术位 %.2f 超出 %s 可交易区间 %.2f-%.2f，但未标明为跨交易日结构位",
						value, boundary.TradeDate, boundary.LimitDown, boundary.LimitUp,
					)}
				}
			}
		}
	}
	return nil
}

func prequalifyStockReportFacts(facts domain.StockReportFacts) domain.StockReportFacts {
	facts.Technical.Support = qualifyTechnicalTextForBoundary(facts.Technical.Support, facts)
	facts.Technical.Resistance = qualifyTechnicalTextForBoundary(facts.Technical.Resistance, facts)
	facts.Technical.BuyTrigger = qualifyTechnicalTextForBoundary(facts.Technical.BuyTrigger, facts)
	facts.Technical.SellTrigger = qualifyTechnicalTextForBoundary(facts.Technical.SellTrigger, facts)
	facts.Technical.Invalidation = qualifyTechnicalTextForBoundary(facts.Technical.Invalidation, facts)
	return facts
}

func boundaryLevelTokens(facts domain.StockReportFacts) []string {
	seen := make(map[string]bool)
	tokens := make([]string, 0)
	for _, level := range []float64{
		facts.Technical.Price, facts.Technical.MA5, facts.Technical.MA20, facts.Technical.MA60,
		facts.Technical.High20, facts.Technical.Low20,
	} {
		if level <= 0 || !priceOutsideBoundary(level, facts.PriceBoundary) {
			continue
		}
		for _, token := range []string{strconv.FormatFloat(level, 'f', 2, 64)} {
			if !seen[token] {
				seen[token] = true
				tokens = append(tokens, token)
			}
		}
	}
	sort.SliceStable(tokens, func(i, j int) bool { return len(tokens[i]) > len(tokens[j]) })
	return tokens
}

func autoQualifyPriceBoundaryText(text string, facts domain.StockReportFacts) string {
	if !facts.PriceBoundary.Available || strings.TrimSpace(text) == "" {
		return text
	}
	annotation := "（跨交易日结构位，超出当日涨跌停区间；仅作后续交易日观察，不可作为今日触发）"
	tokens := boundaryLevelTokens(facts)
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		if boundarySentenceQualified(line) || !sentenceUsesActionablePrice(line) {
			continue
		}
		for _, match := range explicitYuanPricePattern.FindAllStringSubmatch(line, -1) {
			value, err := strconv.ParseFloat(match[1], 64)
			if err != nil || !priceOutsideBoundary(value, facts.PriceBoundary) {
				continue
			}
			token := match[0]
			line = strings.ReplaceAll(line, token, token+annotation)
		}
		for _, token := range tokens {
			if !strings.Contains(line, token) || strings.Contains(line, token+annotation) || strings.Contains(line, token+"元"+annotation) {
				continue
			}
			line = strings.ReplaceAll(line, token, token+annotation)
		}
		lines[index] = line
	}
	return strings.Join(lines, "\n")
}

func qualifyTechnicalTextForBoundary(value string, facts domain.StockReportFacts) string {
	boundary := facts.PriceBoundary
	if !boundary.Available || boundarySentenceQualified(value) {
		return value
	}
	for _, level := range []float64{
		facts.Technical.Price, facts.Technical.MA5, facts.Technical.MA20, facts.Technical.MA60,
		facts.Technical.High20, facts.Technical.Low20,
	} {
		if level <= 0 || !priceOutsideBoundary(level, boundary) {
			continue
		}
		formatted := strconv.FormatFloat(level, 'f', 2, 64)
		if strings.Contains(value, formatted) {
			return value + "（跨交易日结构位，超出当日涨跌停区间，不可作为今日触发）"
		}
	}
	return value
}

func priceBoundaryCorrectionPrompt(original, draft string, violation error, facts domain.StockReportFacts) string {
	boundary := facts.PriceBoundary
	return original + fmt.Sprintf(`

上一版草稿未通过价格边界校验：%s
请重新完整作答。%s 的当日跌停价为 %.2f 元、涨停价为 %.2f 元。任何超出区间的技术位只能写成“跨交易日结构位/后续交易日观察位”，不得写成当日、今日、盘中或当日收盘可以完成的买卖、突破、跌破、止盈、止损或确认条件。

不合规草稿（仅用于纠错，不得照抄）：
%s`, violation, boundary.TradeDate, boundary.LimitDown, boundary.LimitUp, draft)
}

func (app *App) synthesizeWithPriceBoundary(ctx context.Context, prompt string, facts domain.StockReportFacts) (string, error) {
	firstContext, cancelFirst := context.WithTimeout(ctx, codexReportTimeout())
	markdown, err := app.marketReportAI.Synthesize(firstContext, prompt)
	cancelFirst()
	if err != nil {
		return "", err
	}
	violation := validatePriceBoundaryText(facts, markdown)
	if violation == nil {
		return markdown, nil
	}
	correctionContext, cancelCorrection := context.WithTimeout(ctx, codexReportTimeout())
	corrected, err := app.marketReportAI.Synthesize(
		correctionContext, priceBoundaryCorrectionPrompt(prompt, markdown, violation, facts),
	)
	cancelCorrection()
	if err != nil {
		return "", err
	}
	if secondViolation := validatePriceBoundaryText(facts, corrected); secondViolation != nil {
		autoCorrected := autoQualifyPriceBoundaryText(corrected, facts)
		if finalViolation := validatePriceBoundaryText(facts, autoCorrected); finalViolation != nil {
			return "", finalViolation
		}
		return autoCorrected, nil
	}
	return corrected, nil
}

func strongestStockBoards(boards []domain.BoardFlow) []domain.BoardFlow {
	result := append([]domain.BoardFlow(nil), boards...)
	sort.SliceStable(result, func(i, j int) bool {
		left := result[i].Percent + math.Max(result[i].MainNet/1e9, -5)
		right := result[j].Percent + math.Max(result[j].MainNet/1e9, -5)
		return left > right
	})
	if len(result) > 3 {
		result = result[:3]
	}
	return result
}

func stockReportRiskLines(facts domain.StockReportFacts) []string {
	risks := make([]string, 0)
	if facts.Technical.RSI14 >= 75 {
		risks = append(risks, fmt.Sprintf("RSI14 %.1f处于过热区，追高风险较高", facts.Technical.RSI14))
	}
	if facts.Technical.Price < facts.Technical.MA20 {
		risks = append(risks, fmt.Sprintf("价格低于MA20 %.2f，趋势修复尚未确认", facts.Technical.MA20))
	}
	if facts.Technical.Bias == "看涨" && facts.Technical.VolumeRatio < 0.8 {
		risks = append(risks, fmt.Sprintf("20日量比仅%.2f，看涨结构缺少量能确认", facts.Technical.VolumeRatio))
	}
	if facts.Fund.MainNet < 0 {
		risks = append(risks, "当日主力资金为净流出，需防范价格与资金背离")
	}
	weakBoards := 0
	for _, board := range facts.Boards {
		if board.Percent < 0 && board.MainNet < 0 && board.RiseCount < board.FallCount {
			risks = append(risks, fmt.Sprintf("关联板块%s%+.2f%%、主力%s、广度%d/%d，板块承接偏弱",
				board.Name, board.Percent, humanReportAmount(board.MainNet), board.RiseCount, board.FallCount))
			weakBoards++
			if weakBoards == 2 {
				break
			}
		}
	}
	for _, item := range facts.Announcements {
		for _, term := range riskAnnouncementTerms {
			if strings.Contains(item.Title, term) {
				risks = append(risks, item.Date+"公告待核: "+item.Title)
				break
			}
		}
	}
	if len(risks) == 0 {
		risks = append(risks, "未发现结构化硬风险不等于无风险，仍需核验公告正文和次日承接")
	}
	return risks
}

func dragonTigerReportLines(snapshot domain.DragonTigerSnapshot) []string {
	if !snapshot.Loaded || len(snapshot.Entries) == 0 {
		return nil
	}
	lines := make([]string, 0, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		direction := "净流入"
		if entry.NetAmount < 0 {
			direction = "净流出"
		}
		lines = append(lines, fmt.Sprintf("%s龙虎榜席位%s%s（上榜原因：%s）",
			entry.TradeDate, direction, humanReportAmount(math.Abs(entry.NetAmount)), strings.TrimSpace(entry.Reason)))
	}
	return lines
}

func renderDeterministicStockReport(facts domain.StockReportFacts, aiError string) string {
	var builder strings.Builder
	quote := facts.Quote
	fmt.Fprintf(&builder, "# %s %s 个股多维研判\n\n", quote.Symbol[2:], quote.Name)
	fmt.Fprintf(&builder, "生成时间：%s\n\n", facts.GeneratedAt.Format("2006-01-02 15:04:05"))
	if aiError != "" {
		fmt.Fprintf(&builder, "> AI综合未采用，以下为确定性数据报告：%s\n\n", aiError)
	}
	fmt.Fprintf(&builder, "## 结论\n\n- 技术方向：%s，当前动作属于“%s”；依据是评分%d、MA5/20/60为%.2f/%.2f/%.2f、20日量比%.2f。\n",
		facts.Technical.Bias, facts.Technical.Action, facts.Technical.Score, facts.Technical.MA5,
		facts.Technical.MA20, facts.Technical.MA60, facts.Technical.VolumeRatio)
	fmt.Fprintf(&builder, "- 当前价%.2f，涨跌%+.2f%%；主力净额%s、净占比%+.2f%%。\n\n",
		quote.Price, quote.Percent, humanReportAmount(facts.Fund.MainNet), facts.Fund.MainRatio)
	if facts.PriceBoundary.Available {
		fmt.Fprintf(&builder, "- %s 当日可交易价格边界：跌停 %.2f 元，涨停 %.2f 元。\n\n",
			facts.PriceBoundary.TradeDate, facts.PriceBoundary.LimitDown, facts.PriceBoundary.LimitUp)
	}
	builder.WriteString("## 多维论证\n\n")
	fmt.Fprintf(&builder, "- 技术面：%s；支撑%s，压力%s。\n", strings.Join(facts.Technical.Evidence, "；"),
		qualifyTechnicalTextForBoundary(facts.Technical.Support, facts),
		qualifyTechnicalTextForBoundary(facts.Technical.Resistance, facts))
	if facts.FundMovement != nil {
		movement := facts.FundMovement
		fmt.Fprintf(&builder, "- 资金：状态%s，1/3/5分钟变化%s/%s/%s。\n", movement.State,
			humanReportAmount(movement.Delta1Minute), humanReportAmount(movement.Delta3Minutes), humanReportAmount(movement.Delta5Minutes))
	} else {
		builder.WriteString("- 资金：仅有当日累计主力快照，缺少1/3/5分钟连续样本。\n")
	}
	for _, board := range strongestStockBoards(facts.Boards) {
		rank := "涨幅排名未进入前100"
		if board.ChangeRank > 0 {
			rank = fmt.Sprintf("涨幅排名%d/%d", board.ChangeRank, board.UniverseSize)
		}
		fmt.Fprintf(&builder, "- 板块：%s%+.2f%%，主力%s，广度%d涨/%d跌，%s。\n",
			board.Name, board.Percent, humanReportAmount(board.MainNet), board.RiseCount, board.FallCount, rank)
	}
	if len(facts.Announcements) > 0 {
		builder.WriteString("- 公告线索：近期有正式披露索引，正文未核验，详见下方信息凭证。\n")
	}
	if len(facts.News) > 0 {
		builder.WriteString("- 舆情线索：仅保留标题直接提及个股或匹配所属行业的新闻，详见下方信息凭证。\n")
	}
	if id := firstEvidenceID(facts.Evidence, domain.EvidenceBrokerResearch); id != "" {
		builder.WriteString("- 专业观点：已采集近期券商研报作为B级观点 [" + id + "]；评级和预测不视为公司事实。\n")
	}
	if lines := dragonTigerReportLines(facts.DragonTiger); len(lines) > 0 {
		fmt.Fprintf(&builder, "- 龙虎榜：%s；这是历史异动日席位统计，不代表当前主力持仓或持续方向。\n", strings.Join(lines, "；"))
	}
	builder.WriteString("\n## 关键点位与条件\n\n")
	fmt.Fprintf(&builder, "- 买入观察点：%s。\n- 卖出/减仓观察点：%s。\n- 判断失效：%s。\n- 仓位原则：%s。\n",
		qualifyTechnicalTextForBoundary(facts.Technical.BuyTrigger, facts),
		qualifyTechnicalTextForBoundary(facts.Technical.SellTrigger, facts),
		qualifyTechnicalTextForBoundary(facts.Technical.Invalidation, facts), facts.Technical.PositionPlan)
	builder.WriteString("\n## 风险\n\n")
	for _, risk := range stockReportRiskLines(facts) {
		fmt.Fprintf(&builder, "- %s。\n", risk)
	}
	builder.WriteString("\n## 使用边界\n\n- 新闻和公告标题只是线索，关键条款需回到交易所、巨潮或公司正式PDF核验。\n- 点位来自未复权日K的均线与结构高低点，不是保证成交或收益的指令。\n- 报告不触发自动交易。\n")
	return builder.String()
}

func stockReportPrompt(facts domain.StockReportFacts) (string, error) {
	facts = prequalifyStockReportFacts(facts)
	data, err := json.MarshalIndent(facts, "", "  ")
	if err != nil {
		return "", err
	}
	prompt := `你是A股个股研判报告编辑。不要运行命令，不要读取文件，不要搜索网络，只分析下面提供的结构化事实。

硬性要求：
1. 用中文Markdown，言简意赅，不使用表格，总长度控制在1000个汉字左右；不要重复顶部已有的数据时效、警告数和Agent成功数。
2. 依次输出“一句话结论”“多空论证”“资金与热门板块”“舆情与事件线索”“关键点位与操作条件”“风险点”。
3. 短线和波段方向分别写成偏多、偏空或中性；每个方向必须引用均线、MACD、RSI、量比、结构高低点、资金或板块数据中的至少两项。
4. 买入点只能写成“买入观察点/触发条件”，卖出点只能写成“卖出或减仓观察点”；必须沿用technical.buy_trigger、sell_trigger、invalidation、support和resistance，不得创造新价位。先写靠近现价的MA5/MA20/MA60触发与防守，20日高低点若距离较远只能写成二级结构确认，不能把远端20日高低点写成唯一或首要买卖点。
5. price_boundary.available为true时，它是报价交易日的硬价格边界：任何高于limit_up或低于limit_down的技术位，只能标成“跨交易日结构位/后续交易日观察位”，不得描述为当日、今日、盘中或当日收盘能够完成的买卖、突破、跌破、止盈、止损或确认条件；必须先明确当日涨停价和跌停价，再解释跨日结构位。available为false时不得自行推算涨跌停价，需说明边界缺失并降低关键点位置信度。
6. main_net是当日累计主力净额；只有fund_movement存在时才能描述1/3/5分钟增量。资金行为不得写成确定性主力意图。
   quote.volume_ratio是行情源的盘中量比，technical.volume_ratio_20d是完整日K成交量相对前20日均量，两者口径不同，引用时必须明确名称，不能混称“量比”。
7. 板块结论必须结合涨跌幅、主力净额、广度和排名，不能只看概念名称；change_rank或flow_rank为0表示未进入已采集的Top100或排名缺失，禁止写成第0名。
8. evidence是冻结的信息凭证。涉及公告、新闻、券商观点的每项判断都必须在句末引用已有的[E##]（即evidence.id），禁止创造不存在的ID。只有kind=official_disclosure、tier=A且verified_body=true才可作为已核验公告事实；当前disclosure_index来自第三方公告索引，只能称待核线索，同一事项的公告、董事会决议和委员会意见要合并成一条叙述。B层券商研报仅是专业观点；C层新闻中只有usage标明“标题直接提及个股”时才可作为个股舆情，行业背景不能推断个股入选、金额或受益，市场名单不得进入正文。任何B/C/D层均不得单独支撑买卖结论。news_clues和announcements只为兼容字段。
9. facts没有财务数据，不得编造业绩、估值、产品、机构持仓或政策事实。数据缺失时明确降低置信度。
   公告标题必须保留关键名词，不得把“超短期融资券”缩写成容易与“融资融券”混淆的“融资券”。
10. dragon_tiger必须逐条写明交易日期和净流入/净流出方向，并说明它是历史异动日席位统计，不代表当前主力持仓或持续方向；不得把不同日期简单相加。
11. 面向用户写自然中文，不输出warnings、fund_movement、main_net、price_boundary等JSON字段名。正文不自行编造来源链接；最后明确报告不触发自动交易，不给出无条件买卖指令。系统会在末尾追加冻结凭证清单。

结构化事实JSON：
` + string(data)
	return prompt, nil
}

func stockReportAIErrorText(err error) string {
	if err == nil {
		return ""
	}
	var boundaryError *priceBoundaryViolation
	if errors.As(err, &boundaryError) {
		return "Codex未能完成当日价格边界校正，已切换为确定性报告"
	}
	return err.Error()
}

func validateStockReportPresentation(markdown string) error {
	for _, forbidden := range []string{
		"warnings", "fund_movement", "main_net", "price_boundary", "news_clues", "dragon_tiger",
	} {
		if strings.Contains(markdown, forbidden) {
			return fmt.Errorf("AI报告暴露内部字段 %s", forbidden)
		}
	}
	for _, duplicated := range []string{"数据采集于", "采集时间：", "Agent成功数", "独立角色均成功"} {
		if strings.Contains(markdown, duplicated) {
			return fmt.Errorf("AI报告重复系统数据时效或角色状态")
		}
	}
	return nil
}

func (app *App) generateStockReport(ctx context.Context, symbol string, movement *domain.FundMovement, progress stockReportProgress) (domain.GeneratedStockReport, error) {
	facts, err := app.collectStockReportFacts(ctx, symbol, movement, progress)
	if err != nil {
		return domain.GeneratedStockReport{}, err
	}
	facts = prequalifyStockReportFacts(facts)
	setSnapshotHash(&facts)
	report := domain.GeneratedStockReport{
		GeneratedAt: facts.GeneratedAt, Symbol: symbol, Name: facts.Quote.Name, Facts: facts,
	}
	prompt, err := stockReportPrompt(facts)
	if err != nil {
		return report, err
	}
	if app.marketReportAI != nil {
		freshness := stockFactsFreshness(facts)
		report.Agents = app.runResearchAgents(ctx, "个股研判", freshness, facts, stockReportAgentRoles, progress)
		if successfulAgentCount(report.Agents) > 0 {
			reportStockProgress(progress, "主Agent校验角色分歧并综合个股研判")
			supervisorPrompt := researchSupervisorPrompt(prompt, "个股研判", freshness, report.Agents)
			markdown, aiError := app.synthesizeWithPriceBoundary(ctx, supervisorPrompt, facts)
			if aiError == nil {
				aiError = validateEvidenceReferences(markdown, facts.Evidence)
			}
			if aiError == nil {
				aiError = validateStockReportPresentation(markdown)
			}
			if aiError == nil {
				report.AIUsed = true
				report.Markdown = markdown
			} else {
				report.AIError = stockReportAIErrorText(aiError)
			}
		} else {
			report.AIError = "多角色Agent均不可用: " + researchFailureSummary(report.Agents)
		}
	}
	if strings.TrimSpace(report.Markdown) == "" {
		report.Markdown = renderDeterministicStockReport(facts, report.AIError)
	}
	report.Markdown = attachEvidenceSection(report.Markdown, facts.Evidence)
	report.Markdown = attachResearchFreshness(report.Markdown, stockFactsFreshness(facts), report.Agents)
	if app.stockReports == nil {
		return report, fmt.Errorf("个股研判存储未初始化")
	}
	reportStockProgress(progress, "保存个股报告与结构化快照")
	return app.stockReports.Save(report)
}
