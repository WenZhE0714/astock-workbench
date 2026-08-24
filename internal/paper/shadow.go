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
	SignalID                string   `json:"signal_id,omitempty"`
	Symbol                  string   `json:"symbol"`
	Name                    string   `json:"name,omitempty"`
	SignalDate              string   `json:"signal_date"`
	EntryDate               string   `json:"entry_date"`
	Quantity                int      `json:"quantity"`
	EntryPrice              float64  `json:"entry_price"`
	EntryAmount             float64  `json:"entry_amount,omitempty"`
	EntryFee                float64  `json:"entry_fee,omitempty"`
	SignalClose             float64  `json:"signal_close,omitempty"`
	AvailableQuantity       int      `json:"available_quantity"`
	LastDate                string   `json:"last_date"`
	LastPrice               float64  `json:"last_price"`
	MarketValue             float64  `json:"market_value"`
	UnrealizedProfit        float64  `json:"unrealized_profit"`
	UnrealizedReturnPercent float64  `json:"unrealized_return_percent"`
	TargetExitDate          string   `json:"target_exit_date,omitempty"`
	EntryTime               string   `json:"entry_time,omitempty"`
	SignalScore             float64  `json:"signal_score,omitempty"`
	TriggerPrice            float64  `json:"trigger_price,omitempty"`
	InvalidationPrice       float64  `json:"invalidation_price,omitempty"`
	SignalReasons           []string `json:"signal_reasons,omitempty"`
	ValuationTime           string   `json:"valuation_time,omitempty"`
	ValuationSource         string   `json:"valuation_source,omitempty"`
	RealtimeValuation       bool     `json:"realtime_valuation,omitempty"`
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
}

const (
	ShadowEngineVersion = "tplus1-v6"
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
	previousRejections := rejectionsThrough(previous.Rejections, previous.AsOf)
	nextRejections := rejectionsThrough(next.Rejections, previous.AsOf)
	if len(previousRejections) != len(nextRejections) {
		return fmt.Errorf("截至 %s 的拒绝记录数量从 %d 变为 %d", previous.AsOf, len(previousRejections), len(nextRejections))
	}
	for index := range previousRejections {
		if !sameRejection(previousRejections[index], nextRejections[index]) {
			return fmt.Errorf("截至 %s 的拒绝记录被改写: %s", previous.AsOf, previousRejections[index].OrderID)
		}
	}
	if previous.EngineVersion == ShadowEngineVersion {
		metrics := ledgerMetricsThrough(next, previous.AsOf)
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
	cfg := canonicalConfig(report.Config)
	cash := report.InitialCash
	if cash <= 0 || !finite(cash) {
		cash = cfg.InitialCash
	}
	metrics := ledgerMetrics{remainingCash: cash}
	for _, order := range ordersThrough(report.Orders, date) {
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
	metrics.completedTrades = len(tradesThrough(report.Trades, date))
	metrics.rejectedOrders = len(rejectionsThrough(report.Rejections, date))
	return metrics
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
	signal         realtime.Signal
	bars           []domain.DailyBar
	signalClose    float64
	capacity       float64
	entryIndex     int
	exitIndex      int
	entryDate      string
	exitDate       string
	targetExitDate string
}

type shadowPosition struct {
	plan      shadowPlan
	entry     ShadowOrder
	entryCost float64
	fallback  *ShadowOpenPosition
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
		signal := realtime.Signal{ID: position.SignalID, Symbol: position.Symbol, Name: position.Name, Price: position.SignalClose}
		if enriched, found := matchingArchivedSignal(archivedSignals, position); found {
			signal.ID = enriched.ID
			if signal.Name == "" {
				signal.Name = enriched.Name
			}
			if signal.Price <= 0 {
				signal.Price = enriched.Price
			}
			signal.Score = enriched.Score
			signal.TriggerPrice = enriched.TriggerPrice
			signal.InvalidationPrice = enriched.InvalidationPrice
			signal.Reasons = append([]string(nil), enriched.Reasons...)
		}
		if signal.ID == "" {
			signal.ID = position.Symbol + "-" + position.EntryDate
		}
		if position.SignalDate != "" {
			signal.AsOf = parseShanghaiDate(position.SignalDate)
		}
		entry := ShadowOrder{ID: orderID(signal, "buy"), Symbol: position.Symbol, Name: position.Name, Side: "buy", SignalDate: position.SignalDate, AttemptDate: position.EntryDate, Quantity: position.Quantity, Price: position.EntryPrice, RawPrice: position.EntryPrice, Amount: position.EntryAmount, Status: OrderFilled, ExecutionTime: position.EntryTime, SignalScore: position.SignalScore, TriggerPrice: position.TriggerPrice, InvalidationPrice: position.InvalidationPrice, SignalReasons: append([]string(nil), position.SignalReasons...)}
		if existing, found := matchingFilledOrder(report.Orders, position); found {
			entry = existing
		}
		if entry.SignalScore <= 0 {
			entry.SignalScore = signal.Score
			entry.TriggerPrice = signal.TriggerPrice
			entry.InvalidationPrice = signal.InvalidationPrice
			entry.SignalReasons = append([]string(nil), signal.Reasons...)
		}
		if entry.ExecutionTime == "" {
			entry.ExecutionTime = simulatedExecutionTime("buy", position.EntryDate)
		}
		if index := matchingOrderIndex(report.Orders, entry); index >= 0 {
			report.Orders[index].ExecutionTime = entry.ExecutionTime
			if report.Orders[index].SignalScore <= 0 {
				report.Orders[index].SignalScore = entry.SignalScore
				report.Orders[index].TriggerPrice = entry.TriggerPrice
				report.Orders[index].InvalidationPrice = entry.InvalidationPrice
				report.Orders[index].SignalReasons = append([]string(nil), entry.SignalReasons...)
			}
		}
		if entry.Amount <= 0 {
			entry.Amount = position.EntryPrice * float64(position.Quantity)
		}
		entryCost := entry.Amount + position.EntryFee
		if entryCost <= entry.Amount {
			entryCost = entry.Amount + transactionFee(entry.Amount, "buy", cfg)
		}
		targetExitDate := position.TargetExitDate
		if targetExitDate == "" {
			targetExitDate = holdingExitDate(calendarDates, position.EntryDate, cfg.HoldingDays)
		}
		if targetExitDate == "" {
			targetExitDate = holdingExitDate(barDates(bars), position.EntryDate, cfg.HoldingDays)
		}
		plan := shadowPlan{signal: signal, bars: bars, signalClose: position.SignalClose, entryDate: "", exitDate: targetExitDate, targetExitDate: targetExitDate}
		plan.entryIndex = barIndex(bars, position.EntryDate)
		plan.exitIndex = barIndex(bars, targetExitDate)
		active[position.Symbol] = shadowPosition{plan: plan, entry: entry, entryCost: entryCost}
	}
	newPlans, err := e.plansAfter(ctx, signals, cfg, previous.AsOf, checkpoint, calendarDates)
	if err != nil {
		return previous, err
	}
	for symbol, position := range active {
		if position.fallback != nil {
			continue
		}
		if position.plan.exitDate == "" || checkpoint.Phase != CheckpointClose || position.plan.exitDate > checkpoint.Date {
			position.plan.exitDate = ""
			position.plan.exitIndex = -1
		} else if position.plan.exitDate < checkpoint.Date {
			// A limit-locked/停牌 exit is retried on the current settled close,
			// rather than replaying the historical target date forever.
			position.plan.exitDate = checkpoint.Date
			position.plan.exitIndex = barIndex(position.plan.bars, checkpoint.Date)
		}
		active[symbol] = position
		newPlans = append(newPlans, position.plan)
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
			if signal.State != realtime.StateTriggered && signal.State != realtime.StateWatching || signal.Score < cfg.MinimumScore {
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
		if !found || signal.Score > best.Score || (signal.Score == best.Score && signal.AsOf.After(best.AsOf)) {
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
		if !found || signal.Score > best.Score || (signal.Score == best.Score && signal.AsOf.After(best.AsOf)) {
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
		for _, index := range indexes {
			plan := plans[index]
			if plan.exitDate != date {
				continue
			}
			position, ok := active[plan.signal.Symbol]
			if !ok {
				continue
			}
			if date <= position.entry.AttemptDate {
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
			sell := makeOrder(plan.signal, "sell", date, bar.Close, position.entry.Quantity, cfg, 0)
			sell.Status = OrderFilled
			fee := transactionFee(sell.Amount, "sell", cfg)
			cash += sell.Amount - fee
			report.Orders = append(report.Orders, sell)
			theoretical := 0.0
			if plan.signalClose > 0 {
				theoretical = (bar.Close/plan.signalClose - 1) * 100
			}
			executable := safeReturnPercent(sell.Amount-fee-position.entryCost, position.entryCost)
			report.Trades = append(report.Trades, ShadowTrade{ID: fmt.Sprintf("ST%04d", len(report.Trades)+1), Symbol: plan.signal.Symbol, Name: plan.signal.Name, SignalDate: signalDate(plan.signal), EntryDate: position.entry.AttemptDate, ExitDate: date, Quantity: position.entry.Quantity, SignalClose: plan.signalClose, ExitClose: bar.Close, EntryPrice: position.entry.Price, ExitPrice: sell.Price, TheoreticalReturnPercent: theoretical, ExecutableReturnPercent: executable, ExecutionGapPercent: executable - theoretical, GrossProfit: (sell.Price - position.entry.Price) * float64(sell.Quantity), NetProfit: sell.Amount - fee - position.entryCost, TotalFee: position.entryCost - position.entry.Amount + fee, HoldingDays: plan.exitIndex - plan.entryIndex + 1})
			report.CompletedTrades++
			report.TotalTurnover += sell.Amount
			report.TotalFees += fee
			delete(active, plan.signal.Symbol)
		}
		for _, index := range indexes {
			plan := plans[index]
			if plan.entryDate != date {
				continue
			}
			if _, exists := active[plan.signal.Symbol]; exists || plan.entryIndex < 0 || plan.entryIndex >= len(plan.bars) {
				rejection := makeRejection(plan.signal, "buy", date, "已有同股票影子持仓，拒绝重叠开仓")
				if !hasRejection(report.Rejections, rejection) {
					report.RejectedOrders++
					report.Rejections = append(report.Rejections, rejection)
				}
				continue
			}
			bar := plan.bars[plan.entryIndex]
			if onePriceBar(bar) || limitLockedAtEntry(plan.signal, plan.bars, plan.entryIndex) || bar.Open <= 0 {
				rejection := makeRejection(plan.signal, "buy", date, "次日开盘一字板或停牌，无法执行")
				if !hasRejection(report.Rejections, rejection) {
					report.RejectedOrders++
					report.Rejections = append(report.Rejections, rejection)
				}
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
				rejection := makeRejection(plan.signal, "buy", date, "资金不足或不足一手")
				if !hasRejection(report.Rejections, rejection) {
					report.RejectedOrders++
					report.Rejections = append(report.Rejections, rejection)
				}
				continue
			}
			buy := makeOrder(plan.signal, "buy", date, bar.Open, quantity, cfg, capacity)
			fee := transactionFee(buy.Amount, "buy", cfg)
			if buy.Amount+fee > cash {
				continue
			}
			buy.Status = OrderFilled
			cash -= buy.Amount + fee
			report.Orders = append(report.Orders, buy)
			report.FilledEntries++
			report.TotalTurnover += buy.Amount
			report.TotalFees += fee
			active[plan.signal.Symbol] = shadowPosition{plan: plan, entry: buy, entryCost: buy.Amount + fee}
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
		lastBar := latestBar(position.plan.bars, report.AsOf)
		marketValue := lastBar.Close * float64(position.entry.Quantity)
		profit := marketValue - position.entryCost
		report.Positions = append(report.Positions, ShadowOpenPosition{SignalID: position.plan.signal.ID, Symbol: position.plan.signal.Symbol, Name: position.plan.signal.Name, SignalDate: signalDate(position.plan.signal), EntryDate: position.entry.AttemptDate, EntryTime: position.entry.ExecutionTime, Quantity: position.entry.Quantity, EntryPrice: position.entry.Price, EntryAmount: position.entry.Amount, EntryFee: position.entryCost - position.entry.Amount, SignalClose: position.plan.signalClose, AvailableQuantity: tPlusOneAvailableQuantity(position.entry.AttemptDate, report.AsOf, position.entry.Quantity), SignalScore: position.plan.signal.Score, TriggerPrice: position.plan.signal.TriggerPrice, InvalidationPrice: position.plan.signal.InvalidationPrice, SignalReasons: append([]string(nil), position.plan.signal.Reasons...), LastDate: lastBar.Date, LastPrice: lastBar.Close, MarketValue: marketValue, UnrealizedProfit: profit, UnrealizedReturnPercent: safeReturnPercent(profit, position.entryCost), TargetExitDate: position.plan.targetExitDate})
	}
	recomputeAccountMetrics(&report)
	return report
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
	return shadowPlan{signal: signal, bars: bars, signalClose: signalClose, capacity: capacity, entryIndex: entryIndex, exitIndex: exitIndex, entryDate: entryDate, exitDate: exitDate, targetExitDate: exitDate}, "", false
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
			if date <= position.entry.AttemptDate {
				continue
			}
			bar := plan.bars[plan.exitIndex]
			if onePriceBar(bar) || limitLockedAtExit(plan.signal, plan.bars, plan.exitIndex) || bar.Close <= 0 {
				report.RejectedOrders++
				report.Rejections = append(report.Rejections, makeRejection(plan.signal, "sell", date, "卖出日一字板或停牌，无法执行"))
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
			report.TotalTurnover += sell.Amount
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
				report.Rejections = append(report.Rejections, makeRejection(plan.signal, "buy", date, "已有同股票影子持仓，拒绝重叠开仓"))
				continue
			}
			bar := plan.bars[plan.entryIndex]
			if onePriceBar(bar) || limitLockedAtEntry(plan.signal, plan.bars, plan.entryIndex) || bar.Open <= 0 {
				report.RejectedOrders++
				report.Rejections = append(report.Rejections, makeRejection(plan.signal, "buy", date, "次日开盘一字板或停牌，无法执行"))
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
				report.Rejections = append(report.Rejections, makeRejection(plan.signal, "buy", date, reason))
				continue
			}
			buy := makeOrder(plan.signal, "buy", date, bar.Open, quantity, cfg, capacity)
			fee := transactionFee(buy.Amount, "buy", cfg)
			if buy.Amount+fee > cash {
				report.RejectedOrders++
				report.Rejections = append(report.Rejections, makeRejection(plan.signal, "buy", date, "资金不足"))
				continue
			}
			buy.Status = OrderFilled
			report.Orders = append(report.Orders, buy)
			cash -= buy.Amount + fee
			report.FilledEntries++
			report.TotalTurnover += buy.Amount
			positions[plan.signal.Symbol] = shadowPosition{plan: plan, entry: buy, entryCost: buy.Amount + fee}
			report.TotalFees += fee
		}
	}
	report.RemainingCash = cash
	report.OpenPositions = len(positions)
	for _, position := range positions {
		lastBar := latestBar(position.plan.bars, report.AsOf)
		marketValue := lastBar.Close * float64(position.entry.Quantity)
		profit := marketValue - position.entryCost
		report.Positions = append(report.Positions, ShadowOpenPosition{
			SignalID: position.plan.signal.ID, Symbol: position.plan.signal.Symbol, Name: position.plan.signal.Name, SignalDate: signalDate(position.plan.signal),
			EntryDate: position.entry.AttemptDate, EntryTime: position.entry.ExecutionTime, Quantity: position.entry.Quantity, EntryPrice: position.entry.Price,
			EntryAmount: position.entry.Amount, EntryFee: position.entryCost - position.entry.Amount, SignalClose: position.plan.signalClose, AvailableQuantity: tPlusOneAvailableQuantity(position.entry.AttemptDate, report.AsOf, position.entry.Quantity),
			SignalScore: position.plan.signal.Score, TriggerPrice: position.plan.signal.TriggerPrice, InvalidationPrice: position.plan.signal.InvalidationPrice, SignalReasons: append([]string(nil), position.plan.signal.Reasons...),
			LastDate: lastBar.Date, LastPrice: lastBar.Close, MarketValue: marketValue, UnrealizedProfit: profit,
			UnrealizedReturnPercent: safeReturnPercent(profit, position.entryCost), TargetExitDate: position.plan.targetExitDate,
		})
	}
	sort.SliceStable(report.Positions, func(i, j int) bool { return report.Positions[i].EntryDate > report.Positions[j].EntryDate })
	recomputeTradeStats(&report)
	recomputeAccountMetrics(&report)
	return report
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
func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
