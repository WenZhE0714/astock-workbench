package domain

import "time"

const PatternPoolLimit = 50

type PatternPoolMember struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Industry string `json:"industry"`
}

type PatternPool struct {
	Version   string              `json:"version"`
	Name      string              `json:"name"`
	Source    string              `json:"source"`
	CreatedAt time.Time           `json:"created_at"`
	Members   []PatternPoolMember `json:"members"`
}

type PatternBatchItem struct {
	Symbol    string `json:"symbol"`
	State     string `json:"state"`
	Attempts  int    `json:"attempts"`
	InputHash string `json:"input_hash,omitempty"`
	Error     string `json:"error,omitempty"`
}

type PatternBatchJob struct {
	ID               string                   `json:"id"`
	Pool             PatternPool              `json:"pool"`
	Request          PatternValidationRequest `json:"request"`
	AlgorithmVersion string                   `json:"algorithm_version"`
	ConfigHash       string                   `json:"config_hash"`
	CreatedAt        time.Time                `json:"created_at"`
	UpdatedAt        time.Time                `json:"updated_at"`
	Status           string                   `json:"status"`
	Phase            string                   `json:"phase"`
	CurrentSymbol    string                   `json:"current_symbol,omitempty"`
	BenchmarkHash    string                   `json:"benchmark_hash,omitempty"`
	Items            []PatternBatchItem       `json:"items"`
	RunIDs           []string                 `json:"run_ids"`
	Error            string                   `json:"error,omitempty"`
}

type PatternStockSummary struct {
	Symbol      string                  `json:"symbol"`
	Name        string                  `json:"name"`
	Industry    string                  `json:"industry"`
	SampleShare float64                 `json:"sample_share"`
	Totals      PatternValidationTotals `json:"totals"`
}

type PatternIndustrySummary struct {
	Industry    string                  `json:"industry"`
	Stocks      int                     `json:"stocks"`
	SampleShare float64                 `json:"sample_share"`
	Totals      PatternValidationTotals `json:"totals"`
}
