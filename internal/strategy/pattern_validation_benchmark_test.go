package strategy

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestPatternBenchmarkPairsExactDatesForBothDirections(t *testing.T) {
	for _, kind := range []string{"bull-flag", "bear-flag"} {
		t.Run(kind, func(t *testing.T) {
			bars, benchmark := validationFixture(kind)
			for i := range benchmark {
				price := 100.0
				if i >= 79 {
					price += float64(i-79) * .5
				}
				benchmark[i].Open, benchmark[i].Close, benchmark[i].High, benchmark[i].Low = price, price, price+1, price-1
			}
			report := validationReport(t, bars, benchmark, 114)
			sample := validationSample(t, report, kind)
			if report.Benchmark == nil || report.Benchmark.Symbol != "sh000300" || report.Benchmark.PriceBasis != "price-index" {
				t.Fatal("benchmark basis not archived")
			}
			for _, outcome := range sample.Outcomes {
				comparison := outcome.Benchmark
				market := float64(outcome.Horizon) * .5
				excess := float64(outcome.Horizon)*.2 - market
				direction := excess
				if kind == "bear-flag" {
					excess = -float64(outcome.Horizon)*.2 - market
					direction = -excess
				}
				if comparison == nil || comparison.State != "matched" || comparison.Start != sample.ConfirmedOn || comparison.End != outcome.Through || math.Abs(*comparison.Return-market) > 1e-10 || math.Abs(*comparison.ExcessReturn-excess) > 1e-10 || math.Abs(*comparison.DirectionExcessReturn-direction) > 1e-10 {
					t.Fatalf("incorrect pair: %+v", comparison)
				}
				view, err := PatternValidationResult(report, PatternValidationFilter{Pattern: kind, Horizon: outcome.Horizon})
				if err != nil || view.Totals.BenchmarkPaired != 1 || *view.Totals.PositiveExcessRate != 0 {
					t.Fatalf("incorrect paired summary: %+v %v", view.Totals, err)
				}
				if kind == "bear-flag" && *view.Totals.PositiveDirectionExcessRate != 100 {
					t.Fatal("bearish directional excess not counted")
				}
			}
		})
	}
}

func TestPatternBenchmarkMissingDatesAreNotReplaced(t *testing.T) {
	for _, kind := range []string{"stock-middle", "index-middle", "index-start", "index-end", "index-tail", "index-invalid", "no-index"} {
		t.Run(kind, func(t *testing.T) {
			bars, benchmark := validationFixture("bull-flag")
			switch kind {
			case "stock-middle":
				bars = append(bars[:82:82], bars[83:]...)
			case "index-middle":
				benchmark = append(benchmark[:82:82], benchmark[83:]...)
			case "index-start":
				benchmark = append(benchmark[:79:79], benchmark[80:]...)
			case "index-end":
				benchmark = append(benchmark[:84:84], benchmark[85:]...)
			case "index-tail":
				benchmark = benchmark[:84]
			case "index-invalid":
				benchmark[82].Close = 0
			case "no-index":
				benchmark = nil
			}
			report := validationReport(t, bars, benchmark, len(bars)-1)
			sample := validationSample(t, report, "bull-flag")
			for _, outcome := range sample.Outcomes {
				if outcome.State != "unavailable" || outcome.PriceReturn != nil || outcome.Benchmark == nil || outcome.Benchmark.State != "unavailable" || outcome.Benchmark.Return != nil || outcome.Benchmark.ExcessReturn != nil {
					t.Fatalf("missing dates became valid performance: %+v", outcome)
				}
			}
			if kind == "index-middle" && !strings.Contains(sample.Outcomes[0].Detail, "基准缺少") {
				t.Fatal("missing index date not explained")
			}
		})
	}
}

func TestPatternBenchmarkPendingAndZeroRemainDistinct(t *testing.T) {
	bars, benchmark := validationFixture("bull-flag")
	for i := 79; i < len(benchmark); i++ {
		benchmark[i].Open, benchmark[i].High, benchmark[i].Low, benchmark[i].Close = bars[i].Open, bars[i].High, bars[i].Low, bars[i].Close
	}
	report := validationReport(t, bars, benchmark, 84)
	sample := validationSample(t, report, "bull-flag")
	if sample.Outcomes[0].Benchmark.ExcessReturn == nil || *sample.Outcomes[0].Benchmark.ExcessReturn != 0 || sample.Outcomes[1].Benchmark.State != "pending" || sample.Outcomes[1].Benchmark.ExcessReturn != nil {
		t.Fatal("zero excess confused with missing data")
	}
	totals := SummarizePatternValidation([]domain.PatternValidationSample{sample}, 5)
	if totals.BenchmarkPaired != 1 || *totals.PositiveExcessRate != 0 {
		t.Fatal("tie counted as a positive excess")
	}
	nearTie := sample.Outcomes[0]
	nearTie.PriceReturn = chartNumber(*nearTie.PriceReturn + 1e-12)
	if got := comparePatternBenchmark(sample, nearTie, benchmark); *got.ExcessReturn != 0 {
		t.Fatal("roundoff tie counted as excess")
	}
	if totals := SummarizePatternValidation([]domain.PatternValidationSample{sample}, 10); totals.BenchmarkPaired != 0 || totals.AverageExcessReturn != nil {
		t.Fatal("pending window entered paired summary")
	}
}

func TestPatternBenchmarkSummaryUsesOnlyEligiblePairs(t *testing.T) {
	makeSample := func(price, index float64, action string, paired bool) domain.PatternValidationSample {
		outcome := domain.PatternValidationOutcome{Horizon: 5, State: "mature", PriceReturn: chartNumber(price), DirectionReturn: chartNumber(price), Favorable: chartNumber(1), Adverse: chartNumber(-1), Invalidated: new(bool), ActionCheck: &domain.PatternActionCheck{State: action}}
		if paired {
			outcome.Benchmark = &domain.PatternBenchmarkOutcome{State: "matched", Return: chartNumber(index), ExcessReturn: chartNumber(price - index), DirectionExcessReturn: chartNumber(price - index)}
		}
		return domain.PatternValidationSample{ConfirmedOn: "2026-01-01", Outcomes: []domain.PatternValidationOutcome{outcome}}
	}
	samples := []domain.PatternValidationSample{makeSample(5, 2, "clear", true), makeSample(-1, -1, "adjusted", true), makeSample(90, 0, "clear", false), makeSample(100, 0, "affected", true), makeSample(100, 0, "unverified", true)}
	totals := SummarizePatternValidation(samples, 5)
	if totals.Mature != 3 || totals.BenchmarkPaired != 2 || totals.BenchmarkUnpaired != 1 || totals.PositiveExcess != 1 || *totals.PositiveExcessRate != 50 || *totals.AveragePairedPriceReturn != 2 || *totals.AverageBenchmarkReturn != .5 || *totals.AverageExcessReturn != 1.5 {
		t.Fatalf("inconsistent paired denominator: %+v", totals)
	}
	if totals.ActionAffected != 1 || totals.ActionUnverified != 1 {
		t.Fatal("event isolation bypassed")
	}
	if totals := SummarizePatternValidation(nil, 5); totals.AverageBenchmarkReturn != nil || totals.PositiveExcessRate != nil {
		t.Fatal("empty sample fabricated zero statistics")
	}
}

func TestPatternBenchmarkArchiveReplaysAndExcludesFuture(t *testing.T) {
	bars, benchmark := validationFixture("bull-flag")
	report := validationReport(t, bars, benchmark, 84)
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var archive domain.PatternValidationReport
	if err = json.Unmarshal(encoded, &archive); err != nil {
		t.Fatal(err)
	}
	replayed, err := BuildPatternValidation(context.Background(), archive.Request, archive.Inputs, chartTestTime(bars[114].Date, 16))
	if err != nil || replayed.InputHash != report.InputHash || !reflect.DeepEqual(report.Samples, replayed.Samples) {
		t.Fatalf("paired archive did not replay: %v", err)
	}
	for i := 85; i < len(benchmark); i++ {
		benchmark[i].Close = math.NaN()
	}
	after := validationReport(t, bars, benchmark, 84)
	if !reflect.DeepEqual(report.Samples, after.Samples) {
		t.Fatal("future index prices leaked into earlier outcomes")
	}
}
