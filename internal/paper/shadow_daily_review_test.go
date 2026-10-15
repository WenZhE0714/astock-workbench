package paper

import (
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/realtime"
)

func TestShadowDailyScanQualificationAndDeduplication(t *testing.T) {
	at := time.Date(2026, 10, 15, 10, 0, 0, 0, shanghaiLocation)
	cfg := DefaultConfig()
	cfg.TradingStartAt = at
	qualified := realtime.Signal{ID: "one", Symbol: "sh600000", AsOf: at, State: realtime.StateWatching, Score: 70, RiskAdjustedScore: 59.5, RiskMultiplier: .85, PortfolioEligible: true}
	blocked := realtime.Signal{ID: "two", Symbol: "sh600001", AsOf: at, State: realtime.StateWeak, Score: 70, RiskAdjustedScore: 42, RiskMultiplier: .6, MarketRegime: "熊市"}
	scan := realtime.ScanResult{GeneratedAt: at, MarketState: realtime.MarketStateTrading, TradingDate: "2026-10-15", TradingDay: true, Universe: "test", Signals: []realtime.Signal{qualified, blocked}}
	builder := NewShadowScanReviewBuilder("2026-10-15", cfg)
	before := scan
	before.GeneratedAt = at.Add(-time.Minute)
	builder.Add(before)
	auction := scan
	auction.MarketState = realtime.MarketStateAuction
	builder.Add(auction)
	builder.Add(scan)
	builder.Add(scan)
	result := builder.Result()
	if result.Scans != 1 || result.Observations != 2 || result.Eligible != 1 || result.EligibleSymbols != 1 || result.Symbols != 2 || result.BeforeStart != 1 || result.ExcludedScans != 1 || result.DuplicateScans != 1 {
		t.Fatalf("incorrect counts: %+v", result)
	}
	if len(result.Gates) != 1 || result.Gates[0].Code != "risk-adjusted-score" || result.Gates[0].Count != 1 {
		t.Fatalf("incorrect gate: %+v", result.Gates)
	}
	scan.GeneratedAt = at.Add(time.Minute)
	scan.Signals = []realtime.Signal{{ID: "future", Symbol: "sh600003", AsOf: at.Add(2 * time.Minute), State: realtime.StateTriggered, Score: 90}}
	builder.Add(scan)
	if builder.Result().InvalidSignals != 1 || builder.Result().Observations != 2 {
		t.Fatal("future signal included")
	}
}

func TestShadowDailyMonsterExplainsHighScoreWithoutEligibility(t *testing.T) {
	at := time.Date(2026, 10, 15, 10, 0, 0, 0, shanghaiLocation)
	cfg := DefaultConfig()
	cfg.UseMonsterRadar = true
	cfg.MinimumScore = 58
	builder := NewShadowScanReviewBuilder("2026-10-15", cfg)
	builder.Add(realtime.ScanResult{GeneratedAt: at, MarketState: realtime.MarketStateTrading, Signals: []realtime.Signal{{Symbol: "sh600000", AsOf: at, MarketRegime: "熊市", Monster: realtime.MonsterRadar{Score: 100, Eligible: false, QuoteFresh: true}}}})
	result := builder.Result()
	if result.Eligible != 0 || result.HighestEntryScore != 100 || len(result.Gates) != 1 || result.Gates[0].Code != "monster-bear" {
		t.Fatalf("bear gate missing: %+v", result)
	}
}

func TestShadowDailyLedgerDateExcludesLaterEvents(t *testing.T) {
	report := Report{AsOf: "2026-10-16", Config: DefaultConfig(), CalibrationID: "future-calibration",
		Orders:    []ShadowOrder{{Status: OrderFilled, Side: "buy", AttemptDate: "2026-10-15", Amount: 1000}, {Status: OrderFilled, Side: "sell", AttemptDate: "2026-10-16", Amount: 2000}},
		Trades:    []ShadowTrade{{ExitDate: "2026-10-15", NetProfit: -10}, {ExitDate: "2026-10-16", NetProfit: 200}},
		Decisions: []ShadowDecision{{Date: "2026-10-15", Action: "wait", Reason: "入场价高于参考价 10.00%，超过对照上限 5.00%"}},
	}
	review := ShadowDailyReviewForDate(report, "2026-10-15")
	if review.OrderCount != 1 || review.RealizedProfit != -10 || review.Turnover != 1000 || review.CalibrationSummary != "" || report.AsOf != "2026-10-16" {
		t.Fatalf("historical view leaked: %+v", review)
	}
	if len(review.TopBlockers) != 1 || review.TopBlockers[0].Reason != "入场价格延伸超过上限" {
		t.Fatalf("policy reason not grouped: %+v", review.TopBlockers)
	}
}
