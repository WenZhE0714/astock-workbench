package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/notify"
)

func TestNativeNotificationStorePersistsPrivateRecordsAndRejectsCorruption(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notifications", "mac.json")
	store := NewNativeNotificationStore(file)
	state, err := store.Load()
	if err != nil || len(state.Records) != 0 {
		t.Fatalf("empty store: %+v, %v", state, err)
	}
	state.Records = map[string]notify.Record{"one": {Message: notify.Message{ID: "one", At: time.Now()}, Status: "submitted"}}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	restored, err := NewNativeNotificationStore(file).Load()
	if err != nil || restored.Records["one"].Status != "submitted" {
		t.Fatalf("restore: %+v, %v", restored, err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("notification record permissions: %v, %v", info, err)
	}
	if err := os.WriteFile(file, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("corrupt dedup state silently discarded")
	}
}
