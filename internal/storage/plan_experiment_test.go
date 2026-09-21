package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
)

func TestPlanExperimentStoreConcurrentControlsAndRestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plan-experiment.json")
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := NewPlanExperimentStore(file).Update(func(state paper.PlanExperiment) (paper.PlanExperiment, error) {
				return paper.ConfigurePlanExperiment(state, true, now)
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	state, err := NewPlanExperimentStore(file).Load()
	if err != nil || !state.EntriesEnabled || len(state.Changes) != 1 || len(state.Arms) != 2 {
		t.Fatalf("duplicate controls/reset: %v %+v", err, state)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file permissions: %v", err)
	}
	_, err = NewPlanExperimentStore(file).Update(func(current paper.PlanExperiment) (paper.PlanExperiment, error) {
		current.Arms[0].Report.RemainingCash++
		return current, nil
	})
	if err == nil {
		t.Fatal("cash rewrite committed")
	}
	restored, err := NewPlanExperimentStore(file).Load()
	if err != nil || restored.Arms[0].Report.RemainingCash != state.Arms[0].Report.RemainingCash {
		t.Fatal("rejected mutation leaked into storage")
	}
}
