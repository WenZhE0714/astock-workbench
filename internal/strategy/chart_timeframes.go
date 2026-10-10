package strategy

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

// Use the same conservative Friday boundary and initial-week exclusion as
// chartWeeklyTrend. No trading calendar or future bars are inferred.
func completedTimeframeWeeks(bars []domain.DailyBar, complete bool) []domain.DailyBar {
	if len(bars) == 0 {
		return nil
	}
	cutoff, _ := time.Parse(time.DateOnly, bars[len(bars)-1].Date)
	if !complete {
		cutoff = cutoff.AddDate(0, 0, -1)
	}
	weeks := []domain.DailyBar{}
	lastFriday := ""
	for _, bar := range bars {
		date, _ := time.Parse(time.DateOnly, bar.Date)
		weekday := int(date.Weekday())
		if weekday == 0 || weekday == 6 {
			continue
		}
		friday := date.AddDate(0, 0, 5-weekday)
		if friday.After(cutoff) {
			continue
		}
		key := friday.Format(time.DateOnly)
		if key != lastFriday {
			weeks = append(weeks, bar)
			lastFriday = key
			continue
		}
		last := &weeks[len(weeks)-1]
		last.Date, last.Close, last.Source = bar.Date, bar.Close, bar.Source
		last.High, last.Low = math.Max(last.High, bar.High), math.Min(last.Low, bar.Low)
		last.Volume += bar.Volume
	}
	if len(weeks) > 0 {
		weeks = weeks[1:]
	}
	return weeks
}

func timeframeTrend(bars []domain.DailyBar, timeframe string, fast, slow, rangeWindow, volumeWindow int) domain.ChartTimeframeTrend {
	result := domain.ChartTimeframeTrend{Timeframe: timeframe, State: "insufficient", Samples: len(bars), Required: slow,
		FastPeriod: fast, SlowPeriod: slow, RangeLookback: rangeWindow, VolumeLookback: volumeWindow, RangeState: "unavailable"}
	if len(bars) == 0 {
		return result
	}
	last := bars[len(bars)-1]
	result.DataDate, result.Close = last.Date, chartNumber(last.Close)
	if timeframe == "1w" {
		date, _ := time.Parse(time.DateOnly, last.Date)
		result.PeriodEnd = date.AddDate(0, 0, 5-int(date.Weekday())).Format(time.DateOnly)
	}
	values := closes(bars)
	if len(values) >= fast {
		result.FastMA = chartNumber(average(values[len(values)-fast:]))
	}
	if len(values) >= slow {
		result.SlowMA = chartNumber(average(values[len(values)-slow:]))
	}
	if result.FastMA != nil && result.SlowMA != nil {
		result.State = "sideways"
		if last.Close > *result.FastMA && *result.FastMA > *result.SlowMA {
			result.State = "bullish"
		}
		if last.Close < *result.FastMA && *result.FastMA < *result.SlowMA {
			result.State = "bearish"
		}
	}
	if len(bars) > rangeWindow {
		high, low := chartRangeAnchors(bars[len(bars)-rangeWindow-1 : len(bars)-1])
		result.RangeHigh, result.RangeLow, result.RangeState = chartNumber(high.Price), chartNumber(low.Price), "inside"
		if last.Close > high.Price {
			result.RangeState = "above"
		}
		if last.Close < low.Price {
			result.RangeState = "below"
		}
	}
	if len(bars) > volumeWindow {
		volume := 0.0
		for _, bar := range bars[len(bars)-volumeWindow-1 : len(bars)-1] {
			volume += bar.Volume
		}
		if volume > 0 && !math.IsInf(volume, 0) {
			result.VolumeRatio = chartNumber(last.Volume / (volume / float64(volumeWindow)))
		}
	}
	return result
}

func timeframeAlignment(daily, weekly string) (string, string) {
	switch {
	case daily == "insufficient" || weekly == "insufficient":
		return "insufficient", "完整样本不足，暂不能判断周日方向是否一致"
	case daily == "bullish" && weekly == "bullish":
		return "aligned_bullish", "日线与周线同向偏多，仍需单独核对形态、位置和成交量"
	case daily == "bearish" && weekly == "bearish":
		return "aligned_bearish", "日线与周线同向偏空，当前主要观察风险边界"
	case daily == "bullish" && weekly == "bearish":
		return "conflict", "日线走强但周线仍偏空，短期反弹尚未得到周线确认"
	case daily == "bearish" && weekly == "bullish":
		return "conflict", "周线偏多但日线转弱，短期回撤与中期方向存在冲突"
	default:
		return "mixed", "至少一个周期处于整理，尚未形成同向趋势"
	}
}

func AnalyzeChartTimeframes(symbol string, input []domain.DailyBar, asOf time.Time, through string) (domain.ChartTimeframeComparison, error) {
	base, err := AnalyzeChart(symbol, input, asOf, through)
	if err != nil {
		return domain.ChartTimeframeComparison{}, err
	}
	bars, discarded := chartBarsThrough(symbol, input, base.DataDate)
	closed := bars
	excluded := 0
	if !base.Complete {
		closed = bars[:len(bars)-1]
		excluded = 1
	}
	result := domain.ChartTimeframeComparison{Version: "timeframes-v1", Symbol: symbol, DataDate: base.DataDate, BaseFingerprint: base.Fingerprint,
		Source: base.Source, PriceBasis: base.PriceBasis, ExcludedDaily: excluded,
		Daily: timeframeTrend(closed, "1d", 20, 60, 20, 20), Weekly: timeframeTrend(completedTimeframeWeeks(bars, base.Complete), "1w", 5, 10, 10, 4),
		Checks: []domain.ChartTimeframeCheck{}, Warnings: []string{}}
	result.Alignment, result.Summary = timeframeAlignment(result.Daily.State, result.Weekly.State)
	if result.Daily.DataDate != "" {
		result.Daily.PeriodDays = 1
	}
	if result.Weekly.PeriodEnd != "" {
		end, _ := time.Parse(time.DateOnly, result.Weekly.PeriodEnd)
		start := end.AddDate(0, 0, -4).Format(time.DateOnly)
		for _, bar := range bars {
			if bar.Date >= start && bar.Date <= result.Weekly.DataDate {
				result.Weekly.PeriodDays++
			}
		}
	}
	for _, bar := range bars {
		if strings.Contains(bar.Source, "缓存") {
			result.ReferenceOnly = true
			break
		}
	}
	if result.ReferenceOnly {
		result.Warnings = append(result.Warnings, "当前含缓存日K，方向对照仅作历史参考")
	}
	if discarded > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("已排除%d根无效日K，需核查数据覆盖", discarded))
	}
	if excluded > 0 {
		result.Warnings = append(result.Warnings, "观察日尚未收盘，日线指标已排除该日盘中K线")
	}
	result.Checks = append(result.Checks, domain.ChartTimeframeCheck{Key: "alignment", State: result.Alignment, Text: result.Summary})
	volume := domain.ChartTimeframeCheck{Key: "volume", State: "pending", Text: "完整日成交量尚未达到此前20日均量的1.20倍"}
	if result.Daily.VolumeRatio == nil {
		volume.State, volume.Text = "unavailable", "完整日成交量覆盖不足，无法核验放量条件"
	} else if *result.Daily.VolumeRatio >= 1.2 {
		volume.State, volume.Text = "met", fmt.Sprintf("完整日量比%.2f倍，达到1.20倍放量观察门槛", *result.Daily.VolumeRatio)
	}
	result.Checks = append(result.Checks, volume)
	position := domain.ChartTimeframeCheck{Key: "range", State: result.Daily.RangeState}
	switch result.Daily.RangeState {
	case "above":
		position.Text = "完整日收盘已高于此前20日区间上沿"
	case "below":
		position.Text = "完整日收盘已低于此前20日区间下沿"
	case "inside":
		position.Text = "完整日收盘仍在此前20日区间内，区间突破尚未成立"
	default:
		position.Text = "区间样本不足，无法判断突破位置"
	}
	result.Checks = append(result.Checks, position)
	result.Warnings = append(result.Warnings, "周线按自然周合成，排除未结束周及首个取数周；未额外核验交易日历、停牌或缺失日线", "周成交量为周总量，未按交易日数折算，节假日短周需单独核查", "均线方向与量价条件不是买卖信号；未复权数据需核查除权除息")
	return result, nil
}
