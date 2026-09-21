package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

func storedMonitorPlan() domain.TradePlan {
	plan := tradePlanFixture()
	plan.Analysis.Version = "chart-v1"
	plan.Structure.Name = "区间突破"
	plan.Structure.Anchors = []domain.ChartAnchor{{Date: "2026-09-17", Price: 100}}
	return plan
}

func TestPlanMonitorStoreConcurrentChecksAndRestart(t *testing.T) {
	root := t.TempDir()
	plan := storedMonitorPlan()
	now := plan.CreatedAt
	store := NewPlanMonitorStore(root)
	_, err := store.Update(plan.ID, func(state domain.PlanMonitor) (domain.PlanMonitor, error) {
		return strategy.ConfigurePlanMonitor(plan, state, true, now)
	})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 12 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := NewPlanMonitorStore(root).Update(plan.ID, func(state domain.PlanMonitor) (domain.PlanMonitor, error) {
				return strategy.AdvancePlanMonitor(state, strategy.PlanMonitorObservation{Now: now.Add(24 * 10 * time.Hour), Session: "closed"}), nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	state, err := NewPlanMonitorStore(root).Load(plan.ID)
	if err != nil || state.Phase != "expired" || len(state.Events) != 2 {
		t.Fatalf("concurrent expiry duplicated/lost: %v %+v", err, state)
	}
	info, err := os.Stat(filepath.Join(root, plan.ID+".json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("monitor permissions: %v %v", info, err)
	}
}

func TestPlanMonitorStoreRefusesRewritingHistoryAndFrozenRule(t *testing.T) {
	plan := storedMonitorPlan()
	store := NewPlanMonitorStore(t.TempDir())
	original, err := store.Update(plan.ID, func(state domain.PlanMonitor) (domain.PlanMonitor, error) {
		return strategy.ConfigurePlanMonitor(plan, state, true, plan.CreatedAt)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*domain.PlanMonitor){
		func(state *domain.PlanMonitor) { state.Events[0].Message = "changed" },
		func(state *domain.PlanMonitor) { state.Rule.Levels.Invalidation = 1 },
		func(state *domain.PlanMonitor) { state.ExpiresOn = "2099-01-01" },
		func(state *domain.PlanMonitor) { state.Events = nil; state.Sequence = 0 },
	} {
		_, err := store.Update(plan.ID, func(state domain.PlanMonitor) (domain.PlanMonitor, error) { mutate(&state); return state, nil })
		if err == nil {
			t.Fatal("immutable monitoring evidence was rewritten")
		}
		actual, loadErr := store.Load(plan.ID)
		if loadErr != nil || !reflect.DeepEqual(original, actual) {
			t.Fatal("failed update changed durable state")
		}
	}
}

func TestPlanMonitorStoreDoesNotTouchPlanArchive(t *testing.T) {
	root := t.TempDir()
	plan := storedMonitorPlan()
	plans := NewTradePlanStore(filepath.Join(root, "plans"))
	if _, _, err := plans.Save(plan); err != nil {
		t.Fatal(err)
	}
	monitors := NewPlanMonitorStore(filepath.Join(root, "monitors"))
	if _, err := monitors.Update(plan.ID, func(state domain.PlanMonitor) (domain.PlanMonitor, error) {
		return strategy.ConfigurePlanMonitor(plan, state, true, plan.CreatedAt)
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := plans.Load(plan.Symbol, plan.ID)
	if err != nil || !reflect.DeepEqual(plan, loaded) {
		t.Fatal("monitoring changed original plan")
	}
	if _, err := monitors.Load("../../plan"); err == nil {
		t.Fatal("unsafe monitor path allowed")
	}
}
