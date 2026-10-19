package backtest

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func exitProtectionFixture() ([]domain.DailyBar, Request) {
	bars, request := confirmationFixture()
	request.EntryConfirmation = ""
	request.ExitProtection = ExitProtectHalfProfit
	request.NoFutureData = true
	request.Technical.StopLoss = .08
	request.End, _ = time.Parse(time.DateOnly, bars[64].Date)
	bars[61].Open, bars[61].Close, bars[61].High, bars[61].Low = 11, 12.2, 12.3, 10.8
	bars[62].Open, bars[62].Close, bars[62].High, bars[62].Low = 12, 11.5, 12.1, 11.4
	bars[63].Open, bars[63].Close, bars[63].High, bars[63].Low = 11.3, 11.4, 11.6, 11
	return bars, request
}

func TestProfitProtectionUsesClosingPeakAndNextOpen(t *testing.T) {
	bars, request := exitProtectionFixture()
	run, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(run.Trades) != 1 {
		t.Fatalf("missing protection trade: %+v %v", run, err)
	}
	trade := run.Trades[0]
	evidence := trade.ExitSignal.ProfitProtection
	if evidence == nil || evidence.PeakDate != bars[61].Date || trade.ExitSignal.Date != bars[62].Date || trade.Exit.Date != bars[63].Date || trade.Exit.RawPrice != 11.3 || trade.Entry.Date != bars[61].Date {
		t.Fatalf("incorrect close/next-open timing: %+v", trade)
	}
	if evidence.ActivationReturn != 8 || evidence.PeakNetReturn < 8 || evidence.CurrentNetReturn > evidence.TriggerNetReturn || evidence.TriggerNetReturn != evidence.PeakNetReturn/2 {
		t.Fatalf("incorrect protection evidence: %+v", evidence)
	}
	// An intraday spike and the exit day's later close cannot move the exit signal or fill.
	bars[61].High = 50
	bars[63].Close, bars[63].Low = 5, 4.8
	changed, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(changed.Trades) != 1 {
		t.Fatalf("missing modified-path trade: %v", err)
	}
	if changed.Trades[0].ExitSignal.Date != trade.ExitSignal.Date || !reflect.DeepEqual(evidence, changed.Trades[0].ExitSignal.ProfitProtection) || !reflect.DeepEqual(trade.Exit, changed.Trades[0].Exit) {
		t.Fatal("future close or intraday high leaked into protection or fill")
	}
	request.ExitProtection = ""
	baseline, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(baseline.Trades) != 1 || !reflect.DeepEqual(baseline.Trades[0].Entry, trade.Entry) || baseline.Trades[0].ExitSignal.ProfitProtection != nil {
		t.Fatal("entry/default strategy behavior changed")
	}
}

func TestProfitProtectionPreservesOriginalExitPriorityAndOnePriceDeferral(t *testing.T) {
	bars, request := exitProtectionFixture()
	request.Technical.TakeProfit = .10
	run, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(run.Trades) != 1 || run.Trades[0].Exit.Date != bars[62].Date || run.Trades[0].ExitSignal.ProfitProtection != nil {
		t.Fatalf("original take profit did not take priority: %+v %v", run, err)
	}
	bars, request = exitProtectionFixture()
	bars[62].Close, bars[62].Low = 9.5, 9.3
	run, err = NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(run.Trades) != 1 || run.Trades[0].ExitSignal.ProfitProtection != nil {
		t.Fatal("original stop loss did not take priority")
	}
	bars, request = exitProtectionFixture()
	bars[63].Open, bars[63].Close, bars[63].High, bars[63].Low = 11.3, 11.3, 11.3, 11.3
	bars[64].Open = 9
	bars[64].Low = 8.8
	run, err = NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(run.Trades) != 1 || run.Trades[0].Exit.Date != bars[64].Date || run.Trades[0].ExitSignal.Date != bars[62].Date || run.Trades[0].NetProfit >= 0 {
		t.Fatalf("one-price delay/gap risk hidden: %+v %v", run, err)
	}
}

func TestProfitProtectionDoesNotArmOnIntradayHighOrIgnoreCosts(t *testing.T) {
	bars, request := exitProtectionFixture()
	bars[61].Close, bars[61].High = 11.5, 50
	bars[62].Close, bars[62].Low = 11.2, 11.1
	run, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for _, trade := range run.Trades {
		if trade.ExitSignal.ProfitProtection != nil {
			t.Fatal("intraday high armed closing protection")
		}
	}
	entry := makeFill(request, "sh600000", "buy", "2026-01-01", 10, 100, false)
	position := &positionState{symbol: "sh600000", quantity: 100, entry: entry, entryCost: entry.Amount + entry.TotalFee}
	updateNetClosePeak(request, position, 10.9, "2026-01-01")
	if position.peakNetCloseReturn >= 8 {
		t.Fatal("entry/exit fees were not included in activation")
	}
	_, _, triggered := profitProtectionSignal(SignalSnapshot{Date: "2026-01-02", Close: 10.1}, position, request)
	if triggered {
		t.Fatal("protection armed on gross instead of net return")
	}
	request.ExitProtection = "invalid"
	if validateRequest(request) == nil {
		t.Fatal("unknown exit protection accepted")
	}
	request.ExitProtection, request.Technical.EntryMode = ExitProtectHalfProfit, EntryModeBreakout
	if validateRequest(request) == nil {
		t.Fatal("protection applied to unsupported strategy")
	}
}

func TestProfitProtectionPendingAtEndIsNotFilledAtSignalClose(t *testing.T) {
	bars, request := exitProtectionFixture()
	request.End, _ = time.Parse(time.DateOnly, bars[62].Date)
	request.LiquidateAtEnd = false
	run, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(run.Trades) != 0 || len(run.OpenPositions) != 1 {
		t.Fatalf("protection filled at same close: %+v %v", run, err)
	}
}
