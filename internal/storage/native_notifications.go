package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wenzhe/astock-workbench/internal/notify"
)

type NativeNotificationStore struct{ file string }

func NewNativeNotificationStore(file string) *NativeNotificationStore {
	return &NativeNotificationStore{file: file}
}

func (store *NativeNotificationStore) Load() (notify.State, error) {
	data, err := os.ReadFile(store.file)
	if errors.Is(err, os.ErrNotExist) {
		return notify.State{Version: 1}, nil
	}
	if err != nil {
		return notify.State{}, err
	}
	var state notify.State
	if err := json.Unmarshal(data, &state); err != nil {
		return notify.State{}, err
	}
	if state.Version != 1 {
		return notify.State{}, fmt.Errorf("不支持的通知记录版本: %d", state.Version)
	}
	return state, nil
}

func (store *NativeNotificationStore) Save(state notify.State) error {
	state.Version = 1
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(store.file), 0o700); err != nil {
		return err
	}
	return atomicWrite(store.file, append(data, '\n'), 0o600)
}
