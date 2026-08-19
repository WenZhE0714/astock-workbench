package backtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type DailyEngine struct {
	provider DailyBarProvider
	now      func() time.Time
}

func NewDailyEngine(provider DailyBarProvider) *DailyEngine {
	return &DailyEngine{provider: provider, now: time.Now}
}

type pendingOrder struct {
	side   string
	signal SignalSnapshot
	reason string
}

type positionState struct {
	symbol      string
	name        string
	quantity    int
	entrySignal SignalSnapshot
	entry       Fill
	entryCost   float64
	holdingDays int
	maxHigh     float64
	minLow      float64
}

type symbolState struct {
	bars      []domain.DailyBar
	byDate    map[string]int
	lastClose float64
	position  *positionState
	pending   *pendingOrder
}

func validateRequest(request Request) error {
	if len(request.Tickers) == 0 {
		return fmt.Errorf("至少需要一个回测股票")
	}
	seenTickers := make(map[string]bool, len(request.Tickers))
	for _, ticker := range request.Tickers {
		if strings.TrimSpace(ticker) == "" || seenTickers[ticker] {
			return fmt.Errorf("回测股票代码为空或重复")
		}
		seenTickers[ticker] = true
	}
	if !finiteValues(request.InitialCash, request.CommissionRate, request.MinimumCommission, request.StampDutyRate,
		request.TransferFeeRate, request.SlippageBPS) || request.InitialCash <= 0 || request.Start.IsZero() || request.End.IsZero() || request.Start.After(request.End) {
		return fmt.Errorf("回测资金或日期区间无效")
	}
	if request.Adjustment != AdjustmentNone {
		return fmt.Errorf("当前回测仅支持不复权日K")
	}
	if request.Strategy != "technical-breakout" {
		return fmt.Errorf("当前仅支持策略 technical-breakout")
	}
	p := request.Technical
	if p.FastMA < 2 || p.SlowMA <= p.FastMA || p.BreakoutDays < 2 || p.VolumeRatioMin <= 0 ||
		p.StopLoss <= 0 || p.TakeProfit <= 0 || p.MaxHoldingDays < 1 || p.MaxPosition <= 0 || p.MaxPosition > 1 {
		return fmt.Errorf("技术策略参数无效")
	}
	if request.CommissionRate < 0 || request.MinimumCommission < 0 || request.StampDutyRate < 0 ||
		request.TransferFeeRate < 0 || request.SlippageBPS < 0 {
		return fmt.Errorf("交易费用参数无效")
	}
	return nil
}

func canonicalRequest(request Request) Request {
	request.Tickers = append([]string(nil), request.Tickers...)
	sort.Strings(request.Tickers)
	return request
}

func averageBars(bars []domain.DailyBar, end, period int, value func(domain.DailyBar) float64) float64 {
	if period < 1 || end-period+1 < 0 || end >= len(bars) {
		return math.NaN()
	}
	total := 0.0
	for index := end - period + 1; index <= end; index++ {
		total += value(bars[index])
	}
	return total / float64(period)
}

func technicalSnapshot(bars []domain.DailyBar, index int, parameters TechnicalParameters) (SignalSnapshot, bool) {
	warmup := max(parameters.SlowMA, parameters.BreakoutDays+1, 21)
	if index < warmup {
		return SignalSnapshot{}, false
	}
	bar := bars[index]
	fast := averageBars(bars, index, parameters.FastMA, func(item domain.DailyBar) float64 { return item.Close })
	slow := averageBars(bars, index, parameters.SlowMA, func(item domain.DailyBar) float64 { return item.Close })
	volumeAverage := averageBars(bars, index-1, 20, func(item domain.DailyBar) float64 { return item.Volume })
	volumeRatio := math.NaN()
	if volumeAverage > 0 {
		volumeRatio = bar.Volume / volumeAverage
	}
	priorHigh := bars[index-parameters.BreakoutDays].High
	for candidate := index - parameters.BreakoutDays + 1; candidate < index; candidate++ {
		if bars[candidate].High > priorHigh {
			priorHigh = bars[candidate].High
		}
	}
	return SignalSnapshot{
		Date: bar.Date, Close: bar.Close, Low: bar.Low,
		PreviousClose:  bars[index-1].Close,
		PreviousFastMA: averageBars(bars, index-1, parameters.FastMA, func(item domain.DailyBar) float64 { return item.Close }),
		FastMA:         fast, SlowMA: slow,
		PriorHigh: priorHigh, VolumeRatio: volumeRatio,
	}, true
}

func entrySignal(snapshot SignalSnapshot, parameters TechnicalParameters) (SignalSnapshot, bool) {
	if snapshot.FastMA <= snapshot.SlowMA || math.IsNaN(snapshot.VolumeRatio) || snapshot.VolumeRatio < parameters.VolumeRatioMin {
		return snapshot, false
	}
	mode := parameters.EffectiveEntryMode()
	triggered := false
	reason := ""
	switch mode {
	case EntryModeBreakout:
		triggered = snapshot.Close > snapshot.PriorHigh
		reason = fmt.Sprintf("收盘 %.2f 突破前%d日高点 %.2f", snapshot.Close, parameters.BreakoutDays, snapshot.PriorHigh)
	case EntryModeReclaim:
		triggered = snapshot.PreviousClose <= snapshot.PreviousFastMA && snapshot.Close > snapshot.FastMA
		reason = fmt.Sprintf("收盘 %.2f 重新站上 MA%d %.2f", snapshot.Close, parameters.FastMA, snapshot.FastMA)
	case EntryModePullback:
		triggered = snapshot.Low <= snapshot.FastMA && snapshot.Close >= snapshot.FastMA
		reason = fmt.Sprintf("日内回踩 MA%d %.2f 后收于均线上方", parameters.FastMA, snapshot.FastMA)
	}
	if !triggered {
		return snapshot, false
	}
	snapshot.Action = "买入"
	snapshot.Reasons = []string{
		reason,
		fmt.Sprintf("MA%d %.2f 高于 MA%d %.2f", parameters.FastMA, snapshot.FastMA, parameters.SlowMA, snapshot.SlowMA),
		fmt.Sprintf("成交量为前20日均量的 %.2f 倍，达到 %.2f 倍门槛", snapshot.VolumeRatio, parameters.VolumeRatioMin),
	}
	return snapshot, true
}

func exitSignal(snapshot SignalSnapshot, position *positionState, parameters TechnicalParameters) (SignalSnapshot, string, bool) {
	returnRate := snapshot.Close/position.entry.Price - 1
	reason := ""
	switch {
	case returnRate <= -parameters.StopLoss:
		reason = fmt.Sprintf("收盘触发 %.2f%% 止损", parameters.StopLoss*100)
	case returnRate >= parameters.TakeProfit:
		reason = fmt.Sprintf("收盘触发 %.2f%% 止盈", parameters.TakeProfit*100)
	case snapshot.Close < snapshot.FastMA:
		reason = fmt.Sprintf("收盘 %.2f 跌破 MA%d %.2f", snapshot.Close, parameters.FastMA, snapshot.FastMA)
	case position.holdingDays >= parameters.MaxHoldingDays:
		reason = fmt.Sprintf("持有达到 %d 个交易日", parameters.MaxHoldingDays)
	default:
		return snapshot, "", false
	}
	snapshot.Action = "卖出"
	snapshot.Reasons = []string{reason}
	return snapshot, reason, true
}

func onePriceBar(bar domain.DailyBar) bool {
	return math.Abs(bar.High-bar.Low) < 1e-9
}

func transactionFees(request Request, symbol, side string, amount float64) (commission, stamp, transfer, total float64) {
	commission = math.Max(request.MinimumCommission, amount*request.CommissionRate)
	if side == "sell" {
		stamp = amount * request.StampDutyRate
	}
	transfer = amount * request.TransferFeeRate
	total = commission + stamp + transfer
	return
}

func makeFill(request Request, symbol, side, date string, rawPrice float64, quantity int, forced bool) Fill {
	direction := 1.0
	if side == "sell" {
		direction = -1
	}
	price := rawPrice * (1 + direction*request.SlippageBPS/10000)
	amount := price * float64(quantity)
	commission, stamp, transfer, total := transactionFees(request, symbol, side, amount)
	return Fill{
		Date: date, Side: side, Price: price, RawPrice: rawPrice, Quantity: quantity, Amount: amount,
		Commission: commission, StampDuty: stamp, TransferFee: transfer, TotalFee: total,
		SlippageBPS: request.SlippageBPS, Forced: forced,
	}
}

func buyQuantity(request Request, cash, equity, rawPrice float64) int {
	budget := math.Min(cash, equity*request.Technical.MaxPosition)
	price := rawPrice * (1 + request.SlippageBPS/10000)
	quantity := int(budget/price/100) * 100
	for quantity >= 100 {
		fill := makeFill(request, "", "buy", "", rawPrice, quantity, false)
		if fill.Amount+fill.TotalFee <= cash && fill.Amount+fill.TotalFee <= budget {
			return quantity
		}
		quantity -= 100
	}
	return 0
}

func dateInRange(value string, start, end time.Time) bool {
	return len(value) == len("2006-01-02") && value >= start.Format("2006-01-02") && value <= end.Format("2006-01-02")
}

func (engine *DailyEngine) loadStates(ctx context.Context, request Request) (map[string]*symbolState, map[string]string, []string, error) {
	states := make(map[string]*symbolState, len(request.Tickers))
	sources := make(map[string]string, len(request.Tickers))
	warnings := []string{
		"使用不复权日K；跨除权除息事件的收益可能失真，报告不得与其他复权口径混用",
		"日K只能识别一字板，无法还原盘中涨跌停排队和实际成交概率",
		"佣金、印花税和过户费使用本次请求中的固定费率，未按历史政策日期自动切换",
	}
	if request.LiquidateAtEnd {
		warnings = append(warnings, "回测期末强制平仓使用最后一个可成交日收盘价并计入卖出成本，属于估值收尾而非次日开盘真实成交")
	}
	if !request.PointInTimePool {
		warnings = append(warnings, "股票池由本次输入的静态代码组成，不是历史点时成份股池；多股横截面结果可能存在幸存者偏差")
	}
	warmupBars := max(request.Technical.SlowMA, request.Technical.BreakoutDays+1, 21)
	// Three calendar days per required trading bar covers weekends, exchange
	// holidays and long holiday closures without fetching years of unused data.
	warmStart := request.Start.AddDate(0, 0, -warmupBars*3)
	for _, symbol := range request.Tickers {
		bars, err := engine.provider.FetchDailyBarsRange(ctx, symbol, warmStart, request.End, request.Adjustment)
		if err != nil {
			return nil, nil, warnings, fmt.Errorf("%s: %w", symbol, err)
		}
		sort.SliceStable(bars, func(left, right int) bool { return bars[left].Date < bars[right].Date })
		byDate := make(map[string]int, len(bars))
		for index, bar := range bars {
			byDate[bar.Date] = index
		}
		if len(bars) < request.Technical.SlowMA+2 {
			return nil, nil, warnings, fmt.Errorf("%s 有效日K不足：%d", symbol, len(bars))
		}
		states[symbol] = &symbolState{bars: bars, byDate: byDate}
		sources[symbol] = bars[len(bars)-1].Source
	}
	return states, sources, warnings, nil
}

func tradingDates(states map[string]*symbolState, start, end time.Time) []string {
	seen := make(map[string]bool)
	for _, state := range states {
		for _, bar := range state.bars {
			if dateInRange(bar.Date, start, end) {
				seen[bar.Date] = true
			}
		}
	}
	dates := make([]string, 0, len(seen))
	for date := range seen {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	return dates
}

func portfolioEquity(cash float64, states map[string]*symbolState) (float64, float64) {
	positions := 0.0
	for _, state := range states {
		if state.position != nil && state.lastClose > 0 {
			positions += state.lastClose * float64(state.position.quantity)
		}
	}
	return cash + positions, positions
}

func closeTrade(request Request, state *symbolState, signal SignalSnapshot, reason string, fill Fill) Trade {
	position := state.position
	gross := (fill.Price - position.entry.Price) * float64(position.quantity)
	net := fill.Amount - fill.TotalFee - position.entryCost
	return Trade{
		Symbol: state.position.symbol, Name: state.position.name, Strategy: request.Strategy,
		StrategyVersion: request.StrategyVersion, EntrySignal: position.entrySignal, Entry: position.entry,
		ExitSignal: signal, Exit: fill, HoldingDays: position.holdingDays,
		GrossProfit: gross, NetProfit: net, ReturnPercent: net / position.entryCost * 100,
		MaxFavorable: (position.maxHigh/position.entry.Price - 1) * 100,
		MaxAdverse:   (position.minLow/position.entry.Price - 1) * 100,
		ExitReason:   reason,
	}
}

func benchmarkMetrics(ctx context.Context, engine *DailyEngine, request Request) (float64, bool, string) {
	if strings.TrimSpace(request.Benchmark) == "" {
		return 0, false, ""
	}
	bars, err := engine.provider.FetchDailyBarsRange(ctx, request.Benchmark, request.Start, request.End, request.Adjustment)
	if err != nil || len(bars) < 2 {
		if err == nil {
			err = fmt.Errorf("有效日K不足")
		}
		return 0, false, "基准数据不可用: " + err.Error()
	}
	return (bars[len(bars)-1].Close/bars[0].Close - 1) * 100, true, ""
}

func (engine *DailyEngine) Run(ctx context.Context, request Request) (Result, error) {
	if engine == nil || engine.provider == nil {
		return Result{}, fmt.Errorf("回测历史数据源未初始化")
	}
	request = canonicalRequest(request)
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}
	states, sources, warnings, err := engine.loadStates(ctx, request)
	if err != nil {
		return Result{}, err
	}
	dates := tradingDates(states, request.Start, request.End)
	if len(dates) < 2 {
		return Result{}, fmt.Errorf("回测区间内交易日不足")
	}
	coverage := make(map[string]DataCoverage, len(request.Tickers))
	for _, symbol := range request.Tickers {
		item := DataCoverage{RequestedStart: request.Start.Format("2006-01-02"), RequestedEnd: request.End.Format("2006-01-02")}
		state := states[symbol]
		if state != nil {
			for _, date := range dates {
				if _, ok := state.byDate[date]; !ok {
					continue
				}
				if item.FirstDate == "" {
					item.FirstDate = date
				}
				item.LastDate = date
				item.Bars++
			}
		}
		item.CoverageRatio = float64(item.Bars) / float64(len(dates))
		coverage[symbol] = item
	}
	cash := request.InitialCash
	trades := make([]Trade, 0)
	equity := make([]EquityPoint, 0, len(dates))
	turnover, totalFees, peak := 0.0, 0.0, request.InitialCash

	for _, date := range dates {
		// Establish a point-in-time portfolio value at the open before any fill.
		// This prevents one symbol's same-day close from leaking into another
		// symbol's opening position-size calculation.
		for _, symbol := range request.Tickers {
			state := states[symbol]
			if state == nil {
				continue
			}
			if index, ok := state.byDate[date]; ok {
				state.lastClose = state.bars[index].Open
			}
		}

		// Execute signals created at earlier closes against today's open.
		for _, symbol := range request.Tickers {
			state := states[symbol]
			if state == nil {
				continue
			}
			index, ok := state.byDate[date]
			if !ok {
				continue
			}
			bar := state.bars[index]
			if state.pending != nil {
				pending := state.pending
				if onePriceBar(bar) {
					// A one-price daily bar is treated as non-executable. Keep the
					// pending order for the next trading day and make no fill.
					warnings = append(warnings, fmt.Sprintf("%s %s 的%s信号因 %s 一字板未成交，顺延至下一交易日", symbol, pending.signal.Date, pending.signal.Action, date))
					continue
				}
				state.pending = nil
				if !onePriceBar(bar) {
					currentEquity, _ := portfolioEquity(cash, states)
					if pending.side == "buy" && state.position == nil {
						quantity := buyQuantity(request, cash, currentEquity, bar.Open)
						if quantity >= 100 {
							fill := makeFill(request, symbol, "buy", date, bar.Open, quantity, false)
							cash -= fill.Amount + fill.TotalFee
							turnover += fill.Amount
							totalFees += fill.TotalFee
							state.position = &positionState{
								symbol: symbol, name: request.Names[symbol], quantity: quantity,
								entrySignal: pending.signal, entry: fill, entryCost: fill.Amount + fill.TotalFee,
								maxHigh: bar.High, minLow: bar.Low,
							}
						} else {
							warnings = append(warnings, fmt.Sprintf("%s %s 的买入信号因资金不足100股未成交", symbol, pending.signal.Date))
						}
					} else if pending.side == "sell" && state.position != nil && date != state.position.entry.Date {
						fill := makeFill(request, symbol, "sell", date, bar.Open, state.position.quantity, false)
						cash += fill.Amount - fill.TotalFee
						turnover += fill.Amount
						totalFees += fill.TotalFee
						trades = append(trades, closeTrade(request, state, pending.signal, pending.reason, fill))
						state.position = nil
					}
				}
			}
		}

		// Mark positions and create new signals only after all opening fills.
		for _, symbol := range request.Tickers {
			state := states[symbol]
			if state == nil {
				continue
			}
			index, ok := state.byDate[date]
			if !ok {
				continue
			}
			bar := state.bars[index]
			state.lastClose = bar.Close
			if state.position != nil {
				state.position.holdingDays++
				state.position.maxHigh = math.Max(state.position.maxHigh, bar.High)
				state.position.minLow = math.Min(state.position.minLow, bar.Low)
			}
			snapshot, ready := technicalSnapshot(state.bars, index, request.Technical)
			if !ready || state.pending != nil {
				continue
			}
			if state.position == nil {
				if signal, triggered := entrySignal(snapshot, request.Technical); triggered {
					state.pending = &pendingOrder{side: "buy", signal: signal, reason: strings.Join(signal.Reasons, "；")}
				}
			} else {
				if signal, reason, triggered := exitSignal(snapshot, state.position, request.Technical); triggered {
					state.pending = &pendingOrder{side: "sell", signal: signal, reason: reason}
				}
			}
		}
		currentEquity, positions := portfolioEquity(cash, states)
		if currentEquity > peak {
			peak = currentEquity
		}
		drawdown := 0.0
		if peak > 0 {
			drawdown = (currentEquity/peak - 1) * 100
		}
		equity = append(equity, EquityPoint{Date: date, Cash: cash, Positions: positions, Equity: currentEquity, Drawdown: drawdown})
	}

	if request.LiquidateAtEnd {
		lastDate := dates[len(dates)-1]
		for _, symbol := range request.Tickers {
			state := states[symbol]
			if state == nil {
				continue
			}
			if state.position == nil || state.lastClose <= 0 {
				continue
			}
			if state.position.entry.Date == lastDate {
				warnings = append(warnings, symbol+" 于回测最后一个交易日买入，受T+1约束保留为未平仓")
				continue
			}
			lastIndex, hasLastBar := state.byDate[lastDate]
			if !hasLastBar || onePriceBar(state.bars[lastIndex]) {
				warnings = append(warnings, symbol+" 回测期末为不可成交的一字板，保留为未平仓")
				continue
			}
			signal := SignalSnapshot{Date: lastDate, Action: "卖出", Close: state.lastClose, Reasons: []string{"回测期末强制平仓"}}
			fill := makeFill(request, symbol, "sell", lastDate, state.lastClose, state.position.quantity, true)
			cash += fill.Amount - fill.TotalFee
			turnover += fill.Amount
			totalFees += fill.TotalFee
			trades = append(trades, closeTrade(request, state, signal, "回测期末强制平仓", fill))
			state.position = nil
		}
		if len(equity) > 0 {
			finalEquity, finalPositions := portfolioEquity(cash, states)
			equity[len(equity)-1].Cash = cash
			equity[len(equity)-1].Positions = finalPositions
			equity[len(equity)-1].Equity = finalEquity
			if finalEquity > peak {
				peak = finalEquity
			}
			equity[len(equity)-1].Drawdown = (finalEquity/peak - 1) * 100
		}
	}
	for _, symbol := range request.Tickers {
		state := states[symbol]
		if state != nil && state.pending != nil {
			warnings = append(warnings, fmt.Sprintf("%s %s 的%s信号在回测期末尚无下一交易日，未生成成交", symbol, state.pending.signal.Date, state.pending.signal.Action))
		}
	}
	for index := range trades {
		trades[index].ID = fmt.Sprintf("T%04d", index+1)
	}
	openPositions := make([]OpenPosition, 0)
	for _, symbol := range request.Tickers {
		state := states[symbol]
		if state == nil {
			continue
		}
		if state.position == nil {
			continue
		}
		position := state.position
		value := state.lastClose * float64(position.quantity)
		profit := value - position.entryCost
		openPositions = append(openPositions, OpenPosition{
			Symbol: position.symbol, Name: position.name, Quantity: position.quantity,
			EntrySignal: position.entrySignal, Entry: position.entry,
			LastDate: dates[len(dates)-1], LastPrice: state.lastClose, MarketValue: value,
			UnrealizedProfit: profit, ReturnPercent: profit / position.entryCost * 100,
		})
	}
	benchmarkReturn, benchmarkAvailable, benchmarkWarning := benchmarkMetrics(ctx, engine, request)
	if benchmarkWarning != "" {
		warnings = append(warnings, benchmarkWarning)
	}
	metrics := calculateMetrics(request, equity, trades, turnover, totalFees, benchmarkReturn, benchmarkAvailable)
	generatedAt := engine.now()
	return Result{
		RunID: generatedAt.Format("20060102T150405"), GeneratedAt: generatedAt, Request: request,
		Metrics: metrics, Trades: trades, OpenPositions: openPositions, Equity: equity,
		DataSources: sources, DataCoverage: coverage, Warnings: warnings,
	}, nil
}
