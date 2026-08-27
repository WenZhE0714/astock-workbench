package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

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
	if err := os.MkdirAll(filepath.Dir(store.file), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(store.file+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	existing, err := store.loadUnlocked()
	if err != nil {
		return err
	}
	if !shadowReportCanReplace(existing, report) {
		return fmt.Errorf("影子账本拒绝回退写入：现有报告 %s/%s/%s 更新于 %s", existing.EngineVersion, existing.AsOf, existing.CheckpointPhase, existing.LastRealtimeAt)
	}
	return atomicWrite(store.file, append(data, '\n'), 0o600)
}

func (store *ShadowStore) Load() (paper.Report, error) {
	if store == nil || store.file == "" {
		return paper.Report{}, fmt.Errorf("影子报告文件未初始化")
	}
	return store.loadUnlocked()
}

func (store *ShadowStore) loadUnlocked() (paper.Report, error) {
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

func shadowReportCanReplace(existing, incoming paper.Report) bool {
	if existing.AsOf == "" && len(existing.Orders) == 0 && len(existing.Positions) == 0 && len(existing.Trades) == 0 {
		return true
	}
	if shadowEngineRank(incoming.EngineVersion) < shadowEngineRank(existing.EngineVersion) {
		return false
	}
	if incoming.AsOf < existing.AsOf {
		return false
	}
	if incoming.AsOf > existing.AsOf {
		return true
	}
	if shadowPhaseRank(incoming.CheckpointPhase) < shadowPhaseRank(existing.CheckpointPhase) {
		return false
	}
	if incoming.CheckpointPhase == existing.CheckpointPhase && existing.CheckpointPhase == paper.CheckpointOpen {
		existingWatermark := existing.LastRealtimeAt
		incomingWatermark := incoming.LastRealtimeAt
		if existingWatermark != "" && incomingWatermark == "" {
			return false
		}
		if existingWatermark != "" && incomingWatermark != "" && incomingWatermark < existingWatermark {
			return false
		}
		if incomingWatermark == existingWatermark && shadowLedgerSize(incoming) < shadowLedgerSize(existing) {
			return false
		}
	}
	if incoming.CheckpointPhase == existing.CheckpointPhase && !existing.GeneratedAt.IsZero() && !incoming.GeneratedAt.IsZero() && incoming.GeneratedAt.Before(existing.GeneratedAt) {
		return false
	}
	return true
}

func shadowLedgerSize(report paper.Report) int {
	return len(report.Orders) + len(report.Trades) + len(report.Rejections) + len(report.Positions)
}

func shadowPhaseRank(phase string) int {
	if strings.EqualFold(strings.TrimSpace(phase), paper.CheckpointClose) {
		return 2
	}
	if strings.EqualFold(strings.TrimSpace(phase), paper.CheckpointOpen) {
		return 1
	}
	return 0
}

func shadowEngineRank(version string) int {
	value := strings.TrimSpace(version)
	if index := strings.LastIndex(value, "-v"); index >= 0 {
		if rank, err := strconv.Atoi(value[index+2:]); err == nil {
			return rank
		}
	}
	return 0
}
