package paper

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

func TestShadowPolicyKeepsBaselineFingerprint(t *testing.T) {
	if hash := ConfigFingerprint(DefaultConfig()); hash != "2f712dff775cffd4085e53a823f5b5eda307a3222b4ca0312ce6893c92eb7db6" {
		t.Fatalf("baseline fingerprint changed: %s", hash)
	}
}

func TestShadowPolicyRealtimeVariants(t *testing.T) {
	for _, variant := range []string{"baseline", "no-add", "bounded", "intraday"} {
		t.Run(variant, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.DisableSignalAdditions = variant == "no-add"
			cfg.IntradayEntriesOnly = variant == "intraday"
			if variant == "bounded" {
				cfg.MaxEntryExtensionPercent = 5
			}
			now := time.Date(2026, 8, 26, 10, 0, 0, 0, shanghaiLocation)
			cfg.TradingStartAt = now.Add(-time.Minute)
			symbol := "sh600000"
			history := shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: shadowStrategyBars(symbol)}}
			evaluator := NewEvaluator(history)
			report := Report{EngineVersion: ShadowEngineVersion, Config: cfg, ConfigFingerprint: ConfigFingerprint(cfg), AsOf: "2026-08-26", CheckpointPhase: CheckpointOpen, InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash}
			signal := realtimeShadowSignal("first", symbol, "2026-08-26 10:00", 60)
			signal.TriggerPrice = 10
			options := Options{Config: cfg, Realtime: true, RealtimeAt: now, CalendarDates: []string{"2026-08-25", "2026-08-26"}, RealtimeQuotes: []PositionQuote{{Symbol: symbol, Price: 10, QuoteTime: "2026-08-26 10:00:00"}}}
			first, err := evaluator.AdvanceRealtime(context.Background(), report, []realtime.Signal{signal}, options)
			if err != nil || len(first.Orders) != 1 {
				t.Fatalf("initial entry: %+v %v", first.Orders, err)
			}
			options.RealtimeAt = now.Add(30 * time.Minute)
			options.RealtimeQuotes[0].QuoteTime = "2026-08-26 10:30:00"
			options.RealtimeQuotes[0].Price = 11
			signal.AsOf, signal.Score = options.RealtimeAt, 80
			second, err := evaluator.AdvanceRealtime(context.Background(), first, []realtime.Signal{signal}, options)
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if variant == "no-add" || variant == "bounded" {
				want = 1
			}
			if len(second.Orders) != want {
				t.Fatalf("orders=%d want=%d decisions=%+v", len(second.Orders), want, second.Decisions)
			}
			if first.Orders[0].ID != second.Orders[0].ID {
				t.Fatal("initial fill rewritten")
			}
		})
	}
}

func TestShadowPolicyStartAndDailyIsolation(t *testing.T) {
	symbol := "sh600000"
	bars := shadowStrategyBars(symbol)
	evaluator := NewEvaluator(shadowHistoryStub{bars: map[string][]domain.DailyBar{symbol: bars, "sh000300": bars}})
	base := DefaultConfig()
	start := time.Date(2026, 8, 20, 14, 0, 0, 0, shanghaiLocation)
	base.TradingStartAt = start
	old := shadowSignal("old", symbol, "2026-08-19", 80)
	current := shadowSignal("current", symbol, "2026-08-20", 80)
	for _, intraday := range []bool{false, true} {
		cfg := base
		cfg.IntradayEntriesOnly = intraday
		report, err := evaluator.Evaluate(context.Background(), []realtime.Signal{old, current}, Options{Config: cfg, CalendarDates: barDates(bars), Now: func() time.Time { return time.Date(2026, 8, 21, 10, 0, 0, 0, shanghaiLocation) }})
		if err != nil {
			t.Fatal(err)
		}
		if intraday && len(report.Orders) != 0 {
			t.Fatal("daily entry bypassed intraday-only policy")
		}
		if !intraday && len(report.Orders) != 1 {
			t.Fatalf("daily control did not enter: %+v", report)
		}
		for _, order := range report.Orders {
			if strings.HasPrefix(order.ID, "old") {
				t.Fatal("pre-start trade")
			}
		}
	}
	report, err := evaluator.Evaluate(context.Background(), []realtime.Signal{old}, Options{Config: base, CalendarDates: barDates(bars), Now: func() time.Time { return start }})
	if err != nil || len(report.Orders) != 0 || report.TotalEquity != base.InitialCash {
		t.Fatalf("empty trial: %+v %v", report, err)
	}
}

func TestShadowHoldingDaysUseDatesAcrossHistoryWindows(t *testing.T) {
	bars := shadowStrategyBars("sh600000")
	lot := shadowLot{entry: ShadowOrder{AttemptDate: "2026-08-25", Quantity: 100, Price: 10, Amount: 1000}, entryCost: 1005, plan: shadowPlan{bars: bars, entryIndex: 4}}
	trade := tradeForLot(lot, ShadowOrder{Price: 11, Amount: 1100, Quantity: 100}, 11, "2026-08-26", 5000, 5, 1)
	if trade.HoldingDays != 2 {
		t.Fatalf("window index leaked into holding days: %d", trade.HoldingDays)
	}
	position := ShadowOpenPosition{Quantity: 100, Lots: []ShadowPositionLot{{OrderID: "empty", Quantity: 0, EntryAmount: 5000}, {OrderID: "valid", Quantity: 100, EntryAmount: 1000}}}
	lots := normalizePositionLots(position, nil, DefaultConfig(), nil, nil)
	if len(lots) != 1 || lots[0].entry.ID != "valid" {
		t.Fatalf("zero lot restored: %+v", lots)
	}
}
