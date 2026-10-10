package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type PatternValidationStore struct{ root string }

func NewPatternValidationStore(root string) *PatternValidationStore {
	return &PatternValidationStore{root: root}
}

func (s *PatternValidationStore) Save(report domain.PatternValidationReport) (domain.PatternValidationReport, error) {
	if report.Version == "" || report.GeneratedAt.IsZero() || report.InputHash == "" {
		return report, fmt.Errorf("形态验证归档缺少版本、时间或数据指纹")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return report, err
	}
	report.RunID = hex.EncodeToString(id[:])
	data, err := json.Marshal(report)
	if err != nil {
		return report, err
	}
	return report, atomicWrite(filepath.Join(s.root, report.RunID+".json"), data, 0o600)
}

func (s *PatternValidationStore) Load(id string) (domain.PatternValidationReport, error) {
	var report domain.PatternValidationReport
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 16 {
		return report, fmt.Errorf("无效形态验证归档ID")
	}
	data, err := os.ReadFile(filepath.Join(s.root, id+".json"))
	if err != nil {
		return report, err
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return report, err
	}
	if report.RunID != id {
		return report, fmt.Errorf("形态验证归档ID不匹配")
	}
	return report, nil
}

func (s *PatternValidationStore) List(limit int) ([]domain.PatternValidationRun, error) {
	items := []domain.PatternValidationRun{}
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := entry.Name()[:len(entry.Name())-5]
		report, err := s.Load(id)
		if err != nil {
			return nil, fmt.Errorf("读取形态验证归档 %s: %w", id, err)
		}
		items = append(items, domain.PatternValidationRun{RunID: id, GeneratedAt: report.GeneratedAt, Request: report.Request, Samples: len(report.Samples)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].GeneratedAt.After(items[j].GeneratedAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
