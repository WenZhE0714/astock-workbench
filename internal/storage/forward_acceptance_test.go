package storage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

func TestForwardAcceptanceReaderBindsAndValidatesEvidence(t *testing.T) {
	now := time.Date(2026, 10, 23, 16, 0, 0, 0, time.FixedZone("CST", 8*3600))
	plan := backtest.VolumeForwardPlan{Fingerprint: "frozen-plan", CreatedAt: now.AddDate(0, 0, -4), Baseline: backtest.Request{Start: now.AddDate(0, 0, -3)}}
	archive, root := t.TempDir(), t.TempDir()
	valid := func() ForwardAcceptanceReport {
		return ForwardAcceptanceReport{CheckedAt: now, Status: "passed", Archive: archive, PlanFingerprint: plan.Fingerprint, FirstDate: "2026-10-20", FirstFingerprint: "first-day", SealedDays: 4,
			Execution: &ForwardExecutionAcceptance{Status: "partial", VerifiedThrough: "2026-10-23", Transitions: 3, Accounts: []ForwardAccountExecution{
				{Account: "原规则", FirstBuy: "2026-10-21", FirstSell: "2026-10-22", Buys: 1, Sells: 1}, {Account: "量比1.5"}, {Account: "原规则双倍成本"}, {Account: "量比1.5双倍成本"},
			}}}
	}
	cases := []struct {
		name   string
		mutate func(*ForwardAcceptanceReport)
		valid  bool
	}{
		{"valid", func(r *ForwardAcceptanceReport) {}, true},
		{"legacy", func(r *ForwardAcceptanceReport) { r.Execution = nil }, true},
		{"wrong-plan", func(r *ForwardAcceptanceReport) { r.PlanFingerprint = "other" }, false},
		{"wrong-archive", func(r *ForwardAcceptanceReport) { r.Archive = root }, false},
		{"future-report", func(r *ForwardAcceptanceReport) { r.CheckedAt = now.Add(time.Hour) }, false},
		{"promotion", func(r *ForwardAcceptanceReport) { r.PromotionAllowed = true }, false},
		{"no-first-proof", func(r *ForwardAcceptanceReport) { r.FirstFingerprint = "" }, false},
		{"bad-date", func(r *ForwardAcceptanceReport) { r.FirstDate = "2026-02-30" }, false},
		{"future-fill", func(r *ForwardAcceptanceReport) { r.Execution.Accounts[0].FirstSell = "2026-10-24" }, false},
		{"same-day-sell", func(r *ForwardAcceptanceReport) { r.Execution.Accounts[0].FirstSell = "2026-10-21" }, false},
		{"duplicate-account", func(r *ForwardAcceptanceReport) { r.Execution.Accounts[1].Account = "原规则" }, false},
		{"missing-account", func(r *ForwardAcceptanceReport) { r.Execution.Accounts = r.Execution.Accounts[:3] }, false},
		{"false-passed", func(r *ForwardAcceptanceReport) { r.Execution.Status = "passed" }, false},
		{"negative-count", func(r *ForwardAcceptanceReport) { r.Execution.Accounts[0].Buys = -1 }, false},
		{"missing-fill-date", func(r *ForwardAcceptanceReport) { r.Execution.Accounts[0].FirstBuy = "" }, false},
		{"unverified-fill", func(r *ForwardAcceptanceReport) { r.Execution.VerifiedThrough = "2026-10-21" }, false},
		{"too-many-transitions", func(r *ForwardAcceptanceReport) { r.Execution.Transitions = 4 }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := valid()
			tc.mutate(&report)
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "status.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := NewForwardAcceptanceStore(root).Read(plan, archive, now)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if tc.name == "legacy" && got.Execution != nil {
				t.Fatal("invented execution evidence")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, after) {
				t.Fatal("reader changed report")
			}
		})
	}
	if err := os.WriteFile(filepath.Join(root, "status.json"), []byte(`{"status":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewForwardAcceptanceStore(root).Read(plan, archive, now); err == nil {
		t.Fatal("malformed JSON accepted")
	}
	missing := filepath.Join(root, "missing")
	if _, err := NewForwardAcceptanceStore(missing).Read(plan, archive, now); err == nil {
		t.Fatal("missing report accepted")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("reader created missing directory")
	}
}
