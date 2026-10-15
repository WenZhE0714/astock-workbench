package paper

import "time"

type ShadowDailyCheckIssue struct {
	ProfileID string `json:"profile_id,omitempty"`
	Code      string `json:"code"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

type ShadowDailyCheck struct {
	Date            string                  `json:"date"`
	CheckedAt       time.Time               `json:"checked_at"`
	Status          string                  `json:"status"`
	AccountStatus   string                  `json:"account_status"`
	Accounts        int                     `json:"accounts"`
	NotStarted      int                     `json:"not_started"`
	SealedAccounts  int                     `json:"sealed_accounts"`
	TradedAccounts  int                     `json:"traded_accounts"`
	Orders          int                     `json:"orders"`
	Fees            float64                 `json:"fees"`
	RealizedProfit  float64                 `json:"realized_profit"`
	SourceSignature string                  `json:"source_signature"`
	Issues          []ShadowDailyCheckIssue `json:"issues"`
}
