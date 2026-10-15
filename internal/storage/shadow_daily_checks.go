package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/wenzhe/astock-workbench/internal/paper"
)

// Checks are replaceable observations; sealed account snapshots stay immutable.
func (store *ShadowDailyReviewStore) SaveCheck(check paper.ShadowDailyCheck) error {
	if !validReviewDate(check.Date) || check.CheckedAt.IsZero() || check.Date > check.CheckedAt.In(shadowReviewLocation).Format("2006-01-02") {
		return fmt.Errorf("每日检查日期或时点无效")
	}
	dir := filepath.Join(store.root, "checks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, check.Date+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	path := filepath.Join(dir, check.Date+".json")
	data, err := os.ReadFile(path)
	if err == nil {
		var previous paper.ShadowDailyCheck
		if err := json.Unmarshal(data, &previous); err != nil {
			return err
		}
		if previous.CheckedAt.After(check.CheckedAt) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err = json.MarshalIndent(check, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0o600)
}
