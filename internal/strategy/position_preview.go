package strategy

import (
	"fmt"
	"math"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type PositionBudget struct {
	Equity             float64 `json:"equity"`
	Cash               float64 `json:"cash"`
	RiskPercent        float64 `json:"risk_percent"`
	MaxPositionPercent float64 `json:"max_position_percent"`
	ExistingShares     int     `json:"existing_shares"`
	ExistingCost       float64 `json:"existing_cost"`
	CommissionBPS      float64 `json:"commission_bps"`
	MinimumCommission  float64 `json:"minimum_commission"`
	StampDutyBPS       float64 `json:"stamp_duty_bps"`
	TransferBPS        float64 `json:"transfer_bps"`
	SlippageBPS        float64 `json:"slippage_bps"`
	GapPercent         float64 `json:"gap_percent"`
}

type PositionPreview struct {
	Shares          int      `json:"shares"`
	MinimumShares   int      `json:"minimum_shares"`
	ShareStep       int      `json:"share_step"`
	EntryPrice      float64  `json:"entry_price"`
	StopPrice       float64  `json:"stop_price"`
	CashRequired    float64  `json:"cash_required"`
	BuyFees         float64  `json:"buy_fees"`
	StopFees        float64  `json:"stop_fees"`
	RiskBudget      float64  `json:"risk_budget"`
	ExistingRisk    float64  `json:"existing_risk"`
	NewRisk         float64  `json:"new_risk"`
	TotalRisk       float64  `json:"total_risk"`
	PositionPercent float64  `json:"position_percent"`
	Target1Profit   float64  `json:"target_1_profit"`
	Target2Profit   float64  `json:"target_2_profit"`
	NetRewardRisk   *float64 `json:"net_reward_risk"`
	GapLoss         float64  `json:"gap_loss"`
	Constraint      string   `json:"constraint"`
	Warnings        []string `json:"warnings"`
}

func PreviewPosition(symbol string, plan domain.ChartPlanLevels, budget PositionBudget) (PositionPreview, error) {
	result := PositionPreview{}
	for _, value := range []float64{plan.EntryHigh, plan.Invalidation, plan.Target1, plan.Target2,
		budget.Equity, budget.Cash, budget.RiskPercent, budget.MaxPositionPercent, budget.ExistingCost,
		budget.CommissionBPS, budget.MinimumCommission, budget.StampDutyBPS, budget.TransferBPS, budget.SlippageBPS, budget.GapPercent} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return result, fmt.Errorf("试算参数必须是非负有限数值")
		}
	}
	if budget.Equity <= 0 || budget.Equity > 1e12 || budget.Cash > budget.Equity ||
		budget.ExistingCost > 1e9 || plan.EntryHigh > 1e9 || plan.Invalidation > 1e9 || plan.Target1 > 1e9 || plan.Target2 > 1e9 ||
		budget.RiskPercent <= 0 || budget.RiskPercent > 100 || budget.MaxPositionPercent <= 0 || budget.MaxPositionPercent > 100 ||
		budget.ExistingShares < 0 || budget.ExistingShares > 1e9 || (budget.ExistingShares > 0 && budget.ExistingCost <= 0) ||
		budget.CommissionBPS > 100 || budget.StampDutyBPS > 100 || budget.TransferBPS > 100 ||
		budget.MinimumCommission > 10000 || budget.SlippageBPS > 1000 || budget.GapPercent > 50 {
		return result, fmt.Errorf("请检查资金、风险比例、现有持仓和费用范围")
	}
	if plan.Invalidation <= 0 || plan.EntryHigh <= plan.Invalidation || plan.Target1 <= plan.EntryHigh || plan.Target2 < plan.Target1 {
		return result, fmt.Errorf("当前形态没有有效的做多风险区间")
	}
	minimum, step := 100, 100
	if strings.HasPrefix(symbol, "sh688") || strings.HasPrefix(symbol, "sh689") {
		minimum, step = 200, 1
	} else if strings.HasPrefix(symbol, "bj") {
		minimum, step = 100, 1
	}
	fee := func(amount float64, sell bool) float64 {
		if amount <= 0 {
			return 0
		}
		value := math.Max(budget.MinimumCommission, amount*budget.CommissionBPS/10000) + amount*budget.TransferBPS/10000
		if sell {
			value += amount * budget.StampDutyBPS / 10000
		}
		return math.Ceil(value*100) / 100
	}
	entry := math.Ceil(plan.EntryHigh*(1+budget.SlippageBPS/10000)*100) / 100
	stop := math.Floor(plan.Invalidation*(1-budget.SlippageBPS/10000)*100) / 100
	if stop <= 0 {
		return result, fmt.Errorf("滑点后的失效价格无效")
	}
	existingRiskAt := func(exit float64) float64 {
		quantity := float64(budget.ExistingShares)
		return math.Max(0, (budget.ExistingCost-exit)*quantity+fee(exit*quantity, true))
	}
	existingRisk := existingRiskAt(stop)
	riskLimit := budget.Equity * budget.RiskPercent / 100
	positionLimit := budget.Equity * budget.MaxPositionPercent / 100
	costs := func(shares int) (cash, risk float64) {
		quantity := float64(shares)
		cash = entry*quantity + fee(entry*quantity, false)
		risk = cash - stop*quantity + fee(stop*quantity, true)
		return
	}
	constraint := func(shares int) string {
		cash, risk := costs(shares)
		if risk+existingRisk > riskLimit {
			return "单笔风险预算"
		}
		if cash > budget.Cash {
			return "可用现金"
		}
		if float64(shares+budget.ExistingShares)*entry > positionLimit {
			return "单股仓位上限"
		}
		return ""
	}
	maxShares := math.Min(1e9, math.Min(budget.Cash/entry, positionLimit/entry))
	low, high := 0, int(maxShares)/step
	for low < high {
		mid := low + (high-low+1)/2
		if constraint(mid*step) == "" {
			low = mid
		} else {
			high = mid - 1
		}
	}
	shares := low * step
	if shares < minimum {
		shares = 0
	}
	cash, risk := costs(shares)
	quantity := float64(shares)
	profitAt := func(target float64) float64 {
		exit := math.Floor(target*(1-budget.SlippageBPS/10000)*100) / 100
		return exit*quantity - fee(exit*quantity, true) - cash
	}
	gapStop := math.Floor(stop*(1-budget.GapPercent/100)*100) / 100
	round := func(value float64) float64 { return math.Round(value*100) / 100 }
	result = PositionPreview{
		Shares: shares, MinimumShares: minimum, ShareStep: step, EntryPrice: entry, StopPrice: stop,
		CashRequired: round(cash), BuyFees: fee(entry*quantity, false), StopFees: fee(stop*quantity, true),
		RiskBudget: round(riskLimit), ExistingRisk: round(existingRisk), NewRisk: round(risk), TotalRisk: round(risk + existingRisk),
		PositionPercent: round(float64(shares+budget.ExistingShares) * entry / budget.Equity * 100),
		Target1Profit:   round(profitAt(plan.Target1)), Target2Profit: round(profitAt(plan.Target2)),
		GapLoss:    round(cash - gapStop*quantity + fee(gapStop*quantity, true) + existingRiskAt(gapStop)),
		Constraint: constraint(max(minimum, shares+step)),
		Warnings:   []string{"按入场区间上沿及手动输入试算；未核验其他股票和行业集中度", "止损损失为情景估计，不是亏损上限；T+1、跳空、停牌和跌停可能阻止退出"},
	}
	if shares > 0 && risk > 0 {
		ratio := round(result.Target2Profit / risk)
		result.NetRewardRisk = &ratio
	}
	return result, nil
}
