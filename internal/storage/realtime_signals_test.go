package storage

import (
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/realtime"
)

func TestRealtimeSignalStoreAppendsAndListsNewestFirst(t *testing.T) {
	store := NewRealtimeSignalStore(t.TempDir())
	first := storageRealtimeTime(2026, 8, 20, 10, 0)
	second := first.Add(time.Minute)
	if err := store.Append(realtime.ScanResult{GeneratedAt: first, Signals: []realtime.Signal{{ID: "first", AsOf: first}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(realtime.ScanResult{GeneratedAt: second, Signals: []realtime.Signal{{ID: "second", AsOf: second}}}); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "second" || items[1].ID != "first" {
		t.Fatalf("unexpected history: %+v", items)
	}
}

func TestRealtimeSignalStoreReturnsLatestCompleteSnapshot(t *testing.T) {
	store := NewRealtimeSignalStore(t.TempDir())
	first := storageRealtimeTime(2026, 8, 19, 15, 1)
	second := first.AddDate(0, 0, 1)
	afterHours := storageRealtimeTime(2026, 8, 20, 17, 41)
	if err := store.Append(realtime.ScanResult{GeneratedAt: first, Universe: "watchlist", Signals: []realtime.Signal{{ID: "first"}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(realtime.ScanResult{GeneratedAt: second, Universe: "watchlist+leaders", MarketState: realtime.MarketStateClosed, Signals: []realtime.Signal{{ID: "second"}}, Warnings: []string{"snapshot warning"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(realtime.ScanResult{GeneratedAt: afterHours, Universe: "invalid-after-hours", MarketState: realtime.MarketStateClosed, Signals: []realtime.Signal{{ID: "after-hours"}}}); err != nil {
		t.Fatal(err)
	}

	latest, err := store.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if !latest.GeneratedAt.Equal(second) || latest.Universe != "watchlist+leaders" || latest.MarketState != realtime.MarketStateClosed || len(latest.Signals) != 1 || latest.Signals[0].ID != "second" || len(latest.Warnings) != 1 {
		t.Fatalf("unexpected latest snapshot: %+v", latest)
	}
	items, err := store.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "second" || items[1].ID != "first" {
		t.Fatalf("invalid after-hours scan leaked into history: %+v", items)
	}
}

func storageRealtimeTime(year, month, day, hour, minute int) time.Time {
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
}

func TestRealtimeOutcomeStoreKeepsLatestRevisionPerKey(t *testing.T) {
	store := NewRealtimeOutcomeStore(t.TempDir())
	first := time.Date(2026, 8, 20, 16, 0, 0, 0, time.Local)
	second := first.AddDate(0, 0, 1)
	if err := store.Upsert([]realtime.SignalOutcome{{Key: "sh600519:2026-08-20:1", SignalID: "first", SignalAsOf: first, Horizon: 1, Status: realtime.OutcomePending, EvaluatedAt: first}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert([]realtime.SignalOutcome{{Key: "sh600519:2026-08-20:1", SignalID: "first", SignalAsOf: first, Horizon: 1, Status: realtime.OutcomeReady, ReturnPercent: 2.5, EvaluatedAt: second}}); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != realtime.OutcomeReady || items[0].ReturnPercent != 2.5 {
		t.Fatalf("unexpected latest outcome revision: %+v", items)
	}
}
