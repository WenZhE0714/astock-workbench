package paper

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/realtime"
)

// advanceRealtime applies only point-in-time events from the current trading
// session. The daily evaluator remains the source of historical replay; this
// layer never fabricates fills for a quote time that has not arrived.
func (e *Evaluator) advanceRealtime(ctx context.Context, report Report, signals []realtime.Signal, options Options, calendarDates []string, now time.Time) (Report, error) {
	if options.RealtimeAt.IsZero() && report.LastRealtimeAt != "" && now.Sub(parseTimestampOrZero(report.LastRealtimeAt)) < 0 {
		return report, nil
	}
	eventNow := options.RealtimeAt
	if eventNow.IsZero() {
		eventNow = now
	}
	eventNow = eventNow.In(shanghaiLocation)
	date := localTradingDate(eventNow)
	if date == "" || report.AsOf != date || !realtimeExecutionWindow(eventNow) {
		return report, nil
	}

	quotes := realtimeQuoteMap(options.RealtimeQuotes, date, eventNow)
	if len(quotes) == 0 {
		return report, fmt.Errorf("当前交易时段没有可用的同日实时行情")
	}
	report.ExecutionMode = ExecutionModeLive
	report.GeneratedAt = eventNow
	applyReportCalibrationMetadata(&report, signals)
	report.Positions = normalizeRealtimePositions(report.Positions, report.Config, calendarDates, date)

	// Risk exits are evaluated before discretionary signal events. Planned
	// holding-window exits remain at the completed close so the live layer does
	// not silently shorten the historical strategy's holding convention.
	handledSymbols := make(map[string]bool)
	positionSymbols := make([]string, 0, len(report.Positions))
	for _, position := range report.Positions {
		positionSymbols = append(positionSymbols, position.Symbol)
	}
	sort.Strings(positionSymbols)
	for _, symbol := range positionSymbols {
		quote, ok := quotes[symbol]
		if !ok {
			continue
		}
		positionIndex := realtimePositionIndex(report.Positions, symbol)
		if positionIndex < 0 {
			continue
		}
		position := report.Positions[positionIndex]
		riskReason := realtimeRiskExitReason(position, quote.Price, report.Config)
		if riskReason == "" {
			continue
		}
		action := "risk_exit"
		lotIndexes := realtimeSellableLotIndexes(position.Lots, date, false)
		eventID := realtimeEventID(action, symbol, quote.QuoteTime, "position")
		if realtimeEventHandled(report, eventID) {
			continue
		}
		if realtimeSellBlocked(quote) {
			reason := riskReason + "；但当前报价触及跌停或不可交易，模拟卖出被拒绝"
			appendRealtimeRejection(&report, positionSignal(position, eventNow), "sell", date, eventID, quote, reason)
			appendRealtimeDecision(&report, positionSignal(position, eventNow), date, action, reason, position.Quantity, 0, eventID, quote)
			position.RiskExitPending = action == "risk_exit"
			position.RiskExitReason = riskReason
			report.Positions[positionIndex] = position
			handledSymbols[symbol] = true
			continue
		}
		if len(lotIndexes) == 0 {
			reason := riskReason + "；当日新买批次受 T+1 锁定，当前没有可卖数量"
			appendRealtimeDecision(&report, positionSignal(position, eventNow), date, "hold", reason, position.Quantity, 0, eventID, quote)
			position.RiskExitPending = action == "risk_exit"
			position.RiskExitReason = riskReason
			report.Positions[positionIndex] = position
			handledSymbols[symbol] = true
			continue
		}
		signal := positionSignal(position, eventNow)
		filled := executeRealtimeSell(&report, &position, lotIndexes, signal, quote, eventID, action, riskReason, calendarDates)
		if filled > 0 {
			appendRealtimeDecision(&report, signal, date, action, riskReason, position.Quantity, 0, eventID, quote)
			if position.Quantity == 0 {
				report.Positions = append(report.Positions[:positionIndex], report.Positions[positionIndex+1:]...)
			} else {
				position.RiskExitPending = action == "risk_exit"
				position.RiskExitReason = riskReason
				report.Positions[positionIndex] = position
			}
			handledSymbols[symbol] = true
		}
	}

	candidates := latestRealtimeSignals(signals, date, eventNow, report.Config)
	latestSignals := make(map[string]realtime.Signal, len(candidates))
	for _, signal := range candidates {
		latestSignals[signal.Symbol] = signal
	}
	// T operations are a separate intraday policy. They only touch an older,
	// sellable base lot and must complete a profitable round before ordinary
	// score-based rebalancing is considered for the same symbol.
	if report.Config.EnableIntradayT {
		for _, symbol := range positionSymbols {
			if handledSymbols[symbol] {
				continue
			}
			positionIndex := realtimePositionIndex(report.Positions, symbol)
			if positionIndex < 0 {
				continue
			}
			quote, ok := quotes[symbol]
			if !ok {
				continue
			}
			position := report.Positions[positionIndex]
			signal, found := latestSignals[symbol]
			if !found {
				signal = positionSignal(position, eventNow)
			}
			pending, pendingQuantity := realtimePendingT(report, symbol, date)
			tEventID := ""
			if pendingQuantity > 0 {
				tEventID = realtimeEventID("t-rebuy", symbol, quote.QuoteTime, "")
				if realtimeEventHandled(report, tEventID) {
					continue
				}
				if realtimeTRebuyEligible(pending, pendingQuantity, quote, report.Config) {
					if executeRealtimeTRebuy(&report, &position, signal, quote, pending, pendingQuantity, tEventID, calendarDates, date) {
						report.Positions[positionIndex] = position
						appendRealtimeDecision(&report, signal, date, "t_rebuy", fmt.Sprintf("做T回补：现价 %.2f 较卖出价 %.2f 回落，预计净收益覆盖费用", quote.Price, pending.Price), position.Quantity, position.TargetPositionPercent, tEventID, quote)
						handledSymbols[symbol] = true
					}
				} else {
					appendRealtimeDecision(&report, signal, date, "hold", "已有做T卖出批次，等待价格回落至覆盖费用的回补区间", position.Quantity, position.TargetPositionPercent, tEventID, quote)
				}
				continue
			}
			if !realtimeTReduceEligible(position, quote, report.Config, report, date) {
				continue
			}
			tEventID = realtimeEventID("t-reduce", symbol, quote.QuoteTime, "")
			if realtimeEventHandled(report, tEventID) || realtimeTDayRounds(report, symbol, date) >= report.Config.TMaxDailyRounds {
				continue
			}
			quantity := realtimeTReduceQuantity(position, date, report.Config)
			if quantity <= 0 || realtimeSellBlocked(quote) {
				continue
			}
			if executeRealtimePartialSell(&report, &position, quantity, signal, quote, tEventID, calendarDates, date) > 0 {
				report.Positions[positionIndex] = position
				appendRealtimeDecision(&report, signal, date, "t_reduce", fmt.Sprintf("做T高抛：现价 %.2f 高于VWAP %.2f，卖出 %d 股底仓并保留核心仓位", quote.Price, quote.AveragePrice, quantity), position.Quantity, position.TargetPositionPercent, tEventID, quote)
				handledSymbols[symbol] = true
			}
		}
	}
	for _, signal := range candidates {
		quote, ok := quotes[signal.Symbol]
		if !ok || handledSymbols[signal.Symbol] {
			continue
		}
		// One quote minute is one execution opportunity. Signal IDs change on
		// every scan, so they must not make the same quote executable twice.
		eventID := realtimeEventID("signal", signal.Symbol, quote.QuoteTime, "")
		if realtimeEventHandled(report, eventID) {
			continue
		}
		positionIndex := realtimePositionIndex(report.Positions, signal.Symbol)
		if positionIndex >= 0 {
			position := report.Positions[positionIndex]
			if _, pendingQuantity := realtimePendingT(report, signal.Symbol, date); pendingQuantity > 0 {
				appendRealtimeDecision(&report, signal, date, "hold", "已有做T卖出批次，普通加减仓等待回补完成", position.Quantity, position.TargetPositionPercent, eventID, quote)
				continue
			}
			currentScore := position.SignalScore
			if currentScore <= 0 {
				currentScore = shadowSignalScoreForConfig(signal, report.Config)
			}
			newScore := shadowSignalScoreForConfig(signal, report.Config)
			state := shadowSignalStateForConfig(signal, report.Config)
			rebalance := ""
			if (state == realtime.StateWeak || state == realtime.StateInvalid) && newScore <= currentScore-report.Config.AdditionScoreStep {
				rebalance = "reduce"
			} else if shadowSignalEntryEligible(signal, report.Config) && newScore >= currentScore+report.Config.AdditionScoreStep {
				rebalance = "add"
			}
			if rebalance != "" && !realtimeSignalCooldownElapsed(report, signal.Symbol, date, quote.QuoteTime, report.Config.SignalRebalanceCooldownMinutes) {
				appendRealtimeDecision(&report, signal, date, "hold", fmt.Sprintf("刚完成普通%s，盘中调仓冷却 %d 分钟未结束", map[string]string{"add": "加仓", "reduce": "减仓"}[rebalance], report.Config.SignalRebalanceCooldownMinutes), position.Quantity, position.TargetPositionPercent, eventID, quote)
				continue
			}
			if (state == realtime.StateWeak || state == realtime.StateInvalid) && newScore <= currentScore-report.Config.AdditionScoreStep {
				lotIndexes := realtimeSellableLotIndexes(position.Lots, date, true)
				reason := fmt.Sprintf("盘中信号由 %.1f 分回落至 %.1f 分，减去一个满足 T+1 的批次", currentScore, newScore)
				if len(lotIndexes) == 0 {
					appendRealtimeDecision(&report, signal, date, "hold", reason+"；当前没有可卖批次", position.Quantity, position.TargetPositionPercent, eventID, quote)
					continue
				}
				if realtimeSellBlocked(quote) {
					rejectedReason := reason + "；但当前报价触及跌停或不可交易，模拟减仓被拒绝"
					appendRealtimeRejection(&report, signal, "sell", date, eventID, quote, rejectedReason)
					appendRealtimeDecision(&report, signal, date, "wait", rejectedReason, position.Quantity, position.TargetPositionPercent, eventID, quote)
					continue
				}
				if executeRealtimeSell(&report, &position, lotIndexes[:1], signal, quote, eventID, "reduce", reason, calendarDates) > 0 {
					position.SignalID = signal.ID
					position.SignalDate = date
					position.SignalScore = newScore
					position.SignalReasons = append([]string(nil), signal.Reasons...)
					report.Positions[positionIndex] = position
					appendRealtimeDecision(&report, signal, date, "reduce", reason, position.Quantity, position.TargetPositionPercent, eventID, quote)
				}
				continue
			}
			if !shadowSignalEntryEligible(signal, report.Config) || newScore < currentScore+report.Config.AdditionScoreStep {
				appendRealtimeDecision(&report, signal, date, "hold", "盘中信号尚未显著增强，维持现有仓位", position.Quantity, position.TargetPositionPercent, eventID, quote)
				continue
			}
			if len(position.Lots) >= report.Config.MaxEntryTranches {
				appendRealtimeDecision(&report, signal, date, "hold", "已达到最大分批建仓次数，维持现有仓位", position.Quantity, position.TargetPositionPercent, eventID, quote)
				continue
			}
			if err := e.executeRealtimeBuy(ctx, &report, signal, quote, positionIndex, eventID, calendarDates, date); err != nil {
				appendRealtimeDecision(&report, signal, date, "wait", err.Error(), position.Quantity, position.TargetPositionPercent, eventID, quote)
			}
			continue
		}

		if !shadowSignalEntryEligible(signal, report.Config) {
			continue
		}
		if len(report.Positions) >= report.Config.MaxOpenPositions {
			appendRealtimeDecision(&report, signal, date, "wait", fmt.Sprintf("组合已有 %d 个持仓，达到盘中持仓上限", len(report.Positions)), 0, targetPositionPercent(signal, report.Config), eventID, quote)
			continue
		}
		if err := e.executeRealtimeBuy(ctx, &report, signal, quote, -1, eventID, calendarDates, date); err != nil {
			appendRealtimeDecision(&report, signal, date, "wait", err.Error(), 0, targetPositionPercent(signal, report.Config), eventID, quote)
		}
	}

	quoteItems := make([]PositionQuote, 0, len(quotes))
	latestQuoteTime := report.LastRealtimeAt
	for _, quote := range quotes {
		if !realtimeEventAfterReport(quote.QuoteTime, report) {
			continue
		}
		quoteItems = append(quoteItems, quote)
		if quote.QuoteTime > latestQuoteTime {
			latestQuoteTime = quote.QuoteTime
		}
	}
	report = RevaluePositions(report, quoteItems, eventNow)
	report.GeneratedAt = eventNow
	report.ExecutionMode = ExecutionModeLive
	report.LastRealtimeAt = latestQuoteTime
	// A realtime opportunity can end in a fill, a hold decision, or a rejected
	// order. Count the shared event identity once instead of reporting only
	// filled orders; otherwise an actively monitored account looks idle whenever
	// risk and portfolio gates correctly keep it from trading.
	report.RealtimeEvents = countRealtimeEvents(report)
	report.OpenPositions = len(report.Positions)
	recomputeTradeStats(&report)
	recomputeAccountMetrics(&report)
	return report, nil
}

func parseTimestampOrZero(value string) time.Time {
	parsed, _ := parseRealtimeTimestamp(value)
	return parsed
}

func (e *Evaluator) executeRealtimeBuy(ctx context.Context, report *Report, signal realtime.Signal, quote PositionQuote, positionIndex int, eventID string, calendarDates []string, date string) error {
	if report == nil {
		return fmt.Errorf("影子账户未初始化")
	}
	if signal.InvalidationPrice > 0 && quote.Price <= signal.InvalidationPrice {
		reason := fmt.Sprintf("当前报价 %.2f 已跌破信号失效位 %.2f，不执行盘中买入", quote.Price, signal.InvalidationPrice)
		appendRealtimeRejection(report, signal, "buy", date, eventID, quote, reason)
		return fmt.Errorf("%s", reason)
	}
	if realtimeBuyBlocked(quote) {
		reason := "当前报价触及涨停或不可交易，盘中模拟买入被拒绝"
		appendRealtimeRejection(report, signal, "buy", date, eventID, quote, reason)
		return fmt.Errorf("%s", reason)
	}
	capacity, err := e.realtimeCapacity(ctx, signal.Symbol, date, report.Config)
	if err != nil {
		return err
	}
	active := realtimeActivePositions(report.Positions)
	position, exists := active[signal.Symbol]
	deployedToday := realtimeDailyDeployment(*report, date)
	budget, targetPercent, reason := entryBudget(report.Config, signal, quote.Price, report.RemainingCash, deployedToday, active, position, exists)
	if budget <= 0 {
		return fmt.Errorf("%s", reason)
	}
	quantity := quantityWithinBudget(quote.Price, budget, capacity, report.Config)
	if quantity < report.Config.LotSize {
		reason = "资金或成交额容量不足一手"
		appendRealtimeRejection(report, signal, "buy", date, eventID, quote, reason)
		return fmt.Errorf("%s", reason)
	}
	buy := makeOrder(signal, "buy", date, quote.Price, quantity, report.Config, capacity)
	buy.ID = eventID + "-buy"
	buy.EventID = eventID
	buy.EventSource = "realtime-signal"
	buy.Status = OrderFilled
	buy.ExecutionTime = quote.QuoteTime
	buy.PositionAction = "open"
	buy.PositionSequence = 1
	if positionIndex >= 0 {
		buy.PositionAction = "add"
		buy.PositionSequence = len(report.Positions[positionIndex].Lots) + 1
	}
	fee := transactionFee(buy.Amount, "buy", report.Config)
	if buy.Amount+fee > report.RemainingCash || report.RemainingCash-buy.Amount-fee < report.Config.InitialCash*report.Config.CashReservePercent/100 {
		return fmt.Errorf("现金缓冲不足，等待后续盘中信号")
	}
	report.RemainingCash -= buy.Amount + fee
	report.Orders = append(report.Orders, buy)
	report.FilledEntries++
	report.TotalTurnover += buy.Amount
	report.TotalFees += fee
	targetExit := holdingExitDate(calendarDates, date, report.Config.HoldingDays)
	lot := ShadowPositionLot{
		OrderID: buy.ID, EntryDate: date, EntryTime: quote.QuoteTime, Quantity: quantity,
		EntryPrice: buy.Price, EntryAmount: buy.Amount, EntryFee: fee, SignalID: signal.ID,
		Industry: signal.Industry, SignalDate: date, SignalClose: signal.Price,
		SignalScore: shadowSignalScoreForConfig(signal, report.Config), TriggerPrice: signal.TriggerPrice,
		InvalidationPrice: signal.InvalidationPrice, SignalReasons: append([]string(nil), signal.Reasons...),
		TargetExitDate: targetExit,
	}
	if positionIndex >= 0 {
		position := report.Positions[positionIndex]
		position.Lots = append(position.Lots, lot)
		position.SignalID = signal.ID
		position.SignalDate = date
		position.SignalScore = shadowSignalScoreForConfig(signal, report.Config)
		position.SignalClose = signal.Price
		position.TriggerPrice = signal.TriggerPrice
		position.InvalidationPrice = signal.InvalidationPrice
		position.SignalReasons = append([]string(nil), signal.Reasons...)
		position.TargetPositionPercent = targetPercent
		position = applyRealtimePositionMetrics(position, quote, date)
		report.Positions[positionIndex] = position
		appendRealtimeDecision(report, signal, date, "add", "盘中信号显著增强，按风险预算增加一个批次；新增批次当日不可卖", position.Quantity, targetPercent, eventID, quote)
	} else {
		position := ShadowOpenPosition{
			SignalID: signal.ID, Symbol: signal.Symbol, Name: signal.Name, Industry: signal.Industry,
			SignalDate: date, SignalClose: signal.Price, SignalScore: shadowSignalScoreForConfig(signal, report.Config),
			TriggerPrice: signal.TriggerPrice, InvalidationPrice: signal.InvalidationPrice,
			SignalReasons: append([]string(nil), signal.Reasons...), Lots: []ShadowPositionLot{lot},
			TargetPositionPercent: targetPercent,
		}
		position = applyRealtimePositionMetrics(position, quote, date)
		report.Positions = append(report.Positions, position)
		appendRealtimeDecision(report, signal, date, "open", "当前盘中信号满足组合门槛，按风险预算首次建仓；当日买入受 T+1 锁定", position.Quantity, targetPercent, eventID, quote)
	}
	report.OpenPositions = len(report.Positions)
	return nil
}

func (e *Evaluator) realtimeCapacity(ctx context.Context, symbol, date string, cfg Config) (float64, error) {
	if e == nil || e.history == nil {
		return 0, fmt.Errorf("历史成交额不可用，暂停盘中建仓")
	}
	bars, err := e.history.FetchDailyBars(ctx, symbol)
	if err != nil {
		return 0, fmt.Errorf("%s 历史成交额不可用，暂停盘中建仓: %w", symbol, err)
	}
	bars = normalizedBars(bars)
	endIndex := -1
	for index := len(bars) - 1; index >= 0; index-- {
		if bars[index].Date < date {
			endIndex = index
			break
		}
	}
	average := trailingAverageAmount(bars, endIndex, 20)
	if average <= 0 {
		return 0, fmt.Errorf("%s 缺少已完成交易日成交额，暂停盘中建仓", symbol)
	}
	return average * cfg.MaxParticipationPercent / 100, nil
}

func executeRealtimeSell(report *Report, position *ShadowOpenPosition, indexes []int, signal realtime.Signal, quote PositionQuote, eventID, action, reason string, calendarDates []string) int {
	if report == nil || position == nil || len(indexes) == 0 {
		return 0
	}
	wanted := make(map[int]bool, len(indexes))
	for _, index := range indexes {
		wanted[index] = true
	}
	remaining := make([]ShadowPositionLot, 0, len(position.Lots)-len(indexes))
	filled := 0
	for index, lot := range position.Lots {
		if lot.Quantity <= 0 {
			continue
		}
		if !wanted[index] {
			remaining = append(remaining, lot)
			continue
		}
		lotSignal := signal
		if lotSignal.Symbol == "" {
			lotSignal = positionSignal(*position, time.Now())
		}
		sell := makeOrder(lotSignal, "sell", quote.QuoteTime[:10], quote.Price, lot.Quantity, report.Config, 0)
		sell.ID = fmt.Sprintf("%s-sell-%d", eventID, index+1)
		sell.EventID = eventID
		sell.EventSource = "realtime-risk"
		if action == "reduce" {
			sell.EventSource = "realtime-signal"
		}
		sell.Status = OrderFilled
		sell.ExecutionTime = quote.QuoteTime
		sell.PositionAction = action
		sell.PositionSequence = index + 1
		sell.Reason = reason
		fee := transactionFee(sell.Amount, "sell", report.Config)
		report.RemainingCash += sell.Amount - fee
		report.Orders = append(report.Orders, sell)
		report.Trades = append(report.Trades, tradeForRealtimeLot(lot, sell, quote.Price, fee, len(report.Trades)+1, calendarDates))
		report.CompletedTrades++
		report.TotalTurnover += sell.Amount
		report.TotalFees += fee
		filled += lot.Quantity
	}
	position.Lots = remaining
	if len(remaining) == 0 {
		position.Quantity = 0
		position.AvailableQuantity = 0
		position.MarketValue = 0
		return filled
	}
	*position = applyRealtimePositionMetrics(*position, quote, quote.QuoteTime[:10])
	return filled
}

// executeRealtimePartialSell closes only part of the oldest sellable lot. The
// remainder keeps its original cost basis, while the sold slice is recorded as
// a completed trade so realized P&L remains auditable.
func executeRealtimePartialSell(report *Report, position *ShadowOpenPosition, quantity int, signal realtime.Signal, quote PositionQuote, eventID string, calendarDates []string, date string) int {
	if report == nil || position == nil || quantity <= 0 || realtimeSellBlocked(quote) {
		return 0
	}
	lotIndex := -1
	for index, lot := range position.Lots {
		if lot.Quantity <= 0 || lot.EntryDate == "" || lot.EntryDate >= date {
			continue
		}
		if lotIndex < 0 || lot.EntryDate < position.Lots[lotIndex].EntryDate {
			lotIndex = index
		}
	}
	if lotIndex < 0 {
		return 0
	}
	lot := position.Lots[lotIndex]
	if quantity > lot.Quantity {
		quantity = lot.Quantity
	}
	if quantity <= 0 {
		return 0
	}
	soldLot := lot
	ratio := float64(quantity) / float64(lot.Quantity)
	soldLot.Quantity = quantity
	soldLot.EntryAmount = lot.EntryAmount * ratio
	soldLot.EntryFee = lot.EntryFee * ratio
	lot.Quantity -= quantity
	lot.EntryAmount -= soldLot.EntryAmount
	lot.EntryFee -= soldLot.EntryFee
	if lot.Quantity == 0 {
		position.Lots = append(position.Lots[:lotIndex], position.Lots[lotIndex+1:]...)
	} else {
		position.Lots[lotIndex] = lot
	}
	if signal.Symbol == "" {
		signal = positionSignal(*position, time.Now())
	}
	sell := makeOrder(signal, "sell", date, quote.Price, quantity, report.Config, 0)
	sell.ID = eventID + "-sell"
	sell.EventID = eventID
	sell.EventSource = "realtime-t"
	sell.Status = OrderFilled
	sell.ExecutionTime = quote.QuoteTime
	sell.PositionAction = "t_reduce"
	sell.PositionSequence = lotIndex + 1
	sell.Reason = "做T高抛，保留核心仓位"
	fee := transactionFee(sell.Amount, "sell", report.Config)
	report.RemainingCash += sell.Amount - fee
	report.Orders = append(report.Orders, sell)
	report.Trades = append(report.Trades, tradeForRealtimeLot(soldLot, sell, quote.Price, fee, len(report.Trades)+1, calendarDates))
	report.CompletedTrades++
	report.TotalTurnover += sell.Amount
	report.TotalFees += fee
	*position = applyRealtimePositionMetrics(*position, quote, date)
	return quantity
}

func executeRealtimeTRebuy(report *Report, position *ShadowOpenPosition, signal realtime.Signal, quote PositionQuote, sale ShadowOrder, quantity int, eventID string, calendarDates []string, date string) bool {
	if report == nil || position == nil || quantity <= 0 || realtimeBuyBlocked(quote) {
		return false
	}
	buy := makeOrder(signal, "buy", date, quote.Price, quantity, report.Config, 0)
	buy.ID = eventID + "-buy"
	buy.EventID = eventID
	buy.EventSource = "realtime-t"
	buy.Status = OrderFilled
	buy.ExecutionTime = quote.QuoteTime
	buy.PositionAction = "t_rebuy"
	buy.PositionSequence = len(position.Lots) + 1
	buy.Reason = fmt.Sprintf("做T回补，承接此前卖出批次 %s", sale.ID)
	fee := transactionFee(buy.Amount, "buy", report.Config)
	if buy.Amount+fee > report.RemainingCash || report.RemainingCash-buy.Amount-fee < report.Config.InitialCash*report.Config.CashReservePercent/100 {
		return false
	}
	report.RemainingCash -= buy.Amount + fee
	report.Orders = append(report.Orders, buy)
	report.FilledEntries++
	report.TotalTurnover += buy.Amount
	report.TotalFees += fee
	lot := ShadowPositionLot{
		OrderID: buy.ID, EntryDate: date, EntryTime: quote.QuoteTime, Quantity: quantity,
		EntryPrice: buy.Price, EntryAmount: buy.Amount, EntryFee: fee, SignalID: signal.ID,
		Industry: signal.Industry, SignalDate: date, SignalClose: signal.Price,
		SignalScore: shadowSignalScoreForConfig(signal, report.Config), TriggerPrice: signal.TriggerPrice,
		InvalidationPrice: signal.InvalidationPrice, SignalReasons: append([]string(nil), signal.Reasons...),
		TargetExitDate: holdingExitDate(calendarDates, date, report.Config.HoldingDays),
	}
	position.Lots = append(position.Lots, lot)
	*position = applyRealtimePositionMetrics(*position, quote, date)
	return true
}

func realtimePendingT(report Report, symbol, date string) (ShadowOrder, int) {
	type sale struct {
		order     ShadowOrder
		remaining int
	}
	queue := make([]sale, 0)
	for _, order := range report.Orders {
		if order.Symbol != symbol || order.AttemptDate != date || order.Status != OrderFilled {
			continue
		}
		switch order.PositionAction {
		case "t_reduce":
			if order.Quantity > 0 {
				queue = append(queue, sale{order: order, remaining: order.Quantity})
			}
		case "t_rebuy":
			remaining := order.Quantity
			for index := range queue {
				if remaining <= 0 {
					break
				}
				used := queue[index].remaining
				if used > remaining {
					used = remaining
				}
				queue[index].remaining -= used
				remaining -= used
			}
		}
	}
	quantity := 0
	var latest ShadowOrder
	for _, item := range queue {
		if item.remaining <= 0 {
			continue
		}
		quantity += item.remaining
		if latest.ExecutionTime == "" || item.order.ExecutionTime > latest.ExecutionTime {
			latest = item.order
		}
	}
	return latest, quantity
}

func realtimeTDayRounds(report Report, symbol, date string) int {
	count := 0
	for _, order := range report.Orders {
		if order.Symbol == symbol && order.AttemptDate == date && order.Status == OrderFilled && order.PositionAction == "t_rebuy" {
			count++
		}
	}
	return count
}

func realtimeTReduceQuantity(position ShadowOpenPosition, date string, cfg Config) int {
	if position.Quantity <= 0 {
		return 0
	}
	core := int(math.Ceil(float64(position.Quantity)*cfg.TCorePositionPercent/100/float64(cfg.LotSize))) * cfg.LotSize
	if core >= position.Quantity {
		return 0
	}
	available := 0
	for _, lot := range position.Lots {
		if lot.EntryDate != "" && lot.EntryDate < date && lot.Quantity > 0 {
			available += lot.Quantity
		}
	}
	maxSell := position.Quantity - core
	if available < maxSell {
		maxSell = available
	}
	tranche := int(float64(position.Quantity)*cfg.TTranchePercent/100/float64(cfg.LotSize)) * cfg.LotSize
	if tranche < cfg.LotSize {
		tranche = cfg.LotSize
	}
	if tranche > maxSell {
		tranche = maxSell / cfg.LotSize * cfg.LotSize
	}
	return tranche
}

func realtimeTReduceEligible(position ShadowOpenPosition, quote PositionQuote, cfg Config, report Report, date string) bool {
	if !cfg.EnableIntradayT || position.Quantity <= 0 || position.RiskExitPending || quote.Price <= 0 || quote.AveragePrice <= 0 {
		return false
	}
	if !realtimeTCooldownElapsed(report, position.Symbol, date, quote.QuoteTime, cfg.TCooldownMinutes) {
		return false
	}
	if quote.Price <= position.EntryPrice || quote.Price < quote.AveragePrice*(1+cfg.TVWAPDeviationPercent/100) {
		return false
	}
	if realtimeTReduceQuantity(position, date, cfg) <= 0 || realtimeTDayRounds(report, position.Symbol, date) >= cfg.TMaxDailyRounds {
		return false
	}
	return true
}

func realtimeTCooldownElapsed(report Report, symbol, date, quoteTime string, cooldownMinutes int) bool {
	if cooldownMinutes <= 0 {
		return true
	}
	current, ok := parseRealtimeTimestamp(quoteTime)
	if !ok {
		// A source without a parseable timestamp cannot prove that a cooldown
		// was violated. The event-level de-duplication still prevents replay.
		return true
	}
	for index := len(report.Orders) - 1; index >= 0; index-- {
		order := report.Orders[index]
		if order.Symbol != symbol || order.AttemptDate != date || order.Status != OrderFilled || (order.PositionAction != "t_reduce" && order.PositionAction != "t_rebuy") {
			continue
		}
		at, parsed := parseRealtimeTimestamp(order.ExecutionTime)
		if !parsed {
			continue
		}
		return current.Sub(at) >= time.Duration(cooldownMinutes)*time.Minute
	}
	return true
}

// realtimeSignalCooldownElapsed debounces ordinary score-driven add/reduce
// operations.  It intentionally excludes risk exits and T rounds: those have
// independent safety/profitability gates and must remain responsive.
func realtimeSignalCooldownElapsed(report Report, symbol, date, quoteTime string, cooldownMinutes int) bool {
	if cooldownMinutes <= 0 {
		return true
	}
	current, ok := parseRealtimeTimestamp(quoteTime)
	if !ok {
		return true
	}
	for index := len(report.Orders) - 1; index >= 0; index-- {
		order := report.Orders[index]
		if order.Symbol != symbol || order.AttemptDate != date || order.Status != OrderFilled {
			continue
		}
		switch order.PositionAction {
		case "open", "add", "reduce":
		default:
			continue
		}
		at, parsed := parseRealtimeTimestamp(order.ExecutionTime)
		if !parsed {
			continue
		}
		if current.Before(at) {
			return false
		}
		return current.Sub(at) >= time.Duration(cooldownMinutes)*time.Minute
	}
	return true
}

func realtimeTRebuyEligible(sale ShadowOrder, quantity int, quote PositionQuote, cfg Config) bool {
	if quantity <= 0 || sale.Price <= 0 || quote.Price <= 0 || quote.AveragePrice <= 0 {
		return false
	}
	if quote.Price >= sale.Price*(1-cfg.TMinimumPriceGapPercent/100) || quote.Price > quote.AveragePrice {
		return false
	}
	// Use executable prices including slippage and all configured fees. The
	// expected round must clear both a percentage and a small absolute margin.
	sellAmount := sale.Price * float64(quantity)
	sellFee := transactionFee(sellAmount, "sell", cfg)
	buyPrice := quote.Price * (1 + cfg.SlippageBPS/10000)
	buyAmount := buyPrice * float64(quantity)
	buyFee := transactionFee(buyAmount, "buy", cfg)
	net := sellAmount - sellFee - buyAmount - buyFee
	minimum := math.Max(0.01, buyAmount*cfg.TMinimumNetProfitPercent/100)
	return net >= minimum
}

func tradeForRealtimeLot(lot ShadowPositionLot, sell ShadowOrder, rawExit, fee float64, sequence int, calendarDates []string) ShadowTrade {
	entryAmount := lot.EntryAmount
	if entryAmount <= 0 {
		entryAmount = lot.EntryPrice * float64(lot.Quantity)
	}
	entryCost := entryAmount + lot.EntryFee
	if entryCost <= entryAmount {
		entryCost = entryAmount
	}
	netProfit := sell.Amount - fee - entryCost
	theoretical := 0.0
	if lot.SignalClose > 0 {
		theoretical = (rawExit/lot.SignalClose - 1) * 100
	}
	executable := safeReturnPercent(netProfit, entryCost)
	return ShadowTrade{
		ID: fmt.Sprintf("ST%04d", sequence), EntryOrderID: lot.OrderID, ExitOrderID: sell.ID,
		Symbol: sell.Symbol, Name: sell.Name, Industry: sell.Industry, SignalDate: lot.SignalDate,
		EntryDate: lot.EntryDate, ExitDate: sell.AttemptDate, Quantity: lot.Quantity,
		SignalClose: lot.SignalClose, ExitClose: rawExit, EntryPrice: lot.EntryPrice, ExitPrice: sell.Price,
		TheoreticalReturnPercent: theoretical, ExecutableReturnPercent: executable,
		ExecutionGapPercent: executable - theoretical,
		GrossProfit:         (sell.Price - lot.EntryPrice) * float64(lot.Quantity), NetProfit: netProfit,
		TotalFee: lot.EntryFee + fee, HoldingDays: realtimeHoldingDays(calendarDates, lot.EntryDate, sell.AttemptDate),
		PositionAction: sell.PositionAction, ExitReason: sell.Reason, ExitTime: sell.ExecutionTime,
	}
}

func latestRealtimeSignals(signals []realtime.Signal, date string, now time.Time, cfg Config) []realtime.Signal {
	latest := make(map[string]realtime.Signal)
	latestAt := make(map[string]time.Time)
	for _, signal := range signals {
		if strings.TrimSpace(signal.Symbol) == "" || signalDate(signal) != date || !shadowSignalActionableForConfig(signal, cfg) {
			continue
		}
		at, ok := realtimeSignalTime(signal)
		if !ok || at.After(now.Add(2*time.Minute)) || now.Sub(at) > 10*time.Minute || !realtimeExecutionWindow(at) {
			continue
		}
		previousAt, found := latestAt[signal.Symbol]
		if !found || at.After(previousAt) || (at.Equal(previousAt) && shadowSignalScoreForConfig(signal, cfg) > shadowSignalScoreForConfig(latest[signal.Symbol], cfg)) {
			latest[signal.Symbol] = signal
			latestAt[signal.Symbol] = at
		}
	}
	result := make([]realtime.Signal, 0, len(latest))
	for _, signal := range latest {
		result = append(result, signal)
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, _ := realtimeSignalTime(result[i])
		right, _ := realtimeSignalTime(result[j])
		if left.Equal(right) {
			if shadowSignalScoreForConfig(result[i], cfg) == shadowSignalScoreForConfig(result[j], cfg) {
				return result[i].Symbol < result[j].Symbol
			}
			return shadowSignalScoreForConfig(result[i], cfg) > shadowSignalScoreForConfig(result[j], cfg)
		}
		return left.Before(right)
	})
	return result
}

func realtimeSignalTime(signal realtime.Signal) (time.Time, bool) {
	if value, ok := parseRealtimeTimestamp(signal.QuoteTime); ok {
		return value, true
	}
	if !signal.AsOf.IsZero() {
		return signal.AsOf.In(shanghaiLocation), true
	}
	return time.Time{}, false
}

func realtimeQuoteMap(quotes []PositionQuote, date string, now time.Time) map[string]PositionQuote {
	result := make(map[string]PositionQuote)
	for _, quote := range quotes {
		at, ok := parseRealtimeTimestamp(quote.QuoteTime)
		if quote.Symbol == "" || quote.Price <= 0 || !finite(quote.Price) || !ok || localTradingDate(at) != date || at.After(now.Add(2*time.Minute)) || now.Sub(at) > 10*time.Minute || !realtimeExecutionWindow(at) {
			continue
		}
		if previous, found := result[quote.Symbol]; !found || quote.QuoteTime > previous.QuoteTime {
			result[quote.Symbol] = quote
		}
	}
	return result
}

func realtimeExecutionWindow(value time.Time) bool {
	local := value.In(shanghaiLocation)
	minutes := local.Hour()*60 + local.Minute()
	return minutes >= 9*60+30 && minutes <= 11*60+30 || minutes >= 13*60 && minutes < 15*60
}

func parseRealtimeTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		parsed, err := time.ParseInLocation(layout, value, shanghaiLocation)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func localTradingDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.In(shanghaiLocation).Format("2006-01-02")
}

func realtimeEventID(kind, symbol, quoteTime, sourceID string) string {
	stamp := strings.NewReplacer("-", "", ":", "", " ", "T").Replace(strings.TrimSpace(quoteTime))
	if stamp == "" {
		stamp = "unknown"
	}
	sourceID = strings.TrimSpace(sourceID)
	if len(sourceID) > 48 {
		sourceID = sourceID[len(sourceID)-48:]
	}
	return fmt.Sprintf("rt-%s-%s-%s-%s", stamp, symbol, kind, sourceID)
}

func realtimeEventHandled(report Report, eventID string) bool {
	if eventID == "" {
		return false
	}
	for _, order := range report.Orders {
		if order.EventID == eventID {
			return true
		}
	}
	for _, rejection := range report.Rejections {
		if rejection.EventID == eventID {
			return true
		}
	}
	for _, decision := range report.Decisions {
		if decision.EventID == eventID {
			return true
		}
	}
	return false
}

func appendRealtimeDecision(report *Report, signal realtime.Signal, date, action, reason string, quantity int, targetPercent float64, eventID string, quote PositionQuote) {
	if report == nil || realtimeDecisionHandled(*report, eventID) {
		return
	}
	report.Decisions = append(report.Decisions, ShadowDecision{
		ID: eventID + "-decision", EventID: eventID, EventTime: quote.QuoteTime, EventSource: "realtime",
		Symbol: signal.Symbol, Name: signal.Name, Industry: signal.Industry, Date: date,
		Action: action, Reason: reason, SignalScore: shadowSignalScoreForConfig(signal, report.Config),
		SignalReasons: append([]string(nil), signal.Reasons...), CurrentQuantity: quantity,
		TargetPositionPercent: targetPercent,
	})
}

func appendRealtimeRejection(report *Report, signal realtime.Signal, side, date, eventID string, quote PositionQuote, reason string) {
	if report == nil || realtimeRejectionHandled(*report, eventID) {
		return
	}
	report.Rejections = append(report.Rejections, ShadowRejection{
		OrderID: eventID + "-" + side, EventID: eventID, EventTime: quote.QuoteTime, EventSource: "realtime",
		Symbol: signal.Symbol, Name: signal.Name, Industry: signal.Industry, Side: side,
		SignalDate: signalDate(signal), AttemptDate: date, Reason: reason,
		SignalScore: shadowSignalScoreForConfig(signal, report.Config), SignalReasons: append([]string(nil), signal.Reasons...),
	})
	report.RejectedOrders++
}

func realtimeDecisionHandled(report Report, eventID string) bool {
	for _, decision := range report.Decisions {
		if decision.EventID == eventID {
			return true
		}
	}
	return false
}

func realtimeRejectionHandled(report Report, eventID string) bool {
	for _, rejection := range report.Rejections {
		if rejection.EventID == eventID {
			return true
		}
	}
	return false
}

func realtimeBuyBlocked(quote PositionQuote) bool {
	return quote.Price <= 0 || quote.LimitUp > 0 && quote.Price >= quote.LimitUp-0.0001
}

func realtimeSellBlocked(quote PositionQuote) bool {
	return quote.Price <= 0 || quote.LimitDown > 0 && quote.Price <= quote.LimitDown+0.0001
}

func realtimeRiskExitReason(position ShadowOpenPosition, price float64, cfg Config) string {
	if price <= 0 || position.Quantity <= 0 {
		return ""
	}
	invalidation := position.InvalidationPrice
	for _, lot := range position.Lots {
		if lot.InvalidationPrice > invalidation {
			invalidation = lot.InvalidationPrice
		}
	}
	if invalidation > 0 && price <= invalidation {
		return fmt.Sprintf("盘中报价 %.2f 跌破失效位 %.2f，退出全部满足 T+1 的批次", price, invalidation)
	}
	cost := realtimePositionCost(position)
	if cost <= 0 {
		return ""
	}
	returnPercent := (price*float64(position.Quantity)/cost - 1) * 100
	if returnPercent <= -cfg.MaxLossPercent {
		return fmt.Sprintf("盘中亏损 %.2f%% 达到单股最大亏损 %.2f%%，退出全部满足 T+1 的批次", returnPercent, cfg.MaxLossPercent)
	}
	return ""
}

func realtimeSellableLotIndexes(lots []ShadowPositionLot, date string, oldestOnly bool) []int {
	result := make([]int, 0, len(lots))
	for index, lot := range lots {
		if lot.EntryDate != "" && lot.EntryDate < date && lot.Quantity > 0 {
			result = append(result, index)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return lots[result[i]].EntryDate < lots[result[j]].EntryDate
	})
	if oldestOnly && len(result) > 1 {
		return result[:1]
	}
	return result
}

func normalizeRealtimePositions(positions []ShadowOpenPosition, cfg Config, calendarDates []string, date string) []ShadowOpenPosition {
	result := append([]ShadowOpenPosition(nil), positions...)
	for index := range result {
		position := result[index]
		position.Lots = realtimePositionLots(position, cfg, calendarDates)
		position = applyRealtimePositionMetrics(position, PositionQuote{}, date)
		result[index] = position
	}
	return result
}

func realtimePositionLots(position ShadowOpenPosition, cfg Config, calendarDates []string) []ShadowPositionLot {
	if len(position.Lots) > 0 {
		lots := make([]ShadowPositionLot, 0, len(position.Lots))
		for _, source := range position.Lots {
			if source.Quantity > 0 {
				lots = append(lots, source)
			}
		}
		for index := range lots {
			lots[index].SignalReasons = append([]string(nil), lots[index].SignalReasons...)
			if lots[index].TargetExitDate == "" {
				lots[index].TargetExitDate = holdingExitDate(calendarDates, lots[index].EntryDate, cfg.HoldingDays)
			}
		}
		return lots
	}
	if position.Quantity <= 0 {
		return nil
	}
	amount := position.EntryAmount
	if amount <= 0 {
		amount = position.EntryPrice * float64(position.Quantity)
	}
	fee := position.EntryFee
	if fee <= 0 && amount > 0 {
		fee = transactionFee(amount, "buy", cfg)
	}
	orderID := position.SignalID + "-buy"
	if strings.TrimSpace(orderID) == "-buy" {
		orderID = position.Symbol + "-" + position.EntryDate + "-buy"
	}
	return []ShadowPositionLot{{
		OrderID: orderID, EntryDate: position.EntryDate, EntryTime: position.EntryTime,
		Quantity: position.Quantity, EntryPrice: position.EntryPrice, EntryAmount: amount, EntryFee: fee,
		SignalID: position.SignalID, Industry: position.Industry, SignalDate: position.SignalDate,
		SignalClose: position.SignalClose, SignalScore: position.SignalScore,
		TriggerPrice: position.TriggerPrice, InvalidationPrice: position.InvalidationPrice,
		SignalReasons:  append([]string(nil), position.SignalReasons...),
		TargetExitDate: holdingExitDate(calendarDates, position.EntryDate, cfg.HoldingDays),
	}}
}

func applyRealtimePositionMetrics(position ShadowOpenPosition, quote PositionQuote, date string) ShadowOpenPosition {
	quantity, available := 0, 0
	entryAmount, entryFee := 0.0, 0.0
	entryDate, entryTime, targetExit := "", "", ""
	for _, lot := range position.Lots {
		quantity += lot.Quantity
		entryAmount += lot.EntryAmount
		entryFee += lot.EntryFee
		if lot.EntryDate < date {
			available += lot.Quantity
		}
		if entryDate == "" || lot.EntryDate < entryDate {
			entryDate, entryTime = lot.EntryDate, lot.EntryTime
		}
		if targetExit == "" || lot.TargetExitDate > targetExit {
			targetExit = lot.TargetExitDate
		}
	}
	position.Quantity = quantity
	position.AvailableQuantity = available
	position.EntryDate = entryDate
	position.EntryTime = entryTime
	position.EntryAmount = entryAmount
	position.EntryFee = entryFee
	position.TargetExitDate = targetExit
	position.AdditionCount = maxInt(0, len(position.Lots)-1)
	if quantity > 0 {
		position.EntryPrice = entryAmount / float64(quantity)
	}
	if quote.Price > 0 {
		position.LastDate = date
		position.LastPrice = quote.Price
		position.MarketValue = quote.Price * float64(quantity)
		position.UnrealizedProfit = position.MarketValue - entryAmount - entryFee
		position.UnrealizedReturnPercent = safeReturnPercent(position.UnrealizedProfit, entryAmount+entryFee)
		position.ValuationTime = quote.QuoteTime
		position.ValuationSource = quote.Source
		position.RealtimeValuation = true
	}
	return position
}

func realtimePositionIndex(positions []ShadowOpenPosition, symbol string) int {
	for index := range positions {
		if positions[index].Symbol == symbol {
			return index
		}
	}
	return -1
}

func realtimeActivePositions(positions []ShadowOpenPosition) map[string]shadowPosition {
	result := make(map[string]shadowPosition, len(positions))
	for index := range positions {
		copied := positions[index]
		result[copied.Symbol] = shadowPosition{fallback: &copied, targetPercent: copied.TargetPositionPercent, lastSignalScore: copied.SignalScore, lastSignalDate: copied.SignalDate}
	}
	return result
}

func realtimePositionCost(position ShadowOpenPosition) float64 {
	amount := position.EntryAmount
	if amount <= 0 {
		amount = position.EntryPrice * float64(position.Quantity)
	}
	return amount + position.EntryFee
}

func realtimeDailyDeployment(report Report, date string) float64 {
	total := 0.0
	for _, order := range report.Orders {
		if order.Status == OrderFilled && order.Side == "buy" && order.AttemptDate == date {
			total += order.Amount + transactionFee(order.Amount, "buy", report.Config)
		}
	}
	return total
}

func positionSignal(position ShadowOpenPosition, at time.Time) realtime.Signal {
	signal := realtime.Signal{
		ID: position.SignalID, Symbol: position.Symbol, Name: position.Name, Industry: position.Industry,
		Score: position.SignalScore, Price: position.SignalClose, TriggerPrice: position.TriggerPrice,
		InvalidationPrice: position.InvalidationPrice, Reasons: append([]string(nil), position.SignalReasons...),
		AsOf: at.In(shanghaiLocation), State: realtime.StateWatching,
	}
	if signal.ID == "" {
		signal.ID = position.Symbol + "-position"
	}
	return signal
}

func realtimeHoldingDays(calendarDates []string, entryDate, exitDate string) int {
	entry := sort.SearchStrings(calendarDates, entryDate)
	exit := sort.SearchStrings(calendarDates, exitDate)
	if entry < len(calendarDates) && exit < len(calendarDates) && calendarDates[entry] == entryDate && calendarDates[exit] == exitDate && exit >= entry {
		return exit - entry + 1
	}
	entryTime, entryErr := time.ParseInLocation("2006-01-02", entryDate, shanghaiLocation)
	exitTime, exitErr := time.ParseInLocation("2006-01-02", exitDate, shanghaiLocation)
	if entryErr == nil && exitErr == nil && !exitTime.Before(entryTime) {
		return int(math.Round(exitTime.Sub(entryTime).Hours()/24)) + 1
	}
	return 0
}

func countRealtimeFilledOrders(orders []ShadowOrder) int {
	count := 0
	for _, order := range orders {
		if order.Status == OrderFilled && strings.HasPrefix(order.EventSource, "realtime") {
			count++
		}
	}
	return count
}

func countRealtimeEvents(report Report) int {
	events := make(map[string]struct{})
	add := func(eventID, fallback string) {
		key := strings.TrimSpace(eventID)
		if key == "" {
			key = strings.TrimSpace(fallback)
		}
		if key != "" {
			events[key] = struct{}{}
		}
	}
	for _, order := range report.Orders {
		if !strings.HasPrefix(order.EventSource, "realtime") {
			continue
		}
		add(order.EventID, strings.Join([]string{order.Symbol, order.Side, order.PositionAction, order.ExecutionTime, order.AttemptDate}, "|"))
	}
	for _, rejection := range report.Rejections {
		if rejection.EventSource != "realtime" {
			continue
		}
		add(rejection.EventID, strings.Join([]string{rejection.Symbol, rejection.Side, rejection.EventTime, rejection.AttemptDate}, "|"))
	}
	for _, decision := range report.Decisions {
		if decision.EventSource != "realtime" {
			continue
		}
		add(decision.EventID, strings.Join([]string{decision.Symbol, decision.Action, decision.EventTime, decision.Date}, "|"))
	}
	return len(events)
}

func appendUniqueWarning(warnings []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return warnings
	}
	for _, warning := range warnings {
		if warning == value {
			return warnings
		}
	}
	return append(warnings, value)
}
