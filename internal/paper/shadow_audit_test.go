package paper

import (
	"testing"
)

func TestShadowRepairZeroTradeAndHoldingDates(t *testing.T) {
	cfg := DefaultConfig()
	buy := ShadowOrder{ID: "buy", Symbol: "sh600000", Side: "buy", Status: OrderFilled, AttemptDate: "2026-08-25", Quantity: 100, Price: 10, Amount: 1000}
	sell := ShadowOrder{ID: "sell", Symbol: "sh600000", Side: "sell", Status: OrderFilled, AttemptDate: "2026-08-26", Quantity: 100, Price: 11, Amount: 1100}
	fees := transactionFee(1000, "buy", cfg) + transactionFee(1100, "sell", cfg)
	valid := ShadowTrade{ID: "valid", EntryOrderID: "buy", ExitOrderID: "sell", Symbol: "sh600000", EntryDate: buy.AttemptDate, ExitDate: sell.AttemptDate, Quantity: 100, EntryPrice: 10, ExitPrice: 11, GrossProfit: 100, NetProfit: 100 - fees, TotalFee: fees, HoldingDays: 5046}
	zero := sell
	zero.ID, zero.Quantity, zero.Amount = "zero-sell", 0, 0
	ghost := valid
	ghost.ID, ghost.EntryOrderID, ghost.ExitOrderID = "ghost", "absent", zero.ID
	ghost.Quantity, ghost.GrossProfit, ghost.NetProfit, ghost.TotalFee, ghost.HoldingDays = 0, 0, -1005, 5, 2
	report := Report{AsOf: "2026-08-26", Config: cfg, InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash + 100 - fees - 5, TotalEquity: cfg.InitialCash + 100 - fees - 5, TotalFees: fees + 5, FilledEntries: 1, CompletedTrades: 2, Orders: []ShadowOrder{buy, sell, zero}, Trades: []ShadowTrade{valid, ghost}}
	recomputeAccountMetrics(&report)
	if len(AuditShadowLedger(report)) < 3 {
		t.Fatal("corruption not detected")
	}
	next, changes, err := RepairShadowLedger(report, []string{"2026-08-25", "2026-08-26"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) < 3 || len(next.Orders) != 2 || len(next.Trades) != 1 || next.Trades[0].HoldingDays != 2 || next.LedgerRevision != 1 || next.RemainingCash != report.RemainingCash+5 || next.UnrealizedProfit > .001 {
		t.Fatalf("unexpected repair: %+v", next)
	}
	if len(AuditShadowLedger(next)) != 0 {
		t.Fatal("repair did not reconcile")
	}
	if report.Trades[0].HoldingDays != 5046 || len(report.Orders) != 3 {
		t.Fatal("source report mutated")
	}
	again, changes, err := RepairShadowLedger(next, nil)
	if err != nil || len(changes) != 0 || again.LedgerRevision != 1 {
		t.Fatal("repair not idempotent")
	}
	next.Positions = []ShadowOpenPosition{{Symbol: "sh600000", Quantity: 100}}
	if _, _, err := RepairShadowLedger(next, nil); err == nil {
		t.Fatal("repair accepted open positions")
	}
}
