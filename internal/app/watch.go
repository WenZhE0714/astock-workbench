package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/strategy"
	"github.com/wenzhe/astock-workbench/internal/ui"
)

type watchOptions struct {
	Inputs   []string
	Interval int
	Source   string
	Once     bool
	Depth    bool
	Moyu     bool
	Pinyin   bool
	Color    bool
}

type marketSession struct {
	Label      string
	Poll       bool
	Continuous bool
}

var shanghaiLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func marketSessionAt(now time.Time) marketSession {
	local := now.In(shanghaiLocation)
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
		return marketSession{Label: "休市"}
	}
	minutes := local.Hour()*60 + local.Minute()
	switch {
	case minutes < 9*60+15:
		return marketSession{Label: "未开盘"}
	case minutes < 9*60+25:
		return marketSession{Label: "集合竞价", Poll: true}
	case minutes < 9*60+30:
		return marketSession{Label: "开盘等待", Poll: true}
	case minutes < 11*60+30:
		return marketSession{Label: "交易中", Poll: true, Continuous: true}
	case minutes < 13*60:
		return marketSession{Label: "午间休市"}
	case minutes < 15*60:
		return marketSession{Label: "交易中", Poll: true, Continuous: true}
	default:
		return marketSession{Label: "已收盘"}
	}
}

type watchViewState struct {
	Selected int
	Detail   bool
}

func (state *watchViewState) handle(key terminalKey, count int) (changed, quit bool) {
	if key == terminalKeyQuit {
		return false, true
	}
	if state.Detail {
		if key == terminalKeyBack {
			state.Detail = false
			return true, false
		}
		return false, false
	}
	if count <= 0 {
		return false, false
	}
	oldSelected := state.Selected
	switch key {
	case terminalKeyUp:
		state.Selected--
	case terminalKeyDown:
		state.Selected++
	case terminalKeyPageUp:
		state.Selected -= 10
	case terminalKeyPageDown, terminalKeySpace:
		state.Selected += 10
	case terminalKeyHome:
		state.Selected = 0
	case terminalKeyEnd:
		state.Selected = count - 1
	case terminalKeyEnter:
		state.Detail = true
		return true, false
	default:
		return false, false
	}
	if state.Selected < 0 {
		state.Selected = 0
	}
	if state.Selected >= count {
		state.Selected = count - 1
	}
	return state.Selected != oldSelected, false
}

func envEnabled(value string) bool {
	switch strings.ToLower(value) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func splitInputs(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '，' || r == ' ' || r == '\t' || r == '\n'
	})
}

func isTerminal(output io.Writer) bool {
	file, ok := output.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func terminalWidth(output io.Writer) int {
	width, _ := terminalDimensions(output)
	return width
}

func terminalHeight(output io.Writer) int {
	_, height := terminalDimensions(output)
	return height
}

func terminalDimensions(output io.Writer) (width, height int) {
	width, height, ok := detectedTerminalSize(output)
	if !ok {
		if value, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && value >= 32 {
			width = value
		} else {
			width = 80
		}
		if value, err := strconv.Atoi(os.Getenv("LINES")); err == nil && value >= 2 {
			height = value
		} else {
			height = 24
		}
	}
	// Leave the final physical column unused. Many terminals enable automatic
	// right-margin wrapping, so drawing exactly to that column can create a
	// hidden extra row and break absolute-coordinate repainting.
	if width > 32 {
		width--
	}
	if height < 2 {
		height = 2
	}
	return width, height
}

func parseWatchOptions(arguments []string, terminal bool) (watchOptions, error) {
	result := watchOptions{
		Interval: 1,
		Source:   defaultWatchMarketSource(),
		Color:    terminal && os.Getenv("NO_COLOR") == "",
		Moyu:     envEnabled(os.Getenv("ASTOCK_MOYU")),
		Pinyin:   envEnabled(os.Getenv("ASTOCK_PINYIN")),
	}
	if executableName() == "workmon" || executableName() == "workmon-go" {
		result.Moyu = true
		result.Pinyin = true
	}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "-1", "--once":
			result.Once = true
		case "-i", "--interval":
			if index+1 >= len(arguments) {
				return result, fmt.Errorf("%s 需要秒数", argument)
			}
			value, err := strconv.Atoi(arguments[index+1])
			if err != nil {
				return result, fmt.Errorf("刷新间隔必须是 1–3600 的整数秒")
			}
			result.Interval = value
			index++
		case "--source", "--market-source":
			if index+1 >= len(arguments) {
				return result, fmt.Errorf("%s 需要 http 或 tdx", argument)
			}
			result.Source = arguments[index+1]
			index++
		case "-d", "--depth":
			result.Depth = true
		case "-m", "--moyu":
			result.Moyu = true
		case "-p", "--pinyin", "--moyu-pinyin":
			result.Pinyin = true
			result.Moyu = true
		case "--no-color":
			result.Color = false
		case "--":
			for _, value := range arguments[index+1:] {
				result.Inputs = append(result.Inputs, splitInputs(value)...)
			}
			index = len(arguments)
		default:
			if strings.HasPrefix(argument, "-") {
				return result, fmt.Errorf("未知行情选项 '%s'；运行 astock help 查看用法", argument)
			}
			result.Inputs = append(result.Inputs, splitInputs(argument)...)
		}
	}
	if result.Interval < 1 || result.Interval > 3600 {
		return result, fmt.Errorf("刷新间隔必须是 1–3600 的整数秒")
	}
	if result.Pinyin {
		result.Moyu = true
	}
	if result.Moyu {
		result.Color = false
		result.Depth = false
	}
	return result, nil
}

func quoteCandidates(quotes []domain.Quote) []domain.Candidate {
	result := make([]domain.Candidate, 0, len(quotes))
	for _, item := range quotes {
		if market.AssetKindOf(item.Symbol) == domain.AssetKindSector {
			continue
		}
		result = append(result, domain.Candidate{Symbol: item.Symbol, Name: item.Name})
	}
	return result
}

func quoteRequestSymbols(symbols []string) []string {
	return requestSymbolsWith(symbols, market.QuoteMarketSymbols)
}

func fundFlowRequestSymbols(symbols []string) []string {
	return requestSymbolsWith(symbols, market.BroadMarketSymbols)
}

func requestSymbolsWith(symbols, additional []string) []string {
	result := make([]string, 0, len(symbols)+len(additional))
	for _, symbol := range symbols {
		if market.AssetKindOf(symbol) != domain.AssetKindSector {
			result = append(result, symbol)
		}
	}
	for _, indexSymbol := range additional {
		found := false
		for _, symbol := range result {
			if symbol == indexSymbol {
				found = true
				break
			}
		}
		if !found {
			result = append(result, indexSymbol)
		}
	}
	return result
}

func stockAssetSymbols(symbols []string) []string {
	result := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		if market.AssetKindOf(symbol) != domain.AssetKindSector {
			result = append(result, symbol)
		}
	}
	return result
}

func splitMarketQuotes(quotes []domain.Quote, symbols []string) (stocks, indices []domain.Quote) {
	bySymbol := make(map[string]domain.Quote, len(quotes))
	for _, item := range quotes {
		bySymbol[item.Symbol] = item
	}
	for _, symbol := range symbols {
		if item, ok := bySymbol[symbol]; ok {
			stocks = append(stocks, item)
		} else {
			stocks = append(stocks, emptyAssetQuote(symbol))
		}
	}
	for _, symbol := range market.QuoteMarketSymbols {
		if item, ok := bySymbol[symbol]; ok {
			indices = append(indices, item)
		}
	}
	return stocks, indices
}

func boardAssetQuote(symbol string, flow domain.BoardFlow, pinyin bool) domain.Quote {
	item := emptyAssetQuote(symbol)
	item.Source = "同花顺行业"
	item.Name = flow.Name
	item.TaskName = flow.Name
	if pinyin {
		item.TaskName = storage.ToPinyin(flow.Name)
	}
	item.Percent = flow.Percent
	item.Delta = math.NaN()
	item.Amount = math.NaN()
	if flow.Quote != nil {
		item.Current = boardMetric(flow.Quote.Price, "")
		item.Open = boardMetric(flow.Quote.Open, "")
		item.PreviousClose = boardMetric(flow.Quote.PreviousClose, "")
		item.High = boardMetric(flow.Quote.High, "")
		item.Low = boardMetric(flow.Quote.Low, "")
		item.Delta = flow.Quote.Delta
		item.Amount = flow.Quote.Amount / 1e4
	}
	return item
}

func boardQuotePrice(flow domain.BoardFlow) float64 {
	if flow.Quote == nil {
		return math.NaN()
	}
	return flow.Quote.Price
}

func boardAssetFundFlow(symbol string, flow domain.BoardFlow) domain.FundFlow {
	return domain.FundFlow{
		Symbol: symbol, Name: flow.Name, Industry: flow.Name,
		Price: boardQuotePrice(flow), Percent: flow.Percent,
		Speed: math.NaN(), MainNet: flow.MainNet, MainRatio: flow.MainRatio,
	}
}

func mergeBoardAssetFundFlows(flows map[string]domain.FundFlow, boards map[string]domain.BoardFlow) map[string]domain.FundFlow {
	if flows == nil {
		flows = make(map[string]domain.FundFlow, len(boards))
	}
	for symbol, flow := range boards {
		flows[symbol] = boardAssetFundFlow(symbol, flow)
	}
	return flows
}

func mergeBoardAssetQuotes(quotes []domain.Quote, symbols []string, boards map[string]domain.BoardFlow, pinyin bool) []domain.Quote {
	result := reorderQuotes(quotes, symbols)
	for index := range result {
		if flow, ok := boards[result[index].Symbol]; ok {
			result[index] = boardAssetQuote(result[index].Symbol, flow, pinyin)
		}
	}
	return result
}

func joinWatchStatuses(statuses ...string) string {
	lines := make([]string, 0, len(statuses))
	for _, status := range statuses {
		status = strings.TrimSpace(status)
		if status != "" {
			lines = append(lines, status)
		}
	}
	return strings.Join(lines, "\n")
}

func (app *App) decorateQuotes(quotes []domain.Quote, pinyins *storage.PinyinCache) error {
	if err := app.names.Remember(quoteCandidates(quotes)); err != nil {
		return err
	}
	if pinyins != nil {
		return pinyins.Decorate(quotes)
	}
	for index := range quotes {
		quotes[index].TaskName = quotes[index].Name
	}
	return nil
}

func (app *App) runWatch(ctx context.Context, arguments []string) error {
	options, err := parseWatchOptions(arguments, isTerminal(app.out))
	if err != nil {
		return err
	}
	if err := app.configureMarketSource(options.Source); err != nil {
		return err
	}
	var symbols []string
	groupName := temporaryWatchlistGroup
	if len(options.Inputs) > 0 {
		symbols, err = app.resolver.ResolveMany(ctx, options.Inputs)
	} else {
		var warnings []string
		var groups []storage.WatchlistGroup
		groups, warnings, err = storage.LoadWatchlistGroups(app.paths.WatchlistFile)
		groupName = storage.AllWatchlistGroup
		if len(groups) == 1 {
			groupName = storage.DefaultWatchlistGroup
		}
		symbols = storage.WatchlistSymbols(groups, groupName)
		for _, warning := range warnings {
			fmt.Fprintf(app.errOut, "%s: %s\n", programName, warning)
		}
	}
	if err != nil {
		return err
	}
	marketSymbols := make([]string, 0, len(symbols))
	boardSymbols := make([]string, 0)
	for _, symbol := range symbols {
		if market.AssetKindOf(symbol) == domain.AssetKindSector {
			boardSymbols = append(boardSymbols, symbol)
			continue
		}
		marketSymbols = append(marketSymbols, symbol)
	}
	if len(options.Inputs) > 0 && len(boardSymbols) > 0 && len(marketSymbols) == 0 {
		return app.runBoardSnapshots(ctx, boardSymbols)
	}
	if options.Once && len(boardSymbols) > 0 {
		fmt.Fprintf(app.errOut, "%s: CLI 实时看盘已跳过 %d 个板块；可在 Web 自选中查看板块资金和龙头\n", programName, len(boardSymbols))
		symbols = marketSymbols
	}
	if len(symbols) == 0 && len(options.Inputs) > 0 {
		return fmt.Errorf("没有要显示的证券。直接查看: astock 600519；保存自选: astock add 贵州茅台")
	}
	if len(symbols) > maxStocks {
		return fmt.Errorf("一次最多显示 %d 只股票", maxStocks)
	}
	return app.watchLoop(ctx, symbols, options, groupName)
}

func (app *App) watchLoop(ctx context.Context, symbols []string, options watchOptions, initialGroup string) error {
	var pinyins *storage.PinyinCache
	var err error
	if options.Pinyin {
		pinyins, err = storage.LoadPinyinCache(app.paths.PinyinCacheFile)
		if err != nil {
			return err
		}
	}
	viewOptions := ui.ViewOptions{Depth: options.Depth, Moyu: options.Moyu, Pinyin: options.Pinyin, Color: options.Color}
	requestSymbols := quoteRequestSymbols(symbols)
	if options.Once {
		quotes, fetchError := app.quotes.Fetch(ctx, requestSymbols)
		if fetchError != nil {
			return fmt.Errorf("行情获取失败: %w", fetchError)
		}
		stocks, indices := splitMarketQuotes(quotes, symbols)
		if len(stocks) == 0 {
			return fmt.Errorf("行情获取失败: 未解析到自选股行情")
		}
		if err := app.decorateQuotes(stocks, pinyins); err != nil {
			return err
		}
		flows := map[string]domain.FundFlow{}
		previousAmounts := domain.MarketAmountSnapshot{}
		flowError := ""
		if app.flows != nil {
			flowContext, cancelFlow := context.WithTimeout(ctx, 8*time.Second)
			flows, err = app.flows.Fetch(flowContext, fundFlowRequestSymbols(symbols))
			cancelFlow()
			if err != nil {
				flowError = err.Error()
				flows = map[string]domain.FundFlow{}
			}
		}
		if app.amounts != nil {
			amountContext, cancelAmounts := context.WithTimeout(ctx, 8*time.Second)
			previousAmounts, err = app.amounts.FetchPreviousMarketAmount(amountContext)
			cancelAmounts()
			if err != nil {
				previousAmounts = domain.MarketAmountSnapshot{}
			}
		}
		data := ui.LiveData{
			Quotes: stocks, Symbols: symbols, Indices: indices, Flows: flows, PreviousAmounts: previousAmounts,
			RefreshedAt: time.Now(), MarketStatus: marketSessionAt(time.Now()).Label, FlowError: flowError,
		}
		fmt.Fprintln(app.out, ui.BuildSnapshotFrame(data, viewOptions, terminalWidth(app.out)))
		return nil
	}

	terminal := isTerminal(app.out)
	renderer := ui.NewRenderer(app.out, terminal)
	events, restoreInput, err := startTerminalEvents(ctx, os.Stdin, terminal)
	if err != nil {
		return fmt.Errorf("无法启用终端导航: %w", err)
	}
	defer func() {
		_ = restoreInput()
		renderer.Close()
	}()

	var current []domain.Quote
	var indices []domain.Quote
	flows := map[string]domain.FundFlow{}
	boardFlows := map[string][]domain.BoardFlow{}
	boardAssets := map[string]domain.BoardFlow{}
	dragonTigers := map[string]domain.DragonTigerSnapshot{}
	technicalSignals := map[string]domain.TechnicalSignal{}
	previousAmounts := domain.MarketAmountSnapshot{}
	refreshed := time.Time{}
	message := ""
	flowMessage := ""
	session := marketSessionAt(time.Now())
	viewState := watchViewState{}
	command := watchCommand{}
	sortState := watchSortState{}
	groupChooser := watchGroupChooser{}
	groupAssignment := watchGroupAssignment{}
	marketRanking := watchMarketRanking{}
	marketRankingEpoch := uint64(0)
	fundMonitor := watchFundMonitor{}
	fundMonitorEpoch := uint64(0)
	boardFunds := watchBoardFunds{}
	globalMarkets := watchGlobalMarkets{}
	boardDetail := watchBoardDetail{}
	marketReport := watchMarketReport{}
	marketReportEpoch := uint64(0)
	stockReport := watchStockReport{}
	stockReportEpoch := uint64(0)
	reportHistory := watchReportHistory{}
	aiChat := watchAIChat{}
	aiChatEpoch := uint64(0)
	strategyLab := newWatchStrategyLab()
	currentGroup := initialGroup
	notice := ""
	noticeExpires := time.Time{}
	inputCursorVisible := true
	temporarySymbol := ""
	lastWidth, lastHeight := 0, 0
	setNotice := func(value string) {
		notice = value
		if value == "" {
			noticeExpires = time.Time{}
			return
		}
		noticeExpires = time.Now().Add(5 * time.Second)
	}
	selectedStock := func() (string, string) {
		if viewState.Selected < 0 || viewState.Selected >= len(symbols) {
			return "", ""
		}
		symbol := symbols[viewState.Selected]
		name := app.names.LookupName(symbol)
		for _, quote := range current {
			if quote.Symbol == symbol && quote.Name != "" {
				name = quote.Name
				break
			}
		}
		return symbol, name
	}
	selectedStockReportTarget := func() (string, string) {
		if fundMonitor.viewing && !viewState.Detail {
			if item, ok := fundMonitor.selectedItem(); ok {
				return item.Symbol, item.Name
			}
		}
		if marketRanking.active && !viewState.Detail {
			if item, ok := marketRanking.selectedItem(); ok {
				return item.Symbol, item.Name
			}
		}
		symbol, name := selectedStock()
		if market.AssetKindOf(symbol) == domain.AssetKindSector {
			return "", ""
		}
		return symbol, name
	}
	strategyLabNames := func() map[string]string {
		result := make(map[string]string, len(symbols))
		for _, symbol := range symbols {
			result[symbol] = app.names.LookupName(symbol)
		}
		for _, quote := range current {
			if quote.Name != "" {
				result[quote.Symbol] = quote.Name
			}
		}
		return result
	}
	refreshStrategyLabContext := func() {
		symbol, name := selectedStockReportTarget()
		if market.AssetKindOf(symbol) == domain.AssetKindSector {
			symbol, name = "", ""
		}
		strategyLab.updateContext(symbol, name, currentGroup, stockAssetSymbols(symbols), strategyLabNames())
	}
	status := func() string {
		backgroundStatus := joinWatchStatuses(
			strategyLab.status(options.Moyu),
			aiChat.status(options.Moyu),
			marketReport.status(options.Moyu),
			stockReport.status(options.Moyu),
		)
		foregroundStatus := ""
		if sortState.active {
			symbol, name := selectedStock()
			foregroundStatus = sortState.status(symbol, name, options.Moyu)
		} else if groupAssignment.active {
			foregroundStatus = groupAssignment.status(options.Moyu)
		} else if groupChooser.active {
			foregroundStatus = groupChooser.status(options.Moyu)
		} else if command.active() {
			foregroundStatus = command.status(options.Moyu, inputCursorVisible)
		} else if notice != "" {
			foregroundStatus = notice
		} else if boardFunds.viewing {
			foregroundStatus = boardFunds.status(options.Moyu)
		} else if fundMonitor.viewing && !viewState.Detail {
			foregroundStatus = fundMonitor.status(options.Moyu)
		} else if marketRanking.active && !viewState.Detail {
			foregroundStatus = marketRanking.status(options.Moyu)
		}
		return joinWatchStatuses(foregroundStatus, backgroundStatus)
	}
	controls := func() string {
		if sortState.active {
			return sortState.controls(options.Moyu)
		}
		if groupAssignment.active {
			return groupAssignment.controls(options.Moyu)
		}
		if groupChooser.active {
			return groupChooser.controls(options.Moyu)
		}
		if command.active() {
			return command.controls(options.Moyu)
		}
		if globalMarkets.viewing {
			return globalMarkets.controls(options.Moyu)
		}
		if strategyLab.viewing {
			return strategyLab.controls(options.Moyu)
		}
		if aiChat.viewing {
			return aiChatViewControls(options.Moyu)
		}
		if boardFunds.viewing {
			return boardFunds.controls(options.Moyu)
		}
		if fundMonitor.viewing && !viewState.Detail {
			return fundMonitor.controls(options.Moyu)
		}
		if marketRanking.active && !viewState.Detail {
			return marketRanking.controls(options.Moyu)
		}
		return watchBaseControls(viewState.Detail, options.Moyu)
	}
	render := func(force bool) {
		width, height := terminalDimensions(app.out)
		if !force && width == lastWidth && height == lastHeight {
			return
		}
		if globalMarkets.viewing {
			frame := ui.BuildGlobalMarketsFrame(
				globalMarkets.indices, globalMarkets.refreshedAt, globalMarkets.loading,
				globalMarkets.status(options.Moyu), globalMarkets.controls(options.Moyu),
				width, options.Moyu, options.Color, options.Pinyin,
			)
			renderer.Render(frame, width, height)
			lastWidth, lastHeight = width, height
			return
		}
		if strategyLab.viewing {
			frame := strategyLab.frame(time.Now(), width, options.Moyu, options.Color)
			renderer.Render(frame, width, height)
			lastWidth, lastHeight = width, height
			return
		}
		if boardDetail.viewing {
			frame := ui.BuildBoardDetailFrame(
				boardDetail.flow, boardDetail.leaders, boardDetail.symbol,
				boardDetail.status(options.Moyu), boardDetail.controls(options.Moyu),
				width, options.Moyu, options.Color,
			)
			renderer.Render(frame, width, height)
			lastWidth, lastHeight = width, height
			return
		}
		if aiChat.viewing {
			frame := ui.BuildAIChatFrame(
				aiChat.symbol, aiChat.name, aiChat.turns, aiChatViewControls(options.Moyu), width, options.Moyu,
			)
			renderer.Render(frame, width, height)
			lastWidth, lastHeight = width, height
			return
		}
		if boardFunds.viewing {
			frame := ui.BuildBoardFundDashboardFrame(
				boardFunds.dashboard(), boardFunds.loading, boardFunds.status(options.Moyu),
				boardFunds.controls(options.Moyu), width, options.Moyu, options.Color,
			)
			renderer.Render(frame, width, height)
			lastWidth, lastHeight = width, height
			return
		}
		if reportHistory.viewing {
			frame := reportHistory.frame(width, options.Moyu, options.Color)
			renderer.Render(frame, width, height)
			lastWidth, lastHeight = width, height
			return
		}
		if marketReport.viewing {
			frame := ui.BuildMarketReportFrame(
				marketReport.report, marketReportViewControls(options.Moyu), width, options.Moyu,
			)
			renderer.Render(frame, width, height)
			lastWidth, lastHeight = width, height
			return
		}
		if stockReport.viewing {
			frame := ui.BuildStockReportFrame(
				stockReport.report, marketReportViewControls(options.Moyu), width, options.Moyu,
			)
			renderer.Render(frame, width, height)
			lastWidth, lastHeight = width, height
			return
		}
		var selectedBoards []domain.BoardFlow
		var selectedDragonTiger *domain.DragonTigerSnapshot
		var selectedTechnical *domain.TechnicalSignal
		var rankingKind domain.MarketRankingKind
		var rankingItems []domain.MarketRankingItem
		rankingSelected := 0
		var fundMovements []domain.FundMovement
		if viewState.Detail && viewState.Selected >= 0 && viewState.Selected < len(symbols) {
			symbol := symbols[viewState.Selected]
			selectedBoards = boardFlows[symbol]
			if snapshot, ok := dragonTigers[symbol]; ok {
				selectedDragonTiger = &snapshot
			}
			if signal, ok := technicalSignals[symbol]; ok {
				selectedTechnical = &signal
			}
		}
		if marketRanking.active && !viewState.Detail && !fundMonitor.viewing {
			rankingKind = marketRanking.kind
			rankingItems = marketRanking.items
			rankingSelected = marketRanking.selected
		}
		if fundMonitor.viewing {
			fundMovements = fundMonitor.displayRows(current, marketRanking.items)
		}
		frame := ui.BuildLiveFrame(ui.LiveData{
			Quotes: current, Symbols: symbols, Indices: indices, Flows: flows,
			Boards:          selectedBoards,
			DragonTiger:     selectedDragonTiger,
			Technical:       selectedTechnical,
			PreviousAmounts: previousAmounts,
			RefreshedAt:     refreshed, MarketStatus: session.Label,
			FetchError: message, FlowError: flowMessage,
			Status: status(), Footer: controls(),
			GroupName: currentGroup, GroupCount: len(symbols),
			Selected: viewState.Selected, Detail: viewState.Detail,
			RankingKind: rankingKind, RankingItems: rankingItems, RankingSelected: rankingSelected,
			RankingRefreshedAt: marketRanking.refreshedAt,
			FundMonitorActive:  fundMonitor.viewing, FundMonitorSource: fundMonitor.source,
			FundMonitorCount: len(fundMonitor.symbols),
			FundMovements:    fundMovements, FundMonitorSelected: fundMonitor.selected,
			FundMonitorRefreshedAt:  fundMonitor.refreshedAt,
			FundIndustryRefreshedAt: fundMonitor.industryRefreshedAt,
		}, viewOptions, width, height)
		renderer.Render(frame, width, height)
		lastWidth, lastHeight = width, height
	}
	render(true)

	fetch := func() error {
		quotes, fetchError := app.quotes.Fetch(ctx, requestSymbols)
		if fetchError == nil {
			stocks, marketIndices := splitMarketQuotes(quotes, symbols)
			if len(stocks) == 0 && len(symbols) > 0 {
				fetchError = fmt.Errorf("未解析到自选股行情")
			} else {
				if err := app.decorateQuotes(stocks, pinyins); err != nil {
					return err
				}
				current = mergeBoardAssetQuotes(stocks, symbols, boardAssets, options.Pinyin)
				if len(marketIndices) > 0 {
					indices = marketIndices
				}
				message = ""
				refreshed = time.Now()
			}
		}
		if fetchError != nil {
			if errors.Is(fetchError, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				return context.Canceled
			}
			message = fetchError.Error()
		}
		render(true)
		return nil
	}
	openMarketRanking := func(kind domain.MarketRankingKind) {
		if app.rankings == nil {
			setNotice("个股榜单暂不可用")
			render(true)
			return
		}
		setNotice("正在加载个股榜单…")
		render(true)
		rankingContext, cancelRanking := context.WithTimeout(ctx, 6*time.Second)
		items, rankingError := app.rankings.FetchMarketRanking(rankingContext, kind, 20)
		cancelRanking()
		if rankingError != nil {
			setNotice("榜单加载失败: " + rankingError.Error())
			render(true)
			return
		}
		marketRankingEpoch++
		marketRanking.begin(kind, items)
		setNotice("")
		renderer.ResetViewport()
		render(true)
	}
	if err := fetch(); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	openGroupChooser := func() error {
		groups, warnings, loadError := storage.LoadWatchlistGroups(app.paths.WatchlistFile)
		if loadError != nil {
			return loadError
		}
		groupChooser.begin(groups, currentGroup)
		if len(warnings) > 0 {
			setNotice(warnings[0])
		}
		return nil
	}
	openGroupAssignment := func() error {
		if currentGroup == temporaryWatchlistGroup {
			return fmt.Errorf("临时列表中的股票尚未加入自选")
		}
		symbol, name := selectedStock()
		if symbol == "" || symbol == temporarySymbol {
			return fmt.Errorf("当前股票尚未加入自选")
		}
		groups, warnings, loadError := storage.LoadWatchlistGroups(app.paths.WatchlistFile)
		if loadError != nil {
			return loadError
		}
		groupAssignment.begin(groups, symbol, name)
		if len(warnings) > 0 {
			setNotice(warnings[0])
		}
		return nil
	}
	var prewarmFundMonitor func()
	var prewarmBoardAssets func()
	switchGroup := func(groupName string) error {
		groups, _, loadError := storage.LoadWatchlistGroups(app.paths.WatchlistFile)
		if loadError != nil {
			return loadError
		}
		previousSymbol, _ := selectedStock()
		symbols = storage.WatchlistSymbols(groups, groupName)
		currentGroup = groupName
		requestSymbols = quoteRequestSymbols(symbols)
		viewState.Detail = false
		viewState.Selected = 0
		if fetchError := fetch(); fetchError != nil {
			return fetchError
		}
		for index, symbol := range symbols {
			if symbol == previousSymbol {
				viewState.Selected = index
				break
			}
		}
		if prewarmFundMonitor != nil {
			prewarmFundMonitor()
		}
		if prewarmBoardAssets != nil {
			prewarmBoardAssets()
		}
		renderer.ResetViewport()
		return nil
	}

	type flowResult struct {
		flows map[string]domain.FundFlow
		err   error
	}
	type amountResult struct {
		amounts domain.MarketAmountSnapshot
		err     error
	}
	type globalMarketResult struct {
		indices []domain.GlobalIndex
		err     error
	}
	type boardResult struct {
		symbol string
		boards []domain.BoardFlow
		err    error
	}
	type dragonTigerResult struct {
		symbol   string
		snapshot domain.DragonTigerSnapshot
		err      error
	}
	type technicalResult struct {
		symbol string
		signal domain.TechnicalSignal
		err    error
	}
	type marketRankingResult struct {
		kind  domain.MarketRankingKind
		epoch uint64
		items []domain.MarketRankingItem
		err   error
	}
	type fundMonitorResult struct {
		epoch     uint64
		requestID uint64
		flows     map[string]domain.FundFlow
		err       error
	}
	type industryFlowResult struct {
		epoch      uint64
		requestID  uint64
		industries map[string]domain.BoardFlow
		err        error
	}
	type boardFundDashboardResult struct {
		dashboard domain.BoardFundDashboard
		err       error
	}
	type boardDetailResult struct {
		symbol  string
		flow    domain.BoardFlow
		leaders []domain.MarketStockSnapshot
		err     error
	}
	type boardAssetResult struct {
		symbol string
		flow   domain.BoardFlow
		err    error
	}
	type marketReportResult struct {
		epoch  uint64
		report domain.GeneratedMarketReport
		err    error
	}
	type marketReportProgressResult struct {
		epoch   uint64
		message string
	}
	type stockReportResult struct {
		epoch  uint64
		report domain.GeneratedStockReport
		err    error
	}
	type stockReportProgressResult struct {
		epoch   uint64
		message string
	}
	type aiChatResult struct {
		epoch  uint64
		result aiChatAnswer
		at     time.Time
		err    error
	}
	type aiChatProgressResult struct {
		epoch   uint64
		message string
	}
	type strategyLabResult struct {
		kind         string
		run          backtest.Result
		optimization backtest.OptimizationResult
		continuous   backtest.ContinuousOptimizationResult
		err          error
	}
	type strategyLabProgressResult struct {
		message string
	}
	flowResults := make(chan flowResult, 1)
	amountResults := make(chan amountResult, 1)
	globalMarketResults := make(chan globalMarketResult, 1)
	boardResults := make(chan boardResult, 8)
	dragonTigerResults := make(chan dragonTigerResult, 8)
	technicalResults := make(chan technicalResult, 8)
	marketRankingResults := make(chan marketRankingResult, 1)
	fundMonitorResults := make(chan fundMonitorResult, 4)
	industryFlowResults := make(chan industryFlowResult, 4)
	boardFundDashboardResults := make(chan boardFundDashboardResult, 1)
	boardDetailResults := make(chan boardDetailResult, 1)
	boardAssetResults := make(chan boardAssetResult, maxStocks)
	marketReportResults := make(chan marketReportResult, 1)
	marketReportProgressResults := make(chan marketReportProgressResult, 8)
	stockReportResults := make(chan stockReportResult, 1)
	stockReportProgressResults := make(chan stockReportProgressResult, 8)
	aiChatResults := make(chan aiChatResult, 1)
	aiChatProgressResults := make(chan aiChatProgressResult, 8)
	strategyLabResults := make(chan strategyLabResult, 1)
	strategyLabProgressResults := make(chan strategyLabProgressResult, 8)
	flowRunning := false
	amountRunning := false
	globalMarketRunning := false
	boardRunning := make(map[string]bool)
	boardRefreshed := make(map[string]time.Time)
	dragonTigerRunning := make(map[string]bool)
	dragonTigerRefreshed := make(map[string]time.Time)
	technicalRunning := make(map[string]bool)
	technicalRefreshed := make(map[string]time.Time)
	marketRankingRunning := false
	fundMonitorRunning := false
	fundMonitorRequestID := uint64(0)
	activeFundMonitorRequestID := uint64(0)
	fundMonitorLastStartedAt := time.Time{}
	var cancelFundMonitorRequest context.CancelFunc
	industryFlowRunning := false
	industryFlowRequestID := uint64(0)
	activeIndustryFlowRequestID := uint64(0)
	var cancelIndustryFlowRequest context.CancelFunc
	boardFundDashboardRunning := false
	boardDetailRunning := false
	boardAssetRunning := make(map[string]bool)
	boardAssetRefreshed := make(map[string]time.Time)
	startFlowFetch := func() {
		if app.flows == nil || flowRunning {
			return
		}
		flowRunning = true
		request := fundFlowRequestSymbols(symbols)
		go func() {
			result, fetchError := app.flows.Fetch(ctx, request)
			select {
			case flowResults <- flowResult{flows: result, err: fetchError}:
			case <-ctx.Done():
			}
		}()
	}
	startAmountFetch := func() {
		if app.amounts == nil || amountRunning {
			return
		}
		amountRunning = true
		go func() {
			result, fetchError := app.amounts.FetchPreviousMarketAmount(ctx)
			select {
			case amountResults <- amountResult{amounts: result, err: fetchError}:
			case <-ctx.Done():
			}
		}()
	}
	startGlobalMarketFetch := func() bool {
		if app.globalMarkets == nil || globalMarketRunning {
			return false
		}
		globalMarketRunning = true
		globalMarkets.beginRefresh()
		go func() {
			requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
			result, fetchError := app.globalMarkets.FetchGlobalIndices(requestContext)
			cancel()
			select {
			case globalMarketResults <- globalMarketResult{indices: result, err: fetchError}:
			case <-ctx.Done():
			}
		}()
		return true
	}
	openGlobalMarkets := func() {
		globalMarkets.open()
		setNotice("")
		if app.globalMarkets == nil {
			globalMarkets.fail(fmt.Errorf("外盘指数服务未初始化"))
		} else {
			startGlobalMarketFetch()
		}
		renderer.ResetViewport()
		render(true)
	}
	startBoardFetch := func(symbol string, force bool) {
		if app.boards == nil || symbol == "" || boardRunning[symbol] {
			return
		}
		if !force {
			if refreshedAt, ok := boardRefreshed[symbol]; ok && time.Since(refreshedAt) < 60*time.Second {
				return
			}
		}
		boardRunning[symbol] = true
		go func() {
			result, fetchError := app.boards.FetchBoards(ctx, symbol)
			select {
			case boardResults <- boardResult{symbol: symbol, boards: result, err: fetchError}:
			case <-ctx.Done():
			}
		}()
	}
	startBoardDetailFetch := func(symbol string) {
		if boardDetailRunning || symbol == "" {
			return
		}
		client, ok := app.boards.(market.BoardDetailClient)
		if !ok || client == nil {
			boardDetail.fail(fmt.Errorf("板块查询服务未初始化"))
			return
		}
		boardDetailRunning = true
		go func() {
			requestContext, cancel := context.WithTimeout(ctx, 20*time.Second)
			flow, leaders, fetchError := client.FetchBoard(requestContext, symbol)
			cancel()
			select {
			case boardDetailResults <- boardDetailResult{symbol: symbol, flow: flow, leaders: leaders, err: fetchError}:
			case <-ctx.Done():
			}
		}()
	}
	startBoardAssetFetch := func(force bool) {
		client, ok := app.boards.(market.BoardDetailClient)
		if !ok || client == nil {
			return
		}
		for _, symbol := range symbols {
			if market.AssetKindOf(symbol) != domain.AssetKindSector || boardAssetRunning[symbol] {
				continue
			}
			if !force && time.Since(boardAssetRefreshed[symbol]) < time.Minute {
				continue
			}
			boardAssetRunning[symbol] = true
			go func(symbol string) {
				requestContext, cancel := context.WithTimeout(ctx, 20*time.Second)
				flow, _, fetchError := client.FetchBoard(requestContext, symbol)
				cancel()
				select {
				case boardAssetResults <- boardAssetResult{symbol: symbol, flow: flow, err: fetchError}:
				case <-ctx.Done():
				}
			}(symbol)
		}
	}
	prewarmBoardAssets = func() { startBoardAssetFetch(false) }
	startDragonTigerFetch := func(symbol string, force bool) {
		if app.dragonTiger == nil || symbol == "" || dragonTigerRunning[symbol] {
			return
		}
		if !force {
			if refreshedAt, ok := dragonTigerRefreshed[symbol]; ok && time.Since(refreshedAt) < 30*time.Minute {
				return
			}
		}
		dragonTigerRunning[symbol] = true
		go func() {
			snapshot, fetchError := app.dragonTiger.FetchDragonTiger(ctx, symbol)
			select {
			case dragonTigerResults <- dragonTigerResult{symbol: symbol, snapshot: snapshot, err: fetchError}:
			case <-ctx.Done():
			}
		}()
	}
	startTechnicalFetch := func(symbol string, force bool) {
		if app.history == nil || symbol == "" || technicalRunning[symbol] {
			return
		}
		if !force {
			if refreshedAt, ok := technicalRefreshed[symbol]; ok {
				ttl := 5 * time.Minute
				if technicalSignals[symbol].Status == domain.TechnicalStatusUnavailable {
					ttl = time.Minute
				}
				if time.Since(refreshedAt) < ttl {
					return
				}
			}
		}
		technicalRunning[symbol] = true
		technicalSignals[symbol] = domain.TechnicalSignal{
			Status: domain.TechnicalStatusLoading,
			Symbol: symbol,
		}
		go func() {
			bars, fetchError := app.history.FetchDailyBars(ctx, symbol)
			var signal domain.TechnicalSignal
			if fetchError == nil {
				signal, fetchError = strategy.AnalyzeTechnical(symbol, bars)
			}
			select {
			case technicalResults <- technicalResult{symbol: symbol, signal: signal, err: fetchError}:
			case <-ctx.Done():
			}
		}()
	}
	startMarketRankingFetch := func() {
		if app.rankings == nil || !marketRanking.active || marketRankingRunning || !session.Poll {
			return
		}
		marketRankingRunning = true
		kind := marketRanking.kind
		epoch := marketRankingEpoch
		go func() {
			requestContext, cancel := context.WithTimeout(ctx, 6*time.Second)
			items, fetchError := app.rankings.FetchMarketRanking(requestContext, kind, 20)
			cancel()
			select {
			case marketRankingResults <- marketRankingResult{kind: kind, epoch: epoch, items: items, err: fetchError}:
			case <-ctx.Done():
			}
		}()
	}
	cancelFundMonitorFetches := func() {
		if cancelFundMonitorRequest != nil {
			cancelFundMonitorRequest()
			cancelFundMonitorRequest = nil
		}
		fundMonitorRunning = false
		activeFundMonitorRequestID = 0
		fundMonitorLastStartedAt = time.Time{}
		if cancelIndustryFlowRequest != nil {
			cancelIndustryFlowRequest()
			cancelIndustryFlowRequest = nil
		}
		industryFlowRunning = false
		activeIndustryFlowRequestID = 0
	}
	startFundMonitorFetch := func(force bool) bool {
		if app.flows == nil || !fundMonitor.active || len(fundMonitor.symbols) == 0 || fundMonitorRunning {
			return false
		}
		if !force && !session.Poll {
			return false
		}
		if !force && !fundMonitorLastStartedAt.IsZero() && time.Since(fundMonitorLastStartedAt) < fundMonitorSampleInterval {
			return false
		}
		request := append([]string(nil), fundMonitor.symbols...)
		epoch := fundMonitorEpoch
		fundMonitorRequestID++
		requestID := fundMonitorRequestID
		requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
		cancelFundMonitorRequest = cancel
		fundMonitorRunning = true
		activeFundMonitorRequestID = requestID
		fundMonitorLastStartedAt = time.Now()
		go func() {
			defer cancel()
			result, fetchError := app.flows.Fetch(requestContext, request)
			select {
			case fundMonitorResults <- fundMonitorResult{
				epoch: epoch, requestID: requestID, flows: result, err: fetchError,
			}:
			case <-ctx.Done():
			}
		}()
		return true
	}
	startIndustryFlowFetch := func(force bool) bool {
		if app.industryFlows == nil || !fundMonitor.active || industryFlowRunning {
			return false
		}
		if !force {
			if !session.Poll {
				return false
			}
			if !fundMonitor.industryRefreshedAt.IsZero() && time.Since(fundMonitor.industryRefreshedAt) < fundMonitorIndustryInterval {
				return false
			}
		}
		epoch := fundMonitorEpoch
		industryFlowRequestID++
		requestID := industryFlowRequestID
		requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		cancelIndustryFlowRequest = cancel
		industryFlowRunning = true
		activeIndustryFlowRequestID = requestID
		go func() {
			defer cancel()
			result, fetchError := app.industryFlows.FetchIndustryFlows(requestContext)
			select {
			case industryFlowResults <- industryFlowResult{
				epoch: epoch, requestID: requestID, industries: result, err: fetchError,
			}:
			case <-ctx.Done():
			}
		}()
		return true
	}
	startBoardFundDashboardFetch := func(force bool) bool {
		if boardFundDashboardRunning || !boardFunds.viewing {
			return false
		}
		if !force && !boardFunds.refreshedAt.IsZero() && time.Since(boardFunds.refreshedAt) < boardFundRefreshInterval {
			return false
		}
		boardFundDashboardRunning = true
		boardFunds.beginRefresh()
		go func() {
			requestContext, cancel := context.WithTimeout(ctx, 30*time.Second)
			dashboard, fetchError := app.fetchBoardFundDashboard(requestContext)
			cancel()
			select {
			case boardFundDashboardResults <- boardFundDashboardResult{dashboard: dashboard, err: fetchError}:
			case <-ctx.Done():
			}
		}()
		return true
	}
	openBoardFundDashboard := func() {
		boardFunds.open()
		setNotice("")
		startBoardFundDashboardFetch(true)
		renderer.ResetViewport()
		render(true)
	}
	closeBoardFundDashboard := func() {
		boardFunds.close()
		setNotice("")
		renderer.ResetViewport()
		render(true)
	}
	openFundMonitor := func(source string, rankingKind domain.MarketRankingKind, monitorSymbols []string) {
		monitorSymbols = stockAssetSymbols(monitorSymbols)
		if len(monitorSymbols) == 0 {
			setNotice("当前没有可监视的股票")
			render(true)
			return
		}
		if !fundMonitor.matches(rankingKind, source, monitorSymbols) {
			cancelFundMonitorFetches()
			fundMonitorEpoch++
			if rankingKind == "" {
				fundMonitor.begin(source, monitorSymbols)
			} else {
				fundMonitor.beginRanking(rankingKind, source, monitorSymbols)
			}
		} else {
			fundMonitor.viewing = true
		}
		setNotice("")
		if app.flows == nil {
			fundMonitor.failRefresh(fmt.Errorf("个股资金接口暂不可用"))
		} else {
			startFundMonitorFetch(true)
		}
		if app.industryFlows == nil {
			fundMonitor.failIndustryRefresh(fmt.Errorf("行业资金接口暂不可用"))
		} else {
			startIndustryFlowFetch(true)
		}
		renderer.ResetViewport()
		render(true)
	}
	closeFundMonitor := func() {
		fundMonitor.viewing = false
		setNotice("")
		renderer.ResetViewport()
		render(true)
	}
	prewarmFundMonitor = func() {
		if len(symbols) == 0 {
			return
		}
		source := "自选 · " + currentGroup
		if !fundMonitor.matches("", source, symbols) {
			cancelFundMonitorFetches()
			fundMonitorEpoch++
			fundMonitor.beginHidden(source, symbols)
		}
		if app.flows == nil {
			fundMonitor.failRefresh(fmt.Errorf("个股资金接口暂不可用"))
		} else {
			startFundMonitorFetch(false)
		}
		if app.industryFlows == nil {
			fundMonitor.failIndustryRefresh(fmt.Errorf("行业资金接口暂不可用"))
		} else {
			startIndustryFlowFetch(false)
		}
	}
	startMarketReport := func() {
		if marketReport.generating {
			setNotice("智能报告正在生成，无需重复启动")
			render(true)
			return
		}
		marketReportEpoch++
		epoch := marketReportEpoch
		marketReport.begin()
		setNotice("")
		render(true)
		go func() {
			report, reportError := app.generateMarketReport(ctx, func(message string) {
				select {
				case marketReportProgressResults <- marketReportProgressResult{epoch: epoch, message: message}:
				default:
				}
			})
			select {
			case marketReportResults <- marketReportResult{epoch: epoch, report: report, err: reportError}:
			case <-ctx.Done():
			}
		}()
	}
	openMarketReport := func() {
		if marketReport.generating {
			return
		}
		if app.marketReports == nil {
			setNotice("智能市场报告存储未初始化")
			render(true)
			return
		}
		report, loadError := app.marketReports.LoadLatest()
		if loadError != nil {
			setNotice(loadError.Error() + "；按 s 生成")
			render(true)
			return
		}
		marketReport.open(report)
		setNotice("")
		renderer.ResetViewport()
		render(true)
	}
	openMarketReportHistory := func() {
		if app.marketReports == nil {
			setNotice("智能市场报告存储未初始化")
			render(true)
			return
		}
		items, loadError := app.marketReports.List(0)
		if loadError != nil {
			setNotice("读取市场报告历史失败：" + loadError.Error())
			render(true)
			return
		}
		reportHistory.openMarket(items)
		renderer.ResetViewport()
		render(true)
	}
	startStockReport := func() {
		if stockReport.generating {
			setNotice("个股研判正在生成，无需重复启动")
			render(true)
			return
		}
		symbol, name := selectedStockReportTarget()
		if symbol == "" {
			setNotice("当前没有可研判的股票")
			render(true)
			return
		}
		if app.aiChats != nil && !aiChat.generating && (aiChat.symbol != symbol || len(aiChat.turns) == 0) {
			storedName, turns, loadError := app.aiChats.Load(symbol)
			if loadError != nil {
				setNotice("读取AI咨询历史失败: " + loadError.Error())
				render(true)
				return
			}
			if len(turns) > 0 {
				if storedName != "" {
					name = storedName
				}
				aiChat.hydrate(symbol, name, turns)
			}
		}
		var movement *domain.FundMovement
		if item, ok := fundMonitor.movementFor(symbol); ok {
			copyMovement := item
			movement = &copyMovement
		}
		stockReportEpoch++
		epoch := stockReportEpoch
		stockReport.begin(symbol, name)
		setNotice("")
		render(true)
		go func() {
			report, reportError := app.generateStockReport(ctx, symbol, movement, func(message string) {
				select {
				case stockReportProgressResults <- stockReportProgressResult{epoch: epoch, message: message}:
				default:
				}
			})
			select {
			case stockReportResults <- stockReportResult{epoch: epoch, report: report, err: reportError}:
			case <-ctx.Done():
			}
		}()
	}
	openStockReport := func() {
		if stockReport.generating {
			return
		}
		symbol, _ := selectedStockReportTarget()
		if symbol == "" {
			setNotice("当前没有可查看研判的股票")
			render(true)
			return
		}
		if app.stockReports == nil {
			setNotice("个股研判存储未初始化")
			render(true)
			return
		}
		report, loadError := app.stockReports.LoadLatest(symbol)
		if loadError != nil {
			setNotice(loadError.Error() + "；按 c 生成")
			render(true)
			return
		}
		stockReport.open(report)
		setNotice("")
		renderer.ResetViewport()
		render(true)
	}
	openStockReportHistory := func() {
		symbol := stockReport.report.Symbol
		name := stockReport.report.Name
		if symbol == "" {
			symbol, name = selectedStockReportTarget()
		}
		if symbol == "" {
			setNotice("当前没有可查看研判的股票")
			render(true)
			return
		}
		if app.stockReports == nil {
			setNotice("个股研判存储未初始化")
			render(true)
			return
		}
		items, loadError := app.stockReports.List(symbol, 0)
		if loadError != nil {
			setNotice("读取个股研判历史失败：" + loadError.Error())
			render(true)
			return
		}
		reportHistory.openStock(symbol, name, items)
		renderer.ResetViewport()
		render(true)
	}
	startAIChatQuestion := func(question string) {
		question = strings.TrimSpace(question)
		if question == "" {
			setNotice("请输入要咨询AI的问题")
			render(true)
			return
		}
		if aiChat.generating {
			setNotice("AI问答正在处理，无需重复提交")
			render(true)
			return
		}
		symbol, name := selectedStockReportTarget()
		if symbol == "" {
			setNotice("当前没有可咨询的股票")
			render(true)
			return
		}
		var movement *domain.FundMovement
		if item, ok := fundMonitor.movementFor(symbol); ok {
			copyMovement := item
			movement = &copyMovement
		}
		aiChatEpoch++
		epoch := aiChatEpoch
		var history []domain.AIChatTurn
		if aiChat.symbol == symbol {
			history = aiChat.history()
		}
		aiChat.begin(symbol, name, question)
		setNotice("")
		render(true)
		go func() {
			answer, answerError := app.answerAIChatQuestionDetailed(ctx, symbol, movement, history, question, func(message string) {
				select {
				case aiChatProgressResults <- aiChatProgressResult{epoch: epoch, message: message}:
				default:
				}
			})
			select {
			case aiChatResults <- aiChatResult{epoch: epoch, result: answer, at: time.Now(), err: answerError}:
			case <-ctx.Done():
			}
		}()
	}
	openAIChatOrPrompt := func() {
		if aiChat.generating {
			setNotice("AI问答正在处理，行情继续刷新")
			render(true)
			return
		}
		symbol, name := selectedStockReportTarget()
		if symbol != "" && symbol != aiChat.symbol && app.aiChats != nil {
			storedName, turns, loadError := app.aiChats.Load(symbol)
			if loadError != nil {
				setNotice("读取AI咨询历史失败: " + loadError.Error())
				render(true)
				return
			}
			if len(turns) > 0 {
				if storedName != "" {
					name = storedName
				}
				aiChat.hydrate(symbol, name, turns)
			}
		}
		if symbol != "" && symbol == aiChat.symbol && aiChat.open() {
			setNotice("")
			renderer.ResetViewport()
			render(true)
			return
		}
		command.begin(watchCommandAIChat)
		inputCursorVisible = true
		setNotice("")
		render(true)
	}
	loadStrategyLabHistory := func() {
		backtests, backtestError := app.backtestStore().List(50)
		if backtestError != nil {
			strategyLab.fail(backtestError)
			return
		}
		optimizations, optimizationError := app.optimizationStore().List(50)
		if optimizationError != nil {
			strategyLab.fail(optimizationError)
			return
		}
		strategyLab.backtests = backtests
		strategyLab.optimizations = optimizations
		continuous, continuousError := app.continuousOptimizationStore().List(50)
		if continuousError != nil {
			strategyLab.fail(continuousError)
			return
		}
		strategyLab.continuous = continuous
	}
	openStrategyLab := func(symbol, name, source string, sourceSymbols []string, sourceNames map[string]string) {
		if market.AssetKindOf(symbol) == domain.AssetKindSector {
			symbol, name = "", ""
		}
		if symbol == "" {
			symbol, name = selectedStock()
		}
		if source == "" {
			source = currentGroup
		}
		if len(sourceSymbols) == 0 {
			sourceSymbols = symbols
		}
		sourceSymbols = stockAssetSymbols(sourceSymbols)
		if len(sourceNames) == 0 {
			sourceNames = strategyLabNames()
		}
		strategyLab.updateContext(symbol, name, source, sourceSymbols, sourceNames)
		loadStrategyLabHistory()
		strategyLab.open()
		setNotice("")
		renderer.ResetViewport()
		render(true)
	}
	startStrategyLabBacktest := func() {
		if strategyLab.running {
			setNotice("策略研究正在后台运行")
			render(true)
			return
		}
		refreshStrategyLabContext()
		symbolsForTask, namesForTask, targetError := strategyLab.targets()
		if targetError != nil {
			setNotice(targetError.Error())
			render(true)
			return
		}
		end := dateOnly(time.Now()).AddDate(0, 0, -1)
		period := strategyLabBacktestPeriod(end, strategyLabBacktestYears[strategyLab.backtestYearsIndex])
		strategyLab.begin("backtest", "准备历史日K和确定性撮合")
		setNotice("策略研究已启动，行情继续刷新")
		render(true)
		go func() {
			result, err := app.runStrategyLabBacktest(ctx, strategyLabTask{
				kind: "backtest", symbols: symbolsForTask, names: namesForTask, period: period,
			})
			select {
			case strategyLabResults <- strategyLabResult{kind: "backtest", run: result, err: err}:
			case <-ctx.Done():
			}
		}()
	}
	startStrategyLabOptimization := func() {
		if strategyLab.running {
			setNotice("策略研究正在后台运行")
			render(true)
			return
		}
		refreshStrategyLabContext()
		symbolsForTask, namesForTask, targetError := strategyLab.targets()
		if targetError != nil {
			setNotice(targetError.Error())
			render(true)
			return
		}
		end := dateOnly(time.Now()).AddDate(0, 0, -1)
		train, validate, oos := strategyLabOptimizationPeriods(end, strategyLabSplits[strategyLab.splitIndex])
		strategyLab.begin("optimization", "准备训练/验证候选")
		setNotice("策略优化已启动，行情继续刷新")
		render(true)
		task := strategyLabTask{
			kind: "optimization", symbols: symbolsForTask, names: namesForTask,
			train: train, validate: validate, oos: oos,
			maxCandidates: strategyLabCandidateCounts[strategyLab.candidateIndex],
			minimumTrades: strategyLabMinimumTrades[strategyLab.minimumTradesIndex], useAI: strategyLab.useAI,
		}
		go func() {
			result, err := app.runStrategyLabOptimization(ctx, task, func(message string) {
				select {
				case strategyLabProgressResults <- strategyLabProgressResult{message: message}:
				default:
				}
			})
			select {
			case strategyLabResults <- strategyLabResult{kind: "optimization", optimization: result, err: err}:
			case <-ctx.Done():
			}
		}()
	}
	startStrategyLabContinuous := func() {
		if strategyLab.running {
			setNotice("策略研究正在后台运行")
			render(true)
			return
		}
		refreshStrategyLabContext()
		symbolsForTask, namesForTask, targetError := strategyLab.targets()
		if targetError != nil {
			setNotice(targetError.Error())
			render(true)
			return
		}
		end := dateOnly(time.Now()).AddDate(0, 0, -1)
		strategyLab.begin("continuous", "主Agent准备多Agent候选与滚动窗口")
		setNotice("多Agent持续优化已启动，行情继续刷新")
		render(true)
		options := defaultContinuousOptimizationOptions()
		options.UseAI = strategyLab.useAI
		go func() {
			result, err := app.runContinuousOptimization(ctx, symbolsForTask, namesForTask, end, options, func(message string) {
				select {
				case strategyLabProgressResults <- strategyLabProgressResult{message: message}:
				default:
				}
			})
			select {
			case strategyLabResults <- strategyLabResult{kind: "continuous", continuous: result, err: err}:
			case <-ctx.Done():
			}
		}()
	}
	prewarmFundMonitor()
	prewarmBoardAssets()
	startFlowFetch()
	startAmountFetch()
	executeWatchCommand := func(symbol string) {
		switch command.kind {
		case watchCommandAdd:
			if market.AssetKindOf(symbol) == domain.AssetKindSector {
				inView := false
				for _, value := range symbols {
					if value == symbol {
						inView = true
						break
					}
				}
				var added []bool
				var addError error
				if currentGroup != storage.AllWatchlistGroup && currentGroup != temporaryWatchlistGroup {
					added, addError = storage.AddWatchlistToGroup(app.paths.WatchlistFile, currentGroup, []string{symbol})
				} else {
					added, addError = storage.AddWatchlist(app.paths.WatchlistFile, []string{symbol})
				}
				if addError != nil {
					setNotice("添加板块失败: " + addError.Error())
				} else if len(added) == 0 || !added[0] {
					setNotice("板块已在当前分组中: " + symbol[2:])
				} else {
					setNotice("已添加板块到自选: " + symbol[2:])
				}
				if addError == nil && len(added) > 0 && added[0] && !inView {
					symbols = append(symbols, symbol)
					current = append(current, emptyAssetQuote(symbol))
					requestSymbols = quoteRequestSymbols(symbols)
				}
				command.reset()
				startBoardAssetFetch(true)
				render(true)
				return
			}
			inView := false
			for _, value := range symbols {
				if value == symbol {
					inView = true
					break
				}
			}
			if len(symbols) >= maxStocks && !inView {
				setNotice(fmt.Sprintf("一次最多显示 %d 只股票", maxStocks))
				command.reset()
				render(true)
				return
			}
			var added []bool
			var addError error
			if currentGroup != storage.AllWatchlistGroup && currentGroup != temporaryWatchlistGroup {
				added, addError = storage.AddWatchlistToGroup(app.paths.WatchlistFile, currentGroup, []string{symbol})
			} else {
				added, addError = storage.AddWatchlist(app.paths.WatchlistFile, []string{symbol})
			}
			if addError != nil {
				setNotice("添加失败: " + addError.Error())
			} else if len(added) == 0 || !added[0] {
				setNotice("已在当前分组中: " + symbol[2:])
			} else {
				if !inView {
					symbols = append(symbols, symbol)
				}
				requestSymbols = quoteRequestSymbols(symbols)
				if currentGroup != storage.AllWatchlistGroup && currentGroup != temporaryWatchlistGroup {
					setNotice("已添加到“" + currentGroup + "”: " + symbol[2:])
				} else {
					setNotice("已添加到默认分组: " + symbol[2:])
				}
			}
			command.reset()
			if addError == nil && len(added) > 0 && added[0] {
				if err := fetch(); err != nil && !errors.Is(err, context.Canceled) {
					setNotice("已添加，但刷新失败: " + err.Error())
				}
				if fundMonitor.rankingKind == "" {
					prewarmFundMonitor()
				}
			}
			startFlowFetch()
			render(true)
		case watchCommandJump, watchCommandHistory, watchCommandRanking, watchCommandFundMonitor:
			recordHistory := command.kind != watchCommandRanking && command.kind != watchCommandFundMonitor
			if market.AssetKindOf(symbol) == domain.AssetKindSector {
				boardDetail.open(symbol)
				command.reset()
				startBoardDetailFetch(symbol)
				if recordHistory {
					name := app.names.LookupName(symbol)
					if historyError := storage.RecordViewHistory(app.paths.ViewHistoryFile, symbol, name); historyError != nil {
						boardDetail.refreshErr = "已打开，但保存查看历史失败: " + historyError.Error()
					}
				}
				renderer.ResetViewport()
				render(true)
				return
			}
			opened := false
			if temporarySymbol != "" && temporarySymbol != symbol {
				for index, value := range symbols {
					if value == temporarySymbol {
						symbols = append(symbols[:index], symbols[index+1:]...)
						if index < viewState.Selected {
							viewState.Selected--
						}
						break
					}
				}
				for index, value := range current {
					if value.Symbol == temporarySymbol {
						current = append(current[:index], current[index+1:]...)
						break
					}
				}
				requestSymbols = quoteRequestSymbols(symbols)
				temporarySymbol = ""
			}
			found := -1
			for index, value := range symbols {
				if value == symbol {
					found = index
					break
				}
			}
			if found >= 0 {
				viewState.Selected, viewState.Detail = found, true
				opened = true
				setNotice("")
			} else {
				quotes, quoteError := app.quotes.Fetch(ctx, quoteRequestSymbols([]string{symbol}))
				stocks, marketIndices := splitMarketQuotes(quotes, []string{symbol})
				if quoteError != nil || len(stocks) == 0 {
					if quoteError == nil {
						quoteError = fmt.Errorf("未解析到股票行情")
					}
					setNotice("查看失败: " + quoteError.Error())
				} else if decorateError := app.decorateQuotes(stocks, pinyins); decorateError != nil {
					setNotice("查看失败: " + decorateError.Error())
				} else {
					symbols = append(symbols, symbol)
					current = append(current, stocks[0])
					requestSymbols = quoteRequestSymbols(symbols)
					if len(marketIndices) > 0 {
						indices = marketIndices
					}
					temporarySymbol = symbol
					viewState.Selected, viewState.Detail = len(symbols)-1, true
					opened = true
					setNotice("")
				}
			}
			command.reset()
			startFlowFetch()
			if opened {
				startBoardFetch(symbol, false)
				startDragonTigerFetch(symbol, false)
				startTechnicalFetch(symbol, false)
				if recordHistory {
					_, name := selectedStock()
					if historyError := storage.RecordViewHistory(app.paths.ViewHistoryFile, symbol, name); historyError != nil {
						setNotice("已打开，但保存查看历史失败: " + historyError.Error())
					}
				}
			}
			renderer.ResetViewport()
			render(true)
		}
	}

	quoteTicker := time.NewTicker(time.Duration(options.Interval) * time.Second)
	defer quoteTicker.Stop()
	flowTicker := time.NewTicker(60 * time.Second)
	defer flowTicker.Stop()
	globalMarketTicker := time.NewTicker(globalMarketRefreshInterval)
	defer globalMarketTicker.Stop()
	sessionTicker := time.NewTicker(time.Second)
	defer sessionTicker.Stop()
	resizeTicker := time.NewTicker(250 * time.Millisecond)
	defer resizeTicker.Stop()
	cursorTicker := time.NewTicker(500 * time.Millisecond)
	defer cursorTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if globalMarkets.viewing {
				if event.Key == terminalKeyQuit {
					return nil
				}
				if event.Key == terminalKeyBack {
					globalMarkets.close()
					renderer.ResetViewport()
					render(true)
					continue
				}
				if event.Key == terminalKeyNone && (event.Text == "w" || event.Text == "W") {
					startGlobalMarketFetch()
					render(true)
					continue
				}
				navigateRenderer(renderer, event.Key)
				continue
			}
			if boardDetail.viewing {
				if event.Key == terminalKeyQuit {
					return nil
				}
				if event.Key == terminalKeyBack {
					boardDetail.close()
					renderer.ResetViewport()
					render(true)
					continue
				}
				if event.Key == terminalKeyNone && (event.Text == "w" || event.Text == "W") {
					openGlobalMarkets()
					continue
				}
				navigateRenderer(renderer, event.Key)
				continue
			}
			modalActive := sortState.active || groupAssignment.active || groupChooser.active || command.active()
			if isStrategyLabShortcut(event, modalActive, strategyLab.viewing) {
				symbol, name := selectedStockReportTarget()
				source := currentGroup
				sourceSymbols := stockAssetSymbols(symbols)
				sourceNames := strategyLabNames()
				if fundMonitor.viewing && !viewState.Detail {
					source = fundMonitor.source
					sourceSymbols = sourceSymbols[:0]
					for _, item := range fundMonitor.rows {
						sourceSymbols = append(sourceSymbols, item.Symbol)
						if item.Name != "" {
							sourceNames[item.Symbol] = item.Name
						}
					}
				} else if marketRanking.active && !viewState.Detail {
					source = fundMonitorRankingSource(marketRanking.kind)
					sourceSymbols = sourceSymbols[:0]
					for _, item := range marketRanking.items {
						sourceSymbols = append(sourceSymbols, item.Symbol)
						if item.Name != "" {
							sourceNames[item.Symbol] = item.Name
						}
					}
				}
				aiChat.close()
				marketReport.close()
				stockReport.close()
				reportHistory.close()
				boardFunds.close()
				fundMonitor.viewing = false
				marketRanking.reset()
				openStrategyLab(symbol, name, source, sourceSymbols, sourceNames)
				continue
			}
			if strategyLab.viewing {
				if event.Key == terminalKeyQuit {
					return nil
				}
				if event.Key == terminalKeyBack {
					strategyLab.back()
					renderer.ResetViewport()
					render(true)
					continue
				}
				if strategyLab.view == strategyLabTradeDetail || strategyLab.view == strategyLabOptimizationDetail || strategyLab.view == strategyLabContinuousDetail {
					navigateRenderer(renderer, event.Key)
					continue
				}
				switch event.Key {
				case terminalKeyUp:
					strategyLab.move(-1)
				case terminalKeyDown:
					strategyLab.move(1)
				case terminalKeyPageUp:
					strategyLab.move(-10)
				case terminalKeyPageDown, terminalKeySpace:
					strategyLab.move(10)
				case terminalKeyHome:
					strategyLab.selectEndpoint(false)
				case terminalKeyEnd:
					strategyLab.selectEndpoint(true)
				case terminalKeyEnter:
					switch strategyLab.view {
					case strategyLabMenu:
						switch strategyLab.selected {
						case 0:
							startStrategyLabBacktest()
							continue
						case 1:
							startStrategyLabOptimization()
							continue
						case 2:
							startStrategyLabContinuous()
							continue
						case 3:
							strategyLab.view = strategyLabSettings
						case 4:
							loadStrategyLabHistory()
							strategyLab.view = strategyLabBacktestHistory
						case 5:
							loadStrategyLabHistory()
							strategyLab.view = strategyLabOptimizationHistory
						case 6:
							loadStrategyLabHistory()
							strategyLab.view = strategyLabContinuousHistory
						}
						strategyLab.selected = 0
					case strategyLabSettings:
						strategyLab.cycleSetting()
					case strategyLabBacktestHistory:
						if strategyLab.selected >= 0 && strategyLab.selected < len(strategyLab.backtests) {
							run, loadError := app.backtestStore().Load(strategyLab.backtests[strategyLab.selected].RunID)
							if loadError != nil {
								strategyLab.error = loadError.Error()
							} else {
								strategyLab.run = &run
								strategyLab.view = strategyLabBacktestDetail
								strategyLab.selected = 0
							}
						}
					case strategyLabBacktestDetail:
						if strategyLab.run != nil && strategyLab.selected >= 0 && strategyLab.selected < len(strategyLab.run.Trades) {
							trade := strategyLab.run.Trades[strategyLab.selected]
							strategyLab.trade = &trade
							strategyLab.view = strategyLabTradeDetail
						}
					case strategyLabOptimizationHistory:
						if strategyLab.selected >= 0 && strategyLab.selected < len(strategyLab.optimizations) {
							result, loadError := app.optimizationStore().Load(strategyLab.optimizations[strategyLab.selected].ID)
							if loadError != nil {
								strategyLab.error = loadError.Error()
							} else {
								strategyLab.optimization = &result
								strategyLab.view = strategyLabOptimizationDetail
								strategyLab.selected = 0
							}
						}
					case strategyLabContinuousHistory:
						if strategyLab.selected >= 0 && strategyLab.selected < len(strategyLab.continuous) {
							result, loadError := app.continuousOptimizationStore().Load(strategyLab.continuous[strategyLab.selected].ID)
							if loadError != nil {
								strategyLab.error = loadError.Error()
							} else {
								strategyLab.continuousRun = &result
								strategyLab.view = strategyLabContinuousDetail
								strategyLab.selected = 0
							}
						}
					}
				default:
					continue
				}
				renderer.ResetViewport()
				render(true)
				continue
			}
			if reportHistory.viewing {
				if event.Key == terminalKeyQuit {
					return nil
				}
				if event.Key == terminalKeyBack {
					reportHistory.back()
					renderer.ResetViewport()
					render(true)
					continue
				}
				switch event.Key {
				case terminalKeyUp:
					reportHistory.move(-1)
				case terminalKeyDown:
					reportHistory.move(1)
				case terminalKeyPageUp:
					reportHistory.move(-10)
				case terminalKeyPageDown, terminalKeySpace:
					reportHistory.move(10)
				case terminalKeyHome:
					reportHistory.selectEndpoint(false)
				case terminalKeyEnd:
					reportHistory.selectEndpoint(true)
				case terminalKeyEnter:
					if reportHistory.view == reportHistoryDates {
						reportHistory.openSelectedDate()
					} else if item, ok := reportHistory.selectedReport(); ok {
						if reportHistory.kind == reportHistoryMarket {
							report, loadError := app.marketReports.Load(item.id)
							if loadError != nil {
								reportHistory.error = "读取报告失败：" + loadError.Error()
							} else {
								reportHistory.close()
								marketReport.open(report)
							}
						} else {
							report, loadError := app.stockReports.Load(reportHistory.symbol, item.id)
							if loadError != nil {
								reportHistory.error = "读取报告失败：" + loadError.Error()
							} else {
								reportHistory.close()
								stockReport.open(report)
							}
						}
					}
				default:
					continue
				}
				renderer.ResetViewport()
				render(true)
				continue
			}
			if aiChat.viewing {
				if event.Key == terminalKeyQuit {
					return nil
				}
				if event.Key == terminalKeyBack {
					aiChat.close()
					renderer.ResetViewport()
					render(true)
					continue
				}
				if event.Key == terminalKeyNone && (event.Text == "x" || event.Text == "X") {
					aiChat.close()
					command.begin(watchCommandAIChat)
					inputCursorVisible = true
					renderer.ResetViewport()
					render(true)
					continue
				}
				navigateRenderer(renderer, event.Key)
				continue
			}
			if marketReport.viewing {
				if event.Key == terminalKeyQuit {
					return nil
				}
				if event.Key == terminalKeyBack {
					marketReport.close()
					renderer.ResetViewport()
					render(true)
					continue
				}
				if event.Key == terminalKeyNone && (event.Text == "h" || event.Text == "H") {
					openMarketReportHistory()
					continue
				}
				navigateRenderer(renderer, event.Key)
				continue
			}
			if stockReport.viewing {
				if event.Key == terminalKeyQuit {
					return nil
				}
				if event.Key == terminalKeyBack {
					stockReport.close()
					renderer.ResetViewport()
					render(true)
					continue
				}
				if event.Key == terminalKeyNone && (event.Text == "x" || event.Text == "X") {
					stockReport.close()
					command.begin(watchCommandAIChat)
					inputCursorVisible = true
					renderer.ResetViewport()
					render(true)
					continue
				}
				if event.Key == terminalKeyNone && (event.Text == "h" || event.Text == "H") {
					openStockReportHistory()
					continue
				}
				navigateRenderer(renderer, event.Key)
				continue
			}
			if boardFunds.viewing {
				if event.Key == terminalKeyQuit {
					return nil
				}
				if event.Key == terminalKeyBack {
					closeBoardFundDashboard()
					continue
				}
				if event.Key == terminalKeyNone && (event.Text == "y" || event.Text == "Y") {
					if !startBoardFundDashboardFetch(true) {
						setNotice("板块资金刷新正在进行")
					}
					render(true)
					continue
				}
				if event.Key == terminalKeyNone && (event.Text == "w" || event.Text == "W") {
					openGlobalMarkets()
					continue
				}
				navigateRenderer(renderer, event.Key)
				continue
			}
			modalActive = sortState.active || groupAssignment.active || groupChooser.active || command.active()
			if !modalActive && event.Key == terminalKeyNone {
				switch event.Text {
				case "s", "S":
					startMarketReport()
					continue
				case "r", "R":
					openMarketReport()
					continue
				case "c", "C":
					startStockReport()
					continue
				case "o", "O":
					openStockReport()
					continue
				case "x", "X":
					openAIChatOrPrompt()
					continue
				case "y", "Y":
					openBoardFundDashboard()
					continue
				case "w", "W":
					openGlobalMarkets()
					continue
				}
			}
			if fundMonitor.viewing && !viewState.Detail {
				if event.Key == terminalKeyQuit {
					return nil
				}
				if event.Key == terminalKeyBack {
					closeFundMonitor()
					continue
				}
				if event.Key == terminalKeyNone && (event.Text == "v" || event.Text == "V") {
					started := startFundMonitorFetch(true)
					started = startIndustryFlowFetch(true) || started
					if started {
						setNotice("正在刷新资金雷达")
					} else {
						setNotice("资金刷新正在进行")
					}
					render(true)
					continue
				}
				switch event.Key {
				case terminalKeyUp:
					fundMonitor.move(-1)
				case terminalKeyDown:
					fundMonitor.move(1)
				case terminalKeyPageUp:
					fundMonitor.move(-10)
				case terminalKeyPageDown, terminalKeySpace:
					fundMonitor.move(10)
				case terminalKeyHome:
					fundMonitor.selectIndex(0)
				case terminalKeyEnd:
					fundMonitor.selectIndex(len(fundMonitor.rows) - 1)
				case terminalKeyEnter:
					item, selected := fundMonitor.selectedItem()
					if !selected {
						continue
					}
					command.begin(watchCommandFundMonitor)
					executeWatchCommand(item.Symbol)
					continue
				default:
					continue
				}
				renderer.ResetViewport()
				render(true)
				continue
			}
			if marketRanking.active && !viewState.Detail {
				if event.Key == terminalKeyQuit {
					return nil
				}
				if event.Key == terminalKeyBack {
					marketRankingEpoch++
					marketRanking.reset()
					setNotice("")
					renderer.ResetViewport()
					render(true)
					continue
				}
				if event.Key == terminalKeyNone {
					if event.Text == "v" || event.Text == "V" {
						openFundMonitor(
							fundMonitorRankingSource(marketRanking.kind), marketRanking.kind,
							marketRankingSymbols(marketRanking.items),
						)
						continue
					}
					if kind, ok := marketRankingShortcut(event.Text); ok {
						openMarketRanking(kind)
						continue
					}
				}
				switch event.Key {
				case terminalKeyUp:
					marketRanking.move(-1)
				case terminalKeyDown:
					marketRanking.move(1)
				case terminalKeyPageUp:
					marketRanking.move(-10)
				case terminalKeyPageDown, terminalKeySpace:
					marketRanking.move(10)
				case terminalKeyHome:
					marketRanking.selectIndex(0)
				case terminalKeyEnd:
					marketRanking.selectIndex(len(marketRanking.items) - 1)
				case terminalKeyEnter:
					item, selected := marketRanking.selectedItem()
					if !selected {
						continue
					}
					command.begin(watchCommandRanking)
					executeWatchCommand(item.Symbol)
					continue
				default:
					continue
				}
				renderer.ResetViewport()
				render(true)
				continue
			}
			if sortState.active {
				if event.Key == terminalKeyBack || event.Key == terminalKeyQuit {
					symbols = append([]string(nil), sortState.original...)
					current = reorderQuotes(current, symbols)
					requestSymbols = quoteRequestSymbols(symbols)
					viewState.Selected = sortState.originalSelected
					sortState.reset()
					setNotice("已取消排序")
					render(true)
					continue
				}
				if event.Key == terminalKeyEnter {
					if !sortState.picked {
						sortState.picked = true
						render(true)
						continue
					}
					saveError := storage.SaveWatchlistGroupOrder(app.paths.WatchlistFile, currentGroup, symbols)
					if saveError != nil {
						symbols = append([]string(nil), sortState.original...)
						current = reorderQuotes(current, symbols)
						requestSymbols = quoteRequestSymbols(symbols)
						viewState.Selected = sortState.originalSelected
					}
					sortState.reset()
					if saveError != nil {
						setNotice("排序保存失败: " + saveError.Error())
					} else {
						setNotice("已保存“" + currentGroup + "”分组顺序")
					}
					render(true)
					continue
				}
				target := viewState.Selected
				switch event.Key {
				case terminalKeyUp:
					target--
				case terminalKeyDown:
					target++
				case terminalKeyPageUp:
					target -= 10
				case terminalKeyPageDown, terminalKeySpace:
					target += 10
				case terminalKeyHome:
					target = 0
				case terminalKeyEnd:
					target = len(symbols) - 1
				default:
					continue
				}
				if target < 0 {
					target = 0
				}
				if target >= len(symbols) {
					target = len(symbols) - 1
				}
				if sortState.picked {
					if moveWatchlistSymbol(symbols, viewState.Selected, target) {
						current = reorderQuotes(current, symbols)
						requestSymbols = quoteRequestSymbols(symbols)
					}
				}
				viewState.Selected = target
				renderer.ResetViewport()
				render(true)
				continue
			}
			if groupAssignment.active {
				if event.Key == terminalKeyBack || event.Key == terminalKeyQuit {
					groupAssignment.reset()
					setNotice("已取消分组分配")
					render(true)
					continue
				}
				switch event.Key {
				case terminalKeyUp:
					groupAssignment.move(-1)
				case terminalKeyDown:
					groupAssignment.move(1)
				case terminalKeyPageUp:
					groupAssignment.move(-5)
				case terminalKeyPageDown:
					groupAssignment.move(5)
				case terminalKeyHome:
					groupAssignment.selected = 0
				case terminalKeyEnd:
					groupAssignment.selected = len(groupAssignment.groups) - 1
				case terminalKeySpace:
					groupAssignment.toggle()
				case terminalKeyEnter:
					symbol := groupAssignment.symbol
					selectedGroups := groupAssignment.selectedGroups()
					usedDefaultFallback := len(selectedGroups) == 0
					assignedGroups, assignError := storage.SetWatchlistSymbolGroups(
						app.paths.WatchlistFile, symbol, selectedGroups,
					)
					groupAssignment.reset()
					if assignError != nil {
						setNotice("分组分配失败: " + assignError.Error())
					} else {
						assignNotice := "已分配到: " + strings.Join(assignedGroups, "、")
						if usedDefaultFallback {
							assignNotice = "未选择分组，已保留在默认分组"
						}
						if groupError := switchGroup(currentGroup); groupError != nil {
							assignNotice += "，但刷新失败: " + groupError.Error()
						}
						setNotice(assignNotice)
						startFlowFetch()
					}
					renderer.ResetViewport()
					render(true)
					continue
				default:
					continue
				}
				render(true)
				continue
			}
			if groupChooser.active {
				if event.Key == terminalKeyBack || event.Key == terminalKeyQuit {
					groupChooser.reset()
					render(true)
					continue
				}
				if event.Key == terminalKeyNone {
					switch event.Text {
					case "n", "N":
						groupChooser.reset()
						command.begin(watchCommandGroupCreate)
						inputCursorVisible = true
						render(true)
						continue
					case "d", "D":
						group, selected := groupChooser.selectedGroup()
						if !selected {
							continue
						}
						if group.Name == storage.AllWatchlistGroup || group.Name == storage.DefaultWatchlistGroup {
							setNotice("不能删除“" + group.Name + "”分组")
							groupChooser.reset()
							render(true)
							continue
						}
						groupChooser.reset()
						command.confirmGroupDelete(group.Name)
						render(true)
						continue
					}
				}
				switch event.Key {
				case terminalKeyUp:
					groupChooser.move(-1)
				case terminalKeyDown:
					groupChooser.move(1)
				case terminalKeyPageUp:
					groupChooser.move(-5)
				case terminalKeyPageDown, terminalKeySpace:
					groupChooser.move(5)
				case terminalKeyHome:
					groupChooser.selected = 0
				case terminalKeyEnd:
					groupChooser.selected = len(groupChooser.groups) - 1
				case terminalKeyEnter:
					group, selected := groupChooser.selectedGroup()
					if !selected {
						continue
					}
					groupChooser.reset()
					if groupError := switchGroup(group.Name); groupError != nil {
						setNotice("切换分组失败: " + groupError.Error())
					} else {
						setNotice("当前分组: " + group.Name)
						startFlowFetch()
					}
				default:
					continue
				}
				render(true)
				continue
			}
			if command.active() {
				if event.Key == terminalKeyBack || (event.Key == terminalKeyQuit && event.Text == "") {
					command.reset()
					setNotice("")
					render(true)
					continue
				}
				if command.confirm {
					if event.Key == terminalKeyEnter || event.Text == "y" || event.Text == "Y" {
						if command.kind == watchCommandGroupDelete {
							groupName := command.name
							deleted, moved, deleteError := storage.DeleteWatchlistGroup(app.paths.WatchlistFile, groupName)
							command.reset()
							if deleteError != nil {
								setNotice("删除分组失败: " + deleteError.Error())
							} else if !deleted {
								setNotice("分组不存在: " + groupName)
							} else {
								targetGroup := currentGroup
								if targetGroup == groupName || targetGroup == temporaryWatchlistGroup {
									targetGroup = storage.AllWatchlistGroup
								}
								message := "已删除分组: " + groupName
								if moved > 0 {
									message += fmt.Sprintf("，%d只独有股票已移到默认", moved)
								}
								if groupError := switchGroup(targetGroup); groupError != nil {
									message += "，但刷新失败: " + groupError.Error()
								}
								setNotice(message)
							}
							startFlowFetch()
							render(true)
							continue
						}
						var removed []bool
						var removeError error
						if currentGroup != storage.AllWatchlistGroup && currentGroup != temporaryWatchlistGroup {
							removed, removeError = storage.RemoveWatchlistFromGroup(app.paths.WatchlistFile, currentGroup, []string{command.symbol})
						} else {
							removed, removeError = storage.RemoveWatchlist(app.paths.WatchlistFile, []string{command.symbol})
						}
						if removeError != nil {
							setNotice("删除失败: " + removeError.Error())
						} else if len(removed) == 0 || !removed[0] {
							setNotice("未找到自选: " + command.symbol[2:])
						} else {
							for index, value := range symbols {
								if value == command.symbol {
									symbols = append(symbols[:index], symbols[index+1:]...)
									if index < viewState.Selected {
										viewState.Selected--
									}
									if viewState.Selected >= len(symbols) {
										viewState.Selected = len(symbols) - 1
									}
									break
								}
							}
							for index, value := range current {
								if value.Symbol == command.symbol {
									current = append(current[:index], current[index+1:]...)
									break
								}
							}
							requestSymbols = quoteRequestSymbols(symbols)
							delete(boardFlows, command.symbol)
							delete(boardRefreshed, command.symbol)
							delete(dragonTigers, command.symbol)
							delete(dragonTigerRefreshed, command.symbol)
							delete(technicalSignals, command.symbol)
							delete(technicalRefreshed, command.symbol)
							if currentGroup != storage.AllWatchlistGroup && currentGroup != temporaryWatchlistGroup {
								setNotice("已从“" + currentGroup + "”移出: " + command.symbol[2:])
							} else {
								setNotice("已从全部分组删除: " + command.symbol[2:])
							}
							if temporarySymbol == command.symbol {
								temporarySymbol = ""
							}
							if viewState.Detail {
								viewState.Detail = false
							}
							if err := fetch(); err != nil && !errors.Is(err, context.Canceled) {
								setNotice("刷新失败: " + err.Error())
							}
							if fundMonitor.rankingKind == "" {
								prewarmFundMonitor()
							}
						}
						command.reset()
						startFlowFetch()
						render(true)
					} else if event.Text == "n" || event.Text == "N" {
						command.reset()
						setNotice("已取消删除")
						render(true)
					}
					continue
				}
				if command.choosing() {
					switch event.Key {
					case terminalKeyUp:
						command.moveCandidate(-1)
					case terminalKeyDown:
						command.moveCandidate(1)
					case terminalKeyPageUp:
						command.moveCandidate(-5)
					case terminalKeyPageDown, terminalKeySpace:
						command.moveCandidate(5)
					case terminalKeyHome:
						command.selectCandidate(0)
					case terminalKeyEnd:
						command.selectCandidate(len(command.candidates) - 1)
					case terminalKeyEnter:
						if candidate, ok := command.selectedCandidate(); ok {
							executeWatchCommand(candidate.Symbol)
							continue
						}
					default:
						continue
					}
					render(true)
					continue
				}
				switch event.Key {
				case terminalKeyBackspace:
					command.buffer = removeLastRune(command.buffer)
					inputCursorVisible = true
					render(true)
				case terminalKeyEnter:
					input := commandText(command.buffer)
					if input == "" {
						if command.kind == watchCommandAIChat {
							setNotice("请输入要咨询AI的问题")
						} else if command.kind == watchCommandGroupCreate {
							setNotice("请输入分组名称")
						} else {
							setNotice("请输入代码或完整名称")
						}
						command.reset()
						render(true)
						continue
					}
					if command.kind == watchCommandAIChat {
						command.reset()
						startAIChatQuestion(input)
						continue
					}
					if command.kind == watchCommandGroupCreate {
						created, createError := storage.CreateWatchlistGroup(app.paths.WatchlistFile, input)
						command.reset()
						if createError != nil {
							setNotice("新建分组失败: " + createError.Error())
						} else if groupError := switchGroup(input); groupError != nil {
							setNotice("分组已创建，切换失败: " + groupError.Error())
						} else if created {
							setNotice("已新建并切换分组: " + input)
							startFlowFetch()
						} else {
							setNotice("分组已存在，已切换: " + input)
							startFlowFetch()
						}
						render(true)
						continue
					}
					symbol, resolveError := app.resolver.Resolve(ctx, input)
					if resolveError != nil {
						var ambiguous *market.AmbiguousNameError
						if errors.As(resolveError, &ambiguous) {
							command.chooseCandidates(input, ambiguous.Candidates)
							inputCursorVisible = false
							renderer.ResetViewport()
							render(true)
							continue
						}
						setNotice(resolveError.Error())
						command.reset()
						render(true)
						continue
					}
					executeWatchCommand(symbol)
				default:
					if event.Text != "" {
						command.buffer += event.Text
						inputCursorVisible = true
						render(true)
					}
				}
				continue
			}
			if event.Key == terminalKeyNone && event.Text != "" {
				switch event.Text {
				case "1", "2", "3":
					if viewState.Detail {
						setNotice("请先按 Esc 返回列表再查看榜单")
						render(true)
						continue
					}
					kind, _ := marketRankingShortcut(event.Text)
					openMarketRanking(kind)
					continue
				case "v", "V":
					if fundMonitor.viewing {
						started := startFundMonitorFetch(true)
						started = startIndustryFlowFetch(true) || started
						if started {
							setNotice("正在刷新资金雷达")
						} else {
							setNotice("资金刷新正在进行")
						}
						render(true)
						continue
					}
					if viewState.Detail {
						setNotice("请先按 Esc 返回列表再打开资金雷达")
						render(true)
						continue
					}
					openFundMonitor("自选 · "+currentGroup, "", symbols)
					continue
				case "a", "A":
					command.begin(watchCommandAdd)
				case "d", "D":
					if len(symbols) == 0 {
						setNotice("当前没有可删除的自选")
						render(true)
						continue
					}
					selected := viewState.Selected
					if selected < 0 {
						selected = 0
					}
					if selected >= len(symbols) {
						selected = len(symbols) - 1
					}
					symbol := symbols[selected]
					if symbol == temporarySymbol {
						setNotice(symbol[2:] + " 尚未加入自选")
						render(true)
						continue
					}
					name := app.names.LookupName(symbol)
					for _, item := range current {
						if item.Symbol == symbol && item.Name != "" {
							name = item.Name
							break
						}
					}
					command.confirmDelete(symbol, name, currentGroup)
				case "i", "I":
					command.begin(watchCommandJump)
				case "h", "H":
					history, historyError := storage.LoadViewHistory(app.paths.ViewHistoryFile)
					if historyError != nil {
						setNotice("读取查看历史失败: " + historyError.Error())
						render(true)
						continue
					}
					if len(history) == 0 {
						setNotice("暂无查看历史")
						render(true)
						continue
					}
					command.begin(watchCommandHistory)
					command.chooseCandidates("", history)
					renderer.ResetViewport()
				case "e", "E":
					if viewState.Detail {
						setNotice("请先按 Esc 返回列表再调整顺序")
						render(true)
						continue
					}
					if currentGroup == storage.AllWatchlistGroup || currentGroup == temporaryWatchlistGroup {
						setNotice("请先按 f 选择具体分组后调整顺序")
						render(true)
						continue
					}
					if len(symbols) < 2 {
						setNotice("当前分组至少需要 2 只股票才能排序")
						render(true)
						continue
					}
					sortState.begin(symbols, viewState.Selected)
					renderer.ResetViewport()
					render(true)
					continue
				case "f", "F":
					if viewState.Detail {
						setNotice("请先按 Esc 返回列表再切换分组")
						render(true)
						continue
					}
					if groupError := openGroupChooser(); groupError != nil {
						setNotice("读取分组失败: " + groupError.Error())
						render(true)
						continue
					}
					renderer.ResetViewport()
					render(true)
					continue
				case "m", "M":
					if viewState.Detail {
						setNotice("请先按 Esc 返回列表再分配分组")
						render(true)
						continue
					}
					if len(symbols) == 0 {
						setNotice("当前没有可分配的自选")
						render(true)
						continue
					}
					if groupError := openGroupAssignment(); groupError != nil {
						setNotice("读取分组失败: " + groupError.Error())
						render(true)
						continue
					}
					renderer.ResetViewport()
					render(true)
					continue
				default:
					continue
				}
				setNotice("")
				inputCursorVisible = true
				render(true)
				continue
			}
			wasDetail := viewState.Detail
			if !wasDetail && event.Key == terminalKeyEnter && viewState.Selected >= 0 && viewState.Selected < len(symbols) {
				symbol := symbols[viewState.Selected]
				if market.AssetKindOf(symbol) == domain.AssetKindSector {
					boardDetail.open(symbol)
					startBoardDetailFetch(symbol)
					name := app.names.LookupName(symbol)
					_ = storage.RecordViewHistory(app.paths.ViewHistoryFile, symbol, name)
					renderer.ResetViewport()
					render(true)
					continue
				}
			}
			changed, quit := viewState.handle(event.Key, len(symbols))
			if quit {
				return nil
			}
			if !wasDetail && viewState.Detail && viewState.Selected >= 0 && viewState.Selected < len(symbols) {
				symbol := symbols[viewState.Selected]
				startBoardFetch(symbol, false)
				startDragonTigerFetch(symbol, false)
				startTechnicalFetch(symbol, false)
			}
			if wasDetail && !viewState.Detail && temporarySymbol != "" {
				for index, value := range symbols {
					if value == temporarySymbol {
						symbols = append(symbols[:index], symbols[index+1:]...)
						break
					}
				}
				for index, value := range current {
					if value.Symbol == temporarySymbol {
						current = append(current[:index], current[index+1:]...)
						break
					}
				}
				requestSymbols = quoteRequestSymbols(symbols)
				temporarySymbol = ""
				if viewState.Selected >= len(symbols) {
					viewState.Selected = len(symbols) - 1
				}
			}
			if changed {
				renderer.ResetViewport()
				render(true)
			} else if viewState.Detail && event.Key != terminalKeyEnter && event.Key != terminalKeyBack {
				navigateRenderer(renderer, event.Key)
			}
		case result := <-flowResults:
			flowRunning = false
			if result.err != nil {
				if !errors.Is(result.err, context.Canceled) {
					flowMessage = result.err.Error()
				}
			} else {
				flows = mergeBoardAssetFundFlows(result.flows, boardAssets)
				flowMessage = ""
			}
			render(true)
		case result := <-fundMonitorResults:
			if result.requestID != activeFundMonitorRequestID {
				continue
			}
			fundMonitorRunning = false
			activeFundMonitorRequestID = 0
			cancelFundMonitorRequest = nil
			if !fundMonitor.active || result.epoch != fundMonitorEpoch {
				continue
			}
			if result.err == nil && !fundMonitor.hasValidSample(result.flows) {
				result.err = fmt.Errorf("未返回当前股票的资金样本")
			}
			if result.err != nil {
				if !errors.Is(result.err, context.Canceled) {
					fundMonitor.failRefresh(result.err)
				}
			} else {
				fundMonitor.record(time.Now(), result.flows)
				for symbol, flow := range result.flows {
					flows[symbol] = flow
				}
			}
			if !industryFlowRunning && notice == "正在刷新资金雷达" {
				setNotice("")
			}
			render(true)
		case result := <-industryFlowResults:
			if result.requestID != activeIndustryFlowRequestID {
				continue
			}
			industryFlowRunning = false
			activeIndustryFlowRequestID = 0
			cancelIndustryFlowRequest = nil
			if !fundMonitor.active || result.epoch != fundMonitorEpoch {
				continue
			}
			if result.err != nil {
				if !errors.Is(result.err, context.Canceled) {
					fundMonitor.failIndustryRefresh(result.err)
				}
			} else {
				fundMonitor.setIndustries(time.Now(), result.industries)
			}
			if !fundMonitorRunning && notice == "正在刷新资金雷达" {
				setNotice("")
			}
			render(true)
		case result := <-boardFundDashboardResults:
			boardFundDashboardRunning = false
			if result.err != nil {
				if !errors.Is(result.err, context.Canceled) {
					boardFunds.fail(result.err)
				}
			} else {
				boardFunds.complete(result.dashboard)
			}
			render(true)
		case result := <-boardDetailResults:
			boardDetailRunning = false
			if !boardDetail.viewing || boardDetail.symbol != result.symbol {
				continue
			}
			if result.err != nil {
				if !errors.Is(result.err, context.Canceled) {
					boardDetail.fail(result.err)
				}
			} else {
				boardDetail.complete(result.flow, result.leaders)
				if result.flow.Name != "" {
					_ = app.names.Remember([]domain.Candidate{{Symbol: result.symbol, Name: result.flow.Name}})
					_ = storage.RecordViewHistory(app.paths.ViewHistoryFile, result.symbol, result.flow.Name)
				}
			}
			renderer.ResetViewport()
			render(true)
		case result := <-boardAssetResults:
			boardAssetRunning[result.symbol] = false
			if result.err != nil {
				continue
			}
			boardAssets[result.symbol] = result.flow
			boardAssetRefreshed[result.symbol] = time.Now()
			current = mergeBoardAssetQuotes(current, symbols, boardAssets, options.Pinyin)
			if result.flow.Name != "" {
				_ = app.names.Remember([]domain.Candidate{{Symbol: result.symbol, Name: result.flow.Name}})
			}
			flows[result.symbol] = boardAssetFundFlow(result.symbol, result.flow)
			render(true)
		case result := <-marketReportProgressResults:
			if result.epoch != marketReportEpoch || !marketReport.generating {
				continue
			}
			marketReport.progress = result.message
			render(true)
		case result := <-marketReportResults:
			if result.epoch != marketReportEpoch {
				continue
			}
			if result.err != nil {
				marketReport.fail(result.err)
			} else {
				marketReport.complete(result.report)
			}
			render(true)
		case result := <-stockReportProgressResults:
			if result.epoch != stockReportEpoch || !stockReport.generating {
				continue
			}
			stockReport.progress = result.message
			render(true)
		case result := <-stockReportResults:
			if result.epoch != stockReportEpoch {
				continue
			}
			if result.err != nil {
				stockReport.fail(result.err)
			} else {
				stockReport.complete(result.report)
			}
			render(true)
		case result := <-aiChatProgressResults:
			if result.epoch != aiChatEpoch || !aiChat.generating {
				continue
			}
			aiChat.progress = result.message
			render(true)
		case result := <-aiChatResults:
			if result.epoch != aiChatEpoch {
				continue
			}
			if result.err != nil {
				aiChat.fail(result.err)
			} else {
				aiChat.completeResearch(
					result.result.Answer, result.at, result.result.FactsAt,
					result.result.FactsHash, result.result.Agents,
				)
				if app.aiChats != nil {
					if saveError := app.aiChats.Save(aiChat.symbol, aiChat.name, aiChat.turns); saveError != nil {
						aiChat.error = saveError.Error()
					}
				}
			}
			render(true)
		case progress := <-strategyLabProgressResults:
			if strategyLab.running {
				strategyLab.progress = progress.message
				render(true)
			}
		case result := <-strategyLabResults:
			if result.err != nil {
				strategyLab.fail(result.err)
			} else if result.kind == "optimization" {
				strategyLab.completeOptimization(result.optimization)
			} else if result.kind == "continuous" {
				strategyLab.completeContinuous(result.continuous)
			} else {
				strategyLab.completeBacktest(result.run)
			}
			loadStrategyLabHistory()
			render(true)
		case result := <-marketRankingResults:
			marketRankingRunning = false
			if !marketRanking.active || result.epoch != marketRankingEpoch || result.kind != marketRanking.kind {
				continue
			}
			if result.err != nil {
				marketRanking.failRefresh(result.err)
			} else {
				marketRanking.refresh(result.items)
				if fundMonitor.active && fundMonitor.rankingKind == result.kind {
					fundMonitor.syncSymbols(marketRankingSymbols(result.items))
				}
			}
			if !viewState.Detail {
				render(true)
			}
		case result := <-boardResults:
			boardRunning[result.symbol] = false
			if result.err == nil {
				boardFlows[result.symbol] = result.boards
				boardRefreshed[result.symbol] = time.Now()
			}
			if viewState.Detail && viewState.Selected >= 0 && viewState.Selected < len(symbols) && symbols[viewState.Selected] == result.symbol {
				render(true)
			}
		case result := <-dragonTigerResults:
			dragonTigerRunning[result.symbol] = false
			if result.err == nil {
				dragonTigers[result.symbol] = result.snapshot
				dragonTigerRefreshed[result.symbol] = time.Now()
			}
			if viewState.Detail && viewState.Selected >= 0 && viewState.Selected < len(symbols) && symbols[viewState.Selected] == result.symbol {
				render(true)
			}
		case result := <-technicalResults:
			technicalRunning[result.symbol] = false
			technicalRefreshed[result.symbol] = time.Now()
			if result.err != nil {
				technicalSignals[result.symbol] = domain.TechnicalSignal{
					Status: domain.TechnicalStatusUnavailable,
					Symbol: result.symbol,
					Error:  result.err.Error(),
				}
			} else {
				technicalSignals[result.symbol] = result.signal
			}
			if viewState.Detail && viewState.Selected >= 0 && viewState.Selected < len(symbols) && symbols[viewState.Selected] == result.symbol {
				render(true)
			}
		case result := <-amountResults:
			amountRunning = false
			if result.err == nil {
				previousAmounts = result.amounts
			}
			render(true)
		case result := <-globalMarketResults:
			globalMarketRunning = false
			if result.err != nil {
				if !errors.Is(result.err, context.Canceled) {
					globalMarkets.fail(result.err)
				}
			} else {
				globalMarkets.complete(result.indices)
			}
			if globalMarkets.viewing {
				render(true)
			}
		case <-resizeTicker.C:
			render(false)
		case <-cursorTicker.C:
			if command.active() && !command.confirm && !command.choosing() {
				inputCursorVisible = !inputCursorVisible
				render(true)
			}
		case <-sessionTicker.C:
			now := time.Now()
			if notice != "" && !noticeExpires.IsZero() && !now.Before(noticeExpires) {
				setNotice("")
				render(true)
			}
			nextSession := marketSessionAt(now)
			if nextSession != session {
				wasPolling := session.Poll
				session = nextSession
				render(true)
				if !wasPolling && session.Poll {
					startMarketRankingFetch()
					startFundMonitorFetch(false)
					startIndustryFlowFetch(false)
					if err := fetch(); err != nil {
						if errors.Is(err, context.Canceled) {
							return nil
						}
						return err
					}
					startFlowFetch()
					startAmountFetch()
					startBoardAssetFetch(true)
					if viewState.Detail && viewState.Selected >= 0 && viewState.Selected < len(symbols) {
						symbol := symbols[viewState.Selected]
						startBoardFetch(symbol, true)
						startDragonTigerFetch(symbol, false)
						startTechnicalFetch(symbol, true)
					}
				}
			}
			startFundMonitorFetch(false)
		case <-quoteTicker.C:
			if !session.Poll {
				continue
			}
			startMarketRankingFetch()
			if err := fetch(); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
		case <-globalMarketTicker.C:
			if globalMarkets.viewing {
				startGlobalMarketFetch()
			}
		case <-flowTicker.C:
			if session.Poll {
				startFlowFetch()
				startBoardAssetFetch(false)
				startIndustryFlowFetch(false)
				startBoardFundDashboardFetch(false)
				if viewState.Detail && viewState.Selected >= 0 && viewState.Selected < len(symbols) {
					startBoardFetch(symbols[viewState.Selected], true)
					startTechnicalFetch(symbols[viewState.Selected], false)
				}
			}
		}
	}
}

func navigateRenderer(renderer *ui.Renderer, key terminalKey) {
	switch key {
	case terminalKeyUp:
		renderer.Navigate(ui.NavigateUp)
	case terminalKeyDown:
		renderer.Navigate(ui.NavigateDown)
	case terminalKeyPageUp:
		renderer.Navigate(ui.NavigatePageUp)
	case terminalKeyPageDown, terminalKeySpace:
		renderer.Navigate(ui.NavigatePageDown)
	case terminalKeyHome:
		renderer.Navigate(ui.NavigateHome)
	case terminalKeyEnd:
		renderer.Navigate(ui.NavigateEnd)
	}
}
