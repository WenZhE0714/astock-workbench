package storage

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func (s *PatternForwardStore) Operations(id string) (domain.PatternForwardOperations, error) {
	v := domain.PatternForwardOperations{Version: 1, Active: []domain.PatternForwardIssue{}, Events: []domain.PatternForwardAlert{}}
	root, err := s.taskPath(id)
	if err != nil {
		return v, err
	}
	data, err := os.ReadFile(filepath.Join(root, "operations.json"))
	if os.IsNotExist(err) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(data, &v)
	return v, err
}

func (s *PatternForwardStore) UpdateOperations(id string, update func(domain.PatternForwardOperations) (domain.PatternForwardOperations, error)) (domain.PatternForwardOperations, error) {
	if _, err := s.Load(id); err != nil {
		return domain.PatternForwardOperations{}, err
	}
	unlock, err := NewPatternBatchStore(s.root).lock("operations-" + id)
	if err != nil {
		return domain.PatternForwardOperations{}, err
	}
	defer unlock()
	value, err := s.Operations(id)
	if err != nil {
		return value, err
	}
	value, err = update(value)
	if err != nil {
		return value, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value, err
	}
	root, _ := s.taskPath(id)
	return value, atomicWrite(filepath.Join(root, "operations.json"), data, 0o600)
}
