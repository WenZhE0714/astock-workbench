package app

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func TestParseWatchOptions(t *testing.T) {
	result, err := parseWatchOptions([]string{"--pinyin", "--depth", "--interval", "5", "--source", "tdx", "贵州茅台,000001"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pinyin || !result.Moyu || result.Depth || result.Color || result.Interval != 5 || result.Source != "tdx" || len(result.Inputs) != 2 {
		t.Fatalf("unexpected options: %#v", result)
	}
}

func TestDefaultRefreshIntervalIsOneSecond(t *testing.T) {
	t.Setenv("ASTOCK_MARKET_SOURCE", "")
	result, err := parseWatchOptions([]string{"600519"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Interval != 1 {
		t.Fatalf("expected 1 second, got %d", result.Interval)
	}
	if result.Source != marketSourceTDX {
		t.Fatalf("expected default TDX source, got %q", result.Source)
	}
}

func TestWatchMarketSourceEnvironmentOverridesTDXDefault(t *testing.T) {
	t.Setenv("ASTOCK_MARKET_SOURCE", marketSourceHTTP)
	result, err := parseWatchOptions([]string{"600519"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != marketSourceHTTP {
		t.Fatalf("expected HTTP environment override, got %q", result.Source)
	}
}

func TestQuoteRequestsIncludeAmountIndicesButFundFlowDoesNot(t *testing.T) {
	quoteSymbols := quoteRequestSymbols([]string{"sh600519", "th881155", "BK0438"})
	for _, expected := range []string{"sh600519", "sh000001", "sz399001", "sz399006", "sz399106", "bj899050"} {
		if !containsString(quoteSymbols, expected) {
			t.Fatalf("quote request missing %s: %#v", expected, quoteSymbols)
		}
	}
	for _, excluded := range []string{"th881155", "BK0438"} {
		if containsString(quoteSymbols, excluded) {
			t.Fatalf("quote request should exclude board symbol %s: %#v", excluded, quoteSymbols)
		}
	}
	flowSymbols := fundFlowRequestSymbols([]string{"sh600519", "th881155", "BK0438"})
	for _, excluded := range []string{"sz399106", "bj899050", "th881155", "BK0438"} {
		if containsString(flowSymbols, excluded) {
			t.Fatalf("fund-flow request should exclude quote-only symbol %s: %#v", excluded, flowSymbols)
		}
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
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
		name       string
		hour       int
		minute     int
		label      string
		poll       bool
		continuous bool
	}{
		{name: "before auction", hour: 9, minute: 14, label: "未开盘"},
		{name: "call auction starts", hour: 9, minute: 15, label: "集合竞价", poll: true},
		{name: "call auction", hour: 9, minute: 24, label: "集合竞价", poll: true},
		{name: "waiting open", hour: 9, minute: 29, label: "开盘等待", poll: true},
		{name: "morning", hour: 9, minute: 30, label: "交易中", poll: true, continuous: true},
		{name: "lunch", hour: 11, minute: 30, label: "午间休市"},
		{name: "afternoon", hour: 13, minute: 0, label: "交易中", poll: true, continuous: true},
		{name: "closed", hour: 15, minute: 0, label: "已收盘"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			at := time.Date(date.Year(), date.Month(), date.Day(), test.hour, test.minute, 0, 0, shanghaiLocation)
			got := marketSessionAt(at)
			if got.Label != test.label || got.Poll != test.poll || got.Continuous != test.continuous {
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

func TestWatchViewStateKeepsSpaceAsPageDown(t *testing.T) {
	state := watchViewState{}
	if changed, _ := state.handle(terminalKeySpace, 30); !changed || state.Selected != 10 {
		t.Fatalf("space should page down in list mode: %#v", state)
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
	command.confirmDelete("sh600519", "贵州茅台", storage.AllWatchlistGroup)
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

func TestWatchAIChatCommandExplainsLiveContext(t *testing.T) {
	command := watchCommand{}
	command.begin(watchCommandAIChat)
	status := command.status(false, true)
	for _, expected := range []string{"咨询AI", "行情", "资金", "板块", "技术面", "▌"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("AI chat command status missing %q: %s", expected, status)
		}
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
	for _, expected := range []string{"名称“石化”匹配到多个证券或板块，请选择：", "  600028  中国石化  沪市", "> 000703  恒逸石化  深市"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("candidate status missing %q:\n%s", expected, status)
		}
	}
	if got := command.controls(false); got != "↑/↓ 选择  [/]翻页  Enter确认  Esc取消" {
		t.Fatalf("unexpected candidate controls: %q", got)
	}
}

func TestWatchCommandCandidateWindowScrollsLargeMatches(t *testing.T) {
	command := watchCommand{}
	command.begin(watchCommandJump)
	candidates := make([]domain.Candidate, 25)
	for index := range candidates {
		candidates[index] = domain.Candidate{Symbol: fmt.Sprintf("sh%06d", 600000+index), Name: fmt.Sprintf("候选%d", index)}
	}
	command.chooseCandidates("银行", candidates)
	start, end := command.candidateWindow()
	if start != 0 || end != 10 {
		t.Fatalf("unexpected initial candidate window: %d-%d", start, end)
	}
	command.selectCandidate(24)
	start, end = command.candidateWindow()
	if start != 15 || end != 25 {
		t.Fatalf("candidate window did not follow end selection: %d-%d", start, end)
	}
	if !strings.Contains(command.status(false, false), "候选24") || strings.Contains(command.status(false, false), "候选0") {
		t.Fatalf("large candidate list did not scroll to selected item:\n%s", command.status(false, false))
	}
}

func TestWatchCommandShowsRecentHistoryAndKeepsSelectionVisible(t *testing.T) {
	command := watchCommand{}
	command.begin(watchCommandHistory)
	candidates := make([]domain.Candidate, 12)
	for index := range candidates {
		candidates[index] = domain.Candidate{
			Symbol: fmt.Sprintf("sh%06d", 600000+index),
			Name:   fmt.Sprintf("股票%d", index),
		}
	}
	command.chooseCandidates("", candidates)
	command.selectCandidate(11)
	selected, ok := command.selectedCandidate()
	if !ok || selected.Symbol != "sh600011" {
		t.Fatalf("unexpected history candidate: %#v", selected)
	}
	status := command.status(false, false)
	for _, expected := range []string{"最近查看（最新在前） 3-12/12：", "> 600011  股票11  沪市"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("history status missing %q:\n%s", expected, status)
		}
	}
	if strings.Contains(status, "600000") {
		t.Fatalf("history status should render a bounded window:\n%s", status)
	}
	if got := command.controls(false); got != "↑/↓ 选择  [/]跳选  Enter打开  Esc取消" {
		t.Fatalf("unexpected history controls: %q", got)
	}
}

func TestWatchGroupCommandsShowExplicitPrompts(t *testing.T) {
	command := watchCommand{}
	command.begin(watchCommandGroupCreate)
	command.buffer = "科技"
	if got := command.status(false, true); got != "新建分组，请输入名称：科技▌" {
		t.Fatalf("unexpected create prompt: %q", got)
	}
	command.confirmGroupDelete("科技")
	if got := command.status(false, false); got != "确认删除分组“科技”？独有股票将移到默认分组" {
		t.Fatalf("unexpected delete prompt: %q", got)
	}
	command.confirmDelete("sz300750", "宁德时代", "科技")
	if got := command.status(false, false); got != "确认从“科技”移出 300750 宁德时代？" {
		t.Fatalf("unexpected grouped stock prompt: %q", got)
	}
}
