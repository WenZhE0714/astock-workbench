package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type BoardSnapshotStore struct{ root string }

func NewBoardSnapshotStore(root string) *BoardSnapshotStore { return &BoardSnapshotStore{root: root} }

func validBoardDate(date string) bool {
	parsed, err := time.Parse(time.DateOnly, date)
	return err == nil && parsed.Format(time.DateOnly) == date
}

func (store *BoardSnapshotStore) Save(snapshot domain.BoardSnapshot) error {
	if snapshot.Version != 1 || !validBoardDate(snapshot.Date) || snapshot.CapturedAt.IsZero() || len(snapshot.Items) == 0 {
		return fmt.Errorf("板块快照内容无效")
	}
	local := snapshot.CapturedAt.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	if local.Format(time.DateOnly) != snapshot.Date || snapshot.ID != local.Format("1504") {
		return fmt.Errorf("板块快照日期或分钟标识不一致")
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	directory := filepath.Join(store.root, snapshot.Date)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	err = immutableWrite(filepath.Join(directory, snapshot.ID+".json"), append(data, '\n'))
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	return err
}

func (store *BoardSnapshotStore) Dates() ([]string, error) {
	entries, err := os.ReadDir(store.root)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	dates := []string{}
	for _, entry := range entries {
		if entry.IsDir() && validBoardDate(entry.Name()) {
			dates = append(dates, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	return dates, nil
}

func (store *BoardSnapshotStore) Day(date string) ([]domain.BoardSnapshot, error) {
	if !validBoardDate(date) {
		return nil, fmt.Errorf("快照日期无效")
	}
	entries, err := os.ReadDir(filepath.Join(store.root, date))
	if errors.Is(err, os.ErrNotExist) {
		return []domain.BoardSnapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []domain.BoardSnapshot{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(store.root, date, entry.Name()))
		if err != nil {
			return nil, err
		}
		var snapshot domain.BoardSnapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return nil, fmt.Errorf("读取板块快照 %s: %w", entry.Name(), err)
		}
		if snapshot.Version != 1 || snapshot.Date != date || snapshot.ID+".json" != entry.Name() || snapshot.CapturedAt.IsZero() {
			return nil, fmt.Errorf("板块快照标识校验失败")
		}
		result = append(result, snapshot)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CapturedAt.Before(result[j].CapturedAt) })
	return result, nil
}
