package domain

import "time"

type TradePlanReviewRevision struct {
	Sequence        uint64    `json:"sequence"`
	UpdatedAt       time.Time `json:"updated_at"`
	Note            string    `json:"note,omitempty"`
	Tags            []string  `json:"tags,omitempty"`
	ExecutionStatus string    `json:"execution_status"`
	ActualEntry     *float64  `json:"actual_entry,omitempty"`
	ActualExit      *float64  `json:"actual_exit,omitempty"`
	EntryAt         time.Time `json:"entry_at,omitzero"`
	ExitAt          time.Time `json:"exit_at,omitzero"`
	ExitReason      string    `json:"exit_reason,omitempty"`
	Discipline      string    `json:"discipline,omitempty"`
	RealizedR       *float64  `json:"realized_r,omitempty"`
}

// TradePlanReview is mutable user research attached to an immutable plan.
// Revisions are append-only so later edits cannot erase the decision trail.
type TradePlanReview struct {
	Version     int                       `json:"version"`
	PlanID      string                    `json:"plan_id"`
	Symbol      string                    `json:"symbol"`
	Fingerprint string                    `json:"fingerprint"`
	UpdatedAt   time.Time                 `json:"updated_at"`
	Sequence    uint64                    `json:"sequence"`
	Current     TradePlanReviewRevision   `json:"current"`
	Revisions   []TradePlanReviewRevision `json:"revisions"`
}

type TradePlaybookMetric struct {
	Key                   string   `json:"key"`
	Label                 string   `json:"label"`
	Plans                 int      `json:"plans"`
	Reviewed              int      `json:"reviewed"`
	Entered               int      `json:"entered"`
	Completed             int      `json:"completed"`
	Wins                  int      `json:"wins"`
	AverageR              *float64 `json:"average_r,omitempty"`
	MedianR               *float64 `json:"median_r,omitempty"`
	WinRatePercent        *float64 `json:"win_rate_percent,omitempty"`
	DisciplineSamples     int      `json:"discipline_samples"`
	DisciplineRatePercent *float64 `json:"discipline_rate_percent,omitempty"`
	SampleSufficient      bool     `json:"sample_sufficient"`
}

type TradePlaybookDistribution struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type TradePlaybookItem struct {
	PlanID          string    `json:"plan_id"`
	Symbol          string    `json:"symbol"`
	Name            string    `json:"name,omitempty"`
	StructureID     string    `json:"structure_id"`
	StructureName   string    `json:"structure_name"`
	CreatedAt       time.Time `json:"created_at"`
	ExpiresOn       string    `json:"expires_on"`
	EntryLow        float64   `json:"entry_low,omitempty"`
	EntryHigh       float64   `json:"entry_high,omitempty"`
	Invalidation    float64   `json:"invalidation,omitempty"`
	Target2         float64   `json:"target_2,omitempty"`
	Reviewed        bool      `json:"reviewed"`
	ReviewUpdatedAt time.Time `json:"review_updated_at,omitzero"`
	ExecutionStatus string    `json:"execution_status,omitempty"`
	Discipline      string    `json:"discipline,omitempty"`
	Tags            []string  `json:"tags,omitempty"`
	ExitReason      string    `json:"exit_reason,omitempty"`
	RealizedR       *float64  `json:"realized_r,omitempty"`
	EntryAt         time.Time `json:"entry_at,omitzero"`
	ExitAt          time.Time `json:"exit_at,omitzero"`
	ActualEntry     *float64  `json:"actual_entry,omitempty"`
	ActualExit      *float64  `json:"actual_exit,omitempty"`
	StatisticsNote  string    `json:"statistics_note,omitempty"`
}

type TradePlaybookReport struct {
	GeneratedAt           time.Time                   `json:"generated_at"`
	TotalPlans            int                         `json:"total_plans"`
	ReviewedPlans         int                         `json:"reviewed_plans"`
	EnteredPlans          int                         `json:"entered_plans"`
	CompletedTrades       int                         `json:"completed_trades"`
	Wins                  int                         `json:"wins"`
	Losses                int                         `json:"losses"`
	BreakEven             int                         `json:"break_even"`
	SkippedPlans          int                         `json:"skipped_plans"`
	DeviatedPlans         int                         `json:"deviated_plans"`
	AverageR              *float64                    `json:"average_r,omitempty"`
	MedianR               *float64                    `json:"median_r,omitempty"`
	WinRatePercent        *float64                    `json:"win_rate_percent,omitempty"`
	DisciplineSamples     int                         `json:"discipline_samples"`
	DisciplineRatePercent *float64                    `json:"discipline_rate_percent,omitempty"`
	EntryRatePercent      *float64                    `json:"entry_rate_percent,omitempty"`
	MinimumCompleted      int                         `json:"minimum_completed"`
	Setups                []TradePlaybookMetric       `json:"setups"`
	Tags                  []TradePlaybookMetric       `json:"tags"`
	RDistribution         []TradePlaybookDistribution `json:"r_distribution"`
	Recent                []TradePlaybookItem         `json:"recent"`
	Insights              []string                    `json:"insights,omitempty"`
	Warnings              []string                    `json:"warnings,omitempty"`
}
