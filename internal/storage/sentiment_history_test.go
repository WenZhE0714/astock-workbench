package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestSentimentHistoryStoreRoundTrip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sentiment", "history.json")
	store := NewSentimentHistoryStore(file)
	want := []domain.MarketSentimentPoint{{At: time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC), Score: 63.5, Phase: "强势"}}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Score != want[0].Score || got[0].Phase != want[0].Phase {
		t.Fatalf("unexpected history: %#v", got)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("history file permissions: %o", info.Mode().Perm())
	}
}

func TestSentimentHistoryRetainsLegacyAndCurrentScoreModels(t *testing.T) {
	var points []domain.MarketSentimentPoint
	if err := json.Unmarshal([]byte(`[{"at":"2026-09-10T10:00:00+08:00","score":54,"phase":"修复","northbound_signal":0}]`), &points); err != nil {
		t.Fatal(err)
	}
	points = append(points, domain.MarketSentimentPoint{At: points[0].At.Add(time.Minute), Score: 60, Phase: "强势", ScoreModel: domain.MarketSentimentScoreModel})
	store := NewSentimentHistoryStore(filepath.Join(t.TempDir(), "history.json"))
	if err := store.Save(points); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Score != 54 || got[0].ScoreModel != "" || got[0].LegacyNorthboundSignal == nil || *got[0].LegacyNorthboundSignal != 0 {
		t.Fatalf("legacy score or retired factor changed: %+v", got)
	}
	if got[1].Score != 60 || got[1].ScoreModel != domain.MarketSentimentScoreModel || got[1].LegacyNorthboundSignal != nil {
		t.Fatalf("current score metadata was lost: %+v", got[1])
	}
}
