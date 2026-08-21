package backtest

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type historyRangeMock struct {
	bars []domain.DailyBar
}

func (mock historyRangeMock) FetchDailyBarsRange(_ context.Context, symbol string, _, _ time.Time, _ PriceAdjustment) ([]domain.DailyBar, error) {
	result := make([]domain.DailyBar, len(mock.bars))
	copy(result, mock.bars)
	for index := range result {
		result[index].Symbol = symbol
	}
	return result, nil
}

type trackedHistoryRangeMock struct {
	bars   []domain.DailyBar
	starts map[string]time.Time
}

func (mock *trackedHistoryRangeMock) FetchDailyBarsRange(_ context.Context, symbol string, start, _ time.Time, _ PriceAdjustment) ([]domain.DailyBar, error) {
	if mock.starts == nil {
		mock.starts = make(map[string]time.Time)
	}
	mock.starts[symbol] = start
	result := append([]domain.DailyBar(nil), mock.bars...)
	for index := range result {
		result[index].Symbol = symbol
	}
	return result, nil
}

func testBars() []domain.DailyBar {
	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	bars := make([]domain.DailyBar, 65)
	for index := range bars {
		openPrice, closePrice, high, low, volume := 10.0, 10.0, 10.0, 9.5, 100.0
		if index == 60 {
			openPrice, closePrice, high, low, volume = 11, 12, 12.5, 10.5, 300
		}
		if index == 61 {
			openPrice, closePrice, high, low, volume = 11, 10, 11.2, 9.8, 100
		}
		if index == 62 {
			openPrice, closePrice, high, low, volume = 9, 9, 9.2, 8.8, 100
		}
		bars[index] = domain.DailyBar{
			Date: start.AddDate(0, 0, index).Format("2006-01-02"), Open: openPrice, Close: closePrice,
			High: high, Low: low, Volume: volume, Amount: closePrice * volume, Source: "测试不复权",
		}
	}
	return bars
}

func testRequest() Request {
	return Request{
		Strategy: "technical-breakout", StrategyVersion: "test-v1", Tickers: []string{"sh600519"},
		Start: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), End: time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC),
		InitialCash: 100000, CommissionRate: 0.0003, MinimumCommission: 5, StampDutyRate: 0.0005,
		TransferFeeRate: 0.00001, SlippageBPS: 10, Adjustment: AdjustmentNone, LiquidateAtEnd: true,
		Technical: TechnicalParameters{
			FastMA: 5, SlowMA: 20, BreakoutDays: 20, VolumeRatioMin: 1.2,
			StopLoss: .05, TakeProfit: .5, MaxHoldingDays: 10, MaxPosition: .5,
		},
	}
}

func TestDailyEngineRecordsNextOpenT1TradeAndFees(t *testing.T) {
	result, err := NewDailyEngine(historyRangeMock{bars: testBars()}).Run(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 1 {
		t.Fatalf("expected one closed trade, got %d: %#v", len(result.Trades), result.Trades)
	}
	trade := result.Trades[0]
	if trade.EntrySignal.Date == trade.Entry.Date {
		t.Fatalf("signal and fill must be separated by next-open execution: %#v", trade)
	}
	if trade.Entry.Date != "2024-03-03" || trade.Entry.RawPrice != 11 || trade.Exit.Date != "2024-03-04" || trade.Exit.RawPrice != 9 {
		t.Fatalf("unexpected T+1 fills: %#v", trade)
	}
	if trade.Entry.Quantity%100 != 0 || trade.Entry.TotalFee <= 0 || trade.Exit.TotalFee <= 0 {
		t.Fatalf("A-share lot or fee rules missing: %#v", trade)
	}
	if trade.Entry.Price <= trade.Entry.RawPrice || trade.Exit.Price >= trade.Exit.RawPrice {
		t.Fatalf("slippage direction is incorrect: %#v", trade)
	}
	if len(result.Equity) == 0 || result.Metrics.Trades != 1 || math.IsNaN(result.Metrics.TotalReturn) {
		t.Fatalf("missing auditable metrics: %#v", result)
	}
	if result.Metrics.AnnualizedVolatility <= 0 || result.Metrics.WorstDay >= 0 ||
		result.Metrics.BestDay < 0 || result.Metrics.Sortino == 0 || result.Metrics.Calmar == 0 {
		t.Fatalf("missing industry-standard risk metrics: %#v", result.Metrics)
	}
}

func TestValidateRequestRejectsAdjustedModeAndBadCosts(t *testing.T) {
	request := testRequest()
	request.Adjustment = AdjustmentForward
	if err := validateRequest(request); err == nil {
		t.Fatal("forward-adjusted mode should be rejected until its basis is persisted")
	}
	request = testRequest()
	request.CommissionRate = -1
	if err := validateRequest(request); err == nil {
		t.Fatal("negative fees should be rejected")
	}
	request = testRequest()
	request.Tickers = []string{"sh600519", "sh600519"}
	if err := validateRequest(request); err == nil {
		t.Fatal("duplicate ticker should be rejected")
	}
}

func TestEnrichRiskMetricsUsesPersistedEquityCurve(t *testing.T) {
	result := Result{
		Request: Request{InitialCash: 100000},
		Metrics: Metrics{AnnualizedReturn: 12, MaxDrawdown: -8},
		Equity: []EquityPoint{
			{Date: "2024-01-01", Equity: 100000},
			{Date: "2024-01-02", Equity: 101000},
			{Date: "2024-01-03", Equity: 99000},
			{Date: "2024-01-04", Equity: 100500},
		},
	}
	EnrichRiskMetrics(&result)
	if result.Metrics.AnnualizedVolatility <= 0 || result.Metrics.BestDay <= 0 || result.Metrics.WorstDay >= 0 || result.Metrics.Sharpe == 0 || result.Metrics.Sortino == 0 || result.Metrics.Calmar == 0 {
		t.Fatalf("risk metrics were not enriched: %#v", result.Metrics)
	}
}

func TestDailyEngineCanonicalizesTickerExecutionOrder(t *testing.T) {
	provider := historyRangeMock{bars: testBars()}
	left := testRequest()
	left.Tickers = []string{"sz000001", "sh600519"}
	left.Technical.MaxPosition = .7
	right := left
	right.Tickers = []string{"sh600519", "sz000001"}
	first, err := NewDailyEngine(provider).Run(context.Background(), left)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewDailyEngine(provider).Run(context.Background(), right)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Trades, second.Trades) || first.Metrics != second.Metrics ||
		strings.Join(first.Request.Tickers, ",") != "sh600519,sz000001" {
		t.Fatalf("ticker input order changed execution: first=%#v second=%#v", first, second)
	}
}

func TestDailyEngineFetchesBenchmarkWarmupAndPersistsRegimeMetrics(t *testing.T) {
	provider := &trackedHistoryRangeMock{bars: testBars()}
	request := testRequest()
	request.Benchmark = "sh000300"
	result, err := NewDailyEngine(provider).Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	benchmarkStart, ok := provider.starts[request.Benchmark]
	if !ok || !benchmarkStart.Before(request.Start.AddDate(0, 0, -150)) {
		t.Fatalf("benchmark warmup was not fetched: %v", benchmarkStart)
	}
	if !result.Metrics.BenchmarkAvailable || len(result.BenchmarkEquity) < 2 || len(result.MarketRegimes) == 0 {
		t.Fatalf("benchmark research context was not persisted: %+v", result)
	}
}

func TestCalculateMarketRegimeMetricsCompoundsConditionalReturns(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	benchmark := make([]domain.DailyBar, 70)
	for index := range benchmark {
		closePrice := 100 + float64(index)*.5
		benchmark[index] = domain.DailyBar{
			Date: start.AddDate(0, 0, index).Format("2006-01-02"),
			Open: closePrice, Close: closePrice, High: closePrice + 1, Low: closePrice - 1,
		}
	}
	equityValues := []float64{100, 102, 101, 104, 105}
	equity := make([]EquityPoint, len(equityValues))
	for index, value := range equityValues {
		equity[index] = EquityPoint{Date: benchmark[65+index].Date, Equity: value}
	}
	trades := []Trade{{Entry: Fill{Date: benchmark[68].Date}}}
	items := calculateMarketRegimeMetrics(equity, trades, benchmark)
	if len(items) != 1 {
		t.Fatalf("expected one observed market state, got %+v", items)
	}
	item := items[0]
	if item.Label != "牛市" || item.Days != 4 || item.Trades != 1 || !item.BenchmarkAvailable || item.BenchmarkDays != 4 {
		t.Fatalf("unexpected regime attribution: %+v", item)
	}
	expectedExcess := (1.05 - benchmark[69].Close/benchmark[65].Close) * 100
	if math.Abs(item.ReturnPercent-5) > 1e-9 || item.MaxDrawdown >= 0 || math.Abs(item.ExcessReturn-expectedExcess) > 1e-9 {
		t.Fatalf("unexpected conditional performance: %+v", item)
	}
}
