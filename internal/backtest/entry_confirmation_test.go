package backtest

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func confirmationFixture() ([]domain.DailyBar, Request) {
	bars := testBars()
	bars[60].Open, bars[60].Close, bars[60].High = 10, 11, 11.2
	bars[60].Low = 9.8
	bars[61].Close, bars[61].Volume = 11, 1
	request := testRequest()
	request.Technical.EntryMode = EntryModeReclaim
	request.EntryConfirmation = EntryConfirmReclaimTwoClose
	return bars, request
}

func TestConfirmationWaitsForNextCloseAndPreservesT1Exit(t *testing.T) {
	bars, request := confirmationFixture()
	run, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Trades) != 1 || len(run.EntryConfirmations) != 1 {
		t.Fatalf("missing confirmation trade: %+v", run)
	}
	trade := run.Trades[0]
	event := run.EntryConfirmations[0]
	if trade.EntrySignal.Date != bars[60].Date || event.ConfirmationDate != bars[61].Date || trade.Entry.Date != bars[62].Date || trade.Entry.RawPrice != 9 || trade.Exit.Date != bars[63].Date {
		t.Fatalf("confirmation/open/T+1 timing changed: %+v", trade)
	}
	if event.Status != "confirmed" || !reflect.DeepEqual(trade.EntrySignal.Confirmation, &event) {
		t.Fatalf("missing original signal evidence: %+v", trade)
	}
	// Today's closing crash may cause an exit signal, but cannot resize its opening buy.
	bars[62].Close, bars[62].Low = 5, 4.8
	changed, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(changed.Trades) != 1 || !reflect.DeepEqual(trade.Entry, changed.Trades[0].Entry) {
		t.Fatalf("future close affected buy fill: %+v %v", changed, err)
	}
}

func TestConfirmationRejectsFailureWithoutStaleReplay(t *testing.T) {
	bars, request := confirmationFixture()
	bars[61].Close = 9
	// A later recovery without original signal volume cannot revive the failed candidate.
	bars[62].Open, bars[62].Close, bars[62].High, bars[62].Low, bars[62].Volume = 11, 11, 11.2, 10.8, 1
	run, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(run.Trades) != 0 || len(run.OpenPositions) != 0 || len(run.EntryConfirmations) != 1 || run.EntryConfirmations[0].Status != "rejected" {
		t.Fatalf("failed confirmation created a trade: %+v %v", run, err)
	}
	request.EntryConfirmation = ""
	baseline, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(baseline.Trades) != 1 || baseline.Trades[0].Entry.Date != bars[61].Date || len(baseline.EntryConfirmations) != 0 || baseline.Trades[0].EntrySignal.Confirmation != nil {
		t.Fatalf("default next-open behavior changed: %+v %v", baseline, err)
	}
}

func TestConfirmationUsesStockSessionsAndDefersOnePriceFills(t *testing.T) {
	bars, request := confirmationFixture()
	for i := 61; i < len(bars); i++ {
		date, _ := time.Parse("2006-01-02", bars[i].Date)
		bars[i].Date = date.AddDate(0, 0, 3).Format("2006-01-02")
	}
	bars[62].Open, bars[62].Close, bars[62].High, bars[62].Low = 11, 11, 11, 11
	request.End, _ = time.Parse("2006-01-02", bars[63].Date)
	run, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(run.Trades) != 0 || len(run.OpenPositions) != 1 {
		t.Fatalf("one-price/T+1 behavior changed: %+v %v", run, err)
	}
	position := run.OpenPositions[0]
	if position.Entry.Date != bars[63].Date || position.EntrySignal.Confirmation.ConfirmationDate != bars[61].Date {
		t.Fatalf("confirmation used calendar days or filled one-price bar: %+v", position)
	}
}

func TestConfirmationAtWindowEndDoesNotInventFill(t *testing.T) {
	for _, end := range []int{60, 61} {
		bars, request := confirmationFixture()
		request.End, _ = time.Parse("2006-01-02", bars[end].Date)
		run, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
		if err != nil || len(run.Trades) != 0 || len(run.OpenPositions) != 0 || len(run.EntryConfirmations) != 1 {
			t.Fatalf("end-of-window fill invented: %+v %v", run, err)
		}
		want := "unresolved"
		if end == 61 {
			want = "confirmed"
		}
		if run.EntryConfirmations[0].Status != want {
			t.Fatal("wrong boundary confirmation status")
		}
	}
}

func TestConfirmationRequiresStrictPriceAndTrend(t *testing.T) {
	for _, snapshot := range []SignalSnapshot{{Close: 10, FastMA: 10, SlowMA: 9}, {Close: 12, FastMA: 10, SlowMA: 10}, {Close: 12, FastMA: 10, SlowMA: 11}} {
		if event := confirmReclaimEntry("test", SignalSnapshot{}, snapshot, true); event.Status != "rejected" {
			t.Fatalf("invalid confirmation accepted: %+v", event)
		}
	}
	_, request := confirmationFixture()
	request.EntryConfirmation = "unknown"
	if validateRequest(request) == nil {
		t.Fatal("unknown confirmation accepted")
	}
	request.EntryConfirmation, request.Technical.EntryMode = EntryConfirmReclaimTwoClose, EntryModeBreakout
	if validateRequest(request) == nil {
		t.Fatal("confirmation silently applied to another entry mode")
	}
}
