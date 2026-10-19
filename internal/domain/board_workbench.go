package domain

import "time"

type BoardObservation struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Source    string   `json:"source"`
	Percent   *float64 `json:"percent"`
	MainNet   *float64 `json:"main_net_yuan"`
	MainRatio *float64 `json:"main_ratio_percent"`
	Turnover  *float64 `json:"turnover_percent"`
	RiseCount int      `json:"rise_count"`
	FallCount int      `json:"fall_count"`
	Leader    string   `json:"leader_name"`
}

type BoardSignal struct {
	Symbol   string    `json:"symbol"`
	Name     string    `json:"name"`
	Industry string    `json:"industry"`
	State    string    `json:"state"`
	Score    float64   `json:"score"`
	Price    float64   `json:"price"`
	Reason   string    `json:"reason"`
	AsOf     time.Time `json:"as_of"`
}

type BoardHolding struct {
	Profile     string    `json:"profile"`
	ProfileName string    `json:"profile_name"`
	Symbol      string    `json:"symbol"`
	Name        string    `json:"name"`
	Industry    string    `json:"industry"`
	Quantity    int       `json:"quantity"`
	Available   int       `json:"available"`
	MarketValue float64   `json:"market_value"`
	AsOf        time.Time `json:"as_of"`
}

// CapturedAt is local observation time, not a supplier tick timestamp.
// Linked signals and holdings keep their own timestamps inside the snapshot.
type BoardSnapshot struct {
	Version       int                `json:"version"`
	ID            string             `json:"id"`
	Date          string             `json:"date"`
	CapturedAt    time.Time          `json:"captured_at"`
	CalendarKnown bool               `json:"calendar_known"`
	Items         []BoardObservation `json:"items"`
	Signals       []BoardSignal      `json:"signals"`
	Holdings      []BoardHolding     `json:"holdings"`
	Warnings      []string           `json:"warnings,omitempty"`
}

type BoardDailyFlow struct {
	Date      string  `json:"date"`
	MainNet   float64 `json:"main_net_yuan"`
	MainRatio float64 `json:"main_ratio_percent"`
}
