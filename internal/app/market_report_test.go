package app

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func risingBars(symbol string) []domain.DailyBar {
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.Local)
	bars := make([]domain.DailyBar, 61)
	for index := range bars {
		closePrice := 10 + float64(index)*0.1
		bars[index] = domain.DailyBar{
			Symbol: symbol, Date: start.AddDate(0, 0, index).Format("2006-01-02"),
			Open: closePrice - 0.05, Close: closePrice, High: closePrice + 0.1, Low: closePrice - 0.1,
			Volume: 1000 + float64(index)*10, Amount: 1e8,
		}
	}
	return bars
}

func TestScanTechnicalUsesPriceAndVolumeHistory(t *testing.T) {
	result, ok := scanTechnical(risingBars("sh600001"))
	if !ok {
		t.Fatal("expected technical snapshot")
	}
	if result.Close <= result.MA5 || result.MA5 <= result.MA20 || result.MA20 <= result.MA60 || result.Return20 <= 0 || result.VolumeRatio20 <= 1 || result.Trend != "多头排列" {
		t.Fatalf("unexpected technical snapshot: %#v", result)
	}
}

func TestSelectHotBoardsRequiresFlowAndBreadthAndDeduplicatesLevels(t *testing.T) {
	items := []domain.BoardFlow{
		{Code: "BK1", Name: "油气开采Ⅱ", Percent: 3, MainNet: 4e8, Turnover: 3, RiseCount: 8, FallCount: 1},
		{Code: "BK2", Name: "油气开采Ⅲ", Percent: 3, MainNet: 4e8, Turnover: 3, RiseCount: 8, FallCount: 1},
		{Code: "BK3", Name: "伪热点", Percent: 5, MainNet: -2e8, Turnover: 5, RiseCount: 2, FallCount: 8},
	}
	selected := selectHotBoards(items)
	if len(selected) != 1 || !strings.HasPrefix(selected[0].Name, "油气开采") {
		t.Fatalf("unexpected hot boards: %#v", selected)
	}
}

func TestSelectMarketCandidatesKeepsHotBoardLeaders(t *testing.T) {
	candidates := make([]domain.MarketCandidateAssessment, 0, 15)
	for index := 0; index < 5; index++ {
		candidates = append(candidates, domain.MarketCandidateAssessment{
			Stock: domain.MarketStockSnapshot{
				Symbol: fmt.Sprintf("sh60000%d", index), Industry: fmt.Sprintf("龙头行业%d", index),
			},
			BoardLeader: true, Score: float64(index), Grade: "A", Category: "板块龙头/核心承接",
		})
	}
	for index := 0; index < 10; index++ {
		candidates = append(candidates, domain.MarketCandidateAssessment{
			Stock: domain.MarketStockSnapshot{
				Symbol: fmt.Sprintf("sz0001%02d", index), Industry: fmt.Sprintf("普通行业%d", index),
			},
			Score: 100 - float64(index), Grade: "B", Category: "高波动情绪候选",
		})
	}
	selected := selectMarketCandidates(candidates, 10)
	if len(selected) != 10 {
		t.Fatalf("unexpected candidate count: %d", len(selected))
	}
	leaders := 0
	for _, candidate := range selected {
		if candidate.BoardLeader {
			leaders++
		}
	}
	if leaders != 5 {
		t.Fatalf("expected all five leaders, got %d: %#v", leaders, selected)
	}
}

func TestCandidateClassificationAndLimitUpPenalty(t *testing.T) {
	stock := domain.MarketStockSnapshot{
		Symbol: "sh600001", Name: "测试股份", Industry: "测试", Price: 10, Percent: 9.9,
		Amount: 10e8, MainNet: 2e8, MainRatio: 8,
	}
	nearLimit := preliminaryStockScore(stock, false, 0, 0, false)
	stock.Percent = 9.7
	belowLimit := preliminaryStockScore(stock, false, 0, 0, false)
	if nearLimit >= belowLimit {
		t.Fatalf("near-limit-up candidate should receive a stronger chase penalty: %.2f >= %.2f", nearLimit, belowLimit)
	}
	candidate := domain.MarketCandidateAssessment{
		Stock: stock,
		Technical: domain.MarketTechnicalSnapshot{
			DataDate: "2026-08-11", Close: 9, MA20: 10, MA60: 11, Return60: -15,
		},
	}
	grade, category := classifyMarketCandidate(candidate)
	if grade != "C" || category != "超跌反转观察" {
		t.Fatalf("unexpected classification: %s %s", grade, category)
	}
}

func TestPreliminaryStockScoreRelaxesMissingFlowOutsideContinuousTrading(t *testing.T) {
	stock := domain.MarketStockSnapshot{
		Symbol: "sh600001", Name: "测试股份", Price: 10, Percent: -0.5, Amount: 8e7,
		MainNet: math.NaN(), MainRatio: math.NaN(),
	}
	if score := preliminaryStockScore(stock, false, 5, 0, false); !math.IsInf(score, -1) {
		t.Fatalf("strict intraday score should reject missing flow: %.2f", score)
	}
	if score := preliminaryStockScore(stock, false, 5, 0, true); math.IsInf(score, -1) {
		t.Fatal("relaxed non-trading score should keep a liquid technical candidate")
	}
}

func TestSelectMomentumBoardsWorksWhenMainFlowIsUnavailable(t *testing.T) {
	items := []domain.BoardFlow{
		{Code: "BK1", Name: "活跃板块", Percent: 2.5, MainNet: math.NaN(), RiseCount: 20, FallCount: 4},
		{Code: "BK2", Name: "无扩散", Percent: 4, MainNet: math.NaN(), RiseCount: 2, FallCount: 8},
	}
	selected := selectMomentumBoards(items)
	if len(selected) != 1 || selected[0].Name != "活跃板块" || selected[0].FlowAvailable {
		t.Fatalf("unexpected momentum boards: %#v", selected)
	}
}

type marketReportScanMock struct{}

func (marketReportScanMock) FetchIndustryRanking(_ context.Context, _ domain.MarketScanMetric, descending bool, _ int) ([]domain.BoardFlow, error) {
	if !descending {
		return []domain.BoardFlow{{Code: "BK2", Name: "弱板块", Percent: -3, MainNet: -5e8, RiseCount: 1, FallCount: 9}}, nil
	}
	return []domain.BoardFlow{{
		Code: "BK1", Name: "测试板块", Percent: 3, MainNet: 5e8, MainRatio: 6, Turnover: 4,
		RiseCount: 9, FallCount: 1, LeaderName: "测试股份", LeaderCode: "600001", LeaderPercent: 6,
	}}, nil
}

func testMarketStock() domain.MarketStockSnapshot {
	return domain.MarketStockSnapshot{
		Symbol: "sh600001", Name: "测试股份", Industry: "测试板块", Price: 16, Percent: 6,
		Amount: 20e8, Turnover: 5, VolumeRatio: 1.5, MainNet: 2e8, MainRatio: 10,
	}
}

func (marketReportScanMock) FetchStockRanking(context.Context, domain.MarketScanMetric, bool, int) ([]domain.MarketStockSnapshot, error) {
	return []domain.MarketStockSnapshot{testMarketStock()}, nil
}

func (marketReportScanMock) FetchStocks(context.Context, []string) ([]domain.MarketStockSnapshot, error) {
	return []domain.MarketStockSnapshot{testMarketStock()}, nil
}

func (marketReportScanMock) FetchAnnouncements(context.Context, []string, int) ([]domain.MarketAnnouncement, error) {
	return []domain.MarketAnnouncement{{
		Symbol: "sh600001", Name: "测试股份", Date: "2026-08-11", Title: "测试股份:关于回购股份的公告",
	}}, nil
}

type marketReportQuoteMock struct{}

func (marketReportQuoteMock) Fetch(_ context.Context, _ []string) ([]domain.Quote, error) {
	return []domain.Quote{
		{Symbol: "sh000001", Name: "上证指数", Percent: -0.5, Amount: 100e6},
		{Symbol: "sz399001", Name: "深证成指", Percent: -0.2},
		{Symbol: "sz399006", Name: "创业板指", Percent: 0.1},
		{Symbol: "sz399106", Amount: 120e6}, {Symbol: "bj899050", Amount: 1e6},
	}, nil
}

type marketReportFlowMock struct{}

func (marketReportFlowMock) Fetch(_ context.Context, symbols []string) (map[string]domain.FundFlow, error) {
	result := make(map[string]domain.FundFlow, len(symbols))
	for _, symbol := range symbols {
		result[symbol] = domain.FundFlow{Symbol: symbol, MainNet: -1e8}
	}
	return result, nil
}

type marketReportAmountMock struct{}

func (marketReportAmountMock) FetchPreviousMarketAmount(context.Context) (domain.MarketAmountSnapshot, error) {
	return domain.MarketAmountSnapshot{Shanghai: 110e6, Shenzhen: 130e6, Beijing: 1e6}, nil
}

type marketReportHistoryMock struct{}

func (marketReportHistoryMock) FetchDailyBars(_ context.Context, symbol string) ([]domain.DailyBar, error) {
	return risingBars(symbol), nil
}

type marketReportAIMock struct{}

func (marketReportAIMock) Synthesize(_ context.Context, prompt string) (string, error) {
	if !strings.Contains(prompt, "结构化事实JSON") || !strings.Contains(prompt, "测试股份") {
		return "", fmt.Errorf("prompt missing structured facts")
	}
	return "# Codex市场报告\n", nil
}

func TestGenerateMarketReportBuildsScoresRunsAIAndPersists(t *testing.T) {
	app := &App{
		marketScan: marketReportScanMock{}, quotes: marketReportQuoteMock{}, flows: marketReportFlowMock{},
		amounts: marketReportAmountMock{}, scanHistory: marketReportHistoryMock{}, marketReportAI: marketReportAIMock{},
		marketReports: storage.NewMarketReportStore(t.TempDir()),
	}
	var progress []string
	report, err := app.generateMarketReport(context.Background(), func(value string) { progress = append(progress, value) })
	if err != nil {
		t.Fatal(err)
	}
	if !report.AIUsed || !strings.Contains(report.Markdown, "Codex市场报告") || len(report.Facts.Candidates) != 1 {
		t.Fatalf("unexpected generated report: %#v", report)
	}
	candidate := report.Facts.Candidates[0]
	if !candidate.BoardLeader || candidate.Technical.Trend != "多头排列" || len(candidate.Announcements) != 1 || report.MarkdownPath == "" {
		t.Fatalf("unexpected candidate/report artifact: %#v", candidate)
	}
	if len(progress) < 4 || !strings.Contains(strings.Join(progress, "|"), "Codex") {
		t.Fatalf("missing progress stages: %#v", progress)
	}
}

func TestWatchMarketReportStatusPersistsUntilOpened(t *testing.T) {
	state := watchMarketReport{}
	state.begin()
	if !strings.Contains(state.status(false), "生成中") {
		t.Fatalf("missing generating status: %q", state.status(false))
	}
	state.complete(domain.GeneratedMarketReport{AIUsed: true, Markdown: "# report"})
	if !strings.Contains(state.status(false), "按 r 查看") {
		t.Fatalf("missing ready status: %q", state.status(false))
	}
	state.open(state.report)
	if state.status(false) != "" || !state.viewing {
		t.Fatalf("opening report should clear unread status: %#v", state)
	}
}
