package realtime

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
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
