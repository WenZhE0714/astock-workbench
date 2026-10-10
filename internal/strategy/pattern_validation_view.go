package strategy

import (
	"fmt"
	"sort"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type PatternValidationFilter struct {
	Pattern string `json:"pattern"`
	Bias    string `json:"bias"`
	Regime  string `json:"regime"`
	State   string `json:"state"`
	Horizon int    `json:"horizon"`
	Offset  int    `json:"offset"`
	Limit   int    `json:"limit"`
}

type PatternValidationView struct {
	Report  domain.PatternValidationReport   `json:"report"`
	Filter  PatternValidationFilter          `json:"filter"`
	Totals  domain.PatternValidationTotals   `json:"totals"`
	Groups  []domain.PatternValidationGroup  `json:"groups"`
	Samples []domain.PatternValidationSample `json:"samples"`
	Total   int                              `json:"total"`
}

func PatternValidationResult(report domain.PatternValidationReport, filter PatternValidationFilter) (PatternValidationView, error) {
	if filter.Horizon == 0 {
		filter.Horizon = 5
	}
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if (filter.Horizon != 5 && filter.Horizon != 10 && filter.Horizon != 20) || filter.Offset < 0 || filter.Limit < 1 || filter.Limit > 100 {
		return PatternValidationView{}, fmt.Errorf("窗口须为5、10或20，分页数量须为1至100")
	}
	view := PatternValidationView{Report: report, Filter: filter, Groups: []domain.PatternValidationGroup{}, Samples: []domain.PatternValidationSample{}}
	view.Report.Inputs, view.Report.Samples = nil, nil
	selected := []domain.PatternValidationSample{}
	grouped := map[string][]domain.PatternValidationSample{}
	for _, sample := range report.Samples {
		if (filter.Pattern != "" && sample.PatternID != filter.Pattern) || (filter.Bias != "" && sample.Bias != filter.Bias) || (filter.Regime != "" && sample.Regime != filter.Regime) || (filter.State != "" && sample.State != filter.State) {
			continue
		}
		selected = append(selected, sample)
		key := sample.PatternID + "|" + sample.Bias + "|" + sample.Regime
		grouped[key] = append(grouped[key], sample)
	}
	view.Totals, view.Total = SummarizePatternValidation(selected, filter.Horizon), len(selected)
	keys := []string{}
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		samples := grouped[key]
		first := samples[0]
		view.Groups = append(view.Groups, domain.PatternValidationGroup{PatternID: first.PatternID, Name: first.Name, Bias: first.Bias, Regime: first.Regime, Totals: SummarizePatternValidation(samples, filter.Horizon)})
	}
	start := min(filter.Offset, len(selected))
	end := min(start+filter.Limit, len(selected))
	view.Samples = append(view.Samples, selected[start:end]...)
	return view, nil
}

func SummarizePatternValidation(samples []domain.PatternValidationSample, horizon int) domain.PatternValidationTotals {
	totals := domain.PatternValidationTotals{Samples: len(samples)}
	price, direction, favorable, adverse, failed := 0.0, 0.0, 0.0, 0.0, 0.0
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
			}
		}
	}
	if totals.Mature > 0 {
		count := float64(totals.Mature)
		totals.AveragePriceReturn, totals.AverageDirectionReturn = chartNumber(price/count), chartNumber(direction/count)
		totals.AverageFavorable, totals.AverageAdverse, totals.InvalidationRate = chartNumber(favorable/count), chartNumber(adverse/count), chartNumber(failed/count*100)
	}
	return totals
}
