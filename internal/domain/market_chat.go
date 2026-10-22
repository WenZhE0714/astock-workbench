package domain

import "time"

const MarketChatSymbol = "market"

// MarketChatFacts deliberately excludes company announcements and stock boards.
type MarketChatFacts struct {
	GeneratedAt  time.Time         `json:"generated_at"`
	SnapshotHash string            `json:"snapshot_hash"`
	MarketStatus string            `json:"market_status"`
	Indices      []MarketChatIndex `json:"indices"`
	Breadth      MarketChatBreadth `json:"industry_breadth"`
	Amount       MarketChatAmount  `json:"turnover"`
	Warnings     []string          `json:"warnings"`
}

type MarketChatIndex struct {
	Quote      StockQuoteSnapshot `json:"quote"`
	Technical  TechnicalSignal    `json:"technical"`
	RecentBars []MarketChatBar    `json:"recent_completed_daily_bars"`
	Fund       *FundFlow          `json:"cumulative_fund,omitempty"`
}

type MarketChatBar struct {
	Date   string   `json:"date"`
	Close  float64  `json:"close"`
	Volume *float64 `json:"volume"`
	Amount *float64 `json:"amount_yuan"`
}

type MarketChatBreadth struct {
	Available  bool                    `json:"available"`
	Scope      string                  `json:"scope"`
	Industries int                     `json:"industries"`
	Rising     int                     `json:"rising"`
	Falling    int                     `json:"falling"`
	Flat       int                     `json:"flat"`
	Missing    int                     `json:"missing"`
	Strong     []MarketBoardAssessment `json:"strong"`
	Weak       []MarketBoardAssessment `json:"weak"`
}

type MarketChatAmount struct {
	Scope         string  `json:"scope"`
	Available     bool    `json:"available"`
	Current       float64 `json:"current_wan_yuan"`
	Previous      float64 `json:"previous_wan_yuan"`
	PreviousDate  string  `json:"previous_date"`
	Comparable    bool    `json:"comparable"`
	ChangePercent float64 `json:"change_percent"`
}

type MarketChatPrediction struct {
	TradingDays int    `json:"trading_days"`
	Direction   string `json:"direction"`
	Reason      string `json:"reason"`
}

type MarketChatAssessment struct {
	ID                 string                 `json:"id"`
	AnchorSymbol       string                 `json:"anchor_symbol"`
	BaseDate           string                 `json:"base_date"`
	BaseClose          float64                `json:"base_close"`
	NeutralBandPercent float64                `json:"neutral_band_percent"`
	Evaluable          bool                   `json:"evaluable"`
	LimitReason        string                 `json:"limit_reason,omitempty"`
	Predictions        []MarketChatPrediction `json:"predictions"`
}

type MarketChatRecord struct {
	SchemaVersion int                  `json:"schema_version"`
	Assessment    MarketChatAssessment `json:"assessment"`
	Facts         MarketChatFacts      `json:"facts"`
	Question      string               `json:"question"`
	Answer        string               `json:"answer"`
}

type MarketChatOutcome struct {
	TradingDays       int     `json:"trading_days"`
	Direction         string  `json:"direction"`
	Status            string  `json:"status"`
	ObservedDays      int     `json:"observed_days"`
	EndDate           string  `json:"end_date,omitempty"`
	ReturnPercent     float64 `json:"return_percent"`
	RealizedDirection string  `json:"realized_direction,omitempty"`
	Matched           *bool   `json:"matched,omitempty"`
}

type MarketChatReview struct {
	Assessment MarketChatAssessment `json:"assessment"`
	CheckedAt  time.Time            `json:"checked_at"`
	Outcomes   []MarketChatOutcome  `json:"outcomes"`
}
