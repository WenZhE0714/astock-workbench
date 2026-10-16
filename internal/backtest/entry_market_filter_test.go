package backtest

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/marketregime"
)

type marketFilterHistory struct{ stock, benchmark []domain.DailyBar }

func (p marketFilterHistory) FetchDailyBarsRange(_ context.Context, symbol string, _, _ time.Time, _ PriceAdjustment) ([]domain.DailyBar, error) {
	bars := p.stock
	if symbol == "sh000300" {
		bars = p.benchmark
	}
	result := append([]domain.DailyBar(nil), bars...)
	for i := range result {
		result[i].Symbol = symbol
	}
	return result, nil
}

func marketFilterBars(declining bool) []domain.DailyBar {
	result := testBars()
	for i := range result {
		price := 100.0
		if declining {
			price *= math.Pow(.995, float64(i))
		}
		result[i].Open, result[i].Close, result[i].High, result[i].Low = price, price, price*1.01, price*.99
	}
	return result
}

func TestEntryMarketFilterBlocksOnlyOpeningAndUsesPriorCloses(t *testing.T) {
	request := testRequest()
	request.Benchmark = "sh000300"
	provider := marketFilterHistory{stock: testBars(), benchmark: marketFilterBars(true)}
	baseline, err := NewDailyEngine(provider).Run(context.Background(), request)
	if err != nil || len(baseline.Trades) != 1 {
		t.Fatalf("baseline failed: %v", err)
	}
	request.EntryMarketFilter = EntryMarketNonBear
	filtered, err := NewDailyEngine(provider).Run(context.Background(), request)
	if err != nil || len(filtered.Trades) != 0 || len(filtered.BlockedEntries) != 1 {
		t.Fatalf("bear entry not blocked: %+v %v", filtered.BlockedEntries, err)
	}
	block := filtered.BlockedEntries[0]
	if block.Regime != marketregime.Bear || block.BenchmarkDate >= block.AttemptDate {
		t.Fatal("incorrect information cutoff")
	}
	provider.benchmark[61].Close = 10000
	provider.benchmark[61].High = 10001
	again, err := NewDailyEngine(provider).Run(context.Background(), request)
	if err != nil || !reflect.DeepEqual(filtered.BlockedEntries, again.BlockedEntries) {
		t.Fatal("same-day benchmark close leaked into opening decision")
	}
}

func TestEntryMarketFilterDoesNotBlockExistingExit(t *testing.T) {
	benchmark := marketFilterBars(false)
	benchmark[61].Open, benchmark[61].Close, benchmark[61].High, benchmark[61].Low = 95, 95, 96, 94
	allowed, regime, _ := entryMarketDecision(benchmark, benchmark[62].Date, benchmark[61].Date)
	if allowed || regime != marketregime.Bear {
		t.Fatal("test must enter bear regime before exit")
	}
	provider := marketFilterHistory{stock: testBars(), benchmark: benchmark}
	request := testRequest()
	request.Benchmark = "sh000300"
	baseline, err := NewDailyEngine(provider).Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.EntryMarketFilter = EntryMarketNonBear
	filtered, err := NewDailyEngine(provider).Run(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Trades) != 1 || !reflect.DeepEqual(baseline.Trades, filtered.Trades) {
		t.Fatal("market entry filter changed an existing position exit")
	}
}

func TestEntryMarketFilterRejectsMissingAndStaleBenchmark(t *testing.T) {
	request := testRequest()
	request.Benchmark = "sh000300"
	request.EntryMarketFilter = EntryMarketNonBear
	if _, err := NewDailyEngine(marketFilterHistory{stock: testBars()}).Run(context.Background(), request); err == nil {
		t.Fatal("missing benchmark silently produced cash success")
	}
	bars := marketFilterBars(false)
	bars = append(bars[:60], bars[61:]...)
	allowed, regime, _ := entryMarketDecision(bars, testBars()[61].Date, testBars()[60].Date)
	if allowed || regime != marketregime.Insufficient {
		t.Fatal("stale previous close accepted")
	}
	request.EntryMarketFilter = "unknown"
	if err := validateRequest(request); err == nil {
		t.Fatal("unknown market filter accepted")
	}
}

func TestRegimeOpportunityImpactKeepsAttributionSeparate(t *testing.T) {
	baseline := Result{Trades: []Trade{
		{Symbol: "loss", EntrySignal: SignalSnapshot{Date: "2026-01-01"}, NetProfit: -100},
		{Symbol: "win", EntrySignal: SignalSnapshot{Date: "2026-01-01"}, NetProfit: 60},
		{Symbol: "retained", EntrySignal: SignalSnapshot{Date: "2026-01-02"}, NetProfit: 10},
	}, Metrics: Metrics{FinalEquity: 970}, Equity: []EquityPoint{{Equity: 1000, Positions: 100}}}
	filtered := Result{Trades: []Trade{{Symbol: "retained", EntrySignal: SignalSnapshot{Date: "2026-01-02"}}}, BlockedEntries: []EntryMarketBlock{{Symbol: "loss", SignalDate: "2026-01-01"}, {Symbol: "win", SignalDate: "2026-01-01"}, {Symbol: "no-baseline-fill", SignalDate: "2026-01-01"}}, Metrics: Metrics{FinalEquity: 1020}, Equity: []EquityPoint{{Equity: 1000, Positions: 50}}}
	impact := regimeOpportunityImpact(baseline, filtered)
	if impact.BlockedAttempts != 3 || impact.AvoidedLosses != 1 || impact.AvoidedLossAmount != 100 || impact.MissedWins != 1 || impact.MissedProfitAmount != 60 || impact.EquityDifference != 50 {
		t.Fatalf("invalid attribution: %+v", impact)
	}
}
