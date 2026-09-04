package storage

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

// SentimentHistoryStore persists the cockpit's short rolling history so a
// process restart does not erase the intraday chart.
type SentimentHistoryStore struct {
	file string
}

func NewSentimentHistoryStore(file string) *SentimentHistoryStore {
	return &SentimentHistoryStore{file: file}
}

func (store *SentimentHistoryStore) Load() ([]domain.MarketSentimentPoint, error) {
	if store == nil || store.file == "" {
		return nil, nil
	}
	data, err := os.ReadFile(store.file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var points []domain.MarketSentimentPoint
	if err := json.Unmarshal(data, &points); err != nil {
		return nil, err
	}
	return points, nil
}

func (store *SentimentHistoryStore) Save(points []domain.MarketSentimentPoint) error {
	if store == nil || store.file == "" {
		return nil
	}
	data, err := json.MarshalIndent(points, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(store.file, append(data, '\n'), 0o600)
}
