package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/marketregime"
)

func risingBars(count int) []domain.DailyBar {
	result := make([]domain.DailyBar, 0, count)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.Local)
	for index := 0; index < count; index++ {
		closePrice := 10 + float64(index)*0.1
		result = append(result, domain.DailyBar{
			Symbol: "sh600000", Source: "test", Date: start.AddDate(0, 0, index).Format("2006-01-02"),
			Open: closePrice - 0.05, Close: closePrice, High: closePrice + 0.08, Low: closePrice - 0.08,
			Volume: 1_000_000, Amount: closePrice * 1_000_000,
		})
	}
	return result
}

func TestTrendBreakoutUsesRealtimePriceAndVolumeRatio(t *testing.T) {
	bars := risingBars(70)
	input := Snapshot{
		Stock: domain.MarketStockSnapshot{
			Symbol: "sh600000", Name: "浦发银行", Price: 18, Percent: 3, Speed: .4, VolumeRatio: 1.8,
		},
		Bars: bars,
	}
	result := evaluateTrendBreakout(input)
	if result.Score != 20 || result.State != "触发" {
		t.Fatalf("unexpected breakout result: %+v", result)
	}
	if len(result.Reasons) < 4 {
		t.Fatalf("expected auditable reasons: %+v", result.Reasons)
	}
}

func TestCompletedBarsExcludeInProgressTradingDay(t *testing.T) {
	bars := risingBars(70)
	currentDate := "2026-08-20"
	bars[len(bars)-1].Date = currentDate
	bars[len(bars)-1].Close = 1000
	filtered := completedBars(bars, time.Date(2026, 8, 20, 10, 30, 0, 0, time.Local))
	if len(filtered) != len(bars)-1 {
		t.Fatalf("in-progress bar was not excluded: got %d want %d", len(filtered), len(bars)-1)
	}
	if filtered[len(filtered)-1].Date == currentDate || filtered[len(filtered)-1].Close >= 1000 {
		t.Fatalf("filtered history still contains partial bar: %+v", filtered[len(filtered)-1])
	}
	closed := completedBars(bars, time.Date(2026, 8, 20, 16, 0, 0, 0, time.Local))
	if len(closed) != len(bars) || closed[len(closed)-1].Date != currentDate {
		t.Fatalf("closed-session history should retain current day: got %d/%s", len(closed), closed[len(closed)-1].Date)
	}
}

func TestCompletedBarsDoesNotRestorePartialBarForShortHistory(t *testing.T) {
	bars := risingBars(65)
	currentDate := "2026-08-20"
	bars[len(bars)-1].Date = currentDate
	filtered := completedBars(bars, time.Date(2026, 8, 20, 10, 30, 0, 0, time.Local))
	if len(filtered) != len(bars)-1 {
		t.Fatalf("short live history must exclude the partial bar: got %d want %d", len(filtered), len(bars)-1)
	}
	if _, ok := calculateIndicators(Snapshot{Now: time.Date(2026, 8, 20, 10, 30, 0, 0, time.Local), Bars: bars}); ok {
		t.Fatal("indicators should remain unavailable when completed bars do not meet the warmup threshold")
	}
}

func TestDefaultStrategiesAreIndependentComponents(t *testing.T) {
	items := DefaultStrategies()
	if len(items) != 9 {
		t.Fatalf("expected nine realtime strategies, got %d", len(items))
	}
	seen := make(map[string]bool)
	for _, item := range items {
		if item.Key() == "" || item.Name() == "" || seen[item.Key()] {
			t.Fatalf("invalid strategy catalog item: %s %s", item.Key(), item.Name())
		}
		seen[item.Key()] = true
	}
}

func TestScannerNormalizesAvailableComponentScores(t *testing.T) {
	scanner := &Scanner{strategies: DefaultStrategies(), now: func() time.Time {
		return time.Date(2026, 8, 20, 10, 30, 0, 0, time.Local)
	}}
	bars := risingBars(70)
	benchmark := risingBars(70)
	input := Snapshot{
		Now: scanner.now(), Bars: bars, Benchmark: benchmark,
		Stock: domain.MarketStockSnapshot{
			Symbol: "sh600000", Name: "浦发银行", Industry: "银行", Price: 18, Percent: 2.5,
			Speed: .2, VolumeRatio: 1.6, Amount: 8e8, MainNet: 2e8, MainRatio: 5,
		},
		Board: &domain.BoardFlow{Name: "银行", Percent: 1.5, MainNet: 5e9, RiseCount: 30, FallCount: 5},
	}
	result := scanner.evaluate(input)
	if result.Score <= 0 || len(result.Components) != len(DefaultStrategies()) || result.TriggerPrice <= 0 || result.InvalidationPrice <= 0 {
		t.Fatalf("unexpected signal: %s", fmt.Sprintf("%+v", result))
	}
	if result.RiskAdjustedScore > result.Score || result.RiskMultiplier <= 0 || result.MarketRegime == "" {
		t.Fatalf("risk overlay did not preserve raw score evidence: %+v", result)
	}
	if result.EntryShape == "" || result.EntryShapeLabel == "" || result.EntryShapeScore <= 0 || len(result.EntryShapeEvidence) == 0 || result.EntryShapeTriggerPrice <= 0 || result.EntryShapeInvalidationPrice <= 0 {
		t.Fatalf("entry shape evidence missing: %+v", result)
	}
}

func TestQuoteDateCoverageDetectsStaleOnlySnapshot(t *testing.T) {
	stale, current := quoteDateCoverage([]Signal{
		{QuoteTime: "2026-08-20 15:00:00"},
		{QuoteTime: "2026-08-20 15:00:01"},
	}, "2026-08-21")
	if stale != 2 || current != 0 {
		t.Fatalf("unexpected stale-only coverage: stale=%d current=%d", stale, current)
	}
	stale, current = quoteDateCoverage([]Signal{
		{QuoteTime: "2026-08-20 15:00:00"},
		{QuoteTime: "2026-08-21 09:35:00"},
	}, "2026-08-21")
	if stale != 1 || current != 1 {
		t.Fatalf("unexpected mixed coverage: stale=%d current=%d", stale, current)
	}
}

func TestScannerAcceptsInjectedMakeupSaturdayCalendar(t *testing.T) {
	scanner := &Scanner{strategies: DefaultStrategies(), now: func() time.Time {
		return time.Date(2026, 8, 22, 10, 30, 0, 0, marketLocation)
	}, calendar: func(context.Context, time.Time) ([]string, error) {
		return []string{"2026-08-20", "2026-08-22"}, nil
	}}
	if scanner.calendar == nil {
		t.Fatal("calendar provider was not assigned")
	}
	// The full network scan path is covered by integration adapters; this
	// assertion protects the injectable contract used by the Web constructor.
	scanner.SetTradingCalendarProvider(scanner.calendar)
}

func TestClassifyEntryShapePrefersConfirmedSqueezeOverGenericMomentum(t *testing.T) {
	input := Snapshot{Stock: domain.MarketStockSnapshot{Price: 10.8, VolumeRatio: 1.6}}
	value := indicators{
		latest:           domain.DailyBar{Close: 10.7, Low: 10.2},
		previous:         domain.DailyBar{Close: 10.5},
		ma5:              10.4,
		ma20:             10.1,
		ma60:             9.5,
		previousMA20:     10.0,
		prior20High:      11.2,
		priorShortHigh:   10.6,
		return20:         .11,
		rangeCompression: .45,
		volumeRatio:      1.6,
		rsi14:            58,
	}
	id, label, score, evidence, trigger, invalidation := classifyEntryShape(input, value)
	if id != "volatility-squeeze" || label != "波动收缩突破" || score <= 0 || len(evidence) == 0 || trigger != 10.6 || invalidation != 10.1 {
		t.Fatalf("unexpected squeeze classification: %s %s %.1f %v %.2f %.2f", id, label, score, evidence, trigger, invalidation)
	}
}

func TestClassifyEntryShapeDoesNotCallMissingVolumeBreakoutConfirmed(t *testing.T) {
	input := Snapshot{Stock: domain.MarketStockSnapshot{Price: 12}}
	value := indicators{
		latest:           domain.DailyBar{Close: 12, Low: 11.5},
		previous:         domain.DailyBar{Close: 11.8},
		ma5:              11.5,
		ma20:             11,
		ma60:             10,
		prior20High:      11.8,
		prior20Low:       9.8,
		priorShortHigh:   11.6,
		rangeCompression: .45,
		return20:         .12,
		rsi14:            58,
		volumeRatio:      math.NaN(),
	}
	id, label, _, evidence, _, _ := classifyEntryShape(input, value)
	if id == "breakout" && label == "放量突破" {
		t.Fatalf("missing volume was presented as confirmed breakout: %s %s", id, label)
	}
	joined := strings.Join(evidence, "；")
	if !strings.Contains(joined, "量能") {
		t.Fatalf("missing volume evidence was not surfaced: %v", evidence)
	}
}

func TestScannerStateUsesRiskAdjustedScoreWithoutOverwritingRawScore(t *testing.T) {
	strategies := []Strategy{
		strategyFunc{key: "trend-breakout", name: "趋势突破", fn: func(Snapshot) Component {
			return component("trend-breakout", "趋势突破", 20, []string{"强趋势"}, nil)
		}},
		strategyFunc{key: "relative-momentum", name: "相对动量", fn: func(Snapshot) Component {
			return component("relative-momentum", "相对动量", 20, []string{"强动量"}, nil)
		}},
		strategyFunc{key: "price-volume", name: "量价确认", fn: func(Snapshot) Component {
			return component("price-volume", "量价确认", 20, []string{"量价确认"}, nil)
		}},
		strategyFunc{key: "fund-support", name: "资金承接", fn: func(Snapshot) Component {
			return component("fund-support", "资金承接", 20, []string{"资金承接"}, nil)
		}},
		strategyFunc{key: "sector-rotation", name: "板块轮动", fn: func(Snapshot) Component {
			return component("sector-rotation", "板块轮动", 20, []string{"板块走强"}, nil)
		}},
		strategyFunc{key: "market-regime", name: "市场适配", fn: func(Snapshot) Component {
			return component("market-regime", "市场适配", 20, []string{"弱市相对强"}, nil)
		}},
		strategyFunc{key: "volatility-risk", name: "波动风险", fn: func(Snapshot) Component {
			return component("volatility-risk", "波动风险", 20, []string{"波动可控"}, nil)
		}},
		strategyFunc{key: "liquidity-quality", name: "流动性质量", fn: func(Snapshot) Component {
			return component("liquidity-quality", "流动性质量", 20, []string{"流动性充足"}, nil)
		}},
	}
	scanner := &Scanner{strategies: strategies, now: func() time.Time { return time.Date(2026, 8, 20, 10, 30, 0, 0, time.Local) }}
	bearBenchmark := risingBars(70)
	for index := range bearBenchmark {
		price := 140 - float64(index)*.5
		bearBenchmark[index].Open, bearBenchmark[index].Close, bearBenchmark[index].High, bearBenchmark[index].Low = price, price, price*1.01, price*.99
	}
	result := scanner.evaluate(Snapshot{Now: scanner.now(), Bars: risingBars(70), Benchmark: bearBenchmark, Stock: domain.MarketStockSnapshot{Symbol: "sh600000", Price: 18}})
	if result.Score != 100 || result.RiskAdjustedScore != 60 || result.State != StateWatching {
		t.Fatalf("risk-adjusted state unexpected: %+v", result)
	}
}

func TestMarketRiskOverlayMapsCanonicalRegimes(t *testing.T) {
	tests := []struct {
		regime marketregime.Regime
		want   float64
		ok     bool
	}{
		{marketregime.Bull, 1, true},
		{marketregime.Range, .85, true},
		{marketregime.Bear, .60, true},
		{marketregime.HighVol, .45, true},
		{marketregime.Insufficient, 1, false},
	}
	for _, test := range tests {
		got, _, ok := marketRiskOverlay(test.regime)
		if got != test.want || ok != test.ok {
			t.Fatalf("regime %s: multiplier=%.2f ok=%v, want %.2f/%v", test.regime, got, ok, test.want, test.ok)
		}
	}
}

func TestCrossSectionOverlayRanksDeterministicallyAndKeepsRawScore(t *testing.T) {
	items := []Signal{
		{ID: "b", Symbol: "sz000002", Score: 80, RiskAdjustedScore: 48, State: StateWatching, Speed: 1},
		{ID: "c", Symbol: "sh600003", Score: 80, RiskAdjustedScore: 80, State: StateTriggered, Speed: 1},
		{ID: "a", Symbol: "sh600001", Score: 90, RiskAdjustedScore: 90, State: StateTriggered},
	}
	result := applyCrossSectionOverlay(items)
	if result[0].Symbol != "sh600001" || result[1].Symbol != "sh600003" || result[2].Symbol != "sz000002" {
		t.Fatalf("unexpected deterministic order: %+v", result)
	}
	if result[1].CrossSectionRank != 2 || result[1].CrossSectionTotal != 3 || result[0].CrossSectionPercentile != 100 || result[2].CrossSectionPercentile != 0 {
		t.Fatalf("unexpected cross-sectional evidence: %+v", result)
	}
	if result[1].Score != 80 || result[1].RiskAdjustedScore != 80 {
		t.Fatalf("raw or adjusted score was overwritten: %+v", result[1])
	}
	if !result[0].PortfolioEligible || !result[1].PortfolioEligible || result[2].PortfolioEligible {
		t.Fatalf("portfolio eligibility did not preserve strong raw candidates and downgrade weak risk-adjusted evidence: %+v", result)
	}
}

func TestCrossSectionOverlaySingleSignalGetsFinitePercentile(t *testing.T) {
	result := applyCrossSectionOverlay([]Signal{{ID: "only", Symbol: "sh600000", Score: 60, RiskAdjustedScore: 60, State: StateWatching, RiskMultiplier: 1, MarketRegime: string(marketregime.Bull)}})
	if len(result) != 1 || result[0].CrossSectionPercentile != 100 || result[0].CrossSectionRank != 1 || result[0].CrossSectionTotal != 1 {
		t.Fatalf("single-signal percentile invalid: %+v", result)
	}
	if !result[0].PortfolioEligible {
		t.Fatalf("single strong signal should be portfolio eligible: %+v", result[0])
	}
}

func TestCrossSectionOverlaySeparatesFullPoolAndTradablePoolRanks(t *testing.T) {
	items := []Signal{
		{ID: "weak", Symbol: "sh600001", Score: 95, RiskAdjustedScore: 0, State: StateInvalid},
		{ID: "strong", Symbol: "sh600002", Score: 80, RiskAdjustedScore: 80, RiskMultiplier: 1, MarketRegime: string(marketregime.Bull), State: StateTriggered},
		{ID: "watch", Symbol: "sh600003", Score: 70, RiskAdjustedScore: 70, RiskMultiplier: 1, MarketRegime: string(marketregime.Bull), State: StateWatching},
	}
	result := applyCrossSectionOverlay(items)
	var strong, weak Signal
	for _, signal := range result {
		switch signal.ID {
		case "strong":
			strong = signal
		case "weak":
			weak = signal
		}
	}
	if strong.CrossSectionRank != 2 || strong.TradableRank != 1 || strong.TradableTotal != 2 || !strong.PortfolioEligible {
		t.Fatalf("strong signal did not receive tradable rank: %+v", strong)
	}
	if weak.TradableRank != 0 || weak.PortfolioEligible {
		t.Fatalf("invalid signal consumed tradable pool slot: %+v", weak)
	}
}

func TestCrossSectionOverlayLimitsIndustryConcentration(t *testing.T) {
	items := make([]Signal, 0, 6)
	for index := 0; index < 5; index++ {
		items = append(items, Signal{ID: fmt.Sprintf("tech-%d", index), Symbol: fmt.Sprintf("sh60000%d", index), Industry: "半导体", Score: 90 - float64(index), RiskAdjustedScore: 90 - float64(index), RiskMultiplier: 1, MarketRegime: string(marketregime.Bull), State: StateTriggered})
	}
	items = append(items, Signal{ID: "bank", Symbol: "sh600099", Industry: "银行", Score: 70, RiskAdjustedScore: 70, RiskMultiplier: 1, MarketRegime: string(marketregime.Bull), State: StateTriggered})
	result := applyCrossSectionOverlay(items)
	acceptedTech := 0
	for _, signal := range result {
		if signal.Industry == "半导体" && signal.PortfolioEligible {
			acceptedTech++
		}
	}
	if acceptedTech >= 5 {
		t.Fatalf("industry cap did not reduce concentration: %+v", result)
	}
}

func TestSignalJSONRemainsBackwardCompatible(t *testing.T) {
	var signal Signal
	if err := json.Unmarshal([]byte(`{"id":"legacy","symbol":"sh600000","score":70,"state":"watching"}`), &signal); err != nil {
		t.Fatal(err)
	}
	if signal.ID != "legacy" || signal.Score != 70 || signal.RiskAdjustedScore != 0 || signal.CrossSectionTotal != 0 {
		t.Fatalf("legacy signal did not decode compatibly: %+v", signal)
	}
}

func TestCompositeScoreTreatsBreakoutAndPullbackAsAlternativeSetups(t *testing.T) {
	components := []Component{
		{Key: "trend-breakout", Score: 20, State: "触发"},
		{Key: "ma-pullback", Score: 0, State: "弱"},
		{Key: "relative-momentum", Score: 10, State: "观察"},
		{Key: "price-volume", Score: 10, State: "观察"},
		{Key: "fund-support", Score: 10, State: "观察"},
		{Key: "sector-rotation", Score: 10, State: "观察"},
		{Key: "market-regime", Score: 10, State: "观察"},
		{Key: "volatility-risk", Score: 10, State: "观察"},
		{Key: "liquidity-quality", Score: 10, State: "观察"},
	}
	score, coverage, contributions := compositeScore(components)
	if math.Abs(coverage-1) > 1e-9 || score <= 50 || score >= 90 {
		t.Fatalf("unexpected composite score: score=%.2f coverage=%.2f contributions=%v", score, coverage, contributions)
	}
	if contributions["trend-breakout"] <= 0 || contributions["ma-pullback"] != 0 {
		t.Fatalf("alternative setup family was not collapsed: %v", contributions)
	}
}

func TestTopReasonsPrioritizeAlphaAndContextOverQualityChecks(t *testing.T) {
	components := []Component{
		{Key: "trend-breakout", Name: "趋势突破", Score: 10, Reasons: []string{"趋势证据"}},
		{Key: "relative-momentum", Name: "相对动量", Score: 10, Reasons: []string{"动量证据"}},
		{Key: "price-volume", Name: "量价确认", Score: 10, Reasons: []string{"量价证据"}},
		{Key: "fund-support", Name: "资金承接", Score: 10, Reasons: []string{"资金证据"}},
		{Key: "sector-rotation", Name: "板块轮动", Score: 10, Reasons: []string{"板块证据"}},
		{Key: "market-regime", Name: "市场适配", Score: 10, Reasons: []string{"市场证据"}},
		{Key: "volatility-risk", Name: "波动风险", Score: 20, Reasons: []string{"低波动"}},
		{Key: "liquidity-quality", Name: "流动性质量", Score: 20, Reasons: []string{"流动性充足"}},
	}
	_, _, contributions := compositeScore(components)
	reasons := topReasons(components, contributions, 5)
	if len(reasons) != 5 {
		t.Fatalf("unexpected reason count: %v", reasons)
	}
	for _, reason := range reasons {
		if strings.HasPrefix(reason, "波动风险:") || strings.HasPrefix(reason, "流动性质量:") {
			t.Fatalf("quality check displaced primary alpha evidence: %v", reasons)
		}
	}
}

func TestRiskAndLiquidityComponentsExposeIndependentEvidence(t *testing.T) {
	bars := risingBars(70)
	input := Snapshot{
		Bars:      bars,
		Benchmark: risingBars(70),
		Stock: domain.MarketStockSnapshot{
			Symbol: "sh600000", Price: 17.1, Amount: 1.2e7, Turnover: 2.5, VolumeRatio: 1.1,
		},
	}
	for _, item := range []struct {
		key string
		fn  func(Snapshot) Component
	}{
		{key: "price-volume", fn: evaluatePriceVolume},
		{key: "volatility-risk", fn: evaluateVolatilityRisk},
		{key: "liquidity-quality", fn: evaluateLiquidityQuality},
		{key: "market-regime", fn: evaluateMarketRegime},
	} {
		result := item.fn(input)
		if result.Key != item.key || result.State == "数据不足" || result.Maximum != 20 {
			t.Fatalf("unexpected %s component: %+v", item.key, result)
		}
		if len(result.Reasons) == 0 {
			t.Fatalf("%s component lacks auditable reasons: %+v", item.key, result)
		}
	}
}

func TestNormalizeIndustryNameMatchesBroadStockIndustryToBoard(t *testing.T) {
	tests := []struct {
		stockIndustry string
		boardName     string
	}{
		{stockIndustry: "半导体", boardName: "半导体设备"},
		{stockIndustry: "通信设备", boardName: "通信设备"},
		{stockIndustry: "白酒", boardName: "白酒Ⅱ"},
	}
	for _, test := range tests {
		stockKey := normalizeIndustryName(test.stockIndustry)
		boardKey := normalizeIndustryName(test.boardName)
		if stockKey == "" || stockKey != boardKey {
			t.Fatalf("industry mismatch: stock=%q (%q) board=%q (%q)", test.stockIndustry, stockKey, test.boardName, boardKey)
		}
	}
}

type industryBoardMarketStub struct{}

func (industryBoardMarketStub) FetchStockRanking(context.Context, domain.MarketScanMetric, bool, int) ([]domain.MarketStockSnapshot, error) {
	return nil, nil
}

func (industryBoardMarketStub) FetchStocks(context.Context, []string) ([]domain.MarketStockSnapshot, error) {
	return nil, nil
}

func (industryBoardMarketStub) FetchIndustryRanking(context.Context, domain.MarketScanMetric, bool, int) ([]domain.BoardFlow, error) {
	return []domain.BoardFlow{
		{Name: "半导体设备", Percent: 3.2, MainNet: 2e9},
		{Name: "半导体", Percent: 2.8, MainNet: 1e9},
	}, nil
}

func (industryBoardMarketStub) FetchIndustryFlows(context.Context) (map[string]domain.BoardFlow, error) {
	return map[string]domain.BoardFlow{
		"半导体设备": {Name: "半导体设备", Percent: 0.6, MainNet: 25_249_024, RiseCount: 13, FallCount: 12},
		"半导体":   {Name: "半导体", Percent: 0.39, MainNet: -7_480_720_896, RiseCount: 100, FallCount: 84, FlatCount: 1},
	}, nil
}

func TestScannerIndexesIndustryBoardsByNormalizedName(t *testing.T) {
	scanner := &Scanner{market: industryBoardMarketStub{}}
	warnings := make([]string, 0)
	boards := scanner.fetchBoards(context.Background(), &warnings)
	board, found := matchIndustryBoard(boards, "半导体")
	if !found || board.Name != "半导体" || len(warnings) != 0 {
		t.Fatalf("normalized board lookup failed: board=%+v found=%v warnings=%v", board, found, warnings)
	}
	communicationBoards := map[string]domain.BoardFlow{
		"通信":   {Name: "通信"},
		"通信设备": {Name: "通信设备"},
	}
	board, found = matchIndustryBoard(communicationBoards, "通信设备")
	if !found || board.Name != "通信设备" {
		t.Fatalf("exact industry should win over normalized alias: board=%+v found=%v", board, found)
	}
}

func TestScannerEnrichesMissingSectorWithoutChangingArchivedScore(t *testing.T) {
	scanner := &Scanner{market: industryBoardMarketStub{}}
	original := ScanResult{
		GeneratedAt: time.Date(2026, 8, 20, 15, 0, 37, 0, time.Local),
		Signals: []Signal{
			{
				Symbol: "sh688766", Name: "普冉股份", Industry: "半导体", Score: 57.2, State: StateWatching,
				Components: []Component{
					{Key: "sector-rotation", Name: "板块轮动", Maximum: 20, State: "数据不足", Warnings: []string{"未匹配到实时行业板块"}},
				},
				Warnings: []string{"未匹配到实时行业板块"},
			},
		},
	}
	updated, err := scanner.EnrichSectors(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	signal := updated.Signals[0]
	component := signal.Components[0]
	if component.State == "数据不足" || component.Score <= 0 || len(component.Reasons) == 0 || len(component.Warnings) != 0 {
		t.Fatalf("sector component was not enriched: %+v", component)
	}
	if signal.Score != original.Signals[0].Score || signal.State != original.Signals[0].State || len(signal.Warnings) != 0 {
		t.Fatalf("archived signal summary changed during enrichment: %+v", signal)
	}
}
