package strategy

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func assertNoHeadShoulders(t *testing.T, bars []domain.DailyBar, at time.Time, kind string) {
	t.Helper()
	analysis, err := AnalyzeChart("sh600519", bars, at, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range analysis.Structures {
		if item.ID == kind {
			t.Fatalf("unexpected %s: %+v", kind, item)
		}
	}
}

func TestHeadShouldersReadinessAndHistoricalIsolation(t *testing.T) {
	for _, kind := range []string{"head-shoulders-bottom", "head-shoulders-top"} {
		t.Run(kind, func(t *testing.T) {
			bars := classicPatternBars(kind)
			assertNoHeadShoulders(t, bars[:68], chartTestTime(bars[67].Date, 16), kind)
			assertNoHeadShoulders(t, bars[:69], chartTestTime(bars[68].Date, 10), kind)
			ready, err := AnalyzeChart("sh600519", bars[:69], chartTestTime(bars[68].Date, 16), "")
			if err != nil {
				t.Fatal(err)
			}
			item := classicStructure(t, ready, kind)
			if item.State != "watching" || item.Pattern.ReadyOn != bars[68].Date || len(item.Anchors) != 5 || len(item.Lines) != 7 {
				t.Fatalf("wrong readiness or geometry: %+v", item)
			}
			for i, label := range []string{"左肩", "左颈点", "头部", "右颈点", "右肩"} {
				if item.Anchors[i].Label != label {
					t.Fatalf("missing %s anchor", label)
				}
			}
			if kind == "head-shoulders-bottom" && (item.Pattern.TriggerPrice < math.Max(item.Anchors[1].Price, item.Anchors[3].Price) || item.Pattern.InvalidationPrice >= item.Anchors[4].Price || item.Pattern.InvalidationPrice <= item.Anchors[2].Price) {
				t.Fatalf("boundary must be outside neck and right shoulder: %+v", item)
			}
			bars[79].High, bars[79].Source = math.NaN(), "未来缓存"
			historical, err := AnalyzeChart("sh600519", bars, chartTestTime(bars[79].Date, 16), bars[68].Date)
			if err != nil || !reflect.DeepEqual(ready, historical) {
				t.Fatalf("future data changed historical recognition: %v", err)
			}
		})
	}
}

func TestHeadShouldersRejectsInvalidGeometry(t *testing.T) {
	for _, bottom := range []bool{true, false} {
		kind := "head-shoulders-top"
		if bottom {
			kind = "head-shoulders-bottom"
		}
		bars := classicPatternBars(kind)
		pivots := classicPivots(bars, 69)
		var candidate []classicPivot
		for i := range pivots {
			if pivots[i].index == 34 && i+5 <= len(pivots) {
				candidate = pivots[i : i+5]
			}
		}
		if _, ok := classicHeadShoulders(bars, true, 80, candidate, bottom); !ok {
			t.Fatal("baseline geometry was not recognized")
		}
		for _, test := range []struct {
			name   string
			change func([]classicPivot)
		}{
			{"unequal shoulders", func(p []classicPivot) { p[4].price += 15 }},
			{"flat head", func(p []classicPivot) { p[2].price = p[0].price }},
			{"sloping neck", func(p []classicPivot) { p[3].price += 10 }},
			{"unbalanced duration", func(p []classicPivot) { p[2].index = 60; p[3].index = 63 }},
			{"clustered pivots", func(p []classicPivot) { p[1].index = p[0].index + 1 }},
			{"wrong pivot order", func(p []classicPivot) { p[2].high = !p[2].high }},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				changed := append([]classicPivot(nil), candidate...)
				test.change(changed)
				if _, ok := classicHeadShoulders(bars, true, 80, changed, bottom); ok {
					t.Fatal("invalid geometry accepted")
				}
			})
		}
		bars[29].Close = candidate[0].price
		if _, ok := classicHeadShoulders(bars, true, 80, candidate, bottom); ok {
			t.Fatal("reversal accepted without a preceding directional move")
		}
		if _, ok := classicHeadShoulders(bars, true, 100, candidate, bottom); ok {
			t.Fatal("expired pattern was retained")
		}
	}
}

func TestHeadShouldersFalseBreakoutAndInvalidationPrecedence(t *testing.T) {
	for _, kind := range []string{"head-shoulders-bottom", "head-shoulders-top"} {
		t.Run(kind, func(t *testing.T) {
			bars := classicPatternBars(kind)
			bars[79].Volume = 1000
			analysis, _ := AnalyzeChart("sh600519", bars, chartTestTime(bars[79].Date, 16), "")
			item := classicStructure(t, analysis, kind)
			if item.State != "forming" || item.Pattern.ConfirmedOn != "" {
				t.Fatal("unconfirmed volume created a breakout")
			}
			// The next session returns inside the neckline without breaking the shoulder.
			next := chartTestBars(81)[80]
			price := (item.Pattern.TriggerPrice + item.Pattern.InvalidationPrice) / 2
			next.Open, next.Close, next.High, next.Low = price, price, price+.2, price-.2
			bars = append(bars, next)
			analysis, _ = AnalyzeChart("sh600519", bars, chartTestTime(next.Date, 16), "")
			item = classicStructure(t, analysis, kind)
			if item.State != "watching" || item.Pattern.ConfirmedOn != "" {
				t.Fatal("failed breakout retained confirmation")
			}
			for _, invalidIndex := range []int{70, 79} {
				failed := classicPatternBars(kind)
				if kind == "head-shoulders-bottom" {
					failed[invalidIndex].Low = item.Pattern.InvalidationPrice - 1
				} else {
					failed[invalidIndex].High = item.Pattern.InvalidationPrice + 1
				}
				analysis, _ = AnalyzeChart("sh600519", failed, chartTestTime(failed[79].Date, 16), "")
				invalid := classicStructure(t, analysis, kind)
				if invalid.State != "invalidated" || invalid.Pattern.ConfirmedOn != "" || invalid.Pattern.InvalidatedOn != failed[invalidIndex].Date {
					t.Fatalf("invalidation lost precedence: %+v", invalid)
				}
			}
		})
	}
}

func TestHeadShouldersFrozenMonitorHonorsEarlyInvalidation(t *testing.T) {
	bars := classicPatternBars("head-shoulders-bottom")
	created := chartTestTime(bars[68].Date, 16)
	analysis, _ := AnalyzeChart("sh600519", bars[:69], created, "")
	plan, err := BuildTradePlan(analysis, "head-shoulders-bottom", created.AddDate(0, 0, 60).Format(time.DateOnly), created)
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
		t.Fatalf("monitor confirmed after earlier invalidation: %+v", monitor)
	}
	if monitor.Rule.BreakoutPrice != plan.Structure.Pattern.TriggerPrice || monitor.Rule.Levels.Invalidation != plan.Structure.Pattern.InvalidationPrice {
		t.Fatal("monitor changed the frozen head-shoulders boundaries")
	}
}
