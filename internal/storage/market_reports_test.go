package storage

import (
	"os"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestMarketReportStoreSavesAndLoadsLatest(t *testing.T) {
	store := NewMarketReportStore(t.TempDir())
	firstAt := time.Date(2026, 8, 10, 15, 30, 0, 0, time.Local)
	first, err := store.Save(domain.GeneratedMarketReport{
		GeneratedAt: firstAt,
		AIUsed:      true, Markdown: "# first", Facts: domain.MarketScanFacts{SchemaVersion: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.MarkdownPath); err != nil {
		t.Fatal(err)
	}
	secondAt := time.Date(2026, 8, 11, 10, 1, 0, 0, time.Local)
	_, err = store.Save(domain.GeneratedMarketReport{
		GeneratedAt: secondAt,
		AIError:     "offline", Markdown: "# second", Facts: domain.MarketScanFacts{SchemaVersion: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	thirdAt := time.Date(2026, 8, 11, 15, 31, 0, 0, time.Local)
	_, err = store.Save(domain.GeneratedMarketReport{
		GeneratedAt: thirdAt,
		AIUsed:      true, Markdown: "# third", Facts: domain.MarketScanFacts{SchemaVersion: 1},
		Agents: []domain.AgentResearchRun{{Role: "risk-auditor", Status: "ok", FactsHash: "sha256:test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := store.LoadLatest()
	if err != nil {
		t.Fatal(err)
	}
	if latest.Markdown != "# third\n" || !latest.AIUsed || latest.Facts.SchemaVersion != 1 || len(latest.Agents) != 1 || latest.AgentsPath == "" || latest.EvidencePath == "" {
		t.Fatalf("unexpected latest report: %#v", latest)
	}
	if _, err := os.Stat(latest.EvidencePath); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || !items[0].GeneratedAt.Equal(thirdAt) || !items[1].GeneratedAt.Equal(secondAt) || !items[2].GeneratedAt.Equal(firstAt) {
		t.Fatalf("unexpected report index: %#v", items)
	}
	loaded, err := store.Load(items[2].ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Markdown != "# first\n" || !loaded.GeneratedAt.Equal(firstAt) {
		t.Fatalf("unexpected selected report: %#v", loaded)
	}
}
