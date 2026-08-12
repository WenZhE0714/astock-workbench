package domain

import "time"

// BoardFundRankingItem combines an industry-board fund snapshot with its
// highest-turnover constituents. Leaders are a liquidity ranking, not an
// official designation of industry leadership.
type BoardFundRankingItem struct {
	Board   BoardFlow             `json:"board"`
	Leaders []MarketStockSnapshot `json:"leaders"`
}

type BoardFundDashboard struct {
	RefreshedAt time.Time              `json:"refreshed_at"`
	Inflows     []BoardFundRankingItem `json:"inflows"`
	Outflows    []BoardFundRankingItem `json:"outflows"`
	Warnings    []string               `json:"warnings,omitempty"`
}
