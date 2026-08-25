package paper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	InitialCash               float64 `json:"initial_cash"`
	MinimumScore              float64 `json:"minimum_score"`
	MaxPositionPercent        float64 `json:"max_position_percent"`
	MaxPortfolioPercent       float64 `json:"max_portfolio_percent"`
	CashReservePercent        float64 `json:"cash_reserve_percent"`
	MaxDailyDeploymentPercent float64 `json:"max_daily_deployment_percent"`
	InitialEntryPercent       float64 `json:"initial_entry_percent"`
	MaxEntryTranches          int     `json:"max_entry_tranches"`
	AdditionScoreStep         float64 `json:"addition_score_step"`
	MaxParticipationPercent   float64 `json:"max_participation_percent"`
	SlippageBPS               float64 `json:"slippage_bps"`
	CommissionRate            float64 `json:"commission_rate"`
	MinimumCommission         float64 `json:"minimum_commission"`
	StampDutyRate             float64 `json:"stamp_duty_rate"`
	TransferFeeRate           float64 `json:"transfer_fee_rate"`
	HoldingDays               int     `json:"holding_days"`
	LotSize                   int     `json:"lot_size"`
}

func DefaultConfig() Config {
	return Config{
		InitialCash: 1_000_000, MinimumScore: 55, MaxPositionPercent: 20,
		MaxPortfolioPercent: 80, CashReservePercent: 20, MaxDailyDeploymentPercent: 35,
		InitialEntryPercent: 50, MaxEntryTranches: 3, AdditionScoreStep: 4,
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
	ID                string   `json:"id"`
	Symbol            string   `json:"symbol"`
	Name              string   `json:"name,omitempty"`
	Side              string   `json:"side"`
	SignalDate        string   `json:"signal_date"`
	AttemptDate       string   `json:"attempt_date"`
	Quantity          int      `json:"quantity"`
	RawPrice          float64  `json:"raw_price"`
	Price             float64  `json:"price"`
	Amount            float64  `json:"amount"`
	CapacityAmount    float64  `json:"capacity_amount,omitempty"`
	Status            string   `json:"status"`
	Reason            string   `json:"reason,omitempty"`
	ExecutionTime     string   `json:"execution_time,omitempty"`
	SignalScore       float64  `json:"signal_score,omitempty"`
	TriggerPrice      float64  `json:"trigger_price,omitempty"`
	InvalidationPrice float64  `json:"invalidation_price,omitempty"`
	SignalReasons     []string `json:"signal_reasons,omitempty"`
	PositionAction    string   `json:"position_action,omitempty"`
	PositionSequence  int      `json:"position_sequence,omitempty"`
}

const (
	OrderFilled   = "filled"
	OrderRejected = "rejected"
)

type ShadowRejection struct {
	OrderID       string   `json:"order_id"`
	Symbol        string   `json:"symbol"`
	Name          string   `json:"name,omitempty"`
	Side          string   `json:"side"`
	SignalDate    string   `json:"signal_date"`
	AttemptDate   string   `json:"attempt_date,omitempty"`
	Reason        string   `json:"reason"`
	SignalScore   float64  `json:"signal_score,omitempty"`
	SignalReasons []string `json:"signal_reasons,omitempty"`
}

type ShadowTrade struct {
	ID                       string  `json:"id"`
	EntryOrderID             string  `json:"entry_order_id,omitempty"`
	ExitOrderID              string  `json:"exit_order_id,omitempty"`
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
	PositionAction           string  `json:"position_action,omitempty"`
	ExitTime                 string  `json:"exit_time,omitempty"`
}

type ShadowOpenPosition struct {
	SignalID                string              `json:"signal_id,omitempty"`
	Symbol                  string              `json:"symbol"`
	Name                    string              `json:"name,omitempty"`
	SignalDate              string              `json:"signal_date"`
	EntryDate               string              `json:"entry_date"`
	Quantity                int                 `json:"quantity"`
	EntryPrice              float64             `json:"entry_price"`
	EntryAmount             float64             `json:"entry_amount,omitempty"`
	EntryFee                float64             `json:"entry_fee,omitempty"`
	SignalClose             float64             `json:"signal_close,omitempty"`
	AvailableQuantity       int                 `json:"available_quantity"`
	LastDate                string              `json:"last_date"`
	LastPrice               float64             `json:"last_price"`
	MarketValue             float64             `json:"market_value"`
	UnrealizedProfit        float64             `json:"unrealized_profit"`
	UnrealizedReturnPercent float64             `json:"unrealized_return_percent"`
	TargetExitDate          string              `json:"target_exit_date,omitempty"`
	EntryTime               string              `json:"entry_time,omitempty"`
	SignalScore             float64             `json:"signal_score,omitempty"`
	TriggerPrice            float64             `json:"trigger_price,omitempty"`
	InvalidationPrice       float64             `json:"invalidation_price,omitempty"`
	SignalReasons           []string            `json:"signal_reasons,omitempty"`
	ValuationTime           string              `json:"valuation_time,omitempty"`
	ValuationSource         string              `json:"valuation_source,omitempty"`
	RealtimeValuation       bool                `json:"realtime_valuation,omitempty"`
	Lots                    []ShadowPositionLot `json:"lots,omitempty"`
	AdditionCount           int                 `json:"addition_count,omitempty"`
	TargetPositionPercent   float64             `json:"target_position_percent,omitempty"`
}

type ShadowPositionLot struct {
	OrderID           string   `json:"order_id"`
	EntryDate         string   `json:"entry_date"`
	EntryTime         string   `json:"entry_time,omitempty"`
	Quantity          int      `json:"quantity"`
	EntryPrice        float64  `json:"entry_price"`
	EntryAmount       float64  `json:"entry_amount"`
	EntryFee          float64  `json:"entry_fee"`
	SignalID          string   `json:"signal_id,omitempty"`
	SignalDate        string   `json:"signal_date,omitempty"`
	SignalClose       float64  `json:"signal_close,omitempty"`
	SignalScore       float64  `json:"signal_score,omitempty"`
	TriggerPrice      float64  `json:"trigger_price,omitempty"`
	InvalidationPrice float64  `json:"invalidation_price,omitempty"`
	SignalReasons     []string `json:"signal_reasons,omitempty"`
	TargetExitDate    string   `json:"target_exit_date,omitempty"`
}

type PositionQuote struct {
	Symbol    string
	Price     float64
	QuoteTime string
	Source    string
}

type Report struct {
	EngineVersion            string               `json:"engine_version,omitempty"`
	ConfigFingerprint        string               `json:"config_fingerprint,omitempty"`
	SignalLimit              int                  `json:"signal_limit,omitempty"`
	CheckpointPhase          string               `json:"checkpoint_phase,omitempty"`
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
	TotalMarketValue         float64              `json:"total_market_value"`
	TotalEquity              float64              `json:"total_equity"`
	RealizedProfit           float64              `json:"realized_profit"`
	UnrealizedProfit         float64              `json:"unrealized_profit"`
	TotalProfit              float64              `json:"total_profit"`
	TotalReturnPercent       float64              `json:"total_return_percent"`
	TheoreticalAverageReturn float64              `json:"theoretical_average_return_percent"`
	ExecutableAverageReturn  float64              `json:"executable_average_return_percent"`
	ExecutionGapPercent      float64              `json:"execution_gap_percent"`
	Trades                   []ShadowTrade        `json:"trades,omitempty"`
	Positions                []ShadowOpenPosition `json:"positions,omitempty"`
	Orders                   []ShadowOrder        `json:"orders,omitempty"`
	Rejections               []ShadowRejection    `json:"rejections,omitempty"`
	Warnings                 []string             `json:"warnings,omitempty"`
	Decisions                []ShadowDecision     `json:"decisions,omitempty"`
}

type ShadowDecision struct {
	ID                    string   `json:"id"`
	Symbol                string   `json:"symbol"`
	Name                  string   `json:"name,omitempty"`
	Date                  string   `json:"date"`
	Action                string   `json:"action"`
	Reason                string   `json:"reason"`
	SignalScore           float64  `json:"signal_score,omitempty"`
	SignalReasons         []string `json:"signal_reasons,omitempty"`
	CurrentQuantity       int      `json:"current_quantity,omitempty"`
	TargetPositionPercent float64  `json:"target_position_percent,omitempty"`
}

const (
	ShadowEngineVersion = "tplus1-v7"
	CheckpointOpen      = "open"
	CheckpointClose     = "close"
)

var shanghaiLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

// ConfigFingerprint identifies the execution assumptions that produced a
// report. It lets the web layer reject a cached account when the user changes
// minimum score, holding window or any future execution parameter.
func ConfigFingerprint(cfg Config) string {
	return OptionsFingerprint(cfg, 0)
}

func OptionsFingerprint(cfg Config, limit int) string {
	canonical := canonicalConfig(cfg)
	data, _ := json.Marshal(struct {
		Config Config `json:"config"`
		Limit  int    `json:"limit"`
	}{Config: canonical, Limit: limit})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

type Checkpoint struct {
	Date  string `json:"date"`
	Phase string `json:"phase"`
}

// TradingCheckpointAt separates events known at the open from exits that need
// a completed close. The benchmark calendar prevents weekend/holiday advances.
func TradingCheckpointAt(now time.Time, calendarDates []string) Checkpoint {
	local := now.In(shanghaiLocation)
	today := local.Format("2006-01-02")
	todayIndex := sort.SearchStrings(calendarDates, today)
	todayIsTrading := todayIndex < len(calendarDates) && calendarDates[todayIndex] == today
	marketOpen := time.Date(local.Year(), local.Month(), local.Day(), 9, 30, 0, 0, shanghaiLocation)
	closeSettled := time.Date(local.Year(), local.Month(), local.Day(), 15, 5, 0, 0, shanghaiLocation)
	if todayIsTrading && !local.Before(marketOpen) {
		phase := CheckpointOpen
		if !local.Before(closeSettled) {
			phase = CheckpointClose
		}
		return Checkpoint{Date: today, Phase: phase}
	}
	for index := len(calendarDates) - 1; index >= 0; index-- {
		date := calendarDates[index]
		if date != "" && date < today {
			return Checkpoint{Date: date, Phase: CheckpointClose}
		}
	}
	if len(calendarDates) > 0 {
		return Checkpoint{Date: calendarDates[0], Phase: CheckpointClose}
	}
	phase := CheckpointClose
	if !local.Before(marketOpen) && local.Before(closeSettled) {
		phase = CheckpointOpen
	}
	return Checkpoint{Date: today, Phase: phase}
}

func TradingCheckpointDate(now time.Time, calendarDates []string) string {
	return TradingCheckpointAt(now, calendarDates).Date
}

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
		entryAmount := position.EntryAmount
		if entryAmount <= 0 {
			entryAmount = position.EntryPrice * float64(position.Quantity)
		}
		entryCost := entryAmount + position.EntryFee
		if entryCost <= entryAmount {
			entryCost = entryAmount + transactionFee(entryAmount, "buy", report.Config)
		}
		marketValue := quote.Price * float64(position.Quantity)
		profit := marketValue - entryCost
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
	recomputeAccountMetrics(&report)
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
	previousOrders := ordersThroughCheckpoint(previous.Orders, previous.AsOf, previous.CheckpointPhase)
	nextOrders := ordersThroughCheckpoint(next.Orders, previous.AsOf, previous.CheckpointPhase)
	if len(previousOrders) != len(nextOrders) {
		return fmt.Errorf("截至 %s 的成交单数量从 %d 变为 %d", previous.AsOf, len(previousOrders), len(nextOrders))
	}
	for index := range previousOrders {
		if !sameSettledOrder(previousOrders[index], nextOrders[index]) {
			return fmt.Errorf("截至 %s 的成交单被改写: %s", previous.AsOf, previousOrders[index].ID)
		}
	}
	previousTrades := tradesThroughCheckpoint(previous.Trades, previous.AsOf, previous.CheckpointPhase)
	nextTrades := tradesThroughCheckpoint(next.Trades, previous.AsOf, previous.CheckpointPhase)
	if len(previousTrades) != len(nextTrades) {
		return fmt.Errorf("截至 %s 的完成交易数量从 %d 变为 %d", previous.AsOf, len(previousTrades), len(nextTrades))
	}
	for index := range previousTrades {
		if !sameSettledTrade(previousTrades[index], nextTrades[index]) {
			return fmt.Errorf("截至 %s 的完成交易被改写: %s", previous.AsOf, previousTrades[index].ID)
		}
	}
	previousRejections := rejectionsThroughCheckpoint(previous.Rejections, previous.AsOf, previous.CheckpointPhase)
	nextRejections := rejectionsThroughCheckpoint(next.Rejections, previous.AsOf, previous.CheckpointPhase)
	if len(previousRejections) != len(nextRejections) {
		return fmt.Errorf("截至 %s 的拒绝记录数量从 %d 变为 %d", previous.AsOf, len(previousRejections), len(nextRejections))
	}
	for index := range previousRejections {
		if !sameRejection(previousRejections[index], nextRejections[index]) {
			return fmt.Errorf("截至 %s 的拒绝记录被改写: %s", previous.AsOf, previousRejections[index].OrderID)
		}
	}
	if previous.EngineVersion == ShadowEngineVersion {
		metrics := ledgerMetricsThroughCheckpoint(next, previous.AsOf, previous.CheckpointPhase)
		if !sameFloat(previous.RemainingCash, metrics.remainingCash) ||
			!sameFloat(previous.TotalTurnover, metrics.totalTurnover) ||
			!sameFloat(previous.TotalFees, metrics.totalFees) ||
			previous.FilledEntries != metrics.filledEntries ||
			previous.CompletedTrades != metrics.completedTrades ||
			previous.RejectedOrders != metrics.rejectedOrders {
			return fmt.Errorf("截至 %s 的账户现金、费用或成交统计被改写", previous.AsOf)
		}
	}
	for _, position := range previous.Positions {
		if nextPosition, found := matchingPosition(next.Positions, position); found {
			if position.Quantity == nextPosition.Quantity {
				if !samePositionBasis(position, nextPosition) {
					return fmt.Errorf("持仓成本或数量被改写: %s", position.Symbol)
				}
				continue
			}
			if !positionQuantityTransitionValid(previous, next, position, nextPosition.Quantity) || !positionLotsTransitionValid(previous, next, position, &nextPosition) {
				return fmt.Errorf("持仓成本或数量被改写: %s", position.Symbol)
			}
			continue
		}
		if !positionQuantityTransitionValid(previous, next, position, 0) || !positionLotsTransitionValid(previous, next, position, nil) {
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

func ordersThroughCheckpoint(orders []ShadowOrder, date, phase string) []ShadowOrder {
	result := make([]ShadowOrder, 0, len(orders))
	for _, order := range orders {
		if order.AttemptDate == "" || order.AttemptDate > date {
			continue
		}
		if order.AttemptDate == date && phase == CheckpointOpen && !orderSettledAtOpen(order) {
			continue
		}
		result = append(result, order)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].AttemptDate == result[j].AttemptDate {
			return result[i].ID < result[j].ID
		}
		return result[i].AttemptDate < result[j].AttemptDate
	})
	return result
}

func orderSettledAtOpen(order ShadowOrder) bool {
	if order.PositionAction == "exit" {
		return false
	}
	if order.Side == "buy" || order.PositionAction == "reduce" {
		return true
	}
	if len(order.ExecutionTime) >= 16 {
		return order.ExecutionTime[11:16] < "15:00"
	}
	return false
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

func tradesThroughCheckpoint(trades []ShadowTrade, date, phase string) []ShadowTrade {
	result := make([]ShadowTrade, 0, len(trades))
	for _, trade := range trades {
		if trade.ExitDate == "" || trade.ExitDate > date {
			continue
		}
		if trade.ExitDate == date && phase == CheckpointOpen && !tradeSettledAtOpen(trade) {
			continue
		}
		result = append(result, trade)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].ExitDate == result[j].ExitDate {
			return result[i].ID < result[j].ID
		}
		return result[i].ExitDate < result[j].ExitDate
	})
	return result
}

func tradeSettledAtOpen(trade ShadowTrade) bool {
	if trade.PositionAction == "reduce" {
		return true
	}
	if len(trade.ExitTime) >= 16 {
		return trade.ExitTime[11:16] < "15:00"
	}
	return false
}

func rejectionsThrough(rejections []ShadowRejection, date string) []ShadowRejection {
	result := make([]ShadowRejection, 0, len(rejections))
	for _, rejection := range rejections {
		eventDate := rejection.AttemptDate
		if eventDate == "" {
			eventDate = rejection.SignalDate
		}
		if eventDate != "" && eventDate <= date {
			result = append(result, rejection)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		leftDate, rightDate := result[i].AttemptDate, result[j].AttemptDate
		if leftDate == "" {
			leftDate = result[i].SignalDate
		}
		if rightDate == "" {
			rightDate = result[j].SignalDate
		}
		if leftDate == rightDate {
			return result[i].OrderID < result[j].OrderID
		}
		return leftDate < rightDate
	})
	return result
}

func rejectionsThroughCheckpoint(rejections []ShadowRejection, date, phase string) []ShadowRejection {
	result := make([]ShadowRejection, 0, len(rejections))
	for _, rejection := range rejections {
		eventDate := rejection.AttemptDate
		if eventDate == "" {
			eventDate = rejection.SignalDate
		}
		if eventDate == "" || eventDate > date {
			continue
		}
		if eventDate == date && phase == CheckpointOpen && rejection.Side != "buy" {
			continue
		}
		result = append(result, rejection)
	}
	sort.SliceStable(result, func(i, j int) bool {
		leftDate, rightDate := result[i].AttemptDate, result[j].AttemptDate
		if leftDate == "" {
			leftDate = result[i].SignalDate
		}
		if rightDate == "" {
			rightDate = result[j].SignalDate
		}
		if leftDate == rightDate {
			return result[i].OrderID < result[j].OrderID
		}
		return leftDate < rightDate
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
	return left.ID == right.ID && left.EntryOrderID == right.EntryOrderID && left.ExitOrderID == right.ExitOrderID &&
		left.Symbol == right.Symbol && left.SignalDate == right.SignalDate &&
		left.EntryDate == right.EntryDate && left.ExitDate == right.ExitDate && left.Quantity == right.Quantity &&
		sameFloat(left.EntryPrice, right.EntryPrice) && sameFloat(left.ExitPrice, right.ExitPrice) &&
		sameFloat(left.NetProfit, right.NetProfit) && sameFloat(left.TotalFee, right.TotalFee) &&
		left.PositionAction == right.PositionAction && left.ExitTime == right.ExitTime
}

func sameRejection(left, right ShadowRejection) bool {
	return left.OrderID == right.OrderID && left.Symbol == right.Symbol && left.Side == right.Side &&
		left.SignalDate == right.SignalDate && left.AttemptDate == right.AttemptDate && left.Reason == right.Reason
}

func makeRejection(signal realtime.Signal, side, attemptDate, reason string) ShadowRejection {
	return ShadowRejection{OrderID: orderID(signal, side), Symbol: signal.Symbol, Name: signal.Name, Side: side, SignalDate: signalDate(signal), AttemptDate: attemptDate, Reason: reason, SignalScore: signal.Score, SignalReasons: append([]string(nil), signal.Reasons...)}
}

type ledgerMetrics struct {
	remainingCash   float64
	totalTurnover   float64
	totalFees       float64
	filledEntries   int
	completedTrades int
	rejectedOrders  int
}

func ledgerMetricsThrough(report Report, date string) ledgerMetrics {
	return ledgerMetricsForEvents(report, ordersThrough(report.Orders, date), tradesThrough(report.Trades, date), rejectionsThrough(report.Rejections, date))
}

func ledgerMetricsThroughCheckpoint(report Report, date, phase string) ledgerMetrics {
	return ledgerMetricsForEvents(
		report,
		ordersThroughCheckpoint(report.Orders, date, phase),
		tradesThroughCheckpoint(report.Trades, date, phase),
		rejectionsThroughCheckpoint(report.Rejections, date, phase),
	)
}

func ledgerMetricsForEvents(report Report, orders []ShadowOrder, trades []ShadowTrade, rejections []ShadowRejection) ledgerMetrics {
	cfg := canonicalConfig(report.Config)
	cash := report.InitialCash
	if cash <= 0 || !finite(cash) {
		cash = cfg.InitialCash
	}
	metrics := ledgerMetrics{remainingCash: cash}
	for _, order := range orders {
		if order.Status != OrderFilled {
			continue
		}
		fee := transactionFee(order.Amount, order.Side, cfg)
		metrics.totalTurnover += order.Amount
		metrics.totalFees += fee
		if order.Side == "sell" {
			cash += order.Amount - fee
		} else {
			cash -= order.Amount + fee
			metrics.filledEntries++
		}
	}
	metrics.remainingCash = cash
	metrics.completedTrades = len(trades)
	metrics.rejectedOrders = len(rejections)
	return metrics
}

func matchingPosition(positions []ShadowOpenPosition, target ShadowOpenPosition) (ShadowOpenPosition, bool) {
	for _, position := range positions {
		if position.Symbol == target.Symbol {
			return position, true
		}
	}
	return ShadowOpenPosition{}, false
}

func samePositionBasis(left, right ShadowOpenPosition) bool {
	if left.Symbol != right.Symbol {
		return false
	}
	if len(left.Lots) > 0 {
		if len(left.Lots) != len(right.Lots) {
			return false
		}
		for _, previousLot := range left.Lots {
			if !containsSamePositionLot(right.Lots, previousLot) {
				return false
			}
		}
		return true
	}
	if left.Quantity == right.Quantity {
		return left.SignalDate == right.SignalDate && left.EntryDate == right.EntryDate && sameFloat(left.EntryPrice, right.EntryPrice)
	}
	for _, nextLot := range right.Lots {
		if nextLot.EntryDate == left.EntryDate && nextLot.Quantity == left.Quantity && sameFloat(nextLot.EntryPrice, left.EntryPrice) {
			return true
		}
	}
	return false
}

func normalizePositionLots(position ShadowOpenPosition, orders []ShadowOrder, cfg Config, calendarDates, symbolDates []string) []shadowLot {
	if len(position.Lots) == 0 {
		entry := ShadowOrder{ID: position.SignalID + "-buy", Symbol: position.Symbol, Name: position.Name, Side: "buy", SignalDate: position.SignalDate, AttemptDate: position.EntryDate, Quantity: position.Quantity, Price: position.EntryPrice, RawPrice: position.EntryPrice, Amount: position.EntryAmount, Status: OrderFilled, ExecutionTime: position.EntryTime, SignalScore: position.SignalScore, TriggerPrice: position.TriggerPrice, InvalidationPrice: position.InvalidationPrice, SignalReasons: append([]string(nil), position.SignalReasons...)}
		if existing, found := matchingFilledOrder(orders, position); found {
			entry = existing
		}
		if entry.ID == "" {
			entry.ID = position.Symbol + "-" + position.EntryDate + "-buy"
		}
		if entry.Amount <= 0 {
			entry.Amount = position.EntryPrice * float64(position.Quantity)
		}
		if entry.ExecutionTime == "" {
			entry.ExecutionTime = simulatedExecutionTime("buy", position.EntryDate)
		}
		entryCost := entry.Amount + position.EntryFee
		if entryCost <= entry.Amount {
			entryCost = entry.Amount + transactionFee(entry.Amount, "buy", cfg)
		}
		target := position.TargetExitDate
		if target == "" {
			target = holdingExitDate(calendarDates, position.EntryDate, cfg.HoldingDays)
		}
		if target == "" {
			target = holdingExitDate(symbolDates, position.EntryDate, cfg.HoldingDays)
		}
		return []shadowLot{{entry: entry, entryCost: entryCost, plan: shadowPlan{targetExitDate: target}}}
	}
	lots := make([]shadowLot, 0, len(position.Lots))
	for _, lot := range position.Lots {
		entry := ShadowOrder{ID: lot.OrderID, Symbol: position.Symbol, Name: position.Name, Side: "buy", SignalDate: lot.SignalDate, AttemptDate: lot.EntryDate, Quantity: lot.Quantity, Price: lot.EntryPrice, RawPrice: lot.EntryPrice, Amount: lot.EntryAmount, Status: OrderFilled, ExecutionTime: lot.EntryTime, SignalScore: lot.SignalScore, TriggerPrice: lot.TriggerPrice, InvalidationPrice: lot.InvalidationPrice, SignalReasons: append([]string(nil), lot.SignalReasons...)}
		if entry.ID == "" {
			entry.ID = lot.SignalID + "-buy"
		}
		entryCost := lot.EntryAmount + lot.EntryFee
		if entryCost <= lot.EntryAmount {
			entryCost = lot.EntryAmount + transactionFee(lot.EntryAmount, "buy", cfg)
		}
		target := lot.TargetExitDate
		if target == "" {
			target = holdingExitDate(calendarDates, lot.EntryDate, cfg.HoldingDays)
		}
		lots = append(lots, shadowLot{entry: entry, entryCost: entryCost, plan: shadowPlan{targetExitDate: target}})
	}
	return lots
}

func positionQuantityTransitionValid(previous, next Report, position ShadowOpenPosition, nextQuantity int) bool {
	expected := position.Quantity
	for _, order := range next.Orders {
		if order.Symbol != position.Symbol || order.Status != OrderFilled || !eventAfterCheckpoint(order.AttemptDate, order.PositionAction, previous) {
			continue
		}
		if order.Side == "sell" {
			expected -= order.Quantity
		} else if order.Side == "buy" {
			expected += order.Quantity
		}
	}
	return expected >= 0 && expected == nextQuantity
}

func positionLotsTransitionValid(previous, next Report, position ShadowOpenPosition, nextPosition *ShadowOpenPosition) bool {
	if len(position.Lots) == 0 {
		return true
	}
	nextLots := []ShadowPositionLot(nil)
	if nextPosition != nil {
		nextLots = nextPosition.Lots
		quantity := 0
		for _, lot := range nextLots {
			quantity += lot.Quantity
		}
		if quantity != nextPosition.Quantity {
			return false
		}
	}
	closedLots := make(map[string]int)
	unassignedSellQuantity := 0
	for _, trade := range next.Trades {
		if trade.Symbol == position.Symbol && trade.EntryOrderID != "" && eventAfterCheckpoint(trade.ExitDate, trade.PositionAction, previous) {
			closedLots[trade.EntryOrderID] += trade.Quantity
		}
	}
	for _, order := range next.Orders {
		if order.Symbol == position.Symbol && order.Side == "sell" && order.Status == OrderFilled && eventAfterCheckpoint(order.AttemptDate, order.PositionAction, previous) {
			matched := false
			for _, trade := range next.Trades {
				if trade.ExitOrderID == order.ID {
					matched = true
					break
				}
			}
			if !matched {
				unassignedSellQuantity += order.Quantity
			}
		}
	}
	for _, previousLot := range position.Lots {
		if containsSamePositionLot(nextLots, previousLot) {
			continue
		}
		closedQuantity := closedLots[previousLot.OrderID]
		if closedQuantity == 0 && unassignedSellQuantity >= previousLot.Quantity {
			closedQuantity = previousLot.Quantity
			unassignedSellQuantity -= previousLot.Quantity
		}
		if closedQuantity != previousLot.Quantity {
			return false
		}
	}
	for _, nextLot := range nextLots {
		if containsSamePositionLot(position.Lots, nextLot) {
			continue
		}
		if !newBuyMatchesLot(previous, next.Orders, position.Symbol, nextLot) {
			return false
		}
	}
	return true
}

func containsSamePositionLot(lots []ShadowPositionLot, target ShadowPositionLot) bool {
	for _, lot := range lots {
		if lot.OrderID == target.OrderID && lot.EntryDate == target.EntryDate && lot.Quantity == target.Quantity && sameFloat(lot.EntryPrice, target.EntryPrice) {
			return true
		}
	}
	return false
}

func newBuyMatchesLot(previous Report, orders []ShadowOrder, symbol string, lot ShadowPositionLot) bool {
	for _, order := range orders {
		if order.ID == lot.OrderID && order.Symbol == symbol && order.Side == "buy" && order.Status == OrderFilled &&
			eventAfterCheckpoint(order.AttemptDate, order.PositionAction, previous) && order.Quantity == lot.Quantity && sameFloat(order.Price, lot.EntryPrice) {
			return true
		}
	}
	return false
}

func eventAfterCheckpoint(date, action string, previous Report) bool {
	if date > previous.AsOf {
		return true
	}
	return date == previous.AsOf && previous.CheckpointPhase == CheckpointOpen && action == "exit"
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
	signal         realtime.Signal
	bars           []domain.DailyBar
	action         string
	lotOrderID     string
	signalClose    float64
	capacity       float64
	entryIndex     int
	exitIndex      int
	entryDate      string
	exitDate       string
	targetExitDate string
}

type shadowPosition struct {
	plan            shadowPlan
	lots            []shadowLot
	fallback        *ShadowOpenPosition
	lastSignalDate  string
	lastSignalScore float64
}

type shadowLot struct {
	plan      shadowPlan
	entry     ShadowOrder
	entryCost float64
}

func (e *Evaluator) Advance(ctx context.Context, previous Report, signals []realtime.Signal, options Options) (Report, error) {
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
	calendarBars, calendarError := e.history.FetchDailyBars(ctx, "sh000300")
	calendarDates := barDates(normalizedBars(calendarBars))
	checkpoint := TradingCheckpointAt(now(), calendarDates)
	if calendarError != nil || len(calendarDates) == 0 {
		previous.Warnings = append(previous.Warnings, "沪深300交易日历不可用，停牌识别将退化为个股交易日")
	}
	report := cloneReport(previous)
	report.EngineVersion = ShadowEngineVersion
	report.Config = cfg
	report.ConfigFingerprint = OptionsFingerprint(cfg, options.Limit)
	report.SignalLimit = options.Limit
	report.GeneratedAt = now()
	report.AsOf = checkpoint.Date
	report.CheckpointPhase = checkpoint.Phase
	report.SignalCount = len(signals)
	if report.AsOf < previous.AsOf {
		return previous, fmt.Errorf("影子账户检查点倒退: %s -> %s", previous.AsOf, report.AsOf)
	}
	active := make(map[string]shadowPosition, len(previous.Positions))
	archivedSignals := representativeSignals(signals, 0)
	for index := range report.Rejections {
		if report.Rejections[index].SignalScore > 0 && len(report.Rejections[index].SignalReasons) > 0 {
			continue
		}
		if signal, found := matchingArchivedRejectionSignal(archivedSignals, report.Rejections[index]); found {
			report.Rejections[index].SignalScore = signal.Score
			report.Rejections[index].SignalReasons = append([]string(nil), signal.Reasons...)
		}
	}
	for _, position := range previous.Positions {
		bars, err := e.history.FetchDailyBars(ctx, position.Symbol)
		if err != nil || len(bars) == 0 {
			report.Warnings = append(report.Warnings, position.Symbol+" 持仓日K不可用，保留原持仓")
			preserved := position
			active[position.Symbol] = shadowPosition{fallback: &preserved}
			continue
		}
		bars = normalizedBars(bars)
		signal := realtime.Signal{ID: position.SignalID, Symbol: position.Symbol, Name: position.Name, Price: position.SignalClose, Score: position.SignalScore, TriggerPrice: position.TriggerPrice, InvalidationPrice: position.InvalidationPrice, Reasons: append([]string(nil), position.SignalReasons...)}
		if enriched, found := matchingArchivedSignal(archivedSignals, position); found {
			if signal.ID == "" {
				signal.ID = enriched.ID
			}
			if signal.Name == "" {
				signal.Name = enriched.Name
			}
			if signal.Price <= 0 {
				signal.Price = enriched.Price
			}
			if signal.Score <= 0 {
				signal.Score = enriched.Score
			}
			if signal.TriggerPrice <= 0 {
				signal.TriggerPrice = enriched.TriggerPrice
			}
			if signal.InvalidationPrice <= 0 {
				signal.InvalidationPrice = enriched.InvalidationPrice
			}
			if len(signal.Reasons) == 0 {
				signal.Reasons = append([]string(nil), enriched.Reasons...)
			}
		}
		if signal.ID == "" {
			signal.ID = position.Symbol + "-" + position.EntryDate
		}
		if position.SignalDate != "" {
			signal.AsOf = parseShanghaiDate(position.SignalDate)
		}
		lots := normalizePositionLots(position, report.Orders, cfg, calendarDates, barDates(bars))
		for index := range lots {
			lotSignal := signal
			lotSignal.ID = strings.TrimSuffix(lots[index].entry.ID, "-buy")
			lotSignal.Score = lots[index].entry.SignalScore
			lotSignal.TriggerPrice = lots[index].entry.TriggerPrice
			lotSignal.InvalidationPrice = lots[index].entry.InvalidationPrice
			lotSignal.Reasons = append([]string(nil), lots[index].entry.SignalReasons...)
			lots[index].plan.signal = lotSignal
			lots[index].plan.action = "exit"
			lots[index].plan.lotOrderID = lots[index].entry.ID
			lots[index].plan.bars = bars
			lots[index].plan.signalClose = position.SignalClose
			lots[index].plan.entryIndex = barIndex(bars, lots[index].entry.AttemptDate)
			lots[index].plan.exitIndex = barIndex(bars, lots[index].plan.targetExitDate)
			if orderIndex := matchingOrderIndex(report.Orders, lots[index].entry); orderIndex >= 0 {
				report.Orders[orderIndex].ExecutionTime = lots[index].entry.ExecutionTime
			}
		}
		active[position.Symbol] = shadowPosition{plan: shadowPlan{signal: signal, bars: bars, signalClose: position.SignalClose}, lots: lots, lastSignalDate: position.SignalDate, lastSignalScore: position.SignalScore}
	}
	newPlans, err := e.plansAfter(ctx, signals, cfg, previous.AsOf, checkpoint, calendarDates)
	if err != nil {
		return previous, err
	}
	for symbol, position := range active {
		if position.fallback != nil {
			continue
		}
		for index := range position.lots {
			lot := &position.lots[index]
			lot.plan.exitDate = lot.plan.targetExitDate
			lot.plan.exitIndex = barIndex(lot.plan.bars, lot.plan.exitDate)
			if lot.plan.exitDate == "" || checkpoint.Phase != CheckpointClose || lot.plan.exitDate > checkpoint.Date {
				lot.plan.exitDate = ""
				lot.plan.exitIndex = -1
			} else if lot.plan.exitDate < checkpoint.Date {
				lot.plan.exitDate = checkpoint.Date
				lot.plan.exitIndex = barIndex(lot.plan.bars, checkpoint.Date)
			}
			if lot.plan.exitDate != "" {
				newPlans = append(newPlans, lot.plan)
			}
		}
		active[symbol] = position
	}
	if previous.EngineVersion != ShadowEngineVersion && report.TotalTurnover <= 0 {
		report.TotalTurnover = filledOrderTurnover(report.Orders)
	}
	report = simulateFrom(report, newPlans, cfg, report.RemainingCash, active)
	return report, nil
}

func (e *Evaluator) plansAfter(ctx context.Context, signals []realtime.Signal, cfg Config, after string, checkpoint Checkpoint, calendarDates []string) ([]shadowPlan, error) {
	selected := representativeSignals(signals, 0)
	bySymbol := make(map[string][]realtime.Signal)
	for _, signal := range selected {
		bySymbol[signal.Symbol] = append(bySymbol[signal.Symbol], signal)
	}
	plans := make([]shadowPlan, 0, len(selected))
	for symbol, items := range bySymbol {
		bars, err := e.history.FetchDailyBars(ctx, symbol)
		if err != nil || len(bars) == 0 {
			continue
		}
		bars = normalizedBars(bars)
		for _, signal := range items {
			if !shadowSignalActionable(signal) {
				continue
			}
			plan, reason, pending := makePlan(signal, bars, calendarDates, cfg.HoldingDays)
			if reason != "" {
				if pending {
					continue
				}
				continue
			}
			if plan.entryDate <= after || plan.entryDate > checkpoint.Date {
				continue
			}
			if plan.exitDate > checkpoint.Date || plan.exitDate == checkpoint.Date && checkpoint.Phase != CheckpointClose {
				plan.exitDate = ""
				plan.exitIndex = -1
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
	return plans, nil
}

func matchingArchivedSignal(signals []realtime.Signal, position ShadowOpenPosition) (realtime.Signal, bool) {
	var best realtime.Signal
	found := false
	for _, signal := range signals {
		if signal.Symbol != position.Symbol || signalDate(signal) != position.SignalDate {
			continue
		}
		if !found || signal.AsOf.After(best.AsOf) || (signal.AsOf.Equal(best.AsOf) && signal.Score > best.Score) {
			best = signal
			found = true
		}
	}
	return best, found
}

func matchingArchivedRejectionSignal(signals []realtime.Signal, rejection ShadowRejection) (realtime.Signal, bool) {
	for _, signal := range signals {
		if orderID(signal, rejection.Side) == rejection.OrderID {
			return signal, true
		}
	}
	var best realtime.Signal
	found := false
	for _, signal := range signals {
		if signal.Symbol != rejection.Symbol || signalDate(signal) != rejection.SignalDate {
			continue
		}
		if !found || signal.AsOf.After(best.AsOf) || (signal.AsOf.Equal(best.AsOf) && signal.Score > best.Score) {
			best = signal
			found = true
		}
	}
	return best, found
}

func matchingFilledOrder(orders []ShadowOrder, position ShadowOpenPosition) (ShadowOrder, bool) {
	for _, order := range orders {
		if order.Status == OrderFilled && order.Side == "buy" && order.Symbol == position.Symbol && order.AttemptDate == position.EntryDate && order.Quantity == position.Quantity {
			return order, true
		}
	}
	return ShadowOrder{}, false
}

func matchingOrderIndex(orders []ShadowOrder, target ShadowOrder) int {
	for index, order := range orders {
		if order.Side == target.Side && order.Symbol == target.Symbol && order.AttemptDate == target.AttemptDate && order.Quantity == target.Quantity {
			return index
		}
	}
	return -1
}

func holdingExitDate(calendarDates []string, entryDate string, holdingDays int) string {
	if entryDate == "" || holdingDays <= 0 || len(calendarDates) == 0 {
		return ""
	}
	entryIndex := sort.SearchStrings(calendarDates, entryDate)
	if entryIndex >= len(calendarDates) || calendarDates[entryIndex] != entryDate {
		return ""
	}
	exitIndex := entryIndex + holdingDays - 1
	if exitIndex >= len(calendarDates) {
		return ""
	}
	return calendarDates[exitIndex]
}

func simulateFrom(report Report, plans []shadowPlan, cfg Config, cash float64, active map[string]shadowPosition) Report {
	if !finite(cash) {
		cash = report.RemainingCash
	}
	if executionLedgerEmpty(report) && cash <= 0 {
		cash = cfg.InitialCash
	}
	events := make(map[string][]int)
	for index, plan := range plans {
		if plan.entryDate != "" {
			events[plan.entryDate] = append(events[plan.entryDate], index)
		}
		if plan.exitDate != "" && plan.exitDate != plan.entryDate {
			events[plan.exitDate] = append(events[plan.exitDate], index)
		}
	}
	dates := make([]string, 0, len(events))
	for date := range events {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	dailyDeployment := make(map[string]float64)
	for _, date := range dates {
		indexes := events[date]
		handledOpening := make(map[int]bool, len(indexes))

		// Opening reductions settle before opening entries. A-share sale proceeds
		// can fund another opening buy, while proceeds from the later close cannot.
		for _, index := range indexes {
			plan := plans[index]
			if plan.entryDate != date || plan.entryIndex < 0 || plan.entryIndex >= len(plan.bars) {
				continue
			}
			position, exists := active[plan.signal.Symbol]
			if !exists {
				continue
			}
			if position.fallback != nil {
				appendShadowDecision(&report, plan.signal, date, "wait", "持仓日K不可用，暂停仓位调整", positionQuantity(position), cfg.MaxPositionPercent)
				handledOpening[index] = true
				continue
			}
			if plan.signal.Score > position.lastSignalScore-cfg.AdditionScoreStep {
				continue
			}
			handledOpening[index] = true
			lotIndex := oldestAvailableLot(position.lots, date)
			if lotIndex < 0 {
				appendShadowDecision(&report, plan.signal, date, "hold", "信号走弱，但没有满足T+1的可卖批次", positionQuantity(position), cfg.MaxPositionPercent)
				continue
			}
			bar := plan.bars[plan.entryIndex]
			if bar.Open <= 0 || onePriceBar(bar) || limitLockedAtExit(plan.signal, plan.bars, plan.entryIndex) {
				appendShadowDecision(&report, plan.signal, date, "wait", "信号走弱，但开盘一字板或停牌，减仓无法执行", positionQuantity(position), cfg.MaxPositionPercent)
				continue
			}
			lot := position.lots[lotIndex]
			sell := makeOrder(plan.signal, "sell", date, bar.Open, lot.entry.Quantity, cfg, 0)
			sell.Status = OrderFilled
			sell.ExecutionTime = simulatedOpeningExecutionTime(date)
			sell.PositionAction = "reduce"
			sell.PositionSequence = lotIndex + 1
			fee := transactionFee(sell.Amount, "sell", cfg)
			cash += sell.Amount - fee
			report.Orders = append(report.Orders, sell)
			report.Trades = append(report.Trades, tradeForLot(lot, sell, bar.Open, date, plan.entryIndex, fee, len(report.Trades)+1))
			report.CompletedTrades++
			report.TotalTurnover += sell.Amount
			report.TotalFees += fee
			position.lots = append(position.lots[:lotIndex], position.lots[lotIndex+1:]...)
			position.plan = plan
			position.lastSignalDate = signalDate(plan.signal)
			position.lastSignalScore = plan.signal.Score
			appendShadowDecision(&report, plan.signal, date, "reduce", "信号分显著回落，次日开盘减去一个可卖批次", positionQuantity(position), cfg.MaxPositionPercent)
			if len(position.lots) == 0 {
				delete(active, plan.signal.Symbol)
			} else {
				active[plan.signal.Symbol] = position
			}
		}

		for _, index := range indexes {
			if handledOpening[index] {
				continue
			}
			plan := plans[index]
			if plan.entryDate != date {
				continue
			}
			if plan.entryIndex < 0 || plan.entryIndex >= len(plan.bars) {
				continue
			}
			bar := plan.bars[plan.entryIndex]
			position, exists := active[plan.signal.Symbol]
			if !exists && !shadowSignalEntryEligible(plan.signal, cfg) {
				continue
			}
			if exists && !shadowSignalEntryEligible(plan.signal, cfg) {
				appendShadowDecision(&report, plan.signal, date, "hold", "信号未满足加仓门槛，维持现有仓位", positionQuantity(position), cfg.MaxPositionPercent)
				continue
			}
			if exists && (len(position.lots) >= cfg.MaxEntryTranches || plan.signal.Score < position.lastSignalScore+cfg.AdditionScoreStep) {
				reason := "信号未显著增强，维持现有仓位"
				if len(position.lots) >= cfg.MaxEntryTranches {
					reason = "已达到最大分批建仓次数，维持现有仓位"
				}
				appendShadowDecision(&report, plan.signal, date, "hold", reason, positionQuantity(position), cfg.MaxPositionPercent)
				continue
			}
			if onePriceBar(bar) || limitLockedAtEntry(plan.signal, plan.bars, plan.entryIndex) || bar.Open <= 0 {
				rejection := makeRejection(plan.signal, "buy", date, "次日开盘一字板或停牌，无法执行")
				if !hasRejection(report.Rejections, rejection) {
					report.RejectedOrders++
					report.Rejections = append(report.Rejections, rejection)
				}
				continue
			}
			capacity := plan.capacity * cfg.MaxParticipationPercent / 100
			budget, reason := entryBudget(cfg, cash, dailyDeployment[date], active, position, exists)
			if budget <= 0 {
				appendShadowDecision(&report, plan.signal, date, "wait", reason, positionQuantity(position), cfg.MaxPositionPercent)
				continue
			}
			quantity := quantityWithinBudget(bar.Open, budget, capacity, cfg)
			if quantity < cfg.LotSize {
				reason := "资金不足或不足一手"
				price := bar.Open * (1 + cfg.SlippageBPS/10000)
				if capacity > 0 && int(capacity/price/float64(cfg.LotSize))*cfg.LotSize < cfg.LotSize {
					reason = "成交额容量不足一手"
				}
				rejection := makeRejection(plan.signal, "buy", date, reason)
				if !hasRejection(report.Rejections, rejection) {
					report.RejectedOrders++
					report.Rejections = append(report.Rejections, rejection)
				}
				continue
			}
			buy := makeOrder(plan.signal, "buy", date, bar.Open, quantity, cfg, capacity)
			fee := transactionFee(buy.Amount, "buy", cfg)
			if buy.Amount+fee > cash || cash-buy.Amount-fee < cfg.InitialCash*cfg.CashReservePercent/100 {
				appendShadowDecision(&report, plan.signal, date, "wait", "现金缓冲不足，等待后续交易日", positionQuantity(position), cfg.MaxPositionPercent)
				continue
			}
			buy.Status = OrderFilled
			if exists {
				buy.PositionAction = "add"
				buy.PositionSequence = len(position.lots) + 1
			} else {
				buy.PositionAction = "open"
				buy.PositionSequence = 1
			}
			cash -= buy.Amount + fee
			report.Orders = append(report.Orders, buy)
			report.FilledEntries++
			report.TotalTurnover += buy.Amount
			report.TotalFees += fee
			dailyDeployment[date] += buy.Amount + fee
			lotPlan := plan
			lotPlan.action = "exit"
			lotPlan.lotOrderID = buy.ID
			lot := shadowLot{plan: lotPlan, entry: buy, entryCost: buy.Amount + fee}
			if exists {
				position.lots = append(position.lots, lot)
				position.plan = plan
				position.lastSignalDate = signalDate(plan.signal)
				position.lastSignalScore = plan.signal.Score
				active[plan.signal.Symbol] = position
				appendShadowDecision(&report, plan.signal, date, "add", "信号显著增强，按目标仓位分批加仓", positionQuantity(position), cfg.MaxPositionPercent)
			} else {
				active[plan.signal.Symbol] = shadowPosition{plan: plan, lots: []shadowLot{lot}, lastSignalDate: signalDate(plan.signal), lastSignalScore: plan.signal.Score}
				appendShadowDecision(&report, plan.signal, date, "open", "首次建仓仅使用目标仓位的一部分，保留后续操作空间", buy.Quantity, cfg.MaxPositionPercent)
			}
		}

		for _, index := range indexes {
			plan := plans[index]
			if plan.exitDate != date {
				continue
			}
			position, ok := active[plan.signal.Symbol]
			if !ok {
				continue
			}
			lotIndex := shadowLotIndex(position.lots, plan.lotOrderID)
			if lotIndex < 0 {
				continue
			}
			lot := position.lots[lotIndex]
			if date <= lot.entry.AttemptDate {
				continue
			}
			if plan.exitIndex < 0 || plan.exitIndex >= len(plan.bars) {
				continue
			}
			bar := plan.bars[plan.exitIndex]
			if onePriceBar(bar) || limitLockedAtExit(plan.signal, plan.bars, plan.exitIndex) || bar.Close <= 0 {
				rejection := makeRejection(plan.signal, "sell", date, "卖出日一字板或停牌，无法执行")
				if !hasRejection(report.Rejections, rejection) {
					report.RejectedOrders++
					report.Rejections = append(report.Rejections, rejection)
				}
				continue
			}
			sell := makeOrder(lot.plan.signal, "sell", date, bar.Close, lot.entry.Quantity, cfg, 0)
			sell.Status = OrderFilled
			sell.PositionAction = "exit"
			sell.PositionSequence = lotIndex + 1
			fee := transactionFee(sell.Amount, "sell", cfg)
			cash += sell.Amount - fee
			report.Orders = append(report.Orders, sell)
			report.Trades = append(report.Trades, tradeForLot(lot, sell, bar.Close, date, plan.exitIndex, fee, len(report.Trades)+1))
			report.CompletedTrades++
			report.TotalTurnover += sell.Amount
			report.TotalFees += fee
			position.lots = append(position.lots[:lotIndex], position.lots[lotIndex+1:]...)
			if len(position.lots) == 0 {
				delete(active, plan.signal.Symbol)
			} else {
				active[plan.signal.Symbol] = position
			}
		}
	}
	recomputeTradeStats(&report)
	report.RemainingCash = cash
	report.OpenPositions = len(active)
	report.Positions = report.Positions[:0]
	for _, position := range active {
		if position.fallback != nil {
			report.Positions = append(report.Positions, *position.fallback)
			continue
		}
		report.Positions = append(report.Positions, aggregateShadowPosition(position, cfg, report.AsOf))
	}
	sort.SliceStable(report.Positions, func(i, j int) bool { return report.Positions[i].EntryDate > report.Positions[j].EntryDate })
	recomputeAccountMetrics(&report)
	return report
}

func quantityWithinBudget(rawPrice, budget, capacity float64, cfg Config) int {
	price := rawPrice * (1 + cfg.SlippageBPS/10000)
	if price <= 0 || budget <= 0 {
		return 0
	}
	quantity := int(budget/price/float64(cfg.LotSize)) * cfg.LotSize
	if capacity > 0 {
		quantity = minInt(quantity, int(capacity/price/float64(cfg.LotSize))*cfg.LotSize)
	}
	for quantity >= cfg.LotSize {
		amount := price * float64(quantity)
		if amount+transactionFee(amount, "buy", cfg) <= budget {
			return quantity
		}
		quantity -= cfg.LotSize
	}
	return 0
}

func shadowLotIndex(lots []shadowLot, orderID string) int {
	for index, lot := range lots {
		if lot.entry.ID == orderID {
			return index
		}
	}
	return -1
}

func oldestAvailableLot(lots []shadowLot, date string) int {
	oldest := -1
	for index, lot := range lots {
		if lot.entry.AttemptDate == "" || lot.entry.AttemptDate >= date {
			continue
		}
		if oldest < 0 || lot.entry.AttemptDate < lots[oldest].entry.AttemptDate {
			oldest = index
		}
	}
	return oldest
}

func positionQuantity(position shadowPosition) int {
	if position.fallback != nil {
		return position.fallback.Quantity
	}
	quantity := 0
	for _, lot := range position.lots {
		quantity += lot.entry.Quantity
	}
	return quantity
}

func positionCost(position shadowPosition) float64 {
	cost := 0.0
	for _, lot := range position.lots {
		cost += lot.entryCost
	}
	return cost
}

func investedCost(active map[string]shadowPosition) float64 {
	total := 0.0
	for _, position := range active {
		if position.fallback != nil {
			entryAmount := position.fallback.EntryAmount
			if entryAmount <= 0 {
				entryAmount = position.fallback.EntryPrice * float64(position.fallback.Quantity)
			}
			total += entryAmount + position.fallback.EntryFee
			continue
		}
		total += positionCost(position)
	}
	return total
}

func entryBudget(cfg Config, cash, deployedToday float64, active map[string]shadowPosition, position shadowPosition, exists bool) (float64, string) {
	reserve := cfg.InitialCash * cfg.CashReservePercent / 100
	availableCash := cash - reserve
	if availableCash <= 0 {
		return 0, "已达到现金缓冲下限"
	}
	portfolioRoom := cfg.InitialCash*cfg.MaxPortfolioPercent/100 - investedCost(active)
	if portfolioRoom <= 0 {
		return 0, "组合仓位已达到上限"
	}
	dailyRoom := cfg.InitialCash*cfg.MaxDailyDeploymentPercent/100 - deployedToday
	if dailyRoom <= 0 {
		return 0, "当日新增资金已达到上限"
	}
	targetPosition := cfg.InitialCash * cfg.MaxPositionPercent / 100
	positionRoom := targetPosition
	if exists {
		positionRoom -= positionCost(position)
		remainingTranches := cfg.MaxEntryTranches - 1
		if remainingTranches > 0 {
			additionRoom := targetPosition * (1 - cfg.InitialEntryPercent/100) / float64(remainingTranches)
			positionRoom = math.Min(positionRoom, additionRoom)
		}
	} else {
		positionRoom *= cfg.InitialEntryPercent / 100
	}
	if positionRoom <= 0 {
		return 0, "单股目标仓位已达到上限"
	}
	budget := math.Min(availableCash, math.Min(portfolioRoom, math.Min(dailyRoom, positionRoom)))
	return budget, ""
}

func tradeForLot(lot shadowLot, sell ShadowOrder, exitClose float64, exitDate string, exitIndex int, fee float64, sequence int) ShadowTrade {
	theoretical := 0.0
	if lot.plan.signalClose > 0 {
		theoretical = (exitClose/lot.plan.signalClose - 1) * 100
	}
	netProfit := sell.Amount - fee - lot.entryCost
	executable := safeReturnPercent(netProfit, lot.entryCost)
	holdingDays := 0
	if lot.plan.entryIndex >= 0 && exitIndex >= lot.plan.entryIndex {
		holdingDays = exitIndex - lot.plan.entryIndex + 1
	}
	return ShadowTrade{ID: fmt.Sprintf("ST%04d", sequence), EntryOrderID: lot.entry.ID, ExitOrderID: sell.ID, Symbol: sell.Symbol, Name: sell.Name, SignalDate: lot.entry.SignalDate, EntryDate: lot.entry.AttemptDate, ExitDate: exitDate, Quantity: lot.entry.Quantity, SignalClose: lot.plan.signalClose, ExitClose: exitClose, EntryPrice: lot.entry.Price, ExitPrice: sell.Price, TheoreticalReturnPercent: theoretical, ExecutableReturnPercent: executable, ExecutionGapPercent: executable - theoretical, GrossProfit: (sell.Price - lot.entry.Price) * float64(sell.Quantity), NetProfit: netProfit, TotalFee: lot.entryCost - lot.entry.Amount + fee, HoldingDays: holdingDays, PositionAction: sell.PositionAction, ExitTime: sell.ExecutionTime}
}

func appendShadowDecision(report *Report, signal realtime.Signal, date, action, reason string, quantity int, targetPercent float64) {
	if report == nil {
		return
	}
	id := signal.ID + "-" + action
	for _, decision := range report.Decisions {
		if decision.ID == id && decision.Date == date {
			return
		}
	}
	report.Decisions = append(report.Decisions, ShadowDecision{ID: id, Symbol: signal.Symbol, Name: signal.Name, Date: date, Action: action, Reason: reason, SignalScore: signal.Score, SignalReasons: append([]string(nil), signal.Reasons...), CurrentQuantity: quantity, TargetPositionPercent: targetPercent})
}

func aggregateShadowPosition(position shadowPosition, cfg Config, asOf string) ShadowOpenPosition {
	if position.fallback != nil {
		return *position.fallback
	}
	quantity, available := 0, 0
	entryAmount, entryFee, entryCost := 0.0, 0.0, 0.0
	entryDate, entryTime, targetExit := "", "", ""
	lots := make([]ShadowPositionLot, 0, len(position.lots))
	for _, lot := range position.lots {
		quantity += lot.entry.Quantity
		if lot.entry.AttemptDate < asOf {
			available += lot.entry.Quantity
		}
		entryAmount += lot.entry.Amount
		entryCost += lot.entryCost
		entryFee += lot.entryCost - lot.entry.Amount
		if entryDate == "" || lot.entry.AttemptDate < entryDate {
			entryDate = lot.entry.AttemptDate
			entryTime = lot.entry.ExecutionTime
		}
		if targetExit == "" || lot.plan.targetExitDate > targetExit {
			targetExit = lot.plan.targetExitDate
		}
		lots = append(lots, ShadowPositionLot{OrderID: lot.entry.ID, EntryDate: lot.entry.AttemptDate, EntryTime: lot.entry.ExecutionTime, Quantity: lot.entry.Quantity, EntryPrice: lot.entry.Price, EntryAmount: lot.entry.Amount, EntryFee: lot.entryCost - lot.entry.Amount, SignalID: strings.TrimSuffix(lot.entry.ID, "-buy"), SignalDate: lot.entry.SignalDate, SignalClose: lot.plan.signalClose, SignalScore: lot.entry.SignalScore, TriggerPrice: lot.entry.TriggerPrice, InvalidationPrice: lot.entry.InvalidationPrice, SignalReasons: append([]string(nil), lot.entry.SignalReasons...), TargetExitDate: lot.plan.targetExitDate})
	}
	lastBar := latestBar(position.plan.bars, asOf)
	marketValue := lastBar.Close * float64(quantity)
	profit := marketValue - entryCost
	entryPrice := 0.0
	if quantity > 0 {
		entryPrice = entryAmount / float64(quantity)
	}
	return ShadowOpenPosition{SignalID: position.plan.signal.ID, Symbol: position.plan.signal.Symbol, Name: position.plan.signal.Name, SignalDate: position.lastSignalDate, EntryDate: entryDate, EntryTime: entryTime, Quantity: quantity, EntryPrice: entryPrice, EntryAmount: entryAmount, EntryFee: entryFee, SignalClose: position.plan.signalClose, AvailableQuantity: available, SignalScore: position.lastSignalScore, TriggerPrice: position.plan.signal.TriggerPrice, InvalidationPrice: position.plan.signal.InvalidationPrice, SignalReasons: append([]string(nil), position.plan.signal.Reasons...), LastDate: lastBar.Date, LastPrice: lastBar.Close, MarketValue: marketValue, UnrealizedProfit: profit, UnrealizedReturnPercent: safeReturnPercent(profit, entryCost), TargetExitDate: targetExit, Lots: lots, AdditionCount: maxInt(0, len(lots)-1), TargetPositionPercent: cfg.MaxPositionPercent}
}

func tPlusOneAvailableQuantity(entryDate, asOf string, quantity int) int {
	if entryDate != "" && asOf > entryDate {
		return quantity
	}
	return 0
}

func hasRejection(rejections []ShadowRejection, target ShadowRejection) bool {
	for _, rejection := range rejections {
		if sameRejection(rejection, target) {
			return true
		}
	}
	return false
}

func recomputeTradeStats(report *Report) {
	report.TheoreticalAverageReturn = 0
	report.ExecutableAverageReturn = 0
	report.ExecutionGapPercent = 0
	if len(report.Trades) == 0 {
		return
	}
	for _, trade := range report.Trades {
		report.TheoreticalAverageReturn += trade.TheoreticalReturnPercent
		report.ExecutableAverageReturn += trade.ExecutableReturnPercent
	}
	report.TheoreticalAverageReturn /= float64(len(report.Trades))
	report.ExecutableAverageReturn /= float64(len(report.Trades))
	report.ExecutionGapPercent = report.ExecutableAverageReturn - report.TheoreticalAverageReturn
}

func recomputeAccountMetrics(report *Report) {
	initialCash := report.InitialCash
	if initialCash <= 0 || !finite(initialCash) {
		initialCash = canonicalConfig(report.Config).InitialCash
		report.InitialCash = initialCash
	}
	marketValue := 0.0
	for _, position := range report.Positions {
		if finite(position.MarketValue) {
			marketValue += position.MarketValue
		}
	}
	realized := 0.0
	for _, trade := range report.Trades {
		if finite(trade.NetProfit) {
			realized += trade.NetProfit
		}
	}
	report.TotalMarketValue = marketValue
	report.TotalEquity = report.RemainingCash + marketValue
	report.RealizedProfit = realized
	report.TotalProfit = report.TotalEquity - initialCash
	report.UnrealizedProfit = report.TotalProfit - realized
	if initialCash > 0 {
		report.TotalReturnPercent = report.TotalProfit / initialCash * 100
	}
}

func filledOrderTurnover(orders []ShadowOrder) float64 {
	var turnover float64
	for _, order := range orders {
		if order.Status == OrderFilled && order.Amount > 0 && finite(order.Amount) {
			turnover += order.Amount
		}
	}
	return turnover
}

func cloneReport(report Report) Report {
	report.Trades = append([]ShadowTrade(nil), report.Trades...)
	report.Positions = append([]ShadowOpenPosition(nil), report.Positions...)
	report.Orders = append([]ShadowOrder(nil), report.Orders...)
	report.Rejections = append([]ShadowRejection(nil), report.Rejections...)
	report.Warnings = append([]string(nil), report.Warnings...)
	report.Decisions = append([]ShadowDecision(nil), report.Decisions...)
	return report
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
	calendarBars, calendarError := e.history.FetchDailyBars(ctx, "sh000300")
	calendarDates := barDates(normalizedBars(calendarBars))
	if calendarError != nil || len(calendarDates) == 0 {
		calendarDates = nil
	}
	checkpoint := TradingCheckpointAt(now(), calendarDates)
	report := Report{EngineVersion: ShadowEngineVersion, ConfigFingerprint: OptionsFingerprint(cfg, options.Limit), SignalLimit: options.Limit, CheckpointPhase: checkpoint.Phase, GeneratedAt: now(), AsOf: checkpoint.Date, Config: cfg, SignalCount: len(signals), InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash}
	if len(selected) == 0 {
		return report, nil
	}
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
			if !shadowSignalActionable(signal) {
				continue
			}
			entryEligible := shadowSignalEntryEligible(signal, cfg)
			if entryEligible {
				report.CandidateCount++
			}
			plan, reason, pending := makePlan(signal, bars, calendarDates, cfg.HoldingDays)
			if reason != "" {
				if !entryEligible {
					continue
				}
				if pending {
					report.PendingCandidates++
					report.Warnings = append(report.Warnings, signal.Symbol+" "+signalDate(signal)+" 影子窗口待成熟: "+reason)
				} else {
					report.RejectedOrders++
					report.Rejections = append(report.Rejections, makeRejection(signal, "buy", "", reason))
				}
				continue
			}
			if plan.entryDate > report.AsOf {
				report.PendingCandidates++
				report.Warnings = append(report.Warnings, signal.Symbol+" "+signalDate(signal)+" 影子窗口待成熟: 次日交易日尚未到达")
				continue
			}
			if plan.exitDate > report.AsOf || (plan.exitDate == report.AsOf && report.CheckpointPhase != CheckpointClose) {
				plan.exitDate = ""
				plan.exitIndex = -1
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
	if cfg.MaxPortfolioPercent <= 0 || cfg.MaxPortfolioPercent > 100 {
		cfg.MaxPortfolioPercent = defaults.MaxPortfolioPercent
	}
	if cfg.CashReservePercent <= 0 || cfg.CashReservePercent >= 100 {
		cfg.CashReservePercent = defaults.CashReservePercent
	}
	if cfg.MaxDailyDeploymentPercent <= 0 || cfg.MaxDailyDeploymentPercent > 100 {
		cfg.MaxDailyDeploymentPercent = defaults.MaxDailyDeploymentPercent
	}
	if cfg.InitialEntryPercent <= 0 || cfg.InitialEntryPercent > 100 {
		cfg.InitialEntryPercent = defaults.InitialEntryPercent
	}
	if cfg.MaxEntryTranches <= 0 {
		cfg.MaxEntryTranches = defaults.MaxEntryTranches
	}
	if cfg.AdditionScoreStep <= 0 || !finite(cfg.AdditionScoreStep) {
		cfg.AdditionScoreStep = defaults.AdditionScoreStep
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
		if !found || signal.AsOf.After(previous.AsOf) || (signal.AsOf.Equal(previous.AsOf) && signal.Score > previous.Score) {
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

func shadowSignalActionable(signal realtime.Signal) bool {
	switch signal.State {
	case realtime.StateTriggered, realtime.StateWatching, realtime.StateWeak, realtime.StateInvalid:
		return true
	default:
		return false
	}
}

func shadowSignalEntryEligible(signal realtime.Signal, cfg Config) bool {
	return (signal.State == realtime.StateTriggered || signal.State == realtime.StateWatching) && signal.Score >= cfg.MinimumScore
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
	return shadowPlan{signal: signal, bars: bars, action: "entry", lotOrderID: orderID(signal, "buy"), signalClose: signalClose, capacity: capacity, entryIndex: entryIndex, exitIndex: exitIndex, entryDate: entryDate, exitDate: exitDate, targetExitDate: exitDate}, "", false
}

func simulate(report Report, plans []shadowPlan, cfg Config) Report {
	return simulateFrom(report, plans, cfg, cfg.InitialCash, make(map[string]shadowPosition))
}

func makeOrder(signal realtime.Signal, side, date string, rawPrice float64, quantity int, cfg Config, capacity float64) ShadowOrder {
	direction := 1.0
	if side == "sell" {
		direction = -1
	}
	price := rawPrice * (1 + direction*cfg.SlippageBPS/10000)
	return ShadowOrder{ID: orderID(signal, side), Symbol: signal.Symbol, Name: signal.Name, Side: side, SignalDate: signalDate(signal), AttemptDate: date, Quantity: quantity, RawPrice: rawPrice, Price: price, Amount: price * float64(quantity), CapacityAmount: capacity, Status: "pending", ExecutionTime: simulatedExecutionTime(side, date), SignalScore: signal.Score, TriggerPrice: signal.TriggerPrice, InvalidationPrice: signal.InvalidationPrice, SignalReasons: append([]string(nil), signal.Reasons...)}
}

func simulatedExecutionTime(side, date string) string {
	if date == "" {
		return ""
	}
	if side == "sell" {
		return date + " 15:00:00"
	}
	return date + " 09:30:00"
}

func simulatedOpeningExecutionTime(date string) string {
	if date == "" {
		return ""
	}
	return date + " 09:30:00"
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

func parseShanghaiDate(value string) time.Time {
	parsed, err := time.ParseInLocation("2006-01-02", value, shanghaiLocation)
	if err != nil {
		return time.Time{}
	}
	return parsed
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

func safeReturnPercent(profit, cost float64) float64 {
	if cost <= 0 || !finite(cost) || !finite(profit) {
		return 0
	}
	return profit / cost * 100
}

// DailyBar has no explicit limit-price fields. This conservative approximation
// rejects an opening/closing bar that is pinned to the inferred board limit,
// while leaving ordinary volatile bars executable.
func limitLockedAtEntry(signal realtime.Signal, bars []domain.DailyBar, index int) bool {
	return limitLockedAtBar(signal.Symbol, bars, index, true)
}

func limitLockedAtExit(signal realtime.Signal, bars []domain.DailyBar, index int) bool {
	return limitLockedAtBar(signal.Symbol, bars, index, false)
}

func limitLockedAtBar(symbol string, bars []domain.DailyBar, index int, opening bool) bool {
	if index <= 0 || index >= len(bars) {
		return false
	}
	bar, previous := bars[index], bars[index-1]
	if previous.Close <= 0 || bar.Open <= 0 || bar.Close <= 0 {
		return false
	}
	limitPercent := 0.10
	code := strings.TrimPrefix(strings.TrimPrefix(symbol, "sh"), "sz")
	if strings.HasPrefix(code, "30") || strings.HasPrefix(code, "68") {
		limitPercent = 0.20
	}
	if strings.HasPrefix(code, "8") || strings.HasPrefix(code, "4") {
		limitPercent = 0.30
	}
	upper, lower := previous.Close*(1+limitPercent), previous.Close*(1-limitPercent)
	price := bar.Open
	if !opening {
		price = bar.Close
	}
	return onePriceBar(bar) && (math.Abs(price-upper) <= math.Max(.01, upper*.001) || math.Abs(price-lower) <= math.Max(.01, lower*.001))
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
