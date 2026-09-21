package strategy

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func chartTestBars(count int) []domain.DailyBar {
	date := time.Date(2026, 1, 5, 0, 0, 0, 0, chartLocation)
	bars := make([]domain.DailyBar, 0, count)
	for len(bars) < count {
		if date.Weekday() != time.Saturday && date.Weekday() != time.Sunday {
			bars = append(bars, domain.DailyBar{Symbol: "sh600519", Source: "fixture", Date: date.Format(time.DateOnly), Open: 100, Close: 100, High: 102, Low: 98, Volume: 1000})
		}
		date = date.AddDate(0, 0, 1)
	}
	return bars
}

func chartTestTime(date string, hour int) time.Time {
	value, _ := time.ParseInLocation(time.DateOnly, date, chartLocation)
	return value.Add(time.Duration(hour) * time.Hour)
}

func chartTestLevel(t *testing.T, analysis domain.ChartAnalysis, key string) domain.ChartLevel {
	t.Helper()
	for _, level := range analysis.Levels {
		if level.Key == key {
			return level
		}
	}
	t.Fatalf("missing level %s", key)
	return domain.ChartLevel{}
}

func TestChartLevelsUsePriorSessionPivotAndActualMA(t *testing.T) {
	bars := chartTestBars(80)
	previous := &bars[len(bars)-2]
	previous.High, previous.Low, previous.Close = 106, 96, 103
	analysis, err := AnalyzeChart("sh600519", bars, chartTestTime(bars[79].Date, 16), "")
	if err != nil {
		t.Fatal(err)
	}
	pivot := chartTestLevel(t, analysis, "pivot")
	if pivot.Value == nil || math.Abs(*pivot.Value-(106.0+96+103)/3) > 1e-9 || pivot.Date != previous.Date {
		t.Fatalf("not a prior-session pivot: %+v", pivot)
	}
	ma := chartTestLevel(t, analysis, "ma20")
	if ma.Label != "MA20" || ma.Value == nil || math.Abs(*ma.Value-100.15) > 1e-9 {
		t.Fatalf("incorrect MA or label: %+v", ma)
	}
	if analysis.RangeHigh != 106 || analysis.RangeLow != 96 || !analysis.Complete || len(analysis.Fingerprint) != 64 {
		t.Fatalf("incorrect range/basis: %+v", analysis)
	}
}

func TestChartHistoryDoesNotUseFutureBars(t *testing.T) {
	bars := chartTestBars(90)
	now := chartTestTime(bars[89].Date, 16)
	baseline, err := AnalyzeChart("sh600519", bars[:65], now, bars[64].Date)
	if err != nil {
		t.Fatal(err)
	}
	bars[89].High, bars[89].Close = 10000, 9000
	actual, err := AnalyzeChart("sh600519", bars, now, bars[64].Date)
	if err != nil || actual.Fingerprint != baseline.Fingerprint || actual.DataDate != bars[64].Date {
		t.Fatalf("historical analysis changed with future data: %v, %+v", err, actual)
	}
	bars[89].High = -1
	actual, err = AnalyzeChart("sh600519", bars, now, bars[64].Date)
	if err != nil || actual.Fingerprint != baseline.Fingerprint {
		t.Fatal("future validation errors leaked into historical analysis")
	}
}

func TestChartBreakoutNeedsCompletedCandleAndVolume(t *testing.T) {
	bars := chartTestBars(80)
	last := &bars[79]
	last.Close, last.High, last.Volume = 104, 105, 1600
	for _, sample := range []struct {
		hour  int
		state string
	}{{10, "forming"}, {16, "confirmed"}} {
		analysis, err := AnalyzeChart("sh600519", bars, chartTestTime(last.Date, sample.hour), "")
		if err != nil || analysis.Structures[0].State != sample.state {
			t.Fatalf("hour=%d: %v %+v", sample.hour, err, analysis)
		}
	}
	for index := range bars {
		bars[index].Volume = 0
	}
	analysis, err := AnalyzeChart("sh600519", bars, chartTestTime(last.Date, 16), "")
	if err != nil || analysis.VolumeRatio != nil || analysis.Structures[0].State == "confirmed" {
		t.Fatalf("missing volume confirmed breakout: %v %+v", err, analysis)
	}
}

func TestChartGapRemovesFilledPartAndDisappearsAfterFill(t *testing.T) {
	bars := []domain.DailyBar{
		{Date: "2026-09-01", Open: 10, Close: 10, Low: 9, High: 11},
		{Date: "2026-09-02", Open: 14, Close: 14, Low: 13, High: 15},
		{Date: "2026-09-03", Open: 14, Close: 14, Low: 12, High: 15},
	}
	gap := chartNearestOpenGap(bars)
	if gap.Value == nil || *gap.Value != 11 || *gap.Upper != 12 || gap.Date != "2026-09-02" {
		t.Fatalf("partial up-gap fill lost: %+v", gap)
	}
	bars = append(bars, domain.DailyBar{Date: "2026-09-04", Open: 12, Close: 12, Low: 11, High: 14})
	if gap := chartNearestOpenGap(bars); gap.Value != nil {
		t.Fatalf("filled gap remains: %+v", gap)
	}
	bars = []domain.DailyBar{{Low: 15, High: 18, Close: 16}, {Low: 10, High: 12, Close: 11}, {Low: 11, High: 13, Close: 12}}
	if gap := chartNearestOpenGap(bars); gap.Value == nil || *gap.Value != 13 || *gap.Upper != 15 {
		t.Fatalf("partial down-gap fill lost: %+v", gap)
	}
	bars = append(bars, domain.DailyBar{Low: 12, High: 15, Close: 14})
	if gap := chartNearestOpenGap(bars); gap.Value != nil {
		t.Fatalf("filled down-gap remains: %+v", gap)
	}
}

func TestChartWeeklyExcludesUnfinishedWeek(t *testing.T) {
	bars := chartTestBars(80)
	if chartTestTime(bars[79].Date, 16).Weekday() != time.Friday {
		t.Fatal("fixture must end on Friday")
	}
	intraday, _ := AnalyzeChart("sh600519", bars, chartTestTime(bars[79].Date, 10), "")
	closed, _ := AnalyzeChart("sh600519", bars, chartTestTime(bars[79].Date, 16), "")
	if intraday.Weekly.DataDate >= bars[75].Date || closed.Weekly.DataDate != bars[79].Date || closed.Weekly.Weeks != intraday.Weekly.Weeks+1 {
		t.Fatalf("incomplete week included: %+v %+v", intraday.Weekly, closed.Weekly)
	}
}

func TestChartRejectsInvalidBarsAndKeepsMissingLevelsNull(t *testing.T) {
	bars := chartTestBars(21)
	now := chartTestTime(bars[20].Date, 16)
	analysis, err := AnalyzeChart("sh600519", bars, now, "")
	if err != nil || chartTestLevel(t, analysis, "ma60").Value != nil || analysis.Weekly.State != "insufficient" {
		t.Fatalf("missing data became zero: %v %+v", err, analysis)
	}
	bars[0].High = bars[0].Low - 1
	if _, err := AnalyzeChart("sh600519", bars, now, ""); err == nil {
		t.Fatal("invalid OHLC used as valid history")
	}
	if _, err := AnalyzeChart("sh600519", chartTestBars(21), now, "2099-01-01"); err == nil {
		t.Fatal("future cutoff accepted")
	}
}

func TestTradePlanUsesWorstEntryRiskAndStableIdentity(t *testing.T) {
	bars := chartTestBars(80)
	now := chartTestTime(bars[79].Date, 16)
	analysis, err := AnalyzeChart("sh600519", bars, now, "")
	if err != nil {
		t.Fatal(err)
	}
	expires := now.AddDate(0, 0, 7).Format(time.DateOnly)
	plan, err := BuildTradePlan(analysis, "range-breakout", expires, now)
	if err != nil {
		t.Fatal(err)
	}
	levels := plan.Structure.Plan
	if levels.Invalidation >= levels.EntryLow || levels.Target1 <= levels.EntryHigh || math.Abs(levels.EntryHigh-levels.Invalidation-levels.RiskPerShare) > 1e-9 || !strings.Contains(levels.RiskBasis, "T+1") {
		t.Fatalf("invalid risk math: %+v", levels)
	}
	repeated, _ := BuildTradePlan(analysis, "range-breakout", expires, now.Add(time.Minute))
	if repeated.ID != plan.ID {
		t.Fatal("same snapshot creates duplicate plans")
	}
	if _, err := BuildTradePlan(analysis, "missing", expires, now); err == nil {
		t.Fatal("unknown structure accepted")
	}
	if _, err := BuildTradePlan(analysis, "range-breakout", "2020-01-01", now); err == nil {
		t.Fatal("expired validity accepted")
	}
}
