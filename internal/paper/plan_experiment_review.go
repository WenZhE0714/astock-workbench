package paper

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type PlanExperimentReviewFill struct {
	ArmID         string    `json:"arm_id"`
	Status        string    `json:"status"`
	Reason        string    `json:"reason,omitempty"`
	EntryOrderID  string    `json:"entry_order_id,omitempty"`
	ExitOrderID   string    `json:"exit_order_id,omitempty"`
	EntryAt       string    `json:"entry_at,omitempty"`
	ExitAt        string    `json:"exit_at,omitempty"`
	TriggerAt     time.Time `json:"trigger_at,omitzero"`
	Quantity      int       `json:"quantity"`
	EntryPrice    *float64  `json:"entry_price,omitempty"`
	ExitPrice     *float64  `json:"exit_price,omitempty"`
	Fees          *float64  `json:"fees,omitempty"`
	NetProfit     *float64  `json:"net_profit,omitempty"`
	ReturnPercent *float64  `json:"return_percent,omitempty"`
	InitialRisk   *float64  `json:"initial_risk,omitempty"`
	NetR          *float64  `json:"net_r,omitempty"`
	ExitReason    string    `json:"exit_reason,omitempty"`
	Rejections    int       `json:"rejections"`
	LastRejection string    `json:"last_rejection,omitempty"`
	Warnings      []string  `json:"warnings,omitempty"`
}

type PlanExperimentReviewPlan struct {
	PlanID           string                   `json:"plan_id"`
	Symbol           string                   `json:"symbol"`
	Name             string                   `json:"name,omitempty"`
	StructureName    string                   `json:"structure_name"`
	AnalysisDate     string                   `json:"analysis_date"`
	CreatedAt        time.Time                `json:"created_at"`
	ExpiresOn        string                   `json:"expires_on"`
	Selected         bool                     `json:"selected"`
	Status           string                   `json:"status"`
	Range            PlanExperimentReviewFill `json:"range"`
	Confirmed        PlanExperimentReviewFill `json:"confirmed"`
	ReturnDifference *float64                 `json:"return_difference,omitempty"`
	NetRDifference   *float64                 `json:"net_r_difference,omitempty"`
	ProfitDifference *float64                 `json:"profit_difference,omitempty"`
}

type PlanExperimentReviewArm struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Entered     int      `json:"entered"`
	Open        int      `json:"open"`
	Completed   int      `json:"completed"`
	Unavailable int      `json:"unavailable"`
	Rejections  int      `json:"rejections"`
	Wins        int      `json:"wins"`
	NetProfit   *float64 `json:"net_profit,omitempty"`
	Fees        *float64 `json:"fees,omitempty"`
	AverageNetR *float64 `json:"average_net_r,omitempty"`
	NetRSamples int      `json:"net_r_samples"`
	WinRate     *float64 `json:"win_rate,omitempty"`
}

type PlanExperimentReview struct {
	Version                 string                     `json:"version"`
	Initialized             bool                       `json:"initialized"`
	AsOf                    time.Time                  `json:"as_of,omitzero"`
	TotalPlans              int                        `json:"total_plans"`
	PairedCompleted         int                        `json:"paired_completed"`
	SingleSided             int                        `json:"single_sided"`
	OpenPairs               int                        `json:"open_pairs"`
	Waiting                 int                        `json:"waiting"`
	Unavailable             int                        `json:"unavailable"`
	RangeHigher             int                        `json:"range_higher"`
	ConfirmedHigher         int                        `json:"confirmed_higher"`
	EqualReturn             int                        `json:"equal_return"`
	AverageReturnDifference *float64                   `json:"average_return_difference,omitempty"`
	AverageNetRDifference   *float64                   `json:"average_net_r_difference,omitempty"`
	NetRSamples             int                        `json:"net_r_samples"`
	ProfitDifference        *float64                   `json:"profit_difference,omitempty"`
	Arms                    []PlanExperimentReviewArm  `json:"arms"`
	Plans                   []PlanExperimentReviewPlan `json:"plans"`
	Warnings                []string                   `json:"warnings,omitempty"`
}

type experimentReviewIndex struct {
	arm        *PlanExperimentArm
	orders     map[string][]ShadowOrder
	trades     map[string][]ShadowTrade
	positions  map[string][]ShadowOpenPosition
	rejections map[string][]ShadowRejection
}

func indexExperimentReview(arm *PlanExperimentArm) experimentReviewIndex {
	index := experimentReviewIndex{arm: arm, orders: make(map[string][]ShadowOrder), trades: make(map[string][]ShadowTrade), positions: make(map[string][]ShadowOpenPosition), rejections: make(map[string][]ShadowRejection)}
	if arm == nil {
		return index
	}
	for _, order := range arm.Report.Orders {
		index.orders[order.PlanID] = append(index.orders[order.PlanID], order)
	}
	for _, trade := range arm.Report.Trades {
		index.trades[trade.PlanID] = append(index.trades[trade.PlanID], trade)
	}
	for _, position := range arm.Report.Positions {
		index.positions[position.SignalID] = append(index.positions[position.SignalID], position)
	}
	for _, rejection := range arm.Report.Rejections {
		index.rejections[rejection.PlanID] = append(index.rejections[rejection.PlanID], rejection)
	}
	return index
}

func experimentReviewNumber(value float64) *float64 {
	if !finite(value) {
		return nil
	}
	return &value
}

func experimentReviewAmountMatches(actual, expected float64) bool {
	return finite(actual) && finite(expected) && math.Abs(actual-expected) <= .01
}

func experimentReviewOrderValid(order ShadowOrder, plan domain.TradePlan) bool {
	at, timeOK := parseRealtimeTimestamp(order.ExecutionTime)
	return timeOK && order.Status == OrderFilled && order.ID != "" && order.PlanID == plan.ID && order.Symbol == plan.Symbol &&
		order.AttemptDate == localTradingDate(at) &&
		order.Quantity > 0 && finite(order.Price) && order.Price > 0 && order.Amount > 0 &&
		experimentReviewAmountMatches(order.Amount, order.Price*float64(order.Quantity))
}

func unavailableExperimentReview(row PlanExperimentReviewFill, reason string) PlanExperimentReviewFill {
	row.Status, row.Reason = "unavailable", reason
	row.NetProfit, row.ReturnPercent, row.NetR = nil, nil, nil
	return row
}

// This experiment has one entry and one complete exit per plan and arm. Match
// the linked orders and reconcile costs before exposing a completed result.
func reviewExperimentFill(index experimentReviewIndex, plan domain.TradePlan, armID string) PlanExperimentReviewFill {
	row := PlanExperimentReviewFill{ArmID: armID, Status: "waiting"}
	if index.arm == nil {
		return unavailableExperimentReview(row, "实验账户缺失")
	}
	trial, found := index.arm.Trials[plan.ID]
	if !found || trial.PlanID != plan.ID || trial.Symbol != plan.Symbol {
		return unavailableExperimentReview(row, "缺少与冻结计划一致的执行状态")
	}
	row.Status, row.Reason, row.TriggerAt = trial.Status, trial.Reason, trial.EntryTriggerAt
	seenRejections := make(map[string]bool)
	for _, rejection := range index.rejections[plan.ID] {
		if rejection.OrderID == "" || rejection.Symbol != plan.Symbol || seenRejections[rejection.OrderID] {
			continue
		}
		seenRejections[rejection.OrderID] = true
		row.Rejections++
		row.LastRejection = rejection.Reason
	}
	orders, trades, positions := index.orders[plan.ID], index.trades[plan.ID], index.positions[plan.ID]
	if trial.EntryOrderID == "" {
		if len(orders) > 0 || len(trades) > 0 || len(positions) > 0 || trial.ExitOrderID != "" || trial.Status == "closed" || trial.Status == "open" || trial.Status == "pending_exit" {
			return unavailableExperimentReview(row, "成交或持仓缺少入场订单归因")
		}
		return row
	}
	var buy, sell *ShadowOrder
	for i := range orders {
		order := &orders[i]
		if !experimentReviewOrderValid(*order, plan) {
			return unavailableExperimentReview(row, "成交时间、数量、金额或计划归因不完整")
		}
		switch {
		case order.Side == "buy" && order.ID == trial.EntryOrderID && buy == nil:
			buy = order
		case order.Side == "sell" && order.ID == trial.ExitOrderID && sell == nil:
			sell = order
		default:
			return unavailableExperimentReview(row, "发现重复或不匹配的计划成交")
		}
	}
	if buy == nil {
		return unavailableExperimentReview(row, "入场订单尚未完整归档")
	}
	entryAt, _ := parseRealtimeTimestamp(buy.ExecutionTime)
	if (!trial.EntryTriggerAt.IsZero() && !entryAt.After(trial.EntryTriggerAt)) || entryAt.Before(plan.CreatedAt) {
		return unavailableExperimentReview(row, "入场成交不晚于观察条件或早于计划创建")
	}
	row.EntryOrderID, row.EntryAt, row.Quantity, row.EntryPrice = buy.ID, buy.ExecutionTime, buy.Quantity, experimentReviewNumber(buy.Price)
	entryFee := transactionFee(buy.Amount, "buy", index.arm.Report.Config)
	row.Fees = experimentReviewNumber(entryFee)
	if row.Fees == nil || entryFee < 0 {
		return unavailableExperimentReview(row, "入场费用无效")
	}
	if plan.Structure.Plan != nil && finite(plan.Structure.Plan.Invalidation) && plan.Structure.Plan.Invalidation > 0 {
		risk := (buy.Price - plan.Structure.Plan.Invalidation) * float64(buy.Quantity)
		if risk > 0 {
			row.InitialRisk = experimentReviewNumber(risk)
		}
	}
	if sell == nil {
		if trial.ExitOrderID != "" || len(trades) > 0 || len(positions) != 1 || positions[0].Symbol != plan.Symbol || positions[0].Quantity != buy.Quantity || (trial.Status != "open" && trial.Status != "pending_exit") {
			return unavailableExperimentReview(row, "持仓与计划成交不一致，等待完整账本")
		}
		return row
	}
	exitAt, _ := parseRealtimeTimestamp(sell.ExecutionTime)
	if len(positions) != 0 || len(trades) != 1 || trial.Status != "closed" || sell.Quantity != buy.Quantity || !exitAt.After(entryAt) || localTradingDate(exitAt) <= localTradingDate(entryAt) {
		return unavailableExperimentReview(row, "退出数量、时间或已平仓状态不一致")
	}
	trade := trades[0]
	fees := entryFee + transactionFee(sell.Amount, "sell", index.arm.Report.Config)
	gross := sell.Amount - buy.Amount
	net := gross - fees
	if trade.ID == "" || trade.Symbol != plan.Symbol || trade.Quantity != buy.Quantity || trade.EntryOrderID != buy.ID || trade.ExitOrderID != sell.ID ||
		trade.EntryDate != buy.AttemptDate || trade.ExitDate != sell.AttemptDate || trade.ExitTime != sell.ExecutionTime ||
		trade.EntryPrice != buy.Price || trade.ExitPrice != sell.Price || !experimentReviewAmountMatches(trade.TotalFee, fees) ||
		!experimentReviewAmountMatches(trade.GrossProfit, gross) || !experimentReviewAmountMatches(trade.NetProfit, net) {
		return unavailableExperimentReview(row, "已平仓盈亏与成交费用未通过对账")
	}
	row.Status = "closed"
	row.ExitOrderID, row.ExitAt, row.ExitPrice = sell.ID, sell.ExecutionTime, experimentReviewNumber(sell.Price)
	row.ExitReason = sell.Reason
	row.Fees, row.NetProfit = experimentReviewNumber(fees), experimentReviewNumber(trade.NetProfit)
	row.ReturnPercent = experimentReviewNumber(trade.NetProfit / (buy.Amount + entryFee) * 100)
	if row.ReturnPercent == nil {
		return unavailableExperimentReview(row, "扣费后收益率不可用")
	}
	if row.InitialRisk != nil {
		row.NetR = experimentReviewNumber(trade.NetProfit / *row.InitialRisk)
	}
	if row.NetR == nil {
		row.Warnings = append(row.Warnings, "缺少有效入场风险金额，未计入净R比较")
	}
	return row
}

func experimentReviewSummary(id, name string, plans []PlanExperimentReviewPlan) PlanExperimentReviewArm {
	result := PlanExperimentReviewArm{ID: id, Name: name}
	net, fees, sumR := 0.0, 0.0, 0.0
	for _, plan := range plans {
		row := plan.Range
		if id == "confirmed" {
			row = plan.Confirmed
		}
		result.Rejections += row.Rejections
		if row.Status == "unavailable" {
			result.Unavailable++
			continue
		}
		if row.EntryPrice != nil {
			result.Entered++
		}
		if row.Fees != nil {
			fees += *row.Fees
		}
		if row.Status == "open" || row.Status == "pending_exit" {
			result.Open++
		}
		if row.Status != "closed" || row.NetProfit == nil {
			continue
		}
		result.Completed++
		net += *row.NetProfit
		if *row.NetProfit > 0 {
			result.Wins++
		}
		if row.NetR != nil {
			result.NetRSamples++
			sumR += *row.NetR
		}
	}
	if result.Unavailable == 0 {
		result.Fees = experimentReviewNumber(fees)
	}
	if result.Completed > 0 {
		result.NetProfit = experimentReviewNumber(net)
		result.WinRate = experimentReviewNumber(float64(result.Wins) / float64(result.Completed) * 100)
	}
	if result.NetRSamples > 0 {
		result.AverageNetR = experimentReviewNumber(sumR / float64(result.NetRSamples))
	}
	return result
}

// BuildPlanExperimentReview derives a read-only review from the complete
// persisted ledger, before HTTP pagination. Open or one-sided plans never
// become zero-return observations in the paired comparison.
func BuildPlanExperimentReview(state PlanExperiment) PlanExperimentReview {
	review := PlanExperimentReview{Version: "plan-pair-review-v1", Initialized: state.Version != "", AsOf: state.UpdatedAt, Plans: []PlanExperimentReviewPlan{}, Arms: []PlanExperimentReviewArm{}}
	if state.Version == "" {
		return review
	}
	if state.Version != PlanExperimentVersion {
		review.Warnings = []string{"实验账本版本不受支持"}
		return review
	}
	arms := make(map[string]*PlanExperimentArm)
	for i := range state.Arms {
		if _, duplicate := arms[state.Arms[i].ID]; duplicate {
			review.Warnings = []string{"实验账户编号重复，暂停复盘比较"}
			return review
		}
		arms[state.Arms[i].ID] = &state.Arms[i]
	}
	left, right := indexExperimentReview(arms["range"]), indexExperimentReview(arms["confirmed"])
	compatible := left.arm != nil && right.arm != nil && left.arm.Report.InitialCash > 0 && left.arm.Report.InitialCash == right.arm.Report.InitialCash && sameExperimentJSON(left.arm.Report.Config, right.arm.Report.Config)
	if !compatible {
		review.Warnings = append(review.Warnings, "两组初始资金或执行配置不一致，暂停成对比较")
	}
	returnDiff, riskDiff, profitDiff := 0.0, 0.0, 0.0
	for id, selection := range state.Selections {
		plan := selection.Plan
		row := PlanExperimentReviewPlan{PlanID: id, Symbol: plan.Symbol, StructureName: plan.Structure.Name, AnalysisDate: plan.Analysis.DataDate, CreatedAt: plan.CreatedAt, ExpiresOn: plan.ExpiresOn, Selected: selection.Enabled,
			Range: reviewExperimentFill(left, plan, "range"), Confirmed: reviewExperimentFill(right, plan, "confirmed")}
		if id != plan.ID {
			row.Range = unavailableExperimentReview(row.Range, "计划标识与冻结快照不一致")
			row.Confirmed = unavailableExperimentReview(row.Confirmed, "计划标识与冻结快照不一致")
		}
		switch {
		case row.Range.Status == "unavailable" || row.Confirmed.Status == "unavailable" || !compatible:
			row.Status = "unavailable"
			review.Unavailable++
		case row.Range.Status == "closed" && row.Confirmed.Status == "closed":
			difference := *row.Range.ReturnPercent - *row.Confirmed.ReturnPercent
			row.ReturnDifference = experimentReviewNumber(difference)
			row.ProfitDifference = experimentReviewNumber(*row.Range.NetProfit - *row.Confirmed.NetProfit)
			if row.ReturnDifference == nil || row.ProfitDifference == nil {
				row.Status = "unavailable"
				review.Unavailable++
				break
			}
			row.Status = "paired_closed"
			review.PairedCompleted++
			returnDiff += difference
			profitDiff += *row.ProfitDifference
			if math.Abs(difference) < 1e-8 {
				review.EqualReturn++
			} else if difference > 0 {
				review.RangeHigher++
			} else {
				review.ConfirmedHigher++
			}
			if row.Range.NetR != nil && row.Confirmed.NetR != nil {
				row.NetRDifference = experimentReviewNumber(*row.Range.NetR - *row.Confirmed.NetR)
				if row.NetRDifference != nil {
					review.NetRSamples++
					riskDiff += *row.NetRDifference
				}
			}
		case (row.Range.EntryPrice != nil) != (row.Confirmed.EntryPrice != nil):
			row.Status = "single_sided"
			review.SingleSided++
		case row.Range.EntryPrice != nil || row.Confirmed.EntryPrice != nil:
			row.Status = "open"
			review.OpenPairs++
		default:
			row.Status = "waiting"
			review.Waiting++
		}
		review.Plans = append(review.Plans, row)
	}
	sort.Slice(review.Plans, func(i, j int) bool {
		if review.Plans[i].CreatedAt.Equal(review.Plans[j].CreatedAt) {
			return review.Plans[i].PlanID < review.Plans[j].PlanID
		}
		return review.Plans[i].CreatedAt.After(review.Plans[j].CreatedAt)
	})
	review.TotalPlans = len(review.Plans)
	if review.PairedCompleted > 0 {
		review.AverageReturnDifference = experimentReviewNumber(returnDiff / float64(review.PairedCompleted))
		review.ProfitDifference = experimentReviewNumber(profitDiff)
	}
	if review.NetRSamples > 0 {
		review.AverageNetRDifference = experimentReviewNumber(riskDiff / float64(review.NetRSamples))
	}
	for _, definition := range []struct{ id, name string }{{"range", "区间条件组"}, {"confirmed", "仅确认对照组"}} {
		review.Arms = append(review.Arms, experimentReviewSummary(definition.id, definition.name, review.Plans))
	}
	if review.Unavailable > 0 {
		review.Warnings = append(review.Warnings, fmt.Sprintf("%d条计划的账本不完整或未通过对账，已排除成对比较", review.Unavailable))
	}
	for _, arm := range state.Arms {
		for _, order := range arm.Report.Orders {
			if _, ok := state.Selections[order.PlanID]; !ok && strings.TrimSpace(order.ID) != "" {
				review.Warnings = append(review.Warnings, "发现未关联冻结计划的成交，未计入自动复盘")
				return review
			}
		}
	}
	return review
}
