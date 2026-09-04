package domain

// LimitStatsSnapshot is the market-wide limit-up/limit-down structure for a
// trading date. Counts are valid only when Available is true.
type LimitStatsSnapshot struct {
	TradeDate      string             `json:"trade_date"`
	LimitUpCount   int                `json:"limit_up_count"`
	LimitDownCount int                `json:"limit_down_count"`
	BrokenCount    int                `json:"broken_count"`
	BrokenRate     float64            `json:"broken_rate_percent"`
	HighestStreak  int                `json:"highest_streak"`
	StreakLadder   []LimitStreakGroup `json:"streak_ladder,omitempty"`
	Available      bool               `json:"available"`
	Source         string             `json:"source,omitempty"`
}

type LimitStreakGroup struct {
	Streak int                `json:"streak"`
	Count  int                `json:"count"`
	Stocks []LimitStreakStock `json:"stocks,omitempty"`
}

type LimitStreakStock struct {
	Symbol  string  `json:"symbol"`
	Name    string  `json:"name"`
	Percent float64 `json:"percent,omitempty"`
}
