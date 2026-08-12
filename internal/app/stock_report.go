package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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
	return domain.StockQuoteSnapshot{
		Symbol: quote.Symbol, Name: quote.Name, Price: parseStockReportNumber(quote.Current), Percent: zeroIfNotFinite(quote.Percent),
		Open: parseStockReportNumber(quote.Open), High: parseStockReportNumber(quote.High), Low: parseStockReportNumber(quote.Low),
		PreviousClose: parseStockReportNumber(quote.PreviousClose), AveragePrice: parseStockReportNumber(quote.AveragePrice),
		VolumeRatio: parseStockReportNumber(quote.VolumeRatio), Turnover: parseStockReportNumber(quote.Turnover),
		Amount: zeroIfNotFinite(quote.Amount), PETTM: parseStockReportNumber(quote.PETTM), PB: parseStockReportNumber(quote.PB),
		MarketCap: zeroIfNotFinite(quote.MarketCap), FloatMarketCap: zeroIfNotFinite(quote.FloatMarketCap),
		LimitUp: parseStockReportNumber(quote.LimitUp), LimitDown: parseStockReportNumber(quote.LimitDown), QuoteTime: quote.QuoteTime,
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
	err           error
}

func (app *App) collectStockReportFacts(ctx context.Context, symbol string, movement *domain.FundMovement, progress stockReportProgress) (domain.StockReportFacts, error) {
	if !market.ValidPrefixedSymbol(symbol) || app.quotes == nil || app.history == nil {
		return domain.StockReportFacts{}, fmt.Errorf("个股研判依赖未初始化或股票代码无效")
	}
	reportStockProgress(progress, "采集行情、资金、板块与舆情线索")
	results := make(chan stockReportFetchResult, 7)
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

	var quotes []domain.Quote
	flows := map[string]domain.FundFlow{}
	var bars []domain.DailyBar
	var boards []domain.BoardFlow
	var dragonTiger domain.DragonTigerSnapshot
	var announcements []domain.MarketAnnouncement
	var news []domain.StockNewsItem
	warnings := make([]string, 0)
	var historyError error
	for range 7 {
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
		}
	}
	if len(bars) == 0 {
		if historyError != nil {
			return domain.StockReportFacts{}, fmt.Errorf("个股日 K 不可用，无法给出有依据的关键点位: %w", historyError)
		}
		return domain.StockReportFacts{}, fmt.Errorf("个股日 K 不可用，无法给出有依据的关键点位")
	}
	reportStockProgress(progress, "计算未复权日K趋势与关键点位")
	technical, err := strategy.AnalyzeTechnical(symbol, bars)
	if err != nil {
		return domain.StockReportFacts{}, err
	}

	generatedAt := time.Now()
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
		facts.Quote = domain.StockQuoteSnapshot{Symbol: symbol, Name: name, Price: technical.Price}
		facts.Warnings = append(facts.Warnings, "实时行情缺失，当前价回退到最近日K收盘")
	}
	if facts.Quote.Name == "" {
		facts.Quote.Name = flows[symbol].Name
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
	}
	if movement != nil && movement.Symbol == symbol {
		copyMovement := *movement
		facts.FundMovement = &copyMovement
	}
	if len(boards) > 6 {
		boards = boards[:6]
	}
	facts.Boards = boards
	if len(dragonTiger.Entries) > 3 {
		dragonTiger.Entries = dragonTiger.Entries[:3]
	}
	facts.DragonTiger = dragonTiger
	reportDate := generatedAt.Format("2006-01-02")
	for _, item := range announcements {
		if item.Date != "" && item.Date > reportDate {
			continue
		}
		facts.Announcements = append(facts.Announcements, item)
		if len(facts.Announcements) == 5 {
			break
		}
	}
	for _, item := range news {
		date := item.Date
		if len(date) >= 10 {
			date = date[:10]
		}
		if date != "" && date > reportDate {
			continue
		}
		facts.News = append(facts.News, item)
		if len(facts.News) == 8 {
			break
		}
	}
	sanitizeStockReportFacts(&facts)
	return facts, nil
}

func sanitizeStockReportFacts(facts *domain.StockReportFacts) {
	technicalValues := []*float64{&facts.Technical.Price, &facts.Technical.MA5, &facts.Technical.MA20,
		&facts.Technical.MA60, &facts.Technical.MACD, &facts.Technical.RSI14, &facts.Technical.VolumeRatio,
		&facts.Technical.High20, &facts.Technical.Low20}
	for _, value := range technicalValues {
		*value = zeroIfNotFinite(*value)
	}
	facts.Fund.Price = zeroIfNotFinite(facts.Fund.Price)
	facts.Fund.Percent = zeroIfNotFinite(facts.Fund.Percent)
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

func renderDeterministicStockReport(facts domain.StockReportFacts, aiError string) string {
	var builder strings.Builder
	quote := facts.Quote
	fmt.Fprintf(&builder, "# %s %s 个股多维研判\n\n", quote.Symbol[2:], quote.Name)
	fmt.Fprintf(&builder, "生成时间：%s\n\n", facts.GeneratedAt.Format("2006-01-02 15:04:05"))
	if aiError != "" {
		fmt.Fprintf(&builder, "> Codex综合暂不可用，以下为确定性数据报告：%s\n\n", aiError)
	}
	fmt.Fprintf(&builder, "## 结论\n\n- 技术方向：%s，当前动作属于“%s”；依据是评分%d、MA5/20/60为%.2f/%.2f/%.2f、20日量比%.2f。\n",
		facts.Technical.Bias, facts.Technical.Action, facts.Technical.Score, facts.Technical.MA5,
		facts.Technical.MA20, facts.Technical.MA60, facts.Technical.VolumeRatio)
	fmt.Fprintf(&builder, "- 当前价%.2f，涨跌%+.2f%%；主力净额%s、净占比%+.2f%%。\n\n",
		quote.Price, quote.Percent, humanReportAmount(facts.Fund.MainNet), facts.Fund.MainRatio)
	builder.WriteString("## 多维论证\n\n")
	fmt.Fprintf(&builder, "- 技术面：%s；支撑%s，压力%s。\n", strings.Join(facts.Technical.Evidence, "；"), facts.Technical.Support, facts.Technical.Resistance)
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
		fmt.Fprintf(&builder, "- 公告线索：%s。\n", facts.Announcements[0].Date+" "+facts.Announcements[0].Title)
	}
	if len(facts.News) > 0 {
		fmt.Fprintf(&builder, "- 舆情线索：%s（%s，%s）。\n", facts.News[0].Title, facts.News[0].Source, facts.News[0].Date)
	}
	builder.WriteString("\n## 关键点位与条件\n\n")
	fmt.Fprintf(&builder, "- 买入观察点：%s。\n- 卖出/减仓观察点：%s。\n- 判断失效：%s。\n- 仓位原则：%s。\n",
		facts.Technical.BuyTrigger, facts.Technical.SellTrigger, facts.Technical.Invalidation, facts.Technical.PositionPlan)
	builder.WriteString("\n## 风险\n\n")
	for _, risk := range stockReportRiskLines(facts) {
		fmt.Fprintf(&builder, "- %s。\n", risk)
	}
	builder.WriteString("\n## 使用边界\n\n- 新闻和公告标题只是线索，关键条款需回到交易所、巨潮或公司正式PDF核验。\n- 点位来自未复权日K的均线与结构高低点，不是保证成交或收益的指令。\n- 报告不触发自动交易。\n")
	return builder.String()
}

func stockReportPrompt(facts domain.StockReportFacts) (string, error) {
	data, err := json.MarshalIndent(facts, "", "  ")
	if err != nil {
		return "", err
	}
	prompt := `你是A股个股研判报告编辑。不要运行命令，不要读取文件，不要搜索网络，只分析下面提供的结构化事实。

硬性要求：
1. 用中文Markdown，言简意赅，不使用表格，总长度控制在1200个汉字左右。
2. 依次输出“一句话结论”“多空论证”“资金与热门板块”“舆情与事件线索”“关键点位与操作条件”“风险点”。
3. 短线和波段方向分别写成偏多、偏空或中性；每个方向必须引用均线、MACD、RSI、量比、结构高低点、资金或板块数据中的至少两项。
4. 买入点只能写成“买入观察点/触发条件”，卖出点只能写成“卖出或减仓观察点”；必须沿用technical.buy_trigger、sell_trigger、invalidation、support和resistance，不得创造新价位。
5. main_net是当日累计主力净额；只有fund_movement存在时才能描述1/3/5分钟增量。资金行为不得写成确定性主力意图。
6. 板块结论必须结合涨跌幅、主力净额、广度和排名，不能只看概念名称；change_rank或flow_rank为0表示未进入已采集的Top100或排名缺失，禁止写成第0名。
7. news_clues是第三方新闻标题，announcements是公告索引标题；只可归纳情绪倾向和待核事项，不得补充标题之外的事实。公告关键条款未经PDF核验时必须说明。
8. facts没有财务数据，不得编造业绩、估值、产品、机构持仓或政策事实。数据缺失时明确降低置信度。
9. 最后明确报告不触发自动交易，不给出无条件买卖指令。

结构化事实JSON：
` + string(data)
	return prompt, nil
}

func (app *App) generateStockReport(ctx context.Context, symbol string, movement *domain.FundMovement, progress stockReportProgress) (domain.GeneratedStockReport, error) {
	facts, err := app.collectStockReportFacts(ctx, symbol, movement, progress)
	if err != nil {
		return domain.GeneratedStockReport{}, err
	}
	report := domain.GeneratedStockReport{
		GeneratedAt: facts.GeneratedAt, Symbol: symbol, Name: facts.Quote.Name, Facts: facts,
	}
	prompt, err := stockReportPrompt(facts)
	if err != nil {
		return report, err
	}
	if app.marketReportAI != nil {
		reportStockProgress(progress, "Codex正在综合个股多维研判")
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
		report.Markdown = renderDeterministicStockReport(facts, report.AIError)
	}
	if app.stockReports == nil {
		return report, fmt.Errorf("个股研判存储未初始化")
	}
	reportStockProgress(progress, "保存个股报告与结构化快照")
	return app.stockReports.Save(report)
}
