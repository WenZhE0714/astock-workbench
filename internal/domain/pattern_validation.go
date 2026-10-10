package domain

import "time"

type PatternValidationRequest struct {
	Symbols []string `json:"symbols"`
	Start   string   `json:"start"`
	End     string   `json:"end"`
}

type PatternValidationInput struct {
	Symbol string     `json:"symbol"`
	Bars   []DailyBar `json:"bars"`
	Error  string     `json:"error,omitempty"`
}

type PatternValidationCoverage struct {
	Symbol          string `json:"symbol"`
	FirstDate       string `json:"first_date,omitempty"`
	LastDate        string `json:"last_date,omitempty"`
	Bars            int    `json:"bars"`
	Source          string `json:"source,omitempty"`
	Cached          bool   `json:"cached"`
	LateDiscoveries int    `json:"late_discoveries"`
	Error           string `json:"error,omitempty"`
}

type PatternValidationOutcome struct {
	Horizon         int      `json:"horizon"`
	State           string   `json:"state"`
	Through         string   `json:"through,omitempty"`
	ObservedDays    int      `json:"observed_days"`
	PriceReturn     *float64 `json:"price_return"`
	DirectionReturn *float64 `json:"direction_return"`
	Favorable       *float64 `json:"favorable"`
	Adverse         *float64 `json:"adverse"`
	Invalidated     *bool    `json:"invalidated"`
	Detail          string   `json:"detail,omitempty"`
}

type PatternValidationSample struct {
	ID                  string                     `json:"id"`
	Symbol              string                     `json:"symbol"`
	PatternID           string                     `json:"pattern_id"`
	Name                string                     `json:"name"`
	Bias                string                     `json:"bias"`
	ObservedOn          string                     `json:"observed_on"`
	ConfirmedOn         string                     `json:"confirmed_on,omitempty"`
	InvalidatedOn       string                     `json:"invalidated_on,omitempty"`
	ExpiredOn           string                     `json:"expired_on,omitempty"`
	State               string                     `json:"state"`
	Regime              string                     `json:"regime"`
	RegimeDate          string                     `json:"regime_date"`
	ConfirmationClose   *float64                   `json:"confirmation_close"`
	SnapshotFingerprint string                     `json:"snapshot_fingerprint"`
	Snapshot            ChartStructure             `json:"snapshot"`
	Outcomes            []PatternValidationOutcome `json:"outcomes"`
}

type PatternValidationReport struct {
	Version     string                      `json:"version"`
	RunID       string                      `json:"run_id"`
	GeneratedAt time.Time                   `json:"generated_at"`
	Request     PatternValidationRequest    `json:"request"`
	Cutoff      string                      `json:"cutoff"`
	InputHash   string                      `json:"input_hash"`
	Horizons    []int                       `json:"horizons"`
	Coverage    []PatternValidationCoverage `json:"coverage"`
	Samples     []PatternValidationSample   `json:"samples"`
	Warnings    []string                    `json:"warnings"`
	Inputs      []PatternValidationInput    `json:"inputs,omitempty"`
}

type PatternValidationTotals struct {
	Samples                       int      `json:"samples"`
	Confirmed                     int      `json:"confirmed"`
	InvalidatedBeforeConfirmation int      `json:"invalidated_before_confirmation"`
	Mature                        int      `json:"mature"`
	Pending                       int      `json:"pending"`
	Unavailable                   int      `json:"unavailable"`
	AveragePriceReturn            *float64 `json:"average_price_return"`
	AverageDirectionReturn        *float64 `json:"average_direction_return"`
	AverageFavorable              *float64 `json:"average_favorable"`
	AverageAdverse                *float64 `json:"average_adverse"`
	InvalidationRate              *float64 `json:"invalidation_rate"`
}

type PatternValidationGroup struct {
	PatternID string                  `json:"pattern_id"`
	Name      string                  `json:"name"`
	Bias      string                  `json:"bias"`
	Regime    string                  `json:"regime"`
	Totals    PatternValidationTotals `json:"totals"`
}

type PatternValidationRun struct {
	RunID       string                   `json:"run_id"`
	GeneratedAt time.Time                `json:"generated_at"`
	Request     PatternValidationRequest `json:"request"`
	Samples     int                      `json:"samples"`
}
