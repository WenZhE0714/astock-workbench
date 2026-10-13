package storage

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestPatternPoolVersionsAreImmutableAndCanonical(t *testing.T) {
	store := NewPatternBatchStore(t.TempDir())
	pool := domain.PatternPool{Name: "research", Source: "manual", Members: []domain.PatternPoolMember{{Symbol: "sz002080", Industry: "materials"}, {Symbol: "sh600519", Industry: "food"}}}
	first, err := store.SavePool(pool)
	if err != nil {
		t.Fatal(err)
	}
	pool.Members[0], pool.Members[1] = pool.Members[1], pool.Members[0]
	again, err := store.SavePool(pool)
	if err != nil || again.Version != first.Version || !again.CreatedAt.Equal(first.CreatedAt) {
		t.Fatal("same content acquired a new version or timestamp")
	}
	pool.Members[0].Industry = "changed"
	changed, err := store.SavePool(pool)
	if err != nil || changed.Version == first.Version {
		t.Fatal("changed grouping reused version")
	}
	old, err := store.LoadPool(first.Version)
	if err != nil || old.Members[0].Industry != "food" {
		t.Fatal("old version overwritten")
	}
	if _, err = store.LoadPool("../../outside"); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestPatternBatchCheckpointIntegrityAndLease(t *testing.T) {
	root := t.TempDir()
	store := NewPatternBatchStore(root)
	unlock, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	other := NewPatternBatchStore(root)
	if release, err := other.Acquire(); err == nil {
		release()
		t.Fatal("second worker acquired same storage")
	}
	unlock()
	release, err := other.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	release()
	id := "0123456789abcdef0123456789abcdef"
	input := domain.PatternValidationInput{Symbol: "sh600519", Bars: []domain.DailyBar{{Date: "2026-01-01", Close: 100, Turnover: math.NaN()}}}
	digest, err := store.SaveInput(id, input)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := other.LoadInput(id, input.Symbol, digest)
	if err != nil || !math.IsNaN(loaded.Bars[0].Turnover) {
		t.Fatal("checkpoint lost nullable input")
	}
	path := filepath.Join(root, "jobs", id, "inputs", input.Symbol+".json")
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("checkpoint permissions")
	}
	if err = os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LoadInput(id, input.Symbol, digest); err == nil {
		t.Fatal("corrupt checkpoint accepted")
	}
}
