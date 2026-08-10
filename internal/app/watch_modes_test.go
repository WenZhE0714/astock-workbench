package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func TestMoveWatchlistSymbolAndReorderQuotes(t *testing.T) {
	symbols := []string{"sh600519", "sz000001", "sz300750"}
	if !moveWatchlistSymbol(symbols, 2, 0) {
		t.Fatal("expected move")
	}
	if !reflect.DeepEqual(symbols, []string{"sz300750", "sh600519", "sz000001"}) {
		t.Fatalf("unexpected symbols: %#v", symbols)
	}
	quotes := []domain.Quote{{Symbol: "sz000001"}, {Symbol: "sz300750"}, {Symbol: "sh600519"}}
	ordered := reorderQuotes(quotes, symbols)
	if got := []string{ordered[0].Symbol, ordered[1].Symbol, ordered[2].Symbol}; !reflect.DeepEqual(got, symbols) {
		t.Fatalf("quotes not reordered: %#v", got)
	}
}

func TestWatchSortStateUsesTwoEnterPhases(t *testing.T) {
	state := watchSortState{}
	state.begin([]string{"sh600519", "sz000001"}, 1)
	if state.picked || !strings.Contains(state.controls(false), "Enter选中") {
		t.Fatalf("unexpected selecting phase: %#v", state)
	}
	state.picked = true
	if !strings.Contains(state.controls(false), "Enter保存") || !strings.Contains(state.status("sz000001", "平安银行", false), "平安银行") {
		t.Fatalf("unexpected moving phase: %#v", state)
	}
}

func TestWatchGroupChooserIncludesAllAndSelectsCurrent(t *testing.T) {
	chooser := watchGroupChooser{}
	chooser.begin([]storage.WatchlistGroup{
		{Name: storage.DefaultWatchlistGroup, Symbols: []string{"sh600519"}},
		{Name: "科技", Symbols: []string{"sz300750"}},
	}, "科技")
	group, ok := chooser.selectedGroup()
	if !ok || group.Name != "科技" || chooser.groups[0].Name != storage.AllWatchlistGroup || len(chooser.groups[0].Symbols) != 2 {
		t.Fatalf("unexpected chooser: %#v", chooser)
	}
	status := chooser.status(false)
	if !strings.Contains(status, "> 科技  1只") || !strings.Contains(status, "全部  2只") {
		t.Fatalf("unexpected chooser status:\n%s", status)
	}
}
