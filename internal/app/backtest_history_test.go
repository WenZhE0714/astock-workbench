package app

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
)

type appBacktestRangeProviderMock struct {
	calls int
}

func (mock *appBacktestRangeProviderMock) FetchDailyBars(context.Context, string) ([]domain.DailyBar, error) {
	return nil, nil
}

func (mock *appBacktestRangeProviderMock) FetchDailyBarsRange(
	_ context.Context,
	_ string,
	_, _ time.Time,
	_ backtest.PriceAdjustment,
) ([]domain.DailyBar, error) {
	mock.calls++
	start := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := make([]domain.DailyBar, 140)
	for index := range bars {
		closePrice := 10 + float64(index)/100
		bars[index] = domain.DailyBar{
			Date: start.AddDate(0, 0, index).Format("2006-01-02"), Open: closePrice, Close: closePrice,
			High: closePrice + .1, Low: closePrice - .1, Volume: 1000, Amount: closePrice * 1000,
			Turnover: math.NaN(), Source: "测试区间源",
		}
	}
	return bars, nil
}

func TestNewBacktestEngineUsesApplicationHistoryProvider(t *testing.T) {
	provider := &appBacktestRangeProviderMock{}
	application := &App{history: provider, backtestHistory: &appBacktestRangeProviderMock{}}
	request := backtest.Request{
		Strategy: "technical-breakout", StrategyVersion: "test", Tickers: []string{"sh600519"},
		Start: time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2019, 4, 1, 0, 0, 0, 0, time.UTC),
		InitialCash: 100000, CommissionRate: .0003, MinimumCommission: 5, StampDutyRate: .0005,
		TransferFeeRate: .00001, SlippageBPS: 5, Adjustment: backtest.AdjustmentNone,
		Technical: backtest.DefaultTechnicalParameters(),
	}
	if _, err := application.newBacktestEngine().Run(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if provider.calls == 0 {
		t.Fatal("backtest engine did not use the application's injected history provider")
	}
}
