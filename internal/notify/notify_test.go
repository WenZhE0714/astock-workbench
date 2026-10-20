package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"testing"
	"time"
)

type memoryStore struct {
	data []byte
	fail bool
}

func (s *memoryStore) Load() (State, error) {
	var state State
	if len(s.data) == 0 {
		return state, nil
	}
	err := json.Unmarshal(s.data, &state)
	return state, err
}

func (s *memoryStore) Save(state State) error {
	if s.fail {
		return errors.New("disk unavailable")
	}
	var err error
	s.data, err = json.Marshal(state)
	return err
}

type recordingSender struct {
	items []Message
	fail  bool
}

func (s *recordingSender) Send(_ context.Context, message Message) error {
	s.items = append(s.items, message)
	if s.fail {
		return errors.New("desktop unavailable")
	}
	return nil
}

func testDispatcher(t *testing.T, sender Sender, store Store, at time.Time) *Dispatcher {
	t.Helper()
	d, err := NewDispatcher(sender, store, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	d.now = func() time.Time { return at }
	return d
}

func TestDispatcherDeduplicatesAfterRestartAndResumesFailedDelivery(t *testing.T) {
	now := time.Now()
	store, sender := &memoryStore{}, &recordingSender{}
	message := Message{ID: "candidate:2026-10-15:sh600000", At: now}
	d := testDispatcher(t, sender, store, now)
	d.deliver(context.Background(), message)
	d.deliver(context.Background(), message)
	d = testDispatcher(t, sender, store, now)
	d.deliver(context.Background(), message)
	if len(sender.items) != 1 {
		t.Fatalf("duplicate delivery after refresh/restart: %d", len(sender.items))
	}
	message.ID = "order:balanced:buy-1"
	sender.fail = true
	d.deliver(context.Background(), message)
	sender.fail = false
	d = testDispatcher(t, sender, store, now)
	d.retry(context.Background())
	if len(sender.items) != 3 || d.state.Records[message.ID].Status != "submitted" {
		t.Fatalf("failed delivery did not resume: %+v", d.state.Records[message.ID])
	}
}

func TestDispatcherRejectsStaleAndFutureAndLimitsFailures(t *testing.T) {
	now := time.Now()
	sender := &recordingSender{fail: true}
	d := testDispatcher(t, sender, &memoryStore{}, now)
	for _, at := range []time.Time{{}, now.Add(-6 * time.Minute), now.Add(time.Minute)} {
		d.deliver(context.Background(), Message{ID: at.String(), At: at})
	}
	if len(sender.items) != 0 {
		t.Fatal("invalid timestamps reached the desktop")
	}
	message := Message{ID: "failed", At: now}
	for range 6 {
		d.deliver(context.Background(), message)
	}
	if len(sender.items) != 3 {
		t.Fatalf("retry limit not respected: %d", len(sender.items))
	}
}

func TestForwardFailureStaysDeduplicatedAfterRestart(t *testing.T) {
	now := time.Now()
	store, sender := &memoryStore{}, &recordingSender{}
	message := Message{ID: "forward-health:plan:2026-10-20:collection", Kind: "forward-collection", At: now}
	d := testDispatcher(t, sender, store, now)
	d.deliver(context.Background(), message)
	message.At = now.Add(time.Minute)
	message.Body = "changed diagnostic"
	d = testDispatcher(t, sender, store, message.At)
	d.deliver(context.Background(), message)
	if len(sender.items) != 1 {
		t.Fatal("forward failure repeated after restart")
	}
	message.ID = "forward-health:plan:2026-10-21:collection"
	message.At = now.AddDate(0, 0, 1)
	d = testDispatcher(t, sender, store, message.At)
	d.deliver(context.Background(), message)
	if len(sender.items) != 2 {
		t.Fatal("persistent failure not reported the next day")
	}
}

func TestDispatcherDoesNotSendUntilRecordIsDurable(t *testing.T) {
	now := time.Now()
	store, sender := &memoryStore{fail: true}, &recordingSender{}
	d := testDispatcher(t, sender, store, now)
	message := Message{ID: "disk-failure", At: now}
	for range 4 {
		d.deliver(context.Background(), message)
	}
	if len(sender.items) != 0 {
		t.Fatal("notification sent without durable state")
	}
	store.fail = false
	d.retry(context.Background())
	if len(sender.items) != 1 {
		t.Fatal("storage failures consumed transport attempts")
	}
}

func TestDispatcherDoesNotReviveExpiredCandidateOnNextScan(t *testing.T) {
	now := time.Now()
	store, sender := &memoryStore{}, &recordingSender{fail: true}
	d := testDispatcher(t, sender, store, now)
	message := Message{ID: "candidate", At: now}
	d.deliver(context.Background(), message)
	d.now = func() time.Time { return now.Add(6 * time.Minute) }
	message.At = d.now()
	d.deliver(context.Background(), message)
	if len(sender.items) != 1 || d.state.Records[message.ID].Status != "expired" {
		t.Fatal("new scan incorrectly revived the old pending notification")
	}
}

type blockingSender struct{ started chan struct{} }

func (s blockingSender) Send(ctx context.Context, _ Message) error {
	close(s.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestDispatcherSlowDesktopDoesNotBlockProducerAndCancelsOnShutdown(t *testing.T) {
	now := time.Now()
	sender := blockingSender{started: make(chan struct{})}
	d := testDispatcher(t, sender, &memoryStore{}, now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	d.Offer(Message{ID: "first", At: now})
	select {
	case <-sender.started:
	case <-time.After(time.Second):
		t.Fatal("sender was not started")
	}
	produced := make(chan struct{})
	go func() { d.Offer(Message{ID: "second", At: now}); close(produced) }()
	select {
	case <-produced:
	case <-time.After(time.Second):
		t.Fatal("desktop blocked market producer")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel sender")
	}
}
