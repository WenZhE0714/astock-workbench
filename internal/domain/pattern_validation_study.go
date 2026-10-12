package domain

import "time"

type PatternPeriodCheck struct {
	State  string `json:"state"`
	Cutoff string `json:"cutoff"`
	Detail string `json:"detail"`
}

type PatternStudyConfig struct {
	AlgorithmVersion string   `json:"algorithm_version"`
	PatternVersion   string   `json:"pattern_version"`
	Patterns         []string `json:"patterns"`
	Symbols          []string `json:"symbols"`
	PriceBasis       string   `json:"price_basis"`
	Start            string   `json:"start"`
	ResearchEnd      string   `json:"research_end"`
	ValidationStart  string   `json:"validation_start"`
	End              string   `json:"end"`
	DataCutoff       string   `json:"data_cutoff"`
	Horizons         []int    `json:"horizons"`
	WarmupDays       int      `json:"warmup_days"`
	LookbackBars     int      `json:"lookback_bars"`
	PivotRadius      int      `json:"pivot_radius"`
	ConfirmationBars int      `json:"confirmation_bars"`
	PriceMethod      string   `json:"price_method"`
	VolumeMethod     string   `json:"volume_method"`
	QualityMethod    string   `json:"quality_method"`
	BenchmarkMethod  string   `json:"benchmark_method"`
}

type PatternResearchEvidence struct {
	Cutoff         string                      `json:"cutoff"`
	InputHash      string                      `json:"input_hash"`
	Coverage       []PatternValidationCoverage `json:"coverage"`
	ActionCoverage []CorporateActionSnapshot   `json:"action_coverage,omitempty"`
	Adjustments    []PatternAdjustment         `json:"adjustments,omitempty"`
}

type PatternValidationStudy struct {
	Method     string                  `json:"method"`
	Kind       string                  `json:"kind"`
	FrozenAt   time.Time               `json:"frozen_at"`
	Config     PatternStudyConfig      `json:"config"`
	ConfigHash string                  `json:"config_hash"`
	Research   PatternResearchEvidence `json:"research"`
}

type PatternPeriodSummary struct {
	Period string                   `json:"period"`
	Start  string                   `json:"start"`
	End    string                   `json:"end"`
	Totals PatternValidationTotals  `json:"totals"`
	Groups []PatternValidationGroup `json:"groups"`
}
