package domain

import "time"

const CorporateActionScope = "cash-dividend,bonus-share,capitalization"

type CorporateAction struct {
	ID          string   `json:"id"`
	Symbol      string   `json:"symbol"`
	ExDate      string   `json:"ex_date"`
	RecordDate  string   `json:"record_date,omitempty"`
	NoticeDate  string   `json:"notice_date,omitempty"`
	Kinds       []string `json:"kinds"`
	Description string   `json:"description"`
	Source      string   `json:"source"`
	SourceURL   string   `json:"source_url"`
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
