// Package marketregime defines the point-in-time market context contract
// shared by realtime signal evaluation and historical strategy research.
package marketregime

import (
	"math"
	"sort"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const (
	MinimumBars         = 60
	HighVolatilityLevel = 0.018
	TrendReturnLevel    = 0.05
)

// Regime is deliberately serialized as the Chinese label already used by the
// realtime API and the research UI. Keeping one type prevents callers from
// inventing subtly different state vocabularies.
type Regime string

const (
	Bull         Regime = "牛市"
	Bear         Regime = "熊市"
	Range        Regime = "震荡"
	HighVol      Regime = "高波动"
	Insufficient Regime = "数据不足"
	Unlabeled    Regime = "未标注"
)

// Ordered returns the stable display order for regime tables. The final two
// values are diagnostic states and normally only appear when data is missing.
func Ordered() []Regime {
	return []Regime{Bull, Bear, Range, HighVol, Insufficient, Unlabeled}
}

func Normalize(value string) Regime {
	value = strings.TrimSpace(value)
	for _, candidate := range Ordered() {
		if string(candidate) == value {
			return candidate
		}
	}
	if value == "" {
		return Unlabeled
	}
	return Regime(value)
}

// ClassifyBefore labels the market using only benchmark bars strictly before
// signalDate. bars must be in chronological order; callers fetching remote
// data should sort and validate them before invoking this function.
func ClassifyBefore(bars []domain.DailyBar, signalDate string) Regime {
	if len(bars) == 0 || strings.TrimSpace(signalDate) == "" {
		return Insufficient
	}
	end := sort.Search(len(bars), func(index int) bool { return bars[index].Date >= signalDate }) - 1
	if end < MinimumBars-1 {
		return Insufficient
	}
	window := bars[end-MinimumBars+1 : end+1]
	close := window[len(window)-1].Close
	base20 := window[len(window)-21].Close
	if close <= 0 || base20 <= 0 || !finite(close) || !finite(base20) {
		return Insufficient
	}
	ma60 := 0.0
	for _, bar := range window {
		if bar.Close <= 0 || !finite(bar.Close) {
			return Insufficient
		}
		ma60 += bar.Close
	}
	ma60 /= float64(len(window))
	returns := make([]float64, 0, 20)
	for index := len(window) - 20; index < len(window); index++ {
		previous := window[index-1].Close
		current := window[index].Close
		if previous <= 0 || current <= 0 || !finite(previous) || !finite(current) {
			return Insufficient
		}
		returns = append(returns, current/previous-1)
	}
	mean := 0.0
	for _, value := range returns {
		mean += value
	}
	mean /= float64(len(returns))
	variance := 0.0
	for _, value := range returns {
		variance += (value - mean) * (value - mean)
	}
	variance /= float64(len(returns))
	if math.Sqrt(variance) >= HighVolatilityLevel {
		return HighVol
	}
	trendReturn := close/base20 - 1
	if trendReturn >= TrendReturnLevel && close >= ma60 {
		return Bull
	}
	if trendReturn <= -TrendReturnLevel && close <= ma60 {
		return Bear
	}
	return Range
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
