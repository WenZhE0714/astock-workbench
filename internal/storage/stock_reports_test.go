package storage

import (
	"os"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestStockReportStoreSavesPerSymbolAndLoadsLatest(t *testing.T) {
	store := NewStockReportStore(t.TempDir())
	times := []time.Time{
		time.Date(2026, 8, 10, 15, 30, 0, 0, time.Local),
		time.Date(2026, 8, 11, 10, 1, 0, 0, time.Local),
		time.Date(2026, 8, 11, 15, 31, 0, 0, time.Local),
	}
	for index, markdown := range []string{"# first", "# second", "# third"} {
		agents := []domain.AgentResearchRun(nil)
		if index == 2 {
			agents = []domain.AgentResearchRun{{Role: "technical", Status: "ok", FactsHash: "sha256:test"}}
		}
		_, err := store.Save(domain.GeneratedStockReport{
			GeneratedAt: times[index], Symbol: "sh600519", Name: "贵州茅台",
			AIUsed: index == 2, Markdown: markdown,
			Facts: domain.StockReportFacts{SchemaVersion: 1}, Agents: agents,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	latest, err := store.LoadLatest("sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Markdown != "# third\n" || !latest.AIUsed || latest.Name != "贵州茅台" || latest.Facts.SchemaVersion != 1 || len(latest.Agents) != 1 || latest.AgentsPath == "" || latest.EvidencePath == "" {
		t.Fatalf("unexpected latest report: %#v", latest)
	}
	if _, err := os.Stat(latest.EvidencePath); err != nil {
		t.Fatal(err)
	}
	items, err := store.List("sh600519", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || !items[0].GeneratedAt.Equal(times[2]) || !items[2].GeneratedAt.Equal(times[0]) {
		t.Fatalf("unexpected stock report index: %#v", items)
	}
	loaded, err := store.Load("sh600519", items[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Markdown != "# second\n" || !loaded.GeneratedAt.Equal(times[1]) {
		t.Fatalf("unexpected selected stock report: %#v", loaded)
	}
	if _, err := store.Load("sz000001", items[1].ID); err == nil {
		t.Fatal("expected another stock to be unable to load this report ID")
	}
}
