package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAutomationStoreRoundTrip(t *testing.T) {
	store := NewAutomationStore(filepath.Join(t.TempDir(), "automation", "state.json"))
	want := AutomationState{LastRunAt: time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC), LastError: "暂时失败", ResearchExperimentID: "AUTO-1", Tasks: map[string]AutomationTaskState{"scan": {Status: "success", Detail: "已生成2个信号", LastAttemptAt: time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)}}}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || !got.LastRunAt.Equal(want.LastRunAt) || got.LastError != want.LastError || got.ResearchExperimentID != want.ResearchExperimentID || got.Tasks["scan"].Status != "success" || got.Tasks["scan"].Detail != "已生成2个信号" {
		t.Fatalf("unexpected state: %+v", got)
	}
}

func TestAutomationStoreMissingFileIsEmpty(t *testing.T) {
	got, err := NewAutomationStore(filepath.Join(t.TempDir(), "missing.json")).Load()
	if err != nil || got.SchemaVersion != 0 || got.LastRunAt != (time.Time{}) || len(got.Tasks) != 0 {
		t.Fatalf("missing state should be empty: %+v %v", got, err)
	}
}
