package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

type PatternForwardConfig struct {
	Version          string   `json:"version"`
	EngineVersion    string   `json:"engine_version"`
	PatternVersion   string   `json:"pattern_version"`
	Patterns         []string `json:"patterns"`
	PriceBasis       string   `json:"price_basis"`
	StartOn          string   `json:"start_on"`
	HistoryStart     string   `json:"history_start"`
	WarmupDays       int      `json:"warmup_days"`
	LookbackBars     int      `json:"lookback_bars"`
	PivotRadius      int      `json:"pivot_radius"`
	ConfirmationBars int      `json:"confirmation_bars"`
	PriceMethod      string   `json:"price_method"`
	VolumeMethod     string   `json:"volume_method"`
	QualityMethod    string   `json:"quality_method"`
	OpenMinute       int      `json:"open_minute"`
	CloseMinute      int      `json:"close_minute"`
	RetryMinutes     int      `json:"retry_minutes"`
}

type PatternForwardTask struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Pool       PatternPool          `json:"pool"`
	Config     PatternForwardConfig `json:"config"`
	ConfigHash string               `json:"config_hash"`
	CreatedAt  time.Time            `json:"created_at"`
}

func (task PatternForwardTask) Fingerprint() string {
	data, _ := json.Marshal(struct {
		Name, Pool string
		Config     PatternForwardConfig
		CreatedAt  time.Time
	}{task.Name, task.Pool.Version, task.Config, task.CreatedAt})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

type PatternForwardControl struct {
	Enabled   bool      `json:"enabled"`
	UpdatedAt time.Time `json:"updated_at"`
}
type PatternForwardStatus struct {
	CheckedAt      time.Time `json:"checked_at"`
	LastAttemptAt  time.Time `json:"last_attempt_at,omitempty"`
	Error          string    `json:"error,omitempty"`
	CalendarSource string    `json:"calendar_source,omitempty"`
	CalendarStart  string    `json:"calendar_start,omitempty"`
	CalendarEnd    string    `json:"calendar_end,omitempty"`
}

type PatternForwardItem struct {
	Symbol       string    `json:"symbol"`
	State        string    `json:"state"`
	Attempts     int       `json:"attempts"`
	CollectedAt  time.Time `json:"collected_at,omitempty"`
	SnapshotHash string    `json:"snapshot_hash,omitempty"`
	SampleIDs    []string  `json:"sample_ids"`
	Error        string    `json:"error,omitempty"`
}

type PatternForwardDay struct {
	TaskID         string               `json:"task_id"`
	Date           string               `json:"date"`
	State          string               `json:"state"`
	CalendarSource string               `json:"calendar_source"`
	LastAttemptAt  time.Time            `json:"last_attempt_at,omitempty"`
	SealedAt       time.Time            `json:"sealed_at,omitempty"`
	Items          []PatternForwardItem `json:"items"`
	Error          string               `json:"error,omitempty"`
}

type PatternForwardBenchmark struct {
	TaskID      string                 `json:"task_id"`
	Date        string                 `json:"date"`
	CollectedAt time.Time              `json:"collected_at"`
	Input       PatternValidationInput `json:"input"`
	Hash        string                 `json:"hash"`
}

type PatternForwardSnapshot struct {
	TaskID        string                    `json:"task_id"`
	Date          string                    `json:"date"`
	Symbol        string                    `json:"symbol"`
	ConfigHash    string                    `json:"config_hash"`
	CollectedAt   time.Time                 `json:"collected_at"`
	BenchmarkHash string                    `json:"benchmark_hash"`
	InputHash     string                    `json:"input_hash"`
	Input         PatternValidationInput    `json:"input"`
	Coverage      PatternValidationCoverage `json:"coverage"`
	Adjustment    *PatternAdjustment        `json:"adjustment,omitempty"`
	Samples       []PatternValidationSample `json:"samples"`
	Hash          string                    `json:"hash"`
}
