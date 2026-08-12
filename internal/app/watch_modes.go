package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

const temporaryWatchlistGroup = "临时列表"

type watchSortState struct {
	active           bool
	picked           bool
	original         []string
	originalSelected int
}

func (state *watchSortState) begin(symbols []string, selected int) {
	*state = watchSortState{
		active: true, original: append([]string(nil), symbols...), originalSelected: selected,
	}
}

func (state *watchSortState) reset() {
	*state = watchSortState{}
}

func (state watchSortState) status(symbol, name string, moyu bool) string {
	if !state.active {
		return ""
	}
	if !state.picked {
		if moyu {
			return "REORDER: SELECT A STOCK, THEN PRESS ENTER"
		}
		return "调整顺序：请选择股票，按 Enter 选中"
	}
	label := symbol
	if len(symbol) == 8 {
		label = symbol[2:]
	}
	if name != "" {
		label += " " + name
	}
	if moyu {
		return "MOVING " + label
	}
	return "正在移动 " + label
}

func (state watchSortState) controls(moyu bool) string {
	if !state.active {
		return ""
	}
	if moyu {
		if state.picked {
			return "UP/DOWN MOVE  ENTER SAVE  ESC CANCEL"
		}
		return "UP/DOWN SELECT  ENTER PICK  ESC CANCEL"
	}
	if state.picked {
		return "↑/↓ 移动  Enter保存  Esc取消"
	}
	return "↑/↓ 选择  Enter选中  Esc取消"
}

func moveWatchlistSymbol(symbols []string, from, to int) bool {
	if from < 0 || from >= len(symbols) || to < 0 || to >= len(symbols) || from == to {
		return false
	}
	value := symbols[from]
	if from < to {
		copy(symbols[from:to], symbols[from+1:to+1])
	} else {
		copy(symbols[to+1:from+1], symbols[to:from])
	}
	symbols[to] = value
	return true
}

func reorderQuotes(quotes []domain.Quote, symbols []string) []domain.Quote {
	bySymbol := make(map[string]domain.Quote, len(quotes))
	for _, quote := range quotes {
		bySymbol[quote.Symbol] = quote
	}
	result := make([]domain.Quote, 0, len(symbols))
	for _, symbol := range symbols {
		if quote, ok := bySymbol[symbol]; ok {
			result = append(result, quote)
		}
	}
	return result
}

type watchGroupChooser struct {
	active   bool
	groups   []storage.WatchlistGroup
	selected int
}

func (chooser *watchGroupChooser) begin(groups []storage.WatchlistGroup, currentGroup string) {
	chooser.active = true
	chooser.groups = make([]storage.WatchlistGroup, 0, len(groups)+1)
	chooser.groups = append(chooser.groups, storage.WatchlistGroup{
		Name:    storage.AllWatchlistGroup,
		Symbols: storage.WatchlistSymbols(groups, storage.AllWatchlistGroup),
	})
	chooser.groups = append(chooser.groups, groups...)
	chooser.selected = 0
	for index, group := range chooser.groups {
		if group.Name == currentGroup {
			chooser.selected = index
			break
		}
	}
}

func (chooser *watchGroupChooser) reset() {
	*chooser = watchGroupChooser{}
}

func (chooser *watchGroupChooser) move(delta int) {
	if !chooser.active || len(chooser.groups) == 0 {
		return
	}
	chooser.selected += delta
	if chooser.selected < 0 {
		chooser.selected = 0
	}
	if chooser.selected >= len(chooser.groups) {
		chooser.selected = len(chooser.groups) - 1
	}
}

func (chooser watchGroupChooser) selectedGroup() (storage.WatchlistGroup, bool) {
	if !chooser.active || chooser.selected < 0 || chooser.selected >= len(chooser.groups) {
		return storage.WatchlistGroup{}, false
	}
	return chooser.groups[chooser.selected], true
}

func (chooser watchGroupChooser) status(moyu bool) string {
	if !chooser.active {
		return ""
	}
	var builder strings.Builder
	if moyu {
		builder.WriteString("SELECT GROUP:")
	} else {
		builder.WriteString("选择自选分组：")
	}
	for index, group := range chooser.groups {
		marker := "  "
		if index == chooser.selected {
			marker = "> "
		}
		fmt.Fprintf(&builder, "\n%s%s  %d只", marker, group.Name, len(group.Symbols))
	}
	return builder.String()
}

func (chooser watchGroupChooser) controls(moyu bool) string {
	if !chooser.active {
		return ""
	}
	if moyu {
		return "UP/DOWN SELECT  ENTER OPEN  N NEW  D DELETE  ESC CANCEL"
	}
	return "↑/↓ 选择  Enter打开  n新建  d删除  Esc取消"
}

type watchGroupAssignment struct {
	active   bool
	symbol   string
	name     string
	groups   []storage.WatchlistGroup
	selected int
	original map[string]bool
	checked  map[string]bool
}

func (assignment *watchGroupAssignment) begin(groups []storage.WatchlistGroup, symbol, name string) {
	*assignment = watchGroupAssignment{
		active:   true,
		symbol:   symbol,
		name:     name,
		groups:   append([]storage.WatchlistGroup(nil), groups...),
		original: make(map[string]bool),
		checked:  make(map[string]bool),
	}
	for _, group := range groups {
		for _, member := range group.Symbols {
			if member == symbol {
				assignment.original[group.Name] = true
				assignment.checked[group.Name] = true
				break
			}
		}
	}
}

func (assignment *watchGroupAssignment) reset() {
	*assignment = watchGroupAssignment{}
}

func (assignment *watchGroupAssignment) move(delta int) {
	if !assignment.active || len(assignment.groups) == 0 {
		return
	}
	assignment.selected += delta
	if assignment.selected < 0 {
		assignment.selected = 0
	}
	if assignment.selected >= len(assignment.groups) {
		assignment.selected = len(assignment.groups) - 1
	}
}

func (assignment *watchGroupAssignment) toggle() {
	if !assignment.active || assignment.selected < 0 || assignment.selected >= len(assignment.groups) {
		return
	}
	name := assignment.groups[assignment.selected].Name
	assignment.checked[name] = !assignment.checked[name]
}

func (assignment watchGroupAssignment) selectedGroups() []string {
	result := make([]string, 0, len(assignment.checked))
	for _, group := range assignment.groups {
		if assignment.checked[group.Name] {
			result = append(result, group.Name)
		}
	}
	return result
}

func (assignment watchGroupAssignment) status(moyu bool) string {
	if !assignment.active {
		return ""
	}
	code := assignment.symbol
	if len(code) == 8 {
		code = code[2:]
	}
	label := strings.TrimSpace(code + " " + assignment.name)
	var builder strings.Builder
	if moyu {
		fmt.Fprintf(&builder, "ASSIGN GROUPS: %s", label)
	} else {
		fmt.Fprintf(&builder, "分配分组：%s", label)
	}
	for index, group := range assignment.groups {
		cursor := "  "
		if index == assignment.selected {
			cursor = "> "
		}
		check := "[ ]"
		if assignment.checked[group.Name] {
			check = "[x]"
		}
		change := ""
		switch {
		case assignment.original[group.Name] && !assignment.checked[group.Name]:
			change = "  REMOVE"
			if !moyu {
				change = "  将移出"
			}
		case !assignment.original[group.Name] && assignment.checked[group.Name]:
			change = "  ADD"
			if !moyu {
				change = "  将加入"
			}
		case assignment.original[group.Name]:
			change = "  CURRENT"
			if !moyu {
				change = "  已加入"
			}
		}
		fmt.Fprintf(&builder, "\n%s%s %s%s", cursor, check, group.Name, change)
	}
	if len(assignment.selectedGroups()) == 0 {
		if moyu {
			builder.WriteString("\nNO GROUP SELECTED: DEFAULT WILL BE KEPT")
		} else {
			builder.WriteString("\n未勾选任何分组，保存时将保留到默认")
		}
	}
	return builder.String()
}

func (assignment watchGroupAssignment) controls(moyu bool) string {
	if !assignment.active {
		return ""
	}
	if moyu {
		return "UP/DOWN SELECT  SPACE TOGGLE  ENTER SAVE  ESC CANCEL"
	}
	return "↑/↓ 选择  Space勾选/取消  Enter保存  Esc取消"
}

type watchMarketRanking struct {
	active       bool
	kind         domain.MarketRankingKind
	items        []domain.MarketRankingItem
	selected     int
	refreshedAt  time.Time
	refreshError string
}

func (ranking *watchMarketRanking) begin(kind domain.MarketRankingKind, items []domain.MarketRankingItem) {
	*ranking = watchMarketRanking{
		active:      true,
		kind:        kind,
		items:       append([]domain.MarketRankingItem(nil), items...),
		refreshedAt: time.Now(),
	}
}

func (ranking *watchMarketRanking) refresh(items []domain.MarketRankingItem) {
	if !ranking.active {
		return
	}
	selectedSymbol := ""
	if item, ok := ranking.selectedItem(); ok {
		selectedSymbol = item.Symbol
	}
	ranking.items = append([]domain.MarketRankingItem(nil), items...)
	ranking.selected = 0
	for index, item := range ranking.items {
		if item.Symbol == selectedSymbol {
			ranking.selected = index
			break
		}
	}
	ranking.refreshedAt = time.Now()
	ranking.refreshError = ""
}

func (ranking *watchMarketRanking) failRefresh(err error) {
	if ranking.active && err != nil {
		ranking.refreshError = err.Error()
	}
}

func (ranking *watchMarketRanking) reset() {
	*ranking = watchMarketRanking{}
}

func (ranking *watchMarketRanking) move(delta int) {
	if !ranking.active || len(ranking.items) == 0 {
		return
	}
	ranking.selected += delta
	if ranking.selected < 0 {
		ranking.selected = 0
	}
	if ranking.selected >= len(ranking.items) {
		ranking.selected = len(ranking.items) - 1
	}
}

func (ranking *watchMarketRanking) selectIndex(index int) {
	if !ranking.active || len(ranking.items) == 0 {
		return
	}
	ranking.selected = index
	ranking.move(0)
}

func (ranking watchMarketRanking) selectedItem() (domain.MarketRankingItem, bool) {
	if !ranking.active || ranking.selected < 0 || ranking.selected >= len(ranking.items) {
		return domain.MarketRankingItem{}, false
	}
	return ranking.items[ranking.selected], true
}

func (ranking watchMarketRanking) controls(moyu bool) string {
	if !ranking.active {
		return ""
	}
	if moyu {
		return "UP/DOWN SELECT  [/]/PGUP/PGDN JUMP  ENTER DETAIL  ESC BACK  Q QUIT\n1 GAINERS  2 LOSERS  3 RAPID RISE  V FUND RADAR\nC STOCK REPORT  O OPEN  S MARKET REPORT  R OPEN"
	}
	return "↑/↓选择  [/]跳选  Enter详情  Esc返回  q退出\n1涨幅前20  2跌幅前20  3快速涨幅前20  v资金雷达\nc个股研判  o查看  s市场报告  r查看"
}

func (ranking watchMarketRanking) status(moyu bool) string {
	if !ranking.active || ranking.refreshError == "" {
		return ""
	}
	if moyu {
		return "RANKING REFRESH FAILED; KEEPING " + ranking.refreshedAt.Format("15:04:05") + ": " + ranking.refreshError
	}
	return "榜单刷新失败，保留 " + ranking.refreshedAt.Format("15:04:05") + " 数据: " + ranking.refreshError
}

func marketRankingShortcut(value string) (domain.MarketRankingKind, bool) {
	switch value {
	case "1":
		return domain.MarketRankingGainers, true
	case "2":
		return domain.MarketRankingLosers, true
	case "3":
		return domain.MarketRankingRapidRise, true
	default:
		return "", false
	}
}

func watchBaseControls(detail, moyu bool) string {
	if moyu {
		if detail {
			return "UP/DOWN SCROLL  [/]/PGUP/PGDN PAGE  ESC BACK  Q QUIT\nC STOCK REPORT  O OPEN  S MARKET REPORT  R OPEN"
		}
		return "UP/DN  ENTER  A ADD  D DEL  I VIEW  H HISTORY  E SORT  F GROUP  M GROUP  Q QUIT\n1 GAINERS  2 LOSERS  3 RAPID RISE  V FUND RADAR\nC STOCK REPORT  O OPEN  S MARKET REPORT  R OPEN"
	}
	if detail {
		return "↑/↓ 滚动  [/]翻页  Esc返回  q退出\nc个股研判  o查看  s市场报告  r查看"
	}
	return "↑/↓选择  Enter详情  a添加  d删除  i查看  h历史  e排序  f分组  m分配  q退出\n1涨幅前20  2跌幅前20  3快速涨幅前20  v资金雷达\nc个股研判  o查看  s市场报告  r查看"
}
