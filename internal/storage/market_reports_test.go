package storage

import (
	"os"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestMarketReportStoreSavesAndLoadsLatest(t *testing.T) {
	store := NewMarketReportStore(t.TempDir())
	first, err := store.Save(domain.GeneratedMarketReport{
		GeneratedAt: time.Date(2026, 8, 11, 15, 30, 0, 0, time.Local),
		AIUsed:      true, Markdown: "# first", Facts: domain.MarketScanFacts{SchemaVersion: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.MarkdownPath); err != nil {
		t.Fatal(err)
	}
	_, err = store.Save(domain.GeneratedMarketReport{
		GeneratedAt: time.Date(2026, 8, 11, 15, 31, 0, 0, time.Local),
		AIError:     "offline", Markdown: "# second", Facts: domain.MarketScanFacts{SchemaVersion: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := store.LoadLatest()
	if err != nil {
		t.Fatal(err)
	}
	if latest.Markdown != "# second\n" || latest.AIUsed || latest.AIError != "offline" || latest.Facts.SchemaVersion != 1 {
		t.Fatalf("unexpected latest report: %#v", latest)
	}
}
