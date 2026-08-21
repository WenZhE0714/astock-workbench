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
	Price             float64     `json:"price"`
	Percent           float64     `json:"percent"`
	Speed             float64     `json:"speed_percent"`
	TriggerPrice      float64     `json:"trigger_price,omitempty"`
	InvalidationPrice float64     `json:"invalidation_price,omitempty"`
	AsOf              time.Time   `json:"as_of"`
	QuoteTime         string      `json:"quote_time,omitempty"`
	DataDate          string      `json:"data_date,omitempty"`
	DataSource        string      `json:"data_source,omitempty"`
	Components        []Component `json:"components"`
	Reasons           []string    `json:"reasons"`
	Risks             []string    `json:"risks"`
	Warnings          []string    `json:"warnings,omitempty"`
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
	Key                string             `json:"key"`
	SignalID           string             `json:"signal_id"`
	Symbol             string             `json:"symbol"`
	Name               string             `json:"name,omitempty"`
	Industry           string             `json:"industry,omitempty"`
	SignalDate         string             `json:"signal_date"`
	SignalAsOf         time.Time          `json:"signal_as_of"`
	Score              float64            `json:"score"`
	State              SignalState        `json:"state"`
	MarketRegime       string             `json:"market_regime,omitempty"`
	Horizon            int                `json:"horizon"`
	Status             string             `json:"status"`
	TargetDate         string             `json:"target_date,omitempty"`
	EntryPrice         float64            `json:"entry_price,omitempty"`
	ExitPrice          float64            `json:"exit_price,omitempty"`
	ReturnPercent      float64            `json:"return_percent,omitempty"`
	BenchmarkAvailable bool               `json:"benchmark_available"`
	BenchmarkReturn    float64            `json:"benchmark_return_percent,omitempty"`
	ExcessReturn       float64            `json:"excess_return_percent,omitempty"`
	MaxFavorable       float64            `json:"max_favorable_percent,omitempty"`
	MaxAdverse         float64            `json:"max_adverse_percent,omitempty"`
	HitTrigger         bool               `json:"hit_trigger"`
	HitTarget          bool               `json:"hit_target"`
	HitInvalidation    bool               `json:"hit_invalidation"`
	EvaluatedAt        time.Time          `json:"evaluated_at"`
	DataSource         string             `json:"data_source,omitempty"`
	Warning            string             `json:"warning,omitempty"`
	StrategyScores     map[string]float64 `json:"strategy_scores,omitempty"`
	StrategyStates     map[string]string  `json:"strategy_states,omitempty"`
	StrategyNames      map[string]string  `json:"strategy_names,omitempty"`
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

type OutcomeReport struct {
	GeneratedAt       time.Time          `json:"generated_at"`
	AsOf              string             `json:"as_of"`
	Horizons          []int              `json:"horizons"`
	Summaries         []OutcomeSummary   `json:"summaries"`
	Strategies        []OutcomeBreakdown `json:"strategies"`
	Scores            []OutcomeBreakdown `json:"score_buckets"`
	States            []OutcomeBreakdown `json:"states"`
	Regimes           []OutcomeBreakdown `json:"market_regimes"`
	ComponentAnalysis ComponentAnalysis  `json:"component_analysis"`
	Assessment        OutcomeAssessment  `json:"assessment"`
	Recent            []SignalOutcome    `json:"recent,omitempty"`
	Warnings          []string           `json:"warnings,omitempty"`
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

type ScanResult struct {
	GeneratedAt time.Time `json:"generated_at"`
	Universe    string    `json:"universe"`
	MarketState string    `json:"market_state"`
	Signals     []Signal  `json:"signals"`
	Warnings    []string  `json:"warnings,omitempty"`
}

type Snapshot struct {
	Now       time.Time
	Stock     domain.MarketStockSnapshot
	Quote     *domain.Quote
	Bars      []domain.DailyBar
	Minutes   []domain.MinutePoint
	Board     *domain.BoardFlow
	Benchmark []domain.DailyBar
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
