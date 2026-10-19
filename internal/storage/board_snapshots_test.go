package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestBoardSnapshotsStayImmutableAndDateScoped(t *testing.T) {
	store := NewBoardSnapshotStore(filepath.Join(t.TempDir(), "boards"))
	at := time.Date(2026, 10, 16, 10, 30, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
	snapshot := domain.BoardSnapshot{Version: 1, ID: "1030", Date: "2026-10-16", CapturedAt: at, Items: []domain.BoardObservation{{Code: "BK0475", Name: "银行"}}}
	if err := store.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Items[0].Name = "later revision"
	if err := store.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	rows, err := store.Day("2026-10-16")
	if err != nil || len(rows) != 1 || rows[0].Items[0].Name != "银行" {
		t.Fatalf("snapshot overwritten: %+v %v", rows, err)
	}
	if rows, err := store.Day("2026-10-15"); err != nil || len(rows) != 0 {
		t.Fatal("missing date was fabricated")
	}
	if _, err := store.Day("../../secret"); err == nil {
		t.Fatal("unsafe date accepted")
	}
	snapshot.Date = "2026-10-15"
	if err := store.Save(snapshot); err == nil {
		t.Fatal("cross-date snapshot accepted")
	}
	dates, err := store.Dates()
	if err != nil || len(dates) != 1 || dates[0] != "2026-10-16" {
		t.Fatalf("dates: %+v %v", dates, err)
	}
}
