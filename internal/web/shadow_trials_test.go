package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func TestShadowTrialsHavePairedControlsAndSinglePolicyChanges(t *testing.T) {
	now := time.Now().UTC()
	set := NewShadowTrialSet(now)
	byID := map[string]paper.ShadowTrial{}
	for _, trial := range set.Trials {
		byID[trial.ID] = trial
		if trial.Config.InitialCash != 1_000_000 || !trial.Config.TradingStartAt.Equal(now) {
			t.Fatal("unpaired funding or start")
		}
	}
	for _, trial := range set.Trials {
		if trial.BaselineID == "" {
			continue
		}
		baseline := byID[trial.BaselineID]
		if paper.ConfigFingerprint(trial.Config) == paper.ConfigFingerprint(baseline.Config) {
			t.Fatal("variant equals control")
		}
		cfg := trial.Config
		cfg.DisableSignalAdditions = false
		cfg.MaxEntryExtensionPercent = 0
		cfg.IntradayEntriesOnly = false
		if paper.ConfigFingerprint(cfg) != paper.ConfigFingerprint(baseline.Config) {
			t.Fatal("unintended policy change")
		}
	}
}

func TestShadowTrialsBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_SHADOW_TRIAL_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	s, _, clock, _, _ := forwardServerFixture(t)
	root := t.TempDir()
	baseline := storage.NewShadowStore(filepath.Join(root, "balanced.json"))
	WithShadowExecution(paper.NewEvaluator(nil), baseline)(s)
	set := NewShadowTrialSet(clock.Now())
	set.Hash = "fixture"
	WithShadowTrials(set, filepath.Join(root, "trials"))(s)
	for _, profile := range s.shadowProfiles {
		cfg := profile.Config
		report := paper.Report{EngineVersion: paper.ShadowEngineVersion, Config: cfg, ConfigFingerprint: paper.ConfigFingerprint(cfg), GeneratedAt: clock.Now(), AsOf: clock.Now().Format(time.DateOnly), CheckpointPhase: paper.CheckpointOpen, InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash, TotalEquity: cfg.InitialCash}
		if err := profile.Archive.Save(report); err != nil {
			t.Fatal(err)
		}
	}
	if err := http.ListenAndServe(address, s.Handler()); err != nil {
		t.Fatal(err)
	}
}
