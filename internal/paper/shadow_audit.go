package paper

import (
	"fmt"
	"math"
	"sort"
	"time"
)

type LedgerIssue struct {
	Code    string `json:"code"`
	EventID string `json:"event_id,omitempty"`
	Message string `json:"message"`
}

type LedgerCorrection struct {
	Code    string  `json:"code"`
	EventID string  `json:"event_id,omitempty"`
	Before  float64 `json:"before"`
	After   float64 `json:"after"`
}

func AuditShadowLedger(report Report) []LedgerIssue {
	issues := []LedgerIssue{}
	if report.AsOf == "" && len(report.Orders) == 0 {
		return issues
	}
	add := func(code, id, message string) { issues = append(issues, LedgerIssue{code, id, message}) }
	orders := map[string]ShadowOrder{}
	quantities, closed := map[string]int{}, map[string]int{}
	cash, fees := report.InitialCash, 0.0
	for _, order := range report.Orders {
		if _, ok := orders[order.ID]; ok {
			add("duplicate-order", order.ID, "订单ID重复")
		}
		orders[order.ID] = order
		if order.Status != OrderFilled {
			continue
		}
		if order.Quantity <= 0 || order.Price <= 0 || !finite(order.Amount) || math.Abs(order.Amount-order.Price*float64(order.Quantity)) > .02 {
			add("order-value", order.ID, "已成交订单的数量、价格或金额不合法")
		}
		fee := transactionFee(order.Amount, order.Side, report.Config)
		fees += fee
		cash -= fee
		switch order.Side {
		case "buy":
			cash -= order.Amount
			quantities[order.Symbol] += order.Quantity
		case "sell":
			cash += order.Amount
			quantities[order.Symbol] -= order.Quantity
		default:
			add("order-side", order.ID, "未知成交方向")
		}
	}
	net := 0.0
	seen := map[string]bool{}
	for _, trade := range report.Trades {
		if seen[trade.ID] {
			add("duplicate-trade", trade.ID, "平仓记录ID重复")
		}
		seen[trade.ID] = true
		buy, buyOK := orders[trade.EntryOrderID]
		sell, sellOK := orders[trade.ExitOrderID]
		if !buyOK || !sellOK || buy.Side != "buy" || sell.Side != "sell" || buy.Symbol != trade.Symbol || sell.Symbol != trade.Symbol {
			add("trade-reference", trade.ID, "平仓记录缺少匹配的买卖订单")
		}
		closed[trade.EntryOrderID] += trade.Quantity
		if buyOK && closed[trade.EntryOrderID] > buy.Quantity {
			add("overclosed", trade.ID, "累计平仓数量超过买入数量")
		}
		gross := (trade.ExitPrice - trade.EntryPrice) * float64(trade.Quantity)
		if trade.Quantity <= 0 || !finite(trade.NetProfit) || math.Abs(gross-trade.GrossProfit) > .02 || math.Abs(gross-trade.TotalFee-trade.NetProfit) > .02 {
			add("trade-value", trade.ID, "平仓数量或盈亏恒等式不成立")
		}
		entry, e1 := time.Parse(time.DateOnly, trade.EntryDate)
		exit, e2 := time.Parse(time.DateOnly, trade.ExitDate)
		if e1 != nil || e2 != nil || exit.Before(entry) || trade.HoldingDays < 1 || trade.HoldingDays > int(exit.Sub(entry).Hours()/24)+1 {
			add("holding-days", trade.ID, "持有天数与成交日期不一致")
		}
		net += trade.NetProfit
	}
	for _, position := range report.Positions {
		quantities[position.Symbol] -= position.Quantity
	}
	for symbol, quantity := range quantities {
		if quantity != 0 {
			add("position-balance", symbol, "买卖数量与剩余持仓不一致")
		}
	}
	if !finite(cash) || math.Abs(cash-report.RemainingCash) > .02 {
		add("cash-balance", "", "订单现金流与账户现金不一致")
	}
	if math.Abs(fees-report.TotalFees) > .02 {
		add("fee-balance", "", "订单费用与账户费用不一致")
	}
	if math.Abs(net-report.RealizedProfit) > .02 {
		add("realized-balance", "", "平仓净盈亏与已实现盈亏不一致")
	}
	if len(report.Positions) == 0 && (math.Abs(report.UnrealizedProfit) > .02 || math.Abs(report.TotalEquity-report.InitialCash-net) > .02) {
		add("empty-position-profit", "", "空仓账户仍有未实现盈亏或净值差额")
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Code+issues[i].EventID < issues[j].Code+issues[j].EventID })
	return issues
}

// RepairShadowLedger only removes provable zero-share artifacts and corrects
// impossible holding counts. Any other inconsistency stops the migration.
func RepairShadowLedger(previous Report, calendar []string) (Report, []LedgerCorrection, error) {
	report := cloneReport(previous)
	changes := []LedgerCorrection{}
	if len(previous.Positions) > 0 || previous.OpenPositions > 0 {
		return previous, changes, fmt.Errorf("账本修复需要空仓，保留现有持仓与证据")
	}
	removed := map[string]bool{}
	refund := 0.0
	orders := []ShadowOrder{}
	for _, order := range report.Orders {
		if order.Side == "sell" && order.Status == OrderFilled && order.Quantity == 0 && order.Amount == 0 {
			for _, trade := range report.Trades {
				if trade.ExitOrderID == order.ID && (trade.Quantity != 0 || trade.GrossProfit != 0) {
					return previous, changes, fmt.Errorf("零股卖单关联了非零成交: %s", order.ID)
				}
			}
			removed[order.ID] = true
			refund += transactionFee(order.Amount, order.Side, report.Config)
			changes = append(changes, LedgerCorrection{"remove-zero-order", order.ID, 0, 0})
			continue
		}
		orders = append(orders, order)
	}
	trades := []ShadowTrade{}
	for _, trade := range report.Trades {
		if removed[trade.ExitOrderID] && trade.Quantity == 0 {
			changes = append(changes, LedgerCorrection{"remove-zero-trade", trade.ID, trade.NetProfit, 0})
			continue
		}
		entry, e1 := time.Parse(time.DateOnly, trade.EntryDate)
		exit, e2 := time.Parse(time.DateOnly, trade.ExitDate)
		if e1 == nil && e2 == nil && (trade.HoldingDays < 1 || trade.HoldingDays > int(exit.Sub(entry).Hours()/24)+1) {
			start, end := sort.SearchStrings(calendar, trade.EntryDate), sort.SearchStrings(calendar, trade.ExitDate)
			if start >= len(calendar) || end >= len(calendar) || calendar[start] != trade.EntryDate || calendar[end] != trade.ExitDate || end < start {
				return previous, changes, fmt.Errorf("修复持有天数所需交易日历不完整: %s", trade.ID)
			}
			changes = append(changes, LedgerCorrection{"holding-days", trade.ID, float64(trade.HoldingDays), float64(end - start + 1)})
			trade.HoldingDays = end - start + 1
		}
		trades = append(trades, trade)
	}
	if len(changes) == 0 {
		if issues := AuditShadowLedger(previous); len(issues) > 0 {
			return previous, changes, fmt.Errorf("存在不能自动修复的问题: %s", issues[0].Message)
		}
		return previous, changes, nil
	}
	report.Orders, report.Trades = orders, trades
	report.RemainingCash += refund
	report.TotalFees -= refund
	report.CompletedTrades = len(trades)
	if refund != 0 {
		changes = append(changes, LedgerCorrection{"cash-refund", "", previous.RemainingCash, report.RemainingCash})
	}
	recomputeTradeStats(&report)
	recomputeAccountMetrics(&report)
	if issues := AuditShadowLedger(report); len(issues) > 0 {
		return previous, changes, fmt.Errorf("修复后对账仍失败: %s (%s)", issues[0].Message, issues[0].EventID)
	}
	report.EngineVersion = ShadowEngineVersion
	report.LedgerRevision++
	return report, changes, nil
}
