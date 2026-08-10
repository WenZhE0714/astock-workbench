package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/ui"
)

type watchOptions struct {
	Inputs   []string
	Interval int
	Once     bool
	Depth    bool
	Moyu     bool
	Pinyin   bool
	Color    bool
}

type marketSession struct {
	Label string
	Poll  bool
}

var shanghaiLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func marketSessionAt(now time.Time) marketSession {
	local := now.In(shanghaiLocation)
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
		return marketSession{Label: "休市"}
	}
	minutes := local.Hour()*60 + local.Minute()
	switch {
	case minutes < 9*60+30:
		return marketSession{Label: "未开盘"}
	case minutes < 11*60+30:
		return marketSession{Label: "交易中", Poll: true}
	case minutes < 13*60:
		return marketSession{Label: "午间休市"}
	case minutes < 15*60:
		return marketSession{Label: "交易中", Poll: true}
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
	case terminalKeyPageDown:
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
		result = append(result, domain.Candidate{Symbol: item.Symbol, Name: item.Name})
	}
	return result
}

func quoteRequestSymbols(symbols []string) []string {
	result := append([]string(nil), symbols...)
	for _, indexSymbol := range market.BroadMarketSymbols {
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

func splitMarketQuotes(quotes []domain.Quote, symbols []string) (stocks, indices []domain.Quote) {
	bySymbol := make(map[string]domain.Quote, len(quotes))
	for _, item := range quotes {
		bySymbol[item.Symbol] = item
	}
	for _, symbol := range symbols {
		if item, ok := bySymbol[symbol]; ok {
			stocks = append(stocks, item)
		}
	}
	for _, symbol := range market.BroadMarketSymbols {
		if item, ok := bySymbol[symbol]; ok {
			indices = append(indices, item)
		}
	}
	return stocks, indices
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
	var symbols []string
	if len(options.Inputs) > 0 {
		symbols, err = app.resolver.ResolveMany(ctx, options.Inputs)
	} else {
		var warnings []string
		symbols, warnings, err = storage.LoadWatchlist(app.paths.WatchlistFile)
		for _, warning := range warnings {
			fmt.Fprintf(app.errOut, "%s: %s\n", programName, warning)
		}
	}
	if err != nil {
		return err
	}
	if len(symbols) == 0 {
		return fmt.Errorf("没有要显示的股票。直接查看: astock 600519；保存自选: astock add 贵州茅台")
	}
	if len(symbols) > maxStocks {
		return fmt.Errorf("一次最多显示 %d 只股票", maxStocks)
	}
	return app.watchLoop(ctx, symbols, options)
}

func (app *App) watchLoop(ctx context.Context, symbols []string, options watchOptions) error {
	var pinyins *storage.PinyinCache
	var err error
	if options.Pinyin {
		pinyins, err = storage.LoadPinyinCache(app.paths.PinyinCacheFile)
		if err != nil {
			return err
		}
	}
	viewOptions := ui.ViewOptions{Depth: options.Depth, Moyu: options.Moyu, Color: options.Color}
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
		previousAmounts := map[string]float64{}
		flowError := ""
		if app.flows != nil {
			flowContext, cancelFlow := context.WithTimeout(ctx, 8*time.Second)
			flows, err = app.flows.Fetch(flowContext, requestSymbols)
			cancelFlow()
			if err != nil {
				flowError = err.Error()
				flows = map[string]domain.FundFlow{}
			}
		}
		if app.amounts != nil {
			amountContext, cancelAmounts := context.WithTimeout(ctx, 8*time.Second)
			previousAmounts, err = app.amounts.FetchPreviousAmounts(amountContext, market.BroadMarketSymbols)
			cancelAmounts()
			if err != nil {
				previousAmounts = map[string]float64{}
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
	previousAmounts := map[string]float64{}
	refreshed := time.Time{}
	message := ""
	flowMessage := ""
	session := marketSessionAt(time.Now())
	viewState := watchViewState{}
	command := watchCommand{}
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
	status := func() string {
		if command.active() {
			return command.status(options.Moyu, inputCursorVisible)
		}
		return notice
	}
	controls := func() string {
		if command.active() {
			return command.controls(options.Moyu)
		}
		return ""
	}
	render := func(force bool) {
		width, height := terminalDimensions(app.out)
		if !force && width == lastWidth && height == lastHeight {
			return
		}
		var selectedBoards []domain.BoardFlow
		if viewState.Detail && viewState.Selected >= 0 && viewState.Selected < len(symbols) {
			selectedBoards = boardFlows[symbols[viewState.Selected]]
		}
		frame := ui.BuildLiveFrame(ui.LiveData{
			Quotes: current, Symbols: symbols, Indices: indices, Flows: flows,
			Boards:          selectedBoards,
			PreviousAmounts: previousAmounts,
			RefreshedAt:     refreshed, MarketStatus: session.Label,
			FetchError: message, FlowError: flowMessage,
			Status: status(), Footer: controls(),
			Selected: viewState.Selected, Detail: viewState.Detail,
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
				current = stocks
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
	if err := fetch(); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	type flowResult struct {
		flows map[string]domain.FundFlow
		err   error
	}
	type amountResult struct {
		amounts map[string]float64
		err     error
	}
	type boardResult struct {
		symbol string
		boards []domain.BoardFlow
		err    error
	}
	flowResults := make(chan flowResult, 1)
	amountResults := make(chan amountResult, 1)
	boardResults := make(chan boardResult, 8)
	flowRunning := false
	amountRunning := false
	boardRunning := make(map[string]bool)
	boardRefreshed := make(map[string]time.Time)
	startFlowFetch := func() {
		if app.flows == nil || flowRunning {
			return
		}
		flowRunning = true
		request := append([]string(nil), requestSymbols...)
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
			result, fetchError := app.amounts.FetchPreviousAmounts(ctx, market.BroadMarketSymbols)
			select {
			case amountResults <- amountResult{amounts: result, err: fetchError}:
			case <-ctx.Done():
			}
		}()
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
	startFlowFetch()
	startAmountFetch()
	executeWatchCommand := func(symbol string) {
		switch command.kind {
		case watchCommandAdd:
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
			added, addError := storage.AddWatchlist(app.paths.WatchlistFile, []string{symbol})
			if addError != nil {
				setNotice("添加失败: " + addError.Error())
			} else if len(added) == 0 || !added[0] {
				setNotice("已在自选中: " + symbol[2:])
			} else {
				if !inView {
					symbols = append(symbols, symbol)
				}
				requestSymbols = quoteRequestSymbols(symbols)
				setNotice("已添加自选: " + symbol[2:])
			}
			command.reset()
			if addError == nil && len(added) > 0 && added[0] {
				if err := fetch(); err != nil && !errors.Is(err, context.Canceled) {
					setNotice("已添加，但刷新失败: " + err.Error())
				}
			}
			startFlowFetch()
			render(true)
		case watchCommandJump:
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
			}
			renderer.ResetViewport()
			render(true)
		}
	}

	quoteTicker := time.NewTicker(time.Duration(options.Interval) * time.Second)
	defer quoteTicker.Stop()
	flowTicker := time.NewTicker(60 * time.Second)
	defer flowTicker.Stop()
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
			if command.active() {
				if event.Key == terminalKeyBack || event.Key == terminalKeyQuit {
					command.reset()
					setNotice("")
					render(true)
					continue
				}
				if command.confirm {
					if event.Key == terminalKeyEnter || event.Text == "y" || event.Text == "Y" {
						removed, removeError := storage.RemoveWatchlist(app.paths.WatchlistFile, []string{command.symbol})
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
							setNotice("已删除自选: " + command.symbol[2:])
							if temporarySymbol == command.symbol {
								temporarySymbol = ""
							}
							if viewState.Detail {
								viewState.Detail = false
							}
							if err := fetch(); err != nil && !errors.Is(err, context.Canceled) {
								setNotice("刷新失败: " + err.Error())
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
					case terminalKeyPageDown:
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
						setNotice("请输入代码或完整名称")
						command.reset()
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
					command.confirmDelete(symbol, name)
				case "i", "I":
					command.begin(watchCommandJump)
				default:
					continue
				}
				setNotice("")
				inputCursorVisible = true
				render(true)
				continue
			}
			wasDetail := viewState.Detail
			changed, quit := viewState.handle(event.Key, len(symbols))
			if quit {
				return nil
			}
			if !wasDetail && viewState.Detail && viewState.Selected >= 0 && viewState.Selected < len(symbols) {
				startBoardFetch(symbols[viewState.Selected], false)
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
				flows = result.flows
				flowMessage = ""
			}
			render(true)
		case result := <-boardResults:
			boardRunning[result.symbol] = false
			if result.err == nil {
				boardFlows[result.symbol] = result.boards
				boardRefreshed[result.symbol] = time.Now()
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
					if err := fetch(); err != nil {
						if errors.Is(err, context.Canceled) {
							return nil
						}
						return err
					}
					startFlowFetch()
					startAmountFetch()
					if viewState.Detail && viewState.Selected >= 0 && viewState.Selected < len(symbols) {
						startBoardFetch(symbols[viewState.Selected], true)
					}
				}
			}
		case <-quoteTicker.C:
			if !session.Poll {
				continue
			}
			if err := fetch(); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
		case <-flowTicker.C:
			if session.Poll {
				startFlowFetch()
				if viewState.Detail && viewState.Selected >= 0 && viewState.Selected < len(symbols) {
					startBoardFetch(symbols[viewState.Selected], true)
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
	case terminalKeyPageDown:
		renderer.Navigate(ui.NavigatePageDown)
	case terminalKeyHome:
		renderer.Navigate(ui.NavigateHome)
	case terminalKeyEnd:
		renderer.Navigate(ui.NavigateEnd)
	}
}
