package paper

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

type shadowHistoryStub struct{ bars map[string][]domain.DailyBar }

func (stub shadowHistoryStub) FetchDailyBars(_ context.Context, symbol string) ([]domain.DailyBar, error) {
	return stub.bars[symbol], nil
}

type countingShadowHistoryStub struct {
	bars  map[string][]domain.DailyBar
	calls map[string]int
}

func (stub *countingShadowHistoryStub) FetchDailyBars(_ context.Context, symbol string) ([]domain.DailyBar, error) {
	stub.calls[symbol]++
	return stub.bars[symbol], nil
}

func shadowBar(symbol, date string, open, close, high, low, amount float64) domain.DailyBar {
	return domain.DailyBar{Symbol: symbol, Source: "test", Date: date, Open: open, Close: close, High: high, Low: low, Amount: amount}
}

func shadowSignal(id, symbol, date string, score float64) realtime.Signal {
	asOf, _ := time.ParseInLocation("2006-01-02 15:04", date+" 14:30", shanghaiLocation)
	return realtime.Signal{ID: id, Symbol: symbol, Name: "测试", Score: score, State: realtime.StateTriggered, AsOf: asOf}
}

func realtimeShadowSignal(id, symbol, timestamp string, score float64) realtime.Signal {
	asOf, _ := time.ParseInLocation("2006-01-02 15:04", timestamp, shanghaiLocation)
	return realtime.Signal{ID: id, Symbol: symbol, Name: "盘中测试", Score: score, State: realtime.StateTriggered, AsOf: asOf, Price: 10, Reasons: []string{"盘中趋势确认"}}
}

func shadowRiskComponents(volatilityScore, regimeScore float64) []realtime.Component {
	return []realtime.Component{
		{Key: "volatility-risk", Name: "波动风险", Score: volatilityScore, Maximum: 20, State: "观察"},
		{Key: "market-regime", Name: "市场适配", Score: regimeScore, Maximum: 20, State: "观察"},
	}
}

func TestShadowSignalEntryEligibilityUsesNewPortfolioOverlayAndLegacyFallback(t *testing.T) {
	cfg := DefaultConfig()
	legacy := shadowSignal("legacy", "sh600000", "2026-08-20", 70)
	if !shadowSignalEntryEligible(legacy, cfg) {
		t.Fatal("legacy signal should keep score/state fallback")
	}
	newSignal := legacy
	newSignal.RiskMultiplier = .85
	newSignal.RiskAdjustedScore = 59.5
	newSignal.CrossSectionTotal = 10
	newSignal.PortfolioEligible = false
	if shadowSignalEntryEligible(newSignal, cfg) {
		t.Fatal("new signal without portfolio eligibility should not open a position")
	}
	newSignal.PortfolioEligible = true
	if !shadowSignalEntryEligible(newSignal, cfg) {
		t.Fatal("eligible new signal should pass the entry gate")
	}
}

func TestMonsterShadowSignalUsesRadarGateScoreAndState(t *testing.T) {
	cfg := DefaultConfig()
	cfg.UseMonsterRadar = true
	cfg.MinimumScore = 58
	signal := shadowSignal("monster", "sh600000", "2026-08-20", 12)
	signal.State = realtime.StateInvalid
	signal.Monster = realtime.MonsterRadar{
		Score:    66,
		Stage:    realtime.MonsterStageStarting,
		Eligible: true,
	}
	if !shadowSignalEntryEligible(signal, cfg) {
		t.Fatal("eligible monster radar should pass even when the composite signal is invalid")
	}
	if got := shadowSignalScoreForConfig(signal, cfg); got != 66 {
		t.Fatalf("monster account did not use radar score: got %.1f", got)
	}
	if got := shadowSignalStateForConfig(signal, cfg); got != realtime.StateWatching {
		t.Fatalf("monster starting state with a mid score should be watching, got %s", got)
	}

	signal.Monster.Eligible = false
	if shadowSignalEntryEligible(signal, cfg) {
		t.Fatal("ineligible monster radar must not open a position")
	}
	if got := shadowSignalStateForConfig(signal, cfg); got != realtime.StateWeak {
		t.Fatalf("ineligible monster radar should be weak, got %s", got)
	}
}

func TestMonsterShadowSignalStateMapsEveryRadarStage(t *testing.T) {
	cfg := DefaultConfig()
	cfg.UseMonsterRadar = true
	cases := []struct {
		name  string
		stage realtime.MonsterStage
		score float64
		ok    bool
		want  realtime.SignalState
	}{
		{name: "dormant watching", stage: realtime.MonsterStageDormant, score: 60, ok: true, want: realtime.StateWatching},
		{name: "starting triggered", stage: realtime.MonsterStageStarting, score: 72, ok: true, want: realtime.StateTriggered},
		{name: "accelerating triggered", stage: realtime.MonsterStageAccelerating, score: 80, ok: true, want: realtime.StateTriggered},
		{name: "diverging weak", stage: realtime.MonsterStageDiverging, score: 80, ok: true, want: realtime.StateWeak},
		{name: "ebbing weak", stage: realtime.MonsterStageEbbing, score: 70, ok: true, want: realtime.StateWeak},
		{name: "insufficient invalid", stage: realtime.MonsterStageInsufficient, score: 0, ok: false, want: realtime.StateInvalid},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			signal := realtime.Signal{Monster: realtime.MonsterRadar{Stage: item.stage, Score: item.score, Eligible: item.ok}}
			if got := shadowSignalStateForConfig(signal, cfg); got != item.want {
				t.Fatalf("stage %s mapped to %s, want %s", item.stage, got, item.want)
			}
		})
	}
}

func TestMonsterShadowRealtimeRankingUsesRadarScore(t *testing.T) {
	cfg := DefaultConfig()
	cfg.UseMonsterRadar = true
	now := time.Date(2026, 8, 26, 10, 10, 0, 0, shanghaiLocation)
	low := realtimeShadowSignal("low", "sh600001", "2026-08-26 10:05", 95)
	high := realtimeShadowSignal("high", "sh600002", "2026-08-26 10:05", 10)
	low.Monster = realtime.MonsterRadar{Score: 61, Stage: realtime.MonsterStageStarting, Eligible: true}
	high.Monster = realtime.MonsterRadar{Score: 85, Stage: realtime.MonsterStageAccelerating, Eligible: true}
	items := latestRealtimeSignals([]realtime.Signal{low, high}, "2026-08-26", now, cfg)
	if len(items) != 2 || items[0].Symbol != high.Symbol {
		t.Fatalf("monster ranking ignored radar score: %+v", items)
	}
	if got := shadowSignalScoreForConfig(items[0], cfg); got != 85 {
		t.Fatalf("ranked signal score mismatch: %.1f", got)
	}
}

func TestMonsterShadowRepresentativeSignalPrefersRadarScoreOnTimestampTie(t *testing.T) {
	cfg := DefaultConfig()
	cfg.UseMonsterRadar = true
	at := time.Date(2026, 8, 26, 14, 30, 0, 0, shanghaiLocation)
	compositeWinner := realtime.Signal{
		ID: "composite", Symbol: "sh600003", Score: 95, AsOf: at,
		Monster: realtime.MonsterRadar{Score: 60, Stage: realtime.MonsterStageStarting, Eligible: true},
	}
	radarWinner := compositeWinner
	radarWinner.ID = "radar"
	radarWinner.Score = 20
	radarWinner.Monster.Score = 86
	selected := representativeSignalsForConfig([]realtime.Signal{compositeWinner, radarWinner}, 0, cfg)
	if len(selected) != 1 || selected[0].ID != "radar" {
		t.Fatalf("timestamp tie did not use monster score: %+v", selected)
	}
}

func TestRealtimeShadowFillsAtQuoteTimeAndIsIdempotent(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-22", 9.8, 10, 10.2, 9.6, 2_000_000),
		shadowBar(symbol, "2026-08-25", 10, 10.2, 10.4, 9.8, 2_000_000),
	}
	calendar := []domain.DailyBar{
		shadowBar("sh000300", "2026-08-22", 1, 1, 1, 1, 1),
		shadowBar("sh000300", "2026-08-25", 1, 1, 1, 1, 1),
		shadowBar("sh000300", "2026-08-26", 1, 1, 1, 1, 1),
	}
	signal := realtimeShadowSignal("rt-entry", symbol, "2026-08-26 10:15", 80)
	quote := PositionQuote{Symbol: symbol, Price: 10.25, QuoteTime: "2026-08-26 10:15:00", Source: "test"}
	now := func() time.Time { return time.Date(2026, 8, 26, 10, 16, 0, 0, shanghaiLocation) }
	evaluator := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}})
	options := Options{Config: DefaultConfig(), Realtime: true, RealtimeAt: now(), RealtimeQuotes: []PositionQuote{quote}, CalendarDates: []string{"2026-08-22", "2026-08-25", "2026-08-26"}, Now: now}
	report, err := evaluator.Evaluate(context.Background(), []realtime.Signal{signal}, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Orders) != 1 || report.Orders[0].Status != OrderFilled {
		t.Fatalf("盘中信号没有成交: %+v", report)
	}
	if report.Orders[0].ExecutionTime != quote.QuoteTime || report.Orders[0].PositionAction != "open" || report.ExecutionMode != ExecutionModeLive {
		t.Fatalf("盘中成交时间或模式错误: %+v", report.Orders[0])
	}
	second, err := evaluator.Advance(context.Background(), report, []realtime.Signal{signal}, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Orders) != len(report.Orders) || len(second.Trades) != len(report.Trades) {
		t.Fatalf("重复同一报价产生重复事件: before=%d/%d after=%d/%d", len(report.Orders), len(report.Trades), len(second.Orders), len(second.Trades))
	}
}

func TestMonsterRealtimeEntryWaitsThroughOpeningObservationWindow(t *testing.T) {
	symbol := "sh600000"
	cfg := DefaultConfig()
	cfg.UseMonsterRadar = true
	cfg.MinimumScore = 58
	bars := shadowStrategyBars(symbol)
	calendar := []string{"2026-08-25", "2026-08-26", "2026-08-27", "2026-08-28"}
	stub := shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars}}
	evaluator := NewEvaluator(stub)
	makeSignal := func(at string) realtime.Signal {
		return realtime.Signal{
			ID: symbol + "-" + strings.ReplaceAll(at, ":", ""), Symbol: symbol, Name: "抓妖测试",
			AsOf: func() time.Time {
				value, _ := time.ParseInLocation("2006-01-02 15:04", at, shanghaiLocation)
				return value
			}(),
			Price: 10.8, State: realtime.StateInvalid,
			Monster: realtime.MonsterRadar{Score: 76, Stage: realtime.MonsterStageStarting, Eligible: true},
		}
	}
	makeQuote := func(at string) PositionQuote {
		return PositionQuote{Symbol: symbol, Price: 10.8, QuoteTime: at + ":00", Source: "test"}
	}
	early := time.Date(2026, 8, 26, 9, 31, 0, 0, shanghaiLocation)
	first, err := evaluator.AdvanceRealtime(context.Background(), Report{
		EngineVersion: ShadowEngineVersion, Config: cfg, ConfigFingerprint: OptionsFingerprint(cfg, 0),
		AsOf: "2026-08-26", CheckpointPhase: CheckpointOpen, InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash,
	}, []realtime.Signal{makeSignal("2026-08-26 09:31")}, Options{
		Config: cfg, Realtime: true, RealtimeAt: early, RealtimeQuotes: []PositionQuote{makeQuote("2026-08-26 09:31")}, CalendarDates: calendar,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Orders) != 0 || !hasShadowDecision(first.Decisions, "wait") {
		t.Fatalf("opening observation should defer the first entry: orders=%+v decisions=%+v", first.Orders, first.Decisions)
	}
	foundWindowReason := false
	for _, decision := range first.Decisions {
		if strings.Contains(decision.Reason, "开盘观察期") {
			foundWindowReason = true
			break
		}
	}
	if !foundWindowReason {
		t.Fatalf("opening observation reason missing: %+v", first.Decisions)
	}

	late := time.Date(2026, 8, 26, 9, 41, 0, 0, shanghaiLocation)
	second, err := evaluator.AdvanceRealtime(context.Background(), first, []realtime.Signal{makeSignal("2026-08-26 09:41")}, Options{
		Config: cfg, Realtime: true, RealtimeAt: late, RealtimeQuotes: []PositionQuote{makeQuote("2026-08-26 09:41")}, CalendarDates: calendar,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Orders) != 1 || second.Orders[0].PositionAction != "open" || len(second.Positions) != 1 {
		t.Fatalf("entry was not allowed after opening observation: orders=%+v positions=%+v", second.Orders, second.Positions)
	}
}

func TestBaselineRealtimeEntryIsNotDelayedByMonsterOpeningPolicy(t *testing.T) {
	if !realtimeEntryWindowOpen(time.Date(2026, 8, 26, 9, 31, 0, 0, shanghaiLocation), DefaultConfig()) {
		t.Fatal("baseline accounts must retain their existing opening behavior")
	}
}

func TestAdvanceRealtimeUsesSnapshotWithoutReplayingPositionHistory(t *testing.T) {
	symbol := "sh600000"
	calendar := []string{"2026-08-25", "2026-08-26"}
	stub := &countingShadowHistoryStub{
		bars: map[string][]domain.DailyBar{
			symbol: {
				shadowBar(symbol, "2026-08-25", 9.8, 10, 10.2, 9.6, 2_000_000),
			},
		},
		calls: make(map[string]int),
	}
	cfg := DefaultConfig()
	previous := Report{
		EngineVersion:     ShadowEngineVersion,
		Config:            cfg,
		ConfigFingerprint: OptionsFingerprint(cfg, 0),
		AsOf:              "2026-08-26",
		CheckpointPhase:   CheckpointOpen,
		ExecutionMode:     ExecutionModeLive,
		LastRealtimeAt:    "2026-08-26 10:00:00",
		InitialCash:       cfg.InitialCash,
		RemainingCash:     cfg.InitialCash,
		SignalCount:       1234,
	}
	now := time.Date(2026, 8, 26, 10, 1, 0, 0, shanghaiLocation)
	signal := realtimeShadowSignal("next", symbol, "2026-08-26 10:01", 80)
	report, err := NewEvaluator(stub).AdvanceRealtime(context.Background(), previous, []realtime.Signal{signal}, Options{
		Config: cfg, Realtime: true, RealtimeAt: now,
		RealtimeQuotes: []PositionQuote{{Symbol: symbol, Price: 10, QuoteTime: "2026-08-26 10:01:00", Source: "test"}},
		CalendarDates:  calendar,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Orders) != 1 || report.Orders[0].ExecutionTime != "2026-08-26 10:01:00" {
		t.Fatalf("snapshot did not create realtime event: %+v", report.Orders)
	}
	if stub.calls[symbol] != 1 || stub.calls["sh000300"] != 0 {
		t.Fatalf("realtime path replayed unexpected history: %+v", stub.calls)
	}
	if report.SignalCount != previous.SignalCount {
		t.Fatalf("realtime snapshot overwrote cumulative signal count: %d", report.SignalCount)
	}
}

func TestRealtimeEventsCountUniqueDecisionsFillsAndRejections(t *testing.T) {
	report := Report{
		Orders: []ShadowOrder{
			{EventID: "rt-1", EventSource: "realtime-signal", Symbol: "sh600000", Side: "buy", ExecutionTime: "2026-08-26 10:15:00"},
			{EventID: "daily-1", EventSource: "daily", Symbol: "sh600001", Side: "buy", ExecutionTime: "2026-08-26 09:30:00"},
		},
		Decisions: []ShadowDecision{
			{EventID: "rt-1", EventSource: "realtime", Symbol: "sh600000", Action: "open", EventTime: "2026-08-26 10:15:00"},
			{EventID: "rt-2", EventSource: "realtime", Symbol: "sh600002", Action: "hold", EventTime: "2026-08-26 10:20:00"},
		},
		Rejections: []ShadowRejection{
			{EventID: "rt-3", EventSource: "realtime", Symbol: "sh600003", Side: "buy", EventTime: "2026-08-26 10:25:00"},
		},
	}
	if got := countRealtimeEvents(report); got != 3 {
		t.Fatalf("expected three unique realtime events, got %d", got)
	}
}

func TestRealtimeShadowHonorsTPlusOneForSameDayPosition(t *testing.T) {
	symbol := "sh600000"
	calendar := []string{"2026-08-25", "2026-08-26"}
	position := ShadowOpenPosition{
		SignalID: "old", Symbol: symbol, Name: "盘中测试", EntryDate: "2026-08-26", EntryTime: "2026-08-26 09:45:00",
		Quantity: 100, EntryPrice: 10, EntryAmount: 1000, EntryFee: 5, SignalDate: "2026-08-26", SignalScore: 75,
		Lots: []ShadowPositionLot{{OrderID: "old-buy", EntryDate: "2026-08-26", EntryTime: "2026-08-26 09:45:00", Quantity: 100, EntryPrice: 10, EntryAmount: 1000, EntryFee: 5, SignalID: "old", SignalDate: "2026-08-26"}},
	}
	cfg := DefaultConfig()
	previous := Report{EngineVersion: ShadowEngineVersion, Config: cfg, ConfigFingerprint: OptionsFingerprint(cfg, 0), AsOf: "2026-08-26", CheckpointPhase: CheckpointOpen, InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash - 1005, Positions: []ShadowOpenPosition{position}, Orders: []ShadowOrder{{ID: "old-buy", Symbol: symbol, Side: "buy", AttemptDate: "2026-08-26", Quantity: 100, RawPrice: 10, Price: 10, Amount: 1000, Status: OrderFilled, ExecutionTime: "2026-08-26 09:45:00", EventID: "old-event"}}}
	weak := realtimeShadowSignal("weak", symbol, "2026-08-26 10:20", 40)
	weak.State = realtime.StateWeak
	now := func() time.Time { return time.Date(2026, 8, 26, 10, 21, 0, 0, shanghaiLocation) }
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{}}).Advance(context.Background(), previous, []realtime.Signal{weak}, Options{Config: cfg, Realtime: true, RealtimeAt: now(), RealtimeQuotes: []PositionQuote{{Symbol: symbol, Price: 9.9, QuoteTime: "2026-08-26 10:20:00", Source: "test"}}, CalendarDates: calendar, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Orders) != 1 || report.Positions[0].Quantity != 100 {
		t.Fatalf("当日新买仓位违反 T+1 被卖出: orders=%+v positions=%+v", report.Orders, report.Positions)
	}
	if len(report.Decisions) == 0 || !strings.Contains(report.Decisions[len(report.Decisions)-1].Reason, "没有可卖批次") {
		t.Fatalf("没有记录 T+1 拒绝依据: %+v", report.Decisions)
	}
}

func TestRealtimeIntradayTRoundTripUsesSellableLotAndPreservesTPlusOne(t *testing.T) {
	symbol := "sh600000"
	date := "2026-08-26"
	cfg := DefaultConfig()
	cfg.TCooldownMinutes = 15
	entryFee := transactionFee(10000, "buy", cfg)
	position := ShadowOpenPosition{
		SignalID: "base", Symbol: symbol, Name: "做T测试", EntryDate: "2026-08-25",
		EntryTime: "2026-08-25 10:00:00", Quantity: 1000, EntryPrice: 10,
		EntryAmount: 10000, EntryFee: entryFee,
		Lots: []ShadowPositionLot{{
			OrderID: "base-buy", EntryDate: "2026-08-25", EntryTime: "2026-08-25 10:00:00",
			Quantity: 1000, EntryPrice: 10, EntryAmount: 10000, EntryFee: entryFee, SignalID: "base",
		}},
	}
	report := Report{Config: cfg, InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash, Positions: []ShadowOpenPosition{position}}
	signal := realtimeShadowSignal("t-signal", symbol, date+" 10:30", 80)
	sellQuote := PositionQuote{Symbol: symbol, Price: 11, AveragePrice: 10.5, QuoteTime: date + " 10:30:00", Source: "test"}
	if !realtimeTReduceEligible(position, sellQuote, cfg, report, date) {
		t.Fatal("fee-positive sellable base lot should be eligible for T high-sell")
	}
	if got := realtimeTReduceQuantity(position, date, cfg); got != 200 {
		t.Fatalf("unexpected T tranche quantity: %d", got)
	}
	if filled := executeRealtimePartialSell(&report, &position, 200, signal, sellQuote, "rt-t-reduce", nil, date); filled != 200 {
		t.Fatalf("T high-sell did not fill 200 shares: %d", filled)
	}
	if position.Quantity != 800 {
		t.Fatalf("high-sell should leave the core/base quantity before rebuy: %d", position.Quantity)
	}
	if _, pending := realtimePendingT(report, symbol, date); pending != 200 {
		t.Fatalf("pending T quantity was not tracked: %d", pending)
	}
	if realtimeTCooldownElapsed(report, symbol, date, date+" 10:40:00", cfg.TCooldownMinutes) {
		t.Fatal("T cooldown was ignored")
	}
	rebuyQuote := PositionQuote{Symbol: symbol, Price: 10.2, AveragePrice: 10.5, QuoteTime: date + " 10:50:00", Source: "test"}
	sale, pending := realtimePendingT(report, symbol, date)
	if !realtimeTRebuyEligible(sale, pending, rebuyQuote, cfg) {
		t.Fatal("price gap below VWAP should allow a fee-positive T rebuy")
	}
	if !executeRealtimeTRebuy(&report, &position, signal, rebuyQuote, sale, pending, "rt-t-rebuy", nil, date) {
		t.Fatal("T rebuy did not fill")
	}
	if position.Quantity != 1000 || position.AvailableQuantity != 800 {
		t.Fatalf("rebuy should restore quantity while keeping the new batch T+1 locked: quantity=%d available=%d", position.Quantity, position.AvailableQuantity)
	}
	if len(position.Lots) != 2 || position.Lots[1].EntryDate != date {
		t.Fatalf("rebuy lot was not appended as a new T+1 batch: %+v", position.Lots)
	}
	review := buildShadowDailyReview(Report{AsOf: date, Config: cfg, Orders: report.Orders, Trades: report.Trades, Positions: []ShadowOpenPosition{position}})
	if review.PendingTQuantity != 0 || review.TReduceCount != 0 || review.TRebuyCount != 0 {
		// Direct execution helpers append orders/trades but decisions are owned by
		// the realtime orchestrator; this assertion is only for pending state.
		t.Fatalf("completed T round left pending quantity: %+v", review)
	}
}

func TestRealtimeSignalCooldownDebouncesOnlyOrdinaryRebalances(t *testing.T) {
	report := Report{Orders: []ShadowOrder{
		{Symbol: "sh600000", AttemptDate: "2026-08-26", Status: OrderFilled, PositionAction: "add", ExecutionTime: "2026-08-26 10:00:00"},
		{Symbol: "sh600000", AttemptDate: "2026-08-26", Status: OrderFilled, PositionAction: "t_reduce", ExecutionTime: "2026-08-26 10:05:00"},
	}}
	if realtimeSignalCooldownElapsed(report, "sh600000", "2026-08-26", "2026-08-26 10:15:00", 20) {
		t.Fatal("ordinary add should still be inside the cooldown window")
	}
	if !realtimeSignalCooldownElapsed(report, "sh600000", "2026-08-26", "2026-08-26 10:21:00", 20) {
		t.Fatal("ordinary add cooldown should expire based on the latest ordinary order")
	}
	if !realtimeSignalCooldownElapsed(Report{Orders: []ShadowOrder{{Symbol: "sh600000", AttemptDate: "2026-08-26", Status: OrderFilled, PositionAction: "t_reduce", ExecutionTime: "2026-08-26 10:10:00"}}}, "sh600000", "2026-08-26", "2026-08-26 10:15:00", 20) {
		t.Fatal("T-only orders must not block ordinary signal rebalancing")
	}
}

func TestShadowDailyReviewGroupsBlockersAndCountsPendingT(t *testing.T) {
	cfg := DefaultConfig()
	date := "2026-08-26"
	report := Report{
		AsOf: date, Config: cfg,
		Positions: []ShadowOpenPosition{{Symbol: "sh600000", Quantity: 800}},
		Orders:    []ShadowOrder{{Symbol: "sh600000", AttemptDate: date, Status: OrderFilled, PositionAction: "t_reduce", Quantity: 200, Price: 11, Amount: 2200, ExecutionTime: date + " 10:30:00"}},
		Decisions: []ShadowDecision{{Date: date, Action: "hold", Reason: "盘中信号尚未显著增强，维持现有仓位"}},
	}
	review := buildShadowDailyReview(report)
	if review == nil || review.PendingTQuantity != 200 {
		t.Fatalf("pending T quantity missing from review: %+v", review)
	}
	if len(review.TopBlockers) != 1 || review.TopBlockers[0].Reason != "信号未显著增强" {
		t.Fatalf("blocker reason was not normalized: %+v", review.TopBlockers)
	}
}

func TestTargetPositionPercentAppliesMarketRiskOverlay(t *testing.T) {
	cfg := DefaultConfig()
	base := shadowSignal("base", "sh600000", "2026-08-20", 80)
	base.Components = shadowRiskComponents(20, 20)
	bull := base
	bull.RiskMultiplier = 1
	bull.RiskAdjustedScore = 80
	bear := base
	bear.RiskMultiplier = .60
	bear.RiskAdjustedScore = 48
	bullTarget := targetPositionPercent(bull, cfg)
	bearTarget := targetPositionPercent(bear, cfg)
	if !(bearTarget < bullTarget) || bearTarget <= 0 {
		t.Fatalf("risk overlay did not reduce target position: bull=%.2f bear=%.2f", bullTarget, bearTarget)
	}
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

func TestTrailingAverageAmountUsesConservativeVolumeFallback(t *testing.T) {
	bars := []domain.DailyBar{
		{Open: 9, Close: 10, High: 11, Low: 8, Volume: 1_000},
		{Open: 10, Close: 11, High: 12, Low: 9, Volume: 2_000},
	}
	got := trailingAverageAmount(bars, 1, 2)
	want := ((9.0+10+11+8)/4*1_000 + (10.0+11+12+9)/4*2_000) / 2
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("unexpected conservative amount fallback: got %.2f want %.2f", got, want)
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

func TestShadowDynamicPositionSizingReducesRiskySignal(t *testing.T) {
	safeSymbol, riskySymbol := "sh600000", "sh600001"
	barsBySymbol := map[string][]domain.DailyBar{}
	for _, symbol := range []string{safeSymbol, riskySymbol, "sh000300"} {
		barsBySymbol[symbol] = []domain.DailyBar{
			shadowBar(symbol, "2026-08-19", 10, 10, 10.2, 9.8, 100_000_000),
			shadowBar(symbol, "2026-08-20", 10, 10, 10.2, 9.8, 100_000_000),
			shadowBar(symbol, "2026-08-21", 10, 10, 10.2, 9.8, 100_000_000),
		}
	}
	safe := shadowSignal("safe", safeSymbol, "2026-08-20", 75)
	safe.Industry, safe.InvalidationPrice, safe.Components = "银行", 9, shadowRiskComponents(20, 20)
	risky := shadowSignal("risky", riskySymbol, "2026-08-20", 75)
	risky.Industry, risky.InvalidationPrice, risky.Components = "软件", 9, shadowRiskComponents(0, 10)
	report, err := NewEvaluator(shadowHistoryStub{bars: barsBySymbol}).Evaluate(context.Background(), []realtime.Signal{safe, risky}, Options{Now: func() time.Time {
		return time.Date(2026, 8, 21, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	orders := make(map[string]ShadowOrder)
	positions := make(map[string]ShadowOpenPosition)
	for _, order := range report.Orders {
		orders[order.Symbol] = order
	}
	for _, position := range report.Positions {
		positions[position.Symbol] = position
	}
	if orders[safeSymbol].Amount <= orders[riskySymbol].Amount || positions[safeSymbol].TargetPositionPercent <= positions[riskySymbol].TargetPositionPercent {
		t.Fatalf("riskier signal was not sized down: safe=%+v risky=%+v", positions[safeSymbol], positions[riskySymbol])
	}
	if positions[safeSymbol].TargetPositionPercent > DefaultConfig().MaxPositionPercent {
		t.Fatalf("dynamic target exceeded hard cap: %+v", positions[safeSymbol])
	}
}

func TestShadowIndustryExposureCapsNewEntries(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxIndustryPercent = 25
	cfg.MaxDailyDeploymentPercent = 100
	symbols := []string{"sh600000", "sh600001", "sh600002", "sh600003"}
	barsBySymbol := make(map[string][]domain.DailyBar, len(symbols)+1)
	signals := make([]realtime.Signal, 0, len(symbols))
	for index, symbol := range symbols {
		barsBySymbol[symbol] = []domain.DailyBar{
			shadowBar(symbol, "2026-08-19", 10, 10, 10.2, 9.8, 100_000_000),
			shadowBar(symbol, "2026-08-20", 10, 10, 10.2, 9.8, 100_000_000),
			shadowBar(symbol, "2026-08-21", 10, 10, 10.2, 9.8, 100_000_000),
		}
		signal := shadowSignal("industry-"+symbol, symbol, "2026-08-20", 80-float64(index))
		signal.Industry = "通信设备Ⅱ"
		signal.InvalidationPrice = 9
		signal.Components = shadowRiskComponents(20, 20)
		signals = append(signals, signal)
	}
	barsBySymbol["sh000300"] = append([]domain.DailyBar(nil), barsBySymbol[symbols[0]]...)
	for index := range barsBySymbol["sh000300"] {
		barsBySymbol["sh000300"][index].Symbol = "sh000300"
	}
	report, err := NewEvaluator(shadowHistoryStub{bars: barsBySymbol}).Evaluate(context.Background(), signals, Options{Config: cfg, Now: func() time.Time {
		return time.Date(2026, 8, 21, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.IndustryExposures) != 1 || report.IndustryExposures[0].Industry != "通信设备" || report.IndustryExposures[0].ExposurePercent > cfg.MaxIndustryPercent+1e-6 {
		t.Fatalf("industry cap was not enforced: %+v", report.IndustryExposures)
	}
	if report.FilledEntries != len(signals) || len(report.Orders) != len(signals) {
		t.Fatalf("industry room should allow a final partial entry: %+v", report.Orders)
	}
	if report.Orders[len(report.Orders)-1].Amount >= report.Orders[0].Amount {
		t.Fatalf("final same-industry order was not reduced to remaining room: first=%+v last=%+v", report.Orders[0], report.Orders[len(report.Orders)-1])
	}
}

func TestShadowRejectsEntryWhenNextOpenAlreadyInvalidated(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-19", 10, 10, 10.2, 9.8, 100_000_000),
		shadowBar(symbol, "2026-08-20", 10.5, 10.5, 10.7, 10.2, 100_000_000),
		shadowBar(symbol, "2026-08-21", 10, 10.1, 10.2, 9.9, 100_000_000),
	}
	calendar := append([]domain.DailyBar(nil), bars...)
	for index := range calendar {
		calendar[index].Symbol = "sh000300"
	}
	signal := shadowSignal("invalidated", symbol, "2026-08-20", 75)
	signal.InvalidationPrice = 10.2
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Evaluate(context.Background(), []realtime.Signal{signal}, Options{Now: func() time.Time {
		return time.Date(2026, 8, 21, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.FilledEntries != 0 || len(report.Rejections) != 1 || !strings.Contains(report.Rejections[0].Reason, "跌破信号失效位") {
		t.Fatalf("invalidated signal was still bought: %+v", report)
	}
}

func TestEvaluatorReplaysRiskExitFromCompletedDailyBars(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-19", 10, 10, 10.2, 9.8, 100_000_000),
		shadowBar(symbol, "2026-08-20", 10, 10, 10.2, 9.8, 100_000_000),
		shadowBar(symbol, "2026-08-21", 10, 9.4, 10.1, 9.3, 100_000_000),
		shadowBar(symbol, "2026-08-24", 9.2, 9.3, 9.4, 9.1, 100_000_000),
	}
	calendar := append([]domain.DailyBar(nil), bars...)
	for index := range calendar {
		calendar[index].Symbol = "sh000300"
	}
	cfg := DefaultConfig()
	cfg.HoldingDays = 20
	signal := shadowSignal("risk-replay", symbol, "2026-08-20", 75)
	signal.Industry = "银行"
	signal.InvalidationPrice = 9.5
	signal.Components = shadowRiskComponents(20, 20)
	report, err := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}}).Evaluate(context.Background(), []realtime.Signal{signal}, Options{Config: cfg, Now: func() time.Time {
		return time.Date(2026, 8, 24, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.CompletedTrades != 1 || len(report.Positions) != 0 || len(report.Orders) != 2 || report.Orders[1].AttemptDate != "2026-08-24" || report.Orders[1].PositionAction != "risk_exit" {
		t.Fatalf("full replay missed the historical risk exit: %+v", report)
	}
}

func TestAdvanceRiskLayerExitsAtNextOpenAndStartsCooldown(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-20", 10, 10, 10.2, 9.8, 100_000_000),
		shadowBar(symbol, "2026-08-21", 10, 10, 10.2, 9.8, 100_000_000),
		shadowBar(symbol, "2026-08-24", 9.7, 9.4, 9.8, 9.3, 100_000_000),
		shadowBar(symbol, "2026-08-25", 9.2, 9.3, 9.4, 9.1, 100_000_000),
		shadowBar(symbol, "2026-08-26", 9.4, 9.5, 9.6, 9.3, 100_000_000),
	}
	calendar := append([]domain.DailyBar(nil), bars...)
	for index := range calendar {
		calendar[index].Symbol = "sh000300"
	}
	cfg := DefaultConfig()
	cfg.HoldingDays = 20
	amount := 10.0 * 1_000
	fee := transactionFee(amount, "buy", cfg)
	previous := Report{
		EngineVersion: "tplus1-v7", Config: cfg, AsOf: "2026-08-24", CheckpointPhase: CheckpointClose,
		InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash - amount - fee, FilledEntries: 1, TotalTurnover: amount, TotalFees: fee,
		Orders:    []ShadowOrder{{ID: "risk-buy", Symbol: symbol, Name: "测试", Industry: "银行", Side: "buy", SignalDate: "2026-08-20", AttemptDate: "2026-08-21", Quantity: 1_000, RawPrice: 10, Price: 10, Amount: amount, Status: OrderFilled, InvalidationPrice: 9.5}},
		Positions: []ShadowOpenPosition{{SignalID: "risk", Symbol: symbol, Name: "测试", Industry: "银行", SignalDate: "2026-08-20", EntryDate: "2026-08-21", Quantity: 1_000, EntryPrice: 10, EntryAmount: amount, EntryFee: fee, SignalClose: 10, SignalScore: 70, InvalidationPrice: 9.5}},
	}
	evaluator := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}})
	report, err := evaluator.Advance(context.Background(), previous, nil, Options{Config: cfg, Now: func() time.Time {
		return time.Date(2026, 8, 25, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Positions) != 0 || report.CompletedTrades != 1 || len(report.Orders) != 2 || report.Orders[1].AttemptDate != "2026-08-25" || report.Orders[1].PositionAction != "risk_exit" || !strings.Contains(report.Orders[1].Reason, "跌破失效位") {
		t.Fatalf("risk layer did not exit at the next open: %+v", report)
	}
	if err := ValidateTransition(previous, report); err != nil {
		t.Fatalf("risk exit failed continuity validation: %v", err)
	}
	reentry := shadowSignal("reentry", symbol, "2026-08-25", 80)
	reentry.Industry = "银行"
	reentry.InvalidationPrice = 8.5
	reentry.Components = shadowRiskComponents(20, 20)
	plan, reason, pending := makePlan(reentry, bars, barDates(calendar), cfg.HoldingDays)
	if reason != "" || pending {
		t.Fatalf("reentry plan unavailable: reason=%q pending=%v", reason, pending)
	}
	plan.exitDate = ""
	plan.exitIndex = -1
	cooled := simulateFrom(report, []shadowPlan{plan}, cfg, report.RemainingCash, map[string]shadowPosition{})
	if len(cooled.Orders) != len(report.Orders) || !hasShadowDecision(cooled.Decisions, "wait") {
		t.Fatalf("risk cooldown did not block immediate reentry: %+v", cooled)
	}
}

func TestAdvancePersistsBlockedRiskExitUntilNextTradableOpen(t *testing.T) {
	symbol := "sh600000"
	bars := []domain.DailyBar{
		shadowBar(symbol, "2026-08-20", 10, 10, 10.2, 9.8, 100_000_000),
		shadowBar(symbol, "2026-08-21", 10, 10, 10.2, 9.8, 100_000_000),
		shadowBar(symbol, "2026-08-24", 9.7, 9.4, 9.8, 9.3, 100_000_000),
		shadowBar(symbol, "2026-08-25", 9.0, 9.0, 9.0, 9.0, 100_000_000),
		shadowBar(symbol, "2026-08-26", 8.8, 8.9, 9.0, 8.7, 100_000_000),
	}
	calendar := append([]domain.DailyBar(nil), bars...)
	for index := range calendar {
		calendar[index].Symbol = "sh000300"
	}
	cfg := DefaultConfig()
	cfg.HoldingDays = 20
	amount := 10.0 * 1_000
	fee := transactionFee(amount, "buy", cfg)
	previous := Report{
		EngineVersion: "tplus1-v7", Config: cfg, AsOf: "2026-08-24", CheckpointPhase: CheckpointClose,
		InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash - amount - fee, FilledEntries: 1, TotalTurnover: amount, TotalFees: fee,
		Orders:    []ShadowOrder{{ID: "blocked-buy", Symbol: symbol, Industry: "银行", Side: "buy", SignalDate: "2026-08-20", AttemptDate: "2026-08-21", Quantity: 1_000, RawPrice: 10, Price: 10, Amount: amount, Status: OrderFilled, InvalidationPrice: 9.5}},
		Positions: []ShadowOpenPosition{{SignalID: "blocked", Symbol: symbol, Industry: "银行", SignalDate: "2026-08-20", EntryDate: "2026-08-21", Quantity: 1_000, EntryPrice: 10, EntryAmount: amount, EntryFee: fee, SignalClose: 10, SignalScore: 70, InvalidationPrice: 9.5}},
	}
	evaluator := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": calendar}})
	blocked, err := evaluator.Advance(context.Background(), previous, nil, Options{Config: cfg, Now: func() time.Time {
		return time.Date(2026, 8, 25, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked.Positions) != 1 || !blocked.Positions[0].RiskExitPending || blocked.Positions[0].RiskExitReason == "" || len(blocked.Orders) != 1 {
		t.Fatalf("blocked risk exit was not persisted: %+v", blocked)
	}
	exited, err := evaluator.Advance(context.Background(), blocked, nil, Options{Config: cfg, Now: func() time.Time {
		return time.Date(2026, 8, 26, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(exited.Positions) != 0 || exited.CompletedTrades != 1 || len(exited.Orders) != 2 || exited.Orders[1].AttemptDate != "2026-08-26" || exited.Orders[1].PositionAction != "risk_exit" {
		t.Fatalf("pending risk exit did not retry at the next tradable open: %+v", exited)
	}
}

func TestRiskExitSettlesAtOpenForSameDayContinuity(t *testing.T) {
	cfg := DefaultConfig()
	buyFee := transactionFee(10_000, "buy", cfg)
	sellFee := transactionFee(9_000, "sell", cfg)
	previous := Report{
		EngineVersion: ShadowEngineVersion, Config: cfg, AsOf: "2026-08-25", CheckpointPhase: CheckpointOpen,
		InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash - 10_000 - buyFee + 9_000 - sellFee, TotalTurnover: 19_000, TotalFees: buyFee + sellFee,
		FilledEntries: 1, CompletedTrades: 1,
		Orders: []ShadowOrder{
			{ID: "buy", Symbol: "sh600000", Side: "buy", AttemptDate: "2026-08-21", Quantity: 1_000, RawPrice: 10, Price: 10, Amount: 10_000, Status: OrderFilled},
			{ID: "risk-sell", Symbol: "sh600000", Side: "sell", AttemptDate: "2026-08-25", Quantity: 1_000, RawPrice: 9, Price: 9, Amount: 9_000, Status: OrderFilled, PositionAction: "risk_exit", ExecutionTime: "2026-08-25 09:30:00"},
		},
		Trades: []ShadowTrade{{ID: "ST0001", EntryOrderID: "buy", ExitOrderID: "risk-sell", Symbol: "sh600000", EntryDate: "2026-08-21", ExitDate: "2026-08-25", Quantity: 1_000, EntryPrice: 10, ExitPrice: 9, NetProfit: 9_000 - sellFee - 10_000 - buyFee, TotalFee: buyFee + sellFee, PositionAction: "risk_exit", ExitTime: "2026-08-25 09:30:00"}},
	}
	next := cloneReport(previous)
	next.CheckpointPhase = CheckpointClose
	if err := ValidateTransition(previous, next); err != nil {
		t.Fatalf("open risk exit was not treated as settled: %v", err)
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

func TestShadowRotationReplacesWeakestEligiblePosition(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HoldingDays = 20
	cfg.MaxOpenPositions = 2
	cfg.MaxDailyRotations = 1
	cfg.RotationScoreGap = 8
	cfg.RotationMinimumHoldDays = 2
	date := "2026-08-25"
	weak := rotationTestPosition("sh600000", "弱持仓", 60, cfg)
	strong := rotationTestPosition("sh600001", "强持仓", 75, cfg)
	incoming := shadowSignal("incoming", "sh600002", "2026-08-24", 85)
	incoming.Name, incoming.Industry = "新候选", "软件"
	plan := shadowPlan{signal: incoming, bars: shadowStrategyBars(incoming.Symbol), capacity: 100_000_000, entryDate: date, entryIndex: 4, signalClose: 10.3}
	report, active := rotationTestAccount(cfg, date, weak, strong)

	result := simulateFrom(report, []shadowPlan{plan}, cfg, report.RemainingCash, active)
	if result.CompletedTrades != 1 || result.FilledEntries != 3 || len(result.Positions) != 2 {
		t.Fatalf("finite rotation did not preserve the target portfolio size: %+v", result)
	}
	if shadowReportHasPosition(result, "sh600000") || !shadowReportHasPosition(result, "sh600001") || !shadowReportHasPosition(result, "sh600002") {
		t.Fatalf("rotation did not replace the weakest position: %+v", result.Positions)
	}
	if !hasShadowDecision(result.Decisions, "rotate_out") || !hasShadowDecision(result.Decisions, "rotate_in") {
		t.Fatalf("rotation decisions were not audited: %+v", result.Decisions)
	}
	if result.Trades[0].PositionAction != "rotate_out" || result.Trades[0].ExitTime != "2026-08-25 09:30:00" {
		t.Fatalf("rotation did not settle at the opening checkpoint: %+v", result.Trades[0])
	}
	if result.Orders[len(result.Orders)-1].PositionAction != "rotate_in" {
		t.Fatalf("replacement buy was not marked as rotate_in: %+v", result.Orders)
	}
}

func TestShadowRotationRequiresScoreGapAndMinimumHold(t *testing.T) {
	tests := []struct {
		name        string
		incoming    float64
		minimumHold int
		reason      string
	}{
		{name: "score gap", incoming: 66, minimumHold: 2, reason: "未领先最弱可轮出持仓"},
		{name: "minimum hold", incoming: 85, minimumHold: 3, reason: "至少 3 个交易日"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.HoldingDays = 20
			cfg.MaxOpenPositions = 2
			cfg.MaxDailyRotations = 1
			cfg.RotationScoreGap = 8
			cfg.RotationMinimumHoldDays = test.minimumHold
			date := "2026-08-25"
			weak := rotationTestPosition("sh600000", "弱持仓", 60, cfg)
			strong := rotationTestPosition("sh600001", "强持仓", 75, cfg)
			incoming := shadowSignal("incoming-"+test.name, "sh600002", "2026-08-24", test.incoming)
			incoming.Name = "新候选"
			plan := shadowPlan{signal: incoming, bars: shadowStrategyBars(incoming.Symbol), capacity: 100_000_000, entryDate: date, entryIndex: 4, signalClose: 10.3}
			report, active := rotationTestAccount(cfg, date, weak, strong)

			result := simulateFrom(report, []shadowPlan{plan}, cfg, report.RemainingCash, active)
			if result.CompletedTrades != 0 || result.FilledEntries != 2 || !shadowReportHasPosition(result, "sh600000") || shadowReportHasPosition(result, "sh600002") {
				t.Fatalf("rotation bypassed its hysteresis: %+v", result)
			}
			found := false
			for _, decision := range result.Decisions {
				if decision.Action == "wait" && strings.Contains(decision.Reason, test.reason) {
					found = true
				}
			}
			if !found {
				t.Fatalf("rotation wait reason missing %q: %+v", test.reason, result.Decisions)
			}
		})
	}
}

func TestShadowRotationHonorsDailyLimitAndBuyPreflight(t *testing.T) {
	t.Run("daily limit", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.HoldingDays = 20
		cfg.MaxOpenPositions = 3
		cfg.MaxDailyRotations = 1
		cfg.RotationScoreGap = 8
		cfg.RotationMinimumHoldDays = 2
		date := "2026-08-25"
		first := rotationTestPosition("sh600000", "第一弱仓", 55, cfg)
		second := rotationTestPosition("sh600001", "第二弱仓", 60, cfg)
		third := rotationTestPosition("sh600002", "保留仓", 70, cfg)
		report, active := rotationTestAccount(cfg, date, first, second, third)
		one := shadowSignal("rotation-one", "sh600003", "2026-08-24", 90)
		two := shadowSignal("rotation-two", "sh600004", "2026-08-24", 85)
		plans := []shadowPlan{
			{signal: one, bars: shadowStrategyBars(one.Symbol), capacity: 100_000_000, entryDate: date, entryIndex: 4, signalClose: 10.3},
			{signal: two, bars: shadowStrategyBars(two.Symbol), capacity: 100_000_000, entryDate: date, entryIndex: 4, signalClose: 10.3},
		}
		result := simulateFrom(report, plans, cfg, report.RemainingCash, active)
		if result.CompletedTrades != 1 || result.FilledEntries != 4 || len(result.Positions) != 3 || !shadowReportHasPosition(result, one.Symbol) || shadowReportHasPosition(result, two.Symbol) {
			t.Fatalf("daily rotation limit was not enforced: %+v", result)
		}
		found := false
		for _, decision := range result.Decisions {
			if decision.Symbol == two.Symbol && strings.Contains(decision.Reason, "当日换仓已达到 1 次上限") {
				found = true
			}
		}
		if !found {
			t.Fatalf("daily rotation limit decision missing: %+v", result.Decisions)
		}
	})

	t.Run("buy preflight", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.HoldingDays = 20
		cfg.MaxOpenPositions = 2
		cfg.MaxDailyRotations = 1
		cfg.RotationMinimumHoldDays = 2
		date := "2026-08-25"
		weak := rotationTestPosition("sh600000", "弱持仓", 60, cfg)
		strong := rotationTestPosition("sh600001", "强持仓", 75, cfg)
		report, active := rotationTestAccount(cfg, date, weak, strong)
		incoming := shadowSignal("illiquid", "sh600002", "2026-08-24", 90)
		plan := shadowPlan{signal: incoming, bars: shadowStrategyBars(incoming.Symbol), capacity: 1, entryDate: date, entryIndex: 4, signalClose: 10.3}
		result := simulateFrom(report, []shadowPlan{plan}, cfg, report.RemainingCash, active)
		if result.CompletedTrades != 0 || result.FilledEntries != 2 || !shadowReportHasPosition(result, weak.position.plan.signal.Symbol) {
			t.Fatalf("old position was sold before replacement buy passed capacity checks: %+v", result)
		}
	})
}

func TestShadowRotationDoesNotReenterRotatedSymbolAtSameOpen(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HoldingDays = 20
	cfg.MaxOpenPositions = 2
	cfg.MaxDailyRotations = 2
	cfg.RotationScoreGap = 8
	cfg.RotationMinimumHoldDays = 2
	date := "2026-08-25"
	weak := rotationTestPosition("sh600000", "弱持仓", 60, cfg)
	strong := rotationTestPosition("sh600001", "强持仓", 75, cfg)
	report, active := rotationTestAccount(cfg, date, weak, strong)
	incoming := shadowSignal("incoming", "sh600002", "2026-08-24", 90)
	reentry := shadowSignal("weak-refresh", weak.symbol, "2026-08-24", 80)
	plans := []shadowPlan{
		{signal: incoming, bars: shadowStrategyBars(incoming.Symbol), capacity: 100_000_000, entryDate: date, entryIndex: 4, signalClose: 10.3},
		{signal: reentry, bars: shadowStrategyBars(reentry.Symbol), capacity: 100_000_000, entryDate: date, entryIndex: 4, signalClose: 10.3},
	}
	result := simulateFrom(report, plans, cfg, report.RemainingCash, active)
	if shadowReportHasPosition(result, weak.symbol) || !shadowReportHasPosition(result, incoming.Symbol) || result.CompletedTrades != 1 {
		t.Fatalf("rotated symbol was reentered at the same open: %+v", result)
	}
	found := false
	for _, decision := range result.Decisions {
		if decision.Symbol == weak.symbol && strings.Contains(decision.Reason, "当日已被组合轮出") {
			found = true
		}
	}
	if !found {
		t.Fatalf("same-open reentry guard was not recorded: %+v", result.Decisions)
	}
}

func rotationTestPosition(symbol, name string, score float64, cfg Config) shadowRotationCandidate {
	bars := shadowStrategyBars(symbol)
	signal := shadowSignal("held-"+symbol, symbol, "2026-08-20", score)
	signal.Name = name
	entry := makeOrder(signal, "buy", "2026-08-21", bars[2].Open, 10_000, cfg, 0)
	entry.Status = OrderFilled
	entry.PositionAction = "open"
	plan := shadowPlan{signal: signal, bars: bars, entryDate: entry.AttemptDate, entryIndex: 2, signalClose: bars[1].Close}
	lot := shadowLot{plan: plan, entry: entry, entryCost: entry.Amount + transactionFee(entry.Amount, "buy", cfg)}
	position := shadowPosition{plan: plan, lots: []shadowLot{lot}, lastSignalDate: signalDate(signal), lastSignalScore: score, targetPercent: cfg.MaxPositionPercent}
	return shadowRotationCandidate{symbol: symbol, position: position, bar: bars[4], barIndex: 4, score: score}
}

func rotationTestAccount(cfg Config, date string, positions ...shadowRotationCandidate) (Report, map[string]shadowPosition) {
	report := Report{EngineVersion: ShadowEngineVersion, Config: cfg, InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash, FilledEntries: len(positions), AsOf: date}
	active := make(map[string]shadowPosition, len(positions))
	for _, item := range positions {
		active[item.symbol] = item.position
		for _, lot := range item.position.lots {
			report.Orders = append(report.Orders, lot.entry)
			report.RemainingCash -= lot.entryCost
			report.TotalTurnover += lot.entry.Amount
			report.TotalFees += lot.entryCost - lot.entry.Amount
		}
	}
	return report, active
}

func shadowReportHasPosition(report Report, symbol string) bool {
	for _, position := range report.Positions {
		if position.Symbol == symbol {
			return true
		}
	}
	return false
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

func TestValidateTransitionAllowsLaterSameDayRealtimeEvent(t *testing.T) {
	cfg := DefaultConfig()
	buyFee := transactionFee(1000, "buy", cfg)
	first := ShadowOrder{ID: "rt-1000-buy", EventID: "rt-1000", EventSource: "realtime-signal", Symbol: "sh600000", Side: "buy", AttemptDate: "2026-08-26", ExecutionTime: "2026-08-26 10:00:00", Quantity: 100, RawPrice: 10, Price: 10, Amount: 1000, Status: OrderFilled, PositionAction: "open"}
	firstLot := ShadowPositionLot{OrderID: first.ID, EntryDate: first.AttemptDate, EntryTime: first.ExecutionTime, Quantity: 100, EntryPrice: 10, EntryAmount: 1000, EntryFee: buyFee}
	previous := Report{EngineVersion: ShadowEngineVersion, Config: cfg, AsOf: "2026-08-26", CheckpointPhase: CheckpointOpen, LastRealtimeAt: first.ExecutionTime, InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash - first.Amount - buyFee, TotalTurnover: first.Amount, TotalFees: buyFee, FilledEntries: 1, Orders: []ShadowOrder{first}, Positions: []ShadowOpenPosition{{Symbol: first.Symbol, Quantity: 100, EntryDate: first.AttemptDate, EntryTime: first.ExecutionTime, EntryPrice: 10, EntryAmount: 1000, EntryFee: buyFee, Lots: []ShadowPositionLot{firstLot}}}}

	second := ShadowOrder{ID: "rt-1010-buy", EventID: "rt-1010", EventSource: "realtime-signal", Symbol: first.Symbol, Side: "buy", AttemptDate: first.AttemptDate, ExecutionTime: "2026-08-26 10:10:00", Quantity: 100, RawPrice: 10.1, Price: 10.1, Amount: 1010, Status: OrderFilled, PositionAction: "add"}
	secondFee := transactionFee(second.Amount, "buy", cfg)
	secondLot := ShadowPositionLot{OrderID: second.ID, EntryDate: second.AttemptDate, EntryTime: second.ExecutionTime, Quantity: 100, EntryPrice: second.Price, EntryAmount: second.Amount, EntryFee: secondFee}
	next := cloneReport(previous)
	next.LastRealtimeAt = second.ExecutionTime
	next.Orders = append(next.Orders, second)
	next.Positions = []ShadowOpenPosition{{Symbol: first.Symbol, Quantity: 200, EntryDate: first.AttemptDate, EntryTime: first.ExecutionTime, EntryPrice: (1000 + 1010) / 200, EntryAmount: 2010, EntryFee: buyFee + secondFee, Lots: []ShadowPositionLot{firstLot, secondLot}}}
	next.RemainingCash -= second.Amount + secondFee
	next.TotalTurnover += second.Amount
	next.TotalFees += secondFee
	next.FilledEntries++
	if err := ValidateTransition(previous, next); err != nil {
		t.Fatalf("later intraday event was rejected: %v", err)
	}

	retroactive := cloneReport(next)
	retroactive.Orders = append(retroactive.Orders, ShadowOrder{ID: "rt-0955-buy", EventID: "rt-0955", EventSource: "realtime-signal", Symbol: "sh600001", Side: "buy", AttemptDate: "2026-08-26", ExecutionTime: "2026-08-26 09:55:00", Quantity: 100, RawPrice: 10, Price: 10, Amount: 1000, Status: OrderFilled, PositionAction: "open"})
	if err := ValidateTransition(previous, retroactive); err == nil {
		t.Fatal("retroactive same-day event was accepted")
	}
}

func TestValidateTransitionCountsRealtimeExitAtOpenAsSettled(t *testing.T) {
	cfg := DefaultConfig()
	buyFee := transactionFee(1000, "buy", cfg)
	buy := ShadowOrder{
		ID: "entry-buy", EventID: "entry", EventSource: "realtime-signal",
		Symbol: "sh600000", Side: "buy", AttemptDate: "2026-08-26",
		ExecutionTime: "2026-08-26 09:30:00", Quantity: 100, RawPrice: 10,
		Price: 10, Amount: 1000, Status: OrderFilled, PositionAction: "open",
	}
	lot := ShadowPositionLot{
		OrderID: buy.ID, EntryDate: buy.AttemptDate, EntryTime: buy.ExecutionTime,
		Quantity: 100, EntryPrice: 10, EntryAmount: 1000, EntryFee: buyFee,
	}
	previous := Report{
		EngineVersion: ShadowEngineVersion, Config: cfg,
		AsOf: "2026-08-27", CheckpointPhase: CheckpointOpen,
		ExecutionMode: ExecutionModeLive, LastRealtimeAt: "2026-08-27 09:29:00",
		InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash - 1000 - buyFee,
		TotalTurnover: 1000, TotalFees: buyFee, FilledEntries: 1,
		Orders:    []ShadowOrder{buy},
		Positions: []ShadowOpenPosition{{Symbol: buy.Symbol, Quantity: 100, EntryDate: buy.AttemptDate, EntryTime: buy.ExecutionTime, EntryPrice: 10, EntryAmount: 1000, EntryFee: buyFee, Lots: []ShadowPositionLot{lot}}},
	}
	sell := ShadowOrder{
		ID: "realtime-exit-sell", EventID: "realtime-exit", EventSource: "realtime-risk",
		Symbol: buy.Symbol, Side: "sell", AttemptDate: "2026-08-27",
		ExecutionTime: "2026-08-27 09:30:12", Quantity: 100, RawPrice: 9.8,
		Price: 9.8, Amount: 980, Status: OrderFilled, PositionAction: "exit",
	}
	sellFee := transactionFee(sell.Amount, "sell", cfg)
	next := cloneReport(previous)
	next.LastRealtimeAt = sell.ExecutionTime
	next.Orders = append(next.Orders, sell)
	next.Positions = nil
	next.Trades = []ShadowTrade{{ID: "ST0001", EntryOrderID: buy.ID, ExitOrderID: sell.ID, Symbol: buy.Symbol, EntryDate: buy.AttemptDate, ExitDate: sell.AttemptDate, Quantity: 100, EntryPrice: 10, ExitPrice: sell.Price, PositionAction: "exit", ExitTime: sell.ExecutionTime, NetProfit: sell.Amount - sellFee - buy.Amount, TotalFee: buyFee + sellFee}}
	next.RemainingCash += sell.Amount - sellFee
	next.TotalTurnover += sell.Amount
	next.TotalFees += sellFee
	next.CompletedTrades = 1
	if err := ValidateTransition(previous, next); err != nil {
		t.Fatalf("realtime 09:30 exit was treated as unsettled: %v", err)
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

func TestTradingCheckpointAtFallsBackOutsideFiniteCalendarWindow(t *testing.T) {
	calendar := []string{"2026-08-20", "2026-08-22"}
	weekday := TradingCheckpointAt(time.Date(2026, 8, 24, 10, 0, 0, 0, shanghaiLocation), calendar)
	if weekday.Date != "2026-08-24" || weekday.Phase != CheckpointOpen {
		t.Fatalf("future weekday was incorrectly pinned to last calendar date: %+v", weekday)
	}
	weekend := TradingCheckpointAt(time.Date(2026, 8, 23, 10, 0, 0, 0, shanghaiLocation), calendar)
	if weekend.Date != "2026-08-22" || weekend.Phase != CheckpointClose {
		t.Fatalf("future weekend fallback advanced unexpectedly: %+v", weekend)
	}
}

func TestConfigFingerprintChangesWithExecutionAssumptions(t *testing.T) {
	base := DefaultConfig()
	changed := base
	changed.HoldingDays++
	if ConfigFingerprint(base) == ConfigFingerprint(changed) {
		t.Fatal("holding window did not change config fingerprint")
	}
	changed = base
	changed.RotationScoreGap++
	if ConfigFingerprint(base) == ConfigFingerprint(changed) {
		t.Fatal("rotation hysteresis did not change config fingerprint")
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

func TestAdvanceFetchesHistoryOnlyForSignalsThatCanActAtCheckpoint(t *testing.T) {
	held, relevant, old, current, weak := "sh600000", "sh600001", "sh600002", "sh600003", "sh600004"
	calendar := []domain.DailyBar{
		shadowBar("sh000300", "2026-08-20", 1, 1, 1.1, .9, 100_000_000),
		shadowBar("sh000300", "2026-08-21", 1, 1, 1.1, .9, 100_000_000),
		shadowBar("sh000300", "2026-08-24", 1, 1, 1.1, .9, 100_000_000),
		shadowBar("sh000300", "2026-08-25", 1, 1, 1.1, .9, 100_000_000),
	}
	makeBars := func(symbol string) []domain.DailyBar {
		result := append([]domain.DailyBar(nil), calendar...)
		for index := range result {
			result[index].Symbol = symbol
			result[index].Open = 10
			result[index].Close = 10
			result[index].High = 10.2
			result[index].Low = 9.8
		}
		return result
	}
	stub := &countingShadowHistoryStub{bars: map[string][]domain.DailyBar{
		"sh000300": calendar, held: makeBars(held), relevant: makeBars(relevant), old: makeBars(old), current: makeBars(current), weak: makeBars(weak),
	}, calls: make(map[string]int)}
	cfg := DefaultConfig()
	amount := 10.0 * 100
	fee := transactionFee(amount, "buy", cfg)
	previous := Report{
		EngineVersion: ShadowEngineVersion, Config: cfg, ConfigFingerprint: OptionsFingerprint(cfg, 0),
		AsOf: "2026-08-24", CheckpointPhase: CheckpointClose, InitialCash: cfg.InitialCash,
		RemainingCash: cfg.InitialCash - amount - fee, FilledEntries: 1, TotalTurnover: amount, TotalFees: fee,
		Orders:    []ShadowOrder{{ID: "held-buy", Symbol: held, Side: "buy", SignalDate: "2026-08-20", AttemptDate: "2026-08-21", Quantity: 100, RawPrice: 10, Price: 10, Amount: amount, Status: OrderFilled}},
		Positions: []ShadowOpenPosition{{SignalID: "held", Symbol: held, SignalDate: "2026-08-20", EntryDate: "2026-08-21", Quantity: 100, EntryPrice: 10, EntryAmount: amount, EntryFee: fee, SignalScore: 70}},
	}
	oldSignal := shadowSignal("old", old, "2026-08-21", 80)
	relevantSignal := shadowSignal("relevant", relevant, "2026-08-24", 80)
	currentSignal := shadowSignal("current", current, "2026-08-25", 80)
	weakSignal := shadowSignal("weak", weak, "2026-08-24", 40)
	weakSignal.State = realtime.StateWeak
	_, err := NewEvaluator(stub).Advance(context.Background(), previous, []realtime.Signal{oldSignal, relevantSignal, currentSignal, weakSignal}, Options{Config: cfg, Now: func() time.Time {
		return time.Date(2026, 8, 25, 10, 0, 0, 0, shanghaiLocation)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if stub.calls[relevant] != 1 || stub.calls[old] != 0 || stub.calls[current] != 0 || stub.calls[weak] != 0 {
		t.Fatalf("incremental signal prefilter failed: calls=%+v", stub.calls)
	}
}

func TestEvaluatorReusesTradingCalendarWithinShortWindow(t *testing.T) {
	calendar := []domain.DailyBar{
		shadowBar("sh000300", "2026-08-20", 1, 1, 1, 1, 1),
		shadowBar("sh000300", "2026-08-21", 1, 1, 1, 1, 1),
	}
	stub := &countingShadowHistoryStub{bars: map[string][]domain.DailyBar{"sh000300": calendar}, calls: make(map[string]int)}
	evaluator := NewEvaluator(stub)
	now := func() time.Time { return time.Date(2026, 8, 21, 10, 0, 0, 0, shanghaiLocation) }
	if _, err := evaluator.Evaluate(context.Background(), nil, Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := evaluator.Evaluate(context.Background(), nil, Options{Now: now}); err != nil {
		t.Fatal(err)
	}
	if stub.calls["sh000300"] != 1 {
		t.Fatalf("expected one cached calendar read, got %d", stub.calls["sh000300"])
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
