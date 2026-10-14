package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestForwardTrackingStorageAppendOnlyAndHashChain(t *testing.T) {
	root := t.TempDir()
	s := NewPatternForwardStore(root)
	pool, err := NewPatternBatchStore(t.TempDir()).SavePool(domain.PatternPool{Name: "pool", Source: "fixture", Members: []domain.PatternPoolMember{{Symbol: "sh600519"}}})
	if err != nil {
		t.Fatal(err)
	}
	task := domain.PatternForwardTask{ID: "0123456789abcdef0123456789abcdef", Pool: pool, CreatedAt: time.Now()}
	task.ConfigHash = task.Fingerprint()
	if err = s.Create(task); err != nil {
		t.Fatal(err)
	}
	first := domain.PatternForwardTracking{Version: "test-v1", TaskID: task.ID, ConfigHash: task.ConfigHash, Date: "2026-10-12", EvaluatedAt: time.Now()}
	first, err = s.SaveTracking(first)
	if err != nil || first.Hash == "" {
		t.Fatalf("first append: %v", err)
	}
	if _, err = s.SaveTracking(first); err == nil {
		t.Fatal("existing tracking record overwritten")
	}
	second := first
	second.Date = "2026-10-13"
	if _, err = s.SaveTracking(second); err == nil {
		t.Fatal("broken predecessor accepted")
	}
	second.PreviousHash = first.Hash
	second, err = s.SaveTracking(second)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewPatternForwardStore(root).Tracking(task.ID)
	if err != nil || len(reloaded) != 2 || reloaded[0].Hash != first.Hash || reloaded[1].Hash != second.Hash {
		t.Fatal("restart changed tracking records")
	}
	path := filepath.Join(root, "tasks", task.ID, "tracking", first.Date+".json")
	if err = os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Tracking(task.ID); err == nil {
		t.Fatal("corrupted ledger accepted")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Tracking(task.ID); err == nil {
		t.Fatal("deleted predecessor accepted")
	}
}
