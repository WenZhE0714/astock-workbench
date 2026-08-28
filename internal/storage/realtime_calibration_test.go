package storage

import (
	"path/filepath"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/realtime"
)

func TestRealtimeCalibrationStoreRoundTripsAndBoundsHistory(t *testing.T) {
	store := NewRealtimeCalibrationStore(filepath.Join(t.TempDir(), "calibration-state.json"))
	state := RealtimeCalibrationState{Active: realtime.ScoreCalibration{
		ID: "CAL-1", MinimumScore: 61, ReadySamples: 48,
		ComponentWeights: map[string]float64{"relative-momentum": .6, "price-volume": .4},
	}, RejectedIDs: []string{"OLD", "OLD", "NEW"}}
	for index := 0; index < 25; index++ {
		state.History = append(state.History, RealtimeCalibrationRevision{ID: "CAL", Action: "applied"})
	}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != 1 || loaded.Active.ID != "CAL-1" || len(loaded.History) != 20 || len(loaded.RejectedIDs) != 2 {
		t.Fatalf("unexpected calibration state: %+v", loaded)
	}
	if loaded.Active.ComponentWeights["relative-momentum"] != .6 {
		t.Fatalf("weights were not preserved: %+v", loaded.Active.ComponentWeights)
	}
}

func TestRealtimeCalibrationStoreMissingFileIsEmpty(t *testing.T) {
	store := NewRealtimeCalibrationStore(filepath.Join(t.TempDir(), "missing.json"))
	state, err := store.Load()
	if err != nil || state.Active.ID != "" {
		t.Fatalf("missing calibration state should be empty: %+v / %v", state, err)
	}
}
