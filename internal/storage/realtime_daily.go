package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/realtime"
)

type RealtimeDayRead struct {
	Available     bool   `json:"available"`
	Records       int    `json:"records"`
	InvalidLines  int    `json:"invalid_lines"`
	PartialTail   bool   `json:"partial_tail"`
	FutureRecords int    `json:"future_records"`
	Bytes         int64  `json:"bytes"`
	Signature     string `json:"signature"`
}

func validReviewDate(date string) bool {
	parsed, err := time.Parse(time.DateOnly, date)
	return err == nil && parsed.Format(time.DateOnly) == date
}

func (store *RealtimeSignalStore) DaySignature(date string) (string, error) {
	if !validReviewDate(date) {
		return "", fmt.Errorf("无效复盘日期")
	}
	info, err := os.Stat(filepath.Join(store.root, date+".jsonl"))
	if os.IsNotExist(err) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano()), nil
}

func (store *RealtimeSignalStore) Dates() ([]string, error) {
	files, err := store.scanFiles()
	if err != nil {
		return nil, err
	}
	dates := []string{}
	for _, file := range files {
		date := strings.TrimSuffix(file, ".jsonl")
		if validReviewDate(date) {
			dates = append(dates, date)
		}
	}
	return dates, nil
}

// WalkDay bounds the read to the opening file size. Appends that arrive during
// the request are counted on the next refresh, never as partially decoded scans.
func (store *RealtimeSignalStore) WalkDay(ctx context.Context, date string, visit func(realtime.ScanResult)) (result RealtimeDayRead, err error) {
	if !validReviewDate(date) {
		return result, fmt.Errorf("无效复盘日期")
	}
	file, err := os.Open(filepath.Join(store.root, date+".jsonl"))
	if os.IsNotExist(err) {
		result.Signature = "missing"
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return result, err
	}
	result.Available, result.Bytes = true, info.Size()
	result.Signature = fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
	reader := bufio.NewReader(io.NewSectionReader(file, 0, info.Size()))
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if readErr == io.EOF && line[len(line)-1] != '\n' {
				result.PartialTail = true
			} else {
				var scan realtime.ScanResult
				if err := json.Unmarshal(line, &scan); err != nil {
					result.InvalidLines++
				} else {
					result.Records++
					visit(scan)
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return result, readErr
		}
	}
	return result, nil
}
