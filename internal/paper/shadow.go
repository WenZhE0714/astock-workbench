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
	"sync"
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
	MaxIndustryPercent        float64 `json:"max_industry_percent"`
	MaxPortfolioPercent       float64 `json:"max_portfolio_percent"`
	CashReservePercent        float64 `json:"cash_reserve_percent"`
	MaxDailyDeploymentPercent float64 `json:"max_daily_deployment_percent"`
	InitialEntryPercent       float64 `json:"initial_entry_percent"`
	MaxEntryTranches          int     `json:"max_entry_tranches"`
	AdditionScoreStep         float64 `json:"addition_score_step"`
	MaxOpenPositions          int     `json:"max_open_positions"`
	MaxDailyRotations         int     `json:"max_daily_rotations"`
	RotationScoreGap          float64 `json:"rotation_score_gap"`
	RotationMinimumHoldDays   int     `json:"rotation_minimum_hold_days"`
	UseCalibratedScore        bool    `json:"use_calibrated_score,omitempty"`
	// UseMonsterRadar gates this account with the independent high-volatility
	// radar. It is opt-in so existing Champion accounts keep their historical
	// eligibility and execution semantics unchanged.
	UseMonsterRadar                bool    `json:"use_monster_radar,omitempty"`
	EnableIntradayT                bool    `json:"enable_intraday_t"`
	TCorePositionPercent           float64 `json:"t_core_position_percent"`
	TTranchePercent                float64 `json:"t_tranche_percent"`
	TMaxDailyRounds                int     `json:"t_max_daily_rounds"`
	TVWAPDeviationPercent          float64 `json:"t_vwap_deviation_percent"`
	TMinimumPriceGapPercent        float64 `json:"t_minimum_price_gap_percent"`
	TMinimumNetProfitPercent       float64 `json:"t_minimum_net_profit_percent"`
	TCooldownMinutes               int     `json:"t_cooldown_minutes"`
	SignalRebalanceCooldownMinutes int     `json:"signal_rebalance_cooldown_minutes"`
	MaxPortfolioRiskPercent        float64 `json:"max_portfolio_risk_percent"`
	MaxPositionRiskPercent         float64 `json:"max_position_risk_percent"`
	MaxLossPercent                 float64 `json:"max_loss_percent"`
	MinimumRiskDistancePercent     float64 `json:"minimum_risk_distance_percent"`
	RiskCooldownDays               int     `json:"risk_cooldown_days"`
	MaxParticipationPercent        float64 `json:"max_participation_percent"`
	SlippageBPS                    float64 `json:"slippage_bps"`
	CommissionRate                 float64 `json:"commission_rate"`
	MinimumCommission              float64 `json:"minimum_commission"`
	StampDutyRate                  float64 `json:"stamp_duty_rate"`
	TransferFeeRate                float64 `json:"transfer_fee_rate"`
	HoldingDays                    int     `json:"holding_days"`
	LotSize                        int     `json:"lot_size"`
}

func DefaultConfig() Config {
	return Config{
		InitialCash: 1_000_000, MinimumScore: 55, MaxPositionPercent: 20,
		MaxIndustryPercent:  30,
		MaxPortfolioPercent: 80, CashReservePercent: 20, MaxDailyDeploymentPercent: 35,
		InitialEntryPercent: 50, MaxEntryTranches: 3, AdditionScoreStep: 4,
		MaxOpenPositions: 8, MaxDailyRotations: 2, RotationScoreGap: 8, RotationMinimumHoldDays: 2,
		EnableIntradayT: true, TCorePositionPercent: 60, TTranchePercent: 20,
		TMaxDailyRounds: 2, TVWAPDeviationPercent: .8, TMinimumPriceGapPercent: .8,
		TMinimumNetProfitPercent: .2, TCooldownMinutes: 15, SignalRebalanceCooldownMinutes: 20,
		MaxPortfolioRiskPercent: 6, MaxPositionRiskPercent: 1.5, MaxLossPercent: 10,
		MinimumRiskDistancePercent: 3, RiskCooldownDays: 3,
		MaxParticipationPercent: 10, SlippageBPS: 5, CommissionRate: .0003,
		MinimumCommission: 5, StampDutyRate: .0005, TransferFeeRate: .00001,
		HoldingDays: 5, LotSize: 100,
	}
}

type Options struct {
	Config        Config
	Limit         int
	Now           func() time.Time
	CalendarDates []string `json:"-"`
	// Realtime enables the point-in-time shadow layer. It is deliberately kept
	// out of the execution fingerprint: quote snapshots are inputs to an event,
	// not account configuration.
	Realtime       bool            `json:"-"`
	RealtimeAt     time.Time       `json:"-"`
	RealtimeQuotes []PositionQuote `json:"-"`
}

type ShadowOrder struct {
	ID                string   `json:"id"`
	EventID           string   `json:"event_id,omitempty"`
	EventSource       string   `json:"event_source,omitempty"`
	Symbol            string   `json:"symbol"`
	Name              string   `json:"name,omitempty"`
	Industry          string   `json:"industry,omitempty"`
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
	EventID       string   `json:"event_id,omitempty"`
	EventTime     string   `json:"event_time,omitempty"`
	EventSource   string   `json:"event_source,omitempty"`
	Symbol        string   `json:"symbol"`
	Name          string   `json:"name,omitempty"`
	Industry      string   `json:"industry,omitempty"`
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
	Industry                 string  `json:"industry,omitempty"`
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
	ExitReason               string  `json:"exit_reason,omitempty"`
	ExitTime                 string  `json:"exit_time,omitempty"`
}

type ShadowOpenPosition struct {
	SignalID                string              `json:"signal_id,omitempty"`
	Symbol                  string              `json:"symbol"`
	Name                    string              `json:"name,omitempty"`
	Industry                string              `json:"industry,omitempty"`
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
	RiskExitPending         bool                `json:"risk_exit_pending,omitempty"`
	RiskExitReason          string              `json:"risk_exit_reason,omitempty"`
	// Monster preserves the radar state that justified the latest lot. Keeping
	// it on the position lets an experimental account resume consistently after
	// a process restart.
	Monster realtime.MonsterRadar `json:"monster,omitempty"`
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
	Industry          string   `json:"industry,omitempty"`
	SignalDate        string   `json:"signal_date,omitempty"`
	SignalClose       float64  `json:"signal_close,omitempty"`
	SignalScore       float64  `json:"signal_score,omitempty"`
	TriggerPrice      float64  `json:"trigger_price,omitempty"`
	InvalidationPrice float64  `json:"invalidation_price,omitempty"`
	SignalReasons     []string `json:"signal_reasons,omitempty"`
	TargetExitDate    string   `json:"target_exit_date,omitempty"`
}

type PositionQuote struct {
	Symbol        string
	Price         float64
	PreviousClose float64
	Open          float64
	High          float64
	Low           float64
	AveragePrice  float64
	Volume        float64
	LimitUp       float64
	LimitDown     float64
	Amount        float64
	QuoteTime     string
	Source        string
}

type Report struct {
	EngineVersion            string                   `json:"engine_version,omitempty"`
	ConfigFingerprint        string                   `json:"config_fingerprint,omitempty"`
	SignalLimit              int                      `json:"signal_limit,omitempty"`
	CheckpointPhase          string                   `json:"checkpoint_phase,omitempty"`
	ExecutionMode            string                   `json:"execution_mode,omitempty"`
	LastRealtimeAt           string                   `json:"last_realtime_at,omitempty"`
	RealtimeEvents           int                      `json:"realtime_events,omitempty"`
	GeneratedAt              time.Time                `json:"generated_at"`
	ValuedAt                 *time.Time               `json:"valued_at,omitempty"`
	AsOf                     string                   `json:"as_of"`
	Config                   Config                   `json:"config"`
	SignalCount              int                      `json:"signal_count"`
	CandidateCount           int                      `json:"candidate_count"`
	PendingCandidates        int                      `json:"pending_candidates"`
	FilledEntries            int                      `json:"filled_entries"`
	CompletedTrades          int                      `json:"completed_trades"`
	RejectedOrders           int                      `json:"rejected_orders"`
	OpenPositions            int                      `json:"open_positions"`
	InitialCash              float64                  `json:"initial_cash"`
	RemainingCash            float64                  `json:"remaining_cash"`
	TotalTurnover            float64                  `json:"total_turnover"`
	TotalFees                float64                  `json:"total_fees"`
	TotalMarketValue         float64                  `json:"total_market_value"`
	TotalEquity              float64                  `json:"total_equity"`
	RealizedProfit           float64                  `json:"realized_profit"`
	UnrealizedProfit         float64                  `json:"unrealized_profit"`
	TotalProfit              float64                  `json:"total_profit"`
	TotalReturnPercent       float64                  `json:"total_return_percent"`
	TheoreticalAverageReturn float64                  `json:"theoretical_average_return_percent"`
	ExecutableAverageReturn  float64                  `json:"executable_average_return_percent"`
	ExecutionGapPercent      float64                  `json:"execution_gap_percent"`
	Trades                   []ShadowTrade            `json:"trades,omitempty"`
	Positions                []ShadowOpenPosition     `json:"positions,omitempty"`
	Orders                   []ShadowOrder            `json:"orders,omitempty"`
	Rejections               []ShadowRejection        `json:"rejections,omitempty"`
	Warnings                 []string                 `json:"warnings,omitempty"`
	Decisions                []ShadowDecision         `json:"decisions,omitempty"`
	IndustryExposures        []ShadowIndustryExposure `json:"industry_exposures,omitempty"`
	DailyReview              *ShadowDailyReview       `json:"daily_review,omitempty"`
	CalibrationID            string                   `json:"calibration_id,omitempty"`
	CalibrationReadySamples  int                      `json:"calibration_ready_samples,omitempty"`
	CalibrationMinimumScore  float64                  `json:"calibration_minimum_score,omitempty"`
}

type ShadowReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type ShadowDailyReview struct {
	Date               string              `json:"date"`
	MonitoringEvents   int                 `json:"monitoring_events"`
	DecisionCount      int                 `json:"decision_count"`
	OrderCount         int                 `json:"order_count"`
	OpenCount          int                 `json:"open_count"`
	AddCount           int                 `json:"add_count"`
	ReduceCount        int                 `json:"reduce_count"`
	TReduceCount       int                 `json:"t_reduce_count"`
	TRebuyCount        int                 `json:"t_rebuy_count"`
	RiskExitCount      int                 `json:"risk_exit_count"`
	ExitCount          int                 `json:"exit_count"`
	HoldCount          int                 `json:"hold_count"`
	WaitCount          int                 `json:"wait_count"`
	RejectedCount      int                 `json:"rejected_count"`
	PendingTQuantity   int                 `json:"pending_t_quantity"`
	Turnover           float64             `json:"turnover"`
	Fees               float64             `json:"fees"`
	RealizedProfit     float64             `json:"realized_profit"`
	TNetCashBenefit    float64             `json:"t_net_cash_benefit"`
	TopBlockers        []ShadowReasonCount `json:"top_blockers,omitempty"`
	CalibrationStatus  string              `json:"calibration_status"`
	CalibrationSummary string              `json:"calibration_summary"`
}

type ShadowIndustryExposure struct {
	Industry        string   `json:"industry"`
	InvestedCost    float64  `json:"invested_cost"`
	MarketValue     float64  `json:"market_value"`
	ExposurePercent float64  `json:"exposure_percent"`
	LimitPercent    float64  `json:"limit_percent"`
	Symbols         []string `json:"symbols,omitempty"`
}

type ShadowDecision struct {
	ID                    string   `json:"id"`
	EventID               string   `json:"event_id,omitempty"`
	EventTime             string   `json:"event_time,omitempty"`
	EventSource           string   `json:"event_source,omitempty"`
	Symbol                string   `json:"symbol"`
	Name                  string   `json:"name,omitempty"`
	Industry              string   `json:"industry,omitempty"`
	Date                  string   `json:"date"`
	Action                string   `json:"action"`
	Reason                string   `json:"reason"`
	SignalScore           float64  `json:"signal_score,omitempty"`
	SignalReasons         []string `json:"signal_reasons,omitempty"`
	CurrentQuantity       int      `json:"current_quantity,omitempty"`
	TargetPositionPercent float64  `json:"target_position_percent,omitempty"`
}

const (
	ShadowEngineVersion = "tplus1-v11"
	CheckpointOpen      = "open"
	CheckpointClose     = "close"
	ExecutionModeDaily  = "daily-checkpoint"
	ExecutionModeLive   = "intraday-event"
)

const shadowCalendarCacheTTL = 2 * time.Minute

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
	calendarCoversDate := len(calendarDates) > 0 && today >= calendarDates[0] && today <= calendarDates[len(calendarDates)-1]
	if len(calendarDates) > 0 && !calendarCoversDate {
		// A finite history window is not an authority about future holidays.
		// Outside that window, fall back to the exchange weekday clock while
		// still using the last known close for weekends and pre-open times.
		return fallbackTradingCheckpoint(local, calendarDates)
	}
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

func fallbackTradingCheckpoint(local time.Time, calendarDates []string) Checkpoint {
	today := local.Format("2006-01-02")
	marketOpen := time.Date(local.Year(), local.Month(), local.Day(), 9, 30, 0, 0, shanghaiLocation)
	closeSettled := time.Date(local.Year(), local.Month(), local.Day(), 15, 5, 0, 0, shanghaiLocation)
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday || local.Before(marketOpen) {
		for index := len(calendarDates) - 1; index >= 0; index-- {
			if calendarDates[index] < today {
				return Checkpoint{Date: calendarDates[index], Phase: CheckpointClose}
			}
		}
		return Checkpoint{Date: today, Phase: CheckpointClose}
	}
	phase := CheckpointOpen
	if !local.Before(closeSettled) {
		phase = CheckpointClose
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
	nextOrders, nextTrades, nextRejections, err := settledTransitionEvents(previous, next)
	if err != nil {
		return err
	}
	if previous.EngineVersion == ShadowEngineVersion {
		metrics := ledgerMetricsForEvents(next, nextOrders, nextTrades, nextRejections)
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
		// Older v10 snapshots could retain a zero-quantity shell after the last
		// lot was sold. It is not an open position and must not be fed back into
		// the active map, otherwise the next realtime signal is misclassified as
		// an add-on and the continuity guard correctly rejects the rewrite.
		if position.Quantity <= 0 && !hasPositivePositionLot(position) {
			continue
		}
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

// settledTransitionEvents keeps the old exact-prefix behavior for daily
// reports, while allowing a live report to append events after its quote
// watermark on the same trading date.
func settledTransitionEvents(previous, next Report) ([]ShadowOrder, []ShadowTrade, []ShadowRejection, error) {
	previousOrders := ordersThroughCheckpoint(previous.Orders, previous.AsOf, previous.CheckpointPhase)
	previousTrades := tradesThroughCheckpoint(previous.Trades, previous.AsOf, previous.CheckpointPhase)
	previousRejections := rejectionsThroughCheckpoint(previous.Rejections, previous.AsOf, previous.CheckpointPhase)
	nextOrdersAll := ordersThroughCheckpoint(next.Orders, previous.AsOf, previous.CheckpointPhase)
	nextTradesAll := tradesThroughCheckpoint(next.Trades, previous.AsOf, previous.CheckpointPhase)
	nextRejectionsAll := rejectionsThroughCheckpoint(next.Rejections, previous.AsOf, previous.CheckpointPhase)
	live := previous.LastRealtimeAt != "" || next.LastRealtimeAt != "" || previous.ExecutionMode == ExecutionModeLive || next.ExecutionMode == ExecutionModeLive
	if !live {
		if err := compareOrderPrefix(previousOrders, nextOrdersAll, previous.AsOf); err != nil {
			return nil, nil, nil, err
		}
		if err := compareTradePrefix(previousTrades, nextTradesAll, previous.AsOf); err != nil {
			return nil, nil, nil, err
		}
		if err := compareRejectionPrefix(previousRejections, nextRejectionsAll, previous.AsOf); err != nil {
			return nil, nil, nil, err
		}
		return nextOrdersAll, nextTradesAll, nextRejectionsAll, nil
	}
	watermark := reportEventWatermark(previous)
	nextOrders := liveOrdersAtOrBefore(nextOrdersAll, previous.AsOf, previous.CheckpointPhase, watermark)
	nextTrades := liveTradesAtOrBefore(nextTradesAll, previous.AsOf, previous.CheckpointPhase, watermark)
	nextRejections := liveRejectionsAtOrBefore(nextRejectionsAll, previous.AsOf, previous.CheckpointPhase, watermark)
	if err := compareOrderPrefix(previousOrders, nextOrders, previous.AsOf); err != nil {
		return nil, nil, nil, err
	}
	if err := compareTradePrefix(previousTrades, nextTrades, previous.AsOf); err != nil {
		return nil, nil, nil, err
	}
	if err := compareRejectionPrefix(previousRejections, nextRejections, previous.AsOf); err != nil {
		return nil, nil, nil, err
	}
	if err := validateNewLiveOrders(nextOrdersAll, previousOrders, previous.AsOf, watermark); err != nil {
		return nil, nil, nil, err
	}
	if err := validateNewLiveTrades(nextTradesAll, previousTrades, previous.AsOf, watermark); err != nil {
		return nil, nil, nil, err
	}
	if err := validateNewLiveRejections(nextRejectionsAll, previousRejections, previous.AsOf, watermark); err != nil {
		return nil, nil, nil, err
	}
	return nextOrders, nextTrades, nextRejections, nil
}

func previousOrdersForReport(report Report) []ShadowOrder {
	return ordersThroughCheckpoint(report.Orders, report.AsOf, report.CheckpointPhase)
}

func findOrderByID(items []ShadowOrder, id string) (ShadowOrder, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return ShadowOrder{}, false
}

func findTradeByID(items []ShadowTrade, id string) (ShadowTrade, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return ShadowTrade{}, false
}

func findRejectionByID(items []ShadowRejection, id string) (ShadowRejection, bool) {
	for _, item := range items {
		if item.OrderID == id {
			return item, true
		}
	}
	return ShadowRejection{}, false
}

func findTradeInSlice(items []ShadowTrade, id string) bool {
	_, found := findTradeByID(items, id)
	return found
}

func findRejectionInSlice(items []ShadowRejection, id string) bool {
	_, found := findRejectionByID(items, id)
	return found
}

func compareOrderPrefix(previous, next []ShadowOrder, date string) error {
	if len(previous) != len(next) {
		return fmt.Errorf("截至 %s 的成交单数量从 %d 变为 %d", date, len(previous), len(next))
	}
	for _, old := range previous {
		current, found := findOrderByID(next, old.ID)
		if !found || !sameSettledOrder(old, current) {
			return fmt.Errorf("截至 %s 的成交单被改写: %s", date, old.ID)
		}
	}
	return nil
}

func compareTradePrefix(previous, next []ShadowTrade, date string) error {
	if len(previous) != len(next) {
		return fmt.Errorf("截至 %s 的完成交易数量从 %d 变为 %d", date, len(previous), len(next))
	}
	for _, old := range previous {
		current, found := findTradeByID(next, old.ID)
		if !found || !sameSettledTrade(old, current) {
			return fmt.Errorf("截至 %s 的完成交易被改写: %s", date, old.ID)
		}
	}
	return nil
}

func compareRejectionPrefix(previous, next []ShadowRejection, date string) error {
	if len(previous) != len(next) {
		return fmt.Errorf("截至 %s 的拒绝记录数量从 %d 变为 %d", date, len(previous), len(next))
	}
	for _, old := range previous {
		current, found := findRejectionByID(next, old.OrderID)
		if !found || !sameRejection(old, current) {
			return fmt.Errorf("截至 %s 的拒绝记录被改写: %s", date, old.OrderID)
		}
	}
	return nil
}

func liveOrdersAtOrBefore(orders []ShadowOrder, date, phase, watermark string) []ShadowOrder {
	items := ordersThroughCheckpoint(orders, date, phase)
	if watermark == "" {
		return items
	}
	result := make([]ShadowOrder, 0, len(items))
	for _, item := range items {
		if item.AttemptDate < date || item.AttemptDate == date && (item.ExecutionTime == "" || item.ExecutionTime <= watermark) {
			result = append(result, item)
		}
	}
	return result
}

func liveTradesAtOrBefore(trades []ShadowTrade, date, phase, watermark string) []ShadowTrade {
	items := tradesThroughCheckpoint(trades, date, phase)
	if watermark == "" {
		return items
	}
	result := make([]ShadowTrade, 0, len(items))
	for _, item := range items {
		if item.ExitDate < date || item.ExitDate == date && (item.ExitTime == "" || item.ExitTime <= watermark) {
			result = append(result, item)
		}
	}
	return result
}

func liveRejectionsAtOrBefore(rejections []ShadowRejection, date, phase, watermark string) []ShadowRejection {
	items := rejectionsThroughCheckpoint(rejections, date, phase)
	if watermark == "" {
		return items
	}
	result := make([]ShadowRejection, 0, len(items))
	for _, item := range items {
		itemDate := item.AttemptDate
		if itemDate == "" {
			itemDate = item.SignalDate
		}
		if itemDate < date || itemDate == date && (item.EventTime == "" || item.EventTime <= watermark) {
			result = append(result, item)
		}
	}
	return result
}

func validateNewLiveOrders(items, previous []ShadowOrder, date, watermark string) error {
	known := make(map[string]bool, len(previous))
	for _, item := range previous {
		known[item.ID] = true
	}
	for _, item := range items {
		if known[item.ID] {
			continue
		}
		if item.AttemptDate < date || item.AttemptDate == date && (watermark == "" || item.ExecutionTime == "" || item.ExecutionTime <= watermark) {
			return fmt.Errorf("发现早于实时水位的新增成交单: %s", item.ID)
		}
	}
	return nil
}

func validateNewLiveTrades(items, previous []ShadowTrade, date, watermark string) error {
	known := make(map[string]bool, len(previous))
	for _, item := range previous {
		known[item.ID] = true
	}
	for _, item := range items {
		if known[item.ID] {
			continue
		}
		if item.ExitDate < date || item.ExitDate == date && (watermark == "" || item.ExitTime == "" || item.ExitTime <= watermark) {
			return fmt.Errorf("发现早于实时水位的新增完成交易: %s", item.ID)
		}
	}
	return nil
}

func validateNewLiveRejections(items, previous []ShadowRejection, date, watermark string) error {
	known := make(map[string]bool, len(previous))
	for _, item := range previous {
		known[item.OrderID] = true
	}
	for _, item := range items {
		if known[item.OrderID] {
			continue
		}
		itemDate := item.AttemptDate
		if itemDate == "" {
			itemDate = item.SignalDate
		}
		if itemDate < date || itemDate == date && (watermark == "" || item.EventTime == "" || item.EventTime <= watermark) {
			return fmt.Errorf("发现早于实时水位的新增拒绝记录: %s", item.OrderID)
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
	if order.Side == "buy" || order.PositionAction == "reduce" || order.PositionAction == "risk_exit" {
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
	if trade.PositionAction == "reduce" || trade.PositionAction == "risk_exit" {
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

// The daily checkpoint is intentionally coarse, but a live report can contain
// several events on one date. Use its last quote timestamp as a monotonic
// watermark so a later sync appends valid events instead of looking like a
// retroactive rewrite.
func ordersThroughWatermark(orders []ShadowOrder, previous Report) []ShadowOrder {
	items := ordersThroughCheckpoint(orders, previous.AsOf, previous.CheckpointPhase)
	return filterOrdersByWatermark(items, previous.AsOf, reportEventWatermark(previous))
}

func tradesThroughWatermark(trades []ShadowTrade, previous Report) []ShadowTrade {
	items := tradesThroughCheckpoint(trades, previous.AsOf, previous.CheckpointPhase)
	watermark := reportEventWatermark(previous)
	if watermark == "" {
		return items
	}
	result := make([]ShadowTrade, 0, len(items))
	for _, trade := range items {
		if trade.ExitDate != previous.AsOf || trade.ExitTime == "" || trade.ExitTime <= watermark {
			result = append(result, trade)
		}
	}
	return result
}

func rejectionsThroughWatermark(rejections []ShadowRejection, previous Report) []ShadowRejection {
	items := rejectionsThroughCheckpoint(rejections, previous.AsOf, previous.CheckpointPhase)
	watermark := reportEventWatermark(previous)
	if watermark == "" {
		return items
	}
	result := make([]ShadowRejection, 0, len(items))
	for _, rejection := range items {
		if rejection.AttemptDate != previous.AsOf || rejection.EventTime == "" || rejection.EventTime <= watermark {
			result = append(result, rejection)
		}
	}
	return result
}

func filterOrdersByWatermark(orders []ShadowOrder, date, watermark string) []ShadowOrder {
	if watermark == "" {
		return orders
	}
	result := make([]ShadowOrder, 0, len(orders))
	for _, order := range orders {
		if order.AttemptDate != date || order.ExecutionTime == "" || order.ExecutionTime <= watermark {
			result = append(result, order)
		}
	}
	return result
}

func reportEventWatermark(report Report) string {
	if report.LastRealtimeAt != "" {
		return report.LastRealtimeAt
	}
	if report.AsOf == "" || report.CheckpointPhase != CheckpointOpen || report.GeneratedAt.IsZero() {
		return ""
	}
	local := report.GeneratedAt.In(shanghaiLocation)
	if local.Format("2006-01-02") != report.AsOf {
		return ""
	}
	return local.Format("2006-01-02 15:04:05")
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
	return ShadowRejection{OrderID: orderID(signal, side), Symbol: signal.Symbol, Name: signal.Name, Industry: signal.Industry, Side: side, SignalDate: signalDate(signal), AttemptDate: attemptDate, Reason: reason, SignalScore: signal.Score, SignalReasons: append([]string(nil), signal.Reasons...)}
}

func makeRejectionWithID(signal realtime.Signal, side, attemptDate, reason, id string) ShadowRejection {
	rejection := makeRejection(signal, side, attemptDate, reason)
	if strings.TrimSpace(id) != "" {
		rejection.OrderID = id
	}
	return rejection
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

func hasPositivePositionLot(position ShadowOpenPosition) bool {
	for _, lot := range position.Lots {
		if lot.Quantity > 0 {
			return true
		}
	}
	return false
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
		entry := ShadowOrder{ID: position.SignalID + "-buy", Symbol: position.Symbol, Name: position.Name, Industry: position.Industry, Side: "buy", SignalDate: position.SignalDate, AttemptDate: position.EntryDate, Quantity: position.Quantity, Price: position.EntryPrice, RawPrice: position.EntryPrice, Amount: position.EntryAmount, Status: OrderFilled, ExecutionTime: position.EntryTime, SignalScore: position.SignalScore, TriggerPrice: position.TriggerPrice, InvalidationPrice: position.InvalidationPrice, SignalReasons: append([]string(nil), position.SignalReasons...)}
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
		entry := ShadowOrder{ID: lot.OrderID, Symbol: position.Symbol, Name: position.Name, Industry: lot.Industry, Side: "buy", SignalDate: lot.SignalDate, AttemptDate: lot.EntryDate, Quantity: lot.Quantity, Price: lot.EntryPrice, RawPrice: lot.EntryPrice, Amount: lot.EntryAmount, Status: OrderFilled, ExecutionTime: lot.EntryTime, SignalScore: lot.SignalScore, TriggerPrice: lot.TriggerPrice, InvalidationPrice: lot.InvalidationPrice, SignalReasons: append([]string(nil), lot.SignalReasons...)}
		if entry.Industry == "" {
			entry.Industry = position.Industry
		}
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
		if order.Symbol != position.Symbol || order.Status != OrderFilled || !eventAfterReportCheckpoint(order.AttemptDate, order.ExecutionTime, order.PositionAction, previous) {
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
		if trade.Symbol == position.Symbol && trade.EntryOrderID != "" && eventAfterReportCheckpoint(trade.ExitDate, trade.ExitTime, trade.PositionAction, previous) {
			closedLots[trade.EntryOrderID] += trade.Quantity
		}
	}
	for _, order := range next.Orders {
		if order.Symbol == position.Symbol && order.Side == "sell" && order.Status == OrderFilled && eventAfterReportCheckpoint(order.AttemptDate, order.ExecutionTime, order.PositionAction, previous) {
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
			eventAfterReportCheckpoint(order.AttemptDate, order.ExecutionTime, order.PositionAction, previous) && order.Quantity == lot.Quantity && sameFloat(order.Price, lot.EntryPrice) {
			return true
		}
	}
	return false
}

func eventAfterReportCheckpoint(date, eventTime, action string, previous Report) bool {
	if date > previous.AsOf {
		return true
	}
	if date < previous.AsOf {
		return false
	}
	if watermark := reportEventWatermark(previous); watermark != "" && eventTime != "" {
		return eventTime > watermark
	}
	return previous.CheckpointPhase == CheckpointOpen && (action == "exit" || action == "risk_exit" || action == "reduce")
}

func realtimeEventAfterReport(eventTime string, previous Report) bool {
	if eventTime == "" {
		return true
	}
	if previous.LastRealtimeAt == "" {
		return true
	}
	return eventTime > previous.LastRealtimeAt
}

func sameFloat(left, right float64) bool {
	return math.Abs(left-right) <= 1e-8*math.Max(1, math.Max(math.Abs(left), math.Abs(right)))
}

func clamp(value, minimum, maximum float64) float64 {
	return math.Max(minimum, math.Min(value, maximum))
}

type Evaluator struct {
	history           HistoryClient
	now               func() time.Time
	calendarMu        sync.Mutex
	calendarDates     []string
	calendarFetchedAt time.Time
}

func NewEvaluator(history HistoryClient) *Evaluator {
	return &Evaluator{history: history, now: time.Now}
}

// tradingCalendar returns a short-lived, immutable copy of the benchmark
// trading dates. The same evaluator is shared by the three shadow profiles;
// caching here keeps their checkpoint and T+1 decisions on one calendar read.
// Errors are deliberately not cached so a transient provider failure can
// recover on the next attempt.
func (e *Evaluator) tradingCalendar(ctx context.Context, now time.Time, override []string) ([]string, error) {
	if len(override) > 0 {
		return append([]string(nil), override...), nil
	}
	if e == nil || e.history == nil {
		return nil, fmt.Errorf("交易日日历服务未初始化")
	}
	e.calendarMu.Lock()
	if len(e.calendarDates) > 0 && !e.calendarFetchedAt.IsZero() && now.Sub(e.calendarFetchedAt) >= 0 && now.Sub(e.calendarFetchedAt) < shadowCalendarCacheTTL {
		dates := append([]string(nil), e.calendarDates...)
		e.calendarMu.Unlock()
		return dates, nil
	}
	e.calendarMu.Unlock()

	bars, err := e.history.FetchDailyBars(ctx, "sh000300")
	if err != nil {
		return nil, err
	}
	dates := barDates(normalizedBars(bars))
	if len(dates) == 0 {
		return nil, fmt.Errorf("沪深300未返回有效交易日")
	}
	e.calendarMu.Lock()
	e.calendarDates = append([]string(nil), dates...)
	e.calendarFetchedAt = now
	e.calendarMu.Unlock()
	return dates, nil
}

type shadowPlan struct {
	signal         realtime.Signal
	bars           []domain.DailyBar
	action         string
	lotOrderID     string
	orderID        string
	exitReason     string
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
	targetPercent   float64
	riskExitReason  string
}

type shadowLot struct {
	plan      shadowPlan
	entry     ShadowOrder
	entryCost float64
}

type shadowRotationCandidate struct {
	symbol   string
	position shadowPosition
	bar      domain.DailyBar
	barIndex int
	score    float64
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
	currentNow := now()
	calendarDates, calendarError := e.tradingCalendar(ctx, currentNow, options.CalendarDates)
	if calendarError != nil {
		calendarDates = nil
	}
	checkpoint := TradingCheckpointAt(currentNow, calendarDates)
	if calendarError != nil || len(calendarDates) == 0 {
		previous.Warnings = append(previous.Warnings, "沪深300交易日历不可用，停牌识别将退化为个股交易日")
	}
	report := cloneReport(previous)
	report.EngineVersion = ShadowEngineVersion
	report.Config = cfg
	report.ConfigFingerprint = OptionsFingerprint(cfg, options.Limit)
	report.SignalLimit = options.Limit
	report.GeneratedAt = currentNow
	report.AsOf = checkpoint.Date
	report.CheckpointPhase = checkpoint.Phase
	report.ExecutionMode = ExecutionModeDaily
	report.SignalCount = len(signals)
	if report.AsOf < previous.AsOf {
		return previous, fmt.Errorf("影子账户检查点倒退: %s -> %s", previous.AsOf, report.AsOf)
	}
	active := make(map[string]shadowPosition, len(previous.Positions))
	archivedSignals := representativeSignalsForConfig(signals, 0, cfg)
	for index := range report.Rejections {
		if report.Rejections[index].SignalScore > 0 && len(report.Rejections[index].SignalReasons) > 0 && report.Rejections[index].Industry != "" {
			continue
		}
		if signal, found := matchingArchivedRejectionSignal(archivedSignals, report.Rejections[index]); found {
			if report.Rejections[index].SignalScore <= 0 {
				report.Rejections[index].SignalScore = signal.Score
			}
			if len(report.Rejections[index].SignalReasons) == 0 {
				report.Rejections[index].SignalReasons = append([]string(nil), signal.Reasons...)
			}
			if report.Rejections[index].Industry == "" {
				report.Rejections[index].Industry = signal.Industry
			}
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
		signal := realtime.Signal{ID: position.SignalID, Symbol: position.Symbol, Name: position.Name, Industry: position.Industry, Price: position.SignalClose, Score: position.SignalScore, TriggerPrice: position.TriggerPrice, InvalidationPrice: position.InvalidationPrice, Reasons: append([]string(nil), position.SignalReasons...), Monster: position.Monster}
		if enriched, found := matchingArchivedSignal(archivedSignals, position); found {
			if signal.ID == "" {
				signal.ID = enriched.ID
			}
			if signal.Name == "" {
				signal.Name = enriched.Name
			}
			if signal.Industry == "" {
				signal.Industry = enriched.Industry
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
			if enriched.Monster.Stage != "" {
				signal.Monster = enriched.Monster
			}
			// Preserve the challenger metadata when an adaptive account carries
			// an existing position into the next daily checkpoint. Without this,
			// the position would silently fall back to the champion score after
			// the first restart or close-to-open transition.
			if enriched.CalibrationID != "" {
				signal.CalibrationID = enriched.CalibrationID
				signal.CalibrationReadySamples = enriched.CalibrationReadySamples
				signal.CalibrationMinimumScore = enriched.CalibrationMinimumScore
				signal.CalibratedScore = enriched.CalibratedScore
				signal.CalibratedRiskAdjustedScore = enriched.CalibratedRiskAdjustedScore
				signal.CalibratedState = enriched.CalibratedState
				signal.CalibratedRank = enriched.CalibratedRank
				signal.CalibratedTotal = enriched.CalibratedTotal
				signal.CalibratedPortfolioEligible = enriched.CalibratedPortfolioEligible
				signal.CalibratedPortfolioReason = enriched.CalibratedPortfolioReason
			}
			if enriched.RiskMultiplier > 0 {
				signal.RiskMultiplier = enriched.RiskMultiplier
				signal.RiskAdjustedScore = enriched.RiskAdjustedScore
				signal.MarketRegime = enriched.MarketRegime
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
			lotSignal.Industry = lots[index].entry.Industry
			if lotSignal.Industry == "" {
				lotSignal.Industry = signal.Industry
				lots[index].entry.Industry = signal.Industry
			}
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
				if report.Orders[orderIndex].Industry == "" {
					report.Orders[orderIndex].Industry = lots[index].entry.Industry
				}
			}
		}
		targetPercent := position.TargetPositionPercent
		if targetPercent <= 0 {
			targetPercent = cfg.MaxPositionPercent
		}
		active[position.Symbol] = shadowPosition{plan: shadowPlan{signal: signal, bars: bars, signalClose: position.SignalClose}, lots: lots, fallback: nil, lastSignalDate: position.SignalDate, lastSignalScore: position.SignalScore, targetPercent: targetPercent, riskExitReason: position.RiskExitReason}
	}
	// An open checkpoint has already crossed the day's entry boundary. Replaying
	// signals from that same date would duplicate settled orders when the next
	// scheduler cycle advances the account. Same-day events belong to the
	// realtime layer; daily incrementals start at the following trading date.
	afterDate := previous.AsOf
	if previous.CheckpointPhase == CheckpointOpen {
		// Do not depend on the benchmark calendar containing future dates. Data
		// providers often lag by one or more sessions; a date immediately after
		// the persisted checkpoint is still sufficient because signal dates are
		// exchange trading dates and non-trading dates simply have no signals.
		if parsed, parseErr := time.ParseInLocation("2006-01-02", previous.AsOf, shanghaiLocation); parseErr == nil {
			afterDate = parsed.AddDate(0, 0, 1).Format("2006-01-02")
		} else {
			for _, candidateDate := range calendarDates {
				if candidateDate > previous.AsOf {
					afterDate = candidateDate
					break
				}
			}
		}
	}
	newPlans, err := e.plansAfter(ctx, signals, cfg, afterDate, checkpoint, calendarDates, active)
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
	report = simulateFromCheckpoint(report, newPlans, cfg, report.RemainingCash, active, previous.AsOf)
	if options.Realtime && checkpoint.Phase == CheckpointOpen && checkpoint.Date == localTradingDate(currentNow) {
		var realtimeErr error
		report, realtimeErr = e.advanceRealtime(ctx, report, signals, options, calendarDates, currentNow)
		if realtimeErr != nil {
			// A quote or minute-data outage must not discard the settled daily
			// ledger. Preserve the report and expose the degradation to the UI.
			report.Warnings = appendUniqueWarning(report.Warnings, "盘中影子层未推进: "+realtimeErr.Error())
		}
	}
	return report, nil
}

// AdvanceRealtime processes one current-session quote snapshot without
// rebuilding the daily ledger. The Web scheduler uses this path after the
// account has reached today's open checkpoint, so every fresh scan can update
// risk, position and execution decisions without replaying all archived
// signals and historical bars.
func (e *Evaluator) AdvanceRealtime(ctx context.Context, previous Report, signals []realtime.Signal, options Options) (Report, error) {
	if e == nil || e.history == nil {
		return Report{}, fmt.Errorf("影子执行评估器未初始化")
	}
	if !options.Realtime {
		return previous, fmt.Errorf("盘中快速推进未启用实时行情")
	}
	cfg := canonicalConfig(options.Config)
	currentNow := options.RealtimeAt
	if currentNow.IsZero() {
		now := e.now
		if options.Now != nil {
			now = options.Now
		}
		if now == nil {
			now = time.Now
		}
		currentNow = now()
	}
	calendarDates, calendarError := e.tradingCalendar(ctx, currentNow, options.CalendarDates)
	if calendarError != nil {
		calendarDates = nil
	}
	checkpoint := TradingCheckpointAt(currentNow, calendarDates)
	if previous.EngineVersion != ShadowEngineVersion || previous.AsOf != checkpoint.Date || previous.CheckpointPhase != CheckpointOpen || checkpoint.Phase != CheckpointOpen {
		return previous, fmt.Errorf("影子账户尚未到达当前交易日开盘检查点")
	}
	if previous.ConfigFingerprint != "" && previous.ConfigFingerprint != OptionsFingerprint(cfg, options.Limit) {
		return previous, fmt.Errorf("影子账户配置已变化，需要先完成日线检查点推进")
	}

	report := cloneReport(previous)
	report.EngineVersion = ShadowEngineVersion
	report.Config = cfg
	report.ConfigFingerprint = OptionsFingerprint(cfg, options.Limit)
	report.SignalLimit = options.Limit
	// `signals` is the current scan snapshot on this fast path, not the full
	// archived signal set used to build the account. Preserve the cumulative
	// report count so a 30-second intraday update cannot make history appear to
	// disappear in the UI.
	if report.SignalCount == 0 {
		report.SignalCount = len(signals)
	}
	applyReportCalibrationMetadata(&report, signals)
	if calendarError != nil || len(calendarDates) == 0 {
		report.Warnings = appendUniqueWarning(report.Warnings, "沪深300交易日历不可用，停牌识别将退化为个股交易日")
	}
	return e.advanceRealtime(ctx, report, signals, options, calendarDates, currentNow)
}

func (e *Evaluator) plansAfter(ctx context.Context, signals []realtime.Signal, cfg Config, after string, checkpoint Checkpoint, calendarDates []string, active map[string]shadowPosition) ([]shadowPlan, error) {
	selected := representativeSignalsForConfig(signals, 0, cfg)
	bySymbol := make(map[string][]realtime.Signal)
	for _, signal := range selected {
		date := signalDate(signal)
		if date < after || date >= checkpoint.Date || !shadowSignalActionableForConfig(signal, cfg) {
			continue
		}
		if !shadowSignalEntryEligible(signal, cfg) {
			if _, held := active[signal.Symbol]; !held {
				continue
			}
		}
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
			if !shadowSignalActionableForConfig(signal, cfg) {
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
			return shadowSignalScoreForConfig(plans[i].signal, cfg) > shadowSignalScoreForConfig(plans[j].signal, cfg)
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

func positionRiskExitReason(position shadowPosition, closePrice float64, cfg Config) string {
	if closePrice <= 0 || len(position.lots) == 0 {
		return ""
	}
	invalidation := 0.0
	for _, lot := range position.lots {
		candidate := lot.entry.InvalidationPrice
		if candidate <= 0 {
			candidate = lot.plan.signal.InvalidationPrice
		}
		if candidate > invalidation {
			invalidation = candidate
		}
	}
	if invalidation <= 0 {
		invalidation = position.plan.signal.InvalidationPrice
	}
	if invalidation > 0 && closePrice < invalidation {
		return fmt.Sprintf("前一交易日收盘 %.2f 跌破失效位 %.2f，按风险层退出全部可卖批次", closePrice, invalidation)
	}
	cost := positionCost(position)
	quantity := positionQuantity(position)
	if cost <= 0 || quantity <= 0 {
		return ""
	}
	returnPercent := (closePrice*float64(quantity)/cost - 1) * 100
	if returnPercent <= -cfg.MaxLossPercent {
		return fmt.Sprintf("前一交易日收盘亏损 %.2f%% 达到单股最大亏损 %.2f%%，按风险层退出全部可卖批次", returnPercent, cfg.MaxLossPercent)
	}
	return ""
}

func riskCooldownActive(trades []ShadowTrade, symbol string, bars []domain.DailyBar, entryDate string, cooldownDays int) (string, bool) {
	if cooldownDays <= 0 {
		return "", false
	}
	latestExit := ""
	for _, trade := range trades {
		if trade.Symbol == symbol && trade.PositionAction == "risk_exit" && trade.ExitDate > latestExit {
			latestExit = trade.ExitDate
		}
	}
	if latestExit == "" {
		return "", false
	}
	exitIndex := barIndex(bars, latestExit)
	entryIndex := barIndex(bars, entryDate)
	if exitIndex < 0 || entryIndex < 0 {
		return latestExit, entryDate <= latestExit
	}
	return latestExit, entryIndex-exitIndex <= cooldownDays
}

func simulateFrom(report Report, plans []shadowPlan, cfg Config, cash float64, active map[string]shadowPosition) Report {
	return simulateFromCheckpoint(report, plans, cfg, cash, active, "")
}

func simulateFromCheckpoint(report Report, plans []shadowPlan, cfg Config, cash float64, active map[string]shadowPosition, afterDate string) Report {
	if !finite(cash) {
		cash = report.RemainingCash
	}
	if executionLedgerEmpty(report) && cash <= 0 {
		cash = cfg.InitialCash
	}
	events := make(map[string][]int)
	dateSet := make(map[string]struct{})
	for index, plan := range plans {
		if plan.entryDate != "" {
			events[plan.entryDate] = append(events[plan.entryDate], index)
			dateSet[plan.entryDate] = struct{}{}
		}
		if plan.exitDate != "" && plan.exitDate != plan.entryDate {
			events[plan.exitDate] = append(events[plan.exitDate], index)
			dateSet[plan.exitDate] = struct{}{}
		}
		for _, bar := range plan.bars {
			startDate := plan.entryDate
			if afterDate != "" {
				startDate = afterDate
			}
			if bar.Date != "" && bar.Date > startDate && bar.Date <= report.AsOf {
				dateSet[bar.Date] = struct{}{}
			}
		}
	}
	for symbol, position := range active {
		if position.fallback != nil {
			continue
		}
		if positionQuantity(position) <= 0 {
			delete(active, symbol)
			continue
		}
		entryDate := ""
		for _, lot := range position.lots {
			if entryDate == "" || (lot.entry.AttemptDate != "" && lot.entry.AttemptDate < entryDate) {
				entryDate = lot.entry.AttemptDate
			}
		}
		for _, bar := range position.plan.bars {
			startDate := entryDate
			if afterDate != "" {
				startDate = afterDate
			}
			if bar.Date != "" && bar.Date > startDate && bar.Date <= report.AsOf {
				dateSet[bar.Date] = struct{}{}
			}
		}
	}
	dates := make([]string, 0, len(dateSet))
	for date := range dateSet {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	dailyDeployment := make(map[string]float64)
	dailyRotations := make(map[string]int)
	for _, date := range dates {
		indexes := events[date]
		handledOpening := make(map[int]bool, len(indexes))
		rotatedOutToday := make(map[string]bool)

		for symbol, position := range active {
			if afterDate != "" && (date < afterDate || date == afterDate) {
				continue
			}
			if position.fallback != nil || len(position.lots) == 0 {
				continue
			}
			barIndex := barIndex(position.plan.bars, date)
			if barIndex <= 0 {
				continue
			}
			previousBar := position.plan.bars[barIndex-1]
			reason := position.riskExitReason
			if reason == "" {
				reason = positionRiskExitReason(position, previousBar.Close, cfg)
			}
			if reason == "" {
				continue
			}
			position.riskExitReason = reason
			active[symbol] = position
			signal := position.plan.signal
			signal.ID = signal.ID + "-risk-" + date
			signal.AsOf = parseShanghaiDate(previousBar.Date)
			signal.Price = previousBar.Close
			plans = append(plans, shadowPlan{
				signal: signal, bars: position.plan.bars, action: "risk_exit", entryIndex: barIndex,
				entryDate: date, orderID: position.plan.signal.Symbol + "-risk-" + date + "-sell", exitReason: reason,
			})
			index := len(plans) - 1
			indexes = append(indexes, index)
			events[date] = append(events[date], index)
		}

		// Risk exits are generated only from the previous completed close and
		// execute at the next tradable open. They flatten every T+1-eligible lot
		// before any discretionary reduction or new entry is considered.
		for _, index := range indexes {
			plan := plans[index]
			if plan.action != "risk_exit" || plan.entryDate != date || plan.entryIndex < 0 || plan.entryIndex >= len(plan.bars) {
				continue
			}
			handledOpening[index] = true
			position, exists := active[plan.signal.Symbol]
			if !exists || position.fallback != nil {
				continue
			}
			bar := plan.bars[plan.entryIndex]
			if bar.Open <= 0 || onePriceBar(bar) || limitLockedAtExit(plan.signal, plan.bars, plan.entryIndex) {
				appendShadowDecision(&report, plan.signal, date, "wait", plan.exitReason+"；但开盘一字板或停牌，风险退出顺延", positionQuantity(position), 0)
				continue
			}
			remainingLots := make([]shadowLot, 0, len(position.lots))
			exited := 0
			for lotIndex, lot := range position.lots {
				if lot.entry.AttemptDate == "" || lot.entry.AttemptDate >= date {
					remainingLots = append(remainingLots, lot)
					continue
				}
				sell := makeOrder(plan.signal, "sell", date, bar.Open, lot.entry.Quantity, cfg, 0)
				sell.ID = fmt.Sprintf("%s-%d", plan.orderID, lotIndex+1)
				sell.Status = OrderFilled
				sell.Reason = plan.exitReason
				sell.ExecutionTime = simulatedOpeningExecutionTime(date)
				sell.PositionAction = "risk_exit"
				sell.PositionSequence = lotIndex + 1
				fee := transactionFee(sell.Amount, "sell", cfg)
				cash += sell.Amount - fee
				report.Orders = append(report.Orders, sell)
				report.Trades = append(report.Trades, tradeForLot(lot, sell, bar.Open, date, plan.entryIndex, fee, len(report.Trades)+1))
				report.CompletedTrades++
				report.TotalTurnover += sell.Amount
				report.TotalFees += fee
				exited += lot.entry.Quantity
			}
			position.lots = remainingLots
			if exited == 0 {
				appendShadowDecision(&report, plan.signal, date, "hold", plan.exitReason+"；但没有满足T+1的可卖批次", positionQuantity(position), 0)
				continue
			}
			appendShadowDecision(&report, plan.signal, date, "risk_exit", plan.exitReason, positionQuantity(position), 0)
			if len(position.lots) == 0 {
				delete(active, plan.signal.Symbol)
			} else {
				active[plan.signal.Symbol] = position
			}
		}

		// Opening reductions settle before opening entries. A-share sale proceeds
		// can fund another opening buy, while proceeds from the later close cannot.
		for _, index := range indexes {
			if handledOpening[index] {
				continue
			}
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
			planScore := shadowSignalScoreForConfig(plan.signal, cfg)
			if planScore > position.lastSignalScore-cfg.AdditionScoreStep {
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
			position.lastSignalScore = planScore
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
			if !exists && rotatedOutToday[plan.signal.Symbol] {
				appendShadowDecision(&report, plan.signal, date, "wait", "该股票当日已被组合轮出，不在同一开盘重新买回", 0, targetPositionPercent(plan.signal, cfg))
				continue
			}
			if exists && !shadowSignalEntryEligible(plan.signal, cfg) {
				appendShadowDecision(&report, plan.signal, date, "hold", "信号未满足加仓门槛，维持现有仓位", positionQuantity(position), cfg.MaxPositionPercent)
				continue
			}
			if exists && position.riskExitReason != "" {
				appendShadowDecision(&report, plan.signal, date, "wait", position.riskExitReason+"；风险退出完成前不再加仓", positionQuantity(position), 0)
				continue
			}
			planScore := shadowSignalScoreForConfig(plan.signal, cfg)
			if exists && (len(position.lots) >= cfg.MaxEntryTranches || planScore < position.lastSignalScore+cfg.AdditionScoreStep) {
				reason := "信号未显著增强，维持现有仓位"
				if len(position.lots) >= cfg.MaxEntryTranches {
					reason = "已达到最大分批建仓次数，维持现有仓位"
				}
				appendShadowDecision(&report, plan.signal, date, "hold", reason, positionQuantity(position), cfg.MaxPositionPercent)
				continue
			}
			rotation := shadowRotationCandidate{}
			if !exists && len(active) >= cfg.MaxOpenPositions {
				if len(active) > cfg.MaxOpenPositions {
					appendShadowDecision(&report, plan.signal, date, "wait", fmt.Sprintf("历史持仓 %d 个高于当前 %d 个上限，先等待减仓或到期退出", len(active), cfg.MaxOpenPositions), 0, targetPositionPercent(plan.signal, cfg))
					continue
				}
				if dailyRotations[date] >= cfg.MaxDailyRotations {
					appendShadowDecision(&report, plan.signal, date, "wait", fmt.Sprintf("组合持仓已满，且当日换仓已达到 %d 次上限", cfg.MaxDailyRotations), 0, targetPositionPercent(plan.signal, cfg))
					continue
				}
				var rotationReason string
				rotation, rotationReason = selectShadowRotationCandidate(active, plan.signal, date, cfg)
				if rotation.symbol == "" {
					appendShadowDecision(&report, plan.signal, date, "wait", rotationReason, 0, targetPositionPercent(plan.signal, cfg))
					continue
				}
				if rotation.bar.Open <= 0 || onePriceBar(rotation.bar) || limitLockedAtExit(rotation.position.plan.signal, rotation.position.plan.bars, rotation.barIndex) {
					appendShadowDecision(&report, plan.signal, date, "wait", fmt.Sprintf("候选领先最弱持仓 %s，但该持仓开盘一字板或停牌，无法轮出", rotationDisplayName(rotation)), 0, targetPositionPercent(plan.signal, cfg))
					continue
				}
			}
			if exitDate, blocked := riskCooldownActive(report.Trades, plan.signal.Symbol, plan.bars, date, cfg.RiskCooldownDays); blocked {
				appendShadowDecision(&report, plan.signal, date, "wait", fmt.Sprintf("风险退出后冷却期未结束（最近退出 %s），暂不重新开仓", exitDate), positionQuantity(position), targetPositionPercent(plan.signal, cfg))
				continue
			}
			if plan.signal.InvalidationPrice > 0 && bar.Open <= plan.signal.InvalidationPrice {
				rejection := makeRejection(plan.signal, "buy", date, fmt.Sprintf("次日开盘 %.2f 已跌破信号失效位 %.2f，不再追认原买入信号", bar.Open, plan.signal.InvalidationPrice))
				if !hasRejection(report.Rejections, rejection) {
					report.RejectedOrders++
					report.Rejections = append(report.Rejections, rejection)
				}
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
			budgetActive := active
			budgetCash := cash
			if rotation.symbol != "" {
				budgetActive = cloneShadowPositions(active)
				delete(budgetActive, rotation.symbol)
				budgetCash += rotationSaleProceeds(rotation, cfg)
			}
			budget, targetPercent, reason := entryBudget(cfg, plan.signal, bar.Open, budgetCash, dailyDeployment[date], budgetActive, position, exists)
			if budget <= 0 {
				appendShadowDecision(&report, plan.signal, date, "wait", reason, positionQuantity(position), targetPercent)
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
			projectedCash := cash
			if rotation.symbol != "" {
				projectedCash += rotationSaleProceeds(rotation, cfg)
			}
			if buy.Amount+fee > projectedCash || projectedCash-buy.Amount-fee < cfg.InitialCash*cfg.CashReservePercent/100 {
				appendShadowDecision(&report, plan.signal, date, "wait", "现金缓冲不足，等待后续交易日", positionQuantity(position), cfg.MaxPositionPercent)
				continue
			}
			if rotation.symbol != "" {
				rotationOrders, rotationTrades, rotationProceeds, rotationFees := executeShadowRotationOut(rotation, plan.signal, date, cfg, len(report.Trades)+1)
				cash += rotationProceeds
				report.Orders = append(report.Orders, rotationOrders...)
				report.Trades = append(report.Trades, rotationTrades...)
				report.CompletedTrades += len(rotationTrades)
				for _, order := range rotationOrders {
					report.TotalTurnover += order.Amount
				}
				report.TotalFees += rotationFees
				delete(active, rotation.symbol)
				rotatedOutToday[rotation.symbol] = true
				dailyRotations[date]++
				appendShadowDecision(&report, rotation.position.plan.signal, date, "rotate_out", fmt.Sprintf("组合持仓已满；新候选 %s %.1f 分领先 %.1f 分，轮出最弱持仓", shadowSignalDisplayName(plan.signal), planScore, rotation.score), 0, 0)
			}
			buy.Status = OrderFilled
			if exists {
				buy.PositionAction = "add"
				buy.PositionSequence = len(position.lots) + 1
			} else {
				buy.PositionAction = "open"
				if rotation.symbol != "" {
					buy.PositionAction = "rotate_in"
					buy.Reason = fmt.Sprintf("新候选 %.1f 分领先轮出持仓 %s %.1f 分，执行有限换仓", planScore, rotationDisplayName(rotation), rotation.score)
				}
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
				position.lastSignalScore = planScore
				position.targetPercent = targetPercent
				active[plan.signal.Symbol] = position
				appendShadowDecision(&report, plan.signal, date, "add", "信号显著增强，按质量、波动与风险距离动态加仓", positionQuantity(position), targetPercent)
			} else {
				active[plan.signal.Symbol] = shadowPosition{plan: plan, lots: []shadowLot{lot}, lastSignalDate: signalDate(plan.signal), lastSignalScore: planScore, targetPercent: targetPercent}
				action := "open"
				reason := "首次建仓按信号质量、波动风险与失效距离定仓，并仅使用目标仓位的一部分"
				if rotation.symbol != "" {
					action = "rotate_in"
					reason = fmt.Sprintf("组合满员时仅换入显著更强候选；领先 %s %.1f 分", rotationDisplayName(rotation), planScore-rotation.score)
				}
				appendShadowDecision(&report, plan.signal, date, action, reason, buy.Quantity, targetPercent)
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
			if position.fallback.Quantity <= 0 {
				continue
			}
			report.Positions = append(report.Positions, *position.fallback)
			continue
		}
		if positionQuantity(position) <= 0 {
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

func cloneShadowPositions(input map[string]shadowPosition) map[string]shadowPosition {
	result := make(map[string]shadowPosition, len(input))
	for symbol, position := range input {
		result[symbol] = position
	}
	return result
}

func selectShadowRotationCandidate(active map[string]shadowPosition, incoming realtime.Signal, date string, cfg Config) (shadowRotationCandidate, string) {
	symbols := make([]string, 0, len(active))
	for symbol := range active {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	best := shadowRotationCandidate{}
	bestScore := math.Inf(1)
	bestEntryDate := ""
	for _, symbol := range symbols {
		position := active[symbol]
		if position.fallback != nil || position.riskExitReason != "" || len(position.lots) == 0 {
			continue
		}
		currentBarIndex := barIndex(position.plan.bars, date)
		if currentBarIndex < 0 {
			continue
		}
		bar := position.plan.bars[currentBarIndex]
		if bar.Open <= 0 || onePriceBar(bar) || limitLockedAtExit(position.plan.signal, position.plan.bars, currentBarIndex) {
			continue
		}
		entryDate := ""
		eligible := true
		for _, lot := range position.lots {
			if lot.entry.AttemptDate == "" || lot.entry.AttemptDate >= date {
				eligible = false
				break
			}
			entryIndex := barIndex(position.plan.bars, lot.entry.AttemptDate)
			if entryIndex < 0 || currentBarIndex-entryIndex < cfg.RotationMinimumHoldDays {
				eligible = false
				break
			}
			if entryDate == "" || lot.entry.AttemptDate < entryDate {
				entryDate = lot.entry.AttemptDate
			}
		}
		if !eligible {
			continue
		}
		score := position.lastSignalScore
		if !finite(score) || score <= 0 {
			score = shadowSignalScoreForConfig(position.plan.signal, cfg)
		}
		if score < bestScore || (score == bestScore && (bestEntryDate == "" || entryDate < bestEntryDate)) {
			best = shadowRotationCandidate{symbol: symbol, position: position, bar: bar, barIndex: currentBarIndex, score: score}
			bestScore = score
			bestEntryDate = entryDate
		}
	}
	if best.symbol == "" {
		return shadowRotationCandidate{}, fmt.Sprintf("组合持仓已满，但没有同时满足 T+1、至少 %d 个交易日持有期和开盘可交易条件的轮出持仓", cfg.RotationMinimumHoldDays)
	}
	incomingScore := shadowSignalScoreForConfig(incoming, cfg)
	if incomingScore < bestScore+cfg.RotationScoreGap {
		return shadowRotationCandidate{}, fmt.Sprintf("组合持仓已满；新候选 %.1f 分未领先最弱可轮出持仓 %s %.1f 分达到 %.1f 分", incomingScore, rotationDisplayName(best), bestScore, cfg.RotationScoreGap)
	}
	return best, ""
}

func rotationSaleProceeds(rotation shadowRotationCandidate, cfg Config) float64 {
	proceeds := 0.0
	price := rotation.bar.Open * (1 - cfg.SlippageBPS/10000)
	for _, lot := range rotation.position.lots {
		amount := price * float64(lot.entry.Quantity)
		proceeds += amount - transactionFee(amount, "sell", cfg)
	}
	return proceeds
}

func executeShadowRotationOut(rotation shadowRotationCandidate, incoming realtime.Signal, date string, cfg Config, tradeSequence int) ([]ShadowOrder, []ShadowTrade, float64, float64) {
	orders := make([]ShadowOrder, 0, len(rotation.position.lots))
	trades := make([]ShadowTrade, 0, len(rotation.position.lots))
	proceeds, fees := 0.0, 0.0
	for index, lot := range rotation.position.lots {
		signal := lot.plan.signal
		if signal.Symbol == "" {
			signal = rotation.position.plan.signal
		}
		sell := makeOrder(signal, "sell", date, rotation.bar.Open, lot.entry.Quantity, cfg, 0)
		sell.ID = fmt.Sprintf("%s-rotate-%s-sell-%d", rotation.symbol, date, index+1)
		sell.Status = OrderFilled
		sell.ExecutionTime = simulatedOpeningExecutionTime(date)
		sell.PositionAction = "rotate_out"
		sell.PositionSequence = index + 1
		sell.Reason = fmt.Sprintf("组合换仓：%s %.1f 分显著强于当前持仓 %.1f 分", shadowSignalDisplayName(incoming), shadowSignalScoreForConfig(incoming, cfg), rotation.score)
		fee := transactionFee(sell.Amount, "sell", cfg)
		orders = append(orders, sell)
		trades = append(trades, tradeForLot(lot, sell, rotation.bar.Open, date, rotation.barIndex, fee, tradeSequence+index))
		proceeds += sell.Amount - fee
		fees += fee
	}
	return orders, trades, proceeds, fees
}

func rotationDisplayName(rotation shadowRotationCandidate) string {
	if rotation.position.plan.signal.Name != "" {
		return rotation.position.plan.signal.Name
	}
	return strings.TrimPrefix(strings.TrimPrefix(rotation.symbol, "sh"), "sz")
}

func shadowSignalDisplayName(signal realtime.Signal) string {
	if signal.Name != "" {
		return signal.Name
	}
	return strings.TrimPrefix(strings.TrimPrefix(signal.Symbol, "sh"), "sz")
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
	if position.fallback != nil {
		amount := position.fallback.EntryAmount
		if amount <= 0 {
			amount = position.fallback.EntryPrice * float64(position.fallback.Quantity)
		}
		return amount + position.fallback.EntryFee
	}
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

func targetPositionPercent(signal realtime.Signal, cfg Config) float64 {
	minimumScore := cfg.MinimumScore
	if cfg.UseCalibratedScore && signal.CalibrationID != "" && signal.CalibrationMinimumScore > 0 {
		minimumScore = signal.CalibrationMinimumScore
	}
	qualityRange := math.Max(1, 75-minimumScore)
	quality := clamp((shadowSignalScoreForConfig(signal, cfg)-minimumScore)/qualityRange, 0, 1)
	qualityMultiplier := .6 + .4*quality
	riskMultiplier := .8
	if score, ok := signalComponentRatio(signal, "volatility-risk"); ok {
		riskMultiplier = .65 + .35*score
	}
	regimeMultiplier := .9
	if score, ok := signalComponentRatio(signal, "market-regime"); ok {
		regimeMultiplier = .85 + .15*score
	}
	overlayMultiplier := 1.0
	if signal.RiskMultiplier > 0 && finite(signal.RiskMultiplier) {
		overlayMultiplier = clamp(signal.RiskMultiplier, .25, 1)
	}
	target := cfg.MaxPositionPercent * qualityMultiplier * riskMultiplier * regimeMultiplier * overlayMultiplier
	return clamp(target, cfg.MaxPositionPercent*.25, cfg.MaxPositionPercent)
}

func targetPositionPercentAtPrice(signal realtime.Signal, rawPrice float64, cfg Config) float64 {
	target := targetPositionPercent(signal, cfg)
	riskDistance := riskDistancePercent(signal, rawPrice, cfg)
	if riskDistance > 0 {
		target = math.Min(target, cfg.MaxPositionRiskPercent/riskDistance*100)
	}
	return clamp(target, cfg.MaxPositionPercent*.25, cfg.MaxPositionPercent)
}

func signalComponentRatio(signal realtime.Signal, key string) (float64, bool) {
	for _, component := range signal.Components {
		if component.Key != key || strings.TrimSpace(component.State) == "数据不足" || !finite(component.Score) {
			continue
		}
		maximum := component.Maximum
		if maximum <= 0 || !finite(maximum) {
			maximum = 20
		}
		return clamp(component.Score/maximum, 0, 1), true
	}
	return 0, false
}

func riskDistancePercent(signal realtime.Signal, rawPrice float64, cfg Config) float64 {
	distance := cfg.MaxLossPercent
	if signal.InvalidationPrice > 0 && signal.InvalidationPrice < rawPrice {
		invalidationDistance := (rawPrice - signal.InvalidationPrice) / rawPrice * 100
		if invalidationDistance > 0 {
			distance = math.Min(distance, invalidationDistance)
		}
	}
	return clamp(distance, cfg.MinimumRiskDistancePercent, cfg.MaxLossPercent)
}

func lotRiskAmount(lot shadowLot, cfg Config) float64 {
	rawPrice := lot.entry.RawPrice
	if rawPrice <= 0 {
		rawPrice = lot.entry.Price
	}
	signal := lot.plan.signal
	if signal.InvalidationPrice <= 0 {
		signal.InvalidationPrice = lot.entry.InvalidationPrice
	}
	return lot.entryCost * riskDistancePercent(signal, rawPrice, cfg) / 100
}

func positionRiskAmount(position shadowPosition, cfg Config) float64 {
	if position.fallback != nil {
		amount := position.fallback.EntryAmount + position.fallback.EntryFee
		if amount <= 0 {
			amount = position.fallback.EntryPrice * float64(position.fallback.Quantity)
		}
		signal := realtime.Signal{InvalidationPrice: position.fallback.InvalidationPrice}
		return amount * riskDistancePercent(signal, position.fallback.EntryPrice, cfg) / 100
	}
	total := 0.0
	for _, lot := range position.lots {
		total += lotRiskAmount(lot, cfg)
	}
	return total
}

func investedRisk(active map[string]shadowPosition, cfg Config) float64 {
	total := 0.0
	for _, position := range active {
		total += positionRiskAmount(position, cfg)
	}
	return total
}

func positionIndustry(position shadowPosition) string {
	if position.fallback != nil {
		return industryExposureKey(position.fallback.Industry)
	}
	if industry := industryExposureKey(position.plan.signal.Industry); industry != "行业待补充" {
		return industry
	}
	for _, lot := range position.lots {
		if industry := industryExposureKey(lot.entry.Industry); industry != "行业待补充" {
			return industry
		}
	}
	return "行业待补充"
}

func industryExposureKey(industry string) string {
	value := strings.TrimSpace(industry)
	for _, suffix := range []string{"Ⅳ", "Ⅲ", "Ⅱ", "Ⅰ", " IV", " III", " II", " I"} {
		if strings.HasSuffix(value, suffix) {
			value = strings.TrimSpace(strings.TrimSuffix(value, suffix))
			break
		}
	}
	if value == "" {
		return "行业待补充"
	}
	return value
}

func industryInvestedCost(active map[string]shadowPosition, industry string) float64 {
	total := 0.0
	for _, position := range active {
		if positionIndustry(position) == industry {
			total += positionCost(position)
		}
	}
	return total
}

func entryBudget(cfg Config, signal realtime.Signal, rawPrice, cash, deployedToday float64, active map[string]shadowPosition, position shadowPosition, exists bool) (float64, float64, string) {
	targetPercent := targetPositionPercentAtPrice(signal, rawPrice, cfg)
	reserve := cfg.InitialCash * cfg.CashReservePercent / 100
	availableCash := cash - reserve
	if availableCash <= 0 {
		return 0, targetPercent, "已达到现金缓冲下限"
	}
	portfolioRoom := cfg.InitialCash*cfg.MaxPortfolioPercent/100 - investedCost(active)
	if portfolioRoom <= 0 {
		return 0, targetPercent, "组合仓位已达到上限"
	}
	dailyRoom := cfg.InitialCash*cfg.MaxDailyDeploymentPercent/100 - deployedToday
	if dailyRoom <= 0 {
		return 0, targetPercent, "当日新增资金已达到上限"
	}
	industry := industryExposureKey(signal.Industry)
	industryRoom := math.Inf(1)
	if industry != "行业待补充" {
		industryRoom = cfg.InitialCash*cfg.MaxIndustryPercent/100 - industryInvestedCost(active, industry)
		if industryRoom <= 0 {
			return 0, targetPercent, fmt.Sprintf("%s行业仓位已达到 %.0f%% 上限", industry, cfg.MaxIndustryPercent)
		}
	}
	targetPosition := cfg.InitialCash * targetPercent / 100
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
		return 0, targetPercent, "单股动态目标仓位已达到上限"
	}
	positionRiskRoom := cfg.InitialCash*cfg.MaxPositionRiskPercent/100 - positionRiskAmount(position, cfg)
	if positionRiskRoom <= 0 {
		return 0, targetPercent, "单股风险预算已达到上限"
	}
	portfolioRiskRoom := cfg.InitialCash*cfg.MaxPortfolioRiskPercent/100 - investedRisk(active, cfg)
	if portfolioRiskRoom <= 0 {
		return 0, targetPercent, "组合风险预算已达到上限"
	}
	riskDistance := riskDistancePercent(signal, rawPrice, cfg) / 100
	riskBudgetRoom := math.Min(positionRiskRoom, portfolioRiskRoom) / riskDistance
	budget := math.Min(availableCash, math.Min(portfolioRoom, math.Min(dailyRoom, math.Min(industryRoom, math.Min(positionRoom, riskBudgetRoom)))))
	return budget, targetPercent, ""
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
	industry := sell.Industry
	if industry == "" {
		industry = lot.entry.Industry
	}
	return ShadowTrade{ID: fmt.Sprintf("ST%04d", sequence), EntryOrderID: lot.entry.ID, ExitOrderID: sell.ID, Symbol: sell.Symbol, Name: sell.Name, Industry: industry, SignalDate: lot.entry.SignalDate, EntryDate: lot.entry.AttemptDate, ExitDate: exitDate, Quantity: lot.entry.Quantity, SignalClose: lot.plan.signalClose, ExitClose: exitClose, EntryPrice: lot.entry.Price, ExitPrice: sell.Price, TheoreticalReturnPercent: theoretical, ExecutableReturnPercent: executable, ExecutionGapPercent: executable - theoretical, GrossProfit: (sell.Price - lot.entry.Price) * float64(sell.Quantity), NetProfit: netProfit, TotalFee: lot.entryCost - lot.entry.Amount + fee, HoldingDays: holdingDays, PositionAction: sell.PositionAction, ExitReason: sell.Reason, ExitTime: sell.ExecutionTime}
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
	report.Decisions = append(report.Decisions, ShadowDecision{ID: id, Symbol: signal.Symbol, Name: signal.Name, Industry: signal.Industry, Date: date, Action: action, Reason: reason, SignalScore: shadowSignalScoreForConfig(signal, report.Config), SignalReasons: append([]string(nil), signal.Reasons...), CurrentQuantity: quantity, TargetPositionPercent: targetPercent})
}

func aggregateShadowPosition(position shadowPosition, cfg Config, asOf string) ShadowOpenPosition {
	if position.fallback != nil {
		preserved := *position.fallback
		preserved.RiskExitPending = position.riskExitReason != "" || preserved.RiskExitPending
		if position.riskExitReason != "" {
			preserved.RiskExitReason = position.riskExitReason
		}
		return preserved
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
		lots = append(lots, ShadowPositionLot{OrderID: lot.entry.ID, EntryDate: lot.entry.AttemptDate, EntryTime: lot.entry.ExecutionTime, Quantity: lot.entry.Quantity, EntryPrice: lot.entry.Price, EntryAmount: lot.entry.Amount, EntryFee: lot.entryCost - lot.entry.Amount, SignalID: strings.TrimSuffix(lot.entry.ID, "-buy"), Industry: lot.entry.Industry, SignalDate: lot.entry.SignalDate, SignalClose: lot.plan.signalClose, SignalScore: lot.entry.SignalScore, TriggerPrice: lot.entry.TriggerPrice, InvalidationPrice: lot.entry.InvalidationPrice, SignalReasons: append([]string(nil), lot.entry.SignalReasons...), TargetExitDate: lot.plan.targetExitDate})
	}
	lastBar := latestBar(position.plan.bars, asOf)
	marketValue := lastBar.Close * float64(quantity)
	profit := marketValue - entryCost
	entryPrice := 0.0
	if quantity > 0 {
		entryPrice = entryAmount / float64(quantity)
	}
	targetPercent := position.targetPercent
	if targetPercent <= 0 {
		targetPercent = cfg.MaxPositionPercent
	}
	industry := position.plan.signal.Industry
	if industry == "" && len(lots) > 0 {
		industry = lots[len(lots)-1].Industry
	}
	return ShadowOpenPosition{SignalID: position.plan.signal.ID, Symbol: position.plan.signal.Symbol, Name: position.plan.signal.Name, Industry: industry, SignalDate: position.lastSignalDate, EntryDate: entryDate, EntryTime: entryTime, Quantity: quantity, EntryPrice: entryPrice, EntryAmount: entryAmount, EntryFee: entryFee, SignalClose: position.plan.signalClose, AvailableQuantity: available, SignalScore: position.lastSignalScore, TriggerPrice: position.plan.signal.TriggerPrice, InvalidationPrice: position.plan.signal.InvalidationPrice, SignalReasons: append([]string(nil), position.plan.signal.Reasons...), Monster: position.plan.signal.Monster, LastDate: lastBar.Date, LastPrice: lastBar.Close, MarketValue: marketValue, UnrealizedProfit: profit, UnrealizedReturnPercent: safeReturnPercent(profit, entryCost), TargetExitDate: targetExit, Lots: lots, AdditionCount: maxInt(0, len(lots)-1), TargetPositionPercent: targetPercent, RiskExitPending: position.riskExitReason != "", RiskExitReason: position.riskExitReason}
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
	if report == nil {
		return
	}
	report.Config = canonicalConfig(report.Config)
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
	recomputeIndustryExposures(report)
	report.DailyReview = buildShadowDailyReview(*report)
}

func buildShadowDailyReview(report Report) *ShadowDailyReview {
	date := strings.TrimSpace(report.AsOf)
	if date == "" {
		return nil
	}
	review := &ShadowDailyReview{Date: date, CalibrationStatus: "基线 Champion 运行中"}
	if report.Config.UseCalibratedScore {
		if report.CalibrationID != "" {
			review.CalibrationStatus = "校准 Challenger 运行中"
		} else {
			review.CalibrationStatus = "等待成熟校准候选"
		}
	}
	blockers := make(map[string]int)
	events := make(map[string]struct{})
	for _, decision := range report.Decisions {
		if decision.Date != date {
			continue
		}
		review.DecisionCount++
		if decision.EventSource == "realtime" && decision.EventID != "" {
			events[decision.EventID] = struct{}{}
		}
		switch decision.Action {
		case "open", "rotate_in":
			review.OpenCount++
		case "add":
			review.AddCount++
		case "reduce":
			review.ReduceCount++
		case "t_reduce":
			review.TReduceCount++
		case "t_rebuy":
			review.TRebuyCount++
		case "risk_exit":
			review.RiskExitCount++
		case "exit":
			review.ExitCount++
		case "hold":
			review.HoldCount++
			if strings.TrimSpace(decision.Reason) != "" {
				blockers[shadowReviewBlocker(decision.Reason)]++
			}
		case "wait":
			review.WaitCount++
			if strings.TrimSpace(decision.Reason) != "" {
				blockers[shadowReviewBlocker(decision.Reason)]++
			}
		}
	}
	for _, order := range report.Orders {
		if order.AttemptDate != date || order.Status != OrderFilled {
			continue
		}
		if order.EventID != "" && (strings.HasPrefix(order.EventID, "rt-") || strings.HasPrefix(order.EventSource, "realtime")) {
			events[order.EventID] = struct{}{}
		}
		review.OrderCount++
		review.Turnover += order.Amount
		review.Fees += transactionFee(order.Amount, order.Side, report.Config)
		if order.PositionAction == "t_reduce" {
			review.TNetCashBenefit += order.Amount - transactionFee(order.Amount, "sell", report.Config)
		} else if order.PositionAction == "t_rebuy" {
			review.TNetCashBenefit -= order.Amount + transactionFee(order.Amount, "buy", report.Config)
		}
	}
	for _, rejection := range report.Rejections {
		if rejection.AttemptDate != date {
			continue
		}
		if rejection.EventID != "" && (strings.HasPrefix(rejection.EventID, "rt-") || strings.HasPrefix(rejection.EventSource, "realtime")) {
			events[rejection.EventID] = struct{}{}
		}
		review.RejectedCount++
		if strings.TrimSpace(rejection.Reason) != "" {
			blockers[shadowReviewBlocker(rejection.Reason)]++
		}
	}
	// A T high-sell remains pending until its same-day rebuy is filled. Keep
	// that quantity visible in the daily review so an incomplete round is not
	// mistaken for a normal reduction.
	pendingSymbols := make(map[string]struct{})
	for _, position := range report.Positions {
		if position.Symbol != "" {
			pendingSymbols[position.Symbol] = struct{}{}
		}
	}
	for _, order := range report.Orders {
		if order.AttemptDate == date && order.PositionAction == "t_reduce" && order.Status == OrderFilled && order.Symbol != "" {
			pendingSymbols[order.Symbol] = struct{}{}
		}
	}
	for symbol := range pendingSymbols {
		if _, quantity := realtimePendingT(report, symbol, date); quantity > 0 {
			review.PendingTQuantity += quantity
		}
	}
	for _, trade := range report.Trades {
		if trade.ExitDate == date {
			review.RealizedProfit += trade.NetProfit
		}
	}
	review.MonitoringEvents = len(events)
	items := make([]ShadowReasonCount, 0, len(blockers))
	for reason, count := range blockers {
		items = append(items, ShadowReasonCount{Reason: reason, Count: count})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Reason < items[j].Reason
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > 5 {
		items = items[:5]
	}
	review.TopBlockers = items
	if report.CalibrationID != "" {
		review.CalibrationSummary = fmt.Sprintf("%s · %d 个成熟样本 · 最低分 %.1f", report.CalibrationID, report.CalibrationReadySamples, report.CalibrationMinimumScore)
	} else {
		review.CalibrationSummary = "当前使用固定基线；候选需通过时间留出和前向观察后才会进入 Challenger"
	}
	return review
}

// shadowReviewBlocker keeps the daily summary useful without throwing away
// the full explanation stored on each decision. Similar reasons differ by
// score, price or symbol and would otherwise each occupy a separate row.
func shadowReviewBlocker(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "未说明"
	}
	for _, item := range []struct {
		contains string
		label    string
	}{
		{"信号尚未显著增强", "信号未显著增强"},
		{"信号未显著增强", "信号未显著增强"},
		{"现金缓冲不足", "现金缓冲不足"},
		{"达到盘中持仓上限", "持仓数量达到上限"},
		{"达到最大分批建仓次数", "分批建仓次数达到上限"},
		{"没有满足T+1", "T+1 暂无可卖批次"},
		{"当日新买批次受 T+1 锁定", "T+1 暂无可卖批次"},
		{"已有做T卖出批次", "等待做T回补"},
		{"等待价格回落至覆盖费用", "等待做T回补"},
		{"调仓冷却", "普通调仓冷却"},
		{"行业集中度", "行业集中度保护"},
		{"风险退出后冷却期", "风险退出冷却"},
		{"历史成交额不可用", "历史成交额不可用"},
		{"成交额容量不足", "成交额容量不足"},
		{"触及涨停", "涨停或不可交易"},
		{"触及跌停", "跌停或不可交易"},
	} {
		if strings.Contains(reason, item.contains) {
			return item.label
		}
	}
	return reason
}

func recomputeIndustryExposures(report *Report) {
	if report == nil {
		return
	}
	byIndustry := make(map[string]*ShadowIndustryExposure)
	for _, position := range report.Positions {
		industry := industryExposureKey(position.Industry)
		item := byIndustry[industry]
		if item == nil {
			item = &ShadowIndustryExposure{Industry: industry, LimitPercent: report.Config.MaxIndustryPercent}
			byIndustry[industry] = item
		}
		cost := position.EntryAmount + position.EntryFee
		if cost <= 0 {
			cost = position.EntryPrice * float64(position.Quantity)
		}
		item.InvestedCost += cost
		item.MarketValue += position.MarketValue
		item.Symbols = append(item.Symbols, position.Symbol)
	}
	report.IndustryExposures = report.IndustryExposures[:0]
	for _, item := range byIndustry {
		if report.InitialCash > 0 {
			item.ExposurePercent = item.InvestedCost / report.InitialCash * 100
		}
		sort.Strings(item.Symbols)
		report.IndustryExposures = append(report.IndustryExposures, *item)
	}
	sort.SliceStable(report.IndustryExposures, func(i, j int) bool {
		if report.IndustryExposures[i].ExposurePercent == report.IndustryExposures[j].ExposurePercent {
			return report.IndustryExposures[i].Industry < report.IndustryExposures[j].Industry
		}
		return report.IndustryExposures[i].ExposurePercent > report.IndustryExposures[j].ExposurePercent
	})
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
	report.IndustryExposures = append([]ShadowIndustryExposure(nil), report.IndustryExposures...)
	for index := range report.Positions {
		report.Positions[index].Lots = append([]ShadowPositionLot(nil), report.Positions[index].Lots...)
		report.Positions[index].SignalReasons = append([]string(nil), report.Positions[index].SignalReasons...)
		report.Positions[index].Monster.Reasons = append([]string(nil), report.Positions[index].Monster.Reasons...)
		report.Positions[index].Monster.Risks = append([]string(nil), report.Positions[index].Monster.Risks...)
		report.Positions[index].Monster.Warnings = append([]string(nil), report.Positions[index].Monster.Warnings...)
		for lotIndex := range report.Positions[index].Lots {
			report.Positions[index].Lots[lotIndex].SignalReasons = append([]string(nil), report.Positions[index].Lots[lotIndex].SignalReasons...)
		}
	}
	for index := range report.Orders {
		report.Orders[index].SignalReasons = append([]string(nil), report.Orders[index].SignalReasons...)
	}
	for index := range report.Rejections {
		report.Rejections[index].SignalReasons = append([]string(nil), report.Rejections[index].SignalReasons...)
	}
	for index := range report.Decisions {
		report.Decisions[index].SignalReasons = append([]string(nil), report.Decisions[index].SignalReasons...)
	}
	for index := range report.IndustryExposures {
		report.IndustryExposures[index].Symbols = append([]string(nil), report.IndustryExposures[index].Symbols...)
	}
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
	selected := representativeSignalsForConfig(signals, limit, cfg)
	currentNow := now()
	calendarDates, calendarError := e.tradingCalendar(ctx, currentNow, options.CalendarDates)
	if calendarError != nil {
		calendarDates = nil
	}
	checkpoint := TradingCheckpointAt(currentNow, calendarDates)
	report := Report{EngineVersion: ShadowEngineVersion, ConfigFingerprint: OptionsFingerprint(cfg, options.Limit), SignalLimit: options.Limit, CheckpointPhase: checkpoint.Phase, ExecutionMode: ExecutionModeDaily, GeneratedAt: currentNow, AsOf: checkpoint.Date, Config: cfg, SignalCount: len(signals), InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash}
	applyReportCalibrationMetadata(&report, signals)
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
			if !shadowSignalActionableForConfig(signal, cfg) {
				continue
			}
			entryEligible := shadowSignalEntryEligible(signal, cfg)
			if entryEligible {
				report.CandidateCount++
			}
			if options.Realtime && signalDate(signal) == checkpoint.Date {
				// Current-session signals belong to the point-in-time layer. Do not
				// also label them as next-day pending daily plans.
				continue
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
			return shadowSignalScoreForConfig(plans[i].signal, cfg) > shadowSignalScoreForConfig(plans[j].signal, cfg)
		}
		return plans[i].entryDate < plans[j].entryDate
	})
	report = simulate(report, plans, cfg)
	if options.Realtime && checkpoint.Phase == CheckpointOpen && checkpoint.Date == localTradingDate(currentNow) {
		var realtimeErr error
		report, realtimeErr = e.advanceRealtime(ctx, report, signals, options, calendarDates, currentNow)
		if realtimeErr != nil {
			report.Warnings = appendUniqueWarning(report.Warnings, "盘中影子层未推进: "+realtimeErr.Error())
		}
	}
	return report, nil
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
	if cfg.MaxIndustryPercent <= 0 || cfg.MaxIndustryPercent > 100 {
		cfg.MaxIndustryPercent = defaults.MaxIndustryPercent
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
	if cfg.MaxOpenPositions <= 0 || cfg.MaxOpenPositions > 200 {
		cfg.MaxOpenPositions = defaults.MaxOpenPositions
	}
	if cfg.MaxDailyRotations <= 0 || cfg.MaxDailyRotations > cfg.MaxOpenPositions {
		cfg.MaxDailyRotations = minInt(defaults.MaxDailyRotations, cfg.MaxOpenPositions)
	}
	if cfg.RotationScoreGap <= 0 || !finite(cfg.RotationScoreGap) {
		cfg.RotationScoreGap = defaults.RotationScoreGap
	}
	if cfg.RotationMinimumHoldDays <= 0 || cfg.RotationMinimumHoldDays > 60 {
		cfg.RotationMinimumHoldDays = defaults.RotationMinimumHoldDays
	}
	if !cfg.EnableIntradayT {
		cfg.EnableIntradayT = defaults.EnableIntradayT
	}
	if cfg.TCorePositionPercent <= 0 || cfg.TCorePositionPercent >= 100 || !finite(cfg.TCorePositionPercent) {
		cfg.TCorePositionPercent = defaults.TCorePositionPercent
	}
	if cfg.TTranchePercent <= 0 || cfg.TTranchePercent >= 100 || !finite(cfg.TTranchePercent) {
		cfg.TTranchePercent = defaults.TTranchePercent
	}
	if cfg.TCorePositionPercent+cfg.TTranchePercent > 100 {
		cfg.TTranchePercent = 100 - cfg.TCorePositionPercent
	}
	if cfg.TMaxDailyRounds <= 0 || cfg.TMaxDailyRounds > 10 {
		cfg.TMaxDailyRounds = defaults.TMaxDailyRounds
	}
	if cfg.TVWAPDeviationPercent <= 0 || cfg.TVWAPDeviationPercent > 20 || !finite(cfg.TVWAPDeviationPercent) {
		cfg.TVWAPDeviationPercent = defaults.TVWAPDeviationPercent
	}
	if cfg.TMinimumPriceGapPercent <= 0 || cfg.TMinimumPriceGapPercent > 20 || !finite(cfg.TMinimumPriceGapPercent) {
		cfg.TMinimumPriceGapPercent = defaults.TMinimumPriceGapPercent
	}
	if cfg.TMinimumNetProfitPercent < 0 || cfg.TMinimumNetProfitPercent > 20 || !finite(cfg.TMinimumNetProfitPercent) {
		cfg.TMinimumNetProfitPercent = defaults.TMinimumNetProfitPercent
	}
	if cfg.TCooldownMinutes <= 0 || cfg.TCooldownMinutes > 240 {
		cfg.TCooldownMinutes = defaults.TCooldownMinutes
	}
	if cfg.SignalRebalanceCooldownMinutes <= 0 || cfg.SignalRebalanceCooldownMinutes > 240 {
		cfg.SignalRebalanceCooldownMinutes = defaults.SignalRebalanceCooldownMinutes
	}
	if cfg.MaxPortfolioRiskPercent <= 0 || cfg.MaxPortfolioRiskPercent > 100 || !finite(cfg.MaxPortfolioRiskPercent) {
		cfg.MaxPortfolioRiskPercent = defaults.MaxPortfolioRiskPercent
	}
	if cfg.MaxPositionRiskPercent <= 0 || cfg.MaxPositionRiskPercent > 100 || !finite(cfg.MaxPositionRiskPercent) {
		cfg.MaxPositionRiskPercent = defaults.MaxPositionRiskPercent
	}
	if cfg.MaxLossPercent <= 0 || cfg.MaxLossPercent >= 100 || !finite(cfg.MaxLossPercent) {
		cfg.MaxLossPercent = defaults.MaxLossPercent
	}
	if cfg.MinimumRiskDistancePercent <= 0 || cfg.MinimumRiskDistancePercent >= 100 || !finite(cfg.MinimumRiskDistancePercent) {
		cfg.MinimumRiskDistancePercent = defaults.MinimumRiskDistancePercent
	}
	if cfg.RiskCooldownDays <= 0 || cfg.RiskCooldownDays > 60 {
		cfg.RiskCooldownDays = defaults.RiskCooldownDays
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

// CanonicalConfigForMigration exposes the same zero-value/default normalization
// used by the evaluator to callers that need to upgrade an older persisted
// shadow ledger without replaying its settled history.
func CanonicalConfigForMigration(cfg Config) Config {
	return canonicalConfig(cfg)
}

func representativeSignals(signals []realtime.Signal, limit int) []realtime.Signal {
	return representativeSignalsForConfig(signals, limit, Config{})
}

// representativeSignalsForConfig keeps one point-in-time signal per symbol and
// signal date. When two archive rows share the same timestamp, the account's
// active score model decides which row is representative. This matters for
// the independent Monster ledger because the composite score and radar score
// can intentionally disagree.
func representativeSignalsForConfig(signals []realtime.Signal, limit int, cfg Config) []realtime.Signal {
	latest := make(map[string]realtime.Signal)
	for _, signal := range signals {
		if strings.TrimSpace(signal.Symbol) == "" {
			continue
		}
		key := signal.Symbol + ":" + signalDate(signal)
		previous, found := latest[key]
		if !found || signal.AsOf.After(previous.AsOf) || (signal.AsOf.Equal(previous.AsOf) && representativeSignalScore(signal, cfg) > representativeSignalScore(previous, cfg)) {
			latest[key] = signal
		}
	}
	result := make([]realtime.Signal, 0, len(latest))
	for _, signal := range latest {
		result = append(result, signal)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].AsOf.Equal(result[j].AsOf) {
			return representativeSignalScore(result[i], cfg) > representativeSignalScore(result[j], cfg)
		}
		return result[i].AsOf.Before(result[j].AsOf)
	})
	if limit > 0 && len(result) > limit {
		return result[:limit]
	}
	return result
}

func representativeSignalScore(signal realtime.Signal, cfg Config) float64 {
	if cfg.UseMonsterRadar || cfg.UseCalibratedScore {
		return shadowSignalScoreForConfig(signal, cfg)
	}
	return signal.Score
}

func shadowSignalActionable(signal realtime.Signal) bool {
	return shadowSignalActionableForConfig(signal, Config{})
}

func shadowSignalActionableForConfig(signal realtime.Signal, cfg Config) bool {
	state := shadowSignalStateForConfig(signal, cfg)
	switch state {
	case realtime.StateTriggered, realtime.StateWatching, realtime.StateWeak, realtime.StateInvalid:
		return true
	default:
		return false
	}
}

func shadowSignalStateForConfig(signal realtime.Signal, cfg Config) realtime.SignalState {
	if cfg.UseMonsterRadar {
		return monsterSignalState(signal.Monster)
	}
	if cfg.UseCalibratedScore && signal.CalibrationID != "" && signal.CalibratedState != "" {
		return signal.CalibratedState
	}
	return signal.State
}

func monsterSignalState(radar realtime.MonsterRadar) realtime.SignalState {
	switch radar.Stage {
	case realtime.MonsterStageEbbing, realtime.MonsterStageDiverging:
		return realtime.StateWeak
	case realtime.MonsterStageInsufficient, "":
		return realtime.StateInvalid
	}
	if !radar.Eligible {
		return realtime.StateWeak
	}
	if radar.Score >= 72 {
		return realtime.StateTriggered
	}
	return realtime.StateWatching
}

func shadowSignalEntryEligible(signal realtime.Signal, cfg Config) bool {
	minimumScore := cfg.MinimumScore
	if cfg.UseMonsterRadar {
		return signal.Monster.Eligible && shadowSignalScoreForConfig(signal, cfg) >= minimumScore
	}
	if cfg.UseCalibratedScore && signal.CalibrationID != "" && signal.CalibrationMinimumScore > 0 {
		minimumScore = signal.CalibrationMinimumScore
	}
	score := shadowSignalScoreForConfig(signal, cfg)
	if cfg.UseCalibratedScore && signal.CalibrationID != "" {
		return signal.CalibratedPortfolioEligible && score >= minimumScore
	}
	if signal.RiskMultiplier > 0 || signal.CrossSectionTotal > 0 || signal.MarketRegime != "" {
		return signal.PortfolioEligible && score >= minimumScore
	}
	return (signal.State == realtime.StateTriggered || signal.State == realtime.StateWatching) && score >= minimumScore
}

func shadowSignalScore(signal realtime.Signal) float64 {
	if signal.RiskAdjustedScore > 0 && finite(signal.RiskAdjustedScore) {
		return signal.RiskAdjustedScore
	}
	return signal.Score
}

func shadowSignalScoreForConfig(signal realtime.Signal, cfg Config) float64 {
	if cfg.UseMonsterRadar && signal.Monster.Score > 0 && finite(signal.Monster.Score) {
		return signal.Monster.Score
	}
	if cfg.UseCalibratedScore && signal.CalibrationID != "" {
		if signal.CalibratedRiskAdjustedScore > 0 && finite(signal.CalibratedRiskAdjustedScore) {
			return signal.CalibratedRiskAdjustedScore
		}
		if signal.CalibratedScore > 0 && finite(signal.CalibratedScore) {
			return signal.CalibratedScore
		}
	}
	return shadowSignalScore(signal)
}

func applyReportCalibrationMetadata(report *Report, signals []realtime.Signal) {
	if report == nil || !report.Config.UseCalibratedScore {
		return
	}
	// The report-level fields describe the score model used by the current
	// signal batch, not the model that created an older position. Clear the
	// watermark before inspecting the batch so a Challenger rollback is visible
	// instead of leaving a stale "active" label on the adaptive account.
	report.CalibrationID = ""
	report.CalibrationReadySamples = 0
	report.CalibrationMinimumScore = 0
	for _, signal := range signals {
		if strings.TrimSpace(signal.CalibrationID) == "" {
			continue
		}
		report.CalibrationID = signal.CalibrationID
		report.CalibrationReadySamples = signal.CalibrationReadySamples
		report.CalibrationMinimumScore = signal.CalibrationMinimumScore
		return
	}
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
	return ShadowOrder{ID: orderID(signal, side), Symbol: signal.Symbol, Name: signal.Name, Industry: signal.Industry, Side: side, SignalDate: signalDate(signal), AttemptDate: date, Quantity: quantity, RawPrice: rawPrice, Price: price, Amount: price * float64(quantity), CapacityAmount: capacity, Status: "pending", ExecutionTime: simulatedExecutionTime(side, date), SignalScore: shadowSignalScoreForConfig(signal, cfg), TriggerPrice: signal.TriggerPrice, InvalidationPrice: signal.InvalidationPrice, SignalReasons: append([]string(nil), signal.Reasons...)}
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
		amount := bars[index].Amount
		if amount <= 0 || !finite(amount) {
			price := bars[index].Close
			if bars[index].Open > 0 && bars[index].High > 0 && bars[index].Low > 0 {
				price = (bars[index].Open + bars[index].Close + bars[index].High + bars[index].Low) / 4
			}
			// Volume units differ by provider. Volume times price is therefore a
			// conservative lower bound when the upstream amount field is absent.
			if price > 0 && bars[index].Volume > 0 && finite(bars[index].Volume) {
				amount = price * bars[index].Volume
			}
		}
		if amount > 0 && finite(amount) {
			total += amount
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
