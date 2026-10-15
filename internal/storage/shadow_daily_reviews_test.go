package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

func TestRealtimeDayReadIsBoundedAndReportsIncompleteData(t *testing.T) {
	root := t.TempDir()
	store := NewRealtimeSignalStore(root)
	at := time.Date(2026, 10, 15, 10, 0, 0, 0, shadowReviewLocation)
	scan := realtime.ScanResult{GeneratedAt: at, MarketState: realtime.MarketStateTrading}
	if err := store.Append(scan); err != nil {
		t.Fatal(err)
	}
	read, err := store.WalkDay(context.Background(), "2026-10-15", func(realtime.ScanResult) {
		scan.GeneratedAt = at.Add(time.Minute)
		if err := store.Append(scan); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil || read.Records != 1 || !read.Available {
		t.Fatalf("unbounded read: %+v %v", read, err)
	}
	again, err := store.WalkDay(context.Background(), "2026-10-15", func(realtime.ScanResult) {})
	if err != nil || again.Records != 2 || again.Signature == read.Signature {
		t.Fatal("append not detected")
	}
	file, _ := os.OpenFile(filepath.Join(root, "2026-10-15.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if _, err := file.WriteString("invalid\n{\"generated_at\":"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	again, err = store.WalkDay(context.Background(), "2026-10-15", func(realtime.ScanResult) {})
	if err != nil || again.InvalidLines != 1 || !again.PartialTail || again.Records != 2 {
		t.Fatalf("quality not surfaced: %+v %v", again, err)
	}
	missing, err := store.WalkDay(context.Background(), "2026-10-14", func(realtime.ScanResult) {})
	if err != nil || missing.Available {
		t.Fatal("missing file treated as collected")
	}
	if _, err := store.DaySignature("../bad"); err == nil {
		t.Fatal("invalid path accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.WalkDay(ctx, "2026-10-15", func(realtime.ScanResult) {}); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestShadowDailySnapshotsSealAndDoNotBackfill(t *testing.T) {
	root := t.TempDir()
	store := NewShadowDailyReviewStore(root)
	at := time.Date(2026, 10, 15, 14, 0, 0, 0, shadowReviewLocation)
	cfg := paper.DefaultConfig()
	report := paper.Report{AsOf: "2026-10-15", GeneratedAt: at, EngineVersion: paper.ShadowEngineVersion, Config: cfg, InitialCash: 1_000_000, RemainingCash: 1_000_000, TotalEquity: 1_000_000, CheckpointPhase: paper.CheckpointOpen}
	if err := store.Capture("balanced", "", report, at); err != nil {
		t.Fatal(err)
	}
	day, err := store.Day(report.AsOf)
	if err != nil || day["balanced"].Sealed {
		t.Fatal("open snapshot was sealed")
	}
	closeAt := at.Add(2 * time.Hour)
	report.GeneratedAt, report.CheckpointPhase = closeAt, paper.CheckpointClose
	if err := store.Capture("balanced", "", report, closeAt); err != nil {
		t.Fatal(err)
	}
	day, _ = store.Day(report.AsOf)
	hash := day["balanced"].Hash
	report.GeneratedAt = closeAt.Add(time.Minute)
	if err := store.Capture("balanced", "", report, report.GeneratedAt); err != nil {
		t.Fatal(err)
	}
	day, _ = store.Day(report.AsOf)
	if !day["balanced"].Sealed || day["balanced"].Hash != hash {
		t.Fatal("sealed snapshot overwritten")
	}
	report.AsOf = "2026-10-14"
	if err := store.Capture("balanced", "", report, closeAt); err != nil {
		t.Fatal(err)
	}
	old, _ := store.Day("2026-10-14")
	if len(old) != 0 {
		t.Fatal("historical valuation fabricated")
	}
	report.AsOf, report.LedgerRevision = "2026-10-15", 1
	if err := store.Capture("balanced", "", report, report.GeneratedAt); err != nil {
		t.Fatal(err)
	}
	day, _ = store.Day(report.AsOf)
	if day["balanced"].Revision != 1 {
		t.Fatal("ledger correction ignored")
	}
	if _, err := os.Stat(filepath.Join(root, report.AsOf, "balanced-r0.json")); err != nil {
		t.Fatal("prior revision lost")
	}
	path := filepath.Join(root, report.AsOf, "balanced-r1.json")
	if err := os.WriteFile(path, []byte(`{"date":"2026-10-15","hash":"bad"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Day(report.AsOf); err == nil {
		t.Fatal("corrupt snapshot accepted")
	}
}
