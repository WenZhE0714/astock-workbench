package app

import (
	"math"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestWatchFundMonitorCalculatesMinuteDeltasAndPreservesSelection(t *testing.T) {
	monitor := watchFundMonitor{}
	monitor.begin("默认", []string{"sh600519", "sz000001"})
	start := time.Date(2026, 8, 11, 10, 0, 0, 0, time.Local)
	for step := 0; step <= 30; step++ {
		at := start.Add(time.Duration(step) * 10 * time.Second)
		monitor.record(at, map[string]domain.FundFlow{
			"sh600519": {
				Symbol: "sh600519", Name: "贵州茅台", Industry: "白酒Ⅱ", Price: 1300 + float64(step), Percent: 1,
				MainNet: float64(step) * 12e6, MainRatio: float64(step) * 0.2,
			},
			"sz000001": {
				Symbol: "sz000001", Name: "平安银行", Industry: "银行Ⅱ", Price: 12, Percent: -0.2,
				MainNet: -float64(step) * 2e6, MainRatio: -float64(step) * 0.02,
			},
		})
	}
	if len(monitor.rows) != 2 || monitor.rows[0].Symbol != "sh600519" {
		t.Fatalf("unexpected sorted monitor rows: %#v", monitor.rows)
	}
	if monitor.rows[0].Delta1Minute != 72e6 || monitor.rows[0].Delta3Minutes != 216e6 || monitor.rows[0].Delta5Minutes != 360e6 {
		t.Fatalf("unexpected rolling deltas: %#v", monitor.rows[0])
	}
	monitor.selectIndex(1)
	selected, _ := monitor.selectedItem()
	monitor.record(start.Add(310*time.Second), map[string]domain.FundFlow{
		"sh600519": {Symbol: "sh600519", Name: "贵州茅台", MainNet: 500e6, MainRatio: 8, Price: 1332},
		"sz000001": {Symbol: "sz000001", Name: "平安银行", MainNet: -500e6, MainRatio: -5, Price: 12},
	})
	selectedAfter, _ := monitor.selectedItem()
	if selected.Symbol != selectedAfter.Symbol {
		t.Fatalf("selection changed across resort: before=%s after=%s", selected.Symbol, selectedAfter.Symbol)
	}
}

func TestSortFundMovementsByOneMinuteFlowDescending(t *testing.T) {
	rows := []domain.FundMovement{
		{Symbol: "sh600519", Delta1Minute: 1e7},
		{Symbol: "sz000001", Delta1Minute: -3e7},
		{Symbol: "sz300750", Delta1Minute: 5e7},
		{Symbol: "sh600000", Delta1Minute: math.NaN()},
	}
	sortFundMovementsByOneMinuteFlow(rows)
	want := []string{"sz300750", "sh600519", "sz000001", "sh600000"}
	for index, symbol := range want {
		if rows[index].Symbol != symbol {
			t.Fatalf("rows are not sorted by one-minute flow descending: %#v", rows)
		}
	}
}

func TestClassifyFundMovementUsesReversalPriceAndIndustryEvidence(t *testing.T) {
	current := domain.FundFlow{Percent: 1.2}
	positiveIndustry := domain.BoardFlow{MainNet: 2e9, Percent: 1.5}
	negativeIndustry := domain.BoardFlow{MainNet: -2e9, Percent: -1.5}
	tests := []struct {
		name       string
		delta1     float64
		delta3     float64
		previous1  float64
		ratioDelta float64
		price      float64
		industry   domain.BoardFlow
		want       string
	}{
		{name: "sampling", delta1: math.NaN(), delta3: math.NaN(), previous1: math.NaN(), ratioDelta: math.NaN(), price: math.NaN(), want: "采样中"},
		{name: "reversal", delta1: 3e7, delta3: 1e7, previous1: -2e7, price: 0.1, want: "流出转回流"},
		{name: "industry resonance", delta1: 3e7, delta3: 4e7, previous1: 1e7, price: 0.3, industry: positiveIndustry, want: "个股板块共振"},
		{name: "price divergence", delta1: -3e7, delta3: -5e7, previous1: -1e7, price: 0.5, want: "价涨资出"},
		{name: "negative resonance", delta1: -3e7, delta3: -4e7, previous1: -1e7, price: -0.3, industry: negativeIndustry, want: "板块共振流出"},
	}
	for _, test := range tests {
		if got := classifyFundMovement(current, test.delta1, test.delta3, test.previous1, test.ratioDelta, test.price, test.industry); got != test.want {
			t.Fatalf("%s: got %q want %q", test.name, got, test.want)
		}
	}
}

func TestWatchFundMonitorMatchesIndustryLevels(t *testing.T) {
	monitor := watchFundMonitor{industries: map[string]domain.BoardFlow{
		"银行": {Name: "银行", MainNet: 1},
	}}
	if got := monitor.industryFlow("银行Ⅱ"); got.Name != "银行" {
		t.Fatalf("industry level fallback failed: %#v", got)
	}
}

func TestWatchFundMonitorSyncsRankingMembershipWithoutTreatingReorderAsChange(t *testing.T) {
	monitor := watchFundMonitor{}
	monitor.beginRanking(domain.MarketRankingGainers, "涨幅榜前20", []string{"sh600519", "sz000001"})
	if changed := monitor.syncSymbols([]string{"sz000001", "sh600519"}); changed {
		t.Fatal("ranking reorder should not be treated as membership change")
	}
	if changed := monitor.syncSymbols([]string{"sz000001", "sz300750"}); !changed {
		t.Fatal("ranking membership change should update the monitor universe")
	}
	if len(monitor.symbols) != 2 || monitor.symbols[1] != "sz300750" {
		t.Fatalf("unexpected synchronized symbols: %#v", monitor.symbols)
	}
	if monitor.hasValidSample(map[string]domain.FundFlow{"sh600519": {Symbol: "sh600519", MainNet: 1}}) {
		t.Fatal("departed ranking symbol should not count as a current sample")
	}
	if !monitor.hasValidSample(map[string]domain.FundFlow{"sz300750": {Symbol: "sz300750", MainNet: 1}}) {
		t.Fatal("current ranking symbol should count as a valid sample")
	}
}

func TestWatchFundMonitorHiddenWarmupKeepsSamplesWhenOpened(t *testing.T) {
	monitor := watchFundMonitor{}
	symbols := []string{"sh600519", "sz000001"}
	monitor.beginHidden("自选 · 默认", symbols)
	if !monitor.active || monitor.viewing {
		t.Fatalf("hidden warmup should sample without changing the current view: %#v", monitor)
	}
	at := time.Date(2026, 8, 11, 10, 0, 0, 0, time.Local)
	monitor.record(at, map[string]domain.FundFlow{
		"sh600519": {Symbol: "sh600519", Name: "贵州茅台", MainNet: 1e8},
		"sz000001": {Symbol: "sz000001", Name: "平安银行", MainNet: 2e8},
	})
	if !monitor.matches("", "自选 · 默认", []string{"sz000001", "sh600519"}) {
		t.Fatal("reordered current watchlist should reuse the warmed monitor")
	}
	monitor.viewing = true
	if len(monitor.rows) != 2 || monitor.refreshedAt.IsZero() {
		t.Fatalf("opening should retain warmed samples: %#v", monitor)
	}
	if monitor.matches("", "自选 · 科技", symbols) {
		t.Fatal("different group should not reuse the old sample pool")
	}
}
