package strategy

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func requireNoFlag(t *testing.T, bars []domain.DailyBar, at time.Time, kind string) {
	t.Helper()
	analysis, err := AnalyzeChart("sh600519", bars, at, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range analysis.Structures {
		if item.ID == kind {
			t.Fatalf("unexpected flag: %+v", item)
		}
	}
}

func TestFlagReadinessAndHistoricalIsolation(t *testing.T) {
	for _, kind := range []string{"bull-flag", "bear-flag"} {
		t.Run(kind, func(t *testing.T) {
			bars := classicPatternBars(kind)
			requireNoFlag(t, bars[:74], chartTestTime(bars[73].Date, 16), kind)
			requireNoFlag(t, bars[:75], chartTestTime(bars[74].Date, 10), kind)
			ready, err := AnalyzeChart("sh600519", bars[:75], chartTestTime(bars[74].Date, 16), "")
			if err != nil {
				t.Fatal(err)
			}
			item := classicStructure(t, ready, kind)
			if item.State != "watching" || item.Pattern.ReadyOn != bars[74].Date || len(item.Anchors) != 7 || len(item.Lines) != 5 {
				t.Fatalf("wrong readiness or flag geometry: %+v", item)
			}
			later, _ := AnalyzeChart("sh600519", bars, chartTestTime(bars[79].Date, 16), "")
			confirmed := classicStructure(t, later, kind)
			if item.Pattern.TriggerPrice != confirmed.Pattern.TriggerPrice || item.Pattern.InvalidationPrice != confirmed.Pattern.InvalidationPrice || item.Pattern.ReadyOn != confirmed.Pattern.ReadyOn || !reflect.DeepEqual(item.Lines[:3], confirmed.Lines[:3]) {
				t.Fatal("later prices moved the frozen channel or boundary")
			}
			bars[79].High, bars[79].Source = math.NaN(), "未来缓存"
			historical, err := AnalyzeChart("sh600519", bars, chartTestTime(bars[79].Date, 16), bars[74].Date)
			if err != nil || !reflect.DeepEqual(ready, historical) {
				t.Fatalf("future data leaked into historical flag: %v", err)
			}
		})
	}
}

func TestFlagRejectsWeakPoleOrMissingContraction(t *testing.T) {
	for _, kind := range []string{"bull-flag", "bear-flag"} {
		for _, test := range []struct {
			name   string
			change func([]domain.DailyBar)
		}{
			{"weak pole", func(bars []domain.DailyBar) {
				for i := range bars {
					bar := &bars[i]
					bar.Open, bar.Close = 100+(bar.Open-100)*.1, 100+(bar.Close-100)*.1
					bar.High, bar.Low = 100+(bar.High-100)*.1, 100+(bar.Low-100)*.1
				}
			}},
			{"expanding volume", func(bars []domain.DailyBar) {
				for i := 43; i < 74; i++ {
					bars[i].Volume = 4000
				}
			}},
			{"missing pole volume", func(bars []domain.DailyBar) { bars[35].Volume = 0 }},
			{"missing flag volume", func(bars []domain.DailyBar) { bars[65].Volume = 0 }},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				bars := classicPatternBars(kind)
				test.change(bars)
				requireNoFlag(t, bars, chartTestTime(bars[79].Date, 16), kind)
			})
		}
	}
}

func TestFlagRejectsInvalidChannelGeometry(t *testing.T) {
	for _, bullish := range []bool{true, false} {
		kind := "bear-flag"
		if bullish {
			kind = "bull-flag"
		}
		bars := classicPatternBars(kind)
		pivots := classicPivots(bars, 75)
		candidate := pivots[len(pivots)-7:]
		if _, ok := classicFlag(bars, true, 80, candidate, bullish); !ok {
			t.Fatal("baseline flag was not recognized")
		}
		for _, test := range []struct {
			name   string
			change func([]classicPivot)
		}{
			{"nonparallel channel", func(p []classicPivot) { p[6].price += 5 }},
			{"off-channel middle pivot", func(p []classicPivot) { p[3].price += 4 }},
			{"flat channel", func(p []classicPivot) { p[5].price = p[1].price; p[6].price = p[2].price }},
			{"short pole", func(p []classicPivot) { p[0].index = p[1].index - 4 }},
			{"wrong pivot order", func(p []classicPivot) { p[4].high = !p[4].high }},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				changed := append([]classicPivot(nil), candidate...)
				test.change(changed)
				if _, ok := classicFlag(bars, true, 80, changed, bullish); ok {
					t.Fatal("invalid flag geometry accepted")
				}
			})
		}
		// Preserve the parallel channel but deepen its countertrend move beyond half the pole.
		for i := 43; i < 79; i++ {
			delta := float64(i-42) * .6
			if bullish {
				delta = -delta
			}
			bars[i].Open += delta
			bars[i].Close += delta
			bars[i].High += delta
			bars[i].Low += delta
		}
		requireNoFlag(t, bars, chartTestTime(bars[79].Date, 16), kind)
	}
}

func TestFlagRequiresFrozenBoundaryNotLaterProjection(t *testing.T) {
	for _, kind := range []string{"bull-flag", "bear-flag"} {
		bars := classicPatternBars(kind)
		analysis, _ := AnalyzeChart("sh600519", bars[:75], chartTestTime(bars[74].Date, 16), "")
		pattern := classicStructure(t, analysis, kind).Pattern
		direction := 1.0
		if kind == "bear-flag" {
			direction = -1
		}
		price := pattern.TriggerPrice - direction*.04
		bars[75].Open, bars[75].Close, bars[75].High, bars[75].Low, bars[75].Volume = price, price, price+.1, price-.1, 4000
		analysis, _ = AnalyzeChart("sh600519", bars[:76], chartTestTime(bars[75].Date, 16), "")
		item := classicStructure(t, analysis, kind)
		if item.Pattern.ConfirmedOn != "" || item.Pattern.TriggerPrice != pattern.TriggerPrice {
			t.Fatalf("projected line replaced frozen confirmation: %+v", item.Pattern)
		}
		price = pattern.TriggerPrice + direction*.1
		bars[75].Open, bars[75].Close, bars[75].High, bars[75].Low = price, price, price+.1, price-.1
		analysis, _ = AnalyzeChart("sh600519", bars[:76], chartTestTime(bars[75].Date, 16), "")
		if item = classicStructure(t, analysis, kind); item.Pattern.ConfirmedOn != bars[75].Date {
			t.Fatalf("valid frozen breakout was not confirmed: %+v", item)
		}
	}
}

func TestFlagInvalidationHasPriorityAndCannotRecover(t *testing.T) {
	for _, kind := range []string{"bull-flag", "bear-flag"} {
		for _, failedAt := range []int{75, 79, 80} {
			bars := classicPatternBars(kind)
			analysis, _ := AnalyzeChart("sh600519", bars, chartTestTime(bars[79].Date, 16), "")
			pattern := classicStructure(t, analysis, kind).Pattern
			if failedAt == 80 {
				next := chartTestBars(84)
				for i := 80; i < 84; i++ {
					bar := bars[79]
					bar.Date = next[i].Date
					bars = append(bars, bar)
				}
			}
			if kind == "bull-flag" {
				bars[failedAt].Low = pattern.InvalidationPrice - 1
			} else {
				bars[failedAt].High = pattern.InvalidationPrice + 1
			}
			analysis, _ = AnalyzeChart("sh600519", bars, chartTestTime(bars[len(bars)-1].Date, 16), "")
			item := classicStructure(t, analysis, kind)
			confirmed := ""
			if failedAt == 80 {
				confirmed = bars[79].Date
			}
			if item.State != "invalidated" || item.Pattern.InvalidatedOn != bars[failedAt].Date || item.Pattern.ConfirmedOn != confirmed || item.Pattern.InvalidationPrice != pattern.InvalidationPrice || item.Pattern.ReadyOn != pattern.ReadyOn {
				t.Fatalf("flag failure was rewritten: %+v", item)
			}
		}
	}
}

func TestFlagFrozenMonitorHonorsEarlyInvalidation(t *testing.T) {
	bars := classicPatternBars("bull-flag")
	created := chartTestTime(bars[74].Date, 16)
	analysis, _ := AnalyzeChart("sh600519", bars[:75], created, "")
	plan, err := BuildTradePlan(analysis, "bull-flag", created.AddDate(0, 0, 30).Format(time.DateOnly), created)
	if err != nil {
		t.Fatal(err)
	}
	monitor, err := ConfigurePlanMonitor(plan, domain.PlanMonitor{}, true, created)
	if err != nil {
		t.Fatal(err)
	}
	bars[75].Low = plan.Structure.Plan.Invalidation - 1
	now := chartTestTime(bars[79].Date, 16)
	monitor = AdvancePlanMonitor(monitor, PlanMonitorObservation{Now: now, Session: "closing", CalendarKnown: true, TradingDate: bars[79].Date, PreviousTradingDate: bars[78].Date, Bars: bars})
	if monitor.Phase != "invalidated" || monitor.ConfirmedOn != "" || monitor.Events[len(monitor.Events)-1].DataDate != bars[75].Date || monitor.Rule.BreakoutPrice != plan.Structure.Pattern.TriggerPrice {
		t.Fatalf("frozen flag monitor missed invalidation: %+v", monitor)
	}
}
