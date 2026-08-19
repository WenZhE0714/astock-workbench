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
		{Symbol: "sh600519", Code: "600519", Name: "贵州茅台", Current: "144.50", PreviousClose: "142.00", Open: "143.00", High: "145.00", Low: "141.80", AveragePrice: "143.50", Percent: 1.76, Amount: 20e6, VolumeRatio: "1.50", Turnover: "1.20", PETTM: "20", PB: "7", LimitUp: "156.20", LimitDown: "127.80", QuoteTime: "2026-08-11 15:00:00"},
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

type stockReportResearchMock struct{}

func (stockReportResearchMock) FetchBrokerResearch(context.Context, string, time.Time, time.Time, int) ([]domain.BrokerResearchItem, error) {
	return []domain.BrokerResearchItem{{
		Symbol: "sh600519", Name: "贵州茅台", Title: "需求根基稳固", Organization: "测试证券",
		Author: "分析师甲", PublishedAt: "2026-08-10", SourceID: "R1", Rating: "增持",
	}}, nil
}

type stockReportAIMock struct{}

func (stockReportAIMock) Synthesize(_ context.Context, prompt string) (string, error) {
	for _, expected := range []string{"结构化事实JSON", "technical", "fund_movement", "price_boundary", "贵州茅台", "不得创造新价位"} {
		if !strings.Contains(prompt, expected) {
			return "", fmt.Errorf("prompt missing %s", expected)
		}
	}
	return "# 一句话结论\n\n短线偏多，但只按条件观察。公告线索待核 [E01]。\n", nil
}

func TestGenerateStockReportCombinesDimensionsRunsAIAndPersists(t *testing.T) {
	app := &App{
		quotes: stockReportQuoteMock{}, flows: stockReportFlowMock{}, history: stockReportHistoryMock{},
		boards: stockReportBoardMock{}, dragonTiger: stockReportDragonMock{}, marketScan: stockReportScanMock{},
		news: stockReportNewsMock{}, research: stockReportResearchMock{}, marketReportAI: stockReportAIMock{}, stockReports: storage.NewStockReportStore(t.TempDir()),
	}
	movement := domain.FundMovement{Symbol: "sh600519", State: "持续流入", Delta1Minute: 1e8, Delta3Minutes: 2e8, Delta5Minutes: 3e8}
	var progress []string
	report, err := app.generateStockReport(context.Background(), "sh600519", &movement, func(value string) { progress = append(progress, value) })
	if err != nil {
		t.Fatal(err)
	}
	if !report.AIUsed || report.Symbol != "sh600519" || report.Name != "贵州茅台" || report.MarkdownPath == "" || report.EvidencePath == "" || !strings.Contains(report.Markdown, "## 信息凭证") {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.Facts.Technical.Status != domain.TechnicalStatusReady || report.Facts.FundMovement == nil || len(report.Facts.Boards) != 1 || len(report.Facts.News) != 1 || len(report.Facts.Announcements) != 1 || len(report.Facts.Evidence.Items) != 3 {
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

func TestCollectStockFactsExcludesStaleFundMovement(t *testing.T) {
	app := &App{
		quotes: stockReportQuoteMock{}, flows: stockReportFlowMock{}, history: stockReportHistoryMock{},
		boards: stockReportBoardMock{}, dragonTiger: stockReportDragonMock{}, marketScan: stockReportScanMock{}, news: stockReportNewsMock{},
	}
	movement := domain.FundMovement{Symbol: "sh600519", SampledAt: time.Now().Add(-5 * time.Minute), Delta1Minute: 1e8}
	facts, err := app.collectStockReportFacts(context.Background(), "sh600519", &movement, nil)
	if err != nil {
		t.Fatal(err)
	}
	if facts.FundMovement != nil || !strings.Contains(strings.Join(facts.Warnings, "|"), "已排除过期增量") {
		t.Fatalf("stale fund movement was treated as current: %#v", facts)
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

func TestStockReportQualifiesTechnicalLevelsOutsideDailyPriceBoundary(t *testing.T) {
	facts := domain.StockReportFacts{
		GeneratedAt: time.Date(2026, 8, 12, 14, 30, 0, 0, time.Local),
		Quote:       domain.StockQuoteSnapshot{Symbol: "sh600176", Name: "中国巨石", Price: 42.69, Percent: 10},
		PriceBoundary: domain.StockPriceBoundary{
			TradeDate: "2026-08-12", LimitUp: 46.96, LimitDown: 38.42, Available: true,
		},
		Technical: domain.TechnicalSignal{
			Bias: "看涨", Action: "持有观察", MA5: 40.19, MA20: 39.5, MA60: 33.72,
			High20: 56, Low20: 33.72, Resistance: "20日结构高点 56.00",
			BuyTrigger:   "收盘突破20日高点 56.00，且成交量达到20日均量的1.20倍",
			SellTrigger:  "收盘跌破20日低点 33.72；放量时风险升级",
			Invalidation: "收盘跌破MA20 39.50，且不能快速收回；跌破 33.72 时看涨结构失效",
			PositionPlan: "保持观察", Evidence: []string{"MA20向上"},
		},
	}
	markdown := renderDeterministicStockReport(facts, "")
	for _, expected := range []string{
		"跌停 38.42 元，涨停 46.96 元", "压力20日结构高点 56.00（跨交易日结构位",
		"收盘突破20日高点 56.00", "超出当日涨跌停区间，不可作为今日触发",
	} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("boundary-aware fallback missing %q:\n%s", expected, markdown)
		}
	}
}

func TestStockReportPromptPrequalifiesChineseJushiLevels(t *testing.T) {
	facts := domain.StockReportFacts{
		PriceBoundary: domain.StockPriceBoundary{TradeDate: "2026-08-13", LimitUp: 47.64, LimitDown: 38.98, Available: true},
		Technical: domain.TechnicalSignal{
			High20: 51.75, Low20: 33.72,
			Resistance:   "20日结构高点 51.75",
			BuyTrigger:   "收盘突破20日高点 51.75，且成交量达到20日均量的1.20倍",
			Invalidation: "突破 51.75 转强 / 跌破 33.72 转弱",
		},
	}
	prompt, err := stockReportPrompt(facts)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"51.75", "33.72", "跨交易日结构位", "不可作为今日触发"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prequalified prompt missing %q:\n%s", expected, prompt)
		}
	}
}

type boundaryRetrySynthesizer struct {
	calls     int
	prompts   []string
	deadlines []time.Time
}

func (mock *boundaryRetrySynthesizer) Synthesize(ctx context.Context, prompt string) (string, error) {
	mock.calls++
	mock.prompts = append(mock.prompts, prompt)
	deadline, ok := ctx.Deadline()
	if ok {
		mock.deadlines = append(mock.deadlines, deadline)
	}
	if mock.calls == 1 {
		return "- 强势确认：收盘突破56元。", nil
	}
	return "- 当日涨停价46.96元；56元属于跨交易日结构位，不可作为今日收盘确认条件。", nil
}

func TestPriceBoundaryValidatorRetriesChineseJushiLikeImpossibleTrigger(t *testing.T) {
	t.Setenv("ASTOCK_CODEX_TIMEOUT_SECONDS", "600")
	facts := domain.StockReportFacts{
		PriceBoundary: domain.StockPriceBoundary{TradeDate: "2026-08-12", LimitUp: 46.96, LimitDown: 38.42, Available: true},
		Technical:     domain.TechnicalSignal{High20: 56},
	}
	if err := validatePriceBoundaryText(facts, "强势确认：收盘突破56元，且成交量达到20日均量1.20倍。"); err == nil {
		t.Fatal("expected impossible same-day trigger to fail validation")
	}
	if err := validatePriceBoundaryText(facts, "我的持仓成本是33元，当前先观察。 "); err != nil {
		t.Fatalf("historical holding cost should not be treated as a same-day trigger: %v", err)
	}
	if err := validatePriceBoundaryText(facts, "我的持仓成本是33元，跌破40元时减仓。 "); err != nil {
		t.Fatalf("holding cost should remain distinct from an in-range sell trigger: %v", err)
	}
	if err := validatePriceBoundaryText(facts, "56元是跨交易日结构位，不可作为今日收盘确认条件。"); err != nil {
		t.Fatalf("qualified cross-session level should pass validation: %v", err)
	}
	if err := validatePriceBoundaryText(facts, "失效条件为突破56元转强、跌破33.72元转弱，两者均按跨交易日结构确认处理。"); err != nil {
		t.Fatalf("sentence-level cross-session qualifier should cover both levels: %v", err)
	}
	mock := &boundaryRetrySynthesizer{}
	app := &App{marketReportAI: mock}
	answer, err := app.synthesizeWithPriceBoundary(context.Background(), "原始提示", facts)
	if err != nil {
		t.Fatal(err)
	}
	if mock.calls != 2 || !strings.Contains(answer, "跨交易日结构位") || !strings.Contains(mock.prompts[1], "未通过价格边界校验") {
		t.Fatalf("boundary retry not applied: calls=%d answer=%q prompts=%#v", mock.calls, answer, mock.prompts)
	}
	if len(mock.deadlines) != 2 {
		t.Fatalf("expected independent deadlines for both AI calls: %#v", mock.deadlines)
	}
	for _, deadline := range mock.deadlines {
		remaining := time.Until(deadline)
		if remaining < 599*time.Second || remaining > 601*time.Second {
			t.Fatalf("unexpected per-call timeout: %s", remaining)
		}
	}
}

func TestCodexReportTimeoutDefaultsToTenMinutes(t *testing.T) {
	t.Setenv("ASTOCK_CODEX_TIMEOUT_SECONDS", "")
	if timeout := codexReportTimeout(); timeout != 10*time.Minute {
		t.Fatalf("unexpected default Codex timeout: %s", timeout)
	}
	t.Setenv("ASTOCK_CODEX_TIMEOUT_SECONDS", "900")
	if timeout := codexReportTimeout(); timeout != 15*time.Minute {
		t.Fatalf("environment override ignored: %s", timeout)
	}
}

type alwaysInvalidBoundarySynthesizer struct{}

func (alwaysInvalidBoundarySynthesizer) Synthesize(context.Context, string) (string, error) {
	return "- 今日收盘突破56元后确认强势。", nil
}

func TestPriceBoundaryValidatorAutoQualifiesSecondInvalidDraft(t *testing.T) {
	facts := domain.StockReportFacts{
		PriceBoundary: domain.StockPriceBoundary{TradeDate: "2026-08-12", LimitUp: 46.96, LimitDown: 38.42, Available: true},
		Technical:     domain.TechnicalSignal{High20: 56},
	}
	app := &App{marketReportAI: alwaysInvalidBoundarySynthesizer{}}
	answer, err := app.synthesizeWithPriceBoundary(context.Background(), "prompt", facts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "56元（跨交易日结构位") || !strings.Contains(answer, "不可作为今日触发") {
		t.Fatalf("second invalid draft was not deterministically qualified: %q", answer)
	}
}

func TestAutoQualifyPriceBoundaryTextHandlesChineseJushiReport(t *testing.T) {
	facts := domain.StockReportFacts{
		PriceBoundary: domain.StockPriceBoundary{TradeDate: "2026-08-13", LimitUp: 47.64, LimitDown: 38.98, Available: true},
		Technical:     domain.TechnicalSignal{High20: 51.75, Low20: 33.72},
	}
	draft := "- 强势确认：收盘突破51.75元，且成交量达到20日均量1.20倍。\n- 风险控制：跌破33.72元时风险升级。"
	corrected := autoQualifyPriceBoundaryText(draft, facts)
	if err := validatePriceBoundaryText(facts, corrected); err != nil {
		t.Fatalf("deterministic boundary correction remains invalid: %v\n%s", err, corrected)
	}
	if strings.Count(corrected, "跨交易日结构位") != 2 {
		t.Fatalf("expected both out-of-range levels to be qualified once: %s", corrected)
	}
}

func TestStockReportBoundaryFailureUsesFriendlyFallbackReason(t *testing.T) {
	err := &priceBoundaryViolation{message: "价位 51.75 超出 2026-08-13 可交易区间 38.98-47.64，但未标明为跨交易日结构位"}
	message := stockReportAIErrorText(err)
	if strings.Contains(message, "51.75") || !strings.Contains(message, "价格边界校正") {
		t.Fatalf("boundary failure leaked internal validator text: %q", message)
	}
	markdown := renderDeterministicStockReport(domain.StockReportFacts{
		GeneratedAt: time.Now(), Quote: domain.StockQuoteSnapshot{Symbol: "sh600176", Name: "中国巨石"},
	}, message)
	if !strings.Contains(markdown, "AI综合未采用") || strings.Contains(markdown, "Codex综合暂不可用") {
		t.Fatalf("fallback header is not user-facing: %s", markdown)
	}
}

func TestStockReportPresentationRejectsInternalFieldsAndRepeatedFreshness(t *testing.T) {
	for _, markdown := range []string{
		"分钟资金：fund_movement缺失。",
		"数据采集于2026-08-13，独立角色均成功。",
	} {
		if err := validateStockReportPresentation(markdown); err == nil {
			t.Fatalf("expected presentation failure for %q", markdown)
		}
	}
	if err := validateStockReportPresentation("分钟资金样本缺失，结论置信度降低。"); err != nil {
		t.Fatalf("natural user-facing wording was rejected: %v", err)
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
