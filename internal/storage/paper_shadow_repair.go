package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
)

type ShadowRepairResult struct {
	File        string                   `json:"file"`
	Applied     bool                     `json:"applied"`
	BeforeHash  string                   `json:"before_hash"`
	AfterHash   string                   `json:"after_hash"`
	Archive     string                   `json:"archive,omitempty"`
	CheckedAt   time.Time                `json:"checked_at"`
	Corrections []paper.LedgerCorrection `json:"corrections"`
}

func (store *ShadowStore) Repair(calendar []string, now time.Time, apply bool) (result ShadowRepairResult, err error) {
	result.File, result.CheckedAt = store.file, now
	if apply {
		lock, err := os.OpenFile(store.file+".lock", os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return result, err
		}
		defer lock.Close()
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
			return result, err
		}
		defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	}
	before, err := os.ReadFile(store.file)
	if err != nil {
		return result, err
	}
	var previous paper.Report
	if err := json.Unmarshal(before, &previous); err != nil {
		return result, err
	}
	next, changes, err := paper.RepairShadowLedger(previous, calendar)
	result.BeforeHash, result.Corrections = shadowBytesHash(before), changes
	if err != nil {
		return result, err
	}
	if len(changes) == 0 {
		result.AfterHash = result.BeforeHash
		return result, nil
	}
	after, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return result, err
	}
	after = append(after, '\n')
	result.AfterHash = shadowBytesHash(after)
	result.Archive = filepath.Join(store.file+".repairs", result.BeforeHash)
	if !apply {
		return result, nil
	}
	// Publish evidence before the replacement. An interrupted repair can be
	// inspected by comparing the active file with these before/after hashes.
	for name, data := range map[string][]byte{"before.json": before, "after.json": after} {
		path := filepath.Join(result.Archive, name)
		if err := immutableWrite(path, data); err != nil {
			if !os.IsExist(err) {
				return result, err
			}
			existing, readErr := os.ReadFile(path)
			if readErr != nil || shadowBytesHash(existing) != shadowBytesHash(data) {
				return result, fmt.Errorf("已有校正档案不同，保留现场: %s", path)
			}
		}
	}
	record, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return result, err
	}
	if err := atomicWrite(filepath.Join(result.Archive, "record.json"), append(record, '\n'), 0o600); err != nil {
		return result, err
	}
	if err := atomicWrite(store.file, after, 0o600); err != nil {
		return result, err
	}
	result.Applied = true
	record, err = json.MarshalIndent(result, "", "  ")
	if err != nil {
		return result, err
	}
	return result, atomicWrite(filepath.Join(result.Archive, "record.json"), append(record, '\n'), 0o600)
}

func shadowBytesHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
