package app

import (
	"fmt"
	"strings"

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
