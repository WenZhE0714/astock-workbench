package ui

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestMoyuTableHasStableFrame(t *testing.T) {
	frame := buildQuoteTable(
		[]domain.Quote{{Symbol: "sh600519", TaskName: "gui zhou mao tai", Current: "1418.00", Percent: 1.2, Low: "1398", High: "1420"}},
		map[string]domain.FundFlow{"sh600519": {Symbol: "sh600519", Speed: 0.18}}, -1, 80, true, false,
	)
	if !strings.Contains(frame, "+-") || !strings.Contains(frame, "TASK") || !strings.Contains(frame, "SPEED") || !strings.Contains(frame, "+0.18%") || !strings.Contains(frame, "FLOW") || !strings.Contains(frame, "gui zhou mao tai") {
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
			"sh600519": {Symbol: "sh600519", Speed: 0.18, MainNet: 125000000, MainRatio: 3.25},
			"sz000001": {Symbol: "sz000001", Speed: -0.06, MainNet: -6300000, MainRatio: -0.81},
			"sh000001": {Symbol: "sh000001", MainNet: 22349627392, MainRatio: 1.85},
			"sz399001": {Symbol: "sz399001", MainNet: 14089814016, MainRatio: 0.97},
			"sz399006": {Symbol: "sz399006", MainNet: 4331544576, MainRatio: 0.59},
		},
		RefreshedAt:  time.Date(2026, 8, 7, 15, 1, 2, 0, time.Local),
		MarketStatus: "已收盘",
		Selected:     1,
	}, ViewOptions{Moyu: true}, 79, 24)
	for _, expected := range []string{"WORKMON", "UPDATE 08-07 15:01:02", "CLOSED", "MARKET", "SSE", "3635.13", "+0.24%", "INDEX FLOW", "↑ 223.50亿", "↑ 43.32亿", "SPEED", "-0.06%", "FLOW", "> ping an yin hang", "↓ 630万", "TOTAL AMT   --"} {
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
	if !strings.Contains(frame, "↑/↓ 选择  Enter详情  a添加  d删除  i查看  h历史  e排序  f分组  q退出") {
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
	moyuFrame := BuildLiveFrame(LiveData{Quotes: []domain.Quote{dashboardQuote()}}, ViewOptions{Moyu: true}, 79, 24)
	if !strings.Contains(moyuFrame, "H HISTORY") {
		t.Fatalf("moyu navigation footer missing history shortcut:\n%s", moyuFrame)
	}
}

func TestLiveMarketRankingShowsIndustryMetricsAndSecondControlLine(t *testing.T) {
	items := make([]domain.MarketRankingItem, 20)
	for index := range items {
		items[index] = domain.MarketRankingItem{
			Symbol: "sh688166", Name: fmt.Sprintf("医药股%d", index+1),
			Price: 43.33, Percent: 15.82 - float64(index), Speed: 3.02,
			Industry: "化学制药",
		}
	}
	frame := BuildLiveFrame(LiveData{
		RankingKind: domain.MarketRankingGainers, RankingItems: items, RankingSelected: 1,
		GroupName: "默认", GroupCount: 3,
	}, ViewOptions{}, 79, 50)
	for _, expected := range []string{"涨幅榜 TOP 20", "排名", "代码", "名称", "涨幅", "涨速", "行业", "化学制药", "> 02", "1涨幅前20", "3快速涨幅前20"} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("ranking frame missing %q:\n%s", expected, frame)
		}
	}
	if strings.Contains(frame, "自选分组") {
		t.Fatalf("ranking frame should not show the watchlist group header:\n%s", frame)
	}
	for _, line := range strings.Split(frame, "\n") {
		if width := displayWidth(line); width > 79 {
			t.Fatalf("ranking line width %d exceeds terminal:\n%s", width, line)
		}
	}
}

func TestLiveMarketRankingKeepsSelectedRowInHeightAwareWindow(t *testing.T) {
	items := make([]domain.MarketRankingItem, 20)
	for index := range items {
		items[index] = domain.MarketRankingItem{
			Symbol: fmt.Sprintf("sz%06d", 300000+index), Name: fmt.Sprintf("股票%d", index+1),
			Percent: -float64(index), Speed: -0.1, Industry: "电子",
		}
	}
	frame := BuildLiveFrame(LiveData{
		RankingKind: domain.MarketRankingLosers, RankingItems: items, RankingSelected: 19,
	}, ViewOptions{}, 79, 24)
	if !strings.Contains(frame, "> 20") || !strings.Contains(frame, "11-20/20") || strings.Contains(frame, "  01") {
		t.Fatalf("ranking viewport did not follow selection:\n%s", frame)
	}
	if rows := strings.Count(frame, "\n") + 1; rows > 24 {
		t.Fatalf("ranking frame has %d rows for a 24-row terminal:\n%s", rows, frame)
	}
}

func TestLiveFrameShowsCurrentGroupAndDetailControls(t *testing.T) {
	quote := dashboardQuote()
	list := BuildLiveFrame(LiveData{
		Quotes: []domain.Quote{quote}, GroupName: "科技", GroupCount: 1,
	}, ViewOptions{}, 99, 30)
	if !strings.Contains(list, "自选分组  科技  ·  1只") {
		t.Fatalf("group header missing:\n%s", list)
	}
	detail := BuildLiveFrame(LiveData{
		Quotes: []domain.Quote{quote}, GroupName: "科技", GroupCount: 1, Detail: true,
	}, ViewOptions{}, 99, 30)
	if !strings.Contains(detail, "↑/↓ 滚动  [/]翻页  Esc返回  q退出") {
		t.Fatalf("detail controls missing:\n%s", detail)
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
	for _, expected := range []string{"指数资金", "上证", "↑ 223.50亿", "资金", "涨停", "跌停", "1439.41", "1177.70", "> 贵州茅台", "↓ 1.16亿"} {
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

func TestLiveDetailShowsConditionalTechnicalSignalAndMarketEvidence(t *testing.T) {
	quote := dashboardQuote()
	signal := &domain.TechnicalSignal{
		Status: domain.TechnicalStatusReady, Symbol: quote.Symbol, DataSource: "腾讯", DataDate: "2026-08-07",
		Bias: "看涨", Action: "买入触发", OptionLike: "CALL-like", Strength: 82,
		Price: 1308, MA5: 1298, MA20: 1276, MA60: 1240, MACD: 4.216, RSI14: 62.4,
		VolumeRatio: 1.38, High20: 1300, Low20: 1198,
		Support: "MA20 1276.00", Resistance: "已突破既有关键位，等待新压力",
		BuyTrigger:   "回踩MA20 1276.00缩量企稳，或收盘突破20日高点 1300.00，且成交量达到20日均量的1.20倍",
		SellTrigger:  "收盘跌破20日低点 1198.00；放量时风险升级",
		Invalidation: "收盘跌破MA20 1276.00，且不能快速收回；跌破 1198.00 时看涨结构失效",
		PositionPlan: "可用10%–20%试错仓，回踩确认后再分批",
		Evidence:     []string{"收盘位于MA20上方", "MA20高于MA60", "放量突破20日高点"},
	}
	frame := BuildLiveFrame(LiveData{
		Quotes: []domain.Quote{quote}, Detail: true, Technical: signal,
		Flows:       map[string]domain.FundFlow{quote.Symbol: {Symbol: quote.Symbol, MainNet: 125000000, MainRatio: 3.25}},
		Boards:      []domain.BoardFlow{{Name: "白酒", MainNet: 3}, {Name: "食品饮料", MainNet: 2}, {Name: "消费", MainNet: -1}},
		DragonTiger: &domain.DragonTigerSnapshot{Loaded: true, WindowDays: 30},
	}, ViewOptions{}, 119, 50)
	for _, expected := range []string{
		"交易信号（日线波段）  看涨  ·  买入触发  ·  CALL-like  ·  强度 82/100",
		"趋势指标  收盘 1308.00  ·  MA5 1298.00  ·  MA20 1276.00  ·  MA60 1240.00",
		"MACD柱 +4.216  ·  RSI14 62.4  ·  量能 1.38x  ·  前20日 1198.00–1300.00",
		"买入条件", "卖出条件", "失效条件", "仓位策略", "10%–20%试错仓", "支撑 MA20 1276.00",
		"个股主力 ↑ 1.25亿 +3.25%", "关联板块多数净流入（2/3）", "近30日无龙虎榜",
		"腾讯  ·  未复权日 K（当日 K 线未收盘）", "技术观察，不是自动交易指令", "PUT-like表示看跌或减仓",
	} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("technical detail missing %q:\n%s", expected, frame)
		}
	}
	for _, line := range strings.Split(frame, "\n") {
		if width := displayWidth(line); width > 119 {
			t.Fatalf("technical detail line width %d exceeds terminal:\n%s", width, line)
		}
	}
}

func TestTechnicalSignalLoadingAndUnavailableStates(t *testing.T) {
	loading := technicalSignalLines(&domain.TechnicalSignal{Status: domain.TechnicalStatusLoading}, nil, nil, nil, false, false, 80)
	if len(loading) != 1 || !strings.Contains(loading[0], "正在加载未复权日 K") {
		t.Fatalf("unexpected loading state: %#v", loading)
	}
	unavailable := technicalSignalLines(&domain.TechnicalSignal{Status: domain.TechnicalStatusUnavailable, Error: "历史数据不足"}, nil, nil, nil, false, false, 80)
	if !strings.Contains(strings.Join(unavailable, "\n"), "历史日 K 暂不可用：历史数据不足") {
		t.Fatalf("unexpected unavailable state: %#v", unavailable)
	}
}

func TestTechnicalSignalWrappingKeepsIndicatorValuesTogether(t *testing.T) {
	lines := labeledTechnicalLines("动量量价", "MACD柱 +1.326  ·  RSI14 61.8  ·  量能 1.23x  ·  前20日 1190.19–1363.35", 60, false)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "13\n") || !strings.Contains(joined, "1190.19–1363.35") {
		t.Fatalf("numeric range was split:\n%s", joined)
	}
	for _, line := range lines {
		if displayWidth(line) > 60 {
			t.Fatalf("wrapped line exceeds width: %q", line)
		}
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
		"行业  食品饮料  +2.25%  ↑ 11.99亿  +4.62%  领涨  金达威",
		"概念  超级品牌  +1.39%   ↓ 9.49亿  -5.35%  领涨  贵州茅台",
	} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("board detail missing %q:\n%s", expected, frame)
		}
	}
	if count := strings.Count(frame, "领涨  "); count != 6 {
		t.Fatalf("expected six related boards, got %d:\n%s", count, frame)
	}
}

func TestBoardFlowLinesAlignColumnsWithAndWithoutColor(t *testing.T) {
	boards := []domain.BoardFlow{
		{Code: "BK0438", Name: "食品饮料", Kind: domain.BoardKindIndustry, Percent: 2.25, MainNet: 1199132256, MainRatio: 4.62, LeaderName: "金达威", LeaderPercent: 10.03},
		{Code: "BK1277", Name: "白酒Ⅱ", Kind: domain.BoardKindIndustry, Percent: -0.72, MainNet: -854524336, MainRatio: -7.31, LeaderName: "迎驾贡酒", LeaderPercent: 6.15},
		{Code: "BK1653", Name: "味蕾经济", Kind: domain.BoardKindConcept, Percent: 12.32, MainNet: 69653264, MainRatio: 0.53, LeaderName: "一鸣食品", LeaderPercent: -9.98},
	}
	for _, color := range []bool{false, true} {
		lines := boardFlowLines(boards, color, 96)
		var expected []int
		for index, line := range lines {
			columns := []struct {
				token      string
				rightAlign bool
			}{
				{token: boards[index].Name},
				{token: signedPercent(boards[index].Percent), rightAlign: true},
				{token: directionalFundFlow(&domain.FundFlow{MainNet: boards[index].MainNet}), rightAlign: true},
				{token: fundFlowRatio(&domain.FundFlow{MainRatio: boards[index].MainRatio}), rightAlign: true},
				{token: "领涨"},
				{token: boards[index].LeaderName},
				{token: signedPercent(boards[index].LeaderPercent), rightAlign: true},
			}
			positions := make([]int, 0, len(columns))
			for _, column := range columns {
				position := strings.Index(line, column.token)
				if position < 0 {
					t.Fatalf("missing token %q in board line %q", column.token, line)
				}
				visiblePosition := displayWidth(line[:position])
				if column.rightAlign {
					visiblePosition += displayWidth(column.token)
				}
				positions = append(positions, visiblePosition)
			}
			if index == 0 {
				expected = positions
				continue
			}
			for column := range positions {
				if positions[column] != expected[column] {
					t.Fatalf("color=%v row=%d column=%d boundary is %d, want %d:\n%s", color, index, column, positions[column], expected[column], line)
				}
			}
		}
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

func TestBoardHeatLabelsUseRanksBreadthAndFlow(t *testing.T) {
	tests := []struct {
		name  string
		board domain.BoardFlow
		want  string
	}{
		{
			name: "hot",
			board: domain.BoardFlow{Percent: 3.2, MainNet: 5e8, Turnover: 4.2, RiseCount: 80, FallCount: 20,
				ChangeRank: 8, FlowRank: 12, TurnoverRank: 15, UniverseSize: 100},
			want: "热门",
		},
		{
			name: "hot divergence",
			board: domain.BoardFlow{Percent: 3.2, MainNet: -2e8, Turnover: 7.1, RiseCount: 75, FallCount: 25,
				ChangeRank: 5, FlowRank: 82, TurnoverRank: 8, UniverseSize: 100},
			want: "热门分歧",
		},
		{
			name: "active",
			board: domain.BoardFlow{Percent: 1.1, MainNet: 8e7, Turnover: 1.8, RiseCount: 60, FallCount: 40,
				ChangeRank: 25, FlowRank: 28, TurnoverRank: 45, UniverseSize: 100},
			want: "活跃",
		},
		{
			name: "cold",
			board: domain.BoardFlow{Percent: -2.1, MainNet: -5e8, Turnover: 0.5, RiseCount: 20, FallCount: 80,
				ChangeRank: 85, FlowRank: 90, TurnoverRank: 80, UniverseSize: 100},
			want: "偏冷",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := boardHeatLabel(test.board); got != test.want {
				t.Fatalf("got %q want %q", got, test.want)
			}
		})
	}
	line := boardHeatLine(tests[0].board, false)
	for _, expected := range []string{"热度 热门", "涨幅 8/100", "资金 12/100", "换手 15/100(4.20%)", "涨80/跌20"} {
		if !strings.Contains(line, expected) {
			t.Fatalf("heat line missing %q: %s", expected, line)
		}
	}
}

func TestLiveDetailShowsDragonTigerReasonsAndData(t *testing.T) {
	quote := dashboardQuote()
	snapshot := &domain.DragonTigerSnapshot{
		Loaded: true, WindowDays: 30,
		Entries: []domain.DragonTigerEntry{
			{
				Symbol: "sz000603", TradeDate: "2026-08-07", Reason: "连续三个交易日内，涨幅偏离值累计达到20%的证券",
				SeatSummary: "4家机构买入，成功率40.14%", ChangePercent: 10, NetAmount: 113039557.06,
				BuyAmount: 1309617360.05, SellAmount: 1196577802.99, NetRatio: 1.2746, DealAmountRatio: 28.259,
				Turnover: 16.9158, Next1Percent: math.NaN(), Next5Percent: math.NaN(), Next10Percent: math.NaN(),
			},
			{
				Symbol: "sz000603", TradeDate: "2026-08-07", Reason: "日涨幅偏离值达到7%的前5只证券",
				SeatSummary: "3家机构买入，成功率15.83%", ChangePercent: 10, NetAmount: -28864029.08,
				BuyAmount: 550792447.15, SellAmount: 579656476.23, NetRatio: -0.7982, DealAmountRatio: 31.2624,
				Turnover: 16.9158, Next1Percent: -0.12, Next5Percent: -3.06, Next10Percent: 26.06,
			},
		},
	}
	frame := BuildLiveFrame(LiveData{
		Quotes: []domain.Quote{quote}, DragonTiger: snapshot, Detail: true, MarketStatus: "交易中",
	}, ViewOptions{}, 119, 40)
	for _, expected := range []string{
		"龙虎榜  近30日上榜1日 / 2条  ·  最近 08-07",
		"净买入 ↑ 1.13亿 +1.27%",
		"买入 13.10亿  卖出 11.97亿",
		"原因  连续三个交易日内，涨幅偏离值累计达到20%的证券",
		"榜单成交占比 28.26%  ·  换手 16.92%  ·  席位标签 4家机构买入，成功率40.14%",
		"上榜后  1日 -0.12%  ·  5日 -3.06%  ·  10日 +26.06%",
	} {
		if !strings.Contains(frame, expected) {
			t.Fatalf("dragon-tiger detail missing %q:\n%s", expected, frame)
		}
	}
}

func TestDragonTigerLoadedEmptyShowsNoRecentRecord(t *testing.T) {
	lines := dragonTigerLines(&domain.DragonTigerSnapshot{Loaded: true, WindowDays: 30}, false)
	if len(lines) != 1 || lines[0] != "龙虎榜  近30日无上榜记录" {
		t.Fatalf("unexpected empty snapshot: %#v", lines)
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
		{Symbol: "sh000001", Amount: 120954357, QuoteTime: "2026-08-07 15:00:00"},
		{Symbol: "sz399001", Amount: 145487618},
		{Symbol: "bj899050", Amount: 1000000},
	}
	previous := domain.MarketAmountSnapshot{
		TradeDate: "2026-08-06",
		Shanghai:  110000000,
		Shenzhen:  140000000,
		Beijing:   1000000,
	}
	line := marketAmountOverview(indices, previous, false, 79)
	for _, expected := range []string{"沪深成交额", "2.67万亿", "↑", "1644.20亿元", "+6.55%"} {
		if !strings.Contains(line, expected) {
			t.Fatalf("market amount line missing %q: %s", expected, line)
		}
	}
}

func TestMarketAmountOverviewShowsShanghaiShenzhenAndBeijingTotals(t *testing.T) {
	indices := []domain.Quote{
		{Symbol: "sh000001", Amount: 116689325.2608, QuoteTime: "2026-08-10 15:00:00"},
		{Symbol: "sz399001", Amount: 135620953.5368},
		{Symbol: "sz399106", Amount: 135620953.5368},
		{Symbol: "bj899050", Amount: 1576946.9231, QuoteTime: "2026-08-10 15:00:00"},
	}
	previous := domain.MarketAmountSnapshot{
		TradeDate: "2026-08-07",
		Shanghai:  120954355.5712,
		Shenzhen:  145487613.1328,
		Beijing:   1915024.7936,
	}
	line := marketAmountOverview(indices, previous, false, 120)
	for _, expected := range []string{"沪深成交额", "2.54万亿", "1446.98亿元"} {
		if !strings.Contains(line, expected) {
			t.Fatalf("market amount overview missing %q: %s", expected, line)
		}
	}
	for _, unexpected := range []string{"沪深京A股成交额", "2.52万亿", "1413.17亿元", "\n"} {
		if strings.Contains(line, unexpected) {
			t.Fatalf("market amount overview should not contain %q: %s", unexpected, line)
		}
	}
}

func TestMarketAmountOverviewHidesChangeWhenQuoteDateHasNotAdvanced(t *testing.T) {
	indices := []domain.Quote{
		{Symbol: "sh000001", Amount: 100000000, QuoteTime: "2026-08-07 15:00:00"},
		{Symbol: "sz399106", Amount: 100000000},
		{Symbol: "bj899050", Amount: 1000000},
	}
	previous := domain.MarketAmountSnapshot{TradeDate: "2026-08-07", Shanghai: 90000000, Shenzhen: 90000000, Beijing: 900000}
	line := marketAmountOverview(indices, previous, false, 120)
	if strings.Contains(line, "较昨") {
		t.Fatalf("should hide comparison for an unchanged pre-open quote: %s", line)
	}
}

func TestMarketSummaryRowsAlignIndexColumns(t *testing.T) {
	indices := []domain.Quote{
		{Symbol: "sh000001", Current: "3966.59", Percent: 0.67},
		{Symbol: "sz399001", Current: "14316.96", Percent: 0.04},
		{Symbol: "sz399006", Current: "3537.21", Percent: -0.73},
	}
	flows := map[string]domain.FundFlow{
		"sh000001": {Symbol: "sh000001", MainNet: -13649000000},
		"sz399001": {Symbol: "sz399001", MainNet: -27818000000},
		"sz399006": {Symbol: "sz399006", MainNet: -15342000000},
	}
	marketLine := marketOverview(indices, false, false, 120)
	flowLine := marketFlowOverview(flows, false, false, 120)
	for _, label := range []string{"上证", "深证", "创业板"} {
		marketIndex := strings.Index(marketLine, label)
		flowIndex := strings.Index(flowLine, label)
		if marketIndex < 0 || flowIndex < 0 {
			t.Fatalf("missing %s in market summaries:\n%s\n%s", label, marketLine, flowLine)
		}
		marketPosition := displayWidth(marketLine[:marketIndex])
		flowPosition := displayWidth(flowLine[:flowIndex])
		if marketPosition != flowPosition {
			t.Fatalf("%s is not aligned: market=%d flow=%d\n%s\n%s", label, marketPosition, flowPosition, marketLine, flowLine)
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
