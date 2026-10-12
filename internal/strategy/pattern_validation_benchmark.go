package strategy

import (
	"math"
	"sort"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const patternBenchmarkMethod = "paired-close-return-v1"
const patternExcessZeroTolerance = 1e-10

func comparePatternBenchmark(sample domain.PatternValidationSample, outcome domain.PatternValidationOutcome, benchmark []domain.DailyBar) *domain.PatternBenchmarkOutcome {
	result := &domain.PatternBenchmarkOutcome{State: outcome.State, Start: sample.ConfirmedOn, End: outcome.Through, Detail: outcome.Detail}
	if outcome.State != "mature" {
		return result
	}
	result.State = "unavailable"
	index := sort.Search(len(benchmark), func(i int) bool { return benchmark[i].Date >= sample.ConfirmedOn })
	if outcome.Horizon <= 0 || index == len(benchmark) || benchmark[index].Date != sample.ConfirmedOn || index+outcome.Horizon >= len(benchmark) || benchmark[index+outcome.Horizon].Date != outcome.Through {
		result.Detail = "基准起止日期与观察窗口不一致"
		return result
	}
	first, last := benchmark[index].Close, benchmark[index+outcome.Horizon].Close
	if !positiveMonitorPrice(first) || !positiveMonitorPrice(last) || outcome.PriceReturn == nil || math.IsNaN(*outcome.PriceReturn) || math.IsInf(*outcome.PriceReturn, 0) {
		result.Detail = "基准或个股收益缺少有效价格"
		return result
	}
	benchmarkReturn := (last/first - 1) * 100
	excess := *outcome.PriceReturn - benchmarkReturn
	// Suppress floating-point ties, not economically small positive returns.
	if math.Abs(excess) < patternExcessZeroTolerance {
		excess = 0
	}
	direction := excess
	if sample.Bias == "bearish" {
		direction = -excess
	}
	if chartNumber(benchmarkReturn) == nil || chartNumber(excess) == nil || chartNumber(direction) == nil {
		result.Detail = "基准或超额收益超出有效范围"
		return result
	}
	result.State, result.Detail = "matched", ""
	result.Return, result.ExcessReturn, result.DirectionExcessReturn = chartNumber(benchmarkReturn), chartNumber(excess), chartNumber(direction)
	return result
}

func validPatternBenchmark(comparison *domain.PatternBenchmarkOutcome) bool {
	if comparison == nil || comparison.State != "matched" {
		return false
	}
	for _, value := range []*float64{comparison.Return, comparison.ExcessReturn, comparison.DirectionExcessReturn} {
		if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
			return false
		}
	}
	return true
}
