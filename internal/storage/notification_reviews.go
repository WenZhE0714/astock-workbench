package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type NotificationReview struct {
	ReadAt    time.Time `json:"read_at,omitempty"`
	HandledAt time.Time `json:"handled_at,omitempty"`
	Note      string    `json:"note,omitempty"`
}
type NotificationSymbolPreference struct {
	Followed   bool      `json:"followed"`
	MutedUntil time.Time `json:"muted_until,omitempty"`
}
type NotificationReviews struct {
	Version int                                     `json:"version"`
	Items   map[string]NotificationReview           `json:"items"`
	Symbols map[string]NotificationSymbolPreference `json:"symbols"`
}
type NotificationReviewStore struct{ file string }

func NewNotificationReviewStore(file string) *NotificationReviewStore {
	return &NotificationReviewStore{file: file}
}
func (s *NotificationReviewStore) Load() (NotificationReviews, error) {
	state := NotificationReviews{Version: 1, Items: map[string]NotificationReview{}, Symbols: map[string]NotificationSymbolPreference{}}
	data, err := os.ReadFile(s.file)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err = json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if state.Version != 1 {
		return state, fmt.Errorf("通知处理记录版本无效")
	}
	if state.Items == nil {
		state.Items = map[string]NotificationReview{}
	}
	if state.Symbols == nil {
		state.Symbols = map[string]NotificationSymbolPreference{}
	}
	return state, nil
}
func (s *NotificationReviewStore) Update(change func(*NotificationReviews)) error {
	if err := os.MkdirAll(filepath.Dir(s.file), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(s.file+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	state, err := s.Load()
	if err != nil {
		return err
	}
	change(&state)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(s.file, append(data, '\n'), 0o600)
}
