package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type workbenchFlows struct {
	fail  bool
	calls int
}

func (f *workbenchFlows) FetchIndustryFlows(context.Context) (map[string]domain.BoardFlow, error) {
	f.calls++
	rows := map[string]domain.BoardFlow{"银行": {Code: "BK0475", Name: "银行", Percent: 1, MainNet: 1e8}}
	if f.fail {
		return rows, errors.New("stale cache")
	}
	return rows, nil
}

type workbenchDaily struct{}

func (workbenchDaily) FetchBoardDailyFlows(context.Context, string) ([]domain.BoardDailyFlow, error) {
	return []domain.BoardDailyFlow{{Date: "2026-10-15", MainNet: 1e8}, {Date: "2026-10-16", MainNet: 2e8}, {Date: "2026-10-19", MainNet: 3e8}}, nil
}

func TestBoardCaptureOnlyLiveAndNeverArchivesFailedCache(t *testing.T) {
	flows := &workbenchFlows{}
	store := storage.NewBoardSnapshotStore(filepath.Join(t.TempDir(), "boards"))
	s := NewServer(nil, nil, nil, nil, "", WithIndustryFlows(flows), WithBoardWorkbench(store, workbenchDaily{}))
	s.tradingCalendarProvider = func(context.Context, time.Time) ([]string, error) {
		return []string{"2026-10-15", "2026-10-16", "2026-10-19"}, nil
	}
	now := time.Date(2026, 10, 16, 10, 30, 0, 0, realtimeWebLocation)
	s.now = func() time.Time { return now }
	s.captureBoardSnapshot(context.Background())
	rows, err := store.Day("2026-10-16")
	if err != nil || len(rows) != 1 {
		t.Fatalf("capture: %+v %v", rows, err)
	}
	recorder := httptest.NewRecorder()
	s.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/api/board-snapshots?date=2026-10-16&after=1030", nil))
	var delta struct {
		Snapshots []domain.BoardSnapshot `json:"snapshots"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &delta); err != nil || recorder.Code != 200 || len(delta.Snapshots) != 0 {
		t.Fatalf("incremental read: %s %v", recorder.Body.String(), err)
	}
	now = now.Add(time.Minute)
	flows.fail = true
	s.captureBoardSnapshot(context.Background())
	rows, _ = store.Day("2026-10-16")
	if len(rows) != 1 {
		t.Fatal("failed cache archived as new")
	}
	flows.fail = false
	now = time.Date(2026, 10, 16, 12, 0, 0, 0, realtimeWebLocation)
	calls := flows.calls
	s.captureBoardSnapshot(context.Background())
	if calls != flows.calls {
		t.Fatal("lunch break fetched new snapshot")
	}
	now = time.Date(2026, 10, 17, 10, 0, 0, 0, realtimeWebLocation)
	s.captureBoardSnapshot(context.Background())
	if calls != flows.calls {
		t.Fatal("closed day fetched snapshot")
	}
}

func TestBoardEndpointsValidateDateAndExcludeUnfinishedDailyRow(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, "", WithBoardWorkbench(storage.NewBoardSnapshotStore(t.TempDir()), workbenchDaily{}))
	s.now = func() time.Time { return time.Date(2026, 10, 16, 10, 0, 0, 0, realtimeWebLocation) }
	for _, test := range []struct {
		url    string
		status int
	}{{"/api/board-snapshots?date=bad", 400}, {"/api/board-snapshots?date=2026-10-15", 200}, {"/api/board-daily-flows?code=bad", 400}, {"/api/board-daily-flows?code=BK0475", 200}} {
		recorder := httptest.NewRecorder()
		s.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", test.url, nil))
		if recorder.Code != test.status {
			t.Fatalf("%s: %d %s", test.url, recorder.Code, recorder.Body.String())
		}
		if test.url == "/api/board-daily-flows?code=BK0475" {
			var result struct {
				Items []domain.BoardDailyFlow `json:"items"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Items) != 1 || result.Items[0].Date != "2026-10-15" {
				t.Fatalf("unfinished/future daily data exposed: %+v", result.Items)
			}
		}
	}
}

type changingBoardDaily struct{ calls int }

func (client *changingBoardDaily) FetchBoardDailyFlows(context.Context, string) ([]domain.BoardDailyFlow, error) {
	client.calls++
	return []domain.BoardDailyFlow{
		{Date: "2026-10-15", MainNet: 1e8},
		{Date: "2026-10-16", MainNet: float64(client.calls) * 1e8},
	}, nil
}

func TestBoardDailyCacheRefreshesAtCloseAndRejectsFutureCache(t *testing.T) {
	client := &changingBoardDaily{}
	s := NewServer(nil, nil, nil, nil, "", WithBoardWorkbench(storage.NewBoardSnapshotStore(t.TempDir()), client))
	now := time.Date(2026, 10, 16, 14, 59, 0, 0, realtimeWebLocation)
	s.now = func() time.Time { return now }
	read := func() []domain.BoardDailyFlow {
		t.Helper()
		recorder := httptest.NewRecorder()
		s.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/api/board-daily-flows?code=BK0475", nil))
		var result struct {
			Items []domain.BoardDailyFlow `json:"items"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || recorder.Code != 200 {
			t.Fatalf("read daily flows: %s %v", recorder.Body.String(), err)
		}
		return result.Items
	}
	if rows := read(); len(rows) != 1 || client.calls != 1 {
		t.Fatal("unfinished row was exposed")
	}
	now = now.Add(30 * time.Second)
	read()
	if client.calls != 1 {
		t.Fatal("valid intraday cache was not reused")
	}
	now = now.Add(time.Minute)
	if rows := read(); len(rows) != 2 || rows[1].MainNet != 2e8 || client.calls != 2 {
		t.Fatalf("pre-close cache reused as final data: %+v, calls=%d", rows, client.calls)
	}
	now = now.Add(-15 * time.Second)
	read()
	if client.calls != 3 {
		t.Fatal("future cache survived a clock rollback")
	}
}
