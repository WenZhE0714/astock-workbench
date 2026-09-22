package strategy

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func playbookNumber(value float64) *float64 { return &value }

func playbookPlan(id, symbol, structure, name string, created time.Time) domain.TradePlan {
	return domain.TradePlan{
		ID: id, Version: 1, Symbol: symbol, CreatedAt: created, ExpiresOn: created.AddDate(0, 0, 7).Format(time.DateOnly),
		Analysis:  domain.ChartAnalysis{Symbol: symbol, Fingerprint: "fingerprint-" + id},
		Structure: domain.ChartStructure{ID: structure, Name: name, Plan: &domain.ChartPlanLevels{EntryLow: 10, EntryHigh: 11, Invalidation: 9, Target2: 14}},
	}
}

func playbookReview(plan domain.TradePlan, realized float64, discipline, status string, tags []string, at time.Time) domain.TradePlanReview {
	return domain.TradePlanReview{
		Version: 1, PlanID: plan.ID, Symbol: plan.Symbol, Fingerprint: plan.Analysis.Fingerprint, UpdatedAt: at, Sequence: 1,
		Current: domain.TradePlanReviewRevision{Sequence: 1, UpdatedAt: at, ExecutionStatus: status, Discipline: discipline, ActualEntry: playbookNumber(10), ActualExit: playbookNumber(10 + realized), RealizedR: playbookNumber(realized), Tags: tags},
	}
}

func TestBuildTradePlaybookReportAggregatesSetupsTagsAndDiscipline(t *testing.T) {
	now := time.Date(2026, 9, 22, 18, 0, 0, 0, time.UTC)
	first := playbookPlan("first", "sh600519", "range-breakout", "区间突破", now.Add(-48*time.Hour))
	second := playbookPlan("second", "sz000001", "range-breakout", "区间突破", now.Add(-24*time.Hour))
	third := playbookPlan("third", "sz300001", "ma-pullback", "趋势回踩", now.Add(-12*time.Hour))
	report := BuildTradePlaybookReport([]domain.TradePlan{first, second, third}, []domain.TradePlanReview{
		playbookReview(first, 2, "followed", "followed", []string{"放量", "主线"}, now.Add(-20*time.Hour)),
		playbookReview(second, -1, "deviated", "deviated", []string{"放量"}, now.Add(-10*time.Hour)),
	}, now)
	if report.TotalPlans != 3 || report.ReviewedPlans != 2 || report.EnteredPlans != 2 || report.CompletedTrades != 2 || report.Wins != 1 || report.Losses != 1 || report.DeviatedPlans != 1 {
		t.Fatalf("unexpected totals: %+v", report)
	}
	if report.AverageR == nil || math.Abs(*report.AverageR-.5) > 1e-9 || report.MedianR == nil || *report.MedianR != .5 || report.WinRatePercent == nil || *report.WinRatePercent != 50 || report.DisciplineRatePercent == nil || *report.DisciplineRatePercent != 50 {
		t.Fatalf("unexpected performance: %+v", report)
	}
	if len(report.Setups) != 2 || report.Setups[0].Key != "range-breakout" || report.Setups[0].Completed != 2 || report.Setups[0].SampleSufficient {
		t.Fatalf("unexpected setup metrics: %+v", report.Setups)
	}
	if len(report.Tags) != 2 || report.Tags[0].Key != "放量" || report.Tags[0].Plans != 2 {
		t.Fatalf("unexpected tag metrics: %+v", report.Tags)
	}
	if len(report.RDistribution) != 6 || report.RDistribution[0].Count != 1 || report.RDistribution[5].Count != 1 || len(report.Recent) != 3 {
		t.Fatalf("unexpected distribution/recent: %+v %+v", report.RDistribution, report.Recent)
	}
}

func TestBuildTradePlaybookReportRejectsMismatchedReview(t *testing.T) {
	now := time.Now()
	plan := playbookPlan("plan", "sh600519", "range-breakout", "区间突破", now)
	review := playbookReview(plan, 1, "followed", "followed", nil, now)
	review.Fingerprint = "different"
	report := BuildTradePlaybookReport([]domain.TradePlan{plan}, []domain.TradePlanReview{review}, now)
	if report.ReviewedPlans != 0 || len(report.Warnings) != 1 {
		t.Fatalf("mismatched review affected report: %+v", report)
	}
}

func TestTradePlaybookCountsOnlyRecordedFillsAndRecalculatesR(t *testing.T) {
	now := time.Date(2026, 9, 22, 18, 0, 0, 0, time.UTC)
	plans := make([]domain.TradePlan, 0)
	reviews := make([]domain.TradePlanReview, 0)
	for index, status := range []string{"watching", "skipped", "followed", "followed", "deviated", "followed"} {
		plan := playbookPlan(string(rune('a'+index)), "sh600519", "range-breakout", "区间突破", now)
		review := playbookReview(plan, 2, "", status, nil, now)
		switch index {
		case 0:
			review.Current.ActualEntry, review.Current.ActualExit = nil, nil
		case 2:
			review.Current.ActualExit = nil
		case 3:
			review.Current.ActualExit = playbookNumber(10)
		case 4:
			review.Current.ActualExit = playbookNumber(9)
		case 5:
			review.Current.ActualExit = playbookNumber(math.NaN())
		}
		plans = append(plans, plan)
		reviews = append(reviews, review)
	}
	report := BuildTradePlaybookReport(plans, reviews, now)
	if report.ReviewedPlans != 6 || report.EnteredPlans != 4 || report.CompletedTrades != 2 || report.BreakEven != 1 || report.Losses != 1 || report.Wins != 0 {
		t.Fatalf("incomplete or conflicting fills affected totals: %+v", report)
	}
	if report.AverageR == nil || *report.AverageR != -.5 || report.WinRatePercent == nil || *report.WinRatePercent != 0 || report.DisciplineRatePercent != nil {
		t.Fatalf("derived R or missing discipline was counted: %+v", report)
	}
	count := 0
	for _, bin := range report.RDistribution {
		count += bin.Count
	}
	if count != report.CompletedTrades || report.RDistribution[2].Count != 1 {
		t.Fatalf("distribution differs from completed trades: %+v", report.RDistribution)
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("invalid price leaked into report: %v", err)
	}
}

func TestTradePlaybookLatestRevisionAndDuplicatePlansCountOnce(t *testing.T) {
	now := time.Date(2026, 9, 22, 18, 0, 0, 0, time.UTC)
	plan := playbookPlan("one", "sh600519", "range-breakout", "区间突破", now)
	old := playbookReview(plan, 2, "followed", "followed", []string{"重复", "重复"}, now.Add(-time.Hour))
	latest := playbookReview(plan, -1, "deviated", "deviated", []string{"重复", "重复"}, now)
	latest.Sequence, latest.Current.Sequence = 2, 2
	report := BuildTradePlaybookReport([]domain.TradePlan{plan, plan}, []domain.TradePlanReview{latest, old}, now)
	if report.TotalPlans != 1 || report.CompletedTrades != 1 || *report.AverageR != -1 || len(report.Warnings) != 0 || report.Tags[0].Plans != 1 {
		t.Fatalf("duplicate snapshots inflated evidence: %+v", report)
	}
	for _, insight := range report.Insights {
		if strings.Contains(insight, "平均") {
			t.Fatalf("ranked a one-trade setup: %s", insight)
		}
	}
	if old.Current.RealizedR == nil || *old.Current.RealizedR != 2 {
		t.Fatal("aggregation changed review input")
	}
}

func TestTradePlaybookDistributionBoundariesAndEmptyReport(t *testing.T) {
	bins := playbookDistribution([]float64{-2, -1, -.01, 0, .001, .5, 1, 1.99, 2})
	for index, want := range []int{2, 1, 1, 2, 2, 1} {
		if bins[index].Count != want {
			t.Fatalf("boundary mismatch: %+v", bins)
		}
	}
	report := BuildTradePlaybookReport(nil, nil, time.Now())
	if report.AverageR != nil || report.WinRatePercent != nil || report.EntryRatePercent != nil || report.Recent == nil || report.Setups == nil || report.Tags == nil {
		t.Fatalf("empty report fabricates measurements: %+v", report)
	}
}
