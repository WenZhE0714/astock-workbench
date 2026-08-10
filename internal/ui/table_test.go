package ui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestMoyuTableHasStableFrame(t *testing.T) {
	frame := buildMoyuTable([]domain.Quote{{TaskName: "gui zhou mao tai", Current: "1418.00", Percent: 1.2, Low: "1398", High: "1420"}}, 80)
	if !strings.Contains(frame, "+-") || !strings.Contains(frame, "TASK") || !strings.Contains(frame, "FLOW") || !strings.Contains(frame, "gui zhou mao tai") {
		t.Fatalf("unexpected frame:\n%s", frame)
	}
}

func TestLiveMoyuFrameShowsMarketRefreshSelectionAndFundFlow(t *testing.T) {
	first := dashboardQuote()
	first.TaskName = "gui zhou mao tai"
	second := dashboardQuote()
	second.Symbol = "sz000001"
	second.Code = "000001"
	second.Name = "平安银行"
	second.TaskName = "ping an yin hang"
	second.Current = "12.34"
	second.Percent = -0.81
	frame := BuildLiveFrame(LiveData{
		Quotes: []domain.Quote{first, second},
		Indices: []domain.Quote{
			{Symbol: "sh000001", Current: "3635.13", Percent: 0.24},
			{Symbol: "sz399001", Current: "11128.67", Percent: -0.12},
			{Symbol: "sz399006", Current: "2341.56", Percent: 0.58},
		},
		Flows: map[string]domain.FundFlow{
			"sh600519": {Symbol: "sh600519", MainNet: 125000000, MainRatio: 3.25},
			"sz000001": {Symbol: "sz000001", MainNet: -6300000, MainRatio: -0.81},
			"sh000001": {Symbol: "sh000001", MainNet: 22349627392, MainRatio: 1.85},
			"sz399001": {Symbol: "sz399001", MainNet: 14089814016, MainRatio: 0.97},
			"sz399006": {Symbol: "sz399006", MainNet: 4331544576, MainRatio: 0.59},
		},
		RefreshedAt:  time.Date(2026, 8, 7, 15, 1, 2, 0, time.Local),
		MarketStatus: "已收盘",
		Selected:     1,
	}, ViewOptions{Moyu: true}, 79, 24)
	for _, expected := range []string{"WORKMON", "UPDATE 08-07 15:01:02", "CLOSED", "MARKET", "SSE 3635.13 +0.24%", "INDEX FLOW", "SSE ↑ 223.50亿", "CHINEXT ↑ 43.32亿", "FLOW", "> ping an yin hang", "↓ 630万", "TOTAL AMT  --"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("live moyu frame missing %q:\n%s", expected, frame)
		}
	}
	for _, line := range strings.Split(frame, "\n") {
		if width := displayWidth(line); width > 79 {
			t.Fatalf("line width %d exceeds terminal:\n%s", width, line)
		}
	}
}

func TestLiveFrameShowsNavigationFooter(t *testing.T) {
	frame := BuildLiveFrame(LiveData{Quotes: []domain.Quote{dashboardQuote()}}, ViewOptions{}, 79, 24)
	if !strings.Contains(frame, "↑/↓ 选择  Enter详情  Esc返回  a添加  d删除  i查看  q退出") {
		t.Fatalf("navigation footer missing:\n%s", frame)
	}
	commandFrame := BuildLiveFrame(LiveData{
		Quotes: []domain.Quote{dashboardQuote()},
		Status: "添加自选，请输入代码或完整名称：600519▌",
		Footer: "Enter确认  Esc取消",
	}, ViewOptions{}, 79, 24)
	if !strings.Contains(commandFrame, "添加自选，请输入代码或完整名称：600519▌\nEnter确认  Esc取消") {
		t.Fatalf("separate command status/footer missing:\n%s", commandFrame)
	}
}

func TestLiveStandardFrameShowsStockAndIndexFundFlow(t *testing.T) {
	first := dashboardQuote()
	frame := BuildLiveFrame(LiveData{
		Quotes:  []domain.Quote{first},
		Indices: []domain.Quote{{Symbol: "sh000001", Current: "3940.04", Percent: 1.02}},
		Flows: map[string]domain.FundFlow{
			"sh600519": {Symbol: "sh600519", MainNet: -116062624, MainRatio: -3.55},
			"sh000001": {Symbol: "sh000001", MainNet: 22349627392, MainRatio: 1.85},
		},
		RefreshedAt:  time.Date(2026, 8, 7, 15, 1, 2, 0, time.Local),
		MarketStatus: "已收盘",
	}, ViewOptions{}, 79, 24)
	for _, expected := range []string{"指数资金", "上证 ↑ 223.50亿", "资金", "涨停", "跌停", "1439.41", "1177.70", "> 贵州茅台", "↓ 1.16亿"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("live standard frame missing %q:\n%s", expected, frame)
		}
	}
	for _, line := range strings.Split(frame, "\n") {
		if width := displayWidth(line); width > 79 {
			t.Fatalf("line width %d exceeds terminal:\n%s", width, line)
		}
	}
}

func TestLiveStandardFrameUsesColorWhileMoyuStaysColorless(t *testing.T) {
	quote := dashboardQuote()
	data := LiveData{
		Quotes: []domain.Quote{quote},
		Indices: []domain.Quote{
			{Symbol: "sh000001", Current: "3940.04", Percent: 1.02, Delta: 39.69, Amount: 120954357},
			{Symbol: "sz399001", Current: "14311.01", Percent: 1.42, Delta: 200.89, Amount: 145487618},
		},
		Flows: map[string]domain.FundFlow{
			"sh600519": {Symbol: "sh600519", MainNet: -116062624},
			"sh000001": {Symbol: "sh000001", MainNet: 22349627392},
		},
		MarketStatus: "交易中",
	}
	standard := BuildLiveFrame(data, ViewOptions{Color: true}, 79, 24)
	if !strings.Contains(standard, "\x1b[") {
		t.Fatalf("standard frame should contain ANSI colors:\n%s", standard)
	}
	moyu := BuildLiveFrame(data, ViewOptions{Moyu: true, Color: false}, 79, 24)
	if strings.Contains(moyu, "\x1b[") {
		t.Fatalf("moyu frame should remain colorless:\n%s", moyu)
	}
}

func TestLiveDetailShowsOnlySelectedStockAndFlow(t *testing.T) {
	first := dashboardQuote()
	second := dashboardQuote()
	second.Symbol = "sz000001"
	second.Code = "000001"
	second.Name = "平安银行"
	frame := BuildLiveFrame(LiveData{
		Quotes:       []domain.Quote{first, second},
		Flows:        map[string]domain.FundFlow{"sz000001": {Symbol: "sz000001", MainNet: -6300000, MainRatio: -0.81}},
		MarketStatus: "交易中",
		Selected:     1,
		Detail:       true,
	}, ViewOptions{Moyu: true}, 79, 24)
	for _, expected := range []string{"平安银行", "000001", "主力资金", "↓ 630万", "主力净占比", "-0.81%"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("detail missing %q:\n%s", expected, frame)
		}
	}
	if strings.Contains(frame, "贵州茅台") {
		t.Fatalf("unselected stock leaked into detail:\n%s", frame)
	}
}

func TestLiveDetailShowsSixRelatedBoardFlowsAndAnalysis(t *testing.T) {
	quote := dashboardQuote()
	boards := []domain.BoardFlow{
		{Code: "BK0438", Name: "食品饮料", Kind: domain.BoardKindIndustry, Percent: 2.25, MainNet: 1199132256, MainRatio: 4.62, LeaderName: "金达威", LeaderPercent: 10.03},
		{Code: "BK1277", Name: "白酒Ⅱ", Kind: domain.BoardKindIndustry, Percent: 2.12, MainNet: 854524336, MainRatio: 7.31, LeaderName: "迎驾贡酒", LeaderPercent: 6.15},
		{Code: "BK1575", Name: "白酒Ⅲ", Kind: domain.BoardKindIndustry, Percent: 2.12, MainNet: 854524336, MainRatio: 7.31, LeaderName: "迎驾贡酒", LeaderPercent: 6.15},
		{Code: "BK1653", Name: "味蕾经济", Kind: domain.BoardKindConcept, Percent: 2.32, MainNet: 1069653264, MainRatio: 5.03, LeaderName: "一鸣食品", LeaderPercent: 9.98},
		{Code: "BK0896", Name: "白酒", Kind: domain.BoardKindConcept, Percent: 1.52, MainNet: -837661504, MainRatio: -6.21, LeaderName: "迎驾贡酒", LeaderPercent: 6.15},
		{Code: "BK0811", Name: "超级品牌", Kind: domain.BoardKindConcept, Percent: 1.39, MainNet: -948835904, MainRatio: -5.35, LeaderName: "贵州茅台", LeaderPercent: 2.89},
	}
	frame := BuildLiveFrame(LiveData{
		Quotes: []domain.Quote{quote},
		Flows: map[string]domain.FundFlow{
			quote.Symbol: {Symbol: quote.Symbol, MainNet: 125000000, MainRatio: 3.25},
		},
		Boards: boards, Detail: true, MarketStatus: "交易中",
	}, ViewOptions{}, 119, 35)
	for _, expected := range []string{
		"板块资金  多数净流入（4/6），个股与板块同向",
		"行业  食品饮料  +2.25%  ↑ 11.99亿 +4.62%  领涨 金达威 +10.03%",
		"概念  超级品牌  +1.39%  ↓ 9.49亿 -5.35%  领涨 贵州茅台 +2.89%",
	} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("board detail missing %q:\n%s", expected, frame)
		}
	}
	if count := strings.Count(frame, "领涨 "); count != 6 {
		t.Fatalf("expected six related boards, got %d:\n%s", count, frame)
	}
}

func TestBoardFlowSummaryComparesStockWithBoards(t *testing.T) {
	positiveBoards := []domain.BoardFlow{{MainNet: 3}, {MainNet: 2}, {MainNet: 1}, {MainNet: 1}, {MainNet: -1}, {MainNet: -2}}
	if got := boardFlowSummary(positiveBoards, &domain.FundFlow{MainNet: -1}); got != "多数净流入（4/6），个股弱于板块" {
		t.Fatalf("unexpected positive-board summary: %s", got)
	}
	negativeBoards := []domain.BoardFlow{{MainNet: -3}, {MainNet: -2}, {MainNet: -1}, {MainNet: -1}, {MainNet: 1}, {MainNet: 2}}
	if got := boardFlowSummary(negativeBoards, &domain.FundFlow{MainNet: 1}); got != "多数净流出（4/6），个股逆板块走强" {
		t.Fatalf("unexpected negative-board summary: %s", got)
	}
	mixedBoards := []domain.BoardFlow{{MainNet: 3}, {MainNet: 2}, {MainNet: 1}, {MainNet: -1}, {MainNet: -2}, {MainNet: -3}}
	if got := boardFlowSummary(mixedBoards, nil); got != "资金分化（流入3/流出3）" {
		t.Fatalf("unexpected mixed summary: %s", got)
	}
}

func TestMarketTotalAmountUsesShanghaiAndShenzhenOnly(t *testing.T) {
	indices := []domain.Quote{
		{Symbol: "sh000001", Amount: 120954357},
		{Symbol: "sz399001", Amount: 145487618},
		{Symbol: "sz399006", Amount: 73010373},
	}
	total := marketTotalAmount(indices)
	if total != 266441975 {
		t.Fatalf("unexpected market amount: %v", total)
	}
	if got := humanMarketAmount(total); got != "2.66万亿" {
		t.Fatalf("unexpected market amount display: %s", got)
	}
}

func TestMarketAmountOverviewShowsChangeFromPreviousTradingDay(t *testing.T) {
	indices := []domain.Quote{
		{Symbol: "sh000001", Amount: 120954357},
		{Symbol: "sz399001", Amount: 145487618},
	}
	previous := map[string]float64{"sh000001": 110000000, "sz399001": 140000000}
	line := marketAmountOverview(indices, previous, false, 79)
	for _, expected := range []string{"沪深总成交额", "2.66万亿", "↑", "1644.20亿元", "+6.58%"} {
		if !strings.Contains(line, expected) {
			t.Fatalf("market amount line missing %q: %s", expected, line)
		}
	}
}

func TestLiveMoyuFrameFitsNarrowTerminal(t *testing.T) {
	quote := dashboardQuote()
	quote.TaskName = "a very long disguised task name"
	frame := BuildLiveFrame(LiveData{
		Quotes:       []domain.Quote{quote},
		Flows:        map[string]domain.FundFlow{"sh600519": {Symbol: "sh600519", MainNet: 125000000}},
		MarketStatus: "交易中",
	}, ViewOptions{Moyu: true}, 36, 10)
	for _, line := range strings.Split(frame, "\n") {
		if width := displayWidth(line); width > 36 {
			t.Fatalf("line width %d exceeds narrow terminal:\n%s", width, line)
		}
	}
}

func TestDisplayWidth(t *testing.T) {
	if displayWidth("贵州A") != 5 {
		t.Fatalf("unexpected display width")
	}
}

func dashboardQuote() domain.Quote {
	return domain.Quote{
		Symbol: "sh600519", Name: "贵州茅台", Code: "600519",
		Current: "1308.00", PreviousClose: "1308.55", Open: "1308.66",
		QuoteTime: "2026-08-07 10:57:34", Delta: -0.55, Percent: -0.04,
		High: "1315.28", Low: "1301.00", Volume: 12135, Amount: 158585,
		Turnover: "0.10", Amplitude: "1.09", VolumeRatio: "0.84",
		AveragePrice: "1306.89", PETTM: "19.77", PB: "7.02",
		MarketCap: 16351.07, FloatMarketCap: 16000,
		LimitUp: "1439.41", LimitDown: "1177.70",
		Bids: []domain.DepthLevel{{Level: 1, Price: "1307.55", Volume: "11"}},
		Asks: []domain.DepthLevel{{Level: 1, Price: "1308.29", Volume: "1"}},
	}
}

func TestStandardDashboardContainsKeyMarketData(t *testing.T) {
	frame := BuildFrame(
		[]domain.Quote{dashboardQuote()},
		[]string{"sh600519"},
		ViewOptions{Color: false},
		80,
		time.Date(2026, 8, 7, 10, 57, 35, 0, time.Local),
		"",
	)
	for _, expected := range []string{
		"贵州茅台", "现价", "买一", "卖一", "1307.55", "1308.29",
		"振幅", "量比", "PE(TTM)", "总市值", "日内", "●",
	} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("dashboard missing %q:\n%s", expected, frame)
		}
	}
	if strings.Contains(frame, "\x1b[") {
		t.Fatalf("color-disabled dashboard contains ANSI escapes")
	}
}

func TestStandardDashboardFitsNarrowTerminal(t *testing.T) {
	frame := BuildFrame(
		[]domain.Quote{dashboardQuote()},
		[]string{"sh600519"},
		ViewOptions{Depth: true, Color: true},
		50,
		time.Now(),
		"",
	)
	for _, line := range strings.Split(frame, "\n") {
		if width := displayWidth(line); width > 50 {
			t.Fatalf("line width %d exceeds terminal:\n%s", width, line)
		}
	}
	if !strings.Contains(frame, "五档盘口") || !strings.Contains(frame, "\x1b[") {
		t.Fatalf("expected colored depth dashboard:\n%s", frame)
	}
}

func TestPlaceholderUsesUnavailableValues(t *testing.T) {
	quote := placeholderQuotes([]string{"sh600519"})[0]
	if !math.IsNaN(quote.MarketCap) || quote.PETTM != "--" {
		t.Fatalf("unexpected placeholder: %#v", quote)
	}
}
