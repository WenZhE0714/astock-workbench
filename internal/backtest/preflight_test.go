package backtest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type inspectionProvider struct {
	mu      sync.Mutex
	starts  map[string]time.Time
	started chan struct{}
}

func (p *inspectionProvider) FetchDailyBarsRange(ctx context.Context, symbol string, start, _ time.Time, _ PriceAdjustment) ([]domain.DailyBar, error) {
	if p.started != nil {
		select {
		case p.started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	p.mu.Lock()
	if p.starts == nil {
		p.starts = map[string]time.Time{}
	}
	p.starts[symbol] = start
	p.mu.Unlock()
	switch symbol {
	case "sh600001", "sh000300":
		return nil, errors.New("source offline")
	case "sh688825":
		return testBars()[:55], nil
	default:
		return testBars(), nil
	}
}

func TestHistoryPreflightChecksWholePoolWithoutExclusion(t *testing.T) {
	provider := &inspectionProvider{}
	request := testRequest()
	request.Technical.SlowMA = 60
	request.Tickers = []string{"sh688825", "sh600002", "sh600001"}
	request.Benchmark = "sh000300"
	report, err := NewDailyEngine(provider).InspectHistory(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if report.Ready || report.Available != 1 || report.Unavailable != 2 || len(report.Items) != 3 {
		t.Fatalf("wrong summary: %+v", report)
	}
	if report.Items[0].Symbol != "sh600001" || report.Items[1].Symbol != "sh600002" || report.Items[2].Symbol != "sh688825" {
		t.Fatal("unstable input order")
	}
	if report.Items[2].Bars != 55 || report.Items[2].MinimumBars != 62 || report.Items[2].Detail == "" || report.Items[2].FirstDate == "" {
		t.Fatal("short history evidence lost")
	}
	if report.Benchmark == nil || report.Benchmark.Ready || report.Benchmark.Detail == "" {
		t.Fatal("benchmark failure hidden")
	}
	if request.Tickers[0] != "sh688825" || len(provider.starts) != 4 {
		t.Fatal("input changed or failure stopped the remaining checks")
	}
	want := request.Start.AddDate(0, 0, -180)
	for _, start := range provider.starts {
		if !start.Equal(want) {
			t.Fatalf("wrong warmup: %s", start)
		}
	}
	_, runErr := NewDailyEngine(provider).Run(t.Context(), request)
	var historyErr *SymbolHistoryError
	if !errors.As(runErr, &historyErr) || historyErr.Symbol != report.Items[0].Symbol {
		t.Fatal("preflight disagrees with engine")
	}
}

func TestHistoryPreflightPreservesBenchmarkWarningPolicy(t *testing.T) {
	request := testRequest()
	request.Benchmark = "sh000300"
	report, err := NewDailyEngine(&inspectionProvider{}).InspectHistory(t.Context(), request)
	if err != nil || !report.Ready || report.Available != 1 || report.Benchmark.Ready {
		t.Fatalf("benchmark should warn separately: %+v %v", report, err)
	}
}

func TestHistoryPreflightCancellationDoesNotPublishPartialReadiness(t *testing.T) {
	provider := &inspectionProvider{started: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		report, err := NewDailyEngine(provider).InspectHistory(ctx, testRequest())
		if report.Ready || len(report.Items) > 0 {
			done <- errors.New("partial report published")
			return
		}
		done <- err
	}()
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("inspection not started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("inspection did not stop")
	}
}
