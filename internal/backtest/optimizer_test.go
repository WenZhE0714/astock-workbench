package backtest

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type optimizerEngineMock struct {
	calls []Request
}

func (mock *optimizerEngineMock) Run(_ context.Context, request Request) (Result, error) {
	mock.calls = append(mock.calls, request)
	metrics := Metrics{Trades: 8, FinalEquity: request.InitialCash}
	month := request.Start.Month()
	if month == time.January {
		metrics.TotalReturn = float64(request.Technical.FastMA)
		metrics.AnnualizedReturn = float64(request.Technical.FastMA)
		metrics.MaxDrawdown = -5
		metrics.Sharpe = 1
	} else if month == time.May {
		metrics.TotalReturn = float64(100 - request.Technical.FastMA)
		metrics.AnnualizedReturn = float64(100 - request.Technical.FastMA)
		metrics.MaxDrawdown = -6
		metrics.Sharpe = 1
	} else {
		// Deliberately favor the opposite parameter in OOS. Selection must
		// already be complete before this value exists.
		metrics.TotalReturn = float64(request.Technical.FastMA * 100)
		metrics.AnnualizedReturn = metrics.TotalReturn
		metrics.MaxDrawdown = -7
	}
	return Result{Request: request, Metrics: metrics}, nil
}

func optimizerTestRequest() OptimizationRequest {
	base := testRequest()
	base.Technical = DefaultTechnicalParameters()
	train := Period{Start: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2020, 3, 31, 0, 0, 0, 0, time.UTC)}
	validate := Period{Start: time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2020, 7, 31, 0, 0, 0, 0, time.UTC)}
	oos := Period{Start: time.Date(2020, 9, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2020, 12, 31, 0, 0, 0, 0, time.UTC)}
	request := DefaultOptimizationRequest(base, train, validate, oos)
	request.MaxCandidates = 7
	request.MinimumValidationTrades = 1
	request.MinimumValidationReturn = -100
	request.MaximumPerformanceGap = 100
	return request
}

func TestGenerateCandidatesIsBoundedDeterministicAndDeduplicated(t *testing.T) {
	baseline := DefaultTechnicalParameters()
	first := GenerateCandidates(baseline, 30)
	second := GenerateCandidates(baseline, 30)
	if len(first) != 30 || !reflect.DeepEqual(first, second) {
		t.Fatalf("candidate generation is not bounded and deterministic: %d %d", len(first), len(second))
	}
	seen := make(map[string]bool)
	foundBaseline := false
	for _, candidate := range first {
		key := parameterKey(candidate)
		if seen[key] {
			t.Fatalf("duplicate candidate %s", key)
		}
		seen[key] = true
		foundBaseline = foundBaseline || key == parameterKey(baseline)
	}
	if !foundBaseline {
		t.Fatal("baseline candidate must always be included")
	}
}

func TestOptimizerSelectsBeforeSingleOutOfSampleRun(t *testing.T) {
	engine := &optimizerEngineMock{}
	optimizer := NewOptimizer(engine)
	optimizer.now = func() time.Time { return time.Date(2024, 8, 12, 12, 0, 0, 0, time.UTC) }
	request := optimizerTestRequest()
	result, err := optimizer.Optimize(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected == nil || result.OutOfSample == nil {
		t.Fatalf("selection or OOS result missing: %#v", result)
	}
	wantCalls := request.MaxCandidates*2 + 1
	if len(engine.calls) != wantCalls {
		t.Fatalf("unexpected call count %d, want %d", len(engine.calls), wantCalls)
	}
	for index, call := range engine.calls[:len(engine.calls)-1] {
		if !call.End.Before(request.OutOfSample.Start) {
			t.Fatalf("call %d read OOS before selection: %#v", index, call)
		}
	}
	last := engine.calls[len(engine.calls)-1]
	if !last.Start.Equal(request.OutOfSample.Start) || !last.End.Equal(request.OutOfSample.End) {
		t.Fatalf("last call must be the only OOS run: %#v", last)
	}
	if last.Technical != result.Selected.Parameters {
		t.Fatalf("OOS did not use locked parameters: %#v %#v", last.Technical, result.Selected.Parameters)
	}
	bestValidation := result.Candidates[0]
	if bestValidation.ID != result.Selected.ID {
		t.Fatalf("selection must match train/validation ranking: %#v", result.Candidates)
	}
}

func TestOptimizerSkipsOutOfSampleWhenAllCandidatesFailGates(t *testing.T) {
	engine := &optimizerEngineMock{}
	request := optimizerTestRequest()
	request.MinimumValidationTrades = 100
	result, err := NewOptimizer(engine).Optimize(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != nil || result.OutOfSample != nil {
		t.Fatalf("rejected experiment must not run OOS: %#v", result)
	}
	if len(engine.calls) != request.MaxCandidates*2 {
		t.Fatalf("OOS was called despite all candidates failing: %d", len(engine.calls))
	}
	for _, candidate := range result.Candidates {
		if !candidate.Rejected || len(candidate.Reasons) == 0 {
			t.Fatalf("candidate gate evidence missing: %#v", candidate)
		}
	}
}

func TestCandidateGateRejectsDrawdownAndPerformanceGap(t *testing.T) {
	request := optimizerTestRequest()
	request.MaximumValidationDrawdown = 10
	request.MaximumPerformanceGap = 20
	_, reasons := scoreCandidate(request,
		Metrics{AnnualizedReturn: 50, Trades: 10},
		Metrics{AnnualizedReturn: 5, TotalReturn: 5, MaxDrawdown: -25, Trades: 10},
	)
	joined := strings.Join(reasons, "；")
	for _, expected := range []string{"验证最大回撤", "训练/验证年化差"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("hard gate missing %q: %s", expected, joined)
		}
	}
}

func TestValidateOptimizationRequestRejectsOverlappingPeriods(t *testing.T) {
	request := optimizerTestRequest()
	request.Validate.Start = request.Train.End
	if err := validateOptimizationRequest(request); err == nil {
		t.Fatal("overlapping periods should be rejected")
	}
	request = optimizerTestRequest()
	request.BaseRequest.Technical.FastMA = 100
	if err := validateOptimizationRequest(request); err == nil {
		t.Fatal("parameters outside controlled bounds should be rejected")
	}
	request = optimizerTestRequest()
	request.MinimumValidationReturn = math.NaN()
	if err := validateOptimizationRequest(request); err == nil {
		t.Fatal("non-finite gate values should be rejected")
	}
	request = optimizerTestRequest()
	request.BaseRequest.Technical.StopLoss = math.Inf(1)
	if err := validateOptimizationRequest(request); err == nil {
		t.Fatal("non-finite strategy parameters should be rejected")
	}
}

type countingBarProvider struct {
	calls int
	bars  []domain.DailyBar
}

func (provider *countingBarProvider) FetchDailyBarsRange(_ context.Context, _ string, _, _ time.Time, _ PriceAdjustment) ([]domain.DailyBar, error) {
	provider.calls++
	if len(provider.bars) == 0 {
		return nil, fmt.Errorf("no bars")
	}
	return append([]domain.DailyBar(nil), provider.bars...), nil
}

func TestCachingDailyBarProviderReusesContainingRange(t *testing.T) {
	upstream := &countingBarProvider{bars: testBars()}
	provider := NewCachingDailyBarProvider(upstream)
	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	if _, err := provider.FetchDailyBarsRange(context.Background(), "sh600519", start, end, AdjustmentNone); err != nil {
		t.Fatal(err)
	}
	bars, err := provider.FetchDailyBarsRange(context.Background(), "sh600519", start.AddDate(0, 0, 10), end.AddDate(0, 0, -10), AdjustmentNone)
	if err != nil {
		t.Fatal(err)
	}
	if upstream.calls != 1 || len(bars) == 0 {
		t.Fatalf("containing range was not reused: calls=%d bars=%d", upstream.calls, len(bars))
	}
}
