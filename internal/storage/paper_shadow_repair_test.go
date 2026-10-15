package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
)

func TestShadowRepairArchivesAndRejectsStaleWriters(t *testing.T) {
	file := filepath.Join(t.TempDir(), "shadow.json")
	store := NewShadowStore(file)
	cfg := paper.DefaultConfig()
	old := paper.Report{EngineVersion: "tplus1-v11", AsOf: "2026-08-28", CheckpointPhase: paper.CheckpointClose, Config: cfg, InitialCash: 1_000_000, RemainingCash: 999995, TotalEquity: 999995, TotalProfit: -5, TotalFees: 5, RealizedProfit: -1005, UnrealizedProfit: 1000, CompletedTrades: 1,
		Orders: []paper.ShadowOrder{{ID: "zero", Symbol: "sh600000", Side: "sell", Status: paper.OrderFilled, Price: 10}},
		Trades: []paper.ShadowTrade{{ID: "ghost", Symbol: "sh600000", EntryOrderID: "absent", ExitOrderID: "zero", NetProfit: -1005, TotalFee: 5, HoldingDays: 2, EntryDate: "2026-08-27", ExitDate: "2026-08-28"}},
	}
	if err := store.Save(old); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(file)
	now := time.Now()
	preview, err := store.Repair(nil, now, false)
	if err != nil || preview.Applied || len(preview.Corrections) == 0 {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	if _, err := os.Stat(file + ".repairs"); !os.IsNotExist(err) {
		t.Fatal("preview wrote archive")
	}
	result, err := store.Repair(nil, now, true)
	if err != nil || !result.Applied {
		t.Fatalf("apply: %+v %v", result, err)
	}
	archived, _ := os.ReadFile(filepath.Join(result.Archive, "before.json"))
	if string(archived) != string(before) {
		t.Fatal("original evidence differs")
	}
	next, _ := store.Load()
	if next.RemainingCash != 1_000_000 || next.LedgerRevision != 1 || len(paper.AuditShadowLedger(next)) != 0 {
		t.Fatalf("invalid corrected report: %+v", next)
	}
	old.AsOf = "2026-08-31"
	if err := store.Save(old); err == nil {
		t.Fatal("stale writer overwrote correction")
	}
	next.LedgerRevision = 0
	if err := store.Save(next); err == nil {
		t.Fatal("revision regressed")
	}
	result, err = store.Repair(nil, now.Add(time.Hour), true)
	if err != nil || result.Applied || len(result.Corrections) != 0 {
		t.Fatal("repair is not idempotent")
	}
}

func TestShadowTrialsImmutableAndFingerprintValidated(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	cfg := paper.DefaultConfig()
	cfg.TradingStartAt = now
	set := paper.ShadowTrialSet{EngineVersion: paper.ShadowEngineVersion, Version: 1, CreatedAt: now}
	for _, id := range []string{"trial-a", "trial-b", "trial-c", "trial-d", "trial-e", "trial-f"} {
		set.Trials = append(set.Trials, paper.ShadowTrial{ID: id, Config: cfg})
	}
	saved, err := SaveShadowTrials(root, set)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadShadowTrials(root)
	if err != nil || loaded.Hash != saved.Hash || !loaded.CreatedAt.Equal(now) {
		t.Fatal("trial config changed on reload")
	}
	if _, err := SaveShadowTrials(root, set); err == nil {
		t.Fatal("frozen trial overwritten")
	}
	loaded.Trials[0].Config.DisableSignalAdditions = true
	data, _ := json.Marshal(loaded)
	if err := os.WriteFile(filepath.Join(root, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadShadowTrials(root); err == nil {
		t.Fatal("config corruption accepted")
	}
}
