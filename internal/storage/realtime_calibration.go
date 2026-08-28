package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/wenzhe/astock-workbench/internal/realtime"
)

// RealtimeCalibrationState is the durable control-plane record for the latest
// gated Challenger. Signal and order ledgers remain append-only elsewhere;
// this file only records which calibration was applied and why.
type RealtimeCalibrationState struct {
	SchemaVersion int                       `json:"schema_version"`
	Active        realtime.ScoreCalibration `json:"active"`
	AppliedAt     time.Time                 `json:"applied_at,omitempty"`
	Source        string                    `json:"source,omitempty"`
	Status        string                    `json:"status,omitempty"`
	// RejectedIDs prevents a Challenger that failed its forward comparison from
	// being re-installed on every subsequent read of the same outcome report.
	// Keep the list short; the full audit trail remains in History.
	RejectedIDs    []string                      `json:"rejected_ids,omitempty"`
	LastDecisionAt time.Time                     `json:"last_decision_at,omitempty"`
	History        []RealtimeCalibrationRevision `json:"history,omitempty"`
}

type RealtimeCalibrationRevision struct {
	ID               string             `json:"id"`
	AppliedAt        time.Time          `json:"applied_at"`
	Action           string             `json:"action"`
	ReadySamples     int                `json:"ready_samples"`
	MinimumScore     float64            `json:"minimum_score"`
	ComponentWeights map[string]float64 `json:"component_weights,omitempty"`
	Note             string             `json:"note,omitempty"`
}

type RealtimeCalibrationStore struct{ file string }

func NewRealtimeCalibrationStore(file string) *RealtimeCalibrationStore {
	return &RealtimeCalibrationStore{file: file}
}

func (store *RealtimeCalibrationStore) Path() string {
	if store == nil {
		return ""
	}
	return store.file
}

func (store *RealtimeCalibrationStore) Load() (RealtimeCalibrationState, error) {
	if store == nil || store.file == "" {
		return RealtimeCalibrationState{}, errors.New("实时校准状态文件未初始化")
	}
	data, err := os.ReadFile(store.file)
	if errors.Is(err, os.ErrNotExist) {
		return RealtimeCalibrationState{}, nil
	}
	if err != nil {
		return RealtimeCalibrationState{}, err
	}
	var state RealtimeCalibrationState
	if err := json.Unmarshal(data, &state); err != nil {
		return RealtimeCalibrationState{}, err
	}
	if state.SchemaVersion == 0 {
		state.SchemaVersion = 1
	}
	return state, nil
}

func (store *RealtimeCalibrationStore) Save(state RealtimeCalibrationState) error {
	if store == nil || store.file == "" {
		return errors.New("实时校准状态文件未初始化")
	}
	state.SchemaVersion = 1
	state.RejectedIDs = uniqueCalibrationIDs(state.RejectedIDs, 20)
	if len(state.History) > 20 {
		state.History = append([]RealtimeCalibrationRevision(nil), state.History[len(state.History)-20:]...)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(store.file), 0o700); err != nil {
		return err
	}
	return atomicWrite(store.file, append(data, '\n'), 0o600)
}

func uniqueCalibrationIDs(input []string, limit int) []string {
	if limit <= 0 {
		limit = 20
	}
	seen := make(map[string]struct{}, len(input))
	result := make([]string, 0, minInt(limit, len(input)))
	for index := len(input) - 1; index >= 0 && len(result) < limit; index-- {
		value := input[index]
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	// Preserve chronological order for a stable on-disk audit record.
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
