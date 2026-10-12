package domain

import "time"

const CorporateActionScope = "cash-dividend,bonus-share,capitalization"

type DistributionTerms struct {
	CashPer10           *float64 `json:"cash_per_10"`
	BonusPer10          *float64 `json:"bonus_per_10"`
	CapitalizationPer10 *float64 `json:"capitalization_per_10"`
}

type CorporateAction struct {
	ID          string             `json:"id"`
	Symbol      string             `json:"symbol"`
	ExDate      string             `json:"ex_date"`
	RecordDate  string             `json:"record_date,omitempty"`
	NoticeDate  string             `json:"notice_date,omitempty"`
	Kinds       []string           `json:"kinds"`
	Description string             `json:"description"`
	Source      string             `json:"source"`
	SourceURL   string             `json:"source_url"`
	Terms       *DistributionTerms `json:"terms,omitempty"`
}

type CorporateActionSnapshot struct {
	Symbol    string            `json:"symbol"`
	Start     string            `json:"start"`
	End       string            `json:"end"`
	Status    string            `json:"status"`
	Scope     string            `json:"scope"`
	Source    string            `json:"source"`
	SourceURL string            `json:"source_url,omitempty"`
	FetchedAt time.Time         `json:"fetched_at"`
	Events    []CorporateAction `json:"events"`
	Error     string            `json:"error,omitempty"`
}

type PatternActionCheck struct {
	State  string            `json:"state"`
	Start  string            `json:"start"`
	End    string            `json:"end"`
	Events []CorporateAction `json:"events,omitempty"`
	Detail string            `json:"detail"`
}
