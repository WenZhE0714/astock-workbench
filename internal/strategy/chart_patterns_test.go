package strategy

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func classicPatternBars(kind string) []domain.DailyBar {
	bars := chartTestBars(80)
	knots := []struct {
		index int
		price float64
	}{{0, 114}, {36, 114}, {42, 115}, {50, 100}, {58, 110}, {66, 100.4}, {71, 106}, {76, 108}, {79, 112}}
	if kind == "ascending-triangle" || kind == "descending-triangle" {
		knots = []struct {
			index int
			price float64
		}{{0, 100}, {36, 100}, {42, 110}, {48, 98}, {54, 110.2}, {60, 101}, {66, 110.1}, {72, 104}, {79, 112}}
	}
	if kind == "head-shoulders-bottom" || kind == "head-shoulders-top" {
		knots = []struct {
			index int
			price float64
		}{{0, 120}, {20, 120}, {26, 122}, {34, 102}, {42, 112}, {50, 92}, {58, 112.4}, {66, 102.5}, {73, 108}, {79, 116}}
	}
	if kind == "bull-flag" || kind == "bear-flag" {
		knots = []struct {
			index int
			price float64
		}{{0, 100}, {24, 100}, {30, 95}, {42, 120}, {48, 116}, {54, 118.8}, {60, 114.8}, {66, 117.6}, {72, 113.6}, {79, 120.2}}
	}
	segment := 0
	for index := range bars {
		for segment < len(knots)-2 && index > knots[segment+1].index {
			segment++
		}
		left, right := knots[segment], knots[segment+1]
		price := left.price + (right.price-left.price)*float64(index-left.index)/float64(right.index-left.index)
		if kind == "double-top" || kind == "descending-triangle" || kind == "head-shoulders-top" || kind == "bear-flag" {
			price = 220 - price
		}
		bars[index].Open, bars[index].Close = price-.1, price
		bars[index].High, bars[index].Low = price+.5, price-.5
		if kind == "bull-flag" || kind == "bear-flag" {
			if index > 30 && index <= 42 {
				bars[index].Volume = 3000
			} else if index > 42 {
				bars[index].Volume = 700
			}
		}
	}
	bars[len(bars)-1].Volume = 2000
	return bars
}

func classicStructure(t *testing.T, analysis domain.ChartAnalysis, id string) domain.ChartStructure {
	t.Helper()
	for _, item := range analysis.Structures {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("missing %s in %+v", id, analysis.Structures)
	return domain.ChartStructure{}
}

func TestClassicPatternsUseDatedGeometryAndCompletedConfirmation(t *testing.T) {
	for _, kind := range []string{"double-bottom", "double-top", "ascending-triangle", "descending-triangle", "head-shoulders-bottom", "head-shoulders-top", "bull-flag", "bear-flag"} {
		t.Run(kind, func(t *testing.T) {
			bars := classicPatternBars(kind)
			now := chartTestTime(bars[79].Date, 16)
			analysis, err := AnalyzeChart("sh600519", bars, now, "")
			if err != nil {
				t.Fatal(err)
			}
			item := classicStructure(t, analysis, kind)
			if item.State != "confirmed" || item.Pattern == nil || item.Pattern.ConfirmedOn != bars[79].Date || len(item.Anchors) < 4 || len(item.Lines) < 3 {
				t.Fatalf("incomplete pattern result: %+v", item)
			}
			if item.Pattern.ReadyOn >= item.Pattern.ConfirmedOn || item.Pattern.Version != "classic-v1" {
				t.Fatalf("pattern readiness is not point-in-time: %+v", item.Pattern)
			}
			for _, line := range item.Lines {
				if line.From.Date > line.To.Date || line.To.Date > analysis.DataDate || line.From.Price <= 0 || line.To.Price <= 0 {
					t.Fatalf("invalid geometry: %+v", line)
				}
			}
			if kind == "double-top" || kind == "descending-triangle" || kind == "head-shoulders-top" || kind == "bear-flag" {
				if item.Pattern.Bias != "bearish" || item.Plan != nil || item.Pattern.InvalidationPrice <= item.Pattern.TriggerPrice {
					t.Fatalf("bearish structure generated a long plan: %+v", item)
				}
				if _, err := BuildTradePlan(analysis, kind, now.AddDate(0, 0, 7).Format(time.DateOnly), now); err == nil {
					t.Fatal("bearish structure was saved as a long plan")
				}
			} else {
				if item.Plan == nil || item.Plan.Invalidation >= item.Plan.EntryLow || item.Plan.Target2 <= item.Plan.EntryHigh {
					t.Fatalf("long plan risk boundaries invalid: %+v", item)
				}
				plan, err := BuildTradePlan(analysis, kind, now.AddDate(0, 0, 7).Format(time.DateOnly), now)
				if err != nil || plan.MonitorRule == nil || plan.MonitorRule.BreakoutPrice != item.Pattern.TriggerPrice || plan.MonitorRule.PatternReadyOn != item.Pattern.ReadyOn {
					t.Fatalf("plan did not freeze pattern rule: %v %+v", err, plan)
				}
				monitor, err := ConfigurePlanMonitor(plan, domain.PlanMonitor{}, true, now)
				if err != nil {
					t.Fatal(err)
				}
				monitor = AdvancePlanMonitor(monitor, PlanMonitorObservation{Now: now.Add(time.Minute), Session: "closing", CalendarKnown: true, TradingDate: bars[79].Date, PreviousTradingDate: bars[78].Date, Bars: bars})
				if monitor.Phase != "confirmed" || monitor.ConfirmedOn != item.Pattern.ConfirmedOn {
					t.Fatalf("chart and monitor disagree: %+v", monitor)
				}
			}
			intraday, err := AnalyzeChart("sh600519", bars, chartTestTime(bars[79].Date, 10), "")
			if err != nil {
				t.Fatal(err)
			}
			if provisional := classicStructure(t, intraday, kind); provisional.State != "forming" || provisional.Pattern.ConfirmedOn != "" {
				t.Fatalf("unfinished day confirmed pattern: %+v", provisional)
			}
			bars[79].Volume = 0
			withoutVolume, _ := AnalyzeChart("sh600519", bars, now, "")
			if classicStructure(t, withoutVolume, kind).State == "confirmed" {
				t.Fatal("missing volume confirmed a breakout")
			}
		})
	}
}

func TestClassicPatternPivotsDoNotUseUnfinishedOrFutureBars(t *testing.T) {
	bars := classicPatternBars("double-bottom")
	beforeReady, _ := AnalyzeChart("sh600519", bars[:68], chartTestTime(bars[67].Date, 16), "")
	for _, item := range beforeReady.Structures {
		if item.ID == "double-bottom" {
			t.Fatal("right pivot used fewer than two later completed bars")
		}
	}
	provisional, _ := AnalyzeChart("sh600519", bars[:69], chartTestTime(bars[68].Date, 10), "")
	for _, item := range provisional.Structures {
		if item.ID == "double-bottom" {
			t.Fatal("unfinished confirmation candle completed a pivot")
		}
	}
	ready, err := AnalyzeChart("sh600519", bars[:69], chartTestTime(bars[68].Date, 16), "")
	if err != nil || classicStructure(t, ready, "double-bottom").Pattern.ReadyOn != bars[68].Date {
		t.Fatalf("second completed bar did not confirm pivot: %v", err)
	}
	historical, _ := AnalyzeChart("sh600519", bars, chartTestTime(bars[79].Date, 16), bars[68].Date)
	if historical.Fingerprint != ready.Fingerprint {
		t.Fatal("future bars leaked into historical pattern")
	}
	bars[79].High, bars[79].Close = 10000, 9000
	historical, _ = AnalyzeChart("sh600519", bars, chartTestTime(bars[79].Date, 16), bars[68].Date)
	if historical.Fingerprint != ready.Fingerprint {
		t.Fatal("changing future price changed the historical pattern")
	}
}

func TestClassicPatternRejectsFlatHistoryAndUnequalBottoms(t *testing.T) {
	if items := classicChartStructures(chartTestBars(100), true); len(items) != 0 {
		t.Fatalf("flat history produced classic patterns: %+v", items)
	}
	bars := classicPatternBars("double-bottom")
	for index := 64; index <= 68; index++ {
		bars[index].Low -= 6
	}
	for _, item := range classicChartStructures(bars, true) {
		if item.ID == "double-bottom" {
			t.Fatalf("unequal bottoms matched: %+v", item)
		}
	}
}

func TestClassicMonitorCannotConfirmAfterEarlierInvalidation(t *testing.T) {
	bars := classicPatternBars("double-bottom")
	created := chartTestTime(bars[68].Date, 16)
	analysis, _ := AnalyzeChart("sh600519", bars[:69], created, "")
	plan, err := BuildTradePlan(analysis, "double-bottom", created.AddDate(0, 0, 60).Format(time.DateOnly), created)
	if err != nil {
		t.Fatal(err)
	}
	monitor, err := ConfigurePlanMonitor(plan, domain.PlanMonitor{}, true, created)
	if err != nil {
		t.Fatal(err)
	}
	bars[70].Low = plan.Structure.Plan.Invalidation - 1
	now := chartTestTime(bars[79].Date, 16)
	monitor = AdvancePlanMonitor(monitor, PlanMonitorObservation{Now: now, Session: "closing", CalendarKnown: true, TradingDate: bars[79].Date, PreviousTradingDate: bars[78].Date, Bars: bars})
	if monitor.Phase != "invalidated" || monitor.ConfirmedOn != "" || monitor.Events[len(monitor.Events)-1].DataDate != bars[70].Date {
		t.Fatalf("old invalidation ignored: %+v", monitor)
	}
}

func TestClassicMonitorRequiresCompleteVolumeWarmup(t *testing.T) {
	bars := classicPatternBars("double-bottom")
	now := chartTestTime(bars[79].Date, 16)
	analysis, _ := AnalyzeChart("sh600519", bars, now, "")
	plan, err := BuildTradePlan(analysis, "double-bottom", now.AddDate(0, 0, 7).Format(time.DateOnly), now)
	if err != nil {
		t.Fatal(err)
	}
	monitor, err := ConfigurePlanMonitor(plan, domain.PlanMonitor{}, true, now)
	if err != nil {
		t.Fatal(err)
	}
	bars[65].Volume = 0
	monitor = AdvancePlanMonitor(monitor, PlanMonitorObservation{Now: now.Add(time.Minute), Session: "closing", CalendarKnown: true, TradingDate: bars[79].Date, PreviousTradingDate: bars[78].Date, Bars: bars})
	if monitor.ConfirmedOn != "" {
		t.Fatal("incomplete volume warmup confirmed a pattern")
	}
	if ratio := classicVolumeRatio(bars, 79); ratio != nil {
		t.Fatalf("missing volume converted to %.2f", *ratio)
	}
	if math.IsNaN(plan.Structure.Plan.RiskPerShare) {
		t.Fatal("risk levels are not finite")
	}
}

func TestClassicMonitorInvalidatesIntradayBeforeConfirmation(t *testing.T) {
	bars := classicPatternBars("double-bottom")
	created := chartTestTime(bars[68].Date, 16)
	analysis, _ := AnalyzeChart("sh600519", bars[:69], created, "")
	plan, err := BuildTradePlan(analysis, "double-bottom", created.AddDate(0, 0, 60).Format(time.DateOnly), created)
	if err != nil {
		t.Fatal(err)
	}
	monitor, err := ConfigurePlanMonitor(plan, domain.PlanMonitor{}, true, created)
	if err != nil {
		t.Fatal(err)
	}
	now := chartTestTime(bars[69].Date, 10)
	monitor = AdvancePlanMonitor(monitor, PlanMonitorObservation{Now: now, Session: "trading", CalendarKnown: true, TradingDate: bars[69].Date, PreviousTradingDate: bars[68].Date, Bars: bars[:69], Quote: domain.Quote{Symbol: plan.Symbol, Current: "98.00", PreviousClose: fmt.Sprintf("%.2f", bars[68].Close), QuoteTime: now.Format("2006-01-02 15:04:05")}})
	if monitor.Phase != "invalidated" || monitor.ConfirmedOn != "" {
		t.Fatalf("unconfirmed pattern ignored live invalidation: %+v", monitor)
	}
}

func TestClassicPatternDoesNotRecoverAfterConfirmedInvalidation(t *testing.T) {
	bars := classicPatternBars("double-bottom")
	now := chartTestTime(bars[79].Date, 16)
	initial, _ := AnalyzeChart("sh600519", bars, now, "")
	pattern := classicStructure(t, initial, "double-bottom")
	next := chartTestBars(82)
	bars = append(bars, next[80:]...)
	bars[80].Open, bars[80].High, bars[80].Low, bars[80].Close = 110, 111, pattern.Pattern.InvalidationPrice-1, 100
	bars[81].Open, bars[81].Low, bars[81].High, bars[81].Close, bars[81].Volume = 111, 110, 116, 115, 3000
	analysis, err := AnalyzeChart("sh600519", bars, chartTestTime(bars[81].Date, 16), "")
	if err != nil {
		t.Fatal(err)
	}
	item := classicStructure(t, analysis, "double-bottom")
	if item.State != "invalidated" || item.Pattern.ConfirmedOn != initial.DataDate || item.Pattern.InvalidatedOn != bars[80].Date {
		t.Fatalf("invalidated pattern silently recovered: %+v", item)
	}
}

func TestClassicMonitorWaitsForHistoryCoveringPatternOrigin(t *testing.T) {
	bars := classicPatternBars("double-bottom")
	now := chartTestTime(bars[79].Date, 16)
	analysis, _ := AnalyzeChart("sh600519", bars, now, "")
	plan, err := BuildTradePlan(analysis, "double-bottom", now.AddDate(0, 0, 7).Format(time.DateOnly), now)
	if err != nil {
		t.Fatal(err)
	}
	monitor, err := ConfigurePlanMonitor(plan, domain.PlanMonitor{}, true, now)
	if err != nil {
		t.Fatal(err)
	}
	observation := PlanMonitorObservation{Now: now.Add(time.Minute), Session: "closing", CalendarKnown: true, TradingDate: bars[79].Date, PreviousTradingDate: bars[78].Date, Bars: bars[55:]}
	monitor = AdvancePlanMonitor(monitor, observation)
	if monitor.DataStatus != "stale_history" || monitor.ConfirmedOn != "" {
		t.Fatalf("truncated history confirmed a pattern: %+v", monitor)
	}
	observation.Now = observation.Now.Add(time.Minute)
	observation.Bars = bars
	monitor = AdvancePlanMonitor(monitor, observation)
	if monitor.ConfirmedOn != bars[79].Date || monitor.DataStatus != "healthy" {
		t.Fatalf("complete history did not re-enable evaluation: %+v", monitor)
	}
}
