package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/wenzhe/astock-workbench/internal/paper"
)

var shadowTrialID = regexp.MustCompile(`^trial-[a-z-]+$`)

func SaveShadowTrials(root string, set paper.ShadowTrialSet) (paper.ShadowTrialSet, error) {
	set.Hash = ""
	data, err := json.Marshal(set)
	if err != nil {
		return set, err
	}
	set.Hash = shadowBytesHash(data)
	if err := validateShadowTrials(set); err != nil {
		return set, err
	}
	data, err = json.MarshalIndent(set, "", "  ")
	if err != nil {
		return set, err
	}
	return set, immutableWrite(filepath.Join(root, "config.json"), append(data, '\n'))
}

func LoadShadowTrials(root string) (paper.ShadowTrialSet, error) {
	var set paper.ShadowTrialSet
	data, err := os.ReadFile(filepath.Join(root, "config.json"))
	if os.IsNotExist(err) {
		return set, nil
	}
	if err != nil {
		return set, err
	}
	if err := json.Unmarshal(data, &set); err != nil {
		return set, err
	}
	return set, validateShadowTrials(set)
}

func validateShadowTrials(set paper.ShadowTrialSet) error {
	hash := set.Hash
	set.Hash = ""
	data, err := json.Marshal(set)
	if err != nil || hash != shadowBytesHash(data) || set.EngineVersion != paper.ShadowEngineVersion || set.Version != 1 || set.CreatedAt.IsZero() || len(set.Trials) != 6 {
		return fmt.Errorf("影子对照配置或指纹不合法")
	}
	byID := map[string]paper.ShadowTrial{}
	for _, trial := range set.Trials {
		if !shadowTrialID.MatchString(trial.ID) || trial.Config.InitialCash != 1_000_000 || !trial.Config.TradingStartAt.Equal(set.CreatedAt) {
			return fmt.Errorf("影子对照的身份、资金或启动时间不一致")
		}
		if _, duplicate := byID[trial.ID]; duplicate {
			return fmt.Errorf("影子对照ID重复")
		}
		byID[trial.ID] = trial
	}
	for _, trial := range set.Trials {
		if trial.BaselineID == "" {
			continue
		}
		baseline, ok := byID[trial.BaselineID]
		if !ok || baseline.BaselineID != "" {
			return fmt.Errorf("影子对照基线不存在")
		}
	}
	return nil
}
