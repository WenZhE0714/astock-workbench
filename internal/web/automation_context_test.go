package web

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type budgetShadowEvaluator struct {
	t       *testing.T
	calls   int
	now     *time.Time
	scanCtx *context.Context
}

func (e *budgetShadowEvaluator) Evaluate(ctx context.Context, signals []realtime.Signal, options paper.Options) (paper.Report, error) {
	e.calls++
	if ctx.Err() != nil || (*e.scanCtx).Err() == nil {
		e.t.Fatal("scan cancellation leaked into shadow task")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) < shadowExecutionTimeout-5*time.Second {
		e.t.Fatal("shadow did not receive its own deadline")
	}
	*e.now = e.now.Add(3 * time.Minute)
	if e.calls == 1 {
		return paper.Report{}, context.DeadlineExceeded
	}
	return (shadowProfileAnalyzerStub{}).Evaluate(ctx, signals, options)
}

func TestAutomationContinuesOtherProfilesAfterScanAndProfileFailure(t *testing.T) {
	watchlist := filepath.Join(t.TempDir(), "watchlist")
	if err := storage.SaveWatchlist(watchlist, []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	now := realtimeWebTime(2026, 8, 21, 10, 15)
	snapshot := realtime.ScanResult{GeneratedAt: now, Signals: []realtime.Signal{{ID: "cached", Symbol: "sh600519", QuoteTime: "2026-08-21 10:15:00"}}}
	var scanCtx context.Context
	scanner := realtimeScannerFunc(func(ctx context.Context, _ []string, _ bool) (realtime.ScanResult, error) {
		scanCtx = ctx
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > realtimeScanTimeout {
			t.Fatal("scan has no independent timeout")
		}
		return realtime.ScanResult{}, context.DeadlineExceeded
	})
	evaluator := &budgetShadowEvaluator{t: t, now: &now, scanCtx: &scanCtx}
	balanced, conservative, aggressive, monster, adaptive := &shadowArchiveStub{}, &shadowArchiveStub{}, &shadowArchiveStub{}, &shadowArchiveStub{}, &shadowArchiveStub{}
	calendarCalls := 0
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithWatchlist(watchlist),
		WithRealtimeStrategy(scanner, realtimeArchiveStub{latest: snapshot, items: snapshot.Signals}),
		WithShadowExecutionProfiles(evaluator, balanced, conservative, aggressive), WithMonsterShadowExecution(monster), WithAdaptiveShadowExecution(adaptive),
		WithTradingCalendar(func(context.Context, time.Time) ([]string, error) {
			calendarCalls++
			return []string{"2026-08-20", "2026-08-21", "2026-08-24"}, nil
		}),
	)
	server.now = func() time.Time { return now }
	server.runAutomationCycle(context.Background())
	if evaluator.calls != 5 || balanced.saves != 0 || conservative.saves != 1 || aggressive.saves != 1 || monster.saves != 1 || adaptive.saves != 1 {
		t.Fatalf("accounts starved: calls=%d saves=%d/%d/%d/%d/%d", evaluator.calls, balanced.saves, conservative.saves, aggressive.saves, monster.saves, adaptive.saves)
	}
	if calendarCalls != 1 {
		t.Fatalf("calendar refetched across a long cycle: %d", calendarCalls)
	}
	status := server.automationStatus()
	if status.TaskStates[automationTaskScan].Status != "error" || status.TaskStates[automationTaskShadow].Status != "error" || !strings.Contains(status.LastError, "同步失败") {
		t.Fatalf("failures were hidden: %+v", status)
	}
}

func TestCalendarSnapshotIsDateBoundAndRespectsCancellation(t *testing.T) {
	calls := 0
	server := NewServer(nil, nil, nil, nil, "", WithTradingCalendar(func(context.Context, time.Time) ([]string, error) {
		calls++
		return []string{"2026-08-20", "2026-08-21", "2026-08-24"}, nil
	}))
	now := realtimeWebTime(2026, 8, 21, 10, 15)
	ctx := server.withTradingCalendarSnapshot(context.Background(), now)
	for i := 0; i < 3; i++ {
		dates, err := server.tradingCalendar(ctx, now.Add(time.Duration(i+1)*3*time.Minute))
		if err != nil || len(dates) != 3 {
			t.Fatal(dates, err)
		}
		dates[0] = "mutated"
	}
	if calls != 1 {
		t.Fatalf("snapshot not shared: %d", calls)
	}
	if _, err := server.tradingCalendar(ctx, now.AddDate(0, 0, 3)); err != nil || calls != 2 {
		t.Fatalf("snapshot crossed trading dates: %d %v", calls, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := server.tradingCalendar(canceled, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("calendar bypassed cancellation: %v", err)
	}
}

type failedCalendarHistory struct{}

func (failedCalendarHistory) FetchDailyBars(context.Context, string) ([]domain.DailyBar, error) {
	return nil, context.DeadlineExceeded
}

func TestCalendarFailureIsNotPinnedAsWeekdayFallback(t *testing.T) {
	server := NewServer(nil, nil, failedCalendarHistory{}, nil, "")
	now := realtimeWebTime(2026, 8, 21, 10, 15)
	ctx := server.withTradingCalendarSnapshot(context.Background(), now)
	if ctx.Value(tradingCalendarSnapshotKey{}) != nil {
		t.Fatal("failed calendar was pinned")
	}
	if _, err := server.tradingCalendar(ctx, now); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("calendar failure hidden: %v", err)
	}
}
