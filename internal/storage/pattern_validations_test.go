package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestPatternValidationArchivesRemainIndependent(t *testing.T) {
	root := t.TempDir()
	store := NewPatternValidationStore(root)
	report := domain.PatternValidationReport{Version: "pattern-validation-v1", GeneratedAt: time.Now().UTC(), InputHash: "input", Request: domain.PatternValidationRequest{Symbols: []string{"sh600519"}, Start: "2026-01-01", End: "2026-10-09"}, Inputs: []domain.PatternValidationInput{{Symbol: "sh600519", Bars: []domain.DailyBar{{Date: "2026-01-01", Close: 100}}}}}
	first, err := store.Save(report)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Save(report)
	if err != nil || first.RunID == second.RunID {
		t.Fatal("new run overwrote original archive")
	}
	loaded, err := NewPatternValidationStore(root).Load(first.RunID)
	if err != nil || !reflect.DeepEqual(loaded, first) {
		t.Fatalf("archive changed: %v", err)
	}
	items, err := store.List(1)
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, first.RunID+".json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("archive permissions")
	}
	if _, err := store.Load("../secret"); err == nil {
		t.Fatal("path traversal accepted")
	}
}
