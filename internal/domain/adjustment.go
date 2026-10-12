package domain

import "time"

const PatternPriceRaw = "raw"
const PatternPriceHFQ = "hfq"
const PatternAdjustmentMethod = "sina-hfq-first-bar-v1"

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
	Symbol        string    `json:"symbol"`
	Status        string    `json:"status"`
	Method        string    `json:"method"`
	Start         string    `json:"start"`
	End           string    `json:"end"`
	AnchorDate    string    `json:"anchor_date,omitempty"`
	AnchorFactor  float64   `json:"anchor_factor,omitempty"`
	FactorVersion string    `json:"factor_version,omitempty"`
	FactorCount   int       `json:"factor_count"`
	Source        string    `json:"source"`
	SourceURL     string    `json:"source_url"`
	FetchedAt     time.Time `json:"fetched_at"`
	Error         string    `json:"error,omitempty"`
}
