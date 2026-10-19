package backtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

type StabilityWindow struct {
	ID         string            `json:"id"`
	Normal     StabilityAnalysis `json:"normal"`
	DoubleCost StabilityAnalysis `json:"double_cost"`
}

type StabilityGroupWindow struct {
	ID         string         `json:"id"`
	Normal     StabilityStats `json:"normal"`
	DoubleCost StabilityStats `json:"double_cost"`
}

type StabilityGroupCheck struct {
	Dimension        string                 `json:"dimension"`
	Key              string                 `json:"key"`
	Label            string                 `json:"label"`
	Normal           StabilityStats         `json:"normal"`
	DoubleCost       StabilityStats         `json:"double_cost"`
	Windows          []StabilityGroupWindow `json:"windows"`
	SufficientSample bool                   `json:"sufficient_sample"`
	AllNegative      bool                   `json:"all_validation_windows_negative_both_costs"`
	AllPositive      bool                   `json:"all_validation_windows_positive_both_costs"`
}

type StabilityLeaveOneOut struct {
	OmittedWindow       string  `json:"omitted_window"`
	NormalAverageReturn float64 `json:"normal_average_return_percent"`
	StressAverageReturn float64 `json:"double_cost_average_return_percent"`
	NormalPositiveRatio float64 `json:"normal_positive_fold_ratio"`
	StressPositiveRatio float64 `json:"double_cost_positive_fold_ratio"`
}

type StabilityReviewResult struct {
	Completed          bool                   `json:"completed"`
	SourceID           string                 `json:"source_id"`
	CandidateID        string                 `json:"candidate_id"`
	GeneratedAt        time.Time              `json:"generated_at"`
	BaselineReproduced bool                   `json:"baseline_reproduced"`
	Baseline           PositionStudyVariant   `json:"baseline"`
	Windows            []StabilityWindow      `json:"windows"`
	NormalValidation   StabilityAnalysis      `json:"normal_validation"`
	StressValidation   StabilityAnalysis      `json:"double_cost_validation"`
	GroupChecks        []StabilityGroupCheck  `json:"group_checks"`
	LeaveOneOut        []StabilityLeaveOneOut `json:"leave_one_window_out"`
	PromotionAllowed   bool                   `json:"promotion_allowed"`
	Warnings           []string               `json:"warnings"`
}

func RunStabilityReview(ctx context.Context, engine Engine, provider DailyBarProvider, sourceID, candidateID string, reference PositionStudyVariant, progress func(string)) (StabilityReviewResult, error) {
	result := StabilityReviewResult{SourceID: sourceID, CandidateID: candidateID, GeneratedAt: time.Now(), Baseline: reference, Windows: []StabilityWindow{}, GroupChecks: []StabilityGroupCheck{}, LeaveOneOut: []StabilityLeaveOneOut{}, Warnings: []string{
		"只诊断原策略，不筛选股票、不调整阈值、不修改影子账户或自动晋级",
		"已见近期区间单独报告，不参与验证组稳定性或集中度汇总",
		"日历年按每日盯市归因；各窗口独立重置本金，金额合计不是连续账户年度收益率",
		"减去最大赢家或股票贡献只是事后账面敏感性，不是删除交易后的组合回测",
		"信号分组只使用原始入场信号收盘信息，分组边界预先固定；多组比较仍有选择偏差",
		"同向判断要求至少3个验证窗、每窗每种成本至少5笔平仓、合计至少20笔；不代表统计显著或未来有效",
		"分组盈亏含未平仓盯市；跨窗同向判断仅用已平仓盈亏，未完成持仓不算胜负",
		"静态池、不复权日K、固定历史费用及期末强制清算限制仍在，不能作为正式晋级证据",
	}}
	if engine == nil || provider == nil || len(reference.Windows) < 2 || !finiteValues(reference.MaxPosition) || reference.MaxPosition <= 0 || reference.MaxPosition > 1 {
		return result, fmt.Errorf("稳定性诊断需要完整冻结仓位基线和至少两个验证窗")
	}
	windows := append(append([]PositionStudyWindow{}, reference.Windows...), reference.Diagnostic)
	seen := map[string]bool{}
	for _, window := range windows {
		if window.ID == "" || seen[window.ID] {
			return result, fmt.Errorf("诊断窗口标识为空或重复")
		}
		seen[window.ID] = true
		for _, baseline := range []Result{window.Normal, window.DoubleCost} {
			r := baseline.Request
			if !r.NoFutureData || r.Adjustment != AdjustmentNone || r.EntryConfirmation != "" || r.EntryMarketFilter != "" || r.ExitProtection != "" || r.Technical.EffectiveEntryMode() != EntryModeReclaim || r.Technical.MaxPosition != reference.MaxPosition {
				return result, fmt.Errorf("只接受未附加过滤、确认或退出保护的原趋势重返基线")
			}
		}
		if progress != nil {
			progress(window.ID + "：复现常规及双倍成本基线")
		}
		for _, baseline := range []Result{window.Normal, window.DoubleCost} {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			replayed, err := engine.Run(ctx, baseline.Request)
			if err != nil {
				return result, err
			}
			if !sameConfirmationBaseline(baseline, replayed) {
				return result, fmt.Errorf("%s 基线成交或净值未完整复现", window.ID)
			}
		}
	}
	result.BaselineReproduced = true
	for _, window := range windows {
		if progress != nil {
			progress(window.ID + "：逐股、逐年与信号分组归因")
		}
		normal, err := analyzeStabilityRun(ctx, provider, window.Normal, window.ID)
		if err != nil {
			return result, fmt.Errorf("%s 常规: %w", window.ID, err)
		}
		stress, err := analyzeStabilityRun(ctx, provider, window.DoubleCost, window.ID)
		if err != nil {
			return result, fmt.Errorf("%s 双倍成本: %w", window.ID, err)
		}
		result.Windows = append(result.Windows, StabilityWindow{ID: window.ID, Normal: normal, DoubleCost: stress})
	}
	validation := result.Windows[:len(reference.Windows)]
	result.NormalValidation, result.StressValidation = aggregateStabilityWindows(validation, false), aggregateStabilityWindows(validation, true)
	result.GroupChecks = stabilityGroupChecks(validation, result.NormalValidation, result.StressValidation)
	for i, omitted := range reference.Windows {
		item := StabilityLeaveOneOut{OmittedWindow: omitted.ID}
		for j, window := range reference.Windows {
			if i == j {
				continue
			}
			item.NormalAverageReturn += window.Normal.Metrics.TotalReturn
			item.StressAverageReturn += window.DoubleCost.Metrics.TotalReturn
			if window.Normal.Metrics.TotalReturn > 0 {
				item.NormalPositiveRatio++
			}
			if window.DoubleCost.Metrics.TotalReturn > 0 {
				item.StressPositiveRatio++
			}
		}
		n := float64(len(reference.Windows) - 1)
		item.NormalAverageReturn /= n
		item.StressAverageReturn /= n
		item.NormalPositiveRatio /= n
		item.StressPositiveRatio /= n
		result.LeaveOneOut = append(result.LeaveOneOut, item)
	}
	result.Completed = true
	return result, nil
}

func aggregateStabilityWindows(windows []StabilityWindow, stress bool) StabilityAnalysis {
	rows, contributions := []StabilityRow{}, []StabilityContribution{}
	names, years := map[string]string{}, map[string]*StabilityYear{}
	maxError := 0.0
	for _, window := range windows {
		run := window.Normal
		if stress {
			run = window.DoubleCost
		}
		rows = append(rows, run.Rows...)
		contributions = append(contributions, run.Contributions...)
		maxError = math.Max(maxError, run.MaxDailyError)
		for _, symbol := range run.Symbols {
			names[symbol.Key] = symbol.Label
		}
		for _, year := range run.Years {
			if years[year.Year] == nil {
				years[year.Year] = &StabilityYear{Year: year.Year, Start: year.Start, End: year.End, Windows: []string{}}
			}
			total := years[year.Year]
			total.Start, total.End = min(total.Start, year.Start), max(total.End, year.End)
			total.Profit += year.Profit
			total.Days += year.Days
			total.Windows = append(total.Windows, year.Windows...)
		}
	}
	result := summarizeStabilityRows(rows, names)
	result.Contributions, result.MaxDailyError, result.Years = contributions, maxError, []StabilityYear{}
	for _, year := range years {
		result.Years = append(result.Years, *year)
	}
	sort.Slice(result.Years, func(i, j int) bool { return result.Years[i].Year < result.Years[j].Year })
	return result
}

func stabilityGroupChecks(windows []StabilityWindow, normal, stress StabilityAnalysis) []StabilityGroupCheck {
	groups := func(a StabilityAnalysis) map[string]StabilityGroup {
		result := map[string]StabilityGroup{}
		for _, group := range a.Symbols {
			result["symbol|"+group.Key] = group
		}
		for _, dimension := range a.Signals {
			for _, group := range dimension.Groups {
				result[dimension.Key+"|"+group.Key] = group
			}
		}
		return result
	}
	type pair struct{ normal, stress map[string]StabilityGroup }
	byWindow := make([]pair, len(windows))
	for i, window := range windows {
		byWindow[i] = pair{groups(window.Normal), groups(window.DoubleCost)}
	}
	totalsNormal, totalsStress := groups(normal), groups(stress)
	result := []StabilityGroupCheck{}
	dimensions := append([]StabilityDimension{{Key: "symbol", Groups: normal.Symbols}}, normal.Signals...)
	for _, dimension := range dimensions {
		for _, group := range dimension.Groups {
			key := dimension.Key + "|" + group.Key
			item := StabilityGroupCheck{Dimension: dimension.Key, Key: group.Key, Label: group.Label, Normal: totalsNormal[key].Stats, DoubleCost: totalsStress[key].Stats, Windows: []StabilityGroupWindow{}, SufficientSample: len(windows) >= 3, AllNegative: true, AllPositive: true}
			for i, window := range windows {
				n, s := byWindow[i].normal[key].Stats, byWindow[i].stress[key].Stats
				item.Windows = append(item.Windows, StabilityGroupWindow{ID: window.ID, Normal: n, DoubleCost: s})
				item.SufficientSample = item.SufficientSample && n.ClosedTrades >= 5 && s.ClosedTrades >= 5
				item.AllNegative = item.AllNegative && n.RealizedProfit < 0 && s.RealizedProfit < 0
				item.AllPositive = item.AllPositive && n.RealizedProfit > 0 && s.RealizedProfit > 0
			}
			item.SufficientSample = item.SufficientSample && item.Normal.ClosedTrades >= 20 && item.DoubleCost.ClosedTrades >= 20
			item.AllNegative = item.AllNegative && item.SufficientSample
			item.AllPositive = item.AllPositive && item.SufficientSample
			result = append(result, item)
		}
	}
	return result
}
