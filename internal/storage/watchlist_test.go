package storage

import (
	"path/filepath"
	"testing"
)

func TestWatchlistLifecycle(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config", "watchlist")
	added, err := AddWatchlist(file, []string{"sh600519", "sz000001"})
	if err != nil || !added[0] || !added[1] {
		t.Fatalf("add failed: %v %#v", err, added)
	}
	removed, err := RemoveWatchlist(file, []string{"sh600519"})
	if err != nil || !removed[0] {
		t.Fatalf("remove failed: %v %#v", err, removed)
	}
	items, _, err := LoadWatchlist(file)
	if err != nil || len(items) != 1 || items[0] != "sz000001" {
		t.Fatalf("unexpected items: %v %#v", err, items)
	}
}
