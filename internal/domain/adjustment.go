package domain

import "time"

const PatternPriceRaw = "raw"
const PatternPriceHFQ = "hfq"
const PatternAdjustmentMethod = "sina-hfq-first-bar-v1"
const PatternVolumeMethod = "first-bar-shares-v1"
const PatternAdjustmentQualityMethod = "ex-price-one-tick-v1"

type AdjustmentFactor struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

type AdjustmentSnapshot struct {
	Symbol    string             `json:"symbol"`
	Start     string             `json:"start"`
	End       string             `json:"end"`
	Status    string             `json:"status"`
	Source    string             `json:"source"`
	SourceURL string             `json:"source_url"`
	FetchedAt time.Time          `json:"fetched_at"`
	Factors   []AdjustmentFactor `json:"factors"`
	Error     string             `json:"error,omitempty"`
}

type PatternAdjustment struct {
	Symbol        string                   `json:"symbol"`
	Status        string                   `json:"status"`
	Method        string                   `json:"method"`
	Start         string                   `json:"start"`
	End           string                   `json:"end"`
	AnchorDate    string                   `json:"anchor_date,omitempty"`
	AnchorFactor  float64                  `json:"anchor_factor,omitempty"`
	FactorVersion string                   `json:"factor_version,omitempty"`
	FactorCount   int                      `json:"factor_count"`
	Source        string                   `json:"source"`
	SourceURL     string                   `json:"source_url"`
	FetchedAt     time.Time                `json:"fetched_at"`
	Error         string                   `json:"error,omitempty"`
	VolumeMethod  string                   `json:"volume_method,omitempty"`
	VolumeVersion string                   `json:"volume_version,omitempty"`
	QualityMethod string                   `json:"quality_method,omitempty"`
	QualityStatus string                   `json:"quality_status,omitempty"`
	Quality       []AdjustmentQualityCheck `json:"quality,omitempty"`
}

type AdjustmentQualityCheck struct {
	Date            string   `json:"date"`
	EventIDs        []string `json:"event_ids,omitempty"`
	State           string   `json:"state"`
	Code            string   `json:"code"`
	Detail          string   `json:"detail"`
	PreviousDate    string   `json:"previous_date,omitempty"`
	PreviousClose   *float64 `json:"previous_close,omitempty"`
	ShareRatio      *float64 `json:"share_ratio,omitempty"`
	ExpectedExPrice *float64 `json:"expected_ex_price,omitempty"`
	ImpliedExPrice  *float64 `json:"implied_ex_price,omitempty"`
	ExpectedRatio   *float64 `json:"expected_ratio,omitempty"`
	ObservedRatio   *float64 `json:"observed_ratio,omitempty"`
	PriceError      *float64 `json:"price_error,omitempty"`
	Tolerance       float64  `json:"tolerance"`
}
