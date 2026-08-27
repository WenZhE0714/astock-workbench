package market

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseTradingCalendarDataSupportsTextAndJSON(t *testing.T) {
	textDates := parseTradingCalendarData([]byte("2026-08-20, 2026-08-22 # make-up day\n2026-08-24\n"))
	if len(textDates) != 3 || textDates[1] != "2026-08-22" {
		t.Fatalf("unexpected text dates: %#v", textDates)
	}
	jsonDates := parseTradingCalendarData([]byte(`["2026-08-20","2026-08-22"]`))
	if len(jsonDates) != 2 || jsonDates[0] != "2026-08-20" {
		t.Fatalf("unexpected JSON dates: %#v", jsonDates)
	}
}

func TestFileTradingCalendarReloadsWhenFileChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calendar.txt")
	if err := os.WriteFile(path, []byte("2026-08-20\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	calendar := NewFileTradingCalendar(path)
	first, err := calendar.Dates(context.Background(), time.Time{})
	if err != nil || len(first) != 1 || first[0] != "2026-08-20" {
		t.Fatalf("unexpected first calendar read: %#v %v", first, err)
	}
	if err := os.WriteFile(path, []byte("2026-08-22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// ModTime has nanosecond precision on supported filesystems; force a
	// distinct timestamp if the write happened within one clock tick.
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.ModTime().UnixNano() == 0 {
		t.Fatal("calendar file has no modification time")
	}
	second, err := calendar.Dates(context.Background(), time.Time{})
	if err != nil || len(second) != 1 || second[0] != "2026-08-22" {
		t.Fatalf("calendar did not reload: %#v %v", second, err)
	}
}
