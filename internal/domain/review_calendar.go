package domain

import "time"

type ReviewCalendarEvent struct {
	ID              string    `json:"id"`
	Date            string    `json:"date,omitempty"`
	At              time.Time `json:"at,omitzero"`
	Kind            string    `json:"kind"`
	Symbol          string    `json:"symbol"`
	Name            string    `json:"name,omitempty"`
	PlanID          string    `json:"plan_id,omitempty"`
	Label           string    `json:"label"`
	ExecutionStatus string    `json:"execution_status,omitempty"`
	Discipline      string    `json:"discipline,omitempty"`
	Tags            []string  `json:"tags,omitempty"`
	Note            string    `json:"note,omitempty"`
	R               *float64  `json:"r,omitempty"`
	NetProfit       *float64  `json:"net_profit,omitempty"`
	Quantity        int       `json:"quantity,omitempty"`
	Price           *float64  `json:"price,omitempty"`
}

type ReviewCalendarTotals struct {
	Plans      int      `json:"plans"`
	Entries    int      `json:"entries"`
	Exits      int      `json:"exits"`
	Reviews    int      `json:"reviews"`
	Deviations int      `json:"deviations"`
	AverageR   *float64 `json:"average_r"`
	NetProfit  *float64 `json:"net_profit"`
}

type ReviewCalendarDay struct {
	Date   string                `json:"date"`
	Totals ReviewCalendarTotals  `json:"totals"`
	Events []ReviewCalendarEvent `json:"events"`
}

type ReviewCalendarReport struct {
	Month       string                `json:"month"`
	Source      string                `json:"source"`
	Timezone    string                `json:"timezone"`
	GeneratedAt time.Time             `json:"generated_at"`
	AsOf        string                `json:"as_of,omitempty"`
	Days        []ReviewCalendarDay   `json:"days"`
	Totals      ReviewCalendarTotals  `json:"totals"`
	Undated     []ReviewCalendarEvent `json:"undated"`
	Warnings    []string              `json:"warnings"`
}
