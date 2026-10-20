package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"syscall"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type PriceAlertStore struct{ file string }

func NewPriceAlertStore(file string) *PriceAlertStore { return &PriceAlertStore{file: file} }

func (s *PriceAlertStore) Load() (domain.PriceAlertState, error) {
	data, err := os.ReadFile(s.file)
	if errors.Is(err, os.ErrNotExist) {
		return domain.PriceAlertState{Version: 1, Alerts: map[string]domain.PriceAlert{}}, nil
	}
	if err != nil {
		return domain.PriceAlertState{}, err
	}
	var state domain.PriceAlertState
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, validatePriceAlertState(state)
}

func validatePriceAlertState(state domain.PriceAlertState) error {
	if state.Version != 1 || state.Alerts == nil {
		return fmt.Errorf("价位提醒存储版本无效")
	}
	for id, alert := range state.Alerts {
		if id != alert.ID {
			return fmt.Errorf("价位提醒标识不一致")
		}
		if err := strategy.ValidatePriceAlert(alert); err != nil {
			return err
		}
	}
	return nil
}

func (s *PriceAlertStore) lock(suffix string, nonblocking bool) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(s.file), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(s.file+suffix, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	mode := syscall.LOCK_EX
	if nonblocking {
		mode |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(file.Fd()), mode); err != nil {
		file.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(file.Fd()), syscall.LOCK_UN); file.Close() }, nil
}

func (s *PriceAlertStore) AcquireWorker() (func(), error) { return s.lock(".worker.lock", true) }

// Polling and controls share a cross-process lock. Network and desktop calls
// happen outside this transaction; trigger events must be durable before offer.
func (s *PriceAlertStore) Update(change func(*domain.PriceAlertState) error) (domain.PriceAlertState, error) {
	unlock, err := s.lock(".lock", false)
	if err != nil {
		return domain.PriceAlertState{}, err
	}
	defer unlock()
	current, err := s.Load()
	if err != nil {
		return current, err
	}
	data, err := json.Marshal(current)
	if err != nil {
		return current, err
	}
	var next domain.PriceAlertState
	if err := json.Unmarshal(data, &next); err != nil {
		return current, err
	}
	if err := change(&next); err != nil {
		return current, err
	}
	if err := validatePriceAlertState(next); err != nil {
		return current, err
	}
	for id, old := range current.Alerts {
		item, exists := next.Alerts[id]
		if !exists || !reflect.DeepEqual(item.Rule, old.Rule) || !item.CreatedAt.Equal(old.CreatedAt) || item.Revision < old.Revision || item.UpdatedAt.Before(old.UpdatedAt) || item.LastCheckedAt.Before(old.LastCheckedAt) || old.Event != nil && !reflect.DeepEqual(old.Event, item.Event) || !old.DeletedAt.IsZero() && !item.DeletedAt.Equal(old.DeletedAt) {
			return current, fmt.Errorf("不能改写价位规则或已触发事件")
		}
	}
	if reflect.DeepEqual(current, next) {
		return current, nil
	}
	data, err = json.MarshalIndent(next, "", "  ")
	if err != nil {
		return current, err
	}
	if err := atomicWrite(s.file, append(data, '\n'), 0600); err != nil {
		return current, err
	}
	return next, nil
}
