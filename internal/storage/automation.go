package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// AutomationState contains scheduler metadata only. It deliberately excludes
// market signals, orders and account balances; those remain in their own
// append-only/ledger stores.
type AutomationState struct {
	SchemaVersion        int                            `json:"schema_version"`
	LastRunAt            time.Time                      `json:"last_run_at,omitempty"`
	LastSuccessAt        time.Time                      `json:"last_success_at,omitempty"`
	LastError            string                         `json:"last_error,omitempty"`
	NextRunAt            time.Time                      `json:"next_run_at,omitempty"`
	LastOutcomeAt        time.Time                      `json:"last_outcome_at,omitempty"`
	LastShadowAt         time.Time                      `json:"last_shadow_at,omitempty"`
	ResearchAttemptAt    time.Time                      `json:"research_attempt_at,omitempty"`
	ResearchSuccessAt    time.Time                      `json:"research_success_at,omitempty"`
	ResearchExperimentID string                         `json:"research_experiment_id,omitempty"`
	ResearchMessage      string                         `json:"research_message,omitempty"`
	ResearchError        string                         `json:"research_error,omitempty"`
	Tasks                map[string]AutomationTaskState `json:"tasks,omitempty"`
}

// AutomationTaskState is intentionally metadata-only. Market snapshots,
// orders and balances stay in their own stores; this map only explains what
// the scheduler attempted and whether the attempt was useful.
type AutomationTaskState struct {
	Status        string    `json:"status,omitempty"`
	Detail        string    `json:"detail,omitempty"`
	LastAttemptAt time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt time.Time `json:"last_success_at,omitempty"`
	NextRunAt     time.Time `json:"next_run_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
}

type AutomationStore struct{ file string }

func NewAutomationStore(file string) *AutomationStore {
	return &AutomationStore{file: file}
}

func (store *AutomationStore) Path() string {
	if store == nil {
		return ""
	}
	return store.file
}

func (store *AutomationStore) Load() (AutomationState, error) {
	if store == nil || store.file == "" {
		return AutomationState{}, errors.New("自动化状态文件未初始化")
	}
	data, err := os.ReadFile(store.file)
	if errors.Is(err, os.ErrNotExist) {
		return AutomationState{}, nil
	}
	if err != nil {
		return AutomationState{}, err
	}
	var state AutomationState
	if err := json.Unmarshal(data, &state); err != nil {
		return AutomationState{}, err
	}
	if state.SchemaVersion == 0 {
		state.SchemaVersion = 1
	}
	return state, nil
}

func (store *AutomationStore) Save(state AutomationState) error {
	if store == nil || store.file == "" {
		return errors.New("自动化状态文件未初始化")
	}
	state.SchemaVersion = 1
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(store.file), 0o700); err != nil {
		return err
	}
	return atomicWrite(store.file, append(data, '\n'), 0o600)
}
