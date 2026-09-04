package domain

import "time"

// NorthboundFlowSnapshot is the latest intraday cumulative northbound flow.
// Values are in hundred-million yuan and may be unavailable outside sessions.
type NorthboundFlowSnapshot struct {
	At        time.Time `json:"at"`
	Shanghai  float64   `json:"shanghai_hundred_million_yuan"`
	Shenzhen  float64   `json:"shenzhen_hundred_million_yuan"`
	Total     float64   `json:"total_hundred_million_yuan"`
	Available bool      `json:"available"`
	Source    string    `json:"source,omitempty"`
}

type HotTheme struct {
	Name        string  `json:"name"`
	Count       int     `json:"count"`
	Leader      string  `json:"leader,omitempty"`
	AverageRise float64 `json:"average_rise_percent,omitempty"`
	Amount      float64 `json:"amount_yuan,omitempty"`
}

type HotStockSignal struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	Percent   float64 `json:"percent"`
	Turnover  float64 `json:"turnover_percent"`
	Amount    float64 `json:"amount_yuan"`
	MainRatio float64 `json:"main_ratio"`
	Reason    string  `json:"reason,omitempty"`
}

type HotStockSnapshot struct {
	TradeDate string           `json:"trade_date"`
	Stocks    []HotStockSignal `json:"stocks"`
	Themes    []HotTheme       `json:"themes"`
	Available bool             `json:"available"`
	Source    string           `json:"source,omitempty"`
}
