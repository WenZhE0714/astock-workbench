package paper

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

type shadowHistoryStub struct{ bars map[string][]domain.DailyBar }

func (stub shadowHistoryStub) FetchDailyBars(_ context.Context, symbol string) ([]domain.DailyBar, error) {
	return stub.bars[symbol], nil
}

func shadowBar(symbol, date string, open, close, high, low, amount float64) domain.DailyBar {
	return domain.DailyBar{Symbol: symbol, Source: "test", Date: date, Open: open, Close: close, High: high, Low: low, Amount: amount}
}

func shadowSignal(id, symbol, date string, score float64) realtime.Signal {
	return realtime.Signal{ID: id, Symbol: symbol, Name: "测试", Score: score, State: realtime.StateTriggered, AsOf: time.Date(2026, 8, 20, 14, 30, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))}
}

func TestEvaluatorUsesNextOpenAndKeepsTheoreticalGap(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-19", 99, 99, 100, 98, 1_000_000),
		shadowBar(symbol, "2026-08-20", 100, 100, 101, 99, 1_000_000),
		shadowBar(symbol, "2026-08-21", 110, 112, 113, 109, 1_000_000),
		shadowBar(symbol, "2026-08-24", 120, 121, 122, 119, 1_000_000),
		shadowBar(symbol, "2026-08-25", 122, 123, 124, 121, 1_000_000),
		shadowBar(symbol, "2026-08-26", 124, 125, 126, 123, 1_000_000),
		shadowBar(symbol, "2026-08-27", 125, 126, 127, 124, 1_000_000),
	}
	evaluator := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars}})
	report, err := evaluator.Evaluate(context.Background(), []realtime.Signal{shadowSignal("s1", symbol, "2026-08-20", 80)}, Options{Now: func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.Local) }})
	if err != nil {
		t.Fatal(err)
	}
	if report.FilledEntries != 1 || report.CompletedTrades != 1 || len(report.Trades) != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	trade := report.Trades[0]
	if trade.EntryDate != "2026-08-21" || trade.ExitDate != "2026-08-27" || trade.EntryPrice <= 110 {
		t.Fatalf("unexpected execution dates/prices: %+v", trade)
	}
	if math.Abs(trade.TheoreticalReturnPercent-(126.0/100.0-1)*100) > 1e-9 {
		t.Fatalf("theoretical return leaked: %+v", trade)
	}
	if trade.ExecutableReturnPercent >= trade.TheoreticalReturnPercent {
		t.Fatalf("fees/slippage should reduce executable return: %+v", trade)
	}
}

func TestEvaluatorRejectsCapacityAndOnePriceBar(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-19", 99, 99, 100, 98, 1_000_000),
		shadowBar(symbol, "2026-08-20", 100, 100, 101, 99, 1_000_000),
		shadowBar(symbol, "2026-08-21", 110, 110, 110, 110, 100),
		shadowBar(symbol, "2026-08-24", 112, 113, 114, 111, 1_000_000),
		shadowBar(symbol, "2026-08-25", 114, 115, 116, 113, 1_000_000),
		shadowBar(symbol, "2026-08-26", 116, 117, 118, 115, 1_000_000),
		shadowBar(symbol, "2026-08-27", 118, 119, 120, 117, 1_000_000),
		shadowBar(symbol, "2026-08-28", 120, 121, 122, 119, 1_000_000),
	}
	evaluator := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars}})
	config := DefaultConfig()
	config.MaxParticipationPercent = 0.01
	report, err := evaluator.Evaluate(context.Background(), []realtime.Signal{shadowSignal("s1", symbol, "2026-08-20", 80)}, Options{Config: config})
	if err != nil {
		t.Fatal(err)
	}
	if report.FilledEntries != 0 || report.RejectedOrders == 0 || len(report.Rejections) == 0 {
		t.Fatalf("expected rejection: %+v", report)
	}
	if report.Rejections[0].Reason != "次日开盘一字板或停牌，无法执行" && report.Rejections[0].Reason != "成交额容量不足一手" {
		t.Fatalf("unexpected rejection reason: %+v", report.Rejections[0])
	}
}

func TestRepresentativeSignalsIsIdempotent(t *testing.T) {
	one := shadowSignal("morning", "sh600000", "2026-08-20", 60)
	two := shadowSignal("afternoon", "sh600000", "2026-08-20", 80)
	selected := representativeSignals([]realtime.Signal{one, two}, 10)
	if len(selected) != 1 || selected[0].ID != "afternoon" {
		t.Fatalf("unexpected representative signal: %+v", selected)
	}
}

func TestRepresentativeSignalsZeroLimitKeepsAllDates(t *testing.T) {
	first := shadowSignal("first", "sh600000", "2026-08-20", 60)
	second := shadowSignal("second", "sh600000", "2026-08-21", 70)
	second.AsOf = time.Date(2026, 8, 21, 14, 30, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	selected := representativeSignals([]realtime.Signal{first, second}, 0)
	if len(selected) != 2 {
		t.Fatalf("zero limit unexpectedly truncated archived signals: %+v", selected)
	}
}

func TestEvaluatorKeepsImmatureWindowPending(t *testing.T) {
	symbol := "sh600000"
	signal := shadowSignal("pending", symbol, "2026-08-20", 80)
	signal.Price = 10.5
	bars := []domain.DailyBar{shadowBar(symbol, "2026-08-19", 10, 10.2, 10.3, 9.9, 1_000_000), shadowBar(symbol, "2026-08-20", 10, 10.4, 10.6, 9.9, 1_000_000)}
	calendar := []domain.DailyBar{shadowBar("sh000300", "2026-08-20", 1, 1, 1.1, .9, 1_000_000)}
	evaluator := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}})
	report, err := evaluator.Evaluate(context.Background(), []realtime.Signal{signal}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.PendingCandidates != 1 || report.RejectedOrders != 0 || len(report.Rejections) != 0 {
		t.Fatalf("immature signal was treated as rejected: %+v", report)
	}
}

func TestCapacityUsesOnlyAmountsKnownAtSignalTime(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-19", 10, 10, 10.2, 9.8, 1_000),
		shadowBar(symbol, "2026-08-20", 10, 10, 10.2, 9.8, 1_000_000_000),
		shadowBar(symbol, "2026-08-21", 10, 10, 10.2, 9.8, 1_000_000_000),
		shadowBar(symbol, "2026-08-24", 10, 10, 10.2, 9.8, 1_000_000_000),
		shadowBar(symbol, "2026-08-25", 10, 10, 10.2, 9.8, 1_000_000_000),
		shadowBar(symbol, "2026-08-26", 10, 10, 10.2, 9.8, 1_000_000_000),
		shadowBar(symbol, "2026-08-27", 10, 10, 10.2, 9.8, 1_000_000_000),
	}
	calendar := append([]domain.DailyBar(nil), bars...)
	for index := range calendar {
		calendar[index].Symbol = "sh000300"
	}
	signal := shadowSignal("capacity", symbol, "2026-08-20", 80)
	signal.Price = 10
	evaluator := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}})
	report, err := evaluator.Evaluate(context.Background(), []realtime.Signal{signal}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.FilledEntries != 0 || report.RejectedOrders != 1 || report.Rejections[0].Reason != "成交额容量不足一手" {
		t.Fatalf("future entry-day amount affected capacity: %+v", report)
	}
}

func TestMissingHistoricalAmountIsPendingInsteadOfRejected(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-19", 10, 10, 10.2, 9.8, 0),
		shadowBar(symbol, "2026-08-20", 10, 10, 10.2, 9.8, 0),
		shadowBar(symbol, "2026-08-21", 10, 10, 10.2, 9.8, 1_000_000),
		shadowBar(symbol, "2026-08-24", 10, 10, 10.2, 9.8, 1_000_000),
		shadowBar(symbol, "2026-08-25", 10, 10, 10.2, 9.8, 1_000_000),
		shadowBar(symbol, "2026-08-26", 10, 10, 10.2, 9.8, 1_000_000),
		shadowBar(symbol, "2026-08-27", 10, 10, 10.2, 9.8, 1_000_000),
	}
	calendar := append([]domain.DailyBar(nil), bars...)
	for index := range calendar {
		calendar[index].Symbol = "sh000300"
	}
	signal := shadowSignal("missing-amount", symbol, "2026-08-20", 80)
	signal.Price = 10
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Evaluate(context.Background(), []realtime.Signal{signal}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.PendingCandidates != 1 || report.RejectedOrders != 0 {
		t.Fatalf("missing amount was treated as execution rejection: %+v", report)
	}
}

func TestEvaluatorKeepsEnteredButImmatureTradeAsOpenPosition(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-19", 10, 10, 10.2, 9.8, 1_000_000),
		shadowBar(symbol, "2026-08-20", 10, 10.2, 10.3, 9.9, 1_000_000),
		shadowBar(symbol, "2026-08-21", 10.5, 10.8, 10.9, 10.4, 1_000_000),
		shadowBar(symbol, "2026-08-24", 10.9, 11, 11.1, 10.8, 1_000_000),
	}
	calendar := append([]domain.DailyBar(nil), bars...)
	for index := range calendar {
		calendar[index].Symbol = "sh000300"
	}
	signal := shadowSignal("open-position", symbol, "2026-08-20", 80)
	signal.Price = 10.2
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Evaluate(context.Background(), []realtime.Signal{signal}, Options{Now: func() time.Time { return time.Date(2026, 8, 24, 16, 0, 0, 0, time.Local) }})
	if err != nil {
		t.Fatal(err)
	}
	if report.FilledEntries != 1 || report.OpenPositions != 1 || report.CompletedTrades != 0 || len(report.Positions) != 1 {
		t.Fatalf("immature entered trade missing: %+v", report)
	}
	if report.Positions[0].EntryDate != "2026-08-21" || report.Positions[0].LastDate != "2026-08-24" {
		t.Fatalf("unexpected open position: %+v", report.Positions[0])
	}
}

func TestRevaluePositionsOnlyUpdatesMarkToMarketFields(t *testing.T) {
	generated := time.Date(2026, 8, 21, 15, 0, 0, 0, time.Local)
	valued := time.Date(2026, 8, 21, 15, 1, 0, 0, time.Local)
	report := Report{
		GeneratedAt:   generated,
		Config:        DefaultConfig(),
		RemainingCash: 12345,
		FilledEntries: 1,
		Positions: []ShadowOpenPosition{{
			Symbol: "sh600000", Quantity: 100, EntryPrice: 10,
			LastDate: "2026-08-21", LastPrice: 10.5, MarketValue: 1050,
			UnrealizedProfit: 1, UnrealizedReturnPercent: .1,
		}},
		Orders: []ShadowOrder{{ID: "buy-1", Symbol: "sh600000", Quantity: 100, Status: OrderFilled}},
	}
	updated := RevaluePositions(report, []PositionQuote{{
		Symbol: "sh600000", Price: 11.2, QuoteTime: "2026-08-21 15:01:00", Source: "test-l1",
	}}, valued)
	if updated.RemainingCash != report.RemainingCash || updated.FilledEntries != report.FilledEntries || len(updated.Orders) != len(report.Orders) {
		t.Fatalf("revaluation changed execution state: before=%+v after=%+v", report, updated)
	}
	if len(updated.Positions) != 1 || updated.Positions[0].LastPrice != 11.2 || updated.Positions[0].MarketValue != 1120 {
		t.Fatalf("quote was not applied: %+v", updated.Positions)
	}
	position := updated.Positions[0]
	if position.ValuationTime != "2026-08-21 15:01:00" || position.ValuationSource != "test-l1" || !position.RealtimeValuation {
		t.Fatalf("missing realtime valuation metadata: %+v", position)
	}
	if updated.ValuedAt == nil || !updated.ValuedAt.Equal(valued) {
		t.Fatalf("unexpected valued_at: %v", updated.ValuedAt)
	}
	if report.Positions[0].LastPrice != 10.5 {
		t.Fatalf("input report was mutated: %+v", report.Positions[0])
	}
}

func TestRevaluePositionsIgnoresInvalidOrUnknownQuotes(t *testing.T) {
	report := Report{Config: DefaultConfig(), Positions: []ShadowOpenPosition{{Symbol: "sh600000", Quantity: 100, EntryPrice: 10, LastPrice: 10}}}
	updated := RevaluePositions(report, []PositionQuote{{Symbol: "sh600001", Price: 12}, {Symbol: "sh600000", Price: 0}}, time.Now())
	if updated.ValuedAt != nil || updated.Positions[0].LastPrice != 10 || updated.Positions[0].RealtimeValuation {
		t.Fatalf("invalid quote changed report: %+v", updated)
	}
}

func TestValidateTransitionRejectsRetroactiveLedgerChanges(t *testing.T) {
	baseOrder := ShadowOrder{ID: "signal-buy", Symbol: "sh600000", Side: "buy", SignalDate: "2026-08-20", AttemptDate: "2026-08-21", Quantity: 100, RawPrice: 10, Price: 10.005, Amount: 1000.5, Status: OrderFilled}
	basePosition := ShadowOpenPosition{Symbol: "sh600000", SignalDate: "2026-08-20", EntryDate: "2026-08-21", Quantity: 100, EntryPrice: 10.005}
	previous := Report{AsOf: "2026-08-21", Orders: []ShadowOrder{baseOrder}, Positions: []ShadowOpenPosition{basePosition}}

	tests := []struct {
		name string
		next Report
	}{
		{name: "missing order", next: Report{AsOf: "2026-08-22"}},
		{name: "changed quantity", next: Report{AsOf: "2026-08-22", Orders: []ShadowOrder{func() ShadowOrder { item := baseOrder; item.Quantity = 200; return item }()}, Positions: []ShadowOpenPosition{basePosition}}},
		{name: "position disappeared", next: Report{AsOf: "2026-08-22", Orders: []ShadowOrder{baseOrder}}},
		{name: "past order added", next: Report{AsOf: "2026-08-22", Orders: []ShadowOrder{baseOrder, {ID: "other-buy", Symbol: "sh600001", Side: "buy", AttemptDate: "2026-08-21", Quantity: 100, Status: OrderFilled}}, Positions: []ShadowOpenPosition{basePosition}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateTransition(previous, test.next); err == nil {
				t.Fatalf("retroactive ledger change was accepted: %+v", test.next)
			}
		})
	}
}

func TestValidateTransitionAllowsMarkToMarketAndFutureExit(t *testing.T) {
	order := ShadowOrder{ID: "signal-buy", Symbol: "sh600000", Side: "buy", SignalDate: "2026-08-20", AttemptDate: "2026-08-21", Quantity: 100, RawPrice: 10, Price: 10.005, Amount: 1000.5, Status: OrderFilled}
	position := ShadowOpenPosition{Symbol: "sh600000", SignalDate: "2026-08-20", EntryDate: "2026-08-21", Quantity: 100, EntryPrice: 10.005, LastPrice: 10.2}
	previous := Report{AsOf: "2026-08-21", Orders: []ShadowOrder{order}, Positions: []ShadowOpenPosition{position}}

	marked := previous
	marked.AsOf = "2026-08-22"
	marked.Positions = append([]ShadowOpenPosition(nil), position)
	marked.Positions[0].LastPrice = 10.8
	if err := ValidateTransition(previous, marked); err != nil {
		t.Fatalf("mark-to-market was rejected: %v", err)
	}

	exited := Report{
		AsOf:   "2026-08-25",
		Orders: []ShadowOrder{order, {ID: "signal-sell", Symbol: "sh600000", Side: "sell", SignalDate: "2026-08-20", AttemptDate: "2026-08-25", Quantity: 100, RawPrice: 11, Price: 10.9945, Amount: 1099.45, Status: OrderFilled}},
		Trades: []ShadowTrade{{ID: "ST0001", Symbol: "sh600000", SignalDate: "2026-08-20", EntryDate: "2026-08-21", ExitDate: "2026-08-25", Quantity: 100, EntryPrice: 10.005, ExitPrice: 10.9945}},
	}
	if err := ValidateTransition(previous, exited); err != nil {
		t.Fatalf("future exit was rejected: %v", err)
	}
}
