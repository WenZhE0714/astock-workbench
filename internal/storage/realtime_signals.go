package storage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/realtime"
)

type RealtimeSignalStore struct{ root string }

type RealtimeOutcomeStore struct{ root string }

func NewRealtimeSignalStore(root string) *RealtimeSignalStore {
	return &RealtimeSignalStore{root: root}
}

func NewRealtimeOutcomeStore(root string) *RealtimeOutcomeStore {
	return &RealtimeOutcomeStore{root: root}
}

func (store *RealtimeSignalStore) Append(result realtime.ScanResult) error {
	if store == nil || store.root == "" {
		return fmt.Errorf("实时信号目录未初始化")
	}
	if result.GeneratedAt.IsZero() {
		result.GeneratedAt = time.Now()
	}
	path := filepath.Join(store.root, result.GeneratedAt.Format("2006-01-02")+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}

func (store *RealtimeSignalStore) List(limit int) ([]realtime.Signal, error) {
	files, err := store.scanFiles()
	if err != nil {
		return nil, err
	}
	result := make([]realtime.Signal, 0)
	for _, name := range files {
		lines, readError := readRealtimeScanResults(filepath.Join(store.root, name))
		if readError != nil {
			return nil, readError
		}
		for index := len(lines) - 1; index >= 0; index-- {
			if !validRealtimeScan(lines[index]) {
				continue
			}
			result = append(result, lines[index].Signals...)
			if limit > 0 && len(result) >= limit {
				return result[:limit], nil
			}
		}
	}
	return result, nil
}

// Latest returns the newest complete scan snapshot rather than flattening its
// signals. The web server uses it to restore the frozen view after a restart.
func (store *RealtimeSignalStore) Latest() (realtime.ScanResult, error) {
	files, err := store.scanFiles()
	if err != nil {
		return realtime.ScanResult{}, err
	}
	for _, name := range files {
		lines, readError := readRealtimeScanResults(filepath.Join(store.root, name))
		if readError != nil {
			return realtime.ScanResult{}, readError
		}
		for index := len(lines) - 1; index >= 0; index-- {
			if validRealtimeScan(lines[index]) {
				return lines[index], nil
			}
		}
	}
	return realtime.ScanResult{}, nil
}

func validRealtimeScan(result realtime.ScanResult) bool {
	if result.GeneratedAt.IsZero() {
		return false
	}
	// New snapshots carry the point-in-time calendar decision. Reject a
	// snapshot explicitly marked as a non-trading day even when the current
	// process has no access to the same holiday file after restart. Older
	// snapshots omit TradingDate and retain the legacy session fallback.
	if result.TradingDate != "" {
		if !result.TradingDay {
			return false
		}
		generatedDate := result.GeneratedAt.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
		if generatedDate != result.TradingDate {
			return false
		}
		// Reconstruct a one-day calendar from the persisted decision. This
		// preserves valid make-up Saturdays after restart without depending on
		// the current process having the same external holiday file.
		session := realtime.MarketSessionAtWithCalendar(result.GeneratedAt, []string{result.TradingDate})
		return session.ScanAllowed || session.FinalizationAllowed
	}
	session := realtime.MarketSessionAt(result.GeneratedAt)
	return session.ScanAllowed || session.FinalizationAllowed
}

func (store *RealtimeSignalStore) scanFiles() ([]string, error) {
	if store == nil || strings.TrimSpace(store.root) == "" {
		return nil, fmt.Errorf("实时信号目录未初始化")
	}
	entries, err := os.ReadDir(store.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasPrefix(entry.Name(), "outcomes-") && filepath.Ext(entry.Name()) == ".jsonl" {
			files = append(files, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	return files, nil
}

func readRealtimeScanResults(path string) ([]realtime.ScanResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	results := make([]realtime.ScanResult, 0)
	reader := bufio.NewReader(file)
	for {
		line, readError := reader.ReadBytes('\n')
		if len(line) > 0 {
			var item realtime.ScanResult
			if json.Unmarshal(line, &item) == nil {
				results = append(results, item)
			}
		}
		if readError != nil {
			if readError != io.EOF {
				return nil, readError
			}
			break
		}
	}
	return results, nil
}

// Upsert appends outcome revisions. List collapses revisions by key and keeps
// the newest evaluation, allowing pending horizons to become ready later.
func (store *RealtimeOutcomeStore) Upsert(items []realtime.SignalOutcome) error {
	if store == nil || store.root == "" {
		return fmt.Errorf("实时结果目录未初始化")
	}
	if len(items) == 0 {
		return nil
	}
	if err := os.MkdirAll(store.root, 0o700); err != nil {
		return err
	}
	byDate := make(map[string][]realtime.SignalOutcome)
	for _, item := range items {
		when := item.EvaluatedAt
		if when.IsZero() {
			when = time.Now()
			item.EvaluatedAt = when
		}
		byDate[when.Format("2006-01-02")] = append(byDate[when.Format("2006-01-02")], item)
	}
	for date, values := range byDate {
		path := filepath.Join(store.root, "outcomes-"+date+".jsonl")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(file)
		for _, item := range values {
			if err := encoder.Encode(item); err != nil {
				file.Close()
				return err
			}
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (store *RealtimeOutcomeStore) List(limit int) ([]realtime.SignalOutcome, error) {
	if store == nil || store.root == "" {
		return nil, fmt.Errorf("实时结果目录未初始化")
	}
	entries, err := os.ReadDir(store.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "outcomes-") && filepath.Ext(entry.Name()) == ".jsonl" {
			files = append(files, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	latest := make(map[string]realtime.SignalOutcome)
	for _, name := range files {
		file, openErr := os.Open(filepath.Join(store.root, name))
		if openErr != nil {
			continue
		}
		decoder := json.NewDecoder(file)
		for {
			var item realtime.SignalOutcome
			if decodeErr := decoder.Decode(&item); decodeErr != nil {
				if decodeErr != io.EOF {
					file.Close()
					return nil, decodeErr
				}
				break
			}
			if item.Key == "" {
				continue
			}
			previous, found := latest[item.Key]
			if !found || item.EvaluatedAt.After(previous.EvaluatedAt) || item.EvaluatedAt.Equal(previous.EvaluatedAt) {
				latest[item.Key] = item
			}
		}
		file.Close()
	}
	result := make([]realtime.SignalOutcome, 0, len(latest))
	for _, item := range latest {
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].SignalAsOf.Equal(result[j].SignalAsOf) {
			return result[i].Horizon < result[j].Horizon
		}
		return result[i].SignalAsOf.After(result[j].SignalAsOf)
	})
	if limit > 0 && len(result) > limit {
		return result[:limit], nil
	}
	return result, nil
}
