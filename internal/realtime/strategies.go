package realtime

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/marketregime"
)

const minimumBars = 65

type strategyFunc struct {
	key  string
	name string
	fn   func(Snapshot) Component
}

func (item strategyFunc) Key() string                       { return item.key }
func (item strategyFunc) Name() string                      { return item.name }
func (item strategyFunc) Evaluate(input Snapshot) Component { return item.fn(input) }

func DefaultStrategies() []Strategy {
	return []Strategy{
		strategyFunc{key: "trend-breakout", name: "趋势突破", fn: evaluateTrendBreakout},
		strategyFunc{key: "ma-pullback", name: "均线回踩", fn: evaluatePullback},
		strategyFunc{key: "relative-momentum", name: "相对动量", fn: evaluateMomentum},
		strategyFunc{key: "price-volume", name: "量价确认", fn: evaluatePriceVolume},
		strategyFunc{key: "volatility-risk", name: "波动风险", fn: evaluateVolatilityRisk},
		strategyFunc{key: "liquidity-quality", name: "流动性质量", fn: evaluateLiquidityQuality},
		strategyFunc{key: "fund-support", name: "资金承接", fn: evaluateFundSupport},
		strategyFunc{key: "sector-rotation", name: "板块轮动", fn: evaluateSectorRotation},
		strategyFunc{key: "market-regime", name: "市场适配", fn: evaluateMarketRegime},
	}
}

type indicators struct {
	latest, previous                 domain.DailyBar
	ma5, ma20, ma60                  float64
	previousMA20                     float64
	prior20High, prior20Low          float64
	priorShortHigh                   float64
	bollingerLower, rsi14            float64
	rangeCompression                 float64
	volumeRatio                      float64
	return5, return20, return60      float64
	benchmark5, benchmark20          float64
	volatility20, drawdown20         float64
	upDayRatio20, upVolumeShare20    float64
	closePosition20, averageAmount20 float64
	benchmarkVolatility20            float64
	marketRegime                     marketregime.Regime
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func normalizedBars(input []domain.DailyBar) []domain.DailyBar {
	bars := make([]domain.DailyBar, 0, len(input))
	for _, bar := range input {
		if bar.Date != "" && bar.Open > 0 && bar.Close > 0 && bar.High > 0 && bar.Low > 0 && finite(bar.Close) {
			bars = append(bars, bar)
		}
	}
	sort.SliceStable(bars, func(left, right int) bool { return bars[left].Date < bars[right].Date })
	return bars
}

// completedBars removes the in-progress trading-day bar during a live
// session. Slow factors must compare completed sessions with completed
// sessions; the current quote remains available through Snapshot.Stock/Quote.
func completedBars(input []domain.DailyBar, now time.Time) []domain.DailyBar {
	return completedBarsWithCalendar(input, now, nil)
}

func completedBarsWithCalendar(input []domain.DailyBar, now time.Time, calendarDates []string) []domain.DailyBar {
	bars := normalizedBars(input)
	if now.IsZero() {
		return bars
	}
	session := MarketSessionAtWithCalendar(now, calendarDates)
	if session.State == MarketStateClosed {
		return bars
	}
	filtered := make([]domain.DailyBar, 0, len(bars))
	for _, bar := range bars {
		if bar.Date < session.TradingDate {
			filtered = append(filtered, bar)
		}
	}
	// Never fall back to the in-progress bar just to satisfy the warmup
	// threshold. A provider may return a short window that contains today's
	// partially formed candle; using it would let the live quote leak into
	// slow indicators and make the result differ from the point-in-time audit.
	return filtered
}

func meanBars(bars []domain.DailyBar, count int, value func(domain.DailyBar) float64) float64 {
	if count < 1 || len(bars) < count {
		return math.NaN()
	}
	total := 0.0
	for _, bar := range bars[len(bars)-count:] {
		total += value(bar)
	}
	return total / float64(count)
}

func standardDeviationBars(bars []domain.DailyBar, count int, value func(domain.DailyBar) float64) float64 {
	if count < 2 || len(bars) < count {
		return math.NaN()
	}
	mean := meanBars(bars, count, value)
	if !finite(mean) {
		return math.NaN()
	}
	total := 0.0
	for _, bar := range bars[len(bars)-count:] {
		delta := value(bar) - mean
		total += delta * delta
	}
	return math.Sqrt(total / float64(count))
}

func rsiBars(bars []domain.DailyBar, count int) float64 {
	if count < 1 || len(bars) <= count {
		return math.NaN()
	}
	start := len(bars) - count
	gain, loss := 0.0, 0.0
	for index := start; index < len(bars); index++ {
		change := bars[index].Close - bars[index-1].Close
		if change > 0 {
			gain += change
		} else {
			loss -= change
		}
	}
	if loss == 0 {
		if gain == 0 {
			return 50
		}
		return 100
	}
	strength := gain / loss
	return 100 - 100/(1+strength)
}

func rangeWidth(bars []domain.DailyBar, count int) float64 {
	if count < 2 || len(bars) < count {
		return math.NaN()
	}
	window := bars[len(bars)-count:]
	high, low := window[0].High, window[0].Low
	for _, bar := range window[1:] {
		high = math.Max(high, bar.High)
		low = math.Min(low, bar.Low)
	}
	if low <= 0 || !finite(high) || !finite(low) {
		return math.NaN()
	}
	return high/low - 1
}

func periodReturn(bars []domain.DailyBar, days int) float64 {
	if len(bars) <= days || bars[len(bars)-days-1].Close <= 0 {
		return math.NaN()
	}
	return (bars[len(bars)-1].Close/bars[len(bars)-days-1].Close - 1) * 100
}

func calculateIndicators(input Snapshot) (indicators, bool) {
	bars := completedBarsWithCalendar(input.Bars, input.Now, input.CalendarDates)
	if len(bars) < minimumBars {
		return indicators{}, false
	}
	latest := bars[len(bars)-1]
	previous := bars[len(bars)-2]
	prior := bars[len(bars)-21 : len(bars)-1]
	high, low := prior[0].High, prior[0].Low
	for _, bar := range prior[1:] {
		if bar.High > high {
			high = bar.High
		}
		if bar.Low < low {
			low = bar.Low
		}
	}
	volumeAverage := 0.0
	for _, bar := range prior {
		volumeAverage += bar.Volume
	}
	volumeRatio := math.NaN()
	if volumeAverage > 0 {
		volumeRatio = latest.Volume / (volumeAverage / float64(len(prior)))
	}
	shortWindow := 10
	shortHigh := prior[0].High
	if len(prior) > shortWindow {
		shortHigh = prior[len(prior)-shortWindow].High
		for _, bar := range prior[len(prior)-shortWindow+1:] {
			shortHigh = math.Max(shortHigh, bar.High)
		}
	}
	ma20Previous := meanBars(bars[:len(bars)-1], 20, func(bar domain.DailyBar) float64 { return bar.Close })
	std20 := standardDeviationBars(bars, 20, func(bar domain.DailyBar) float64 { return bar.Close })
	bollingerLower := math.NaN()
	ma20 := meanBars(bars, 20, func(bar domain.DailyBar) float64 { return bar.Close })
	if finite(ma20) && finite(std20) {
		bollingerLower = ma20 - 2*std20
	}
	width20 := rangeWidth(bars, 20)
	width10 := rangeWidth(bars, 10)
	compression := math.NaN()
	if finite(width20) && width20 > 0 && finite(width10) {
		compression = width10 / width20
	}
	benchmark := completedBarsWithCalendar(input.Benchmark, input.Now, input.CalendarDates)
	volatility20, drawdown20 := rollingRisk(bars, 20)
	upDayRatio20, upVolumeShare20 := priceVolumeBreadth(bars, 20)
	closePosition20 := closePosition(bars, 20)
	averageAmount20 := averageBarAmount(bars, 20)
	benchmarkVolatility20, _ := rollingRisk(benchmark, 20)
	regime := marketregime.Insufficient
	if len(benchmark) >= marketregime.MinimumBars {
		regimeDate := "9999-12-31"
		if !input.Now.IsZero() {
			regimeDate = input.Now.In(marketLocation).Format("2006-01-02")
		}
		regime = marketregime.ClassifyBefore(benchmark, regimeDate)
	}
	return indicators{
		latest: latest, previous: previous,
		ma5:          meanBars(bars, 5, func(bar domain.DailyBar) float64 { return bar.Close }),
		ma20:         ma20,
		ma60:         meanBars(bars, 60, func(bar domain.DailyBar) float64 { return bar.Close }),
		previousMA20: ma20Previous, priorShortHigh: shortHigh,
		bollingerLower: bollingerLower, rsi14: rsiBars(bars, 14), rangeCompression: compression,
		prior20High: high, prior20Low: low, volumeRatio: volumeRatio,
		return5: periodReturn(bars, 5), return20: periodReturn(bars, 20), return60: periodReturn(bars, 60),
		benchmark5: periodReturn(benchmark, 5), benchmark20: periodReturn(benchmark, 20),
		volatility20: volatility20, drawdown20: drawdown20,
		upDayRatio20: upDayRatio20, upVolumeShare20: upVolumeShare20,
		closePosition20: closePosition20, averageAmount20: averageAmount20,
		benchmarkVolatility20: benchmarkVolatility20, marketRegime: regime,
	}, true
}

func component(key, name string, score float64, reasons, warnings []string) Component {
	state := "弱"
	if score >= 14 {
		state = "触发"
	} else if score >= 8 {
		state = "观察"
	} else if score >= 4 {
		state = "中性"
	}
	return Component{Key: key, Name: name, Score: clamp(score, 0, 20), Maximum: 20, State: state, Reasons: reasons, Warnings: warnings}
}

func unavailable(key, name, warning string) Component {
	return Component{Key: key, Name: name, Maximum: 20, State: "数据不足", Warnings: []string{warning}}
}

func currentPrice(input Snapshot, fallback float64) float64 {
	if input.Quote != nil {
		var value float64
		if _, err := fmt.Sscan(input.Quote.Current, &value); err == nil && value > 0 {
			return value
		}
	}
	if input.Stock.Price > 0 && finite(input.Stock.Price) {
		return input.Stock.Price
	}
	return fallback
}

func effectiveVolumeRatio(input Snapshot, fallback float64) float64 {
	if finite(input.Stock.VolumeRatio) && input.Stock.VolumeRatio > 0 {
		return input.Stock.VolumeRatio
	}
	return fallback
}

// estimatedMinuteSpeed supplies a transparent fallback when a ranking feed
// omits its speed field. It uses the latest two valid minute points rather than
// inventing a value from the daily percentage change, so the fallback remains
// a short-horizon observation and can be disclosed in the signal warnings.
func estimatedMinuteSpeed(points []domain.MinutePoint) (float64, bool) {
	valid := make([]domain.MinutePoint, 0, len(points))
	for _, point := range points {
		if point.Price > 0 && finite(point.Price) {
			valid = append(valid, point)
		}
	}
	if len(valid) < 2 {
		return 0, false
	}
	previous, latest := valid[len(valid)-2].Price, valid[len(valid)-1].Price
	if previous <= 0 || latest <= 0 || !finite(previous) || !finite(latest) {
		return 0, false
	}
	return (latest/previous - 1) * 100, true
}

func evaluateTrendBreakout(input Snapshot) Component {
	value, ok := calculateIndicators(input)
	if !ok {
		return unavailable("trend-breakout", "趋势突破", "有效日K不足65根")
	}
	price := currentPrice(input, value.latest.Close)
	score := 0.0
	reasons := make([]string, 0, 5)
	if price > value.ma20 {
		score += 4
		reasons = append(reasons, fmt.Sprintf("现价高于MA20 %.2f", value.ma20))
	}
	if value.ma20 > value.ma60 {
		score += 4
		reasons = append(reasons, fmt.Sprintf("MA20 %.2f高于MA60 %.2f", value.ma20, value.ma60))
	}
	if price >= value.prior20High {
		score += 7
		reasons = append(reasons, fmt.Sprintf("触及/突破前20日高点 %.2f", value.prior20High))
	} else if price >= value.prior20High*.98 {
		score += 3
		reasons = append(reasons, fmt.Sprintf("距离前20日高点 %.2f 不足2%%", value.prior20High))
	}
	volumeRatio := effectiveVolumeRatio(input, value.volumeRatio)
	if finite(volumeRatio) && volumeRatio >= 1.2 {
		score += 5
		reasons = append(reasons, fmt.Sprintf("当前量比 %.2f", volumeRatio))
	}
	return component("trend-breakout", "趋势突破", score, reasons, nil)
}

func evaluatePullback(input Snapshot) Component {
	value, ok := calculateIndicators(input)
	if !ok {
		return unavailable("ma-pullback", "均线回踩", "有效日K不足65根")
	}
	price := currentPrice(input, value.latest.Close)
	score := 0.0
	reasons := make([]string, 0, 5)
	if value.ma20 > value.ma60 {
		score += 5
		reasons = append(reasons, "中期均线保持多头")
	}
	distance := math.Abs(price/value.ma20-1) * 100
	if distance <= 1.5 && price >= value.ma20 {
		score += 7
		reasons = append(reasons, fmt.Sprintf("现价在MA20 %.2f上方1.5%%内", value.ma20))
	}
	if value.latest.Low <= value.ma20*1.01 && value.latest.Close >= value.ma20 {
		score += 4
		reasons = append(reasons, "最近日K回踩MA20后收回")
	}
	volumeRatio := effectiveVolumeRatio(input, value.volumeRatio)
	if finite(volumeRatio) && volumeRatio <= 1.0 {
		score += 2
		reasons = append(reasons, fmt.Sprintf("回踩量比 %.2f，抛压未放大", volumeRatio))
	}
	if input.Stock.Percent > 0 && input.Stock.Speed >= 0 {
		score += 2
		reasons = append(reasons, "盘中价格与涨速转正")
	}
	return component("ma-pullback", "均线回踩", score, reasons, nil)
}

func evaluateMomentum(input Snapshot) Component {
	value, ok := calculateIndicators(input)
	if !ok {
		return unavailable("relative-momentum", "相对动量", "有效日K不足65根")
	}
	score := 0.0
	reasons := make([]string, 0, 5)
	if value.return5 > 0 {
		score += clamp(value.return5, 0, 5)
		reasons = append(reasons, fmt.Sprintf("5日动量 %+.2f%%", value.return5))
	}
	if value.return20 > 0 {
		score += clamp(value.return20/2, 0, 6)
		reasons = append(reasons, fmt.Sprintf("20日动量 %+.2f%%", value.return20))
	}
	if finite(value.benchmark20) && value.return20 > value.benchmark20 {
		score += 5
		reasons = append(reasons, fmt.Sprintf("20日跑赢沪深300 %+.2f个百分点", value.return20-value.benchmark20))
	}
	if finite(value.benchmark5) && value.return5 > value.benchmark5 {
		score += 2
		reasons = append(reasons, "5日相对强度为正")
	}
	if input.Stock.Speed > 0 {
		score += clamp(input.Stock.Speed*2, 0, 2)
		reasons = append(reasons, fmt.Sprintf("盘中涨速 %+.2f%%", input.Stock.Speed))
	}
	return component("relative-momentum", "相对动量", score, reasons, nil)
}

// rollingRisk returns the standard deviation of daily returns and the
// distance from the latest close to the highest close in the lookback window.
// Both values are expressed as percentages so they remain auditable in the UI.
func rollingRisk(bars []domain.DailyBar, count int) (float64, float64) {
	if count < 2 || len(bars) < count+1 {
		return math.NaN(), math.NaN()
	}
	window := bars[len(bars)-count-1:]
	returns := make([]float64, 0, count)
	peak := window[0].High
	for _, bar := range window {
		if bar.High > peak {
			peak = bar.High
		}
	}
	for index := 1; index < len(window); index++ {
		previous, current := window[index-1].Close, window[index].Close
		if previous <= 0 || current <= 0 || !finite(previous) || !finite(current) {
			return math.NaN(), math.NaN()
		}
		returns = append(returns, current/previous-1)
	}
	if len(returns) == 0 || peak <= 0 || !finite(peak) {
		return math.NaN(), math.NaN()
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
	drawdown := (window[len(window)-1].Close/peak - 1) * 100
	return math.Sqrt(variance) * 100, drawdown
}

func priceVolumeBreadth(bars []domain.DailyBar, count int) (float64, float64) {
	if count < 1 || len(bars) < count+1 {
		return math.NaN(), math.NaN()
	}
	window := bars[len(bars)-count-1:]
	upDays := 0
	upVolume, totalVolume := 0.0, 0.0
	for index := 1; index < len(window); index++ {
		previous, current := window[index-1].Close, window[index].Close
		if previous <= 0 || current <= 0 || !finite(previous) || !finite(current) {
			continue
		}
		volume := window[index].Volume
		if finite(volume) && volume > 0 {
			totalVolume += volume
			if current > previous {
				upVolume += volume
			}
		}
		if current > previous {
			upDays++
		}
	}
	upDayRatio := float64(upDays) / float64(count)
	upVolumeShare := math.NaN()
	if totalVolume > 0 {
		upVolumeShare = upVolume / totalVolume
	}
	return upDayRatio, upVolumeShare
}

func closePosition(bars []domain.DailyBar, count int) float64 {
	if count < 1 || len(bars) < count {
		return math.NaN()
	}
	window := bars[len(bars)-count:]
	high, low := window[0].High, window[0].Low
	for _, bar := range window[1:] {
		if bar.High > high {
			high = bar.High
		}
		if bar.Low < low {
			low = bar.Low
		}
	}
	if high <= low || !finite(high) || !finite(low) {
		return math.NaN()
	}
	return clamp((window[len(window)-1].Close-low)/(high-low), 0, 1)
}

func barAmount(bar domain.DailyBar) float64 {
	if finite(bar.Amount) && bar.Amount > 0 {
		return bar.Amount
	}
	// Volume units differ across the Eastmoney/Tencent history endpoints
	// (lots versus shares), so do not synthesize a yuan amount from volume.
	return math.NaN()
}

func averageBarAmount(bars []domain.DailyBar, count int) float64 {
	if count < 1 || len(bars) < count {
		return math.NaN()
	}
	total, samples := 0.0, 0
	for _, bar := range bars[len(bars)-count:] {
		amount := barAmount(bar)
		if finite(amount) && amount > 0 {
			total += amount
			samples++
		}
	}
	if samples == 0 {
		return math.NaN()
	}
	return total / float64(samples)
}

func evaluatePriceVolume(input Snapshot) Component {
	value, ok := calculateIndicators(input)
	if !ok {
		return unavailable("price-volume", "量价确认", "有效日K不足65根")
	}
	score := 0.0
	reasons := make([]string, 0, 4)
	warnings := make([]string, 0, 2)
	if finite(value.upDayRatio20) {
		switch {
		case value.upDayRatio20 >= .6:
			score += 6
		case value.upDayRatio20 >= .5:
			score += 3
		}
		reasons = append(reasons, fmt.Sprintf("20日上涨日占比 %.0f%%", value.upDayRatio20*100))
	}
	if finite(value.upVolumeShare20) {
		switch {
		case value.upVolumeShare20 >= .6:
			score += 6
		case value.upVolumeShare20 >= .5:
			score += 3
		}
		reasons = append(reasons, fmt.Sprintf("上涨日成交量占比 %.0f%%", value.upVolumeShare20*100))
	} else {
		warnings = append(warnings, "日K成交量不可用")
	}
	if finite(value.closePosition20) {
		switch {
		case value.closePosition20 >= .7:
			score += 5
		case value.closePosition20 >= .5:
			score += 2
		}
		reasons = append(reasons, fmt.Sprintf("收盘位于20日区间 %.0f%%", value.closePosition20*100))
	}
	volumeRatio := effectiveVolumeRatio(input, value.volumeRatio)
	if finite(volumeRatio) && value.latest.Close >= value.previous.Close && volumeRatio >= 1 {
		score += 3
		reasons = append(reasons, fmt.Sprintf("上涨日量比 %.2f", volumeRatio))
	}
	if finite(value.upDayRatio20) && finite(value.upVolumeShare20) && value.upDayRatio20 >= .55 && value.upVolumeShare20 < .45 {
		warnings = append(warnings, "上涨频率高但成交量未同步")
	}
	return component("price-volume", "量价确认", score, reasons, warnings)
}

func evaluateVolatilityRisk(input Snapshot) Component {
	value, ok := calculateIndicators(input)
	if !ok || !finite(value.volatility20) || !finite(value.drawdown20) {
		return unavailable("volatility-risk", "波动风险", "有效日K不足以计算20日波动")
	}
	score := 0.0
	reasons := make([]string, 0, 3)
	warnings := make([]string, 0, 2)
	switch {
	case value.volatility20 <= 1.2:
		score += 10
	case value.volatility20 <= 2:
		score += 8
	case value.volatility20 <= 3:
		score += 5
	case value.volatility20 <= 4:
		score += 2
	default:
		warnings = append(warnings, fmt.Sprintf("20日波动率 %.2f%%偏高", value.volatility20))
	}
	reasons = append(reasons, fmt.Sprintf("20日波动率 %.2f%%", value.volatility20))
	switch {
	case value.drawdown20 >= -3:
		score += 6
	case value.drawdown20 >= -6:
		score += 4
	case value.drawdown20 >= -10:
		score += 2
	default:
		warnings = append(warnings, fmt.Sprintf("较20日高点回撤 %.2f%%", value.drawdown20))
	}
	reasons = append(reasons, fmt.Sprintf("20日高点回撤 %.2f%%", value.drawdown20))
	if finite(value.benchmarkVolatility20) && value.benchmarkVolatility20 > 0 {
		ratio := value.volatility20 / value.benchmarkVolatility20
		switch {
		case ratio <= 1.2:
			score += 4
		case ratio <= 1.8:
			score += 2
		default:
			warnings = append(warnings, fmt.Sprintf("个股波动约为沪深300 %.1f 倍", ratio))
		}
		reasons = append(reasons, fmt.Sprintf("相对基准波动 %.1f 倍", ratio))
	} else {
		warnings = append(warnings, "沪深300波动率不可用")
	}
	return component("volatility-risk", "波动风险", score, reasons, warnings)
}

func evaluateLiquidityQuality(input Snapshot) Component {
	value, ok := calculateIndicators(input)
	if !ok {
		return unavailable("liquidity-quality", "流动性质量", "有效日K不足65根")
	}
	currentAmount := input.Stock.Amount
	historicalAmount := value.latest.Amount
	turnover := input.Stock.Turnover
	if !(finite(turnover) && turnover > 0) {
		turnover = value.latest.Turnover
	}
	volumeRatio := effectiveVolumeRatio(input, value.volumeRatio)
	if !(finite(currentAmount) && currentAmount > 0) && !(finite(historicalAmount) && historicalAmount > 0) && !(finite(turnover) && turnover > 0) && !(finite(volumeRatio) && volumeRatio > 0) {
		return unavailable("liquidity-quality", "流动性质量", "成交额、换手率和成交量均不可用")
	}
	score := 0.0
	reasons := make([]string, 0, 3)
	warnings := make([]string, 0, 3)
	// Compare complete daily bars with complete daily bars. The live amount is
	// cumulative intraday turnover and must not be compared to a full-day mean.
	if finite(value.averageAmount20) && value.averageAmount20 > 0 && finite(historicalAmount) && historicalAmount > 0 {
		ratio := historicalAmount / value.averageAmount20
		switch {
		case ratio >= .5 && ratio <= 3:
			score += 8
		case ratio >= .2 && ratio <= 5:
			score += 4
		default:
			warnings = append(warnings, fmt.Sprintf("当前成交额为20日均值 %.1f 倍", ratio))
		}
		reasons = append(reasons, fmt.Sprintf("最近完整日成交额为20日均值 %.1f 倍", ratio))
	} else if finite(currentAmount) && currentAmount > 0 {
		// Absolute amount is a coarse fallback when the history provider has no
		// yuan amount. It is deliberately capped and accompanied by a warning.
		switch {
		case currentAmount >= 1e9:
			score += 5
		case currentAmount >= 3e8:
			score += 4
		case currentAmount >= 1e8:
			score += 2
		default:
			score += 1
		}
		reasons = append(reasons, fmt.Sprintf("当日累计成交额 %.2f亿元", currentAmount/1e8))
		warnings = append(warnings, "成交额为盘中累计值，未与完整日均值比较")
	} else {
		warnings = append(warnings, "20日成交额均值不可用")
	}
	if finite(turnover) && turnover > 0 {
		switch {
		case turnover >= .5 && turnover <= 8:
			score += 6
		case turnover >= .2 && turnover <= 12:
			score += 3
		default:
			warnings = append(warnings, fmt.Sprintf("换手率 %.2f%%不在健康区间", turnover))
		}
		reasons = append(reasons, fmt.Sprintf("换手率 %.2f%%", turnover))
	} else {
		warnings = append(warnings, "换手率不可用")
	}
	if finite(volumeRatio) && volumeRatio > 0 {
		switch {
		case volumeRatio >= .7 && volumeRatio <= 2.5:
			score += 6
		case volumeRatio >= .4 && volumeRatio <= 4:
			score += 3
		default:
			warnings = append(warnings, fmt.Sprintf("量比 %.2f过于极端", volumeRatio))
		}
		reasons = append(reasons, fmt.Sprintf("量比 %.2f", volumeRatio))
	} else {
		warnings = append(warnings, "量比不可用")
	}
	return component("liquidity-quality", "流动性质量", score, reasons, warnings)
}

func evaluateFundSupport(input Snapshot) Component {
	stock := input.Stock
	if !finite(stock.MainNet) && !finite(stock.MainRatio) && !finite(stock.Amount) {
		return unavailable("fund-support", "资金承接", "实时资金字段不可用")
	}
	score := 0.0
	reasons := make([]string, 0, 5)
	if finite(stock.MainNet) && stock.MainNet > 0 {
		score += clamp(stock.MainNet/1e8, 0, 7)
		reasons = append(reasons, fmt.Sprintf("主力净流入 %.2f亿元", stock.MainNet/1e8))
	}
	if finite(stock.MainRatio) && stock.MainRatio > 0 {
		score += clamp(stock.MainRatio/2, 0, 5)
		reasons = append(reasons, fmt.Sprintf("主力净占比 %+.2f%%", stock.MainRatio))
	}
	if finite(stock.Amount) && stock.Amount >= 3e8 {
		score += clamp(stock.Amount/5e8, 0, 4)
		reasons = append(reasons, fmt.Sprintf("成交额 %.2f亿元", stock.Amount/1e8))
	}
	if stock.Percent > 0 && stock.Speed >= 0 {
		score += 2
		reasons = append(reasons, "资金方向与价格方向一致")
	}
	if minuteSupport(input.Minutes) {
		score += 2
		reasons = append(reasons, "最近分时价格位于均价线上方")
	}
	return component("fund-support", "资金承接", score, reasons, nil)
}

func minuteSupport(points []domain.MinutePoint) bool {
	if len(points) == 0 {
		return false
	}
	point := points[len(points)-1]
	return point.Price > 0 && point.Average > 0 && point.Price >= point.Average
}

func evaluateSectorRotation(input Snapshot) Component {
	if input.Board == nil {
		return unavailable("sector-rotation", "板块轮动", "未匹配到实时行业板块")
	}
	board := *input.Board
	score := 0.0
	reasons := make([]string, 0, 5)
	if board.Percent > 0 {
		score += clamp(board.Percent*2, 0, 6)
		reasons = append(reasons, fmt.Sprintf("板块涨幅 %+.2f%%", board.Percent))
	}
	if finite(board.MainNet) && board.MainNet > 0 {
		score += clamp(board.MainNet/5e8, 0, 6)
		reasons = append(reasons, fmt.Sprintf("板块主力净流入 %.2f亿元", board.MainNet/1e8))
	}
	total := board.RiseCount + board.FallCount + board.FlatCount
	if total > 0 {
		breadth := float64(board.RiseCount) / float64(total)
		if breadth >= .6 {
			score += clamp(breadth*7, 0, 5)
			reasons = append(reasons, fmt.Sprintf("板块上涨家数占比 %.0f%%", breadth*100))
		}
	}
	if board.LeaderCode != "" && (board.LeaderCode == input.Stock.Symbol || board.LeaderCode == trimMarket(input.Stock.Symbol)) {
		score += 3
		reasons = append(reasons, "为板块涨幅龙头")
	}
	return component("sector-rotation", "板块轮动", score, reasons, nil)
}

func evaluateMarketRegime(input Snapshot) Component {
	value, ok := calculateIndicators(input)
	if !ok || value.marketRegime == marketregime.Insufficient {
		return unavailable("market-regime", "市场适配", "沪深300历史不足60根，无法判断市场状态")
	}
	price := currentPrice(input, value.latest.Close)
	score := 0.0
	reasons := []string{fmt.Sprintf("当前市场状态：%s", value.marketRegime)}
	warnings := make([]string, 0, 2)
	relative20 := math.NaN()
	if finite(value.return20) && finite(value.benchmark20) {
		relative20 = value.return20 - value.benchmark20
	}
	volatilityRatio := math.NaN()
	if finite(value.volatility20) && finite(value.benchmarkVolatility20) && value.benchmarkVolatility20 > 0 {
		volatilityRatio = value.volatility20 / value.benchmarkVolatility20
	}

	switch value.marketRegime {
	case marketregime.Bull:
		if price > value.ma20 {
			score += 5
			reasons = append(reasons, "现价站在MA20上方")
		}
		if value.return20 > 0 {
			score += 4
			reasons = append(reasons, fmt.Sprintf("20日收益 %+.2f%%", value.return20))
		}
		if finite(relative20) && relative20 > 0 {
			score += 4
			reasons = append(reasons, fmt.Sprintf("20日跑赢基准 %+.2f个百分点", relative20))
		}
		if finite(volatilityRatio) {
			if volatilityRatio <= 1.5 {
				score += 4
			} else {
				warnings = append(warnings, "个股波动明显高于牛市基准")
			}
		}
	case marketregime.Bear:
		if finite(relative20) && relative20 > 0 {
			score += 7
			reasons = append(reasons, fmt.Sprintf("弱市中仍跑赢基准 %+.2f个百分点", relative20))
		}
		if price > value.ma20 {
			score += 4
			reasons = append(reasons, "相对弱市仍站在MA20上方")
		}
		if finite(volatilityRatio) && volatilityRatio <= 1.2 {
			score += 5
			reasons = append(reasons, "个股波动低于或接近基准")
		} else {
			warnings = append(warnings, "熊市环境下多头暴露风险较高")
		}
	case marketregime.Range:
		if finite(relative20) && relative20 > 0 {
			score += 6
			reasons = append(reasons, fmt.Sprintf("震荡市相对强度 %+.2f个百分点", relative20))
		}
		if price > value.ma20 {
			score += 5
			reasons = append(reasons, "震荡市中价格仍在MA20上方")
		}
		if value.return20 > 0 {
			score += 4
		}
		if finite(volatilityRatio) && volatilityRatio <= 1.5 {
			score += 5
		}
	case marketregime.HighVol:
		if finite(relative20) && relative20 > 0 {
			score += 6
			reasons = append(reasons, fmt.Sprintf("高波动市相对强度 %+.2f个百分点", relative20))
		}
		if price > value.ma20 {
			score += 4
			reasons = append(reasons, "高波动市中价格仍在MA20上方")
		}
		if finite(volatilityRatio) && volatilityRatio <= 1 {
			score += 7
			reasons = append(reasons, "个股波动低于市场")
		} else {
			warnings = append(warnings, "市场处于高波动状态")
		}
	default:
		warnings = append(warnings, "市场状态无法确认")
	}
	if len(reasons) == 1 {
		warnings = append(warnings, "缺少足够的个股相对强弱证据")
	}
	return component("market-regime", "市场适配", score, reasons, warnings)
}

func trimMarket(symbol string) string {
	if len(symbol) == 8 {
		return symbol[2:]
	}
	return symbol
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
