package domain

import "time"

type ChartLevel struct {
	Key   string   `json:"key"`
	Label string   `json:"label"`
	Value *float64 `json:"value"`
	Upper *float64 `json:"upper,omitempty"`
	Date  string   `json:"date,omitempty"`
	Basis string   `json:"basis"`
}

type ChartAnchor struct {
	Date  string  `json:"date"`
	Price float64 `json:"price"`
	Label string  `json:"label"`
}

// ChartPlanLevels describes an observation, never an executable order.
type ChartPlanLevels struct {
	EntryLow     float64 `json:"entry_low"`
	EntryHigh    float64 `json:"entry_high"`
	Invalidation float64 `json:"invalidation"`
	Target1      float64 `json:"target_1"`
	Target2      float64 `json:"target_2"`
	RiskPerShare float64 `json:"risk_per_share"`
	RewardRisk   float64 `json:"reward_risk"`
	Confirmation string  `json:"confirmation"`
	RiskBasis    string  `json:"risk_basis"`
}

type ChartStructure struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	State    string           `json:"state"`
	Anchors  []ChartAnchor    `json:"anchors"`
	Evidence []string         `json:"evidence"`
	Plan     *ChartPlanLevels `json:"plan,omitempty"`
}

type ChartWeeklyTrend struct {
	State    string   `json:"state"`
	DataDate string   `json:"data_date,omitempty"`
	Weeks    int      `json:"weeks"`
	MA5      *float64 `json:"ma5"`
	MA10     *float64 `json:"ma10"`
}

type ChartAnalysis struct {
	Version     string           `json:"version"`
	Fingerprint string           `json:"fingerprint"`
	Symbol      string           `json:"symbol"`
	Source      string           `json:"source"`
	Timeframe   string           `json:"timeframe"`
	PriceBasis  string           `json:"price_basis"`
	DataDate    string           `json:"data_date"`
	Complete    bool             `json:"complete"`
	BarsUsed    int              `json:"bars_used"`
	Price       float64          `json:"price"`
	RangeHigh   float64          `json:"range_high"`
	RangeLow    float64          `json:"range_low"`
	VolumeRatio *float64         `json:"volume_ratio"`
	Levels      []ChartLevel     `json:"levels"`
	Structures  []ChartStructure `json:"structures"`
	Weekly      ChartWeeklyTrend `json:"weekly"`
	Warnings    []string         `json:"warnings,omitempty"`
}

// TradePlan freezes the analysis visible at creation. Monitoring and fills
// have separate lifecycles and must not rewrite this research snapshot.
type TradePlan struct {
	ID        string         `json:"id"`
	Version   int            `json:"version"`
	Symbol    string         `json:"symbol"`
	CreatedAt time.Time      `json:"created_at"`
	ExpiresOn string         `json:"expires_on"`
	Analysis  ChartAnalysis  `json:"analysis"`
	Structure ChartStructure `json:"structure"`
	// MonitorRule is present only for a user-confirmed assistant rule. Legacy
	// chart plans derive the same frozen rule from Structure.ID.
	MonitorRule *PlanMonitorRule `json:"monitor_rule,omitempty"`
}

// AssistantChartContext is recomputed and verified by the server before it is
// supplied to an AI. Client-provided prices are never trusted as chart facts.
type AssistantChartContext struct {
	Version           string         `json:"version"`
	Symbol            string         `json:"symbol"`
	Timeframe         string         `json:"timeframe"`
	VisibleFrom       string         `json:"visible_from,omitempty"`
	VisibleTo         string         `json:"visible_to,omitempty"`
	SelectedStructure ChartStructure `json:"selected_structure"`
	Analysis          ChartAnalysis  `json:"analysis"`
}

// AssistantRuleProposal is the small, bounded language an AI may propose.
// It is not executable until normalized, fingerprinted and confirmed.
type AssistantRuleProposal struct {
	Kind               string  `json:"kind"`
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	EntryLow           float64 `json:"entry_low"`
	EntryHigh          float64 `json:"entry_high"`
	Invalidation       float64 `json:"invalidation"`
	ConfirmationPrice  float64 `json:"confirmation_price"`
	VolumeDays         int     `json:"volume_days"`
	MinimumVolumeRatio float64 `json:"minimum_volume_ratio"`
	RequireTrend       bool    `json:"require_trend"`
}

type AssistantRuleDraft struct {
	Version             string                `json:"version"`
	ID                  string                `json:"id"`
	Symbol              string                `json:"symbol"`
	CreatedAt           time.Time             `json:"created_at"`
	ExpiresOn           string                `json:"expires_on"`
	SourceQuestion      string                `json:"source_question"`
	AnalysisDate        string                `json:"analysis_date"`
	AnalysisFingerprint string                `json:"analysis_fingerprint"`
	StructureID         string                `json:"structure_id"`
	Proposal            AssistantRuleProposal `json:"proposal"`
	Warnings            []string              `json:"warnings,omitempty"`
}
