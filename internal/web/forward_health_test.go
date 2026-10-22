package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/notify"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type forwardHealthSender chan notify.Message

func (sender forwardHealthSender) Send(_ context.Context, message notify.Message) error {
	sender <- message
	return nil
}

func TestForwardHealthLoopDeliversWithoutBrowserAndCancels(t *testing.T) {
	store, _, _ := volumeStudyViewFixture(t)
	now := time.Now()
	if err := store.SaveStatus(storage.VolumeForwardStatus{Status: "error", Detail: "missing bar", CheckedAt: now}); err != nil {
		t.Fatal(err)
	}
	sender := make(forwardHealthSender, 1)
	dispatcher, err := notify.NewDispatcher(sender, storage.NewNativeNotificationStore(filepath.Join(t.TempDir(), "mac.json")), log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(nil, nil, nil, nil, "", WithVolumeForwardStudy(store), WithNativeNotifications(dispatcher))
	ctx, cancel := context.WithCancel(context.Background())
	dispatchDone, healthDone := make(chan struct{}), make(chan struct{})
	go func() { defer close(dispatchDone); dispatcher.Run(ctx) }()
	go func() { defer close(healthDone); s.runForwardHealthLoop(ctx) }()
	t.Cleanup(func() {
		cancel()
		for _, done := range []chan struct{}{healthDone, dispatchDone} {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("background loop did not stop")
			}
		}
	})
	select {
	case message := <-sender:
		if message.Kind != "forward-collection" || !strings.Contains(message.Body, "missing bar") {
			t.Fatalf("wrong message: %+v", message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("background failure did not reach dispatcher")
	}
}

func TestForwardAcceptanceFreshnessAndCoverage(t *testing.T) {
	now := time.Now()
	report := storage.ForwardAcceptanceReport{CheckedAt: now, FirstDate: "2026-10-20", SealedDays: 1}
	if view := assessForwardAcceptance(report, nil, 1, "2026-10-20", now); !view.Bound || !view.Current {
		t.Fatalf("first day: %+v", view)
	}
	if view := assessForwardAcceptance(report, nil, 2, "2026-10-21", now); view.Current {
		t.Fatal("lagging report considered current")
	}
	if view := assessForwardAcceptance(report, nil, 1, "2026-10-20", now.Add(16*time.Minute)); !view.Stale || view.Current {
		t.Fatal("stale report considered current")
	}
	report.SealedDays = 2
	report.Execution = &storage.ForwardExecutionAcceptance{VerifiedThrough: "2026-10-21"}
	if view := assessForwardAcceptance(report, nil, 2, "2026-10-21", now); !view.Current {
		t.Fatal("execution coverage lost")
	}
	if view := assessForwardAcceptance(report, nil, 2, "2026-10-22", now); view.Current {
		t.Fatal("wrong end date considered current")
	}
	if view := assessForwardAcceptance(report, errors.New("wrong plan"), 2, "2026-10-21", now); view.Bound || view.Report != nil || view.Error == "" {
		t.Fatal("unbound report exposed")
	}
}

func TestForwardFailureSelectionAndDailyIdentity(t *testing.T) {
	now := time.Date(2026, 10, 20, 16, 0, 0, 0, realtimeWebLocation)
	status := storage.VolumeForwardStatus{Status: "waiting", CheckedAt: now}
	for _, state := range []string{"waiting", "passed"} {
		view := forwardAcceptanceView{Configured: true, Stale: true, Report: &storage.ForwardAcceptanceReport{Status: state}}
		if got := forwardStatusFailureMessages("plan", status, nil, view, now); len(got) != 0 {
			t.Fatal("normal or stale report notified")
		}
	}
	status.Status, status.Detail = "error", "missing daily bars"
	view := forwardAcceptanceView{Configured: true, Report: &storage.ForwardAcceptanceReport{Status: "error", Detail: "采集任务报错: missing daily bars", Execution: &storage.ForwardExecutionAcceptance{Status: "paused"}}}
	messages := forwardStatusFailureMessages("plan", status, nil, view, now)
	if len(messages) != 1 || messages[0].Kind != "forward-collection" {
		t.Fatalf("mirrored failure duplicated: %+v", messages)
	}
	view.Report.Execution.Status, view.Report.Execution.Detail = "error", "cash mismatch"
	messages = forwardStatusFailureMessages("plan", status, nil, view, now)
	if len(messages) != 2 || !strings.Contains(messages[1].Body, "cash mismatch") {
		t.Fatal("independent execution failure lost")
	}
	status.Status = "waiting"
	for _, tc := range []struct {
		view forwardAcceptanceView
		kind string
	}{
		{forwardAcceptanceView{Configured: true, Error: "wrong archive"}, "forward-acceptance"},
		{forwardAcceptanceView{Configured: true, Report: &storage.ForwardAcceptanceReport{Status: "attention"}}, "forward-acceptance-attention"},
	} {
		got := forwardStatusFailureMessages("plan", status, nil, tc.view, now)
		if len(got) != 1 || got[0].Kind != tc.kind {
			t.Fatalf("missing alert: %+v", got)
		}
	}
	if got := forwardStatusFailureMessages("plan", status, errors.New("invalid JSON"), forwardAcceptanceView{}, now); len(got) != 1 {
		t.Fatal("collector read failure lost")
	}
	a := forwardFailureMessage("plan", "collection", "failed", "old", now, now.Add(-time.Hour))
	b := forwardFailureMessage("plan", "collection", "failed", "new", now.Add(time.Minute), now)
	c := forwardFailureMessage("plan", "collection", "failed", "new", now.AddDate(0, 0, 1), now)
	if a.ID != b.ID || a.ID == c.ID || a.At != now || !strings.Contains(a.Body, "报告时间") {
		t.Fatal("daily identity or detected time invalid")
	}
	boundary := now.Add(8 * time.Hour)
	if forwardFailureMessage("plan", "collection", "", "", boundary, now).ID == a.ID {
		t.Fatal("Shanghai midnight did not reset identity")
	}
}

func TestForwardFailureSelectionAfterCollectorRecovery(t *testing.T) {
	checked := time.Date(2026, 10, 22, 15, 35, 5, 0, realtimeWebLocation)
	for _, tc := range []struct {
		name             string
		collectorStatus  string
		collectorTime    time.Time
		collectorDays    int
		executionStatus  string
		detail           string
		wantNotification bool
	}{
		{"sealed after failure", "sealed", checked.Add(40 * time.Second), 3, "paused", "采集任务报错: HTTP 501", false},
		{"waiting after failure", "waiting", checked.Add(5 * time.Minute), 3, "paused", "采集任务报错: HTTP 501", false},
		{"recovery with same day count", "sealed", checked.Add(40 * time.Second), 2, "paused", "采集任务报错: HTTP 501", false},
		{"collector has not recovered yet", "waiting", checked.Add(-time.Minute), 2, "paused", "采集任务报错: HTTP 501", true},
		{"same timestamp is inconclusive", "sealed", checked, 3, "paused", "采集任务报错: HTTP 501", true},
		{"missing collector timestamp", "sealed", time.Time{}, 3, "paused", "采集任务报错: HTTP 501", true},
		{"collector archive count regressed", "sealed", checked.Add(time.Minute), 1, "paused", "采集任务报错: HTTP 501", true},
		{"unknown collector status", "unknown", checked.Add(time.Minute), 3, "paused", "采集任务报错: HTTP 501", true},
		{"independent execution failure", "sealed", checked.Add(time.Minute), 3, "error", "cash mismatch", true},
		{"independent paused failure", "sealed", checked.Add(time.Minute), 3, "paused", "archive mismatch", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := storage.VolumeForwardStatus{Status: tc.collectorStatus, CheckedAt: tc.collectorTime, SealedDays: tc.collectorDays}
			view := forwardAcceptanceView{Configured: true, Report: &storage.ForwardAcceptanceReport{
				Status: "error", CheckedAt: checked, SealedDays: 2, Detail: tc.detail,
				Execution: &storage.ForwardExecutionAcceptance{Status: tc.executionStatus, Detail: tc.detail},
			}}
			messages := forwardStatusFailureMessages("plan", status, nil, view, checked.Add(6*time.Minute))
			if (len(messages) > 0) != tc.wantNotification {
				t.Fatalf("want notification %v, got %+v", tc.wantNotification, messages)
			}
			if len(messages) > 0 && messages[0].Kind != "forward-acceptance" {
				t.Fatalf("unexpected notification: %+v", messages)
			}
		})
	}
}

func TestForwardAcceptanceAPIIsReadOnlyAndRejectsWrongExperiment(t *testing.T) {
	store, plan, root := volumeStudyViewFixture(t)
	now := plan.CreatedAt.Add(time.Minute)
	directory := t.TempDir()
	report := storage.ForwardAcceptanceReport{CheckedAt: now, Status: "waiting", Archive: root, PlanFingerprint: plan.Fingerprint}
	write := func() {
		data, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "status.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	s := NewServer(nil, nil, nil, nil, "", WithVolumeForwardStudy(store), WithForwardAcceptance(storage.NewForwardAcceptanceStore(directory)))
	s.now = func() time.Time { return now }
	before := map[string]string{}
	for _, path := range []string{filepath.Join(root, "config.json"), filepath.Join(root, "source-result.json"), filepath.Join(root, "status.json"), filepath.Join(directory, "status.json")} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = string(data)
	}
	read := func() volumeForwardStudyResponse {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/strategy/volume-forward", nil))
		var result volumeForwardStudyResponse
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 {
			t.Fatalf("API: %d %s", w.Code, w.Body.String())
		}
		return result
	}
	if result := read(); !result.Acceptance.Current || !result.Acceptance.Bound || result.Acceptance.Report.Execution != nil {
		t.Fatal("legacy report not projected honestly")
	}
	if len(s.forwardHealthMessages(now)) != 0 {
		t.Fatal("normal study emitted a failure")
	}
	for path, expected := range before {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != expected {
			t.Fatal("API or poller modified evidence")
		}
	}
	report.PlanFingerprint = "other"
	write()
	if result := read(); result.Acceptance.Bound || result.Acceptance.Report != nil || result.Acceptance.Error == "" {
		t.Fatal("wrong experiment report exposed")
	}
	if messages := s.forwardHealthMessages(now); len(messages) != 1 || messages[0].Kind != "forward-acceptance" {
		t.Fatal("binding failure not reported")
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if messages := s.forwardHealthMessages(now); len(messages) != 1 || messages[0].Kind != "forward-archive" {
		t.Fatal("invalid archive not reported")
	}
}
