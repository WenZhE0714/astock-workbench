package paper

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

// HistoryClient is deliberately read-only. Shadow evaluation never implements
// or calls execution.Broker, so a research signal cannot become a real order.
type HistoryClient interface {
	FetchDailyBars(context.Context, string) ([]domain.DailyBar, error)
}

type Config struct {
	InitialCash             float64 `json:"initial_cash"`
	MinimumScore            float64 `json:"minimum_score"`
	MaxPositionPercent      float64 `json:"max_position_percent"`
	MaxParticipationPercent float64 `json:"max_participation_percent"`
	SlippageBPS             float64 `json:"slippage_bps"`
	CommissionRate          float64 `json:"commission_rate"`
	MinimumCommission       float64 `json:"minimum_commission"`
	StampDutyRate           float64 `json:"stamp_duty_rate"`
	TransferFeeRate         float64 `json:"transfer_fee_rate"`
	HoldingDays             int     `json:"holding_days"`
	LotSize                 int     `json:"lot_size"`
}

func DefaultConfig() Config {
	return Config{
		InitialCash: 1_000_000, MinimumScore: 55, MaxPositionPercent: 20,
		MaxParticipationPercent: 10, SlippageBPS: 5, CommissionRate: .0003,
		MinimumCommission: 5, StampDutyRate: .0005, TransferFeeRate: .00001,
		HoldingDays: 5, LotSize: 100,
	}
}

type Options struct {
	Config Config
	Limit  int
	Now    func() time.Time
}

type ShadowOrder struct {
	ID             string  `json:"id"`
	Symbol         string  `json:"symbol"`
	Name           string  `json:"name,omitempty"`
	Side           string  `json:"side"`
	SignalDate     string  `json:"signal_date"`
	AttemptDate    string  `json:"attempt_date"`
	Quantity       int     `json:"quantity"`
	RawPrice       float64 `json:"raw_price"`
	Price          float64 `json:"price"`
	Amount         float64 `json:"amount"`
	CapacityAmount float64 `json:"capacity_amount,omitempty"`
	Status         string  `json:"status"`
	Reason         string  `json:"reason,omitempty"`
}

const (
	OrderFilled   = "filled"
	OrderRejected = "rejected"
)

type ShadowRejection struct {
	OrderID     string `json:"order_id"`
	Symbol      string `json:"symbol"`
	Name        string `json:"name,omitempty"`
	Side        string `json:"side"`
	SignalDate  string `json:"signal_date"`
	AttemptDate string `json:"attempt_date,omitempty"`
	Reason      string `json:"reason"`
}

type ShadowTrade struct {
	ID                       string  `json:"id"`
	Symbol                   string  `json:"symbol"`
	Name                     string  `json:"name,omitempty"`
	SignalDate               string  `json:"signal_date"`
	EntryDate                string  `json:"entry_date"`
	ExitDate                 string  `json:"exit_date"`
	Quantity                 int     `json:"quantity"`
	SignalClose              float64 `json:"signal_close"`
	ExitClose                float64 `json:"exit_close"`
	EntryPrice               float64 `json:"entry_price"`
	ExitPrice                float64 `json:"exit_price"`
	TheoreticalReturnPercent float64 `json:"theoretical_return_percent"`
	ExecutableReturnPercent  float64 `json:"executable_return_percent"`
	ExecutionGapPercent      float64 `json:"execution_gap_percent"`
	GrossProfit              float64 `json:"gross_profit"`
	NetProfit                float64 `json:"net_profit"`
	TotalFee                 float64 `json:"total_fee"`
	HoldingDays              int     `json:"holding_days"`
}

type ShadowOpenPosition struct {
	Symbol                  string  `json:"symbol"`
	Name                    string  `json:"name,omitempty"`
	SignalDate              string  `json:"signal_date"`
	EntryDate               string  `json:"entry_date"`
	Quantity                int     `json:"quantity"`
	EntryPrice              float64 `json:"entry_price"`
	LastDate                string  `json:"last_date"`
	LastPrice               float64 `json:"last_price"`
	MarketValue             float64 `json:"market_value"`
	UnrealizedProfit        float64 `json:"unrealized_profit"`
	UnrealizedReturnPercent float64 `json:"unrealized_return_percent"`
	TargetExitDate          string  `json:"target_exit_date,omitempty"`
	ValuationTime           string  `json:"valuation_time,omitempty"`
	ValuationSource         string  `json:"valuation_source,omitempty"`
	RealtimeValuation       bool    `json:"realtime_valuation,omitempty"`
}

type PositionQuote struct {
	Symbol    string
	Price     float64
	QuoteTime string
	Source    string
}

type Report struct {
	EngineVersion            string               `json:"engine_version,omitempty"`
	GeneratedAt              time.Time            `json:"generated_at"`
	ValuedAt                 *time.Time           `json:"valued_at,omitempty"`
	AsOf                     string               `json:"as_of"`
	Config                   Config               `json:"config"`
	SignalCount              int                  `json:"signal_count"`
	CandidateCount           int                  `json:"candidate_count"`
	PendingCandidates        int                  `json:"pending_candidates"`
	FilledEntries            int                  `json:"filled_entries"`
	CompletedTrades          int                  `json:"completed_trades"`
	RejectedOrders           int                  `json:"rejected_orders"`
	OpenPositions            int                  `json:"open_positions"`
	InitialCash              float64              `json:"initial_cash"`
	RemainingCash            float64              `json:"remaining_cash"`
	TotalTurnover            float64              `json:"total_turnover"`
	TotalFees                float64              `json:"total_fees"`
	TheoreticalAverageReturn float64              `json:"theoretical_average_return_percent"`
	ExecutableAverageReturn  float64              `json:"executable_average_return_percent"`
	ExecutionGapPercent      float64              `json:"execution_gap_percent"`
	Trades                   []ShadowTrade        `json:"trades,omitempty"`
	Positions                []ShadowOpenPosition `json:"positions,omitempty"`
	Orders                   []ShadowOrder        `json:"orders,omitempty"`
	Rejections               []ShadowRejection    `json:"rejections,omitempty"`
	Warnings                 []string             `json:"warnings,omitempty"`
}

const ShadowEngineVersion = "tplus1-v2"

// RevaluePositions updates only mark-to-market fields. It never changes fills,
// quantities, cash or completed trades, so an intraday quote cannot become a
// simulated execution retroactively.
func RevaluePositions(report Report, quotes []PositionQuote, valuedAt time.Time) Report {
	bySymbol := make(map[string]PositionQuote, len(quotes))
	for _, quote := range quotes {
		if quote.Symbol != "" && quote.Price > 0 && finite(quote.Price) {
			bySymbol[quote.Symbol] = quote
		}
	}
	report.Positions = append([]ShadowOpenPosition(nil), report.Positions...)
	updated := false
	for index := range report.Positions {
		position := &report.Positions[index]
		quote, found := bySymbol[position.Symbol]
		if !found {
			continue
		}
		entryAmount := position.EntryPrice * float64(position.Quantity)
		entryCost := entryAmount + transactionFee(entryAmount, "buy", report.Config)
		marketValue := quote.Price * float64(position.Quantity)
		exitFee := transactionFee(marketValue, "sell", report.Config)
		profit := marketValue - exitFee - entryCost
		position.LastPrice = quote.Price
		position.MarketValue = marketValue
		position.UnrealizedProfit = profit
		if entryCost > 0 {
			position.UnrealizedReturnPercent = profit / entryCost * 100
		}
		position.ValuationTime = quote.QuoteTime
		position.ValuationSource = quote.Source
		position.RealtimeValuation = true
		updated = true
	}
	if updated {
		report.ValuedAt = &valuedAt
	}
	return report
}

// ValidateTransition protects the account ledger from retroactive replay
// changes. A later daily advance may add new events, but it cannot rewrite any
// order or trade already settled through the previous report date.
func ValidateTransition(previous, next Report) error {
	if previous.AsOf == "" || executionLedgerEmpty(previous) {
		return nil
	}
	if next.AsOf < previous.AsOf {
		return fmt.Errorf("候选账户日期 %s 早于现有账户日期 %s", next.AsOf, previous.AsOf)
	}
	previousOrders := ordersThrough(previous.Orders, previous.AsOf)
	nextOrders := ordersThrough(next.Orders, previous.AsOf)
	if len(previousOrders) != len(nextOrders) {
		return fmt.Errorf("截至 %s 的成交单数量从 %d 变为 %d", previous.AsOf, len(previousOrders), len(nextOrders))
	}
	for index := range previousOrders {
		if !sameSettledOrder(previousOrders[index], nextOrders[index]) {
			return fmt.Errorf("截至 %s 的成交单被改写: %s", previous.AsOf, previousOrders[index].ID)
		}
	}
	previousTrades := tradesThrough(previous.Trades, previous.AsOf)
	nextTrades := tradesThrough(next.Trades, previous.AsOf)
	if len(previousTrades) != len(nextTrades) {
		return fmt.Errorf("截至 %s 的完成交易数量从 %d 变为 %d", previous.AsOf, len(previousTrades), len(nextTrades))
	}
	for index := range previousTrades {
		if !sameSettledTrade(previousTrades[index], nextTrades[index]) {
			return fmt.Errorf("截至 %s 的完成交易被改写: %s", previous.AsOf, previousTrades[index].ID)
		}
	}
	for _, position := range previous.Positions {
		if nextPosition, found := matchingPosition(next.Positions, position); found {
			if !samePositionBasis(position, nextPosition) {
				return fmt.Errorf("持仓成本或数量被改写: %s", position.Symbol)
			}
			continue
		}
		if !positionClosedAfter(next.Trades, position, previous.AsOf) {
			return fmt.Errorf("已有持仓无后续卖出记录却消失: %s", position.Symbol)
		}
	}
	return nil
}

func executionLedgerEmpty(report Report) bool {
	return len(report.Orders) == 0 && len(report.Trades) == 0 && len(report.Positions) == 0
}

func ordersThrough(orders []ShadowOrder, date string) []ShadowOrder {
	result := make([]ShadowOrder, 0, len(orders))
	for _, order := range orders {
		if order.AttemptDate != "" && order.AttemptDate <= date {
			result = append(result, order)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].AttemptDate == result[j].AttemptDate {
			return result[i].ID < result[j].ID
		}
		return result[i].AttemptDate < result[j].AttemptDate
	})
	return result
}

func tradesThrough(trades []ShadowTrade, date string) []ShadowTrade {
	result := make([]ShadowTrade, 0, len(trades))
	for _, trade := range trades {
		if trade.ExitDate != "" && trade.ExitDate <= date {
			result = append(result, trade)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].ExitDate == result[j].ExitDate {
			return result[i].ID < result[j].ID
		}
		return result[i].ExitDate < result[j].ExitDate
	})
	return result
}

func sameSettledOrder(left, right ShadowOrder) bool {
	return left.ID == right.ID && left.Symbol == right.Symbol && left.Side == right.Side &&
		left.SignalDate == right.SignalDate && left.AttemptDate == right.AttemptDate &&
		left.Quantity == right.Quantity && sameFloat(left.RawPrice, right.RawPrice) &&
		sameFloat(left.Price, right.Price) && sameFloat(left.Amount, right.Amount) && left.Status == right.Status
}

func sameSettledTrade(left, right ShadowTrade) bool {
	return left.ID == right.ID && left.Symbol == right.Symbol && left.SignalDate == right.SignalDate &&
		left.EntryDate == right.EntryDate && left.ExitDate == right.ExitDate && left.Quantity == right.Quantity &&
		sameFloat(left.EntryPrice, right.EntryPrice) && sameFloat(left.ExitPrice, right.ExitPrice) &&
		sameFloat(left.NetProfit, right.NetProfit) && sameFloat(left.TotalFee, right.TotalFee)
}

func matchingPosition(positions []ShadowOpenPosition, target ShadowOpenPosition) (ShadowOpenPosition, bool) {
	for _, position := range positions {
		if position.Symbol == target.Symbol && position.EntryDate == target.EntryDate {
			return position, true
		}
	}
	return ShadowOpenPosition{}, false
}

func samePositionBasis(left, right ShadowOpenPosition) bool {
	return left.Symbol == right.Symbol && left.SignalDate == right.SignalDate && left.EntryDate == right.EntryDate &&
		left.Quantity == right.Quantity && sameFloat(left.EntryPrice, right.EntryPrice)
}

func positionClosedAfter(trades []ShadowTrade, position ShadowOpenPosition, date string) bool {
	for _, trade := range trades {
		if trade.Symbol == position.Symbol && trade.EntryDate == position.EntryDate && trade.Quantity == position.Quantity && trade.ExitDate > date {
			return true
		}
	}
	return false
}

func sameFloat(left, right float64) bool {
	return math.Abs(left-right) <= 1e-8*math.Max(1, math.Max(math.Abs(left), math.Abs(right)))
}

type Evaluator struct {
	history HistoryClient
	now     func() time.Time
}

func NewEvaluator(history HistoryClient) *Evaluator {
	return &Evaluator{history: history, now: time.Now}
}

type shadowPlan struct {
	signal      realtime.Signal
	bars        []domain.DailyBar
	signalClose float64
	capacity    float64
	entryIndex  int
	exitIndex   int
	entryDate   string
	exitDate    string
}

type shadowPosition struct {
	plan      shadowPlan
	entry     ShadowOrder
	entryCost float64
}

func (e *Evaluator) Evaluate(ctx context.Context, signals []realtime.Signal, options Options) (Report, error) {
	if e == nil || e.history == nil {
		return Report{}, fmt.Errorf("影子执行评估器未初始化")
	}
	cfg := canonicalConfig(options.Config)
	now := e.now
	if options.Now != nil {
		now = options.Now
	}
	if now == nil {
		now = time.Now
	}
	limit := options.Limit
	if limit < 0 {
		limit = 500
	}
	selected := representativeSignals(signals, limit)
	report := Report{EngineVersion: ShadowEngineVersion, GeneratedAt: now(), AsOf: now().Format("2006-01-02"), Config: cfg, SignalCount: len(signals), InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash}
	if len(selected) == 0 {
		return report, nil
	}
	calendarBars, calendarError := e.history.FetchDailyBars(ctx, "sh000300")
	calendarDates := barDates(normalizedBars(calendarBars))
	if calendarError != nil || len(calendarDates) == 0 {
		report.Warnings = append(report.Warnings, "沪深300交易日历不可用，停牌识别将退化为个股交易日")
	}

	bySymbol := make(map[string][]realtime.Signal)
	for _, signal := range selected {
		bySymbol[signal.Symbol] = append(bySymbol[signal.Symbol], signal)
	}
	plans := make([]shadowPlan, 0, len(selected))
	for symbol, items := range bySymbol {
		bars, err := e.history.FetchDailyBars(ctx, symbol)
		if err != nil {
			report.Warnings = append(report.Warnings, symbol+" 日K不可用: "+err.Error())
			continue
		}
		bars = normalizedBars(bars)
		if len(bars) == 0 {
			report.Warnings = append(report.Warnings, symbol+" 没有有效日K")
			continue
		}
		for _, signal := range items {
			if signal.State != realtime.StateTriggered && signal.State != realtime.StateWatching {
				continue
			}
			if signal.Score < cfg.MinimumScore {
				continue
			}
			report.CandidateCount++
			plan, reason, pending := makePlan(signal, bars, calendarDates, cfg.HoldingDays)
			if reason != "" {
				if pending {
					report.PendingCandidates++
					report.Warnings = append(report.Warnings, signal.Symbol+" "+signalDate(signal)+" 影子窗口待成熟: "+reason)
				} else {
					report.RejectedOrders++
					report.Rejections = append(report.Rejections, ShadowRejection{OrderID: orderID(signal, "buy"), Symbol: signal.Symbol, Name: signal.Name, Side: "buy", SignalDate: signalDate(signal), Reason: reason})
				}
				continue
			}
			plans = append(plans, plan)
		}
	}
	sort.SliceStable(plans, func(i, j int) bool {
		if plans[i].entryDate == plans[j].entryDate {
			return plans[i].signal.Score > plans[j].signal.Score
		}
		return plans[i].entryDate < plans[j].entryDate
	})
	return simulate(report, plans, cfg), nil
}

func canonicalConfig(cfg Config) Config {
	defaults := DefaultConfig()
	if cfg.InitialCash <= 0 || !finite(cfg.InitialCash) {
		cfg.InitialCash = defaults.InitialCash
	}
	if cfg.MinimumScore <= 0 || !finite(cfg.MinimumScore) {
		cfg.MinimumScore = defaults.MinimumScore
	}
	if cfg.MaxPositionPercent <= 0 || cfg.MaxPositionPercent > 100 {
		cfg.MaxPositionPercent = defaults.MaxPositionPercent
	}
	if cfg.MaxParticipationPercent <= 0 || cfg.MaxParticipationPercent > 100 {
		cfg.MaxParticipationPercent = defaults.MaxParticipationPercent
	}
	if cfg.SlippageBPS <= 0 || !finite(cfg.SlippageBPS) {
		cfg.SlippageBPS = defaults.SlippageBPS
	}
	if cfg.CommissionRate <= 0 || !finite(cfg.CommissionRate) {
		cfg.CommissionRate = defaults.CommissionRate
	}
	if cfg.MinimumCommission <= 0 || !finite(cfg.MinimumCommission) {
		cfg.MinimumCommission = defaults.MinimumCommission
	}
	if cfg.StampDutyRate <= 0 || !finite(cfg.StampDutyRate) {
		cfg.StampDutyRate = defaults.StampDutyRate
	}
	if cfg.TransferFeeRate <= 0 || !finite(cfg.TransferFeeRate) {
		cfg.TransferFeeRate = defaults.TransferFeeRate
	}
	if cfg.HoldingDays <= 0 {
		cfg.HoldingDays = defaults.HoldingDays
	}
	if cfg.LotSize <= 0 {
		cfg.LotSize = defaults.LotSize
	}
	return cfg
}

func representativeSignals(signals []realtime.Signal, limit int) []realtime.Signal {
	latest := make(map[string]realtime.Signal)
	for _, signal := range signals {
		if strings.TrimSpace(signal.Symbol) == "" {
			continue
		}
		key := signal.Symbol + ":" + signalDate(signal)
		previous, found := latest[key]
		if !found || signal.Score > previous.Score || (signal.Score == previous.Score && signal.AsOf.After(previous.AsOf)) {
			latest[key] = signal
		}
	}
	result := make([]realtime.Signal, 0, len(latest))
	for _, signal := range latest {
		result = append(result, signal)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].AsOf.Equal(result[j].AsOf) {
			return result[i].Score > result[j].Score
		}
		return result[i].AsOf.Before(result[j].AsOf)
	})
	if limit > 0 && len(result) > limit {
		return result[:limit]
	}
	return result
}

func makePlan(signal realtime.Signal, bars []domain.DailyBar, calendarDates []string, holdingDays int) (shadowPlan, string, bool) {
	sDate := signalDate(signal)
	signalClose := signal.Price
	signalIndex := barIndex(bars, sDate)
	if signalClose <= 0 {
		if signalIndex >= 0 && bars[signalIndex].Close > 0 {
			signalClose = bars[signalIndex].Close
		}
	}
	if signalClose <= 0 {
		for index := len(bars) - 1; index >= 0; index-- {
			if bars[index].Date < sDate && bars[index].Close > 0 {
				signalClose = bars[index].Close
				break
			}
		}
	}
	if signalClose <= 0 {
		return shadowPlan{}, "信号时点没有可用价格", false
	}
	capacityIndex := -1
	for index := len(bars) - 1; index >= 0; index-- {
		if bars[index].Date < sDate {
			capacityIndex = index
			break
		}
	}
	capacity := trailingAverageAmount(bars, capacityIndex, 20)
	if capacity <= 0 {
		return shadowPlan{}, "信号时点缺少历史成交额，容量校验待补数据", true
	}
	entryDate, exitDate := "", ""
	if len(calendarDates) > 0 {
		calendarIndex := sort.SearchStrings(calendarDates, sDate)
		for calendarIndex < len(calendarDates) && calendarDates[calendarIndex] <= sDate {
			calendarIndex++
		}
		if calendarIndex >= len(calendarDates) {
			return shadowPlan{}, "没有信号后的市场交易日", true
		}
		entryDate = calendarDates[calendarIndex]
		if calendarIndex+holdingDays-1 < len(calendarDates) {
			exitDate = calendarDates[calendarIndex+holdingDays-1]
		}
	} else {
		signalIndex := -1
		for index := range bars {
			if bars[index].Date <= sDate {
				signalIndex = index
			}
		}
		entryIndex := signalIndex + 1
		if entryIndex >= len(bars) {
			return shadowPlan{}, "没有信号后的实际交易日", true
		}
		entryDate = bars[entryIndex].Date
		if entryIndex+holdingDays-1 < len(bars) {
			exitDate = bars[entryIndex+holdingDays-1].Date
		}
	}
	entryIndex, exitIndex := barIndex(bars, entryDate), -1
	if entryIndex < 0 {
		return shadowPlan{}, "次日无日K，疑似停牌或数据缺失", false
	}
	if exitDate != "" {
		exitIndex = barIndex(bars, exitDate)
	}
	return shadowPlan{signal: signal, bars: bars, signalClose: signalClose, capacity: capacity, entryIndex: entryIndex, exitIndex: exitIndex, entryDate: entryDate, exitDate: exitDate}, "", false
}

func simulate(report Report, plans []shadowPlan, cfg Config) Report {
	cash := cfg.InitialCash
	positions := make(map[string]shadowPosition)
	events := make(map[string][]int)
	for index, plan := range plans {
		events[plan.entryDate] = append(events[plan.entryDate], index)
		if plan.exitDate != "" {
			events[plan.exitDate] = append(events[plan.exitDate], index)
		}
	}
	dates := make([]string, 0, len(events))
	for date := range events {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	for _, date := range dates {
		indexes := events[date]
		// Exits first release cash before same-day entries.
		for _, index := range indexes {
			plan := plans[index]
			if plan.exitDate != date {
				continue
			}
			position, ok := positions[plan.signal.Symbol]
			if !ok || position.plan.signal.ID != plan.signal.ID {
				continue
			}
			bar := plan.bars[plan.exitIndex]
			if onePriceBar(bar) || bar.Close <= 0 {
				report.RejectedOrders++
				report.Rejections = append(report.Rejections, ShadowRejection{OrderID: orderID(plan.signal, "sell"), Symbol: plan.signal.Symbol, Name: plan.signal.Name, Side: "sell", SignalDate: signalDate(plan.signal), AttemptDate: date, Reason: "卖出日一字板或停牌，无法执行"})
				continue
			}
			sell := makeOrder(plan.signal, "sell", date, bar.Close, position.entry.Quantity, cfg, 0)
			sell.Status = OrderFilled
			report.Orders = append(report.Orders, sell)
			fee := transactionFee(sell.Amount, "sell", cfg)
			cash += sell.Amount - fee
			trade := ShadowTrade{ID: fmt.Sprintf("ST%04d", len(report.Trades)+1), Symbol: plan.signal.Symbol, Name: plan.signal.Name, SignalDate: signalDate(plan.signal), EntryDate: position.entry.AttemptDate, ExitDate: date, Quantity: position.entry.Quantity, SignalClose: plan.signalClose, ExitClose: bar.Close, EntryPrice: position.entry.Price, ExitPrice: sell.Price, HoldingDays: plan.exitIndex - plan.entryIndex + 1, TotalFee: position.entryCost - position.entry.Amount + fee}
			trade.GrossProfit = (sell.Price - position.entry.Price) * float64(sell.Quantity)
			trade.NetProfit = sell.Amount - fee - position.entryCost
			trade.TheoreticalReturnPercent = (bar.Close/plan.signalClose - 1) * 100
			trade.ExecutableReturnPercent = trade.NetProfit / position.entryCost * 100
			trade.ExecutionGapPercent = trade.ExecutableReturnPercent - trade.TheoreticalReturnPercent
			report.Trades = append(report.Trades, trade)
			report.CompletedTrades++
			report.TotalTurnover += position.entry.Amount + sell.Amount
			report.TotalFees += fee
			delete(positions, plan.signal.Symbol)
		}
		for _, index := range indexes {
			plan := plans[index]
			if plan.entryDate != date {
				continue
			}
			if _, exists := positions[plan.signal.Symbol]; exists {
				report.RejectedOrders++
				report.Rejections = append(report.Rejections, ShadowRejection{OrderID: orderID(plan.signal, "buy"), Symbol: plan.signal.Symbol, Name: plan.signal.Name, Side: "buy", SignalDate: signalDate(plan.signal), AttemptDate: date, Reason: "已有同股票影子持仓，拒绝重叠开仓"})
				continue
			}
			bar := plan.bars[plan.entryIndex]
			if onePriceBar(bar) || bar.Open <= 0 {
				report.RejectedOrders++
				report.Rejections = append(report.Rejections, ShadowRejection{OrderID: orderID(plan.signal, "buy"), Symbol: plan.signal.Symbol, Name: plan.signal.Name, Side: "buy", SignalDate: signalDate(plan.signal), AttemptDate: date, Reason: "次日开盘一字板或停牌，无法执行"})
				continue
			}
			capacity := plan.capacity * cfg.MaxParticipationPercent / 100
			budget := math.Min(cash, cfg.InitialCash*cfg.MaxPositionPercent/100)
			price := bar.Open * (1 + cfg.SlippageBPS/10000)
			quantity := int(budget/price/float64(cfg.LotSize)) * cfg.LotSize
			if capacity > 0 {
				quantity = minInt(quantity, int(capacity/price/float64(cfg.LotSize))*cfg.LotSize)
			}
			if quantity < cfg.LotSize {
				reason := "资金不足或不足一手"
				if capacity > 0 && int(capacity/price/float64(cfg.LotSize))*cfg.LotSize < cfg.LotSize {
					reason = "成交额容量不足一手"
				}
				report.RejectedOrders++
				report.Rejections = append(report.Rejections, ShadowRejection{OrderID: orderID(plan.signal, "buy"), Symbol: plan.signal.Symbol, Name: plan.signal.Name, Side: "buy", SignalDate: signalDate(plan.signal), AttemptDate: date, Reason: reason})
				continue
			}
			buy := makeOrder(plan.signal, "buy", date, bar.Open, quantity, cfg, capacity)
			fee := transactionFee(buy.Amount, "buy", cfg)
			if buy.Amount+fee > cash {
				report.RejectedOrders++
				report.Rejections = append(report.Rejections, ShadowRejection{OrderID: buy.ID, Symbol: buy.Symbol, Name: buy.Name, Side: "buy", SignalDate: buy.SignalDate, AttemptDate: date, Reason: "资金不足"})
				continue
			}
			buy.Status = OrderFilled
			report.Orders = append(report.Orders, buy)
			cash -= buy.Amount + fee
			report.FilledEntries++
			positions[plan.signal.Symbol] = shadowPosition{plan: plan, entry: buy, entryCost: buy.Amount + fee}
			report.TotalFees += fee
		}
	}
	report.RemainingCash = cash
	report.OpenPositions = len(positions)
	for _, position := range positions {
		lastBar := latestBar(position.plan.bars, report.AsOf)
		if lastBar.Close <= 0 {
			continue
		}
		marketValue := lastBar.Close * float64(position.entry.Quantity)
		exitFee := transactionFee(marketValue, "sell", cfg)
		profit := marketValue - exitFee - position.entryCost
		report.Positions = append(report.Positions, ShadowOpenPosition{
			Symbol: position.plan.signal.Symbol, Name: position.plan.signal.Name, SignalDate: signalDate(position.plan.signal),
			EntryDate: position.entry.AttemptDate, Quantity: position.entry.Quantity, EntryPrice: position.entry.Price,
			LastDate: lastBar.Date, LastPrice: lastBar.Close, MarketValue: marketValue, UnrealizedProfit: profit,
			UnrealizedReturnPercent: profit / position.entryCost * 100, TargetExitDate: position.plan.exitDate,
		})
	}
	sort.SliceStable(report.Positions, func(i, j int) bool { return report.Positions[i].EntryDate > report.Positions[j].EntryDate })
	if len(report.Trades) > 0 {
		for _, trade := range report.Trades {
			report.TheoreticalAverageReturn += trade.TheoreticalReturnPercent
			report.ExecutableAverageReturn += trade.ExecutableReturnPercent
		}
		report.TheoreticalAverageReturn /= float64(len(report.Trades))
		report.ExecutableAverageReturn /= float64(len(report.Trades))
		report.ExecutionGapPercent = report.ExecutableAverageReturn - report.TheoreticalAverageReturn
	}
	return report
}

func makeOrder(signal realtime.Signal, side, date string, rawPrice float64, quantity int, cfg Config, capacity float64) ShadowOrder {
	direction := 1.0
	if side == "sell" {
		direction = -1
	}
	price := rawPrice * (1 + direction*cfg.SlippageBPS/10000)
	return ShadowOrder{ID: orderID(signal, side), Symbol: signal.Symbol, Name: signal.Name, Side: side, SignalDate: signalDate(signal), AttemptDate: date, Quantity: quantity, RawPrice: rawPrice, Price: price, Amount: price * float64(quantity), CapacityAmount: capacity, Status: "pending"}
}

func transactionFee(amount float64, side string, cfg Config) float64 {
	commission := math.Max(cfg.MinimumCommission, amount*cfg.CommissionRate)
	stamp := 0.0
	if side == "sell" {
		stamp = amount * cfg.StampDutyRate
	}
	return commission + stamp + amount*cfg.TransferFeeRate
}
func orderID(signal realtime.Signal, side string) string { return signal.ID + "-" + side }
func signalDate(signal realtime.Signal) string {
	if !signal.AsOf.IsZero() {
		return signal.AsOf.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
	}
	return signal.DataDate
}
func normalizedBars(bars []domain.DailyBar) []domain.DailyBar {
	result := append([]domain.DailyBar(nil), bars...)
	sort.SliceStable(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result
}
func barDates(bars []domain.DailyBar) []string {
	result := make([]string, 0, len(bars))
	for _, bar := range bars {
		if bar.Date != "" {
			result = append(result, bar.Date)
		}
	}
	return result
}
func barIndex(bars []domain.DailyBar, date string) int {
	index := sort.Search(len(bars), func(index int) bool { return bars[index].Date >= date })
	if index < len(bars) && bars[index].Date == date {
		return index
	}
	return -1
}
func trailingAverageAmount(bars []domain.DailyBar, endIndex, length int) float64 {
	if endIndex < 0 || endIndex >= len(bars) || length <= 0 {
		return 0
	}
	start := endIndex - length + 1
	if start < 0 {
		start = 0
	}
	total := 0.0
	count := 0
	for index := start; index <= endIndex; index++ {
		if bars[index].Amount > 0 && finite(bars[index].Amount) {
			total += bars[index].Amount
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}
func latestBar(bars []domain.DailyBar, asOf string) domain.DailyBar {
	for index := len(bars) - 1; index >= 0; index-- {
		if (asOf == "" || bars[index].Date <= asOf) && bars[index].Close > 0 {
			return bars[index]
		}
	}
	return domain.DailyBar{}
}
func onePriceBar(bar domain.DailyBar) bool { return bar.High > 0 && math.Abs(bar.High-bar.Low) < 1e-9 }
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
