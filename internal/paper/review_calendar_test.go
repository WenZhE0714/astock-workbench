package paper

import (
	"math"
	"testing"
)

func TestCalendarLedgerUsesNetProfitAndRejectsInvalidRecords(t *testing.T) {
	valid := ShadowTrade{ID: "t1", Symbol: "sh600519", EntryDate: "2026-09-30", ExitDate: "2026-10-09", ExitTime: "2026-10-09 10:00:00", Quantity: 100, EntryPrice: 10, ExitPrice: 11, NetProfit: 88, TotalFee: 12}
	bad := valid
	bad.ID = "bad"
	bad.NetProfit = math.NaN()
	future := valid
	future.ID = "future"
	future.ExitDate = "2026-10-12"
	future.ExitTime = ""
	mismatch := valid
	mismatch.ID = "mismatch"
	mismatch.NetProfit = 99
	report := Report{AsOf: "2026-10-09", CheckpointPhase: CheckpointClose, Trades: []ShadowTrade{valid, valid, bad, future, mismatch}}
	events, warnings := ReviewCalendarEvents(report)
	if len(events) != 1 || events[0].NetProfit == nil || *events[0].NetProfit != 88 || events[0].R != nil || len(warnings) == 0 {
		t.Fatalf("bad ledger events: %+v %v", events, warnings)
	}
	conflict := valid
	conflict.NetProfit = 77
	report.Trades = []ShadowTrade{valid, conflict}
	if events, warnings = ReviewCalendarEvents(report); len(events) != 0 || len(warnings) == 0 {
		t.Fatal("conflicting duplicate was credited")
	}
}

func TestCalendarExcludesFillsBeyondIntradayWatermark(t *testing.T) {
	trade := ShadowTrade{ID: "t1", Symbol: "sh600519", EntryDate: "2026-09-30", ExitDate: "2026-10-09", ExitTime: "2026-10-09 15:00:00", Quantity: 100, EntryPrice: 10, ExitPrice: 11, NetProfit: 100}
	order := ShadowOrder{ID: "o1", Symbol: "sh600519", Side: "buy", Status: OrderFilled, AttemptDate: "2026-10-09", ExecutionTime: "2026-10-09 11:00:00", Quantity: 100, Price: 10}
	report := Report{AsOf: "2026-10-09", CheckpointPhase: CheckpointOpen, LastRealtimeAt: "2026-10-09 10:30:00", Trades: []ShadowTrade{trade}, Orders: []ShadowOrder{order}}
	if events, _ := ReviewCalendarEvents(report); len(events) != 0 {
		t.Fatalf("future same-day fills leaked: %+v", events)
	}
	report.LastRealtimeAt = "2026-10-09 11:30:00"
	if events, _ := ReviewCalendarEvents(report); len(events) != 1 || events[0].Kind != "entry" {
		t.Fatalf("wrong watermark: %+v", events)
	}
	report.CheckpointPhase = CheckpointClose
	if events, _ := ReviewCalendarEvents(report); len(events) != 2 {
		t.Fatalf("close lost fills: %+v", events)
	}
}
