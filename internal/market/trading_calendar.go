package market

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

// FileTradingCalendar reads one ISO exchange date per line. A JSON array of
// strings is accepted as well, which makes it easy to export an official
// calendar without introducing a database dependency. Blank lines and '#'
// comments are ignored.
type FileTradingCalendar struct {
	path    string
	mu      sync.Mutex
	modTime int64
	size    int64
	dates   []string
}

func NewFileTradingCalendar(path string) *FileTradingCalendar {
	return &FileTradingCalendar{path: strings.TrimSpace(path)}
}

func (calendar *FileTradingCalendar) Dates(context.Context, time.Time) ([]string, error) {
	if calendar == nil || calendar.path == "" {
		return nil, nil
	}
	info, err := os.Stat(calendar.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	modTime := info.ModTime().UnixNano()
	calendar.mu.Lock()
	if calendar.modTime == modTime && calendar.size == info.Size() && len(calendar.dates) > 0 {
		dates := append([]string(nil), calendar.dates...)
		calendar.mu.Unlock()
		return dates, nil
	}
	calendar.mu.Unlock()

	data, err := os.ReadFile(calendar.path)
	if err != nil {
		return nil, err
	}
	dates := parseTradingCalendarData(data)
	calendar.mu.Lock()
	calendar.modTime = modTime
	calendar.size = info.Size()
	calendar.dates = append([]string(nil), dates...)
	calendar.mu.Unlock()
	return dates, nil
}

func parseTradingCalendarData(data []byte) []string {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var values []string
		if json.Unmarshal([]byte(trimmed), &values) == nil {
			return values
		}
	}
	values := make([]string, 0)
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		for _, value := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ';' || r == '\t' || r == ' '
		}) {
			if value != "" {
				values = append(values, value)
			}
		}
	}
	return values
}

// Keep the provider's signature close to the rest of the market adapters.
// The compile-time assertion catches accidental API drift.
var _ interface {
	Dates(context.Context, time.Time) ([]string, error)
} = (*FileTradingCalendar)(nil)
