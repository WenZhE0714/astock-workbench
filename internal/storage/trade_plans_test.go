package storage

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func tradePlanFixture() domain.TradePlan {
	return domain.TradePlan{
		ID: strings.Repeat("a", 64), Version: 1, Symbol: "sh600519", CreatedAt: time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC), ExpiresOn: "2026-09-25",
		Analysis:  domain.ChartAnalysis{Symbol: "sh600519", Fingerprint: strings.Repeat("b", 64), DataDate: "2026-09-18"},
		Structure: domain.ChartStructure{ID: "range-breakout", Plan: &domain.ChartPlanLevels{EntryLow: 100, EntryHigh: 101, Invalidation: 98}},
	}
}

func TestTradePlanStorePersistsAndNeverRewritesSnapshot(t *testing.T) {
	root := t.TempDir()
	store := NewTradePlanStore(root)
	original := tradePlanFixture()
	if _, created, err := store.Save(original); err != nil || !created {
		t.Fatalf("save failed: created=%v err=%v", created, err)
	}
	changed := tradePlanFixture()
	changed.CreatedAt = changed.CreatedAt.Add(time.Hour)
	changed.Structure.Plan.EntryHigh = 999
	actual, created, err := NewTradePlanStore(root).Save(changed)
	if err != nil || created || actual.Structure.Plan.EntryHigh != 101 || !actual.CreatedAt.Equal(original.CreatedAt) {
		t.Fatalf("existing plan overwritten: %v %v %+v", err, created, actual)
	}
	plans, err := NewTradePlanStore(root).List(original.Symbol, 10)
	if err != nil || len(plans) != 1 || plans[0].ID != original.ID {
		t.Fatalf("restart lost snapshot: %v %+v", err, plans)
	}
	info, err := os.Stat(filepath.Join(root, original.Symbol, original.ID+".json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("incorrect private permissions: %v %v", info, err)
	}
}

func TestTradePlanStoreConcurrentDuplicateIsAtomic(t *testing.T) {
	root := t.TempDir()
	var wait sync.WaitGroup
	for range 12 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, _, err := NewTradePlanStore(root).Save(tradePlanFixture()); err != nil {
				t.Errorf("concurrent save failed: %v", err)
			}
		}()
	}
	wait.Wait()
	plans, err := NewTradePlanStore(root).List("sh600519", 10)
	if err != nil || len(plans) != 1 {
		t.Fatalf("concurrent save corrupted store: %v %+v", err, plans)
	}
}

func TestTradePlanStoreRejectsUnsafePathsAndReportsCorruption(t *testing.T) {
	store := NewTradePlanStore(t.TempDir())
	for _, symbol := range []string{"../../tmp", "sh../a/b", "600519", ""} {
		if _, err := store.List(symbol, 10); err == nil {
			t.Fatalf("unsafe symbol accepted: %q", symbol)
		}
	}
	plan := tradePlanFixture()
	if _, _, err := store.Save(plan); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.root, plan.Symbol, plan.ID+".json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(plan.Symbol, 10); err == nil {
		t.Fatal("corrupt snapshot hidden")
	}
}
