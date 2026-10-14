package strategy

import (
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestForwardHealthScheduleUsesOnlyKnownCalendar(t *testing.T) {
	task, _, _, date := forwardStrategyFixture(t)
	now := chartTestTime(date, 16)
	control := domain.PatternForwardControl{Enabled: true}
	status := domain.PatternForwardStatus{}
	health := PatternForwardHealth(task, control, status, nil, now)
	if health.Phase != "calendar-pending" || !health.NextAttemptAt.IsZero() {
		t.Fatal("unknown calendar produced a scheduled trading day")
	}
	status.CalendarDates = []string{date, now.AddDate(0, 0, 4).Format(time.DateOnly)}
	status.LastAttemptAt = now.Add(-2 * time.Minute)
	health = PatternForwardHealth(task, control, status, nil, now)
	if health.Phase != "retrying" || !health.NextAttemptAt.Equal(now.Add(8*time.Minute)) {
		t.Fatalf("retry did not respect frozen interval: %+v", health)
	}
	status.CalendarError = "calendar unavailable"
	if !PatternForwardHealth(task, control, status, nil, now).NextAttemptAt.IsZero() {
		t.Fatal("failed calendar reused a stale schedule")
	}
	status.CalendarError = ""
	afterClose := chartTestTime(date, 18)
	health = PatternForwardHealth(task, control, status, nil, afterClose)
	if health.NextAttemptAt.Format(time.DateOnly) != status.CalendarDates[1] || health.NextAttemptAt.Hour() != 15 || health.NextAttemptAt.Minute() != 30 {
		t.Fatal("next session guessed from weekday or calendar day")
	}
	control.Enabled = false
	health = PatternForwardHealth(task, control, status, nil, now)
	if health.Phase != "paused" || !health.NextAttemptAt.IsZero() {
		t.Fatal("paused task scheduled a capture")
	}
}

func TestForwardHealthCountsMissingAndTrackingSeparately(t *testing.T) {
	task, _, _, date := forwardStrategyFixture(t)
	now := chartTestTime(date, 16)
	days := []domain.PatternForwardDay{{TaskID: task.ID, Date: date, State: "open", LastAttemptAt: now, Items: []domain.PatternForwardItem{{Symbol: task.Pool.Members[0].Symbol, State: "pending", Error: "stale"}}}}
	status := domain.PatternForwardStatus{CalendarDates: []string{date}, LastAttemptAt: now}
	h := PatternForwardHealth(task, domain.PatternForwardControl{Enabled: true}, status, days, now)
	if h.Captured != 0 || h.MissingDays != 0 || len(h.Issues) != 1 {
		t.Fatal("pending capture counted as a completed or missed day")
	}
	days[0].State, days[0].SealedAt = "missed", chartTestTime(date, 18)
	h = PatternForwardHealth(task, domain.PatternForwardControl{Enabled: true}, status, days, chartTestTime(date, 18))
	if h.Phase != "incomplete" || h.MissingDays != 1 || h.TrackingLag != 1 || h.Captured != 0 {
		t.Fatal("sealed missing day disappeared from health")
	}
	days[0].State = "captured"
	days[0].Items[0].State, days[0].Items[0].SnapshotHash = "captured", "hash"
	status.TrackingThrough = date
	h = PatternForwardHealth(task, domain.PatternForwardControl{Enabled: true}, status, days, now)
	if h.Phase != "complete" || h.TrackingLag != 0 || h.MissingDays != 0 || h.Captured != 1 || h.LastSuccessAt.IsZero() {
		t.Fatal("healthy capture or tracking was not recognized")
	}
}

func TestForwardOperationsDeduplicateRecoverAndPreserveAcknowledgment(t *testing.T) {
	now := time.Now()
	issue := domain.PatternForwardIssue{Key: "calendar:2026-10-14", Code: "calendar", Message: "unavailable", Severity: "error"}
	first := AdvancePatternForwardOperations(domain.PatternForwardOperations{}, []domain.PatternForwardIssue{issue}, now)
	first.Events[0].AcknowledgedAt = now
	repeated := AdvancePatternForwardOperations(first, []domain.PatternForwardIssue{issue}, now.Add(time.Minute))
	if repeated.Sequence != 1 || len(repeated.Events) != 1 || repeated.Events[0].AcknowledgedAt.IsZero() {
		t.Fatal("repeated check duplicated alert or erased acknowledgment")
	}
	recovered := AdvancePatternForwardOperations(repeated, nil, now.Add(2*time.Minute))
	if recovered.Sequence != 2 || recovered.Events[1].Kind != "resolved" || len(recovered.Active) != 0 {
		t.Fatal("recovery not recorded")
	}
	again := AdvancePatternForwardOperations(recovered, []domain.PatternForwardIssue{issue}, now.Add(3*time.Minute))
	if again.Sequence != 3 || again.Events[2].Kind != "opened" {
		t.Fatal("recurring incident not distinguished from duplicate polling")
	}
}
