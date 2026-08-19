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
