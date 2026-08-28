// Package realtime implements deterministic, read-only strategy scanning.
// It produces research signals only and never submits orders.
package realtime

import (
	"context"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type SignalState string

const (
	StateTriggered SignalState = "triggered"
	StateWatching  SignalState = "watching"
	StateWeak      SignalState = "weak"
	StateInvalid   SignalState = "invalid"
)

type Component struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Score    float64  `json:"score"`
	Maximum  float64  `json:"maximum"`
	State    string   `json:"state"`
	Reasons  []string `json:"reasons"`
	Warnings []string `json:"warnings,omitempty"`
}

type Signal struct {
	ID                string      `json:"id"`
	Symbol            string      `json:"symbol"`
	Name              string      `json:"name"`
	Industry          string      `json:"industry,omitempty"`
	State             SignalState `json:"state"`
	Score             float64     `json:"score"`
	RiskAdjustedScore float64     `json:"risk_adjusted_score,omitempty"`
	RiskMultiplier    float64     `json:"risk_multiplier,omitempty"`
	MarketRegime      string      `json:"market_regime,omitempty"`
	RiskOverlayReason string      `json:"risk_overlay_reason,omitempty"`
	CrossSectionRank  int         `json:"cross_section_rank,omitempty"`
	CrossSectionTotal int         `json:"cross_section_total,omitempty"`
	// CrossSectionPercentile is 0-100 and increases with relative strength.
	CrossSectionPercentile float64 `json:"cross_section_percentile,omitempty"`
	// TradableRank/Total are calculated from the comparable, sufficiently
	// covered signal pool. Full-pool rank remains available for audit but never
	// lets an invalid or incomplete signal consume a portfolio slot.
	TradableRank       int     `json:"tradable_rank,omitempty"`
	TradableTotal      int     `json:"tradable_total,omitempty"`
	TradablePercentile float64 `json:"tradable_percentile,omitempty"`
	PortfolioEligible  bool    `json:"portfolio_eligible"`
	PortfolioReason    string  `json:"portfolio_reason,omitempty"`
	// Calibrated* fields carry a gated challenger score beside the immutable
	// baseline score. They are consumed only by the adaptive shadow account;
	// the champion signal and its audit trail remain unchanged.
	CalibrationID               string      `json:"calibration_id,omitempty"`
	CalibrationReadySamples     int         `json:"calibration_ready_samples,omitempty"`
	CalibrationMinimumScore     float64     `json:"calibration_minimum_score,omitempty"`
	CalibratedScore             float64     `json:"calibrated_score,omitempty"`
	CalibratedRiskAdjustedScore float64     `json:"calibrated_risk_adjusted_score,omitempty"`
	CalibratedState             SignalState `json:"calibrated_state,omitempty"`
	CalibratedRank              int         `json:"calibrated_rank,omitempty"`
	CalibratedTotal             int         `json:"calibrated_total,omitempty"`
	CalibratedPortfolioEligible bool        `json:"calibrated_portfolio_eligible,omitempty"`
	CalibratedPortfolioReason   string      `json:"calibrated_portfolio_reason,omitempty"`
	Price                       float64     `json:"price"`
	Percent                     float64     `json:"percent"`
	Speed                       float64     `json:"speed_percent"`
	TriggerPrice                float64     `json:"trigger_price,omitempty"`
	InvalidationPrice           float64     `json:"invalidation_price,omitempty"`
	// EntryShape is an explanatory setup classification. It is independent of
	// the nine-factor composite score and is never an execution instruction.
	EntryShape                  string      `json:"entry_shape,omitempty"`
	EntryShapeLabel             string      `json:"entry_shape_label,omitempty"`
	EntryShapeScore             float64     `json:"entry_shape_score,omitempty"`
	EntryShapeEvidence          []string    `json:"entry_shape_evidence,omitempty"`
	EntryShapeTriggerPrice      float64     `json:"entry_shape_trigger_price,omitempty"`
	EntryShapeInvalidationPrice float64     `json:"entry_shape_invalidation_price,omitempty"`
	AsOf                        time.Time   `json:"as_of"`
	QuoteTime                   string      `json:"quote_time,omitempty"`
	DataDate                    string      `json:"data_date,omitempty"`
	DataSource                  string      `json:"data_source,omitempty"`
	Components                  []Component `json:"components"`
	Reasons                     []string    `json:"reasons"`
	Risks                       []string    `json:"risks"`
	Warnings                    []string    `json:"warnings,omitempty"`
}

const (
	OutcomeHorizon1D  = 1
	OutcomeHorizon3D  = 3
	OutcomeHorizon5D  = 5
	OutcomeHorizon10D = 10
)

// SignalOutcome is a point-in-time label for a previously emitted signal.
// Values are calculated only from bars strictly after SignalDate, so the
// evaluator cannot accidentally use the bar that produced the signal.
type SignalOutcome struct {
	Key                         string             `json:"key"`
	SignalID                    string             `json:"signal_id"`
	Symbol                      string             `json:"symbol"`
	Name                        string             `json:"name,omitempty"`
	Industry                    string             `json:"industry,omitempty"`
	SignalDate                  string             `json:"signal_date"`
	SignalAsOf                  time.Time          `json:"signal_as_of"`
	Score                       float64            `json:"score"`
	RiskAdjustedScore           float64            `json:"risk_adjusted_score,omitempty"`
	RiskMultiplier              float64            `json:"risk_multiplier,omitempty"`
	State                       SignalState        `json:"state"`
	MarketRegime                string             `json:"market_regime,omitempty"`
	Horizon                     int                `json:"horizon"`
	Status                      string             `json:"status"`
	TargetDate                  string             `json:"target_date,omitempty"`
	EntryPrice                  float64            `json:"entry_price,omitempty"`
	ExitPrice                   float64            `json:"exit_price,omitempty"`
	ReturnPercent               float64            `json:"return_percent,omitempty"`
	BenchmarkAvailable          bool               `json:"benchmark_available"`
	BenchmarkReturn             float64            `json:"benchmark_return_percent,omitempty"`
	ExcessReturn                float64            `json:"excess_return_percent,omitempty"`
	MaxFavorable                float64            `json:"max_favorable_percent,omitempty"`
	MaxAdverse                  float64            `json:"max_adverse_percent,omitempty"`
	HitTrigger                  bool               `json:"hit_trigger"`
	HitTarget                   bool               `json:"hit_target"`
	HitInvalidation             bool               `json:"hit_invalidation"`
	EvaluatedAt                 time.Time          `json:"evaluated_at"`
	DataSource                  string             `json:"data_source,omitempty"`
	Warning                     string             `json:"warning,omitempty"`
	CalibrationID               string             `json:"calibration_id,omitempty"`
	CalibratedScore             float64            `json:"calibrated_score,omitempty"`
	CalibratedRiskAdjustedScore float64            `json:"calibrated_risk_adjusted_score,omitempty"`
	CalibratedMinimumScore      float64            `json:"calibrated_minimum_score,omitempty"`
	CalibratedState             SignalState        `json:"calibrated_state,omitempty"`
	StrategyScores              map[string]float64 `json:"strategy_scores,omitempty"`
	StrategyStates              map[string]string  `json:"strategy_states,omitempty"`
	StrategyNames               map[string]string  `json:"strategy_names,omitempty"`
}

const (
	OutcomePending = "pending"
	OutcomeReady   = "ready"
	OutcomeInvalid = "invalid"
)

type OutcomeSummary struct {
	Horizon                    int     `json:"horizon"`
	Total                      int     `json:"total"`
	Ready                      int     `json:"ready"`
	Pending                    int     `json:"pending"`
	Invalid                    int     `json:"invalid"`
	Positive                   int     `json:"positive"`
	PositiveExcess             int     `json:"positive_excess"`
	CoveragePercent            float64 `json:"coverage_percent"`
	HitRatePercent             float64 `json:"hit_rate_percent"`
	ExcessHitRate              float64 `json:"excess_hit_rate_percent"`
	AverageReturn              float64 `json:"average_return_percent"`
	MedianReturn               float64 `json:"median_return_percent"`
	AverageExcess              float64 `json:"average_excess_return_percent"`
	AverageFavorable           float64 `json:"average_max_favorable_percent"`
	AverageAdverse             float64 `json:"average_max_adverse_percent"`
	TriggerHitRate             float64 `json:"trigger_hit_rate_percent"`
	TargetHitRate              float64 `json:"target_hit_rate_percent"`
	InvalidationRate           float64 `json:"invalidation_rate_percent"`
	InformationCoefficient     float64 `json:"information_coefficient"`
	RankInformationCoefficient float64 `json:"rank_information_coefficient"`
	SampleSufficient           bool    `json:"sample_sufficient"`
}

type OutcomeBreakdown struct {
	Key       string           `json:"key"`
	Label     string           `json:"label"`
	Signals   int              `json:"signals"`
	Summaries []OutcomeSummary `json:"summaries"`
}

type ComponentDescriptor struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type ComponentCoverage struct {
	Key              string  `json:"key"`
	Name             string  `json:"name"`
	Signals          int     `json:"signals"`
	Available        int     `json:"available"`
	Active           int     `json:"active"`
	AvailablePercent float64 `json:"available_percent"`
	ActivePercent    float64 `json:"active_percent"`
	AverageScore     float64 `json:"average_score"`
}

type ComponentCorrelation struct {
	LeftKey          string  `json:"left_key"`
	RightKey         string  `json:"right_key"`
	Samples          int     `json:"samples"`
	Correlation      float64 `json:"correlation"`
	RankCorrelation  float64 `json:"rank_correlation"`
	SampleSufficient bool    `json:"sample_sufficient"`
	Relation         string  `json:"relation"`
}

type ComponentRegimeMetric struct {
	ComponentKey     string  `json:"component_key"`
	ComponentName    string  `json:"component_name"`
	Regime           string  `json:"regime"`
	Horizon          int     `json:"horizon"`
	Samples          int     `json:"samples"`
	ActiveSamples    int     `json:"active_samples"`
	ActiveHitRate    float64 `json:"active_hit_rate_percent"`
	AverageExcess    float64 `json:"average_excess_percent"`
	RankIC           float64 `json:"rank_information_coefficient"`
	SampleSufficient bool    `json:"sample_sufficient"`
	State            string  `json:"state"`
}

type ComponentAnalysis struct {
	Signals                   int                     `json:"signals"`
	MinimumCorrelationSamples int                     `json:"minimum_correlation_samples"`
	MinimumRegimeSamples      int                     `json:"minimum_regime_samples"`
	Components                []ComponentDescriptor   `json:"components"`
	Coverage                  []ComponentCoverage     `json:"coverage"`
	Correlations              []ComponentCorrelation  `json:"correlations"`
	RegimeMetrics             []ComponentRegimeMetric `json:"regime_metrics"`
	SufficientPairs           int                     `json:"sufficient_pairs"`
	RedundantPairs            int                     `json:"redundant_pairs"`
	SufficientRegimeCells     int                     `json:"sufficient_regime_cells"`
	PositiveRegimeCells       int                     `json:"positive_regime_cells"`
	NegativeRegimeCells       int                     `json:"negative_regime_cells"`
	MixedRegimeCells          int                     `json:"mixed_regime_cells"`
}

type ComponentValidationFold struct {
	Index                   int     `json:"index"`
	TrainEnd                string  `json:"train_end"`
	ValidationStart         string  `json:"validation_start"`
	ValidationEnd           string  `json:"validation_end"`
	TrainSamples            int     `json:"train_samples"`
	ValidationSamples       int     `json:"validation_samples"`
	ValidationActiveSamples int     `json:"validation_active_samples"`
	TrainRankIC             float64 `json:"train_rank_information_coefficient"`
	ValidationRankIC        float64 `json:"validation_rank_information_coefficient"`
	ValidationAverageExcess float64 `json:"validation_average_excess_percent"`
	ValidationHitRate       float64 `json:"validation_hit_rate_percent"`
	CandidateWeight         float64 `json:"candidate_weight"`
	WeightAvailable         bool    `json:"weight_available"`
	SampleSufficient        bool    `json:"sample_sufficient"`
}

type ComponentValidationMetric struct {
	ComponentKey            string                    `json:"component_key"`
	ComponentName           string                    `json:"component_name"`
	Horizon                 int                       `json:"horizon"`
	AvailableSamples        int                       `json:"available_samples"`
	ValidationSamples       int                       `json:"validation_samples"`
	ValidationActive        int                       `json:"validation_active_samples"`
	SufficientFolds         int                       `json:"sufficient_folds"`
	PositiveFolds           int                       `json:"positive_folds"`
	NegativeFolds           int                       `json:"negative_folds"`
	ValidationAverageExcess float64                   `json:"validation_average_excess_percent"`
	ValidationHitRate       float64                   `json:"validation_hit_rate_percent"`
	ValidationRankIC        float64                   `json:"validation_rank_information_coefficient"`
	MinimumWeight           float64                   `json:"minimum_weight"`
	MaximumWeight           float64                   `json:"maximum_weight"`
	WeightDrift             float64                   `json:"weight_drift"`
	WeightStable            bool                      `json:"weight_stable"`
	SampleSufficient        bool                      `json:"sample_sufficient"`
	State                   string                    `json:"state"`
	Folds                   []ComponentValidationFold `json:"folds,omitempty"`
}

type ComponentWalkForwardAnalysis struct {
	MinimumSamples       int                         `json:"minimum_samples"`
	MinimumActiveSamples int                         `json:"minimum_active_samples"`
	MinimumFolds         int                         `json:"minimum_folds"`
	MaximumWeightDrift   float64                     `json:"maximum_weight_drift"`
	Metrics              []ComponentValidationMetric `json:"metrics"`
}

type PortfolioDayMetric struct {
	Date                    string   `json:"date"`
	Signals                 int      `json:"signals"`
	IndustryLabeledSignals  int      `json:"industry_labeled_signals"`
	IndustryCoveragePercent float64  `json:"industry_coverage_percent"`
	LargestIndustry         string   `json:"largest_industry"`
	LargestIndustryPercent  float64  `json:"largest_industry_percent"`
	LargestComponentKey     string   `json:"largest_component_key"`
	LargestComponentName    string   `json:"largest_component_name"`
	LargestComponentPercent float64  `json:"largest_component_percent"`
	RedundantPairs          int      `json:"redundant_pairs"`
	TotalPairs              int      `json:"total_pairs"`
	RedundantPairPercent    float64  `json:"redundant_pair_percent"`
	Passed                  bool     `json:"passed"`
	Violations              []string `json:"violations,omitempty"`
}

type PortfolioHorizonAnalysis struct {
	Horizon                        int                  `json:"horizon"`
	CandidateDays                  int                  `json:"candidate_days"`
	SufficientDays                 int                  `json:"sufficient_days"`
	PassedDays                     int                  `json:"passed_days"`
	ViolatingDays                  int                  `json:"violating_days"`
	AverageIndustryConcentration   float64              `json:"average_industry_concentration_percent"`
	MaximumIndustryConcentration   float64              `json:"maximum_industry_concentration_percent"`
	AverageComponentConcentration  float64              `json:"average_component_concentration_percent"`
	MaximumComponentConcentration  float64              `json:"maximum_component_concentration_percent"`
	AverageRedundantPairPercent    float64              `json:"average_redundant_pair_percent"`
	MaximumRedundantPairPercent    float64              `json:"maximum_redundant_pair_percent"`
	AverageIndustryCoveragePercent float64              `json:"average_industry_coverage_percent"`
	SampleSufficient               bool                 `json:"sample_sufficient"`
	Passed                         bool                 `json:"passed"`
	Recent                         []PortfolioDayMetric `json:"recent,omitempty"`
}

type PortfolioConstraintAnalysis struct {
	MinimumSignalsPerDay           int                        `json:"minimum_signals_per_day"`
	MinimumDays                    int                        `json:"minimum_days"`
	MinimumScore                   float64                    `json:"minimum_score"`
	MinimumIndustryCoveragePercent float64                    `json:"minimum_industry_coverage_percent"`
	MaximumIndustryConcentration   float64                    `json:"maximum_industry_concentration_percent"`
	MaximumComponentConcentration  float64                    `json:"maximum_component_concentration_percent"`
	MaximumRedundantPairPercent    float64                    `json:"maximum_redundant_pair_percent"`
	Horizons                       []PortfolioHorizonAnalysis `json:"horizons"`
}

type OutcomeReport struct {
	GeneratedAt       time.Time                    `json:"generated_at"`
	AsOf              string                       `json:"as_of"`
	DataThrough       string                       `json:"data_through,omitempty"`
	Horizons          []int                        `json:"horizons"`
	Summaries         []OutcomeSummary             `json:"summaries"`
	Strategies        []OutcomeBreakdown           `json:"strategies"`
	Scores            []OutcomeBreakdown           `json:"score_buckets"`
	States            []OutcomeBreakdown           `json:"states"`
	Regimes           []OutcomeBreakdown           `json:"market_regimes"`
	ComponentAnalysis ComponentAnalysis            `json:"component_analysis"`
	WalkForward       ComponentWalkForwardAnalysis `json:"component_walk_forward"`
	Portfolio         PortfolioConstraintAnalysis  `json:"portfolio_analysis"`
	Assessment        OutcomeAssessment            `json:"assessment"`
	Calibration       CalibrationAnalysis          `json:"calibration"`
	Tuning            TuningAnalysis               `json:"tuning"`
	Recent            []SignalOutcome              `json:"recent,omitempty"`
	Warnings          []string                     `json:"warnings,omitempty"`
}

// TuningRecommendation is a deterministic, evidence-linked next action. It
// is deliberately a recommendation rather than a parameter mutation: the
// calibration/lifecycle gates decide whether a Challenger can be observed or
// promoted.
type TuningRecommendation struct {
	Key      string `json:"key"`
	Priority string `json:"priority"`
	Action   string `json:"action"`
	Evidence string `json:"evidence"`
}

// TuningAnalysis is the daily strategy feedback surface. It summarizes the
// latest mature forward evidence and explains why the next calibration step is
// waiting, observing, or ready for human review.
type TuningAnalysis struct {
	Horizon         int                    `json:"horizon"`
	DataThrough     string                 `json:"data_through,omitempty"`
	MatureSamples   int                    `json:"mature_samples"`
	MatureDates     int                    `json:"mature_dates"`
	AverageExcess   float64                `json:"average_excess_percent"`
	HitRate         float64                `json:"hit_rate_percent"`
	Status          string                 `json:"status"`
	Summary         string                 `json:"summary"`
	Recommendations []TuningRecommendation `json:"recommendations,omitempty"`
}

// CalibrationMetric is one side of the Champion/Challenger comparison. Both
// sides use the same mature, benchmark-covered outcomes so an apparent uplift
// cannot come from different samples.
type CalibrationMetric struct {
	Samples       int     `json:"samples"`
	Positive      int     `json:"positive"`
	HitRate       float64 `json:"hit_rate_percent"`
	AverageReturn float64 `json:"average_return_percent"`
	AverageExcess float64 `json:"average_excess_percent"`
	RankIC        float64 `json:"rank_information_coefficient"`
}

// CalibrationAnalysis describes the live forward comparison for the latest
// Challenger. It is diagnostic evidence only; promotion to Champion remains a
// separately gated lifecycle decision.
type CalibrationAnalysis struct {
	ID                string            `json:"id,omitempty"`
	Horizon           int               `json:"horizon"`
	ReadySamples      int               `json:"ready_samples"`
	ComparisonSamples int               `json:"comparison_samples"`
	Champion          CalibrationMetric `json:"champion"`
	Challenger        CalibrationMetric `json:"challenger"`
	ExcessUplift      float64           `json:"excess_uplift_percent"`
	HitRateUplift     float64           `json:"hit_rate_uplift_percent"`
	RankICUplift      float64           `json:"rank_ic_uplift"`
	Status            string            `json:"status"`
	Recommendation    string            `json:"recommendation"`
}

type OutcomeCheck struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Required bool   `json:"required"`
	Detail   string `json:"detail"`
}

type ThresholdProposal struct {
	MinimumScore             float64                 `json:"minimum_score"`
	TrainSamples             int                     `json:"train_samples"`
	ValidationSamples        int                     `json:"validation_samples"`
	TrainAverageExcess       float64                 `json:"train_average_excess_percent"`
	ValidationAverageExcess  float64                 `json:"validation_average_excess_percent"`
	ValidationHitRate        float64                 `json:"validation_hit_rate_percent"`
	BaselineValidationExcess float64                 `json:"baseline_validation_excess_percent"`
	Folds                    []OutcomeValidationFold `json:"folds,omitempty"`
}

type OutcomeValidationFold struct {
	Index              int     `json:"index"`
	TrainEnd           string  `json:"train_end"`
	ValidationStart    string  `json:"validation_start"`
	ValidationEnd      string  `json:"validation_end"`
	MinimumScore       float64 `json:"minimum_score"`
	TrainSamples       int     `json:"train_samples"`
	ValidationSamples  int     `json:"validation_samples"`
	TrainAverageExcess float64 `json:"train_average_excess_percent"`
	AverageExcess      float64 `json:"average_excess_percent"`
	HitRate            float64 `json:"hit_rate_percent"`
	BaselineExcess     float64 `json:"baseline_excess_percent"`
}

type StrategyWeightProposal struct {
	Key           string  `json:"key"`
	Name          string  `json:"name"`
	Samples       int     `json:"samples"`
	AverageExcess float64 `json:"average_excess_percent"`
	HitRate       float64 `json:"hit_rate_percent"`
	Weight        float64 `json:"weight"`
}

type OutcomeAssessment struct {
	Stage           string                   `json:"stage"`
	Verdict         string                   `json:"verdict"`
	Horizon         int                      `json:"horizon"`
	MinimumSamples  int                      `json:"minimum_samples"`
	ReadySamples    int                      `json:"ready_samples"`
	Checks          []OutcomeCheck           `json:"checks"`
	Threshold       *ThresholdProposal       `json:"threshold,omitempty"`
	StrategyWeights []StrategyWeightProposal `json:"strategy_weights,omitempty"`
	NextStage       string                   `json:"next_stage"`
	Notes           []string                 `json:"notes,omitempty"`
}

// ScoreCalibration is a walk-forward-gated challenger configuration. The
// realtime scanner exposes its score in parallel with the champion score so
// shadow accounts can collect forward evidence without silently promoting it.
type ScoreCalibration struct {
	ID               string             `json:"id"`
	DataThrough      string             `json:"data_through"`
	Horizon          int                `json:"horizon"`
	ReadySamples     int                `json:"ready_samples"`
	MinimumScore     float64            `json:"minimum_score"`
	ComponentWeights map[string]float64 `json:"component_weights"`
}

type ScanResult struct {
	GeneratedAt   time.Time `json:"generated_at"`
	Universe      string    `json:"universe"`
	MarketState   string    `json:"market_state"`
	TradingDate   string    `json:"trading_date,omitempty"`
	TradingDay    bool      `json:"trading_day,omitempty"`
	CalendarKnown bool      `json:"calendar_known,omitempty"`
	Signals       []Signal  `json:"signals"`
	Warnings      []string  `json:"warnings,omitempty"`
}

type Snapshot struct {
	Now       time.Time
	Stock     domain.MarketStockSnapshot
	Quote     *domain.Quote
	Bars      []domain.DailyBar
	Minutes   []domain.MinutePoint
	Board     *domain.BoardFlow
	Benchmark []domain.DailyBar
	// CalendarDates is the immutable benchmark-date snapshot used for
	// point-in-time session classification. It is optional; nil preserves the
	// weekday fallback for embedders that do not provide a calendar.
	CalendarDates []string
}

type Strategy interface {
	Key() string
	Name() string
	Evaluate(Snapshot) Component
}

type MarketClient interface {
	FetchStockRanking(context.Context, domain.MarketScanMetric, bool, int) ([]domain.MarketStockSnapshot, error)
	FetchStocks(context.Context, []string) ([]domain.MarketStockSnapshot, error)
	FetchIndustryRanking(context.Context, domain.MarketScanMetric, bool, int) ([]domain.BoardFlow, error)
}

type QuoteClient interface {
	Fetch(context.Context, []string) ([]domain.Quote, error)
}

type HistoryClient interface {
	FetchDailyBars(context.Context, string) ([]domain.DailyBar, error)
}

// TradingCalendarProvider supplies a point-in-time exchange calendar. It is
// optional; scanners fall back to benchmark K-line dates and finally to the
// weekday clock when the provider is unavailable.
type TradingCalendarProvider func(context.Context, time.Time) ([]string, error)

type MinuteClient interface {
	FetchMinutePoints(context.Context, string) ([]domain.MinutePoint, error)
}

type Store interface {
	Append(ScanResult) error
	List(int) ([]Signal, error)
}

// OutcomeStore persists forward-looking labels separately from the immutable
// signal ledger. A pending horizon may be appended again when more trading
// bars become available, while the original scan evidence remains unchanged.
type OutcomeStore interface {
	Upsert([]SignalOutcome) error
	List(int) ([]SignalOutcome, error)
}
