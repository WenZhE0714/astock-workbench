package strategy

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const minimumPlaybookCompletedSamples = 5

type playbookAccumulator struct {
	metric             domain.TradePlaybookMetric
	returns            []float64
	disciplineFollowed int
}

func playbookReviewValid(plan domain.TradePlan, review domain.TradePlanReview) bool {
	return review.Version == 1 && review.PlanID == plan.ID && review.Symbol == plan.Symbol && review.Fingerprint == plan.Analysis.Fingerprint
}

func playbookPriceValid(value *float64) bool {
	return value != nil && *value > 0 && !math.IsNaN(*value) && !math.IsInf(*value, 0)
}

// Recompute the descriptive R from recorded fills and the frozen stop. Older
// derived values must not turn an incomplete review into a completed trade.
func playbookRevision(plan domain.TradePlan, input domain.TradePlanReviewRevision) (domain.TradePlanReviewRevision, string) {
	input.RealizedR = nil
	if input.ActualEntry == nil && input.ActualExit == nil {
		return input, ""
	}
	if (input.ExecutionStatus != "followed" && input.ExecutionStatus != "deviated") || !playbookPriceValid(input.ActualEntry) {
		input.ActualEntry, input.ActualExit = nil, nil
		return input, "成交价格与执行状态不一致，未计入成交统计"
	}
	if input.ActualExit == nil {
		return input, ""
	}
	if !playbookPriceValid(input.ActualExit) || (!input.ExitAt.IsZero() && (input.EntryAt.IsZero() || input.ExitAt.Before(input.EntryAt))) {
		return input, "退出价格或成交顺序无效，未计入R统计"
	}
	if plan.Structure.Plan == nil || !playbookPriceValid(&plan.Structure.Plan.Invalidation) {
		return input, "冻结失效位缺失，未计入R统计"
	}
	risk := *input.ActualEntry - plan.Structure.Plan.Invalidation
	if risk <= 0 {
		return input, "实际入场价不高于冻结失效位，未计入R统计"
	}
	value := (*input.ActualExit - *input.ActualEntry) / risk
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return input, "R倍数超出有效范围，未计入R统计"
	}
	input.RealizedR = &value
	return input, ""
}

func playbookApply(accumulator *playbookAccumulator, reviewed bool, revision domain.TradePlanReviewRevision) {
	accumulator.metric.Plans++
	if !reviewed {
		return
	}
	accumulator.metric.Reviewed++
	if playbookPriceValid(revision.ActualEntry) {
		accumulator.metric.Entered++
	}
	if revision.RealizedR != nil && !math.IsNaN(*revision.RealizedR) && !math.IsInf(*revision.RealizedR, 0) {
		accumulator.metric.Completed++
		accumulator.returns = append(accumulator.returns, *revision.RealizedR)
		if *revision.RealizedR > 0 {
			accumulator.metric.Wins++
		}
	}
	switch revision.Discipline {
	case "followed", "partial", "deviated":
		accumulator.metric.DisciplineSamples++
		if revision.Discipline == "followed" {
			accumulator.disciplineFollowed++
		}
	}
}

func playbookRounded(value float64) float64 {
	return math.Round(value*100) / 100
}

func playbookMeanMedian(values []float64) (*float64, *float64) {
	if len(values) == 0 {
		return nil, nil
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	total := 0.0
	for _, value := range ordered {
		total += value
	}
	mean := playbookRounded(total / float64(len(ordered)))
	median := ordered[len(ordered)/2]
	if len(ordered)%2 == 0 {
		median = (ordered[len(ordered)/2-1] + ordered[len(ordered)/2]) / 2
	}
	median = playbookRounded(median)
	return &mean, &median
}

func playbookPercent(numerator, denominator int) *float64 {
	if denominator <= 0 {
		return nil
	}
	value := playbookRounded(float64(numerator) / float64(denominator) * 100)
	return &value
}

func finalizePlaybookMetric(accumulator *playbookAccumulator) domain.TradePlaybookMetric {
	metric := accumulator.metric
	metric.AverageR, metric.MedianR = playbookMeanMedian(accumulator.returns)
	metric.WinRatePercent = playbookPercent(metric.Wins, metric.Completed)
	metric.DisciplineRatePercent = playbookPercent(accumulator.disciplineFollowed, metric.DisciplineSamples)
	metric.SampleSufficient = metric.Completed >= minimumPlaybookCompletedSamples
	return metric
}

func playbookStructureLabel(plan domain.TradePlan) string {
	if value := strings.TrimSpace(plan.Structure.Name); value != "" {
		return value
	}
	if value := strings.TrimSpace(plan.Structure.ID); value != "" {
		return value
	}
	return "未分类形态"
}

func playbookItem(plan domain.TradePlan, review domain.TradePlanReview, reviewed bool) domain.TradePlaybookItem {
	item := domain.TradePlaybookItem{
		PlanID: plan.ID, Symbol: plan.Symbol, StructureID: plan.Structure.ID,
		StructureName: playbookStructureLabel(plan), CreatedAt: plan.CreatedAt, ExpiresOn: plan.ExpiresOn,
	}
	if plan.Structure.Plan != nil {
		item.EntryLow, item.EntryHigh = plan.Structure.Plan.EntryLow, plan.Structure.Plan.EntryHigh
		item.Invalidation, item.Target2 = plan.Structure.Plan.Invalidation, plan.Structure.Plan.Target2
	}
	if reviewed {
		item.Reviewed, item.ReviewUpdatedAt = true, review.UpdatedAt
		item.ExecutionStatus, item.Discipline = review.Current.ExecutionStatus, review.Current.Discipline
		item.Tags = append([]string(nil), review.Current.Tags...)
		item.ExitReason, item.RealizedR = review.Current.ExitReason, review.Current.RealizedR
		item.EntryAt, item.ExitAt = review.Current.EntryAt, review.Current.ExitAt
		if playbookPriceValid(review.Current.ActualEntry) {
			item.ActualEntry = review.Current.ActualEntry
		}
		if playbookPriceValid(review.Current.ActualExit) {
			item.ActualExit = review.Current.ActualExit
		}
	}
	return item
}

func playbookDistribution(values []float64) []domain.TradePlaybookDistribution {
	result := []domain.TradePlaybookDistribution{
		{Key: "loss-large", Label: "≤ -1R"},
		{Key: "loss-small", Label: "-1R ~ 0"},
		{Key: "flat", Label: "0R"},
		{Key: "gain-small", Label: "0 ~ 1R"},
		{Key: "gain-medium", Label: "1R ~ 2R"},
		{Key: "gain-large", Label: "≥ 2R"},
	}
	for _, value := range values {
		index := 0
		switch {
		case value <= -1:
			index = 0
		case value < 0:
			index = 1
		case value == 0:
			index = 2
		case value < 1:
			index = 3
		case value < 2:
			index = 4
		default:
			index = 5
		}
		result[index].Count++
	}
	return result
}

func playbookInsights(report domain.TradePlaybookReport) []string {
	insights := make([]string, 0, 4)
	if report.CompletedTrades < minimumPlaybookCompletedSamples {
		insights = append(insights, fmt.Sprintf("已记录%d笔完成交易；每组不足%d笔时仅作逐笔观察", report.CompletedTrades, minimumPlaybookCompletedSamples))
	}
	if report.DisciplineRatePercent != nil && report.DisciplineSamples >= 3 && *report.DisciplineRatePercent < 70 {
		insights = append(insights, fmt.Sprintf("纪律完全遵守率为%.1f%%，当前优先改进执行一致性", *report.DisciplineRatePercent))
	}
	var best *domain.TradePlaybookMetric
	for index := range report.Setups {
		item := &report.Setups[index]
		if !item.SampleSufficient || item.AverageR == nil {
			continue
		}
		if best == nil || *item.AverageR > *best.AverageR {
			best = item
		}
	}
	if best != nil {
		insights = append(insights, fmt.Sprintf("%s已记录%d笔完成交易，平均%.2fR；当前为手工复盘描述统计", best.Label, best.Completed, *best.AverageR))
	}
	if report.DeviatedPlans > 0 {
		insights = append(insights, fmt.Sprintf("%d条计划记录了执行偏差，可结合标签和退出原因复核共性", report.DeviatedPlans))
	}
	if report.TotalPlans > report.ReviewedPlans {
		insights = append(insights, fmt.Sprintf("%d条计划尚未复盘", report.TotalPlans-report.ReviewedPlans))
	}
	return insights
}

// BuildTradePlaybookReport aggregates immutable plans and their latest
// append-only review revision. It reports observed behavior only and never
// changes strategy parameters or monitoring rules.
func BuildTradePlaybookReport(plans []domain.TradePlan, reviews []domain.TradePlanReview, now time.Time) domain.TradePlaybookReport {
	reviewByPlan := make(map[string]domain.TradePlanReview, len(reviews))
	for _, review := range reviews {
		key := review.Symbol + "|" + review.PlanID
		if current, found := reviewByPlan[key]; !found || review.Sequence > current.Sequence || (review.Sequence == current.Sequence && review.UpdatedAt.After(current.UpdatedAt)) {
			reviewByPlan[key] = review
		}
	}
	setupAccumulators := make(map[string]*playbookAccumulator)
	tagAccumulators := make(map[string]*playbookAccumulator)
	overall := &playbookAccumulator{metric: domain.TradePlaybookMetric{Key: "all", Label: "全部计划"}}
	report := domain.TradePlaybookReport{
		GeneratedAt: now, MinimumCompleted: minimumPlaybookCompletedSamples,
		Setups: []domain.TradePlaybookMetric{}, Tags: []domain.TradePlaybookMetric{},
		Recent: make([]domain.TradePlaybookItem, 0, len(plans)),
	}
	matchedReviews := make(map[string]bool)
	seenPlans := make(map[string]bool)
	invalidFills := 0
	for _, plan := range plans {
		key := plan.Symbol + "|" + plan.ID
		if plan.ID == "" || plan.Symbol == "" || seenPlans[key] {
			continue
		}
		seenPlans[key] = true
		review, reviewed := reviewByPlan[key]
		reviewed = reviewed && playbookReviewValid(plan, review)
		statisticsNote := ""
		if reviewed {
			matchedReviews[key] = true
			review.Current, statisticsNote = playbookRevision(plan, review.Current)
			if statisticsNote != "" {
				invalidFills++
			}
		}
		structureKey := strings.TrimSpace(plan.Structure.ID)
		if structureKey == "" {
			structureKey = "unclassified"
		}
		if setupAccumulators[structureKey] == nil {
			setupAccumulators[structureKey] = &playbookAccumulator{metric: domain.TradePlaybookMetric{Key: structureKey, Label: playbookStructureLabel(plan)}}
		}
		playbookApply(overall, reviewed, review.Current)
		playbookApply(setupAccumulators[structureKey], reviewed, review.Current)
		if reviewed {
			seenTags := make(map[string]bool)
			for _, raw := range review.Current.Tags {
				tag := strings.TrimSpace(raw)
				if tag == "" || seenTags[tag] {
					continue
				}
				seenTags[tag] = true
				if tagAccumulators[tag] == nil {
					tagAccumulators[tag] = &playbookAccumulator{metric: domain.TradePlaybookMetric{Key: tag, Label: tag}}
				}
				playbookApply(tagAccumulators[tag], true, review.Current)
			}
			if review.Current.ExecutionStatus == "skipped" || review.Current.ExecutionStatus == "not_traded" {
				report.SkippedPlans++
			}
			if review.Current.ExecutionStatus == "deviated" || review.Current.Discipline == "deviated" {
				report.DeviatedPlans++
			}
		}
		item := playbookItem(plan, review, reviewed)
		item.StatisticsNote = statisticsNote
		report.Recent = append(report.Recent, item)
	}
	overallMetric := finalizePlaybookMetric(overall)
	report.TotalPlans = overallMetric.Plans
	report.ReviewedPlans, report.EnteredPlans, report.CompletedTrades, report.Wins = overallMetric.Reviewed, overallMetric.Entered, overallMetric.Completed, overallMetric.Wins
	for _, value := range overall.returns {
		if value < 0 {
			report.Losses++
		} else if value == 0 {
			report.BreakEven++
		}
	}
	report.AverageR, report.MedianR, report.WinRatePercent = overallMetric.AverageR, overallMetric.MedianR, overallMetric.WinRatePercent
	report.DisciplineSamples, report.DisciplineRatePercent = overallMetric.DisciplineSamples, overallMetric.DisciplineRatePercent
	report.EntryRatePercent = playbookPercent(report.EnteredPlans, report.ReviewedPlans)
	for _, accumulator := range setupAccumulators {
		report.Setups = append(report.Setups, finalizePlaybookMetric(accumulator))
	}
	for _, accumulator := range tagAccumulators {
		report.Tags = append(report.Tags, finalizePlaybookMetric(accumulator))
	}
	sort.Slice(report.Setups, func(i, j int) bool {
		if report.Setups[i].Plans != report.Setups[j].Plans {
			return report.Setups[i].Plans > report.Setups[j].Plans
		}
		return report.Setups[i].Key < report.Setups[j].Key
	})
	sort.Slice(report.Tags, func(i, j int) bool {
		if report.Tags[i].Plans != report.Tags[j].Plans {
			return report.Tags[i].Plans > report.Tags[j].Plans
		}
		return report.Tags[i].Label < report.Tags[j].Label
	})
	sort.Slice(report.Recent, func(i, j int) bool {
		left, right := report.Recent[i].CreatedAt, report.Recent[j].CreatedAt
		if !report.Recent[i].ReviewUpdatedAt.IsZero() {
			left = report.Recent[i].ReviewUpdatedAt
		}
		if !report.Recent[j].ReviewUpdatedAt.IsZero() {
			right = report.Recent[j].ReviewUpdatedAt
		}
		if left.Equal(right) {
			return report.Recent[i].Symbol+report.Recent[i].PlanID < report.Recent[j].Symbol+report.Recent[j].PlanID
		}
		return left.After(right)
	})
	if len(reviewByPlan) > len(matchedReviews) {
		report.Warnings = append(report.Warnings, fmt.Sprintf("%d条复盘未匹配到当前冻结计划，已从统计中排除", len(reviewByPlan)-len(matchedReviews)))
	}
	if invalidFills > 0 {
		report.Warnings = append(report.Warnings, fmt.Sprintf("%d条复盘的成交信息不完整或不一致，已排除相应成交或R统计", invalidFills))
	}
	report.RDistribution = playbookDistribution(overall.returns)
	report.Insights = playbookInsights(report)
	return report
}
