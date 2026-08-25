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
	asOf, _ := time.ParseInLocation("2006-01-02 15:04", date+" 14:30", shanghaiLocation)
	return realtime.Signal{ID: id, Symbol: symbol, Name: "测试", Score: score, State: realtime.StateTriggered, AsOf: asOf}
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
	one.AsOf = one.AsOf.Add(-4 * time.Hour)
	selected := representativeSignals([]realtime.Signal{one, two}, 10)
	if len(selected) != 1 || selected[0].ID != "afternoon" {
		t.Fatalf("unexpected representative signal: %+v", selected)
	}
}

func TestRepresentativeSignalsUsesLatestDailyStateInsteadOfIntradayHigh(t *testing.T) {
	morning := shadowSignal("morning-high", "sh600000", "2026-08-20", 82)
	morning.AsOf = morning.AsOf.Add(-4 * time.Hour)
	close := shadowSignal("close-weak", "sh600000", "2026-08-20", 54)
	close.State = realtime.StateWeak
	selected := representativeSignals([]realtime.Signal{morning, close}, 0)
	if len(selected) != 1 || selected[0].ID != "close-weak" {
		t.Fatalf("intraday high replaced the latest daily state: %+v", selected)
	}
}

func TestShadowPortfolioKeepsDailyCashAndInitialEntryRoom(t *testing.T) {
	const initialCash = 1_000_000.0
	symbols := []string{"sh600000", "sh600001", "sh600002", "sh600003", "sh600004"}
	barsBySymbol := make(map[string][]domain.DailyBar, len(symbols)+1)
	signals := make([]realtime.Signal, 0, len(symbols))
	for index, symbol := range symbols {
		barsBySymbol[symbol] = []domain.DailyBar{
			shadowBar(symbol, "2026-08-19", 10, 10, 10.2, 9.8, 100_000_000),
			shadowBar(symbol, "2026-08-20", 10, 10, 10.2, 9.8, 100_000_000),
			shadowBar(symbol, "2026-08-21", 10, 10, 10.2, 9.8, 100_000_000),
		}
		signals = append(signals, shadowSignal("same-day-"+symbol, symbol, "2026-08-20", 80-float64(index)))
	}
	barsBySymbol["sh000300"] = []domain.DailyBar{
		shadowBar("sh000300", "2026-08-19", 1, 1, 1.1, .9, 100_000_000),
		shadowBar("sh000300", "2026-08-20", 1, 1, 1.1, .9, 100_000_000),
		shadowBar("sh000300", "2026-08-21", 1, 1, 1.1, .9, 100_000_000),
	}
	report, err := NewEvaluator(shadowHistoryStub{bars: barsBySymbol}).Evaluate(context.Background(), signals, Options{Now: func() time.Time {
		return time.Date(2026, 8, 21, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	deployed := initialCash - report.RemainingCash
	if deployed > initialCash*.35+1e-6 || report.RemainingCash < initialCash*.65-1e-6 {
		t.Fatalf("same-day deployment exhausted account room: deployed=%.2f cash=%.2f report=%+v", deployed, report.RemainingCash, report)
	}
	if report.FilledEntries < 3 {
		t.Fatalf("daily cap prevented reasonable diversification: %+v", report)
	}
	cfg := DefaultConfig()
	for _, order := range report.Orders {
		if order.Side != "buy" || order.Status != OrderFilled {
			continue
		}
		cost := order.Amount + transactionFee(order.Amount, "buy", cfg)
		if cost > initialCash*.10+1e-6 {
			t.Fatalf("initial entry exceeded 10%% account target: order=%+v cost=%.2f", order, cost)
		}
		if order.PositionAction != "open" || order.PositionSequence != 1 {
			t.Fatalf("initial entry action missing: %+v", order)
		}
	}
}

func TestShadowPositionAddsOnlyAfterStrongerSignal(t *testing.T) {
	symbol := "sh600000"
	bars := shadowStrategyBars(symbol)
	calendar := shadowStrategyBars("sh000300")
	first := shadowSignal("first", symbol, "2026-08-20", 70)
	stronger := shadowSignal("stronger", symbol, "2026-08-21", 75)
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Evaluate(context.Background(), []realtime.Signal{first, stronger}, Options{Now: func() time.Time {
		return time.Date(2026, 8, 24, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.FilledEntries != 2 || len(report.Positions) != 1 || len(report.Positions[0].Lots) != 2 || report.Positions[0].AdditionCount != 1 {
		t.Fatalf("stronger signal did not add a second tranche: %+v", report)
	}
	if len(report.Orders) != 2 || report.Orders[0].PositionAction != "open" || report.Orders[1].PositionAction != "add" {
		t.Fatalf("unexpected position actions: %+v", report.Orders)
	}
}

func TestShadowPositionUsesThreeStagedTranches(t *testing.T) {
	symbol := "sh600000"
	bars := shadowStrategyBars(symbol)
	calendar := shadowStrategyBars("sh000300")
	cfg := DefaultConfig()
	cfg.HoldingDays = 20
	signals := []realtime.Signal{
		shadowSignal("first", symbol, "2026-08-20", 70),
		shadowSignal("second", symbol, "2026-08-21", 75),
		shadowSignal("third", symbol, "2026-08-24", 80),
	}
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Evaluate(context.Background(), signals, Options{Config: cfg, Now: func() time.Time {
		return time.Date(2026, 8, 25, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.FilledEntries != 3 || len(report.Positions) != 1 || len(report.Positions[0].Lots) != 3 || report.Positions[0].AdditionCount != 2 {
		t.Fatalf("three-stage position was not created: %+v", report)
	}
	maximumCosts := []float64{cfg.InitialCash * .10, cfg.InitialCash * .05, cfg.InitialCash * .05}
	totalCost := 0.0
	for index, lot := range report.Positions[0].Lots {
		cost := lot.EntryAmount + lot.EntryFee
		totalCost += cost
		if cost > maximumCosts[index]+1e-6 {
			t.Fatalf("tranche %d exceeded staged budget: cost=%.2f maximum=%.2f lot=%+v", index+1, cost, maximumCosts[index], lot)
		}
	}
	if totalCost > cfg.InitialCash*cfg.MaxPositionPercent/100+1e-6 {
		t.Fatalf("three tranches exceeded single-stock cap: %.2f", totalCost)
	}
}

func TestShadowPositionHoldsWhenSignalImprovementIsTooSmall(t *testing.T) {
	symbol := "sh600000"
	bars := shadowStrategyBars(symbol)
	calendar := shadowStrategyBars("sh000300")
	first := shadowSignal("first", symbol, "2026-08-20", 70)
	minor := shadowSignal("minor", symbol, "2026-08-21", 72)
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Evaluate(context.Background(), []realtime.Signal{first, minor}, Options{Now: func() time.Time {
		return time.Date(2026, 8, 24, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.FilledEntries != 1 || len(report.Positions) != 1 || len(report.Positions[0].Lots) != 1 {
		t.Fatalf("minor score change unexpectedly added a tranche: %+v", report)
	}
	if !hasShadowDecision(report.Decisions, "hold") {
		t.Fatalf("hold decision missing: %+v", report.Decisions)
	}
}

func TestShadowWeakSignalReducesOldestSellableTranche(t *testing.T) {
	symbol := "sh600000"
	bars := shadowStrategyBars(symbol)
	calendar := shadowStrategyBars("sh000300")
	cfg := DefaultConfig()
	cfg.HoldingDays = 20
	first := shadowSignal("first", symbol, "2026-08-20", 70)
	stronger := shadowSignal("stronger", symbol, "2026-08-21", 75)
	weak := shadowSignal("weak", symbol, "2026-08-24", 70)
	weak.State = realtime.StateWeak
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Evaluate(context.Background(), []realtime.Signal{first, stronger, weak}, Options{Config: cfg, Now: func() time.Time {
		return time.Date(2026, 8, 25, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.FilledEntries != 2 || report.CompletedTrades != 1 || len(report.Positions) != 1 || len(report.Positions[0].Lots) != 1 {
		t.Fatalf("weak signal did not reduce exactly one tranche: %+v", report)
	}
	if report.Trades[0].EntryOrderID != "first-buy" || report.Trades[0].PositionAction != "reduce" || report.Trades[0].ExitTime != "2026-08-25 09:30:00" {
		t.Fatalf("oldest tranche was not reduced at the next open: %+v", report.Trades[0])
	}
	if report.Positions[0].Lots[0].OrderID != "stronger-buy" || !hasShadowDecision(report.Decisions, "reduce") {
		t.Fatalf("remaining tranche or decision mismatch: position=%+v decisions=%+v", report.Positions[0], report.Decisions)
	}
}

func TestShadowReductionCannotSellSameDayTranche(t *testing.T) {
	cfg := DefaultConfig()
	date := "2026-08-21"
	symbol := "sh600000"
	bars := []domain.DailyBar{shadowBar(symbol, date, 10, 10.1, 10.2, 9.9, 100_000_000)}
	entrySignal := shadowSignal("entry", symbol, "2026-08-20", 80)
	entry := makeOrder(entrySignal, "buy", date, 10, 10_000, cfg, 0)
	entry.Status = OrderFilled
	entry.PositionAction = "open"
	lot := shadowLot{entry: entry, entryCost: entry.Amount + transactionFee(entry.Amount, "buy", cfg), plan: shadowPlan{signal: entrySignal, bars: bars, entryIndex: 0, signalClose: 10}}
	weak := shadowSignal("same-day-weak", symbol, "2026-08-20", 70)
	weak.State = realtime.StateWeak
	plan := shadowPlan{signal: weak, bars: bars, entryDate: date, entryIndex: 0, signalClose: 10}
	report := Report{EngineVersion: ShadowEngineVersion, Config: cfg, InitialCash: cfg.InitialCash, RemainingCash: 899_000, AsOf: date}
	active := map[string]shadowPosition{symbol: {plan: lot.plan, lots: []shadowLot{lot}, lastSignalScore: 80}}
	result := simulateFrom(report, []shadowPlan{plan}, cfg, report.RemainingCash, active)
	if result.CompletedTrades != 0 || len(result.Positions) != 1 || result.Positions[0].Quantity != entry.Quantity {
		t.Fatalf("same-day tranche violated T+1: %+v", result)
	}
	if !hasShadowDecision(result.Decisions, "hold") {
		t.Fatalf("T+1 hold decision missing: %+v", result.Decisions)
	}
}

func TestShadowExpiryClosesOnlyMatureTranche(t *testing.T) {
	symbol := "sh600000"
	bars := shadowStrategyBars(symbol)
	calendar := shadowStrategyBars("sh000300")
	cfg := DefaultConfig()
	cfg.HoldingDays = 2
	first := shadowSignal("first", symbol, "2026-08-20", 70)
	stronger := shadowSignal("stronger", symbol, "2026-08-21", 75)
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Evaluate(context.Background(), []realtime.Signal{first, stronger}, Options{Config: cfg, Now: func() time.Time {
		return time.Date(2026, 8, 24, 15, 6, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.FilledEntries != 2 || report.CompletedTrades != 1 || len(report.Positions) != 1 || len(report.Positions[0].Lots) != 1 {
		t.Fatalf("mature tranche exit changed the whole position: %+v", report)
	}
	if report.Trades[0].EntryOrderID != "first-buy" || report.Trades[0].PositionAction != "exit" || report.Positions[0].Lots[0].OrderID != "stronger-buy" {
		t.Fatalf("wrong tranche exited: trades=%+v position=%+v", report.Trades, report.Positions[0])
	}
}

func TestShadowOpeningBuyCannotUseSameDayClosingProceeds(t *testing.T) {
	cfg := DefaultConfig()
	date := "2026-08-24"
	oldSymbol, newSymbol := "sh600000", "sh600001"
	oldBars := []domain.DailyBar{
		shadowBar(oldSymbol, "2026-08-21", 10, 10, 10.2, 9.8, 100_000_000),
		shadowBar(oldSymbol, date, 10, 10, 10.2, 9.8, 100_000_000),
	}
	newBars := []domain.DailyBar{
		shadowBar(newSymbol, "2026-08-21", 10, 10, 10.2, 9.8, 100_000_000),
		shadowBar(newSymbol, date, 10, 10, 10.2, 9.8, 100_000_000),
	}
	oldSignal := shadowSignal("old", oldSymbol, "2026-08-20", 70)
	entry := makeOrder(oldSignal, "buy", "2026-08-21", 10, 80_000, cfg, 0)
	entry.Status = OrderFilled
	entry.PositionAction = "open"
	lotPlan := shadowPlan{signal: oldSignal, bars: oldBars, entryIndex: 0, exitIndex: 1, targetExitDate: date, signalClose: 10}
	lot := shadowLot{plan: lotPlan, entry: entry, entryCost: entry.Amount + transactionFee(entry.Amount, "buy", cfg)}
	exitPlan := lotPlan
	exitPlan.lotOrderID = entry.ID
	exitPlan.exitDate = date
	newSignal := shadowSignal("new", newSymbol, "2026-08-21", 80)
	entryPlan := shadowPlan{signal: newSignal, bars: newBars, capacity: 100_000_000, entryDate: date, entryIndex: 1, signalClose: 10}
	report := Report{EngineVersion: ShadowEngineVersion, Config: cfg, InitialCash: cfg.InitialCash, RemainingCash: 200_000, FilledEntries: 1, AsOf: date, Orders: []ShadowOrder{entry}}
	active := map[string]shadowPosition{oldSymbol: {plan: lotPlan, lots: []shadowLot{lot}, lastSignalScore: 70}}
	result := simulateFrom(report, []shadowPlan{exitPlan, entryPlan}, cfg, report.RemainingCash, active)
	for _, order := range result.Orders {
		if order.Symbol == newSymbol && order.Side == "buy" && order.Status == OrderFilled {
			t.Fatalf("closing proceeds funded a retroactive opening buy: %+v", result.Orders)
		}
	}
	if result.CompletedTrades != 1 || result.RemainingCash <= report.RemainingCash {
		t.Fatalf("closing exit did not settle after skipped opening buy: %+v", result)
	}
}

func shadowStrategyBars(symbol string) []domain.DailyBar {
	dates := []string{"2026-08-19", "2026-08-20", "2026-08-21", "2026-08-24", "2026-08-25", "2026-08-26", "2026-08-27", "2026-08-28"}
	result := make([]domain.DailyBar, 0, len(dates))
	for index, date := range dates {
		price := 10 + float64(index)*.1
		result = append(result, shadowBar(symbol, date, price, price+.03, price+.2, price-.2, 100_000_000))
	}
	return result
}

func hasShadowDecision(decisions []ShadowDecision, action string) bool {
	for _, decision := range decisions {
		if decision.Action == action {
			return true
		}
	}
	return false
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
	if report.Positions[0].AvailableQuantity != report.Positions[0].Quantity {
		t.Fatalf("T+1 position did not become sellable on the next trading day: %+v", report.Positions[0])
	}
}

func TestTPlusOneDoesNotSellOnEntryDate(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-19", 9.8, 9.9, 10, 9.7, 1_000_000),
		shadowBar(symbol, "2026-08-20", 10, 10, 10, 10, 1_000_000),
		shadowBar(symbol, "2026-08-21", 10, 11, 11, 10, 1_000_000),
	}
	calendar := append([]domain.DailyBar(nil), bars...)
	for index := range calendar {
		calendar[index].Symbol = "sh000300"
	}
	signal := shadowSignal("same-day", symbol, "2026-08-20", 80)
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Evaluate(context.Background(), []realtime.Signal{signal}, Options{Config: func() Config { cfg := DefaultConfig(); cfg.HoldingDays = 1; return cfg }(), Now: func() time.Time { return time.Date(2026, 8, 21, 16, 0, 0, 0, shanghaiLocation) }})
	if err != nil {
		t.Fatal(err)
	}
	if report.CompletedTrades != 0 || report.OpenPositions != 1 {
		t.Fatalf("same-day T+1 sell occurred: %+v", report)
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

func TestRevaluePositionsComputesAccountEquityAndTotalReturn(t *testing.T) {
	cfg := DefaultConfig()
	entryAmount := 490_000.0
	entryFee := 152.0
	report := Report{
		Config: cfg, InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash - entryAmount - entryFee,
		Positions: []ShadowOpenPosition{{Symbol: "sh600000", Quantity: 10_000, EntryPrice: 49, EntryAmount: entryAmount, EntryFee: entryFee, LastPrice: 49, MarketValue: entryAmount}},
	}
	updated := RevaluePositions(report, []PositionQuote{{Symbol: "sh600000", Price: 50, QuoteTime: "2026-08-24 10:00:00", Source: "test"}}, time.Now())
	if math.Abs(updated.TotalMarketValue-500_000) > 1e-6 || math.Abs(updated.TotalEquity-1_009_848) > 1e-6 {
		t.Fatalf("unexpected account totals: %+v", updated)
	}
	if updated.UnrealizedProfit == 0 || updated.TotalProfit != updated.UnrealizedProfit || updated.TotalReturnPercent == 0 {
		t.Fatalf("open-position return was not included: %+v", updated)
	}
}

func TestMakeOrderCarriesExecutionTimeAndSignalReasons(t *testing.T) {
	signal := shadowSignal("signal", "sh600000", "2026-08-20", 82)
	signal.Reasons = []string{"趋势突破: 站上前高"}
	signal.TriggerPrice = 10.5
	order := makeOrder(signal, "buy", "2026-08-21", 11, 100, DefaultConfig(), 1_000_000)
	if order.ExecutionTime != "2026-08-21 09:30:00" || order.SignalScore != 82 || len(order.SignalReasons) != 1 || order.TriggerPrice != 10.5 {
		t.Fatalf("missing execution context: %+v", order)
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

func TestValidateTransitionRequiresQuantityConservationForReduction(t *testing.T) {
	order := ShadowOrder{ID: "signal-buy", Symbol: "sh600000", Side: "buy", SignalDate: "2026-08-20", AttemptDate: "2026-08-21", Quantity: 200, RawPrice: 10, Price: 10, Amount: 2000, Status: OrderFilled}
	position := ShadowOpenPosition{Symbol: "sh600000", SignalDate: "2026-08-20", EntryDate: "2026-08-21", Quantity: 200, EntryPrice: 10}
	previous := Report{AsOf: "2026-08-21", CheckpointPhase: CheckpointClose, Orders: []ShadowOrder{order}, Positions: []ShadowOpenPosition{position}}
	sell := ShadowOrder{ID: "reduce-sell", Symbol: "sh600000", Side: "sell", AttemptDate: "2026-08-24", Quantity: 100, RawPrice: 11, Price: 11, Amount: 1100, Status: OrderFilled, PositionAction: "reduce"}

	valid := Report{AsOf: "2026-08-24", Orders: []ShadowOrder{order, sell}, Positions: []ShadowOpenPosition{{Symbol: "sh600000", SignalDate: "2026-08-20", EntryDate: "2026-08-21", Quantity: 100, EntryPrice: 10}}}
	if err := ValidateTransition(previous, valid); err != nil {
		t.Fatalf("quantity-conserving reduction was rejected: %v", err)
	}

	invalid := valid
	invalid.Positions = nil
	if err := ValidateTransition(previous, invalid); err == nil {
		t.Fatal("partial reduction was allowed to erase the remaining position")
	}
}

func TestValidateTransitionAllowsOldestLotRemovalToMoveEntryDate(t *testing.T) {
	first := ShadowPositionLot{OrderID: "first-buy", EntryDate: "2026-08-21", Quantity: 100, EntryPrice: 10, EntryAmount: 1000}
	second := ShadowPositionLot{OrderID: "second-buy", EntryDate: "2026-08-24", Quantity: 100, EntryPrice: 11, EntryAmount: 1100}
	previous := Report{
		AsOf: "2026-08-24", CheckpointPhase: CheckpointClose,
		Orders: []ShadowOrder{
			{ID: first.OrderID, Symbol: "sh600000", Side: "buy", AttemptDate: first.EntryDate, Quantity: first.Quantity, Price: first.EntryPrice, RawPrice: first.EntryPrice, Amount: first.EntryAmount, Status: OrderFilled},
			{ID: second.OrderID, Symbol: "sh600000", Side: "buy", AttemptDate: second.EntryDate, Quantity: second.Quantity, Price: second.EntryPrice, RawPrice: second.EntryPrice, Amount: second.EntryAmount, Status: OrderFilled},
		},
		Positions: []ShadowOpenPosition{{Symbol: "sh600000", EntryDate: first.EntryDate, Quantity: 200, EntryPrice: 10.5, Lots: []ShadowPositionLot{first, second}}},
	}
	sell := ShadowOrder{ID: "reduce-sell", Symbol: "sh600000", Side: "sell", AttemptDate: "2026-08-25", Quantity: 100, Price: 12, RawPrice: 12, Amount: 1200, Status: OrderFilled, PositionAction: "reduce"}
	next := Report{
		AsOf: "2026-08-25", Orders: append(append([]ShadowOrder(nil), previous.Orders...), sell),
		Positions: []ShadowOpenPosition{{Symbol: "sh600000", EntryDate: second.EntryDate, Quantity: 100, EntryPrice: 11, Lots: []ShadowPositionLot{second}}},
	}
	if err := ValidateTransition(previous, next); err != nil {
		t.Fatalf("oldest-lot reduction was mistaken for a rebuilt position: %v", err)
	}
}

func TestTradingCheckpointAtSeparatesOpenAndClose(t *testing.T) {
	calendar := []string{"2026-08-21", "2026-08-24"}
	open := TradingCheckpointAt(time.Date(2026, 8, 24, 10, 0, 0, 0, shanghaiLocation), calendar)
	if open.Date != "2026-08-24" || open.Phase != CheckpointOpen {
		t.Fatalf("unexpected open checkpoint: %+v", open)
	}
	close := TradingCheckpointAt(time.Date(2026, 8, 24, 15, 6, 0, 0, shanghaiLocation), calendar)
	if close.Date != "2026-08-24" || close.Phase != CheckpointClose {
		t.Fatalf("unexpected close checkpoint: %+v", close)
	}
	weekend := TradingCheckpointAt(time.Date(2026, 8, 23, 10, 0, 0, 0, shanghaiLocation), calendar)
	if weekend.Date != "2026-08-21" || weekend.Phase != CheckpointClose {
		t.Fatalf("weekend advanced account: %+v", weekend)
	}
}

func TestConfigFingerprintChangesWithExecutionAssumptions(t *testing.T) {
	base := DefaultConfig()
	changed := base
	changed.HoldingDays++
	if ConfigFingerprint(base) == ConfigFingerprint(changed) {
		t.Fatal("holding window did not change config fingerprint")
	}
}

func TestAdvancePreservesLedgerAndClosesOnlyAtCloseCheckpoint(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-20", 10, 10, 10.2, 9.8, 1_000_000),
		shadowBar(symbol, "2026-08-21", 10.5, 10.8, 10.9, 10.4, 1_000_000),
		shadowBar(symbol, "2026-08-24", 11, 11.2, 11.3, 10.9, 1_000_000),
	}
	calendar := append([]domain.DailyBar(nil), bars...)
	for index := range calendar {
		calendar[index].Symbol = "sh000300"
	}
	cfg := DefaultConfig()
	amount := 10.5 * 100
	fee := transactionFee(amount, "buy", cfg)
	previous := Report{
		EngineVersion: ShadowEngineVersion, Config: cfg, ConfigFingerprint: OptionsFingerprint(cfg, 0),
		AsOf: "2026-08-21", CheckpointPhase: CheckpointClose, InitialCash: cfg.InitialCash,
		RemainingCash: cfg.InitialCash - amount - fee, FilledEntries: 1, TotalTurnover: amount, TotalFees: fee,
		Orders:    []ShadowOrder{{ID: "signal-buy", Symbol: symbol, Side: "buy", SignalDate: "2026-08-20", AttemptDate: "2026-08-21", Quantity: 100, RawPrice: 10.5, Price: 10.5, Amount: amount, Status: OrderFilled}},
		Positions: []ShadowOpenPosition{{SignalID: "signal", Symbol: symbol, SignalDate: "2026-08-20", EntryDate: "2026-08-21", Quantity: 100, AvailableQuantity: 100, EntryPrice: 10.5, EntryAmount: amount, EntryFee: fee, SignalClose: 10, TargetExitDate: "2026-08-24"}},
	}
	evaluator := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}})
	openReport, err := evaluator.Advance(context.Background(), previous, nil, Options{Config: cfg, Now: func() time.Time { return time.Date(2026, 8, 24, 10, 0, 0, 0, shanghaiLocation) }})
	if err != nil {
		t.Fatal(err)
	}
	if openReport.CompletedTrades != 0 || len(openReport.Positions) != 1 || openReport.CheckpointPhase != CheckpointOpen {
		t.Fatalf("position closed before daily close: %+v", openReport)
	}
	closeReport, err := evaluator.Advance(context.Background(), openReport, nil, Options{Config: cfg, Now: func() time.Time { return time.Date(2026, 8, 24, 15, 6, 0, 0, shanghaiLocation) }})
	if err != nil {
		t.Fatal(err)
	}
	if closeReport.CompletedTrades != 1 || len(closeReport.Positions) != 0 || len(closeReport.Orders) != 2 || closeReport.TotalTurnover <= openReport.TotalTurnover {
		t.Fatalf("close checkpoint did not append exit: %+v", closeReport)
	}
	if !sameSettledOrder(closeReport.Orders[0], previous.Orders[0]) {
		t.Fatalf("previous order was rewritten: before=%+v after=%+v", previous.Orders[0], closeReport.Orders[0])
	}
	if err := ValidateTransition(openReport, closeReport); err != nil {
		t.Fatalf("open-to-close transition rejected same-day close settlement: %v", err)
	}
}

func TestAdvanceMigratesLegacyAccountWithoutRebuildingLedger(t *testing.T) {
	symbol := "sh600000"
	bars := shadowStrategyBars(symbol)
	calendar := shadowStrategyBars("sh000300")
	cfg := DefaultConfig()
	legacyConfig := cfg
	legacyConfig.MaxPortfolioPercent = 0
	legacyConfig.CashReservePercent = 0
	legacyConfig.MaxDailyDeploymentPercent = 0
	legacyConfig.InitialEntryPercent = 0
	legacyConfig.MaxEntryTranches = 0
	legacyConfig.AdditionScoreStep = 0
	amount := 10.2 * 1_000
	fee := transactionFee(amount, "buy", cfg)
	previous := Report{
		EngineVersion: "tplus1-v6", ConfigFingerprint: "legacy-v6-fingerprint", Config: legacyConfig,
		AsOf: "2026-08-21", CheckpointPhase: CheckpointClose, InitialCash: cfg.InitialCash,
		RemainingCash: cfg.InitialCash - amount - fee, FilledEntries: 1, TotalTurnover: amount, TotalFees: fee,
		Orders:    []ShadowOrder{{ID: "legacy-buy", Symbol: symbol, Side: "buy", SignalDate: "2026-08-20", AttemptDate: "2026-08-21", Quantity: 1_000, RawPrice: 10.2, Price: 10.2, Amount: amount, Status: OrderFilled}},
		Positions: []ShadowOpenPosition{{SignalID: "legacy", Symbol: symbol, SignalDate: "2026-08-20", EntryDate: "2026-08-21", Quantity: 1_000, EntryPrice: 10.2, EntryAmount: amount, EntryFee: fee, SignalScore: 70, SignalClose: 10.1, TargetExitDate: "2026-08-28"}},
	}
	signal := shadowSignal("legacy", symbol, "2026-08-20", 70)
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Advance(context.Background(), previous, []realtime.Signal{signal}, Options{Config: cfg, Now: func() time.Time {
		return time.Date(2026, 8, 24, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.EngineVersion != ShadowEngineVersion || report.RemainingCash != previous.RemainingCash || len(report.Orders) != 1 || len(report.Positions) != 1 || len(report.Positions[0].Lots) != 1 {
		t.Fatalf("legacy account was rebuilt instead of migrated: before=%+v after=%+v", previous, report)
	}
	if report.Orders[0].ID != previous.Orders[0].ID || report.Positions[0].Quantity != previous.Positions[0].Quantity || report.Positions[0].Lots[0].OrderID != previous.Orders[0].ID {
		t.Fatalf("legacy ledger basis changed during migration: %+v", report)
	}
	if err := ValidateTransition(previous, report); err != nil {
		t.Fatalf("legacy migration failed continuity validation: %v", err)
	}
}

func TestAdvanceUsesWeakArchivedSignalForNextOpenReduction(t *testing.T) {
	symbol := "sh600000"
	bars := shadowStrategyBars(symbol)
	calendar := shadowStrategyBars("sh000300")
	cfg := DefaultConfig()
	cfg.HoldingDays = 20
	amount := 10.2 * 1_000
	fee := transactionFee(amount, "buy", cfg)
	previous := Report{
		EngineVersion: ShadowEngineVersion, ConfigFingerprint: OptionsFingerprint(cfg, 0), Config: cfg,
		AsOf: "2026-08-24", CheckpointPhase: CheckpointClose, InitialCash: cfg.InitialCash,
		RemainingCash: cfg.InitialCash - amount - fee, FilledEntries: 1, TotalTurnover: amount, TotalFees: fee,
		Orders:    []ShadowOrder{{ID: "entry-buy", Symbol: symbol, Side: "buy", SignalDate: "2026-08-20", AttemptDate: "2026-08-21", Quantity: 1_000, RawPrice: 10.2, Price: 10.2, Amount: amount, Status: OrderFilled}},
		Positions: []ShadowOpenPosition{{SignalID: "entry", Symbol: symbol, SignalDate: "2026-08-20", EntryDate: "2026-08-21", Quantity: 1_000, EntryPrice: 10.2, EntryAmount: amount, EntryFee: fee, SignalScore: 75, SignalClose: 10.1}},
	}
	weak := shadowSignal("weak-after", symbol, "2026-08-24", 70)
	weak.State = realtime.StateWeak
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Advance(context.Background(), previous, []realtime.Signal{weak}, Options{Config: cfg, Now: func() time.Time {
		return time.Date(2026, 8, 25, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.CompletedTrades != 1 || len(report.Positions) != 0 || len(report.Orders) != 2 || report.Orders[1].PositionAction != "reduce" {
		t.Fatalf("incremental weak signal did not reduce the position: %+v", report)
	}
	if err := ValidateTransition(previous, report); err != nil {
		t.Fatalf("valid next-day reduction failed continuity validation: %v", err)
	}
}

func TestAdvanceKeepsPositionWhenHistoricalBarsAreUnavailable(t *testing.T) {
	cfg := DefaultConfig()
	previous := Report{
		EngineVersion: ShadowEngineVersion, Config: cfg, AsOf: "2026-08-21", RemainingCash: 500,
		Positions: []ShadowOpenPosition{{Symbol: "sh600000", EntryDate: "2026-08-21", Quantity: 100, EntryPrice: 10, LastDate: "2026-08-21", LastPrice: 10}},
	}
	calendar := []domain.DailyBar{shadowBar("sh000300", "2026-08-21", 1, 1, 1, 1, 1), shadowBar("sh000300", "2026-08-24", 1, 1, 1, 1, 1)}
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{"sh000300": calendar}}).Advance(context.Background(), previous, nil, Options{Config: cfg, Now: func() time.Time { return time.Date(2026, 8, 24, 16, 0, 0, 0, shanghaiLocation) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Positions) != 1 || report.Positions[0].Symbol != "sh600000" {
		t.Fatalf("missing historical bars dropped position: %+v", report.Positions)
	}
}

func TestAdvanceRetriesFailedExitOnCurrentCloseOnce(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-20", 10, 10, 10, 10, 1_000_000),
		shadowBar(symbol, "2026-08-21", 10, 11, 11, 11, 1_000_000),
		shadowBar(symbol, "2026-08-24", 11, 12, 12, 11, 1_000_000),
	}
	calendar := append([]domain.DailyBar(nil), bars...)
	for index := range calendar {
		calendar[index].Symbol = "sh000300"
	}
	cfg := DefaultConfig()
	amount := 10.0 * 100
	fee := transactionFee(float64(amount), "buy", cfg)
	previous := Report{
		EngineVersion: ShadowEngineVersion, Config: cfg, AsOf: "2026-08-21", CheckpointPhase: CheckpointClose,
		InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash - float64(amount) - fee,
		TotalTurnover: float64(amount), TotalFees: fee, FilledEntries: 1, RejectedOrders: 1,
		Orders:     []ShadowOrder{{ID: "signal-buy", Symbol: symbol, Side: "buy", AttemptDate: "2026-08-21", Quantity: 100, Price: 10, RawPrice: 10, Amount: 1000, Status: OrderFilled}},
		Rejections: []ShadowRejection{{OrderID: "signal-sell", Symbol: symbol, Side: "sell", AttemptDate: "2026-08-21", Reason: "卖出日一字板或停牌，无法执行"}},
		Positions:  []ShadowOpenPosition{{SignalID: "signal", Symbol: symbol, EntryDate: "2026-08-21", Quantity: 100, EntryPrice: 10, EntryAmount: 1000, EntryFee: fee, SignalClose: 10, TargetExitDate: "2026-08-21"}},
	}
	evaluator := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}})
	report, err := evaluator.Advance(context.Background(), previous, nil, Options{Config: cfg, Now: func() time.Time { return time.Date(2026, 8, 24, 16, 0, 0, 0, shanghaiLocation) }})
	if err != nil {
		t.Fatal(err)
	}
	if report.CompletedTrades != 1 || len(report.Positions) != 0 || len(report.Orders) != 2 {
		t.Fatalf("failed exit was not retried at current close: %+v", report)
	}
	second, err := evaluator.Advance(context.Background(), report, nil, Options{Config: cfg, Now: func() time.Time { return time.Date(2026, 8, 24, 16, 0, 0, 0, shanghaiLocation) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Orders) != len(report.Orders) || len(second.Trades) != len(report.Trades) {
		t.Fatalf("same close checkpoint duplicated exit: before=%d/%d after=%d/%d", len(report.Orders), len(report.Trades), len(second.Orders), len(second.Trades))
	}
}

func TestAdvanceDerivesMissingTargetExitFromTradingCalendar(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-20", 10, 10, 10.2, 9.8, 1_000_000),
		shadowBar(symbol, "2026-08-21", 10.5, 10.8, 10.9, 10.4, 1_000_000),
		shadowBar(symbol, "2026-08-24", 11, 11.2, 11.3, 10.9, 1_000_000),
	}
	calendar := append([]domain.DailyBar(nil), bars...)
	for index := range calendar {
		calendar[index].Symbol = "sh000300"
	}
	cfg := DefaultConfig()
	cfg.HoldingDays = 2
	amount := 10.5 * 100
	fee := transactionFee(amount, "buy", cfg)
	previous := Report{
		EngineVersion: ShadowEngineVersion, Config: cfg, AsOf: "2026-08-21", CheckpointPhase: CheckpointClose,
		InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash - amount - fee, FilledEntries: 1, TotalTurnover: amount, TotalFees: fee,
		Orders:    []ShadowOrder{{ID: "signal-buy", Symbol: symbol, Side: "buy", SignalDate: "2026-08-20", AttemptDate: "2026-08-21", Quantity: 100, RawPrice: 10.5, Price: 10.5, Amount: amount, Status: OrderFilled}},
		Positions: []ShadowOpenPosition{{SignalID: "signal", Symbol: symbol, SignalDate: "2026-08-20", EntryDate: "2026-08-21", Quantity: 100, EntryPrice: 10.5, EntryAmount: amount, SignalClose: 10}},
	}
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Advance(context.Background(), previous, nil, Options{Config: cfg, Now: func() time.Time { return time.Date(2026, 8, 24, 15, 6, 0, 0, shanghaiLocation) }})
	if err != nil {
		t.Fatal(err)
	}
	if report.CompletedTrades != 1 || len(report.Positions) != 0 || len(report.Orders) != 2 {
		t.Fatalf("missing target exit was not derived: %+v", report)
	}
}

func TestAdvanceInfersLegacyEntryFeeForOpenPosition(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-20", 10, 10, 10.2, 9.8, 1_000_000),
		shadowBar(symbol, "2026-08-21", 10, 11, 11.2, 10, 1_000_000),
	}
	calendar := append([]domain.DailyBar(nil), bars...)
	for index := range calendar {
		calendar[index].Symbol = "sh000300"
	}
	cfg := DefaultConfig()
	amount := 10.0 * 100
	previous := Report{
		EngineVersion: ShadowEngineVersion, Config: cfg, AsOf: "2026-08-20", InitialCash: cfg.InitialCash,
		RemainingCash: cfg.InitialCash - amount - transactionFee(amount, "buy", cfg), FilledEntries: 1, TotalTurnover: amount,
		Orders:    []ShadowOrder{{ID: "signal-buy", Symbol: symbol, Side: "buy", AttemptDate: "2026-08-20", Quantity: 100, RawPrice: 10, Price: 10, Amount: amount, Status: OrderFilled}},
		Positions: []ShadowOpenPosition{{SignalID: "signal", Symbol: symbol, EntryDate: "2026-08-20", Quantity: 100, EntryPrice: 10, EntryAmount: amount}},
	}
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Advance(context.Background(), previous, nil, Options{Config: cfg, Now: func() time.Time { return time.Date(2026, 8, 21, 10, 0, 0, 0, shanghaiLocation) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Positions) != 1 || report.Positions[0].EntryFee <= 0 || report.Positions[0].UnrealizedProfit >= 100 {
		t.Fatalf("legacy entry fee was not restored: %+v", report.Positions)
	}
}
