package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestSentimentHistoryStoreRoundTrip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sentiment", "history.json")
	store := NewSentimentHistoryStore(file)
	want := []domain.MarketSentimentPoint{{At: time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC), Score: 63.5, Phase: "强势"}}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Score != want[0].Score || got[0].Phase != want[0].Phase {
		t.Fatalf("unexpected history: %#v", got)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("history file permissions: %o", info.Mode().Perm())
	}
}
