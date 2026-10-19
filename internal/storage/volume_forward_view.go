package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

type VolumeForwardStatus struct {
	CheckedAt  time.Time `json:"checked_at"`
	Status     string    `json:"status"`
	Detail     string    `json:"detail"`
	SealedDays int       `json:"sealed_days"`
}

// ReadStudy verifies that the displayed evidence actually produced the frozen
// plan. It does not run the engine or update the forward experiment.
func (s *VolumeForwardStore) ReadStudy(plan backtest.VolumeForwardPlan) (backtest.VolumeStudyResult, error) {
	var study backtest.VolumeStudyResult
	data, err := os.ReadFile(filepath.Join(s.root, "source-result.json"))
	if err != nil {
		return study, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != plan.SourceSHA256 {
		return study, fmt.Errorf("源研究指纹变化")
	}
	if err = json.Unmarshal(data, &study); err != nil {
		return study, err
	}
	rebuilt, err := backtest.NewVolumeForwardPlan(study, plan.SourceSHA256, plan.ExecutableSHA256, plan.CreatedAt)
	if err != nil {
		return study, err
	}
	if rebuilt.Fingerprint != plan.Fingerprint {
		return study, fmt.Errorf("冻结参数与源研究不一致")
	}
	return study, nil
}

func (s *VolumeForwardStore) ReadStatus() (VolumeForwardStatus, error) {
	var status VolumeForwardStatus
	data, err := os.ReadFile(filepath.Join(s.root, "status.json"))
	if errors.Is(err, os.ErrNotExist) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	err = json.Unmarshal(data, &status)
	return status, err
}

func (s *VolumeForwardStore) Exists() error {
	_, err := os.Stat(filepath.Join(s.root, "config.json"))
	return err
}
