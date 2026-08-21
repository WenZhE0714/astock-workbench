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
	if err := store.Save(input); err != nil { t.Fatal(err) }
	loaded, err := store.Load()
	if err != nil { t.Fatal(err) }
	if loaded.CandidateCount != 3 || loaded.CompletedTrades != 2 || !loaded.GeneratedAt.Equal(input.GeneratedAt) { t.Fatalf("unexpected report: %+v", loaded) }
}

func TestShadowStoreMissingFileIsEmpty(t *testing.T) {
	loaded, err := NewShadowStore(filepath.Join(t.TempDir(), "missing.json")).Load()
	if err != nil || !loaded.GeneratedAt.IsZero() { t.Fatalf("unexpected load: %+v %v", loaded, err) }
}
