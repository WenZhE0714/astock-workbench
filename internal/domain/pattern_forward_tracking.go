package domain

import "time"

// Evidence contains only snapshots published during their own collection day.
type PatternForwardEvidence struct {
	Day       PatternForwardDay        `json:"day"`
	Benchmark *PatternForwardBenchmark `json:"benchmark,omitempty"`
	Stocks    []PatternForwardSnapshot `json:"stocks"`
}

type PatternForwardProgress struct {
	Sample          PatternValidationSample `json:"sample"`
	ObservationHash string                  `json:"observation_hash"`
	AsOf            string                  `json:"as_of"`
	Finished        bool                    `json:"finished"`
	MissingDates    []string                `json:"missing_dates"`
	Detail          string                  `json:"detail,omitempty"`
}

type PatternForwardTracking struct {
	Version      string                   `json:"version"`
	TaskID       string                   `json:"task_id"`
	ConfigHash   string                   `json:"config_hash"`
	Date         string                   `json:"date"`
	EvaluatedAt  time.Time                `json:"evaluated_at"`
	PreviousHash string                   `json:"previous_hash"`
	TradingDates []string                 `json:"trading_dates"`
	EvidenceHash string                   `json:"evidence_hash"`
	Records      []PatternForwardProgress `json:"records"`
	Hash         string                   `json:"hash"`
}
