package storage

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/realtime"
)

func TestOutcomeSignalStreamMatchesFullArchiveSelection(t *testing.T) {
	store := NewRealtimeSignalStore(t.TempDir())
	at := storageRealtimeTime(2026, 10, 14, 10, 0)
	for _, offset := range []time.Duration{0, time.Minute, -24 * time.Hour, 7 * time.Hour} {
		when := at.Add(offset)
		if err := store.Append(realtime.ScanResult{GeneratedAt: when, Signals: []realtime.Signal{{ID: when.String(), AsOf: when, Symbol: "sh600000", Score: 60}}}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := store.List(-1)
	if err != nil {
		t.Fatal(err)
	}
	var expected realtime.OutcomeSignalCollector
	expected.Add(all)
	got, err := store.ListOutcomeSignals(context.Background())
	if err != nil || !reflect.DeepEqual(got, expected.Signals(0)) || len(got) != 2 {
		t.Fatalf("stream changed selection: %+v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.ListOutcomeSignals(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("stream ignored cancellation: %v", err)
	}
}
