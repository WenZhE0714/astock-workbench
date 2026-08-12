package storage

import (
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestStockReportStoreSavesPerSymbolAndLoadsLatest(t *testing.T) {
	store := NewStockReportStore(t.TempDir())
	for index, markdown := range []string{"# first", "# second"} {
		_, err := store.Save(domain.GeneratedStockReport{
			GeneratedAt: time.Date(2026, 8, 11, 15, 30+index, 0, 0, time.Local),
			Symbol:      "sh600519", Name: "贵州茅台", AIUsed: index == 1, Markdown: markdown,
			Facts: domain.StockReportFacts{SchemaVersion: 1},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	latest, err := store.LoadLatest("sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Markdown != "# second\n" || !latest.AIUsed || latest.Name != "贵州茅台" || latest.Facts.SchemaVersion != 1 {
		t.Fatalf("unexpected latest report: %#v", latest)
	}
}
