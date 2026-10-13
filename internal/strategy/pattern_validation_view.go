package strategy

import (
	"fmt"
	"sort"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type PatternValidationFilter struct {
	Period  string `json:"period"`
	Pattern string `json:"pattern"`
	Bias    string `json:"bias"`
	Regime  string `json:"regime"`
	State   string `json:"state"`
	Action  string `json:"action"`
	Horizon int    `json:"horizon"`
	Offset  int    `json:"offset"`
	Limit   int    `json:"limit"`
}

type PatternValidationView struct {
	Report     domain.PatternValidationReport   `json:"report"`
	Filter     PatternValidationFilter          `json:"filter"`
	Totals     domain.PatternValidationTotals   `json:"totals"`
	Groups     []domain.PatternValidationGroup  `json:"groups"`
	Samples    []domain.PatternValidationSample `json:"samples"`
	Total      int                              `json:"total"`
	Periods    []domain.PatternPeriodSummary    `json:"periods,omitempty"`
	Stocks     []domain.PatternStockSummary     `json:"stocks,omitempty"`
	Industries []domain.PatternIndustrySummary  `json:"industries,omitempty"`
}

func PatternValidationResult(report domain.PatternValidationReport, filter PatternValidationFilter) (PatternValidationView, error) {
	if filter.Period != "" && filter.Period != "research" && filter.Period != "validation" {
		return PatternValidationView{}, fmt.Errorf("无效评估时段")
	}
	if report.Study == nil && filter.Period != "" {
		return PatternValidationView{}, fmt.Errorf("此归档未冻结分期配置")
	}
	if report.Study != nil && filter.Period == "" {
		filter.Period = "validation"
	}
	if filter.Horizon == 0 {
		filter.Horizon = 5
	}
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if (filter.Horizon != 5 && filter.Horizon != 10 && filter.Horizon != 20) || filter.Offset < 0 || filter.Limit < 1 || filter.Limit > 100 {
		return PatternValidationView{}, fmt.Errorf("窗口须为5、10或20，分页数量须为1至100")
	}
	if filter.Action != "" && filter.Action != "clear" && filter.Action != "affected" && filter.Action != "unverified" && filter.Action != "legacy" && filter.Action != "adjusted" {
		return PatternValidationView{}, fmt.Errorf("无效公司行动筛选")
	}
	view := PatternValidationView{Report: report, Filter: filter, Groups: []domain.PatternValidationGroup{}, Samples: []domain.PatternValidationSample{}}
	view.Report.Inputs, view.Report.Samples = nil, nil
	if report.Study != nil && filter.Period == "research" {
		evidence := report.Study.Research
		view.Report.Cutoff, view.Report.Coverage, view.Report.ActionCoverage, view.Report.Adjustments = evidence.Cutoff, evidence.Coverage, evidence.ActionCoverage, evidence.Adjustments
		view.Report.InputHash = evidence.InputHash
	}
	selected := []domain.PatternValidationSample{}
	periodSamples := map[string][]domain.PatternValidationSample{"research": {}, "validation": {}}
	for _, sample := range report.Samples {
		if filter.Action != "" && filter.Action != patternActionState(sample, filter.Horizon) {
			continue
		}
		if (filter.Pattern != "" && sample.PatternID != filter.Pattern) || (filter.Bias != "" && sample.Bias != filter.Bias) || (filter.Regime != "" && sample.Regime != filter.Regime) || (filter.State != "" && sample.State != filter.State) {
			continue
		}
		if report.Study != nil {
			periodSamples[sample.Period] = append(periodSamples[sample.Period], sample)
			if sample.Period != filter.Period {
				continue
			}
		}
		selected = append(selected, sample)
	}
	if report.Study != nil {
		for _, period := range []string{"research", "validation"} {
			start, end := report.Request.Start, report.Study.Config.ResearchEnd
			if period == "validation" {
				start, end = report.Study.Config.ValidationStart, report.Study.Config.DataCutoff
			}
			items := periodSamples[period]
			view.Periods = append(view.Periods, domain.PatternPeriodSummary{Period: period, Start: start, End: end, Totals: SummarizePatternValidation(items, filter.Horizon), Groups: patternValidationGroups(items, filter.Horizon)})
		}
	}
	view.Totals, view.Total = SummarizePatternValidation(selected, filter.Horizon), len(selected)
	view.Groups = patternValidationGroups(selected, filter.Horizon)
	if report.Pool != nil {
		view.Stocks, view.Industries = patternPoolDistribution(*report.Pool, selected, filter.Horizon)
	}
	start := min(filter.Offset, len(selected))
	end := min(start+filter.Limit, len(selected))
	view.Samples = append(view.Samples, selected[start:end]...)
	return view, nil
}

func patternValidationGroups(samples []domain.PatternValidationSample, horizon int) []domain.PatternValidationGroup {
	groups := []domain.PatternValidationGroup{}
	grouped := map[string][]domain.PatternValidationSample{}
	for _, sample := range samples {
		key := sample.PatternID + "|" + sample.Bias + "|" + sample.Regime
		grouped[key] = append(grouped[key], sample)
	}
	keys := []string{}
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		samples := grouped[key]
		first := samples[0]
		groups = append(groups, domain.PatternValidationGroup{PatternID: first.PatternID, Name: first.Name, Bias: first.Bias, Regime: first.Regime, Totals: SummarizePatternValidation(samples, horizon)})
	}
	return groups
}

func SummarizePatternValidation(samples []domain.PatternValidationSample, horizon int) domain.PatternValidationTotals {
	totals := domain.PatternValidationTotals{Samples: len(samples)}
	price, direction, favorable, adverse, failed := 0.0, 0.0, 0.0, 0.0, 0.0
	pairedPrice, benchmark, excess, directionExcess := 0.0, 0.0, 0.0, 0.0
	for _, sample := range samples {
		if sample.ConfirmedOn != "" {
			totals.Confirmed++
		} else if sample.InvalidatedOn != "" {
			totals.InvalidatedBeforeConfirmation++
		}
		for _, outcome := range sample.Outcomes {
			if outcome.Horizon != horizon {
				continue
			}
			if outcome.PeriodCheck != nil && outcome.PeriodCheck.State == "excluded" {
				totals.BoundaryExcluded++
				continue
			}
			if outcome.ActionCheck != nil {
				switch outcome.ActionCheck.State {
				case "affected":
					totals.ActionAffected++
					continue
				case "clear":
				case "adjusted":
					totals.ActionAdjusted++
				default:
					totals.ActionUnverified++
					continue
				}
			}
			switch outcome.State {
			case "pending":
				totals.Pending++
			case "unavailable":
				totals.Unavailable++
			case "mature":
				if outcome.PriceReturn == nil || outcome.DirectionReturn == nil || outcome.Favorable == nil || outcome.Adverse == nil || outcome.Invalidated == nil {
					totals.Unavailable++
					continue
				}
				totals.Mature++
				price += *outcome.PriceReturn
				direction += *outcome.DirectionReturn
				favorable += *outcome.Favorable
				adverse += *outcome.Adverse
				if *outcome.Invalidated {
					failed++
				}
				if validPatternBenchmark(outcome.Benchmark) {
					comparison := outcome.Benchmark
					totals.BenchmarkPaired++
					pairedPrice += *outcome.PriceReturn
					benchmark += *comparison.Return
					excess += *comparison.ExcessReturn
					directionExcess += *comparison.DirectionExcessReturn
					if *comparison.ExcessReturn > 0 {
						totals.PositiveExcess++
					}
					if *comparison.DirectionExcessReturn > 0 {
						totals.PositiveDirectionExcess++
					}
				} else {
					totals.BenchmarkUnpaired++
				}
			}
		}
	}
	if totals.Mature > 0 {
		count := float64(totals.Mature)
		totals.AveragePriceReturn, totals.AverageDirectionReturn = chartNumber(price/count), chartNumber(direction/count)
		totals.AverageFavorable, totals.AverageAdverse, totals.InvalidationRate = chartNumber(favorable/count), chartNumber(adverse/count), chartNumber(failed/count*100)
	}
	if totals.BenchmarkPaired > 0 {
		count := float64(totals.BenchmarkPaired)
		totals.AveragePairedPriceReturn, totals.AverageBenchmarkReturn = chartNumber(pairedPrice/count), chartNumber(benchmark/count)
		totals.AverageExcessReturn, totals.AverageDirectionExcessReturn = chartNumber(excess/count), chartNumber(directionExcess/count)
		totals.PositiveExcessRate, totals.PositiveDirectionExcessRate = chartNumber(float64(totals.PositiveExcess)/count*100), chartNumber(float64(totals.PositiveDirectionExcess)/count*100)
	}
	return totals
}
