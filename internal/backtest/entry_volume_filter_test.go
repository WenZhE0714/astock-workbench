package backtest

import (
	"context"
	"math"
	"reflect"
	"testing"
)

func TestEntryVolumeFloorKeepsOriginalShapeAndUpperBound(t *testing.T) {
	for _, item := range []struct {
		name           string
		ratio          float64
		trades, blocks int
	}{{"below original minimum", .8, 0, 0}, {"below new floor", 1.499, 0, 1}, {"at floor", 1.5, 1, 0}, {"at original upper bound", 3.5, 1, 0}, {"above original upper bound", 3.501, 0, 0}} {
		t.Run(item.name, func(t *testing.T) {
			bars, request := confirmationFixture()
			request.EntryConfirmation, request.EntryVolumeFloor = "", 1.5
			bars[60].Volume = item.ratio * 100
			run, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
			if err != nil || len(run.Trades) != item.trades || len(run.VolumeBlockedEntries) != item.blocks {
				t.Fatalf("unexpected trades=%d blocks=%d: %v", len(run.Trades), len(run.VolumeBlockedEntries), err)
			}
			if item.blocks > 0 {
				block := run.VolumeBlockedEntries[0]
				if block.SignalDate != bars[60].Date || block.VolumeRatio != item.ratio || block.Minimum != 1.5 {
					t.Fatalf("wrong rejection evidence: %+v", block)
				}
			}
			if request.Technical.VolumeRatioMin != 1.2 {
				t.Fatal("original volume parameter changed")
			}
		})
	}
}

func TestEntryVolumeFloorDoesNotUseFutureVolumeOrReviveRejectedSignal(t *testing.T) {
	bars, request := confirmationFixture()
	request.EntryConfirmation, request.EntryVolumeFloor, request.NoFutureData = "", 1.5, true
	bars[60].Volume, bars[61].Volume = 120, 100000
	filtered, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(filtered.Trades) != 0 || len(filtered.OpenPositions) != 0 || len(filtered.VolumeBlockedEntries) != 1 {
		t.Fatalf("future volume revived rejected signal: %v", err)
	}
	request.EntryVolumeFloor = 0
	baseline, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(baseline.Trades) != 1 || baseline.Trades[0].Entry.Date != bars[61].Date || len(baseline.VolumeBlockedEntries) != 0 {
		t.Fatal("default next-open behavior changed")
	}
	request.EntryVolumeFloor, bars[60].Volume = 1.5, 150
	a, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(a.Trades) != 1 {
		t.Fatal("allowed signal did not enter")
	}
	bars[61].Volume = 1
	b, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(b.Trades) != 1 || !reflect.DeepEqual(a.Trades[0].Entry, b.Trades[0].Entry) || !reflect.DeepEqual(a.Trades[0].Exit, b.Trades[0].Exit) {
		t.Fatal("later volume changed fill timing or exit")
	}
}

func TestEntryVolumeFloorPreservesOnePriceDeferralAndT1(t *testing.T) {
	bars, request := confirmationFixture()
	request.EntryConfirmation, request.EntryVolumeFloor = "", 1.5
	bars[60].Volume = 150
	bars[61].Open, bars[61].Close, bars[61].High, bars[61].Low, bars[61].Volume = 11, 11, 11, 11, 1
	run, err := NewDailyEngine(historyRangeMock{bars: bars}).Run(context.Background(), request)
	if err != nil || len(run.Trades) != 1 || run.Trades[0].Entry.Date != bars[62].Date || run.Trades[0].Exit.Date != bars[63].Date || len(run.VolumeBlockedEntries) != 0 {
		t.Fatalf("one-price delay or T+1 changed: %v", err)
	}
}

func TestEntryVolumeFloorValidatesFiniteNonnegativeReclaimOnly(t *testing.T) {
	_, request := confirmationFixture()
	request.EntryConfirmation = ""
	for _, floor := range []float64{-1, math.NaN(), math.Inf(1)} {
		request.EntryVolumeFloor = floor
		if validateRequest(request) == nil {
			t.Fatal("invalid floor accepted")
		}
	}
	request.EntryVolumeFloor, request.Technical.EntryMode = 1.5, EntryModeBreakout
	if validateRequest(request) == nil {
		t.Fatal("floor applied to unsupported mode")
	}
	if entryVolumeAllowed(SignalSnapshot{VolumeRatio: math.NaN()}, 1.5) {
		t.Fatal("non-finite signal accepted")
	}
}
