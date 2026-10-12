package domain

import "time"

type PatternValidationRequest struct {
	ValidationStart string   `json:"validation_start,omitempty"`
	PriceBasis      string   `json:"price_basis,omitempty"`
	Symbols         []string `json:"symbols"`
	Start           string   `json:"start"`
	End             string   `json:"end"`
}

type PatternValidationInput struct {
	Symbol       string                   `json:"symbol"`
	Bars         []DailyBar               `json:"bars"`
	Error        string                   `json:"error,omitempty"`
	FetchStart   string                   `json:"fetch_start,omitempty"`
	FetchEnd     string                   `json:"fetch_end,omitempty"`
	FetchMode    string                   `json:"fetch_mode,omitempty"`
	FetchWarning string                   `json:"fetch_warning,omitempty"`
	Actions      *CorporateActionSnapshot `json:"actions,omitempty"`
	Adjustment   *AdjustmentSnapshot      `json:"adjustment,omitempty"`
}

type PatternValidationCoverage struct {
	Symbol          string   `json:"symbol"`
	FirstDate       string   `json:"first_date,omitempty"`
	LastDate        string   `json:"last_date,omitempty"`
	Bars            int      `json:"bars"`
	Source          string   `json:"source,omitempty"`
	Cached          bool     `json:"cached"`
	LateDiscoveries int      `json:"late_discoveries"`
	Error           string   `json:"error,omitempty"`
	FetchStart      string   `json:"fetch_start,omitempty"`
	FetchEnd        string   `json:"fetch_end,omitempty"`
	FetchMode       string   `json:"fetch_mode,omitempty"`
	WarmupBars      int      `json:"warmup_bars"`
	WarmupRequired  int      `json:"warmup_required,omitempty"`
	ExpectedDays    *int     `json:"expected_days,omitempty"`
	AvailableDays   int      `json:"available_days"`
	MissingDates    []string `json:"missing_dates,omitempty"`
	ReferenceStart  string   `json:"reference_start,omitempty"`
	ReferenceEnd    string   `json:"reference_end,omitempty"`
	Status          string   `json:"status,omitempty"`
	Notes           []string `json:"notes,omitempty"`
}

type PatternValidationOutcome struct {
	Horizon         int                      `json:"horizon"`
	State           string                   `json:"state"`
	Through         string                   `json:"through,omitempty"`
	ObservedDays    int                      `json:"observed_days"`
	PriceReturn     *float64                 `json:"price_return"`
	DirectionReturn *float64                 `json:"direction_return"`
	Favorable       *float64                 `json:"favorable"`
	Adverse         *float64                 `json:"adverse"`
	Invalidated     *bool                    `json:"invalidated"`
	Detail          string                   `json:"detail,omitempty"`
	ActionCheck     *PatternActionCheck      `json:"action_check,omitempty"`
	Benchmark       *PatternBenchmarkOutcome `json:"benchmark,omitempty"`
	PeriodCheck     *PatternPeriodCheck      `json:"period_check,omitempty"`
}

type PatternBenchmarkSpec struct {
	Symbol     string `json:"symbol"`
	Name       string `json:"name"`
	PriceBasis string `json:"price_basis"`
	Method     string `json:"method"`
}

type PatternBenchmarkOutcome struct {
	State                 string   `json:"state"`
	Start                 string   `json:"start,omitempty"`
	End                   string   `json:"end,omitempty"`
	Return                *float64 `json:"return"`
	ExcessReturn          *float64 `json:"excess_return"`
	DirectionExcessReturn *float64 `json:"direction_excess_return"`
	Detail                string   `json:"detail,omitempty"`
}

type PatternValidationSample struct {
	Period              string                     `json:"period,omitempty"`
	ID                  string                     `json:"id"`
	Symbol              string                     `json:"symbol"`
	PatternID           string                     `json:"pattern_id"`
	Name                string                     `json:"name"`
	Bias                string                     `json:"bias"`
	ObservedOn          string                     `json:"observed_on"`
	ConfirmedOn         string                     `json:"confirmed_on,omitempty"`
	InvalidatedOn       string                     `json:"invalidated_on,omitempty"`
	ExpiredOn           string                     `json:"expired_on,omitempty"`
	State               string                     `json:"state"`
	Regime              string                     `json:"regime"`
	RegimeDate          string                     `json:"regime_date"`
	ConfirmationClose   *float64                   `json:"confirmation_close"`
	SnapshotFingerprint string                     `json:"snapshot_fingerprint"`
	Snapshot            ChartStructure             `json:"snapshot"`
	Outcomes            []PatternValidationOutcome `json:"outcomes"`
	ActionCheck         *PatternActionCheck        `json:"action_check,omitempty"`
}

type PatternValidationReport struct {
	Version        string                      `json:"version"`
	RunID          string                      `json:"run_id"`
	GeneratedAt    time.Time                   `json:"generated_at"`
	Request        PatternValidationRequest    `json:"request"`
	Cutoff         string                      `json:"cutoff"`
	InputHash      string                      `json:"input_hash"`
	Horizons       []int                       `json:"horizons"`
	Coverage       []PatternValidationCoverage `json:"coverage"`
	Samples        []PatternValidationSample   `json:"samples"`
	Warnings       []string                    `json:"warnings"`
	Inputs         []PatternValidationInput    `json:"inputs,omitempty"`
	ActionCoverage []CorporateActionSnapshot   `json:"action_coverage,omitempty"`
	Adjustments    []PatternAdjustment         `json:"adjustments,omitempty"`
	Benchmark      *PatternBenchmarkSpec       `json:"benchmark,omitempty"`
	Study          *PatternValidationStudy     `json:"study,omitempty"`
}

type PatternValidationTotals struct {
	Samples                       int      `json:"samples"`
	Confirmed                     int      `json:"confirmed"`
	InvalidatedBeforeConfirmation int      `json:"invalidated_before_confirmation"`
	Mature                        int      `json:"mature"`
	Pending                       int      `json:"pending"`
	Unavailable                   int      `json:"unavailable"`
	ActionAffected                int      `json:"action_affected"`
	ActionUnverified              int      `json:"action_unverified"`
	ActionAdjusted                int      `json:"action_adjusted"`
	AveragePriceReturn            *float64 `json:"average_price_return"`
	AverageDirectionReturn        *float64 `json:"average_direction_return"`
	AverageFavorable              *float64 `json:"average_favorable"`
	AverageAdverse                *float64 `json:"average_adverse"`
	InvalidationRate              *float64 `json:"invalidation_rate"`
	BenchmarkPaired               int      `json:"benchmark_paired"`
	BenchmarkUnpaired             int      `json:"benchmark_unpaired"`
	BoundaryExcluded              int      `json:"boundary_excluded"`
	PositiveExcess                int      `json:"positive_excess"`
	PositiveDirectionExcess       int      `json:"positive_direction_excess"`
	AveragePairedPriceReturn      *float64 `json:"average_paired_price_return"`
	AverageBenchmarkReturn        *float64 `json:"average_benchmark_return"`
	AverageExcessReturn           *float64 `json:"average_excess_return"`
	AverageDirectionExcessReturn  *float64 `json:"average_direction_excess_return"`
	PositiveExcessRate            *float64 `json:"positive_excess_rate"`
	PositiveDirectionExcessRate   *float64 `json:"positive_direction_excess_rate"`
}

type PatternValidationGroup struct {
	PatternID string                  `json:"pattern_id"`
	Name      string                  `json:"name"`
	Bias      string                  `json:"bias"`
	Regime    string                  `json:"regime"`
	Totals    PatternValidationTotals `json:"totals"`
}

type PatternValidationRun struct {
	RunID       string                   `json:"run_id"`
	GeneratedAt time.Time                `json:"generated_at"`
	Request     PatternValidationRequest `json:"request"`
	Samples     int                      `json:"samples"`
}
