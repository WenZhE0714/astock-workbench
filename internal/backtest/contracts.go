// Package backtest owns historical simulation contracts. It is intentionally
// isolated from execution so a research run can never submit a live order.
package backtest

import (
	"context"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type PriceAdjustment string

const (
	AdjustmentNone     PriceAdjustment = "none"
	AdjustmentForward  PriceAdjustment = "forward"
	AdjustmentBackward PriceAdjustment = "backward"
)

type TechnicalParameters struct {
	EntryMode      string  `json:"entry_mode,omitempty"`
	FastMA         int     `json:"fast_ma"`
	SlowMA         int     `json:"slow_ma"`
	BreakoutDays   int     `json:"breakout_days"`
	VolumeRatioMin float64 `json:"volume_ratio_min"`
	StopLoss       float64 `json:"stop_loss"`
	TakeProfit     float64 `json:"take_profit"`
	MaxHoldingDays int     `json:"max_holding_days"`
	MaxPosition    float64 `json:"max_position"`
}

const (
	EntryModeBreakout   = strategy.EntryModeBreakout
	EntryModeReclaim    = strategy.EntryModeReclaim
	EntryModePullback   = strategy.EntryModePullback
	EntryModeMomentum   = strategy.EntryModeMomentum
	EntryModeMeanRevert = strategy.EntryModeMeanRevert
	EntryModeVolSqueeze = strategy.EntryModeVolSqueeze
	EntryModeAdaptive   = strategy.EntryModeAdaptive
)

type EntryModeDescriptor = strategy.EntryModeDescriptor

func EntryModeDescriptors() []EntryModeDescriptor {
	return strategy.EntryModeDescriptors()
}

// EntryModeLabels returns display labels in catalog order. Callers should use
// the catalog instead of maintaining a second list of strategy names.
func EntryModeLabels() []string {
	return strategy.EntryModeLabels()
}

// EntryModeCatalogText is intended for concise CLI and prompt summaries.
func EntryModeCatalogText() string {
	return strategy.EntryModeCatalogText()
}

func EntryModes() []string {
	return strategy.EntryModes()
}

func ValidEntryMode(mode string) bool {
	return strategy.ValidEntryMode(mode)
}

func EntryModeLabel(mode string) string {
	return strategy.EntryModeLabel(mode)
}

func EntryModeFamily(mode string) string {
	return strategy.EntryModeFamily(mode)
}

func (parameters TechnicalParameters) EffectiveEntryMode() string {
	if parameters.EntryMode == "" {
		return EntryModeBreakout
	}
	return parameters.EntryMode
}

func DefaultTechnicalParameters() TechnicalParameters {
	return TechnicalParameters{
		EntryMode: EntryModeBreakout, FastMA: 20, SlowMA: 60, BreakoutDays: 20, VolumeRatioMin: 1.2,
		StopLoss: 0.08, TakeProfit: 0.20, MaxHoldingDays: 40, MaxPosition: 0.20,
	}
}

type Request struct {
	Strategy          string              `json:"strategy"`
	StrategyVersion   string              `json:"strategy_version"`
	Tickers           []string            `json:"tickers"`
	Names             map[string]string   `json:"names,omitempty"`
	Start             time.Time           `json:"start"`
	End               time.Time           `json:"end"`
	InitialCash       float64             `json:"initial_cash"`
	CommissionRate    float64             `json:"commission_rate"`
	MinimumCommission float64             `json:"minimum_commission"`
	StampDutyRate     float64             `json:"stamp_duty_rate"`
	TransferFeeRate   float64             `json:"transfer_fee_rate"`
	SlippageBPS       float64             `json:"slippage_bps"`
	Adjustment        PriceAdjustment     `json:"adjustment"`
	Benchmark         string              `json:"benchmark,omitempty"`
	NoFutureData      bool                `json:"no_future_data"`
	PointInTimePool   bool                `json:"point_in_time_pool"`
	LiquidateAtEnd    bool                `json:"liquidate_at_end"`
	Technical         TechnicalParameters `json:"technical"`
}

type SignalSnapshot struct {
	Date      string `json:"date"`
	Action    string `json:"action"`
	EntryMode string `json:"entry_mode,omitempty"`
	// SelectedEntryMode records the concrete setup selected by the adaptive
	// ensemble. EntryMode remains the configured mode so reports can distinguish
	// an adaptive request from the shape that actually supplied the evidence.
	SelectedEntryMode string   `json:"selected_entry_mode,omitempty"`
	Reasons           []string `json:"reasons"`
	Close             float64  `json:"close"`
	Low               float64  `json:"low,omitempty"`
	PreviousClose     float64  `json:"previous_close,omitempty"`
	FastMA            float64  `json:"fast_ma"`
	PreviousFastMA    float64  `json:"previous_fast_ma,omitempty"`
	SlowMA            float64  `json:"slow_ma"`
	PriorHigh         float64  `json:"prior_high"`
	PriorLow          float64  `json:"prior_low,omitempty"`
	PriorShortHigh    float64  `json:"prior_short_high,omitempty"`
	BollingerLower    float64  `json:"bollinger_lower,omitempty"`
	RSI14             float64  `json:"rsi14,omitempty"`
	Return20          float64  `json:"return_20,omitempty"`
	RangeCompression  float64  `json:"range_compression,omitempty"`
	VolumeRatio       float64  `json:"volume_ratio"`
	VolumeRequirement string   `json:"volume_requirement,omitempty"`
}

type Fill struct {
	Date        string  `json:"date"`
	Side        string  `json:"side"`
	Price       float64 `json:"price"`
	RawPrice    float64 `json:"raw_price"`
	Quantity    int     `json:"quantity"`
	Amount      float64 `json:"amount"`
	Commission  float64 `json:"commission"`
	StampDuty   float64 `json:"stamp_duty"`
	TransferFee float64 `json:"transfer_fee"`
	TotalFee    float64 `json:"total_fee"`
	SlippageBPS float64 `json:"slippage_bps"`
	Forced      bool    `json:"forced,omitempty"`
}

type Trade struct {
	ID              string         `json:"id"`
	Symbol          string         `json:"symbol"`
	Name            string         `json:"name"`
	Strategy        string         `json:"strategy"`
	StrategyVersion string         `json:"strategy_version"`
	EntrySignal     SignalSnapshot `json:"entry_signal"`
	Entry           Fill           `json:"entry"`
	ExitSignal      SignalSnapshot `json:"exit_signal"`
	Exit            Fill           `json:"exit"`
	HoldingDays     int            `json:"holding_days"`
	GrossProfit     float64        `json:"gross_profit"`
	NetProfit       float64        `json:"net_profit"`
	ReturnPercent   float64        `json:"return_percent"`
	MaxFavorable    float64        `json:"max_favorable_percent"`
	MaxAdverse      float64        `json:"max_adverse_percent"`
	ExitReason      string         `json:"exit_reason"`
}

type OpenPosition struct {
	Symbol           string         `json:"symbol"`
	Name             string         `json:"name"`
	Quantity         int            `json:"quantity"`
	EntrySignal      SignalSnapshot `json:"entry_signal"`
	Entry            Fill           `json:"entry"`
	LastDate         string         `json:"last_date"`
	LastPrice        float64        `json:"last_price"`
	MarketValue      float64        `json:"market_value"`
	UnrealizedProfit float64        `json:"unrealized_profit"`
	ReturnPercent    float64        `json:"return_percent"`
}

type EquityPoint struct {
	Date      string  `json:"date"`
	Cash      float64 `json:"cash"`
	Positions float64 `json:"positions"`
	Equity    float64 `json:"equity"`
	Drawdown  float64 `json:"drawdown_percent"`
}

type BenchmarkPoint struct {
	Date   string  `json:"date"`
	Close  float64 `json:"close"`
	Return float64 `json:"return_percent"`
}

type Metrics struct {
	TotalReturn          float64 `json:"total_return_percent"`
	AnnualizedReturn     float64 `json:"annualized_return_percent"`
	AnnualizedVolatility float64 `json:"annualized_volatility_percent"`
	MaxDrawdown          float64 `json:"max_drawdown_percent"`
	Sharpe               float64 `json:"sharpe"`
	Sortino              float64 `json:"sortino"`
	Calmar               float64 `json:"calmar"`
	BestDay              float64 `json:"best_day_percent"`
	WorstDay             float64 `json:"worst_day_percent"`
	BenchmarkAvailable   bool    `json:"benchmark_available"`
	BenchmarkReturn      float64 `json:"benchmark_return_percent"`
	ExcessReturn         float64 `json:"excess_return_percent"`
	Trades               int     `json:"trades"`
	Wins                 int     `json:"wins"`
	Losses               int     `json:"losses"`
	WinRate              float64 `json:"win_rate_percent"`
	ProfitFactor         float64 `json:"profit_factor"`
	AverageTrade         float64 `json:"average_trade_percent"`
	AverageHoldingDays   float64 `json:"average_holding_days"`
	Turnover             float64 `json:"turnover_percent"`
	TotalFees            float64 `json:"total_fees"`
	FinalEquity          float64 `json:"final_equity"`
}

// MarketRegimeMetrics compounds only the daily returns observed while the
// benchmark was in one point-in-time regime. Excess return uses the subset of
// those days where both strategy and benchmark closes are available.
type MarketRegimeMetrics struct {
	Key                string  `json:"key"`
	Label              string  `json:"label"`
	Days               int     `json:"days"`
	Trades             int     `json:"trades"`
	ReturnPercent      float64 `json:"return_percent"`
	MaxDrawdown        float64 `json:"max_drawdown_percent"`
	BenchmarkAvailable bool    `json:"benchmark_available"`
	BenchmarkDays      int     `json:"benchmark_days"`
	BenchmarkReturn    float64 `json:"benchmark_return_percent"`
	ExcessReturn       float64 `json:"excess_return_percent"`
}

type Result struct {
	RunID           string                  `json:"run_id"`
	GeneratedAt     time.Time               `json:"generated_at"`
	Request         Request                 `json:"request"`
	Metrics         Metrics                 `json:"metrics"`
	Trades          []Trade                 `json:"trades"`
	OpenPositions   []OpenPosition          `json:"open_positions,omitempty"`
	Equity          []EquityPoint           `json:"equity"`
	BenchmarkEquity []BenchmarkPoint        `json:"benchmark_equity,omitempty"`
	MarketRegimes   []MarketRegimeMetrics   `json:"market_regimes,omitempty"`
	DataSources     map[string]string       `json:"data_sources"`
	DataCoverage    map[string]DataCoverage `json:"data_coverage,omitempty"`
	Warnings        []string                `json:"warnings,omitempty"`
	Directory       string                  `json:"-"`
	ReportPath      string                  `json:"-"`
}

type DataCoverage struct {
	RequestedStart string  `json:"requested_start"`
	RequestedEnd   string  `json:"requested_end"`
	FirstDate      string  `json:"first_date,omitempty"`
	LastDate       string  `json:"last_date,omitempty"`
	Bars           int     `json:"bars"`
	CoverageRatio  float64 `json:"coverage_ratio"`
}

type DailyBarProvider interface {
	FetchDailyBarsRange(context.Context, string, time.Time, time.Time, PriceAdjustment) ([]domain.DailyBar, error)
}

type Engine interface {
	Run(context.Context, Request) (Result, error)
}

type Period struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type OptimizationRequest struct {
	BaseRequest               Request `json:"base_request"`
	Train                     Period  `json:"train"`
	Validate                  Period  `json:"validate"`
	OutOfSample               Period  `json:"out_of_sample"`
	MaxCandidates             int     `json:"max_candidates"`
	MinimumValidationTrades   int     `json:"minimum_validation_trades"`
	MaximumValidationDrawdown float64 `json:"maximum_validation_drawdown_percent"`
	MaximumPerformanceGap     float64 `json:"maximum_train_validation_gap_percent"`
	MinimumValidationReturn   float64 `json:"minimum_validation_return_percent"`
	UseAI                     bool    `json:"use_ai"`
}

type CandidateResult struct {
	ID         string              `json:"id"`
	Baseline   bool                `json:"baseline"`
	Parameters TechnicalParameters `json:"parameters"`
	Train      Metrics             `json:"train"`
	Validate   Metrics             `json:"validate"`
	Score      float64             `json:"score"`
	Rejected   bool                `json:"rejected"`
	Reasons    []string            `json:"reasons,omitempty"`
}

type OptimizationResult struct {
	ID                 string              `json:"id"`
	GeneratedAt        time.Time           `json:"generated_at"`
	Request            OptimizationRequest `json:"request"`
	Candidates         []CandidateResult   `json:"candidates"`
	Selected           *CandidateResult    `json:"selected,omitempty"`
	SelectedTrain      *Result             `json:"-"`
	SelectedValidation *Result             `json:"-"`
	OutOfSample        *Result             `json:"-"`
	AIReview           string              `json:"ai_review,omitempty"`
	AIError            string              `json:"ai_error,omitempty"`
	Warnings           []string            `json:"warnings,omitempty"`
	Directory          string              `json:"-"`
	ReportPath         string              `json:"-"`
}

type OptimizationProgress struct {
	Phase     string `json:"phase"`
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
}

type StrategyProposal struct {
	ID         string              `json:"id"`
	Agent      string              `json:"agent"`
	Thesis     string              `json:"thesis"`
	Parameters TechnicalParameters `json:"parameters"`
}

type AgentResearchRun struct {
	Agent     string             `json:"agent"`
	Status    string             `json:"status"`
	Error     string             `json:"error,omitempty"`
	Thesis    string             `json:"thesis,omitempty"`
	Proposals []StrategyProposal `json:"proposals,omitempty"`
}

type WalkForwardFold struct {
	ID       string `json:"id"`
	Train    Period `json:"train"`
	Validate Period `json:"validate"`
}

type WalkForwardFoldResult struct {
	Fold             WalkForwardFold         `json:"fold"`
	Train            Metrics                 `json:"train"`
	Validate         Metrics                 `json:"validate"`
	TrainSources     map[string]string       `json:"train_sources,omitempty"`
	ValidateSources  map[string]string       `json:"validate_sources,omitempty"`
	TrainCoverage    map[string]DataCoverage `json:"train_coverage,omitempty"`
	ValidateCoverage map[string]DataCoverage `json:"validate_coverage,omitempty"`
	Warnings         []string                `json:"warnings,omitempty"`
}

type ContinuousCandidateResult struct {
	Proposal          StrategyProposal        `json:"proposal"`
	Folds             []WalkForwardFoldResult `json:"folds"`
	Score             float64                 `json:"score"`
	PositiveFoldRatio float64                 `json:"positive_fold_ratio"`
	ValidationTrades  int                     `json:"validation_trades"`
	AverageValidation float64                 `json:"average_validation_return_percent"`
	WorstDrawdown     float64                 `json:"worst_validation_drawdown_percent"`
	ConsensusAgents   int                     `json:"consensus_agents"`
	NeighborhoodSize  int                     `json:"neighborhood_size"`
	NeighborhoodScore float64                 `json:"neighborhood_score"`
	Rejected          bool                    `json:"rejected"`
	Reasons           []string                `json:"reasons,omitempty"`
}

type DataQualityCheck struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Detail  string `json:"detail"`
	Warning bool   `json:"warning,omitempty"`
}

type DataQualitySummary struct {
	Grade  string             `json:"grade"`
	Passed bool               `json:"passed"`
	Checks []DataQualityCheck `json:"checks"`
}

type ExperimentManifest struct {
	SchemaVersion      int               `json:"schema_version"`
	Strategy           string            `json:"strategy"`
	StrategyVersion    string            `json:"strategy_version"`
	Tickers            []string          `json:"tickers"`
	DataCutoff         string            `json:"data_cutoff"`
	Folds              []WalkForwardFold `json:"folds"`
	Holdout            Period            `json:"holdout"`
	CandidateSetHash   string            `json:"candidate_set_hash"`
	ConfigurationHash  string            `json:"configuration_hash"`
	PreviousExperiment string            `json:"previous_experiment,omitempty"`
	PreviousHoldoutEnd string            `json:"previous_holdout_end,omitempty"`
}

type StressResult struct {
	DoubleCost           *Result `json:"-"`
	BestTradeProfitShare float64 `json:"best_trade_profit_share"`
}

type ContinuousOptimizationRequest struct {
	BaseRequest               Request            `json:"base_request"`
	Folds                     []WalkForwardFold  `json:"folds"`
	Holdout                   Period             `json:"holdout"`
	Proposals                 []StrategyProposal `json:"proposals"`
	MinimumValidationTrades   int                `json:"minimum_validation_trades"`
	MinimumPositiveFoldRatio  float64            `json:"minimum_positive_fold_ratio"`
	MaximumValidationDrawdown float64            `json:"maximum_validation_drawdown_percent"`
}

type ContinuousOptimizationResult struct {
	ID               string                        `json:"id"`
	ParentID         string                        `json:"parent_id,omitempty"`
	Cycle            int                           `json:"cycle"`
	DataCutoff       string                        `json:"data_cutoff"`
	Manifest         ExperimentManifest            `json:"manifest"`
	Quality          DataQualitySummary            `json:"quality"`
	PriorLessons     string                        `json:"prior_lessons,omitempty"`
	GeneratedAt      time.Time                     `json:"generated_at"`
	Request          ContinuousOptimizationRequest `json:"request"`
	Agents           []AgentResearchRun            `json:"agents"`
	Candidates       []ContinuousCandidateResult   `json:"candidates"`
	Selected         *ContinuousCandidateResult    `json:"selected,omitempty"`
	Holdout          *Result                       `json:"-"`
	Stress           StressResult                  `json:"stress"`
	Stage            string                        `json:"stage"`
	GateReasons      []string                      `json:"gate_reasons,omitempty"`
	SupervisorReview string                        `json:"supervisor_review,omitempty"`
	SupervisorError  string                        `json:"supervisor_error,omitempty"`
	Warnings         []string                      `json:"warnings,omitempty"`
	Directory        string                        `json:"-"`
	ReportPath       string                        `json:"-"`
}

const (
	CandidateLifecycleAwaitingObservation = "awaiting-observation"
	CandidateLifecycleObserving           = "observing"
	CandidateLifecycleApprovalReady       = "approval-ready"
	CandidateLifecycleApproved            = "approved"
	CandidateLifecycleRejected            = "rejected"
	CandidateLifecycleRevoked             = "revoked"
)

type CandidateObservationPolicy struct {
	MinimumTradingDays   int     `json:"minimum_trading_days"`
	MinimumTrades        int     `json:"minimum_trades"`
	MinimumTotalReturn   float64 `json:"minimum_total_return_percent"`
	MinimumExcessReturn  float64 `json:"minimum_excess_return_percent"`
	MaximumDrawdown      float64 `json:"maximum_drawdown_percent"`
	MinimumCoverageRatio float64 `json:"minimum_coverage_ratio"`
}

type CandidateObservationCheck struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

type CandidateObservationAssessment struct {
	Passed  bool                        `json:"passed"`
	Verdict string                      `json:"verdict"`
	Checks  []CandidateObservationCheck `json:"checks"`
}

type CandidateLifecycleEvent struct {
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	Actor  string    `json:"actor"`
	Note   string    `json:"note,omitempty"`
}

// CandidateLifecycle is mutable post-research state kept outside the immutable
// optimization summary. Approval only authorizes reuse as a future research
// baseline; it never changes realtime scoring or submits an order.
type CandidateLifecycle struct {
	SchemaVersion      int                            `json:"schema_version"`
	ExperimentID       string                         `json:"experiment_id"`
	CandidateID        string                         `json:"candidate_id"`
	CandidateSetHash   string                         `json:"candidate_set_hash"`
	ConfigurationHash  string                         `json:"configuration_hash"`
	Status             string                         `json:"status"`
	Policy             CandidateObservationPolicy     `json:"policy"`
	ObservationStarted time.Time                      `json:"observation_started_at,omitempty"`
	ObservationStart   string                         `json:"observation_start_date,omitempty"`
	EvaluationEnd      string                         `json:"evaluation_end_date,omitempty"`
	ObservedThrough    string                         `json:"observed_through,omitempty"`
	TradingDays        int                            `json:"trading_days"`
	Assessment         CandidateObservationAssessment `json:"assessment"`
	ApprovedAt         time.Time                      `json:"approved_at,omitempty"`
	RejectedAt         time.Time                      `json:"rejected_at,omitempty"`
	RevokedAt          time.Time                      `json:"revoked_at,omitempty"`
	DecisionNote       string                         `json:"decision_note,omitempty"`
	Events             []CandidateLifecycleEvent      `json:"events,omitempty"`
	Observation        *Result                        `json:"-"`
	Directory          string                         `json:"-"`
}
