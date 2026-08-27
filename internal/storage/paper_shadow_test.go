package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
)

func TestShadowStoreRoundTrip(t *testing.T) {
	store := NewShadowStore(filepath.Join(t.TempDir(), "paper", "shadow-report.json"))
	input := paper.Report{GeneratedAt: time.Date(2026, 8, 21, 15, 0, 0, 0, time.Local), CandidateCount: 3, CompletedTrades: 2}
	if err := store.Save(input); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CandidateCount != 3 || loaded.CompletedTrades != 2 || !loaded.GeneratedAt.Equal(input.GeneratedAt) {
		t.Fatalf("unexpected report: %+v", loaded)
	}
}

func TestShadowStoreMissingFileIsEmpty(t *testing.T) {
	loaded, err := NewShadowStore(filepath.Join(t.TempDir(), "missing.json")).Load()
	if err != nil || !loaded.GeneratedAt.IsZero() {
		t.Fatalf("unexpected load: %+v %v", loaded, err)
	}
}

func TestShadowStoreRejectsOlderReportOverwrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "paper", "shadow-report.json")
	store := NewShadowStore(file)
	newer := paper.Report{EngineVersion: "tplus1-v10", AsOf: "2026-08-26", CheckpointPhase: paper.CheckpointOpen, LastRealtimeAt: "2026-08-26 10:30:00", GeneratedAt: time.Date(2026, 8, 26, 10, 30, 0, 0, time.Local), InitialCash: 1_000_000}
	if err := store.Save(newer); err != nil {
		t.Fatal(err)
	}
	older := newer
	older.LastRealtimeAt = ""
	older.GeneratedAt = time.Date(2026, 8, 26, 9, 30, 0, 0, time.Local)
	if err := store.Save(older); err == nil {
		t.Fatal("older shadow report overwrote newer realtime report")
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LastRealtimeAt != newer.LastRealtimeAt {
		t.Fatalf("newer report was lost: %+v", loaded)
	}
}
