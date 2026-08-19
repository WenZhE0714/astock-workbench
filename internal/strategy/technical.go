package strategy

import (
	"fmt"
	"math"
	"sort"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const minimumTechnicalBars = 65

type priceLevel struct {
	name  string
	value float64
}

func validBar(bar domain.DailyBar) bool {
	values := []float64{bar.Open, bar.Close, bar.High, bar.Low, bar.Volume, bar.Amount}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return bar.Date != "" && bar.Open > 0 && bar.Close > 0 && bar.High > 0 && bar.Low > 0 && bar.Volume >= 0 && bar.Amount >= 0
}

func normalizedBars(bars []domain.DailyBar) []domain.DailyBar {
	result := make([]domain.DailyBar, 0, len(bars))
	for _, bar := range bars {
		if validBar(bar) {
			result = append(result, bar)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	deduplicated := result[:0]
	for _, bar := range result {
		if len(deduplicated) > 0 && deduplicated[len(deduplicated)-1].Date == bar.Date {
			deduplicated[len(deduplicated)-1] = bar
			continue
		}
		deduplicated = append(deduplicated, bar)
	}
	return deduplicated
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func closes(bars []domain.DailyBar) []float64 {
	result := make([]float64, len(bars))
	for index := range bars {
		result[index] = bars[index].Close
	}
	return result
}

func ema(values []float64, period int) []float64 {
	result := make([]float64, len(values))
	if len(values) == 0 {
		return result
	}
	alpha := 2.0 / float64(period+1)
	result[0] = values[0]
	for index := 1; index < len(values); index++ {
		result[index] = alpha*values[index] + (1-alpha)*result[index-1]
	}
	return result
}

func macdHistogram(values []float64) float64 {
	fast := ema(values, 12)
	slow := ema(values, 26)
	difference := make([]float64, len(values))
	for index := range values {
		difference[index] = fast[index] - slow[index]
	}
	signal := ema(difference, 9)
	return 2 * (difference[len(difference)-1] - signal[len(signal)-1])
}

func rsi(values []float64, period int) float64 {
	if len(values) <= period {
		return math.NaN()
	}
	gain, loss := 0.0, 0.0
	for index := 1; index <= period; index++ {
		change := values[index] - values[index-1]
		if change > 0 {
			gain += change
		} else {
			loss -= change
		}
	}
	averageGain := gain / float64(period)
	averageLoss := loss / float64(period)
	for index := period + 1; index < len(values); index++ {
		change := values[index] - values[index-1]
		currentGain, currentLoss := 0.0, 0.0
		if change > 0 {
			currentGain = change
		} else {
			currentLoss = -change
		}
		averageGain = (averageGain*float64(period-1) + currentGain) / float64(period)
		averageLoss = (averageLoss*float64(period-1) + currentLoss) / float64(period)
	}
	if averageLoss == 0 {
		if averageGain == 0 {
			return 50
		}
		return 100
	}
	strength := averageGain / averageLoss
	return 100 - 100/(1+strength)
}

func priorRange(bars []domain.DailyBar, period int) (float64, float64) {
	start := len(bars) - period - 1
	end := len(bars) - 1
	high, low := bars[start].High, bars[start].Low
	for _, bar := range bars[start+1 : end] {
		if bar.High > high {
			high = bar.High
		}
		if bar.Low < low {
			low = bar.Low
		}
	}
	return high, low
}

func nearestLevels(price float64, levels []priceLevel) (support, resistance string) {
	var below, above *priceLevel
	for index := range levels {
		level := &levels[index]
		if level.value <= price && (below == nil || level.value > below.value) {
			below = level
		}
		if level.value >= price && (above == nil || level.value < above.value) {
			above = level
		}
	}
	if below == nil {
		support = "已跌破既有关键位，等待新结构"
	} else {
		support = fmt.Sprintf("%s %.2f", below.name, below.value)
	}
	if above == nil {
		resistance = "已突破既有关键位，等待新压力"
	} else {
		resistance = fmt.Sprintf("%s %.2f", above.name, above.value)
	}
	return support, resistance
}

func technicalStrength(score int, structureConfirmed bool) int {
	strength := 45 + int(math.Abs(float64(score)))*7
	if structureConfirmed {
		strength += 5
	}
	if strength > 90 {
		return 90
	}
	return strength
}

// AnalyzeTechnical evaluates an unadjusted daily price-volume series. The
// latest bar may be an in-progress trading-day bar; DataDate lets the UI make
// that analysis point explicit.
func AnalyzeTechnical(symbol string, input []domain.DailyBar) (domain.TechnicalSignal, error) {
	bars := normalizedBars(input)
	if len(bars) < minimumTechnicalBars {
		return domain.TechnicalSignal{}, fmt.Errorf("有效日 K 不足：需要至少 %d 根，实际 %d 根", minimumTechnicalBars, len(bars))
	}

	values := closes(bars)
	latest := bars[len(bars)-1]
	ma5 := average(values[len(values)-5:])
	ma20 := average(values[len(values)-20:])
	ma60 := average(values[len(values)-60:])
	previousMA20 := average(values[len(values)-25 : len(values)-5])
	high20, low20 := priorRange(bars, 20)
	macd := macdHistogram(values)
	rsi14 := rsi(values, 14)

	previousVolumes := make([]float64, 0, 20)
	for _, bar := range bars[len(bars)-21 : len(bars)-1] {
		previousVolumes = append(previousVolumes, bar.Volume)
	}
	averageVolume := average(previousVolumes)
	volumeRatio := math.NaN()
	if averageVolume > 0 {
		volumeRatio = latest.Volume / averageVolume
	}
	volumeConfirmed := !math.IsNaN(volumeRatio) && volumeRatio >= 1.2
	breakout := latest.Close > high20
	breakdown := latest.Close < low20

	score := 0
	evidence := make([]string, 0, 6)
	if latest.Close >= ma20 {
		score++
		evidence = append(evidence, "收盘位于MA20上方")
	} else {
		score--
		evidence = append(evidence, "收盘位于MA20下方")
	}
	if ma20 >= ma60 {
		score++
		evidence = append(evidence, "MA20高于MA60")
	} else {
		score--
		evidence = append(evidence, "MA20低于MA60")
	}
	if ma20 >= previousMA20 {
		score++
		evidence = append(evidence, "MA20近5日向上")
	} else {
		score--
		evidence = append(evidence, "MA20近5日向下")
	}
	if macd >= 0 {
		score++
		evidence = append(evidence, "MACD柱为正")
	} else {
		score--
		evidence = append(evidence, "MACD柱为负")
	}
	if rsi14 > 55 && rsi14 < 75 {
		score++
		evidence = append(evidence, "RSI处于偏强区")
	} else if rsi14 > 25 && rsi14 < 45 {
		score--
		evidence = append(evidence, "RSI处于偏弱区")
	} else if rsi14 >= 75 {
		evidence = append(evidence, "RSI处于过热区")
	} else if rsi14 <= 25 {
		evidence = append(evidence, "RSI处于超卖区")
	}
	if breakout {
		score++
		if volumeConfirmed {
			score++
			evidence = append(evidence, "放量突破20日高点")
		} else {
			evidence = append(evidence, "突破20日高点但量能未确认")
		}
	} else if breakdown {
		score--
		if volumeConfirmed {
			score--
			evidence = append(evidence, "放量跌破20日低点")
		} else {
			evidence = append(evidence, "跌破20日低点但未放量")
		}
	}

	bias, action, optionLike := "震荡", "观望", ""
	positionPlan := "保持观察；向上或向下触发前不预判方向"
	if score >= 3 {
		bias, action, optionLike = "看涨", "买入观察", "CALL-like"
		positionPlan = "等待触发后以10%–20%试错仓分批参与，不在未确认时追高"
		if breakout && volumeConfirmed && rsi14 < 75 {
			action = "买入触发"
			positionPlan = "可用10%–20%试错仓，回踩确认后再分批；失效位触发时退出试错"
		} else if rsi14 >= 75 {
			action = "持有观察"
			positionPlan = "不追高；已有仓位沿MA20管理，跌破失效位时分批降低仓位"
		}
	} else if score <= -3 {
		bias, action, optionLike = "看跌", "减仓观察", "PUT-like"
		positionPlan = "反弹不能收复MA20时分批降低仓位，新仓等待重新转强"
		if breakdown {
			action = "卖出触发"
			positionPlan = "按可卖数量优先降至轻仓或离场观察；A股T+1仓位需预留隔夜风险"
		}
	}

	levels := []priceLevel{
		{name: "MA20", value: ma20},
		{name: "MA60", value: ma60},
		{name: "20日结构低点", value: low20},
		{name: "20日结构高点", value: high20},
	}
	support, resistance := nearestLevels(latest.Close, levels)
	secondaryBreakout := fmt.Sprintf("突破20日高点 %.2f且成交量达到20日均量的1.20倍仅作二级结构确认", high20)
	buyTrigger := fmt.Sprintf("先收复MA5 %.2f；进一步收复MA60 %.2f时观察趋势修复，%s", ma5, ma60, secondaryBreakout)
	if latest.Close >= ma5 {
		buyTrigger = fmt.Sprintf("回踩MA5 %.2f不破并缩量企稳；进一步收复MA60 %.2f，%s", ma5, ma60, secondaryBreakout)
	}
	if bias == "看涨" && latest.Close >= ma20 {
		buyTrigger = fmt.Sprintf("回踩MA20 %.2f缩量企稳，或收复MA5 %.2f；%s", ma20, ma5, secondaryBreakout)
	} else if bias == "看跌" {
		buyTrigger = fmt.Sprintf("先收复MA20 %.2f与MA5 %.2f；%s", ma20, ma5, secondaryBreakout)
	}
	sellTrigger := fmt.Sprintf("收盘跌破MA20 %.2f且次日不能快速收回时优先控制风险；跌破20日低点 %.2f仅作二级结构恶化确认", ma20, low20)
	invalidation := fmt.Sprintf("收复MA5 %.2f后短线转强观察 / 跌破MA20 %.2f后转弱观察；突破 %.2f 或跌破 %.2f仅作二级结构确认", ma5, ma20, high20, low20)
	if bias == "看涨" {
		invalidation = fmt.Sprintf("收盘跌破MA20 %.2f且不能快速收回时看涨结构失效；跌破20日低点 %.2f确认结构恶化", ma20, low20)
	} else if bias == "看跌" {
		invalidation = fmt.Sprintf("收盘收复MA20 %.2f与MA5 %.2f时看跌结构失效；突破20日高点 %.2f确认结构转强", ma20, ma5, high20)
	}

	return domain.TechnicalSignal{
		Status: domain.TechnicalStatusReady, Symbol: symbol, DataSource: latest.Source, DataDate: latest.Date,
		Bias: bias, Action: action, OptionLike: optionLike,
		Strength: technicalStrength(score, (breakout || breakdown) && volumeConfirmed), Score: score,
		Price: latest.Close, MA5: ma5, MA20: ma20, MA60: ma60, MACD: macd, RSI14: rsi14,
		VolumeRatio: volumeRatio, High20: high20, Low20: low20,
		Support: support, Resistance: resistance,
		BuyTrigger: buyTrigger, SellTrigger: sellTrigger, Invalidation: invalidation,
		PositionPlan: positionPlan, Evidence: evidence,
	}, nil
}
