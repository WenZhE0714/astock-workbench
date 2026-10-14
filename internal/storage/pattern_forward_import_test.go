package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func forwardImportFixture(t *testing.T) (string, domain.PatternForwardTask) {
	t.Helper()
	root := t.TempDir()
	pool, err := NewPatternBatchStore(filepath.Join(root, "pattern-batches")).SavePool(domain.PatternPool{Name: "import", Source: "fixture", Members: []domain.PatternPoolMember{{Symbol: "sh600519"}}})
	if err != nil {
		t.Fatal(err)
	}
	task := domain.PatternForwardTask{ID: "0123456789abcdef0123456789abcdef", Name: "forward", Pool: pool, Config: domain.PatternForwardConfig{StartOn: "2026-10-14"}, CreatedAt: time.Now()}
	task.ConfigHash = task.Fingerprint()
	store := NewPatternForwardStore(filepath.Join(root, "pattern-forwards"))
	if err = store.Create(task); err != nil {
		t.Fatal(err)
	}
	if err = store.SetControl(task.ID, domain.PatternForwardControl{Enabled: true, UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return root, task
}

func TestForwardImportDryRunPreservesOriginalAndIdempotentApply(t *testing.T) {
	source, task := forwardImportFixture(t)
	destination := filepath.Join(t.TempDir(), "destination")
	preview, err := ImportPatternForwards(source, destination, false, time.Now())
	if err != nil || preview.Applied || len(preview.Tasks) != 1 || preview.Tasks[0].ConfigHash != task.ConfigHash || preview.Tasks[0].State != "waiting" {
		t.Fatalf("dry run: %+v %v", preview, err)
	}
	if _, err = os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("dry run wrote to destination")
	}
	result, err := ImportPatternForwards(source, destination, true, time.Now())
	if err != nil || !result.Applied {
		t.Fatalf("apply: %+v %v", result, err)
	}
	from, to := NewPatternForwardStore(filepath.Join(source, "pattern-forwards")), NewPatternForwardStore(filepath.Join(destination, "pattern-forwards"))
	for _, store := range []*PatternForwardStore{from, to} {
		audit := store.Audit(task.ID, time.Now())
		if audit.Error != "" || audit.ConfigHash != task.ConfigHash || audit.State != "waiting" {
			t.Fatal("migration changed configuration or fabricated a capture")
		}
	}
	again, err := ImportPatternForwards(source, destination, true, time.Now())
	if err != nil || again.Existing != 1 {
		t.Fatal("identical repeated import was not idempotent")
	}
	if err = to.SetControl(task.ID, domain.PatternForwardControl{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err = ImportPatternForwards(source, destination, true, time.Now()); err == nil {
		t.Fatal("existing different control state was overwritten")
	}
	control, _ := to.Control(task.ID)
	if control.Enabled {
		t.Fatal("failed import changed destination control")
	}
}

func TestForwardImportRejectsCorruptionAndSymlinks(t *testing.T) {
	for _, mode := range []string{"corrupt", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			source, task := forwardImportFixture(t)
			root := filepath.Join(source, "pattern-forwards", "tasks", task.ID)
			if mode == "corrupt" {
				if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{}`), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(filepath.Join(root, "config.json"), filepath.Join(root, "extra.json")); err != nil {
				t.Fatal(err)
			}
			if _, err := ImportPatternForwards(source, filepath.Join(t.TempDir(), "to"), false, time.Now()); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
}
