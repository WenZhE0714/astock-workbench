package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

type VolumeForwardStore struct{ root string }

func NewVolumeForwardStore(root string) *VolumeForwardStore {
	return &VolumeForwardStore{root: root}
}

func (s *VolumeForwardStore) Acquire() (func(), error) {
	return NewPatternBatchStore(s.root).Acquire()
}

func (s *VolumeForwardStore) Initialize(plan backtest.VolumeForwardPlan, source []byte) error {
	if err := backtest.ValidateVolumeForwardPlan(plan); err != nil {
		return err
	}
	sum := sha256.Sum256(source)
	if hex.EncodeToString(sum[:]) != plan.SourceSHA256 {
		return fmt.Errorf("源研究文件指纹不一致")
	}
	if _, err := os.Stat(filepath.Join(s.root, "config.json")); !os.IsNotExist(err) {
		return fmt.Errorf("前瞻配置已存在或不可读，禁止覆盖")
	}
	if err := atomicWrite(filepath.Join(s.root, "source-result.json"), source, 0o600); err != nil {
		return err
	}
	return s.write("config.json", plan)
}

func (s *VolumeForwardStore) write(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.root, name), data, 0o600)
}

func (s *VolumeForwardStore) Load() (backtest.VolumeForwardPlan, *backtest.VolumeForwardDay, int, error) {
	var plan backtest.VolumeForwardPlan
	data, err := os.ReadFile(filepath.Join(s.root, "config.json"))
	if err != nil {
		return plan, nil, 0, err
	}
	if err = json.Unmarshal(data, &plan); err != nil {
		return plan, nil, 0, err
	}
	if err = backtest.ValidateVolumeForwardPlan(plan); err != nil {
		return plan, nil, 0, err
	}
	source, err := os.ReadFile(filepath.Join(s.root, "source-result.json"))
	if err != nil {
		return plan, nil, 0, err
	}
	sum := sha256.Sum256(source)
	if hex.EncodeToString(sum[:]) != plan.SourceSHA256 {
		return plan, nil, 0, fmt.Errorf("源研究指纹变化")
	}
	entries, err := os.ReadDir(filepath.Join(s.root, "days"))
	if err != nil && !os.IsNotExist(err) {
		return plan, nil, 0, err
	}
	var latest *backtest.VolumeForwardDay
	count, previousHash := 0, ""
	seenHashes := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.root, "days", entry.Name()))
		if err != nil {
			return plan, nil, count, err
		}
		var day backtest.VolumeForwardDay
		if err = json.Unmarshal(data, &day); err != nil {
			return plan, nil, count, err
		}
		if err = backtest.ValidateVolumeForwardDay(plan, day); err != nil {
			return plan, nil, count, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if entry.Name() != day.Date+".json" || day.PreviousHash != previousHash || len(day.Runs[0].Equity) != count+1 {
			return plan, nil, count, fmt.Errorf("前瞻封存链断裂: %s", entry.Name())
		}
		count++
		previousHash, latest = day.Fingerprint, &day
		seenHashes[day.Fingerprint] = true
	}
	var head struct {
		Fingerprint string `json:"fingerprint"`
	}
	data, err = os.ReadFile(filepath.Join(s.root, "head.json"))
	if err == nil {
		if err = json.Unmarshal(data, &head); err != nil || !seenHashes[head.Fingerprint] {
			return plan, nil, count, fmt.Errorf("前瞻封存末端缺失或head损坏")
		}
	} else if !os.IsNotExist(err) {
		return plan, nil, count, err
	}
	return plan, latest, count, nil
}

// Append requires Acquire across load, collection and append. A complete day
// is committed atomically; an interrupted head update can be recovered by Load.
func (s *VolumeForwardStore) Append(plan backtest.VolumeForwardPlan, day backtest.VolumeForwardDay) error {
	if err := backtest.ValidateVolumeForwardDay(plan, day); err != nil {
		return err
	}
	stored, previous, _, err := s.Load()
	if err != nil {
		return err
	}
	if stored.Fingerprint != plan.Fingerprint || previous == nil && day.PreviousHash != "" || previous != nil && (day.PreviousHash != previous.Fingerprint || day.Date <= previous.Date) {
		return fmt.Errorf("前瞻追加基点不一致")
	}
	name := filepath.Join("days", day.Date+".json")
	if _, err := os.Stat(filepath.Join(s.root, name)); !os.IsNotExist(err) {
		return fmt.Errorf("该日档已存在或不可读，禁止覆盖")
	}
	if err := s.write(name, day); err != nil {
		return err
	}
	return s.write("head.json", map[string]string{"date": day.Date, "fingerprint": day.Fingerprint})
}

func (s *VolumeForwardStore) SaveReport(report string) error {
	return atomicWrite(filepath.Join(s.root, "report.md"), []byte(report), 0o600)
}

func (s *VolumeForwardStore) SaveStatus(value any) error {
	return s.write("status.json", value)
}
