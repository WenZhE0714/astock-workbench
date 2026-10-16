package backtest

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/wenzhe/astock-workbench/internal/marketregime"
)

type RegimeStudyVariant struct {
	Filter string `json:"entry_market_filter"`
	PositionStudyVariant
}

type RegimeOpportunityImpact struct {
	BlockedAttempts     int     `json:"blocked_attempts"`
	AvoidedLosses       int     `json:"avoided_losses"`
	AvoidedLossAmount   float64 `json:"avoided_loss_amount"`
	MissedWins          int     `json:"missed_wins"`
	MissedProfitAmount  float64 `json:"missed_profit_amount"`
	OtherRemovedEntries int     `json:"other_removed_entries"`
	NewEntries          int     `json:"new_entries"`
	BaselineEntries     int     `json:"baseline_entries"`
	FilteredEntries     int     `json:"filtered_entries"`
	BaselineExposure    float64 `json:"baseline_average_exposure_percent"`
	FilteredExposure    float64 `json:"filtered_average_exposure_percent"`
	EquityDifference    float64 `json:"equity_difference"`
}

type RegimeStudyComparison struct {
	ID         string                  `json:"id"`
	Normal     RegimeOpportunityImpact `json:"normal"`
	DoubleCost RegimeOpportunityImpact `json:"double_cost"`
}

type RegimeStudyResult struct {
	Completed           bool                    `json:"completed"`
	SourceID            string                  `json:"source_id"`
	CandidateID         string                  `json:"candidate_id"`
	MaxPosition         float64                 `json:"max_position"`
	GeneratedAt         time.Time               `json:"generated_at"`
	BaselineReproduced  bool                    `json:"baseline_reproduced"`
	BaselineDifferences []string                `json:"baseline_differences"`
	Variants            []RegimeStudyVariant    `json:"variants"`
	Comparisons         []RegimeStudyComparison `json:"comparisons"`
	PromotionAllowed    bool                    `json:"promotion_allowed"`
	Warnings            []string                `json:"warnings"`
}

func RunRegimeStudy(ctx context.Context, engine Engine, source ContinuousOptimizationResult, candidateID string, reference PositionStudyVariant, progress func(string)) (RegimeStudyResult, error) {
	result := RegimeStudyResult{SourceID: source.ID, CandidateID: candidateID, MaxPosition: reference.MaxPosition, GeneratedAt: time.Now(), BaselineDifferences: []string{}, Variants: []RegimeStudyVariant{}, Comparisons: []RegimeStudyComparison{}, Warnings: []string{"近期区间为已见数据诊断，不是新的最终留出", "避开亏损与错过盈利只关联基线成交，不能与净值差重复相加", "本研究不自动晋级或修改影子账户"}}
	if engine == nil || len(source.Request.Folds) < 2 || len(reference.Windows) != len(source.Request.Folds) || !source.Request.BaseRequest.NoFutureData || source.Request.BaseRequest.EntryMarketFilter != "" {
		return result, fmt.Errorf("市场过滤对照需要完整且未过滤的固定研究基线")
	}
	var parameters TechnicalParameters
	found := false
	for _, c := range source.Candidates {
		if c.Proposal.ID == candidateID {
			parameters = c.Proposal.Parameters
			found = true
			break
		}
	}
	if !found {
		return result, fmt.Errorf("候选不存在")
	}
	for _, filter := range []string{"", EntryMarketNonBear} {
		variant := RegimeStudyVariant{Filter: filter, PositionStudyVariant: PositionStudyVariant{MaxPosition: reference.MaxPosition, Windows: []PositionStudyWindow{}}}
		base := source.Request.BaseRequest
		base.EntryMarketFilter = filter
		label := "基线"
		if filter != "" {
			label = "非熊市开仓"
		}
		for _, fold := range source.Request.Folds {
			if progress != nil {
				progress(label + "：" + fold.ID + "及双倍成本")
			}
			window, err := runPositionStudyWindow(ctx, engine, base, parameters, reference.MaxPosition, fold.ID, fold.Validate)
			if err != nil {
				return result, err
			}
			variant.Windows = append(variant.Windows, window)
			if filter == "" {
				matched := false
				for _, prior := range reference.Windows {
					if prior.ID == fold.ID {
						matched = sameStudyMetrics(prior.Normal, window.Normal) && sameStudyMetrics(prior.DoubleCost, window.DoubleCost)
						break
					}
				}
				if !matched {
					result.BaselineDifferences = append(result.BaselineDifferences, fold.ID+"基线或成本压力未复现")
				}
			}
		}
		variant.Normal = summarizePositionWindows(variant.Windows, source.Request, false)
		variant.DoubleCost = summarizePositionWindows(variant.Windows, source.Request, true)
		if progress != nil {
			progress(label + "：已见近期区间")
		}
		window, err := runPositionStudyWindow(ctx, engine, base, parameters, reference.MaxPosition, "seen-period", source.Request.Holdout)
		if err != nil {
			return result, err
		}
		variant.Diagnostic = window
		if filter == "" && (!sameStudyMetrics(reference.Diagnostic.Normal, window.Normal) || !sameStudyMetrics(reference.Diagnostic.DoubleCost, window.DoubleCost)) {
			result.BaselineDifferences = append(result.BaselineDifferences, "近期诊断基线未复现")
		}
		result.Variants = append(result.Variants, variant)
	}
	baseline := append(append([]PositionStudyWindow{}, result.Variants[0].Windows...), result.Variants[0].Diagnostic)
	filtered := append(append([]PositionStudyWindow{}, result.Variants[1].Windows...), result.Variants[1].Diagnostic)
	for i, window := range baseline {
		result.Comparisons = append(result.Comparisons, RegimeStudyComparison{ID: window.ID, Normal: regimeOpportunityImpact(window.Normal, filtered[i].Normal), DoubleCost: regimeOpportunityImpact(window.DoubleCost, filtered[i].DoubleCost)})
	}
	result.BaselineReproduced = len(result.BaselineDifferences) == 0
	result.Completed = true
	if !source.Request.BaseRequest.PointInTimePool {
		result.Warnings = append(result.Warnings, "静态股票池存在幸存者偏差")
	}
	return result, nil
}

func sameStudyMetrics(a, b Result) bool {
	return math.Abs(a.Metrics.TotalReturn-b.Metrics.TotalReturn) < .000001 && math.Abs(a.Metrics.MaxDrawdown-b.Metrics.MaxDrawdown) < .000001 && a.Metrics.Trades == b.Metrics.Trades && math.Abs(a.Metrics.TotalFees-b.Metrics.TotalFees) < .01 && len(a.OpenPositions) == len(b.OpenPositions)
}

func regimeOpportunityImpact(baseline, filtered Result) RegimeOpportunityImpact {
	result := RegimeOpportunityImpact{BlockedAttempts: len(filtered.BlockedEntries), EquityDifference: filtered.Metrics.FinalEquity - baseline.Metrics.FinalEquity}
	key := func(symbol, date string) string { return symbol + "|" + date }
	blocked := map[string]bool{}
	for _, event := range filtered.BlockedEntries {
		blocked[key(event.Symbol, event.SignalDate)] = true
	}
	entries := func(run Result) map[string]bool {
		result := map[string]bool{}
		for _, trade := range run.Trades {
			result[key(trade.Symbol, trade.EntrySignal.Date)] = true
		}
		for _, position := range run.OpenPositions {
			result[key(position.Symbol, position.EntrySignal.Date)] = true
		}
		return result
	}
	a, b := entries(baseline), entries(filtered)
	result.BaselineEntries, result.FilteredEntries = len(a), len(b)
	for k := range b {
		if !a[k] {
			result.NewEntries++
		}
	}
	for k := range a {
		if !b[k] && !blocked[k] {
			result.OtherRemovedEntries++
		}
	}
	for _, trade := range baseline.Trades {
		k := key(trade.Symbol, trade.EntrySignal.Date)
		if b[k] || !blocked[k] {
			continue
		}
		if trade.NetProfit > 0 {
			result.MissedWins++
			result.MissedProfitAmount += trade.NetProfit
		}
		if trade.NetProfit < 0 {
			result.AvoidedLosses++
			result.AvoidedLossAmount -= trade.NetProfit
		}
	}
	exposure := func(run Result) float64 {
		value := 0.0
		for _, day := range run.Equity {
			if day.Equity > 0 {
				value += day.Positions / day.Equity * 100
			}
		}
		if len(run.Equity) == 0 {
			return 0
		}
		return value / float64(len(run.Equity))
	}
	result.BaselineExposure, result.FilteredExposure = exposure(baseline), exposure(filtered)
	return result
}

func hasMissingEntryBenchmark(run Result) bool {
	for _, entry := range run.BlockedEntries {
		if entry.Regime == marketregime.Insufficient {
			return true
		}
	}
	return false
}
