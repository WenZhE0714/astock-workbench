package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/wenzhe/astock-workbench/internal/realtime"
)

func (store *RealtimeSignalStore) ListOutcomeSignals(ctx context.Context) ([]realtime.Signal, error) {
	files, err := store.scanFiles()
	if err != nil {
		return nil, err
	}
	var collector realtime.OutcomeSignalCollector
	for _, name := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := collectOutcomeSignals(ctx, filepath.Join(store.root, name), &collector); err != nil {
			return nil, err
		}
	}
	return collector.Signals(0), nil
}

func collectOutcomeSignals(ctx context.Context, path string, collector *realtime.OutcomeSignalCollector) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	reader := bufio.NewReader(io.NewSectionReader(file, 0, info.Size()))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			var scan realtime.ScanResult
			if json.Unmarshal(line, &scan) == nil && validRealtimeScan(scan) {
				collector.Add(scan.Signals)
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
