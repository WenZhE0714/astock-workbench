package domain

import "time"

// MarketSentimentSnapshot is a point-in-time, explicitly coverage-aware
// market emotion snapshot. Missing limit-up/limit-down data is represented by
// availability flags rather than zero values.
type MarketSentimentSnapshot struct {
	GeneratedAt          time.Time                 `json:"generated_at"`
	TradeDate            string                    `json:"trade_date,omitempty"`
	Source               string                    `json:"source,omitempty"`
	Score                float64                   `json:"score"`
	Phase                string                    `json:"phase"`
	CoveragePercent      float64                   `json:"coverage_percent"`
	IndexSignal          float64                   `json:"index_signal"`
	TurnoverSignal       float64                   `json:"turnover_signal"`
	IndustryBreadth      float64                   `json:"industry_breadth"`
	PositiveIndustryRate float64                   `json:"positive_industry_rate"`
	IndustryFlowSignal   float64                   `json:"industry_flow_signal"`
	RiseCount            int                       `json:"rise_count,omitempty"`
	FallCount            int                       `json:"fall_count,omitempty"`
	FlatCount            int                       `json:"flat_count,omitempty"`
	IndustryCount        int                       `json:"industry_count,omitempty"`
	LimitUpCount         int                       `json:"limit_up_count,omitempty"`
	LimitDownCount       int                       `json:"limit_down_count,omitempty"`
	LimitCountsAvailable bool                      `json:"limit_counts_available"`
	BrokenCount          int                       `json:"broken_count,omitempty"`
	BrokenRate           float64                   `json:"broken_rate_percent,omitempty"`
	HighestStreak        int                       `json:"highest_streak,omitempty"`
	StreakLadder         []LimitStreakGroup        `json:"streak_ladder,omitempty"`
	LimitStatsAvailable  bool                      `json:"limit_stats_available"`
	LimitStatsSource     string                    `json:"limit_stats_source,omitempty"`
	Warnings             []string                  `json:"warnings,omitempty"`
	StrongIndustries     []MarketSentimentIndustry `json:"strong_industries,omitempty"`
	WeakIndustries       []MarketSentimentIndustry `json:"weak_industries,omitempty"`
	NorthboundNet        float64                   `json:"northbound_net_hundred_million_yuan"`
	NorthboundSignal     float64                   `json:"northbound_signal"`
	NorthboundAvailable  bool                      `json:"northbound_available"`
	NorthboundAt         time.Time                 `json:"northbound_at,omitempty"`
	HotStockCount        int                       `json:"hot_stock_count,omitempty"`
	HotThemeCount        int                       `json:"hot_theme_count,omitempty"`
	HotSignalAvailable   bool                      `json:"hot_signal_available"`
	HotThemes            []HotTheme                `json:"hot_themes,omitempty"`
}

// MarketSentimentIndustry is a coverage-aware ranking item derived from the
// available industry breadth, price and main-fund direction fields.
type MarketSentimentIndustry struct {
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Percent   float64 `json:"percent"`
	MainNet   float64 `json:"main_net_yuan"`
	RiseCount int     `json:"rise_count"`
	FallCount int     `json:"fall_count"`
	FlatCount int     `json:"flat_count"`
	Breadth   float64 `json:"breadth"`
	Score     float64 `json:"score"`
}

type MarketSentimentPoint struct {
	At                   time.Time `json:"at"`
	Score                float64   `json:"score"`
	Phase                string    `json:"phase"`
	IndexSignal          float64   `json:"index_signal"`
	TurnoverSignal       float64   `json:"turnover_signal"`
	IndustryBreadth      float64   `json:"industry_breadth"`
	PositiveIndustryRate float64   `json:"positive_industry_rate"`
	IndustryFlowSignal   float64   `json:"industry_flow_signal"`
	NorthboundSignal     float64   `json:"northbound_signal"`
}
