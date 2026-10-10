package strategy

import (
	"math"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func previewFixture() (domain.ChartPlanLevels, PositionBudget) {
	return domain.ChartPlanLevels{EntryLow: 51, EntryHigh: 52, Invalidation: 50, Target1: 54, Target2: 56}, PositionBudget{Equity: 100000, Cash: 100000, RiskPercent: 1, MaxPositionPercent: 100, GapPercent: 5}
}

func TestPositionPreviewBudgetsCostsAndGap(t *testing.T) {
	plan, budget := previewFixture()
	plain, err := PreviewPosition("sh600519", plan, budget)
	if err != nil || plain.Shares != 500 || plain.TotalRisk != 1000 || plain.CashRequired != 26000 || *plain.NetRewardRisk != 2 {
		t.Fatalf("unexpected basic scenario: %+v %v", plain, err)
	}
	budget.MinimumCommission, budget.StampDutyBPS, budget.SlippageBPS = 5, 5, 5
	withCosts, err := PreviewPosition("sh600519", plan, budget)
	if err != nil || withCosts.Shares != 400 || withCosts.TotalRisk > 1000 || withCosts.BuyFees < 5 || withCosts.StopFees <= 5 || withCosts.GapLoss <= withCosts.TotalRisk {
		t.Fatalf("costs or gap ignored: %+v %v", withCosts, err)
	}
}

func TestPositionPreviewCashExistingRiskAndLotRules(t *testing.T) {
	plan, budget := previewFixture()
	budget.Cash = 10000
	for symbol, shares := range map[string]int{"sh600519": 100, "sh688001": 0, "bj920001": 192} {
		result, err := PreviewPosition(symbol, plan, budget)
		if err != nil || result.Shares != shares || result.CashRequired > budget.Cash {
			t.Fatalf("%s lots/cash: %+v %v", symbol, result, err)
		}
	}
	budget.Cash, budget.ExistingShares, budget.ExistingCost = 100000, 500, 53
	result, err := PreviewPosition("sh600519", plan, budget)
	if err != nil || result.Shares != 0 || result.ExistingRisk != 1500 || result.Constraint != "单笔风险预算" {
		t.Fatalf("existing risk not deducted: %+v %v", result, err)
	}
	budget.ExistingShares, budget.ExistingCost, budget.MaxPositionPercent = 100, 50, 10
	result, err = PreviewPosition("sh600519", plan, budget)
	if err != nil || result.Shares != 0 || result.Constraint != "单股仓位上限" {
		t.Fatalf("existing exposure ignored: %+v %v", result, err)
	}
}

func TestPositionPreviewRejectsInvalidFinancialInputs(t *testing.T) {
	plan, budget := previewFixture()
	for _, mutate := range []func(*PositionBudget){
		func(b *PositionBudget) { b.Equity = math.NaN() }, func(b *PositionBudget) { b.Cash = 100001 },
		func(b *PositionBudget) { b.ExistingCost = math.MaxFloat64; b.ExistingShares = 100 },
		func(b *PositionBudget) { b.RiskPercent = 0 }, func(b *PositionBudget) { b.ExistingShares = 1 },
		func(b *PositionBudget) { b.SlippageBPS = 10001 }, func(b *PositionBudget) { b.GapPercent = -1 },
	} {
		invalid := budget
		mutate(&invalid)
		if _, err := PreviewPosition("sh600519", plan, invalid); err == nil {
			t.Fatal("invalid budget accepted")
		}
	}
	plan.Invalidation = 52
	if _, err := PreviewPosition("sh600519", plan, budget); err == nil {
		t.Fatal("invalid stop accepted")
	}
}
