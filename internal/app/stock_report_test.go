package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func stockReportBars(symbol string) []domain.DailyBar {
	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.Local)
	bars := make([]domain.DailyBar, 90)
	for index := range bars {
		price := 100 + float64(index)*0.5
		bars[index] = domain.DailyBar{
			Symbol: symbol, Source: "测试未复权", Date: start.AddDate(0, 0, index).Format("2006-01-02"),
			Open: price - 0.2, Close: price, High: price + 0.3, Low: price - 0.3,
			Volume: 1000, Amount: 1e8,
		}
	}
	bars[len(bars)-1].Volume = 1500
	return bars
}

type stockReportQuoteMock struct{}

func (stockReportQuoteMock) Fetch(context.Context, []string) ([]domain.Quote, error) {
	return []domain.Quote{
		{Symbol: "sh600519", Code: "600519", Name: "贵州茅台", Current: "144.50", PreviousClose: "142.00", Open: "143.00", High: "145.00", Low: "141.80", AveragePrice: "143.50", Percent: 1.76, Amount: 20e6, VolumeRatio: "1.50", Turnover: "1.20", PETTM: "20", PB: "7", QuoteTime: "2026-08-11 15:00:00"},
		{Symbol: "sh000001", Name: "上证指数", Current: "3900", Percent: 0.5},
		{Symbol: "sz399001", Name: "深证成指", Current: "14000", Percent: 0.3},
		{Symbol: "sz399006", Name: "创业板指", Current: "3500", Percent: -0.2},
	}, nil
}

type stockReportFlowMock struct{}

func (stockReportFlowMock) Fetch(context.Context, []string) (map[string]domain.FundFlow, error) {
	return map[string]domain.FundFlow{
		"sh600519": {Symbol: "sh600519", Name: "贵州茅台", Industry: "白酒", MainNet: 3e8, MainRatio: 8, Price: 144.5, Percent: 1.76},
		"sh000001": {Symbol: "sh000001", MainNet: 10e9},
	}, nil
}

type stockReportHistoryMock struct{}

func (stockReportHistoryMock) FetchDailyBars(context.Context, string) ([]domain.DailyBar, error) {
	return stockReportBars("sh600519"), nil
}

type stockReportBoardMock struct{}

func (stockReportBoardMock) FetchBoards(context.Context, string) ([]domain.BoardFlow, error) {
	return []domain.BoardFlow{{
		Code: "BK1", Name: "白酒", Kind: domain.BoardKindIndustry, Percent: 2, MainNet: 5e9,
		RiseCount: 18, FallCount: 3, ChangeRank: 3, UniverseSize: 100, LeaderName: "贵州茅台", LeaderCode: "600519",
	}}, nil
}

type stockReportDragonMock struct{}

func (stockReportDragonMock) FetchDragonTiger(context.Context, string) (domain.DragonTigerSnapshot, error) {
	return domain.DragonTigerSnapshot{Loaded: true, WindowDays: 30, Entries: []domain.DragonTigerEntry{{
		Symbol: "sh600519", TradeDate: "2026-08-08", Reason: "测试异动", NetAmount: 1e8,
	}}}, nil
}

type stockReportEmptyDragonMock struct{}

func (stockReportEmptyDragonMock) FetchDragonTiger(context.Context, string) (domain.DragonTigerSnapshot, error) {
	return domain.DragonTigerSnapshot{}, fmt.Errorf("未解析到龙虎榜数据")
}

type stockReportScanMock struct{ marketReportScanMock }

func (stockReportScanMock) FetchAnnouncements(context.Context, []string, int) ([]domain.MarketAnnouncement, error) {
	return []domain.MarketAnnouncement{{
		Symbol: "sh600519", Name: "贵州茅台", Date: "2026-08-10", Title: "贵州茅台关于回购股份的公告",
	}}, nil
}

type stockReportNewsMock struct{}

func (stockReportNewsMock) FetchStockNews(context.Context, string, int) ([]domain.StockNewsItem, error) {
	return []domain.StockNewsItem{{Date: "2026-08-11 10:00:00", Title: "贵州茅台市场关注度上升", Source: "测试媒体"}}, nil
}

type stockReportAIMock struct{}

func (stockReportAIMock) Synthesize(_ context.Context, prompt string) (string, error) {
	for _, expected := range []string{"结构化事实JSON", "technical", "fund_movement", "贵州茅台", "不得创造新价位"} {
		if !strings.Contains(prompt, expected) {
			return "", fmt.Errorf("prompt missing %s", expected)
		}
	}
	return "# 一句话结论\n\n短线偏多，但只按条件观察。\n", nil
}

func TestGenerateStockReportCombinesDimensionsRunsAIAndPersists(t *testing.T) {
	app := &App{
		quotes: stockReportQuoteMock{}, flows: stockReportFlowMock{}, history: stockReportHistoryMock{},
		boards: stockReportBoardMock{}, dragonTiger: stockReportDragonMock{}, marketScan: stockReportScanMock{},
		news: stockReportNewsMock{}, marketReportAI: stockReportAIMock{}, stockReports: storage.NewStockReportStore(t.TempDir()),
	}
	movement := domain.FundMovement{Symbol: "sh600519", State: "持续流入", Delta1Minute: 1e8, Delta3Minutes: 2e8, Delta5Minutes: 3e8}
	var progress []string
	report, err := app.generateStockReport(context.Background(), "sh600519", &movement, func(value string) { progress = append(progress, value) })
	if err != nil {
		t.Fatal(err)
	}
	if !report.AIUsed || report.Symbol != "sh600519" || report.Name != "贵州茅台" || report.MarkdownPath == "" {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.Facts.Technical.Status != domain.TechnicalStatusReady || report.Facts.FundMovement == nil || len(report.Facts.Boards) != 1 || len(report.Facts.News) != 1 || len(report.Facts.Announcements) != 1 {
		t.Fatalf("missing report dimensions: %#v", report.Facts)
	}
	if !strings.Contains(strings.Join(progress, "|"), "Codex") {
		t.Fatalf("missing AI progress: %#v", progress)
	}
}

func TestStockReportDoesNotTreatMissingDragonTigerAsMissingDailyHistory(t *testing.T) {
	app := &App{
		quotes: stockReportQuoteMock{}, flows: stockReportFlowMock{}, history: stockReportHistoryMock{},
		boards: stockReportBoardMock{}, dragonTiger: stockReportEmptyDragonMock{}, marketScan: stockReportScanMock{},
		news: stockReportNewsMock{}, marketReportAI: nil, stockReports: storage.NewStockReportStore(t.TempDir()),
	}
	report, err := app.generateStockReport(context.Background(), "sh600519", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Facts.Technical.Status != domain.TechnicalStatusReady || !strings.Contains(strings.Join(report.Facts.Warnings, "|"), "dragon_tiger") {
		t.Fatalf("missing optional warning or technical facts: %#v", report.Facts)
	}
}

func TestDeterministicStockReportUsesTechnicalLevelsAndRiskReasons(t *testing.T) {
	facts := domain.StockReportFacts{
		GeneratedAt: time.Date(2026, 8, 11, 15, 0, 0, 0, time.Local),
		Quote:       domain.StockQuoteSnapshot{Symbol: "sh600519", Name: "贵州茅台", Price: 144.5, Percent: 1.2},
		Technical: domain.TechnicalSignal{
			Bias: "看涨", Action: "买入观察", Score: 4, MA5: 142, MA20: 138, MA60: 130, RSI14: 80,
			VolumeRatio: 1.5, Support: "MA20 138.00", Resistance: "20日结构高点 146.00",
			BuyTrigger: "回踩MA20 138.00缩量企稳", SellTrigger: "收盘跌破20日低点 125.00",
			Invalidation: "收盘跌破MA20 138.00", PositionPlan: "10%-20%试错仓", Evidence: []string{"MA20向上"},
		},
		Fund: domain.FundFlow{MainNet: -1e8, MainRatio: -2},
	}
	markdown := renderDeterministicStockReport(facts, "offline")
	for _, expected := range []string{"买入观察点", "138.00", "卖出/减仓观察点", "125.00", "RSI14", "主力资金为净流出", "不触发自动交易"} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("fallback missing %q:\n%s", expected, markdown)
		}
	}
}

func TestWatchStockReportStatusNamesTargetAndOpenKey(t *testing.T) {
	state := watchStockReport{}
	state.begin("sh600519", "贵州茅台")
	if status := state.status(false); !strings.Contains(status, "600519 贵州茅台") || !strings.Contains(status, "生成中") {
		t.Fatalf("unexpected generating status: %q", status)
	}
	state.complete(domain.GeneratedStockReport{Symbol: "sh600519", Name: "贵州茅台", AIUsed: true, Markdown: "# report"})
	if status := state.status(false); !strings.Contains(status, "按 o 查看") {
		t.Fatalf("unexpected ready status: %q", status)
	}
}
