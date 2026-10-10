package strategy

import (
	"math"
	"reflect"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func trendTimeframeBars() []domain.DailyBar {
	bars := chartTestBars(160)
	for i := range bars {
		price := 100 + float64(i)
		bars[i].Open, bars[i].Close, bars[i].High, bars[i].Low = price, price, price+1, price-1
	}
	return bars
}

func TestTimeframeComparisonSharesOriginalChartFingerprintAndWeeklyRules(t *testing.T) {
	bars := trendTimeframeBars()
	before := append([]domain.DailyBar(nil), bars...)
	at := chartTestTime(bars[len(bars)-1].Date, 16)
	base, _ := AnalyzeChart("sh600519", bars, at, "")
	result, err := AnalyzeChartTimeframes("sh600519", bars, at, "")
	if err != nil || result.BaseFingerprint != base.Fingerprint || result.Alignment != "aligned_bullish" || result.ExcludedDaily != 0 {
		t.Fatalf("bad comparison binding: %+v %v", result, err)
	}
	if result.Weekly.State != base.Weekly.State || result.Weekly.DataDate != base.Weekly.DataDate || result.Weekly.Samples != base.Weekly.Weeks || *result.Weekly.FastMA != *base.Weekly.MA5 || *result.Weekly.SlowMA != *base.Weekly.MA10 {
		t.Fatalf("weekly convention drift: %+v %+v", result.Weekly, base.Weekly)
	}
	if result.Weekly.PeriodDays != 5 || result.Daily.PeriodDays != 1 {
		t.Fatal("period day counts missing")
	}
	if !reflect.DeepEqual(before, bars) {
		t.Fatal("aggregation mutated daily bars")
	}
	after, _ := AnalyzeChart("sh600519", bars, at, "")
	if !reflect.DeepEqual(base, after) {
		t.Fatal("comparison changed the frozen chart contract")
	}
}

func TestTimeframesExcludeIntradayCandleFromBothPeriods(t *testing.T) {
	bars := chartTestBars(80)
	last := &bars[79]
	at := chartTestTime(last.Date, 10)
	before, _ := AnalyzeChartTimeframes("sh600519", bars, at, "")
	last.Close, last.High, last.Volume = 9999, 10000, 9999999
	after, err := AnalyzeChartTimeframes("sh600519", bars, at, "")
	if err != nil || !reflect.DeepEqual(before.Daily, after.Daily) || !reflect.DeepEqual(before.Weekly, after.Weekly) || after.ExcludedDaily != 1 {
		t.Fatalf("unfinished candle affected comparison: before=%+v after=%+v err=%v", before, after, err)
	}
	if after.BaseFingerprint == before.BaseFingerprint {
		t.Fatal("chart observation itself should still track intraday changes")
	}
	closed, err := AnalyzeChartTimeframes("sh600519", bars, chartTestTime(last.Date, 16), "")
	if err != nil || *closed.Daily.Close != 9999 || *closed.Weekly.Close != 9999 || closed.Weekly.Samples != after.Weekly.Samples+1 {
		t.Fatalf("closed Friday did not enter both periods: %+v %v", closed, err)
	}
}

func TestTimeframesIgnoreFuturePricesSourcesAndValidationFailures(t *testing.T) {
	bars := trendTimeframeBars()
	at := chartTestTime(bars[159].Date, 16)
	before, _ := AnalyzeChartTimeframes("sh600519", bars[:80], at, bars[79].Date)
	bars[159].High, bars[159].Source = math.NaN(), "腾讯缓存"
	after, err := AnalyzeChartTimeframes("sh600519", bars, at, bars[79].Date)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("future input leaked into historical comparison: %v", err)
	}
}

func TestTimeframesKeepInsufficientValuesMissingAndMarkCache(t *testing.T) {
	bars := chartTestBars(21)
	bars[0].Source = "腾讯缓存"
	result, err := AnalyzeChartTimeframes("sh600519", bars, chartTestTime(bars[20].Date, 10), "")
	if err != nil || result.Alignment != "insufficient" || result.Daily.SlowMA != nil || result.Daily.VolumeRatio != nil || result.Daily.RangeHigh != nil || !result.ReferenceOnly {
		t.Fatalf("missing data or cache presented as confirmation: %+v %v", result, err)
	}
}

func TestTimeframeWeeklyAggregationUsesEntireWeek(t *testing.T) {
	bars := chartTestBars(15)
	bars[5].Open, bars[5].Close, bars[5].High, bars[5].Low = 104, 104, 105, 96
	bars[9].Close, bars[9].High = 106, 107
	weeks := completedTimeframeWeeks(bars, true)
	if len(weeks) != 2 || weeks[0].Open != 104 || weeks[0].Close != 106 || weeks[0].High != 107 || weeks[0].Low != 96 || weeks[0].Volume != 5000 {
		t.Fatalf("bad weekly aggregation: %+v", weeks)
	}
	if len(completedTimeframeWeeks(bars, false)) != 1 {
		t.Fatal("unfinished week was included")
	}
}

func TestTimeframeAlignmentExplainsDirectionConflicts(t *testing.T) {
	for _, input := range []struct{ daily, weekly, expected string }{
		{"bullish", "bullish", "aligned_bullish"}, {"bearish", "bearish", "aligned_bearish"},
		{"bullish", "bearish", "conflict"}, {"bearish", "bullish", "conflict"},
		{"sideways", "bullish", "mixed"}, {"bullish", "sideways", "mixed"},
		{"insufficient", "bullish", "insufficient"},
	} {
		state, detail := timeframeAlignment(input.daily, input.weekly)
		if state != input.expected || detail == "" {
			t.Fatalf("bad alignment: %+v %s %s", input, state, detail)
		}
	}
}

func TestTimeframeShortWeekKeepsActualDaysAndUnscaledVolume(t *testing.T) {
	bars := chartTestBars(81)
	friday := bars[79].Date
	wednesday := bars[77].Date
	// Thursday and Friday have no bars; next Monday establishes the week boundary.
	bars = append(bars[:78], bars[80])
	result, err := AnalyzeChartTimeframes("sh600519", bars, chartTestTime(bars[78].Date, 16), "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Weekly.DataDate != wednesday || result.Weekly.PeriodEnd != friday || result.Weekly.PeriodDays != 3 {
		t.Fatalf("short week lost its actual sample boundary: %+v", result.Weekly)
	}
	if result.Weekly.VolumeRatio == nil || math.Abs(*result.Weekly.VolumeRatio-0.6) > 1e-9 {
		t.Fatalf("short-week total volume was rescaled: %+v", result.Weekly)
	}
	if result.Daily.DataDate != bars[78].Date || result.Daily.PeriodDays != 1 {
		t.Fatalf("new week did not remain separate: %+v", result.Daily)
	}
}
