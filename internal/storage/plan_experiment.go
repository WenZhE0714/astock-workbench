package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/wenzhe/astock-workbench/internal/paper"
)

type PlanExperimentStore struct{ file string }

func NewPlanExperimentStore(file string) *PlanExperimentStore {
	return &PlanExperimentStore{file: file}
}

func (store *PlanExperimentStore) Load() (paper.PlanExperiment, error) {
	if store == nil || store.file == "" {
		return paper.PlanExperiment{}, fmt.Errorf("实验账本路径未配置")
	}
	data, err := os.ReadFile(store.file)
	if errors.Is(err, os.ErrNotExist) {
		return paper.PlanExperiment{}, nil
	}
	if err != nil {
		return paper.PlanExperiment{}, err
	}
	var state paper.PlanExperiment
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, paper.ValidatePlanExperimentTransition(paper.PlanExperiment{}, state)
}

func (store *PlanExperimentStore) Update(change func(paper.PlanExperiment) (paper.PlanExperiment, error)) (paper.PlanExperiment, error) {
	if store == nil || store.file == "" {
		return paper.PlanExperiment{}, fmt.Errorf("实验账本路径未配置")
	}
	if err := os.MkdirAll(filepath.Dir(store.file), 0o700); err != nil {
		return paper.PlanExperiment{}, err
	}
	lock, err := os.OpenFile(store.file+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return paper.PlanExperiment{}, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return paper.PlanExperiment{}, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	previous, err := store.Load()
	if err != nil {
		return previous, err
	}
	data, err := json.Marshal(previous)
	if err != nil {
		return previous, err
	}
	var candidate paper.PlanExperiment
	if err := json.Unmarshal(data, &candidate); err != nil {
		return previous, err
	}
	next, err := change(candidate)
	if err != nil {
		return previous, err
	}
	if next.Version == "" && previous.Version == "" {
		return previous, nil
	}
	if err := paper.ValidatePlanExperimentTransition(previous, next); err != nil {
		return previous, err
	}
	encoded, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return previous, err
	}
	if err := atomicWrite(store.file, append(encoded, '\n'), 0o600); err != nil {
		return previous, err
	}
	return next, nil
}
