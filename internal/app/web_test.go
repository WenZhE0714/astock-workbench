package app

import (
	"path/filepath"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/storage"
)

func TestWebInitialSymbolUsesFirstSharedWatchlistItem(t *testing.T) {
	file := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlistGroups(file, []storage.WatchlistGroup{
		{Name: storage.DefaultWatchlistGroup, Symbols: []string{"sz002080", "sh600176"}},
		{Name: "科技", Symbols: []string{"sh601138"}},
	}); err != nil {
		t.Fatal(err)
	}
	app := &App{paths: storage.Paths{WatchlistFile: file}}
	symbol, err := app.webInitialSymbol()
	if err != nil {
		t.Fatal(err)
	}
	if symbol != "sz002080" {
		t.Fatalf("initial symbol = %q, want first shared watchlist item", symbol)
	}
}

func TestWebInitialSymbolFallsBackWhenWatchlistIsEmpty(t *testing.T) {
	app := &App{paths: storage.Paths{WatchlistFile: filepath.Join(t.TempDir(), "watchlist")}}
	symbol, err := app.webInitialSymbol()
	if err != nil {
		t.Fatal(err)
	}
	if symbol != fallbackWebSymbol {
		t.Fatalf("initial symbol = %q, want fallback %q", symbol, fallbackWebSymbol)
	}
}
