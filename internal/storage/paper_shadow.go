package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/wenzhe/astock-workbench/internal/paper"
)

type ShadowStore struct{ file string }

func NewShadowStore(file string) *ShadowStore { return &ShadowStore{file: file} }

func (store *ShadowStore) Save(report paper.Report) error {
	if store == nil || store.file == "" {
		return fmt.Errorf("影子报告文件未初始化")
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(store.file, append(data, '\n'), 0o600)
}

func (store *ShadowStore) Load() (paper.Report, error) {
	if store == nil || store.file == "" {
		return paper.Report{}, fmt.Errorf("影子报告文件未初始化")
	}
	data, err := os.ReadFile(store.file)
	if errors.Is(err, os.ErrNotExist) {
		return paper.Report{}, nil
	}
	if err != nil {
		return paper.Report{}, err
	}
	var report paper.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return paper.Report{}, err
	}
	return report, nil
}
