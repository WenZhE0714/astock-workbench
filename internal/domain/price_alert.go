package domain

import "time"

type PriceAlertSource struct {
	ID          string `json:"id"`
	Date        string `json:"date"`
	Fingerprint string `json:"fingerprint"`
}

type PriceAlertRule struct {
	Symbol    string            `json:"symbol"`
	Name      string            `json:"name"`
	Direction string            `json:"direction"`
	Price     float64           `json:"price"`
	ExpiresOn string            `json:"expires_on"`
	Source    *PriceAlertSource `json:"source,omitempty"`
}

type PriceAlertEvent struct {
	ID         string    `json:"id"`
	ObservedAt time.Time `json:"observed_at"`
	QuoteAt    time.Time `json:"quote_at"`
	Price      float64   `json:"price"`
}

type PriceAlert struct {
	ID            string           `json:"id"`
	Rule          PriceAlertRule   `json:"rule"`
	Revision      uint64           `json:"revision"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
	EnabledAt     time.Time        `json:"enabled_at"`
	Enabled       bool             `json:"enabled"`
	DeletedAt     time.Time        `json:"deleted_at,omitzero"`
	LastCheckedAt time.Time        `json:"last_checked_at,omitzero"`
	QuoteAt       time.Time        `json:"quote_at,omitzero"`
	QuotePrice    *float64         `json:"quote_price,omitempty"`
	CheckState    string           `json:"check_state"`
	CheckDetail   string           `json:"check_detail,omitempty"`
	Event         *PriceAlertEvent `json:"event,omitempty"`
}

type PriceAlertState struct {
	Version int                   `json:"version"`
	Alerts  map[string]PriceAlert `json:"alerts"`
}
