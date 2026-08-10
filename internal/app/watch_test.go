package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestParseWatchOptions(t *testing.T) {
	result, err := parseWatchOptions([]string{"--pinyin", "--depth", "--interval", "5", "贵州茅台,000001"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pinyin || !result.Moyu || result.Depth || result.Color || result.Interval != 5 || len(result.Inputs) != 2 {
		t.Fatalf("unexpected options: %#v", result)
	}
}

func TestDefaultRefreshIntervalIsOneSecond(t *testing.T) {
	result, err := parseWatchOptions([]string{"600519"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Interval != 1 {
		t.Fatalf("expected 1 second, got %d", result.Interval)
	}
}

func TestTerminalWidthLeavesAutowrapGuardColumn(t *testing.T) {
	t.Setenv("COLUMNS", "80")
	if width := terminalWidth(&bytes.Buffer{}); width != 79 {
		t.Fatalf("expected guarded width 79, got %d", width)
	}
}

func TestTerminalHeightUsesVisibleRows(t *testing.T) {
	t.Setenv("LINES", "17")
	if height := terminalHeight(&bytes.Buffer{}); height != 17 {
		t.Fatalf("expected height 17, got %d", height)
	}
}

func TestMarketSessionAtStopsPollingOutsideTradingHours(t *testing.T) {
	date := time.Date(2026, 8, 3, 0, 0, 0, 0, shanghaiLocation)
	tests := []struct {
		name   string
		hour   int
		minute int
		label  string
		poll   bool
	}{
		{name: "before open", hour: 9, minute: 29, label: "未开盘"},
		{name: "morning", hour: 9, minute: 30, label: "交易中", poll: true},
		{name: "lunch", hour: 11, minute: 30, label: "午间休市"},
		{name: "afternoon", hour: 13, minute: 0, label: "交易中", poll: true},
		{name: "closed", hour: 15, minute: 0, label: "已收盘"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			at := time.Date(date.Year(), date.Month(), date.Day(), test.hour, test.minute, 0, 0, shanghaiLocation)
			got := marketSessionAt(at)
			if got.Label != test.label || got.Poll != test.poll {
				t.Fatalf("unexpected session at %s: %#v", at, got)
			}
		})
	}
	weekend := marketSessionAt(time.Date(2026, 8, 2, 10, 0, 0, 0, shanghaiLocation))
	if weekend.Label != "休市" || weekend.Poll {
		t.Fatalf("weekend should not poll: %#v", weekend)
	}
}

func TestWatchViewStateSelectsAndOpensDetail(t *testing.T) {
	state := watchViewState{}
	if changed, _ := state.handle(terminalKeyUp, 3); changed || state.Selected != 0 {
		t.Fatalf("selection moved above first item: %#v", state)
	}
	if changed, _ := state.handle(terminalKeyDown, 3); !changed || state.Selected != 1 {
		t.Fatalf("down did not select next item: %#v", state)
	}
	if changed, _ := state.handle(terminalKeyEnter, 3); !changed || !state.Detail {
		t.Fatalf("enter did not open detail: %#v", state)
	}
	if changed, _ := state.handle(terminalKeyDown, 3); changed || state.Selected != 1 {
		t.Fatalf("detail view should not change selection: %#v", state)
	}
	if changed, _ := state.handle(terminalKeyBack, 3); !changed || state.Detail {
		t.Fatalf("escape did not return to list: %#v", state)
	}
	state.handle(terminalKeyEnd, 3)
	state.handle(terminalKeyDown, 3)
	if state.Selected != 2 {
		t.Fatalf("selection moved below last item: %#v", state)
	}
}

func TestWatchCommandFooterAndRuneEditing(t *testing.T) {
	command := watchCommand{}
	command.begin(watchCommandAdd)
	command.buffer = "贵州茅台"
	if got := removeLastRune(command.buffer); got != "贵州茅" {
		t.Fatalf("removeLastRune split UTF-8 text: %q", got)
	}
	if got := command.status(false, true); got != "添加自选，请输入代码或完整名称：贵州茅台▌" {
		t.Fatalf("unexpected command status: %q", got)
	}
	if got := command.controls(false); got != "Enter确认  Esc取消" {
		t.Fatalf("unexpected command controls: %q", got)
	}
	command.confirmDelete("sh600519", "贵州茅台")
	if command.buffer != "" || !command.confirm {
		t.Fatalf("delete should confirm the selected symbol without input: %#v", command)
	}
	if got := command.status(false, false); got != "确认删除 600519 贵州茅台？" {
		t.Fatalf("unexpected confirmation footer: %q", got)
	}
	if got := command.controls(false); got != "Enter/y确认  Esc/n取消" {
		t.Fatalf("unexpected confirmation controls: %q", got)
	}
}

func TestWatchCommandSelectsAmbiguousCandidate(t *testing.T) {
	command := watchCommand{}
	command.begin(watchCommandAdd)
	command.chooseCandidates("石化", []domain.Candidate{
		{Symbol: "sh600028", Name: "中国石化"},
		{Symbol: "sz000703", Name: "恒逸石化"},
		{Symbol: "sh600688", Name: "上海石化"},
	})
	if !command.choosing() {
		t.Fatal("candidate selection should be active")
	}
	command.moveCandidate(1)
	selected, ok := command.selectedCandidate()
	if !ok || selected.Symbol != "sz000703" {
		t.Fatalf("unexpected selected candidate: %#v", selected)
	}
	status := command.status(false, false)
	for _, expected := range []string{"名称“石化”匹配到多只沪深 A 股，请选择：", "  600028  中国石化  沪市", "> 000703  恒逸石化  深市"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("candidate status missing %q:\n%s", expected, status)
		}
	}
	if got := command.controls(false); got != "↑/↓ 选择  Enter确认  Esc取消" {
		t.Fatalf("unexpected candidate controls: %q", got)
	}
}
