package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

var marketChatRecordID = regexp.MustCompile(`^[0-9]{19}-[0-9a-f]{12}$`)

func (store *AIChatStore) marketRecordPath(id string) (string, error) {
	if !marketChatRecordID.MatchString(id) {
		return "", fmt.Errorf("无效的大盘判断记录编号")
	}
	return filepath.Join(store.root, "market", "assessments", id+".json"), nil
}

// Exclusive creation prevents a later answer from rewriting an earlier forecast.
func (store *AIChatStore) SaveMarketRecord(record domain.MarketChatRecord) error {
	path, err := store.marketRecordPath(record.Assessment.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(data, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func (store *AIChatStore) LoadMarketRecord(id string) (domain.MarketChatRecord, error) {
	var record domain.MarketChatRecord
	path, err := store.marketRecordPath(id)
	if err != nil {
		return record, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	if record.SchemaVersion != 1 || record.Assessment.ID != id {
		return record, fmt.Errorf("大盘判断记录格式无效")
	}
	return record, nil
}
