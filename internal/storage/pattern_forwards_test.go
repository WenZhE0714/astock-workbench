package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestForwardSnapshotsAreImmutableAndChecksummed(t *testing.T) {
	root := t.TempDir()
	s := NewPatternForwardStore(root)
	pool, err := NewPatternBatchStore(t.TempDir()).SavePool(domain.PatternPool{Name: "pool", Source: "fixture", Members: []domain.PatternPoolMember{{Symbol: "sh600519"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := "0123456789abcdef0123456789abcdef"
	task := domain.PatternForwardTask{ID: id, Name: "task", Pool: pool, CreatedAt: time.Now()}
	task.ConfigHash = task.Fingerprint()
	if err = s.Create(task); err != nil {
		t.Fatal(err)
	}
	if err = s.Create(task); !os.IsExist(err) {
		t.Fatal("configuration overwritten")
	}
	v, err := s.SaveSnapshot(domain.PatternForwardSnapshot{TaskID: id, Date: "2026-10-13", Symbol: "sh600519", ConfigHash: task.ConfigHash, CollectedAt: time.Now(), Input: domain.PatternValidationInput{Symbol: "sh600519"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveSnapshot(v); !os.IsExist(err) {
		t.Fatal("observation snapshot overwritten")
	}
	loaded, err := s.Snapshot(id, v.Date, v.Symbol)
	if err != nil || loaded.Hash != v.Hash {
		t.Fatal("snapshot changed on reload")
	}
	if err = s.SetControl(id, domain.PatternForwardControl{Enabled: false, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	unchanged, err := s.Load(id)
	if err != nil || unchanged.ConfigHash != task.ConfigHash {
		t.Fatal("pause mutated configuration")
	}
	day := domain.PatternForwardDay{TaskID: id, Date: v.Date, State: "captured", SealedAt: time.Now()}
	if err = s.SaveDay(day); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveDay(day); err == nil {
		t.Fatal("sealed day overwritten")
	}
	path := filepath.Join(root, "tasks", id, "days", v.Date, v.Symbol+".json")
	if err = os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Snapshot(id, v.Date, v.Symbol); err == nil {
		t.Fatal("corrupt snapshot accepted")
	}
}
