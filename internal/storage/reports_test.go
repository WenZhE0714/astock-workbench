package storage

import (
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestReportRoundTrip(t *testing.T) {
	store := NewReportStore(t.TempDir())
	input := domain.AnalysisResult{
		SchemaVersion: 1,
		Status:        "ok",
		ID:            "20260807T120000Z-test",
		Ticker:        "600519",
		TradeDate:     "2026-08-07",
		CreatedAt:     "2026-08-07T20:00:00+08:00",
		Signal:        "Hold",
		Reports:       map[string]string{"market_report": "市场报告"},
	}
	saved, err := store.Save(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(saved.MarkdownPath, "report.md") {
		t.Fatalf("unexpected path: %s", saved.MarkdownPath)
	}
	output, _, err := store.LoadLatest("600519")
	if err != nil {
		t.Fatal(err)
	}
	if output.Signal != "Hold" || output.Reports["market_report"] != "市场报告" {
		t.Fatalf("unexpected report: %#v", output)
	}
}
