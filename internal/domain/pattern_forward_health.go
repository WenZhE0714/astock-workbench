package domain

import "time"

type PatternForwardIssue struct {
	Key      string   `json:"key"`
	Code     string   `json:"code"`
	Date     string   `json:"date,omitempty"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Symbols  []string `json:"symbols,omitempty"`
}

type PatternForwardAlert struct {
	Sequence       int64               `json:"sequence"`
	Kind           string              `json:"kind"`
	At             time.Time           `json:"at"`
	Issue          PatternForwardIssue `json:"issue"`
	AcknowledgedAt time.Time           `json:"acknowledged_at,omitzero"`
}

type PatternForwardAudit struct {
	TaskID          string    `json:"task_id"`
	ConfigHash      string    `json:"config_hash"`
	CheckedAt       time.Time `json:"checked_at"`
	Through         string    `json:"through"`
	State           string    `json:"state"`
	Days            int       `json:"days"`
	CompleteDays    int       `json:"complete_days"`
	MissingDays     int       `json:"missing_days"`
	Snapshots       int       `json:"snapshots"`
	TrackingThrough string    `json:"tracking_through"`
	Error           string    `json:"error,omitempty"`
}

type PatternForwardOperations struct {
	Version   int                   `json:"version"`
	Sequence  int64                 `json:"sequence"`
	UpdatedAt time.Time             `json:"updated_at"`
	Active    []PatternForwardIssue `json:"active"`
	Events    []PatternForwardAlert `json:"events"`
	Audit     PatternForwardAudit   `json:"audit"`
}

type PatternForwardDailyHealth struct {
	Date         string   `json:"date"`
	State        string   `json:"state"`
	Captured     int      `json:"captured"`
	Expected     int      `json:"expected"`
	Missing      []string `json:"missing"`
	TrackingDone bool     `json:"tracking_done"`
}

type PatternForwardHealth struct {
	Date          string                      `json:"date"`
	Phase         string                      `json:"phase"`
	Detail        string                      `json:"detail"`
	NextAttemptAt time.Time                   `json:"next_attempt_at,omitzero"`
	LastSuccessAt time.Time                   `json:"last_success_at,omitzero"`
	Captured      int                         `json:"captured"`
	Expected      int                         `json:"expected"`
	MissingDays   int                         `json:"missing_days"`
	TrackingLag   int                         `json:"tracking_lag"`
	Issues        []PatternForwardIssue       `json:"issues"`
	Days          []PatternForwardDailyHealth `json:"days"`
}
