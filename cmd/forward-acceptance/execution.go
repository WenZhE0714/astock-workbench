package main

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"slices"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
)

type accountExecution struct {
	Account        string `json:"account"`
	FirstBuy       string `json:"first_buy,omitempty"`
	FirstSell      string `json:"first_sell,omitempty"`
	Buys           int    `json:"buys"`
	Sells          int    `json:"sells"`
	DeferredOrders int    `json:"deferred_orders"`
}

type executionAcceptance struct {
	Status          string             `json:"status"`
	Detail          string             `json:"detail"`
	VerifiedThrough string             `json:"verified_through,omitempty"`
	Transitions     int                `json:"transitions"`
	Accounts        []accountExecution `json:"accounts"`
}

func newExecutionAcceptance() executionAcceptance {
	result := executionAcceptance{Status: "waiting-first-day", Detail: "等待首日日档，尚无成交验收证据"}
	for _, name := range []string{"原规则", "量比1.5", "原规则双倍成本", "量比1.5双倍成本"} {
		result.Accounts = append(result.Accounts, accountExecution{Account: name})
	}
	return result
}

func (result *executionAcceptance) record(date string, changes []accountExecution) {
	result.Transitions++
	result.VerifiedThrough = date
	filled := 0
	for i, change := range changes {
		account := &result.Accounts[i]
		account.Buys += change.Buys
		account.Sells += change.Sells
		account.DeferredOrders += change.DeferredOrders
		if account.FirstBuy == "" && change.Buys > 0 {
			account.FirstBuy = date
		}
		if account.FirstSell == "" && change.Sells > 0 {
			account.FirstSell = date
		}
		if account.Buys > 0 {
			filled++
		}
	}
	switch filled {
	case 0:
		result.Status, result.Detail = "waiting-fill", "已核对后续日档，四组均未发生首次买入"
	case 4:
		result.Status, result.Detail = "passed", "四组首次买入已核验，并持续核对后续成交；卖出覆盖见各组记录"
	default:
		result.Status, result.Detail = "partial", fmt.Sprintf("%d/4组已发生且通过首次买入核验，其余组继续等待", filled)
	}
}

type sealedExecutionBars map[string][]domain.DailyBar

func (bars sealedExecutionBars) FetchDailyBarsRange(ctx context.Context, symbol string, start, end time.Time, adjustment backtest.PriceAdjustment) ([]domain.DailyBar, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if adjustment != backtest.AdjustmentNone {
		return nil, fmt.Errorf("成交验收只支持冻结的不复权日K")
	}
	var result []domain.DailyBar
	for _, bar := range bars[symbol] {
		if bar.Date >= start.Format(time.DateOnly) && bar.Date <= end.Format(time.DateOnly) {
			result = append(result, bar)
		}
	}
	return result, nil
}

func near(a, b float64) bool {
	return !math.IsNaN(a) && !math.IsNaN(b) && !math.IsInf(a, 0) && !math.IsInf(b, 0) && math.Abs(a-b) <= 1e-6
}

func sameItems[T any](a, b []T) bool {
	return slices.EqualFunc(a, b, func(x, y T) bool { return reflect.DeepEqual(x, y) })
}

func compareExecutionReplay(actual, expected backtest.Result) error {
	a, err := backtest.ForwardFingerprint(actual.Request)
	if err != nil {
		return err
	}
	b, err := backtest.ForwardFingerprint(expected.Request)
	if err != nil || a != b {
		return fmt.Errorf("账户规则或成本与冻结配置不一致")
	}
	if !sameItems(actual.Trades, expected.Trades) || !sameItems(actual.OpenPositions, expected.OpenPositions) || !sameItems(actual.PendingOrders, expected.PendingOrders) || !sameItems(actual.VolumeBlockedEntries, expected.VolumeBlockedEntries) {
		return fmt.Errorf("成交、持仓或待执行信号与封存数据复算不一致")
	}
	if len(actual.Equity) != len(expected.Equity) {
		return fmt.Errorf("净值序列长度与复算不一致")
	}
	for i, point := range actual.Equity {
		want := expected.Equity[i]
		if point.Date != want.Date || !near(point.Cash, want.Cash) || !near(point.Positions, want.Positions) || !near(point.Equity, want.Equity) || !near(point.Drawdown, want.Drawdown) {
			return fmt.Errorf("%s 现金、持仓市值或净值与复算不一致", point.Date)
		}
	}
	x, y := actual.Metrics, expected.Metrics
	if x.Trades != y.Trades || !near(x.TotalFees, y.TotalFees) || !near(x.FinalEquity, y.FinalEquity) || !near(x.TotalReturn, y.TotalReturn) || !near(x.MaxDrawdown, y.MaxDrawdown) || !near(x.Turnover, y.Turnover) {
		return fmt.Errorf("累计成交、费用或净值指标与复算不一致")
	}
	return nil
}

func verifyFill(request backtest.Request, fill backtest.Fill, bar domain.DailyBar, side, signalDate string) error {
	if fill.Date != bar.Date || signalDate >= fill.Date || fill.Side != side || fill.Forced || fill.Quantity <= 0 || fill.Quantity%100 != 0 || math.Abs(bar.High-bar.Low) < 1e-9 {
		return fmt.Errorf("成交日期、方向、整手数量或一字板规则不符")
	}
	direction, stampRate := 1.0, 0.0
	if side == "sell" {
		direction, stampRate = -1, request.StampDutyRate
	}
	price := bar.Open * (1 + direction*request.SlippageBPS/10000)
	amount := price * float64(fill.Quantity)
	commission := math.Max(request.MinimumCommission, amount*request.CommissionRate)
	stamp, transfer := amount*stampRate, amount*request.TransferFeeRate
	if !near(fill.RawPrice, bar.Open) || !near(fill.Price, price) || !near(fill.SlippageBPS, request.SlippageBPS) || !near(fill.Amount, amount) || !near(fill.Commission, commission) || !near(fill.StampDuty, stamp) || !near(fill.TransferFee, transfer) || !near(fill.TotalFee, commission+stamp+transfer) {
		return fmt.Errorf("开盘成交价、滑点或费用计算不符")
	}
	return nil
}

// Audit the opening ledger independently of the signal engine, in its frozen
// symbol order. Earlier fills change the cash and equity budget of later buys.
func verifyOpeningLedger(previous, current backtest.Result, bars map[string]domain.DailyBar, date string) (accountExecution, error) {
	var result accountExecution
	if len(previous.Equity) == 0 || len(current.Equity) != len(previous.Equity)+1 || len(current.Trades) < len(previous.Trades) || !sameItems(previous.Trades, current.Trades[:len(previous.Trades)]) {
		return result, fmt.Errorf("历史成交或净值被改写")
	}
	prior := map[string]backtest.OpenPosition{}
	positions := map[string]backtest.OpenPosition{}
	orders := map[string]backtest.ForwardPendingOrder{}
	for _, position := range previous.OpenPositions {
		if _, exists := prior[position.Symbol]; exists {
			return result, fmt.Errorf("重复的历史持仓")
		}
		prior[position.Symbol], positions[position.Symbol] = position, position
	}
	for _, order := range previous.PendingOrders {
		if _, exists := orders[order.Symbol]; exists || order.Signal.Date > previous.Equity[len(previous.Equity)-1].Date || order.Signal.Date >= date {
			return result, fmt.Errorf("重复或尚未封存的待执行信号")
		}
		orders[order.Symbol] = order
	}
	type execution struct {
		fill   backtest.Fill
		signal backtest.SignalSnapshot
	}
	fills := map[string]execution{}
	for _, trade := range current.Trades[len(previous.Trades):] {
		position, exists := prior[trade.Symbol]
		if _, duplicate := fills[trade.Symbol]; duplicate || !exists || !reflect.DeepEqual(position.Entry, trade.Entry) || !reflect.DeepEqual(position.EntrySignal, trade.EntrySignal) || trade.Exit.Date != date || trade.Entry.Date >= date || trade.Exit.Quantity != position.Quantity {
			return result, fmt.Errorf("卖出没有对应历史持仓，或违反T+1及数量约束")
		}
		fills[trade.Symbol] = execution{fill: trade.Exit, signal: trade.ExitSignal}
	}
	seen := map[string]bool{}
	for _, position := range current.OpenPositions {
		if seen[position.Symbol] || position.Quantity != position.Entry.Quantity {
			return result, fmt.Errorf("重复持仓或持仓数量异常")
		}
		seen[position.Symbol] = true
		if old, exists := prior[position.Symbol]; exists {
			if !reflect.DeepEqual(old.Entry, position.Entry) || !reflect.DeepEqual(old.EntrySignal, position.EntrySignal) || old.Quantity != position.Quantity {
				return result, fmt.Errorf("历史持仓入场记录变化")
			}
		} else {
			if _, duplicate := fills[position.Symbol]; duplicate || position.Entry.Date != date {
				return result, fmt.Errorf("新持仓没有当日买入记录")
			}
			fills[position.Symbol] = execution{fill: position.Entry, signal: position.EntrySignal}
		}
	}
	cash := previous.Equity[len(previous.Equity)-1].Cash
	fees := previous.Metrics.TotalFees
	for symbol := range fills {
		if !slices.Contains(current.Request.Tickers, symbol) {
			return result, fmt.Errorf("股票池之外出现成交")
		}
	}
	for _, symbol := range current.Request.Tickers {
		bar, exists := bars[symbol]
		if !exists || bar.Date != date {
			return result, fmt.Errorf("%s 缺少当日日K，不能验收成交", symbol)
		}
		event, filled := fills[symbol]
		order, pending := orders[symbol]
		if pending && math.Abs(bar.High-bar.Low) < 1e-9 {
			found := false
			for _, retained := range current.PendingOrders {
				found = found || reflect.DeepEqual(order, retained)
			}
			if filled || !found {
				return result, fmt.Errorf("一字板待执行信号未正确顺延")
			}
			result.DeferredOrders++
		}
		if !filled {
			continue
		}
		if !pending || order.Side != event.fill.Side || !reflect.DeepEqual(order.Signal, event.signal) {
			return result, fmt.Errorf("%s 成交未对应前一日封存的待执行信号", symbol)
		}
		if err := verifyFill(current.Request, event.fill, bar, order.Side, order.Signal.Date); err != nil {
			return result, fmt.Errorf("%s: %w", symbol, err)
		}
		fill := event.fill
		if fill.Side == "buy" {
			openingEquity := cash
			for _, held := range current.Request.Tickers {
				openingEquity += float64(positions[held].Quantity) * bars[held].Open
			}
			cost := fill.Amount + fill.TotalFee
			budget := math.Min(cash, openingEquity*current.Request.Technical.MaxPosition)
			if cost > budget+1e-6 {
				return result, fmt.Errorf("%s 买入金额含费用超过现金或单股仓位上限", symbol)
			}
			cash -= cost
			positions[symbol] = backtest.OpenPosition{Symbol: symbol, Quantity: fill.Quantity, Entry: fill, EntrySignal: event.signal}
			result.Buys++
		} else {
			cash += fill.Amount - fill.TotalFee
			delete(positions, symbol)
			result.Sells++
		}
		fees += fill.TotalFee
	}
	if len(positions) != len(current.OpenPositions) {
		return result, fmt.Errorf("收盘持仓无法由开盘成交还原")
	}
	marketValue := 0.0
	for _, position := range current.OpenPositions {
		held, exists := positions[position.Symbol]
		bar := bars[position.Symbol]
		value := bar.Close * float64(position.Quantity)
		cost := position.Entry.Amount + position.Entry.TotalFee
		if !exists || held.Quantity != position.Quantity || position.LastDate != date || !near(position.LastPrice, bar.Close) || !near(position.MarketValue, value) || !near(position.UnrealizedProfit, value-cost) || !near(position.ReturnPercent, (value/cost-1)*100) {
			return result, fmt.Errorf("%s 收盘持仓数量、估值或浮盈不符", position.Symbol)
		}
		marketValue += value
	}
	point := current.Equity[len(current.Equity)-1]
	if point.Date != date || cash < -1e-6 || !near(point.Cash, cash) || !near(point.Positions, marketValue) || !near(point.Equity, cash+marketValue) || !near(current.Metrics.TotalFees, fees) || !near(current.Metrics.FinalEquity, point.Equity) {
		return result, fmt.Errorf("现金、持仓市值、净值或累计费用未对平")
	}
	return result, nil
}

func verifyExecutionDay(ctx context.Context, plan backtest.VolumeForwardPlan, previous, current backtest.VolumeForwardDay) ([]accountExecution, error) {
	if current.PreviousHash != previous.Fingerprint || current.Date <= previous.Date || len(current.Runs) != 4 {
		return nil, fmt.Errorf("成交验收的前序日档不匹配")
	}
	replayed, err := backtest.BuildVolumeForwardDay(ctx, plan, &previous, sealedExecutionBars(current.Bars), current.CapturedAt)
	if err != nil {
		return nil, fmt.Errorf("封存数据无法按固定规则复算: %w", err)
	}
	bars := map[string]domain.DailyBar{}
	for symbol, items := range current.Bars {
		if len(items) > 0 {
			bars[symbol] = items[len(items)-1]
		}
	}
	var changes []accountExecution
	for i, run := range current.Runs {
		if err := compareExecutionReplay(run, replayed.Runs[i]); err != nil {
			return nil, fmt.Errorf("%s 账户%d: %w", current.Date, i, err)
		}
		change, err := verifyOpeningLedger(previous.Runs[i], run, bars, current.Date)
		if err != nil {
			return nil, fmt.Errorf("%s 账户%d: %w", current.Date, i, err)
		}
		changes = append(changes, change)
	}
	return changes, nil
}
