package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func TestForwardHealthCalendarRecoveryAndAlertAcknowledgment(t *testing.T) {
	s, _, clock, dates, _ := forwardServerFixture(t)
	task := createForwardFixture(t, s)
	s.tradingCalendarProvider = func(context.Context, time.Time) ([]string, error) { return nil, fmt.Errorf("offline") }
	s.runPatternForwardCycle(context.Background())
	first, _ := s.patternForwards.Operations(task.ID)
	if len(first.Events) != 1 || first.Events[0].Issue.Code != "calendar" {
		t.Fatalf("calendar incident missing: %+v", first)
	}
	s.runPatternForwardCycle(context.Background())
	repeated, _ := s.patternForwards.Operations(task.ID)
	if repeated.Sequence != first.Sequence {
		t.Fatal("repeated scheduler check duplicated an alert")
	}
	r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-forwards", fmt.Sprintf(`{"action":"ack-alert","id":%q,"sequence":%d}`, task.ID, first.Events[0].Sequence))
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	s.tradingCalendarProvider = func(context.Context, time.Time) ([]string, error) { return dates, nil }
	clock.Set(clock.Now().Add(time.Minute))
	s.runPatternForwardCycle(context.Background())
	status, _ := s.patternForwards.Status(task.ID)
	recovered, _ := s.patternForwards.Operations(task.ID)
	if status.Error != "" || status.CalendarError != "" || len(recovered.Events) != 2 || recovered.Events[1].Kind != "resolved" || recovered.Events[0].AcknowledgedAt.IsZero() {
		t.Fatal("recovery retained stale error or lost acknowledgment")
	}
	r = validationHTTPRequest(s, http.MethodGet, "/api/pattern-forwards?id="+task.ID, "")
	var view patternForwardView
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &view) != nil || view.Health.NextAttemptAt.IsZero() || view.Operations.Audit.State != "waiting" {
		t.Fatal("health endpoint did not distinguish waiting from verified")
	}
}

func TestForwardAuditAndImportPreserveCapturedEvidence(t *testing.T) {
	s, _, clock, dates, index := forwardServerFixture(t)
	root := t.TempDir()
	s.patternBatches = storage.NewPatternBatchStore(filepath.Join(root, "pattern-batches"))
	s.patternForwards = storage.NewPatternForwardStore(filepath.Join(root, "pattern-forwards"))
	task := createForwardFixture(t, s)
	for _, date := range dates[index : index+3] {
		day, _ := time.ParseInLocation(time.DateOnly, date, realtimeWebLocation)
		clock.Set(day.Add(16 * time.Hour))
		s.runPatternForwardCycle(context.Background())
	}
	audit := s.patternForwards.Audit(task.ID, clock.Now())
	if audit.State != "verified" || audit.Error != "" || audit.Snapshots != 6 || audit.TrackingThrough != dates[index+2] {
		t.Fatalf("audit failed for immutable evidence: %+v", audit)
	}
	before, _ := s.patternForwards.Tracking(task.ID)
	destination := filepath.Join(t.TempDir(), "copied")
	if _, err := storage.ImportPatternForwards(root, destination, true, clock.Now()); err != nil {
		t.Fatal(err)
	}
	copy := storage.NewPatternForwardStore(filepath.Join(destination, "pattern-forwards"))
	after, _ := copy.Tracking(task.ID)
	if !reflect.DeepEqual(before, after) || copy.Audit(task.ID, clock.Now()).State != "verified" {
		t.Fatal("migration changed evidence or logs")
	}
	path := filepath.Join(destination, "pattern-forwards", "tasks", task.ID, "days", dates[index], "sh600100.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if corrupted := copy.Audit(task.ID, clock.Now()); corrupted.State != "invalid" || corrupted.Error == "" {
		t.Fatal("audit accepted a corrupt snapshot")
	}
}

func TestForwardHealthMissingDayRemainsVisibleWhenPaused(t *testing.T) {
	s, _, clock, dates, index := forwardServerFixture(t)
	task := createForwardFixture(t, s)
	day, _ := time.ParseInLocation(time.DateOnly, dates[index+2], realtimeWebLocation)
	clock.Set(day.Add(16 * time.Hour))
	s.runPatternForwardCycle(context.Background())
	if err := s.patternForwards.SetControl(task.ID, domain.PatternForwardControl{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	s.runPatternForwardCycle(context.Background())
	view, err := s.forwardView(task)
	if err != nil || view.Health.Phase != "paused" || view.Health.MissingDays != 2 || !view.Health.NextAttemptAt.IsZero() || len(view.Operations.Events) < 2 {
		t.Fatalf("paused task hid missing observations: %+v %v", view.Health, err)
	}
}

func TestForwardAuditDoesNotCallAnOpenDayVerified(t *testing.T) {
	s, history, clock, dates, index := forwardServerFixture(t)
	task := createForwardFixture(t, s)
	clock.Set(clock.Now().Add(2 * time.Hour))
	s.runPatternForwardCycle(context.Background())
	day, _ := time.ParseInLocation(time.DateOnly, dates[index+1], realtimeWebLocation)
	clock.Set(day.Add(16 * time.Hour))
	history.stale = "sh600101"
	s.runPatternForwardCycle(context.Background())
	result := s.patternForwards.Audit(task.ID, clock.Now())
	if result.Error != "" || result.State != "collecting" || result.Days != 2 || result.CompleteDays != 1 {
		t.Fatalf("open day incorrectly declared verified: %+v", result)
	}
}
