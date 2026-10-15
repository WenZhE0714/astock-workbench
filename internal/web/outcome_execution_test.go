package web

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/realtime"
)

type streamingOutcomeArchiveStub struct {
	realtimeArchiveStub
	streamed bool
}

func (s *streamingOutcomeArchiveStub) ListOutcomeSignals(ctx context.Context) ([]realtime.Signal, error) {
	s.streamed = true
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) < 2*time.Minute {
		return nil, context.DeadlineExceeded
	}
	return s.items, nil
}

func TestFullOutcomeRetryUsesStreamAndUpdatesSchedule(t *testing.T) {
	at := realtimeWebTime(2026, 10, 15, 16, 30)
	archive := &streamingOutcomeArchiveStub{realtimeArchiveStub: realtimeArchiveStub{items: []realtime.Signal{{Symbol: "sh600000"}}}}
	evaluator := realtimeOutcomeFunc{evaluate: func(ctx context.Context, _ []realtime.Signal, options realtime.OutcomeOptions) (realtime.OutcomeReport, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < 9*time.Minute || options.SignalLimit != -1 {
			t.Error("full evaluation did not get an independent budget")
		}
		return realtime.OutcomeReport{DataThrough: "2026-10-15"}, nil
	}}
	s := NewServer(resolverStub{}, nil, nil, nil, "", WithRealtimeStrategy(realtimeScannerStub{}, archive), WithRealtimeOutcomes(evaluator))
	s.now = func() time.Time { return at }
	r := validationHTTPRequest(s, http.MethodPost, "/api/strategy/realtime?view=outcomes&full=1", "")
	if r.Code != 200 || !archive.streamed || !s.automationStatus().LastOutcomeAt.Equal(at) || s.automationStatus().TaskStates[automationTaskOutcomes].Status != "success" {
		t.Fatalf("manual close retry failed: %d %s %+v", r.Code, r.Body.String(), s.automationStatus())
	}
	s.outcomeMu.Lock()
	r = validationHTTPRequest(s, http.MethodPost, "/api/strategy/realtime?view=outcomes&full=1", "")
	s.outcomeMu.Unlock()
	if r.Code != http.StatusConflict {
		t.Fatal("concurrent duplicate evaluation accepted")
	}
}
