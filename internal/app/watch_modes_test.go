package app

import (
	"fmt"
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

func TestWatchGroupAssignmentShowsCurrentMembershipAndSupportsMultiSelect(t *testing.T) {
	assignment := watchGroupAssignment{}
	assignment.begin([]storage.WatchlistGroup{
		{Name: storage.DefaultWatchlistGroup, Symbols: []string{"sh600519"}},
		{Name: "科技", Symbols: []string{"sz300750"}},
		{Name: "长期", Symbols: []string{"sh600519"}},
	}, "sh600519", "贵州茅台")
	if !reflect.DeepEqual(assignment.selectedGroups(), []string{storage.DefaultWatchlistGroup, "长期"}) {
		t.Fatalf("unexpected initial memberships: %#v", assignment.selectedGroups())
	}
	assignment.move(1)
	assignment.toggle()
	if !reflect.DeepEqual(assignment.selectedGroups(), []string{storage.DefaultWatchlistGroup, "科技", "长期"}) {
		t.Fatalf("multi-select toggle failed: %#v", assignment.selectedGroups())
	}
	status := assignment.status(false)
	for _, expected := range []string{"分配分组：600519 贵州茅台", "[x] 默认  已加入", "> [x] 科技  将加入", "[x] 长期  已加入"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("assignment status missing %q:\n%s", expected, status)
		}
	}
	if !strings.Contains(assignment.controls(false), "Space勾选/取消") {
		t.Fatalf("assignment controls should explain multi-select: %q", assignment.controls(false))
	}
	assignment.checked = make(map[string]bool)
	if status := assignment.status(false); !strings.Contains(status, "保存时将保留到默认") {
		t.Fatalf("empty selection fallback should be visible:\n%s", status)
	}
}

func TestWatchBaseControlsAdvertisesGroupAssignment(t *testing.T) {
	if controls := watchBaseControls(false, false); !strings.Contains(controls, "m分配") {
		t.Fatalf("standard controls missing assignment shortcut: %q", controls)
	}
	if controls := watchBaseControls(false, true); !strings.Contains(controls, "M GROUP") {
		t.Fatalf("moyu controls missing assignment shortcut: %q", controls)
	}
	if controls := watchBaseControls(false, false); !strings.Contains(controls, "h历史") {
		t.Fatalf("standard controls missing history shortcut: %q", controls)
	}
	if controls := watchBaseControls(false, true); !strings.Contains(controls, "H HISTORY") {
		t.Fatalf("moyu controls missing history shortcut: %q", controls)
	}
	if controls := watchBaseControls(false, false); strings.Count(controls, "\n") != 2 || !strings.Contains(controls, "1涨幅前20  2跌幅前20  3快速涨幅前20") {
		t.Fatalf("standard controls should separate navigation, rankings and reports: %q", controls)
	}
	if controls := watchBaseControls(false, false); !strings.Contains(controls, "v资金雷达") {
		t.Fatalf("standard controls missing fund radar shortcut: %q", controls)
	}
	if controls := watchBaseControls(false, false); !strings.Contains(controls, "c个股研判") || !strings.Contains(controls, "o查看") {
		t.Fatalf("standard controls missing stock report shortcuts: %q", controls)
	}
	if controls := watchBaseControls(false, false); !strings.Contains(controls, "x咨询AI") {
		t.Fatalf("standard controls missing AI chat shortcut: %q", controls)
	}
	if controls := watchBaseControls(false, false); !strings.Contains(controls, "y板块资金") {
		t.Fatalf("standard controls missing board fund shortcut: %q", controls)
	}
}

func TestWatchMarketRankingNavigationAndShortcuts(t *testing.T) {
	items := make([]domain.MarketRankingItem, 20)
	for index := range items {
		items[index] = domain.MarketRankingItem{Symbol: "sh600519", Name: "贵州茅台"}
	}
	ranking := watchMarketRanking{}
	ranking.begin(domain.MarketRankingGainers, items)
	ranking.move(10)
	ranking.move(20)
	selected, ok := ranking.selectedItem()
	if !ok || ranking.selected != 19 || selected.Name != "贵州茅台" {
		t.Fatalf("unexpected ranking selection: %#v", ranking)
	}
	ranking.selectIndex(-1)
	if ranking.selected != 0 {
		t.Fatalf("ranking selection should clamp to first row: %#v", ranking)
	}
	if !strings.Contains(ranking.controls(false), "\n1涨幅前20") || !strings.Contains(ranking.controls(false), "v资金雷达") {
		t.Fatalf("ranking controls should use two lines: %q", ranking.controls(false))
	}
	ranking.selectIndex(2)
	ranking.refresh([]domain.MarketRankingItem{
		{Symbol: "sz000001", Name: "平安银行"},
		{Symbol: "sh600519", Name: "贵州茅台"},
	})
	selected, ok = ranking.selectedItem()
	if !ok || selected.Symbol != "sh600519" || ranking.refreshedAt.IsZero() {
		t.Fatalf("refresh should preserve the selected symbol and timestamp: %#v", ranking)
	}
	ranking.failRefresh(fmt.Errorf("network down"))
	if !strings.Contains(ranking.status(false), "榜单刷新失败") {
		t.Fatalf("ranking refresh error should be visible: %q", ranking.status(false))
	}
	for shortcut, expected := range map[string]domain.MarketRankingKind{
		"1": domain.MarketRankingGainers,
		"2": domain.MarketRankingLosers,
		"3": domain.MarketRankingRapidRise,
	} {
		kind, found := marketRankingShortcut(shortcut)
		if !found || kind != expected {
			t.Fatalf("unexpected shortcut %s: %q %v", shortcut, kind, found)
		}
	}
}
