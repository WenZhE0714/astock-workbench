package domain

import "time"

type PlanMonitorRule struct {
	Version       string          `json:"version"`
	StructureID   string          `json:"structure_id"`
	Kind          string          `json:"kind,omitempty"`
	Description   string          `json:"description"`
	Levels        ChartPlanLevels `json:"levels"`
	BreakoutPrice float64         `json:"breakout_price"`
	VolumeDays    int             `json:"volume_days"`
	MinimumVolume float64         `json:"minimum_volume_ratio"`
	RequireTrend  bool            `json:"require_trend,omitempty"`
	CooldownSecs  int             `json:"cooldown_seconds"`
}

type PlanMonitorEvent struct {
	ID         string    `json:"id"`
	Sequence   uint64    `json:"sequence"`
	Kind       string    `json:"kind"`
	Message    string    `json:"message"`
	ObservedAt time.Time `json:"observed_at"`
	QuoteAt    time.Time `json:"quote_at,omitzero"`
	DataDate   string    `json:"data_date,omitempty"`
	Price      *float64  `json:"price,omitempty"`
	Notify     bool      `json:"notify"`
}

// PlanMonitor is mutable observation state. The original TradePlan remains
// immutable, and none of these phases imply an order or a fill.
type PlanMonitor struct {
	Version            int                `json:"version"`
	PlanID             string             `json:"plan_id"`
	Symbol             string             `json:"symbol"`
	Name               string             `json:"name"`
	AnalysisDate       string             `json:"analysis_date"`
	Fingerprint        string             `json:"fingerprint"`
	ExpiresOn          string             `json:"expires_on"`
	Rule               PlanMonitorRule    `json:"rule"`
	Enabled            bool               `json:"enabled"`
	Phase              string             `json:"phase"`
	EnabledAt          time.Time          `json:"enabled_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
	LastCheckedAt      time.Time          `json:"last_checked_at,omitzero"`
	LastClosingSlot    string             `json:"last_closing_slot,omitempty"`
	LastQuoteAt        time.Time          `json:"last_quote_at,omitzero"`
	LastBarDate        string             `json:"last_bar_date,omitempty"`
	LastBarFingerprint string             `json:"last_bar_fingerprint,omitempty"`
	ConfirmedOn        string             `json:"confirmed_on,omitempty"`
	LastZoneAlertAt    time.Time          `json:"last_zone_alert_at,omitzero"`
	Price              *float64           `json:"price,omitempty"`
	QuoteAt            time.Time          `json:"quote_at,omitzero"`
	HistoryDate        string             `json:"history_date,omitempty"`
	DataStatus         string             `json:"data_status"`
	DataMessage        string             `json:"data_message"`
	CalendarBasis      string             `json:"calendar_basis,omitempty"`
	Sequence           uint64             `json:"sequence"`
	Events             []PlanMonitorEvent `json:"events"`
}
