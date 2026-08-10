// Package paper will implement a deterministic simulated broker. The model is
// already separated from research and live execution so fills can later honor
// A-share lot size, T+1, limit-up/down and suspension constraints.
package paper

import "time"

type Position struct {
	Ticker            string  `json:"ticker"`
	Quantity          int     `json:"quantity"`
	AvailableQuantity int     `json:"available_quantity"`
	AverageCost       float64 `json:"average_cost"`
}

type Account struct {
	Cash      float64    `json:"cash"`
	Positions []Position `json:"positions"`
	UpdatedAt time.Time  `json:"updated_at"`
}
