package realtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type outcomeHistoryFunc func(context.Context, string) ([]domain.DailyBar, error)

func (f outcomeHistoryFunc) FetchDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	return f(ctx, symbol)
}

func TestOutcomeCancellationDoesNotWritePartialLabels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	benchmarkRead := false
	benchmark := outcomeHistoryFunc(func(context.Context, string) ([]domain.DailyBar, error) {
		benchmarkRead = true
		return []domain.DailyBar{{Date: "2026-10-15", Open: 10, High: 11, Low: 9, Close: 10}}, nil
	})
	history := outcomeHistoryFunc(func(ctx context.Context, _ string) ([]domain.DailyBar, error) {
		if !benchmarkRead {
			t.Error("benchmark was starved behind stock requests")
		}
		cancel()
		return nil, ctx.Err()
	})
	store := &outcomeStoreStub{}
	evaluator := NewOutcomeEvaluator(history, benchmark, store)
	_, err := evaluator.Evaluate(ctx, []Signal{{ID: "one", Symbol: "sh600000", DataDate: "2026-10-14"}}, OutcomeOptions{})
	if !errors.Is(err, context.Canceled) || len(store.items) != 0 {
		t.Fatalf("partial run persisted: err=%v rows=%d", err, len(store.items))
	}
}

type failingOutcomeStore struct{ readError, writeError error }

func (s failingOutcomeStore) List(int) ([]SignalOutcome, error) { return nil, s.readError }
func (s failingOutcomeStore) Upsert([]SignalOutcome) error      { return s.writeError }

func TestOutcomeArchiveFailureCannotReportSuccess(t *testing.T) {
	history := outcomeHistoryStub{bars: map[string][]domain.DailyBar{"sh600000": {{Date: "2026-10-15", Open: 10, High: 11, Low: 9, Close: 10}}}}
	failure := errors.New("archive unavailable")
	for _, store := range []failingOutcomeStore{{readError: failure}, {writeError: failure}} {
		evaluator := NewOutcomeEvaluator(history, nil, store)
		_, err := evaluator.Evaluate(context.Background(), []Signal{{ID: "one", Symbol: "sh600000", DataDate: "2026-10-14"}}, OutcomeOptions{})
		if !errors.Is(err, failure) {
			t.Fatalf("archive failure swallowed: %v", err)
		}
	}
}

func TestOutcomeSignalCollectorRetainsDatesAndRanking(t *testing.T) {
	at := time.Date(2026, 10, 14, 10, 0, 0, 0, time.Local)
	var c OutcomeSignalCollector
	c.Add([]Signal{{ID: "morning", Symbol: "sh600000", AsOf: at, Score: 99}, {ID: "yesterday", Symbol: "sh600000", AsOf: at.AddDate(0, 0, -1)}})
	c.Add([]Signal{{ID: "afternoon-a", Symbol: "sh600000", AsOf: at.Add(time.Hour), Score: 50}, {ID: "afternoon-b", Symbol: "sh600000", AsOf: at.Add(time.Hour), Score: 55}})
	items := c.Signals(0)
	if len(items) != 2 || items[0].ID != "yesterday" || items[1].ID != "afternoon-b" || c.Signals(1)[0].ID != "afternoon-b" {
		t.Fatalf("representative selection changed: %+v", items)
	}
}
