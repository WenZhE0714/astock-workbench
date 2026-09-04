package realtime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type leaderMarketStub struct {
	mu      sync.Mutex
	calls   []domain.MarketScanMetric
	results map[domain.MarketScanMetric][]domain.MarketStockSnapshot
	errors  map[domain.MarketScanMetric]error
}

func (stub *leaderMarketStub) FetchStockRanking(_ context.Context, metric domain.MarketScanMetric, descending bool, limit int) ([]domain.MarketStockSnapshot, error) {
	stub.mu.Lock()
	stub.calls = append(stub.calls, metric)
	stub.mu.Unlock()
	if !descending || limit != defaultLeaderLimit {
		return nil, errors.New("unexpected ranking request")
	}
	return stub.results[metric], stub.errors[metric]
}

func (*leaderMarketStub) FetchStocks(context.Context, []string) ([]domain.MarketStockSnapshot, error) {
	return nil, nil
}

func (*leaderMarketStub) FetchIndustryRanking(context.Context, domain.MarketScanMetric, bool, int) ([]domain.BoardFlow, error) {
	return nil, nil
}

func TestFetchLeaderSymbolsDiversifiesRankingSources(t *testing.T) {
	stub := &leaderMarketStub{results: map[domain.MarketScanMetric][]domain.MarketStockSnapshot{
		domain.MarketScanByAmount: {
			{Symbol: "sh600001", Percent: 1},
			{Symbol: "sh600004", Speed: .1},
			{Symbol: "sh600099", Percent: -1, Speed: -1, MainNet: -1},
		},
		domain.MarketScanByMainNet: {
			{Symbol: "sz000002", MainNet: 1},
			{Symbol: "sh600001", MainNet: 2},
			{Symbol: "sz000099", MainNet: -1},
		},
		domain.MarketScanByPercent: {
			{Symbol: "sz000003", Percent: 2},
			{Symbol: "sz000005", Percent: 1},
			{Symbol: "sz000098", Percent: -1},
		},
	}}

	symbols, warnings := (&Scanner{market: stub}).fetchLeaderSymbols(context.Background())
	want := []string{"sh600001", "sz000002", "sz000003", "sh600004", "sz000005"}
	if !reflect.DeepEqual(symbols, want) || len(warnings) != 0 {
		t.Fatalf("unexpected diversified leaders: symbols=%v warnings=%v", symbols, warnings)
	}
	stub.mu.Lock()
	calls := append([]domain.MarketScanMetric(nil), stub.calls...)
	stub.mu.Unlock()
	sort.Slice(calls, func(i, j int) bool { return calls[i] < calls[j] })
	wantCalls := []domain.MarketScanMetric{domain.MarketScanByAmount, domain.MarketScanByMainNet, domain.MarketScanByPercent}
	sort.Slice(wantCalls, func(i, j int) bool { return wantCalls[i] < wantCalls[j] })
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("ranking sources were not all fetched: %v", calls)
	}
}

func TestFetchLeaderSymbolsKeepsPartialResultsAndAuditsFailures(t *testing.T) {
	stub := &leaderMarketStub{
		results: map[domain.MarketScanMetric][]domain.MarketStockSnapshot{
			domain.MarketScanByAmount:  {{Symbol: "sh600001", Percent: 1}},
			domain.MarketScanByPercent: {{Symbol: "sz000003", Percent: 2}},
		},
		errors: map[domain.MarketScanMetric]error{domain.MarketScanByMainNet: errors.New("timeout")},
	}

	symbols, warnings := (&Scanner{market: stub}).fetchLeaderSymbols(context.Background())
	if !reflect.DeepEqual(symbols, []string{"sh600001", "sz000003"}) {
		t.Fatalf("partial leader results were lost: %v", symbols)
	}
	if len(warnings) != 1 || warnings[0] != "主力净流入候选获取失败: timeout" {
		t.Fatalf("ranking failure was not audited: %v", warnings)
	}
}

func TestFetchLeaderSymbolsFillsCapacityAfterCrossRankingDuplicates(t *testing.T) {
	amount := make([]domain.MarketStockSnapshot, 0, defaultLeaderLimit)
	mainNet := make([]domain.MarketStockSnapshot, 0, defaultLeaderLimit)
	percent := make([]domain.MarketStockSnapshot, 0, defaultLeaderLimit)
	for index := 0; index < defaultLeaderLimit; index++ {
		amountSymbol := fmt.Sprintf("sh60%04d", index)
		amount = append(amount, domain.MarketStockSnapshot{Symbol: amountSymbol, Percent: 1})
		mainNet = append(mainNet, domain.MarketStockSnapshot{Symbol: amountSymbol, MainNet: 1})
		percent = append(percent, domain.MarketStockSnapshot{Symbol: fmt.Sprintf("sz00%04d", index), Percent: 1})
	}
	stub := &leaderMarketStub{results: map[domain.MarketScanMetric][]domain.MarketStockSnapshot{
		domain.MarketScanByAmount: amount, domain.MarketScanByMainNet: mainNet, domain.MarketScanByPercent: percent,
	}}

	symbols, warnings := (&Scanner{market: stub}).fetchLeaderSymbols(context.Background())
	if len(symbols) != 2*defaultLeaderLimit || len(warnings) != 0 {
		t.Fatalf("duplicates reduced discoverable capacity: got=%d warnings=%v", len(symbols), warnings)
	}
}
