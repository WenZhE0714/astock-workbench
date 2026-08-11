package storage

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestLegacyWatchlistLoadsIntoDefaultGroup(t *testing.T) {
	file := filepath.Join(t.TempDir(), "watchlist")
	if err := os.WriteFile(file, []byte("sh600519\nsz000001\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	groups, warnings, err := LoadWatchlistGroups(file)
	if err != nil || len(warnings) != 0 || len(groups) != 1 {
		t.Fatalf("unexpected legacy load: %v %#v %#v", err, warnings, groups)
	}
	if groups[0].Name != DefaultWatchlistGroup || !reflect.DeepEqual(groups[0].Symbols, []string{"sh600519", "sz000001"}) {
		t.Fatalf("legacy symbols not migrated to default: %#v", groups)
	}
}

func TestWatchlistGroupsAddRemoveAndReorder(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config", "watchlist")
	if created, err := CreateWatchlistGroup(file, "科技"); err != nil || !created {
		t.Fatalf("create group failed: %v %v", created, err)
	}
	if added, err := AddWatchlistToGroup(file, "科技", []string{"sz000001", "sz300750"}); err != nil || !added[0] || !added[1] {
		t.Fatalf("add to group failed: %v %#v", err, added)
	}
	if _, err := AddWatchlistToGroup(file, DefaultWatchlistGroup, []string{"sh600519", "sz000001"}); err != nil {
		t.Fatal(err)
	}
	groups, _, err := LoadWatchlistGroups(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := WatchlistSymbols(groups, AllWatchlistGroup); !reflect.DeepEqual(got, []string{"sh600519", "sz000001", "sz300750"}) {
		t.Fatalf("unexpected flattened symbols: %#v", got)
	}
	if err := SaveWatchlistGroupOrder(file, "科技", []string{"sz300750", "sz000001"}); err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveWatchlistFromGroup(file, "科技", []string{"sz000001"})
	if err != nil || !removed[0] {
		t.Fatalf("remove from group failed: %v %#v", err, removed)
	}
	groups, _, _ = LoadWatchlistGroups(file)
	if got := WatchlistSymbols(groups, "科技"); !reflect.DeepEqual(got, []string{"sz300750"}) {
		t.Fatalf("unexpected technology group: %#v", got)
	}
}

func TestDeleteGroupMovesUniqueSymbolsToDefault(t *testing.T) {
	file := filepath.Join(t.TempDir(), "watchlist")
	groups := []WatchlistGroup{
		{Name: DefaultWatchlistGroup, Symbols: []string{"sh600519"}},
		{Name: "科技", Symbols: []string{"sz300750", "sh600519"}},
	}
	if err := SaveWatchlistGroups(file, groups); err != nil {
		t.Fatal(err)
	}
	deleted, moved, err := DeleteWatchlistGroup(file, "科技")
	if err != nil || !deleted || moved != 1 {
		t.Fatalf("delete group failed: deleted=%v moved=%d err=%v", deleted, moved, err)
	}
	loaded, _, _ := LoadWatchlistGroups(file)
	if len(loaded) != 1 || !reflect.DeepEqual(loaded[0].Symbols, []string{"sh600519", "sz300750"}) {
		t.Fatalf("unique symbol not preserved: %#v", loaded)
	}
}

func TestSavedGroupFormatRemainsReadableAsFlatWatchlist(t *testing.T) {
	file := filepath.Join(t.TempDir(), "watchlist")
	groups := []WatchlistGroup{
		{Name: DefaultWatchlistGroup, Symbols: []string{"sh600519"}},
		{Name: "银行", Symbols: []string{"sz000001"}},
	}
	if err := SaveWatchlistGroups(file, groups); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[默认]\nsh600519\n[银行]\nsz000001") {
		t.Fatalf("unexpected grouped format:\n%s", data)
	}
	items, _, err := LoadWatchlist(file)
	if err != nil || !reflect.DeepEqual(items, []string{"sh600519", "sz000001"}) {
		t.Fatalf("flat compatibility failed: %v %#v", err, items)
	}
}

func TestSetWatchlistSymbolGroupsReplacesMembershipsAndPreservesOrder(t *testing.T) {
	file := filepath.Join(t.TempDir(), "watchlist")
	groups := []WatchlistGroup{
		{Name: DefaultWatchlistGroup, Symbols: []string{"sh600519", "sz000001"}},
		{Name: "科技", Symbols: []string{"sz300750", "sh600519"}},
		{Name: "长期", Symbols: []string{"sz000001"}},
	}
	if err := SaveWatchlistGroups(file, groups); err != nil {
		t.Fatal(err)
	}
	assigned, err := SetWatchlistSymbolGroups(file, "600519", []string{"科技", "长期"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(assigned, []string{"科技", "长期"}) {
		t.Fatalf("unexpected assigned groups: %#v", assigned)
	}
	loaded, _, err := LoadWatchlistGroups(file)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded[0].Symbols, []string{"sz000001"}) {
		t.Fatalf("symbol not removed from default: %#v", loaded[0].Symbols)
	}
	if !reflect.DeepEqual(loaded[1].Symbols, []string{"sz300750", "sh600519"}) {
		t.Fatalf("existing group order changed: %#v", loaded[1].Symbols)
	}
	if !reflect.DeepEqual(loaded[2].Symbols, []string{"sz000001", "sh600519"}) {
		t.Fatalf("symbol not appended to new group: %#v", loaded[2].Symbols)
	}
}

func TestSetWatchlistSymbolGroupsKeepsDefaultWhenSelectionIsEmpty(t *testing.T) {
	file := filepath.Join(t.TempDir(), "watchlist")
	if err := SaveWatchlistGroups(file, []WatchlistGroup{
		{Name: DefaultWatchlistGroup},
		{Name: "科技", Symbols: []string{"sz300750"}},
	}); err != nil {
		t.Fatal(err)
	}
	assigned, err := SetWatchlistSymbolGroups(file, "sz300750", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(assigned, []string{DefaultWatchlistGroup}) {
		t.Fatalf("empty selection should fall back to default: %#v", assigned)
	}
	loaded, _, _ := LoadWatchlistGroups(file)
	if !reflect.DeepEqual(loaded[0].Symbols, []string{"sz300750"}) || len(loaded[1].Symbols) != 0 {
		t.Fatalf("symbol should remain in default only: %#v", loaded)
	}
}

func TestSetWatchlistSymbolGroupsRejectsVirtualOrMissingGroup(t *testing.T) {
	file := filepath.Join(t.TempDir(), "watchlist")
	if err := SaveWatchlist(file, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	for _, groupName := range []string{AllWatchlistGroup, "不存在"} {
		if _, err := SetWatchlistSymbolGroups(file, "sh600519", []string{groupName}); err == nil {
			t.Fatalf("expected invalid target group %q to fail", groupName)
		}
	}
}
