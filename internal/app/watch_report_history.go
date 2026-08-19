package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/ui"
)

type reportHistoryKind string

const (
	reportHistoryMarket reportHistoryKind = "market"
	reportHistoryStock  reportHistoryKind = "stock"
)

type reportHistoryView string

const (
	reportHistoryDates reportHistoryView = "dates"
	reportHistoryTimes reportHistoryView = "times"
)

type reportHistoryItem struct {
	id          string
	generatedAt time.Time
	aiUsed      bool
	aiError     string
}

type watchReportHistory struct {
	viewing      bool
	kind         reportHistoryKind
	symbol       string
	name         string
	view         reportHistoryView
	selected     int
	selectedDate string
	items        []reportHistoryItem
	error        string
}

func (state *watchReportHistory) reset(kind reportHistoryKind, symbol, name string) {
	state.viewing = true
	state.kind = kind
	state.symbol = symbol
	state.name = name
	state.view = reportHistoryDates
	state.selected = 0
	state.selectedDate = ""
	state.items = nil
	state.error = ""
}

func (state *watchReportHistory) openMarket(items []storage.MarketReportIndexEntry) {
	state.reset(reportHistoryMarket, "", "")
	for _, item := range items {
		state.items = append(state.items, reportHistoryItem{
			id: item.ID, generatedAt: item.GeneratedAt, aiUsed: item.AIUsed, aiError: item.AIError,
		})
	}
}

func (state *watchReportHistory) openStock(symbol, name string, items []storage.StockReportIndexEntry) {
	state.reset(reportHistoryStock, symbol, name)
	for _, item := range items {
		state.items = append(state.items, reportHistoryItem{
			id: item.ID, generatedAt: item.GeneratedAt, aiUsed: item.AIUsed, aiError: item.AIError,
		})
	}
}

func (state *watchReportHistory) close() {
	state.viewing = false
	state.error = ""
}

func (state watchReportHistory) dates() []string {
	result := make([]string, 0, len(state.items))
	seen := make(map[string]bool, len(state.items))
	for _, item := range state.items {
		date := item.generatedAt.In(shanghaiLocation).Format("2006-01-02")
		if seen[date] {
			continue
		}
		seen[date] = true
		result = append(result, date)
	}
	return result
}

func (state watchReportHistory) reportsForDate(date string) []reportHistoryItem {
	result := make([]reportHistoryItem, 0)
	for _, item := range state.items {
		if item.generatedAt.In(shanghaiLocation).Format("2006-01-02") == date {
			result = append(result, item)
		}
	}
	return result
}

func (state watchReportHistory) itemCount() int {
	if state.view == reportHistoryTimes {
		return len(state.reportsForDate(state.selectedDate))
	}
	return len(state.dates())
}

func (state *watchReportHistory) move(delta int) {
	count := state.itemCount()
	if count == 0 {
		state.selected = 0
		return
	}
	state.selected += delta
	if state.selected < 0 {
		state.selected = 0
	}
	if state.selected >= count {
		state.selected = count - 1
	}
}

func (state *watchReportHistory) selectEndpoint(end bool) {
	if end {
		state.selected = state.itemCount() - 1
	} else {
		state.selected = 0
	}
	state.move(0)
}

func (state *watchReportHistory) openSelectedDate() bool {
	dates := state.dates()
	if state.view != reportHistoryDates || state.selected < 0 || state.selected >= len(dates) {
		return false
	}
	state.selectedDate = dates[state.selected]
	state.view = reportHistoryTimes
	state.selected = 0
	state.error = ""
	return true
}

func (state watchReportHistory) selectedReport() (reportHistoryItem, bool) {
	items := state.reportsForDate(state.selectedDate)
	if state.view != reportHistoryTimes || state.selected < 0 || state.selected >= len(items) {
		return reportHistoryItem{}, false
	}
	return items[state.selected], true
}

func (state *watchReportHistory) back() bool {
	if state.view == reportHistoryTimes {
		dates := state.dates()
		selectedDate := state.selectedDate
		state.view = reportHistoryDates
		state.selected = 0
		for index, date := range dates {
			if date == selectedDate {
				state.selected = index
				break
			}
		}
		state.error = ""
		return true
	}
	state.close()
	return false
}

func (state watchReportHistory) controls(moyu bool) string {
	if moyu {
		return "UP/DOWN SELECT  [/] JUMP  G/G ENDPOINTS  ENTER OPEN  ESC BACK  Q QUIT"
	}
	return "↑/↓选择  [/]跳选  g/G首尾  Enter进入/打开  Esc返回  q退出"
}

func reportHistoryEngine(item reportHistoryItem, moyu bool) string {
	if item.aiUsed {
		if moyu {
			return "CODEX"
		}
		return "Codex综合"
	}
	if strings.TrimSpace(item.aiError) != "" {
		if moyu {
			return "FALLBACK"
		}
		return "量化回退"
	}
	if moyu {
		return "RULE-BASED"
	}
	return "量化版"
}

func (state watchReportHistory) frame(width int, moyu, color bool) string {
	data := ui.ReportHistoryFrame{
		Controls: state.controls(moyu), Selected: state.selected, Error: state.error,
	}
	if state.kind == reportHistoryStock {
		data.Context = stockReportLabel(state.symbol, state.name)
	}
	if state.view == reportHistoryTimes {
		data.Date = state.selectedDate
		for _, item := range state.reportsForDate(state.selectedDate) {
			data.Items = append(data.Items, fmt.Sprintf(
				"%s  %s", item.generatedAt.In(shanghaiLocation).Format("15:04:05"), reportHistoryEngine(item, moyu),
			))
		}
	} else {
		for _, date := range state.dates() {
			items := state.reportsForDate(date)
			latest := "--:--:--"
			if len(items) > 0 {
				latest = items[0].generatedAt.In(shanghaiLocation).Format("15:04:05")
			}
			if moyu {
				data.Items = append(data.Items, fmt.Sprintf("%s  %d REPORTS  LATEST %s", date, len(items), latest))
			} else {
				data.Items = append(data.Items, fmt.Sprintf("%s  %d 份报告  最新 %s", date, len(items), latest))
			}
		}
	}
	data.Items, data.Selected = strategyLabVisibleItems(data.Items, state.selected, 18)
	return ui.BuildReportHistoryFrame(data, width, moyu, color)
}
