package domain

type ChartTimeframeTrend struct {
	Timeframe      string   `json:"timeframe"`
	State          string   `json:"state"`
	DataDate       string   `json:"data_date,omitempty"`
	PeriodEnd      string   `json:"period_end,omitempty"`
	PeriodDays     int      `json:"period_days"`
	Samples        int      `json:"samples"`
	Required       int      `json:"required"`
	Close          *float64 `json:"close"`
	FastPeriod     int      `json:"fast_period"`
	SlowPeriod     int      `json:"slow_period"`
	FastMA         *float64 `json:"fast_ma"`
	SlowMA         *float64 `json:"slow_ma"`
	VolumeLookback int      `json:"volume_lookback"`
	VolumeRatio    *float64 `json:"volume_ratio"`
	RangeLookback  int      `json:"range_lookback"`
	RangeHigh      *float64 `json:"range_high"`
	RangeLow       *float64 `json:"range_low"`
	RangeState     string   `json:"range_state"`
}

type ChartTimeframeCheck struct {
	Key   string `json:"key"`
	State string `json:"state"`
	Text  string `json:"text"`
}

// This read-only comparison binds to a chart snapshot without extending or
// changing the persisted ChartAnalysis fingerprint contract.
type ChartTimeframeComparison struct {
	Version         string                `json:"version"`
	Symbol          string                `json:"symbol"`
	DataDate        string                `json:"data_date"`
	BaseFingerprint string                `json:"base_fingerprint"`
	Source          string                `json:"source"`
	PriceBasis      string                `json:"price_basis"`
	ExcludedDaily   int                   `json:"excluded_daily"`
	ReferenceOnly   bool                  `json:"reference_only"`
	Alignment       string                `json:"alignment"`
	Summary         string                `json:"summary"`
	Daily           ChartTimeframeTrend   `json:"daily"`
	Weekly          ChartTimeframeTrend   `json:"weekly"`
	Checks          []ChartTimeframeCheck `json:"checks"`
	Warnings        []string              `json:"warnings"`
}
