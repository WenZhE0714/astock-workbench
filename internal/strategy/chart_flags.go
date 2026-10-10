package strategy

import (
	"fmt"
	"math"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func classicFlagStructures(bars []domain.DailyBar, complete bool, end int) []domain.ChartStructure {
	seen := make(map[[2]int]bool)
	latest := make(map[bool]domain.ChartStructure)
	// Freeze the first observable channel for each pole, including its failures.
	for observedEnd := max(25, end-classicLookback); observedEnd <= end; observedEnd++ {
		pivots := classicPivots(bars, observedEnd)
		if len(pivots) < 7 {
			continue
		}
		pivots = pivots[len(pivots)-7:]
		if pivots[6].index+classicPivotRadius != observedEnd-1 || pivots[0].index < end-classicLookback {
			continue
		}
		key := [2]int{pivots[0].index, pivots[1].index}
		if seen[key] {
			continue
		}
		bullish := pivots[1].high
		structure, ok := classicFlag(bars, complete, observedEnd, pivots, bullish)
		if !ok {
			continue
		}
		seen[key] = true
		if end-observedEnd <= classicMaximumAge {
			latest[bullish] = structure
		}
	}
	result := make([]domain.ChartStructure, 0, 2)
	for _, bullish := range []bool{true, false} {
		if structure, ok := latest[bullish]; ok {
			result = append(result, structure)
		}
	}
	return result
}

func flagMeanVolume(bars []domain.DailyBar) float64 {
	if len(bars) == 0 {
		return 0
	}
	total := 0.0
	for _, bar := range bars {
		if !positiveMonitorPrice(bar.Volume) {
			return 0
		}
		total += bar.Volume
	}
	mean := total / float64(len(bars))
	if !positiveMonitorPrice(mean) {
		return 0
	}
	return mean
}

func classicFlag(bars []domain.DailyBar, complete bool, end int, pivots []classicPivot, bullish bool) (domain.ChartStructure, bool) {
	if len(pivots) != 7 {
		return domain.ChartStructure{}, false
	}
	origin, tip, last := pivots[0], pivots[1], pivots[6]
	poleDays, flagDays, ready := tip.index-origin.index, last.index-tip.index, last.index+classicPivotRadius
	if origin.index < 2 || ready < 20 || ready >= end || end-1-ready > classicMaximumAge || poleDays < 5 || poleDays > 20 || flagDays < 15 || flagDays > 40 {
		return domain.ChartStructure{}, false
	}
	for i, pivot := range pivots {
		expectedHigh := !bullish
		if i%2 == 1 {
			expectedHigh = bullish
		}
		if pivot.high != expectedHigh || (i > 0 && pivot.index-pivots[i-1].index < 3) {
			return domain.ChartStructure{}, false
		}
	}
	volatility := classicATR(bars, ready+1)
	if !positiveMonitorPrice(volatility) {
		return domain.ChartStructure{}, false
	}
	direction := 1.0
	if !bullish {
		direction = -1
	}
	poleHeight := direction * (tip.price - origin.price)
	move := direction * (bars[tip.index].Close - bars[origin.index].Close)
	travel := 0.0
	for i := origin.index + 1; i <= tip.index; i++ {
		travel += math.Abs(bars[i].Close - bars[i-1].Close)
	}
	if poleHeight < volatility*4 || move < bars[origin.index].Close*.08 || travel <= 0 || move/travel < .7 {
		return domain.ChartStructure{}, false
	}
	highs, lows := []classicPivot{}, []classicPivot{}
	for _, pivot := range pivots[1:] {
		if pivot.high {
			highs = append(highs, pivot)
		} else {
			lows = append(lows, pivot)
		}
	}
	upperSlope := (highs[2].price - highs[0].price) / float64(highs[2].index-highs[0].index)
	lowerSlope := (lows[2].price - lows[0].price) / float64(lows[2].index-lows[0].index)
	if direction*upperSlope >= 0 || direction*lowerSlope >= 0 || math.Abs(upperSlope)*float64(flagDays) < volatility*.5 || math.Abs(lowerSlope)*float64(flagDays) < volatility*.5 {
		return domain.ChartStructure{}, false
	}
	upperAt := func(index int) float64 { return highs[0].price + upperSlope*float64(index-highs[0].index) }
	lowerAt := func(index int) float64 { return lows[0].price + lowerSlope*float64(index-lows[0].index) }
	width := upperAt(tip.index) - lowerAt(tip.index)
	lastWidth := upperAt(ready) - lowerAt(ready)
	tolerance := math.Min(tip.price*.015, volatility*.5)
	if math.Min(width, lastWidth) < volatility || math.Max(width, lastWidth) > poleHeight*.4 || math.Abs(upperSlope-lowerSlope)*float64(flagDays) > width*.25 || math.Abs(highs[1].price-upperAt(highs[1].index)) > tolerance || math.Abs(lows[1].price-lowerAt(lows[1].index)) > tolerance {
		return domain.ChartStructure{}, false
	}
	flagHigh, flagLow := tip.price, tip.price
	// The readiness candle can be the breakout. Its volume and range must not
	// redefine the consolidation that preceded it.
	for i := tip.index; i < ready; i++ {
		bar := bars[i]
		if bar.High > upperAt(i)+tolerance || bar.Low < lowerAt(i)-tolerance || bar.Close > upperAt(i) || bar.Close < lowerAt(i) {
			return domain.ChartStructure{}, false
		}
		flagHigh, flagLow = math.Max(flagHigh, bar.High), math.Min(flagLow, bar.Low)
	}
	retracement := (tip.price - flagLow) / poleHeight
	if !bullish {
		retracement = (flagHigh - tip.price) / poleHeight
	}
	if retracement < .1 || retracement > .5 {
		return domain.ChartStructure{}, false
	}
	poleVolume := flagMeanVolume(bars[origin.index+1 : tip.index+1])
	flagVolume := flagMeanVolume(bars[tip.index+1 : ready])
	if poleVolume <= 0 || flagVolume <= 0 || flagVolume/poleVolume > .85 {
		return domain.ChartStructure{}, false
	}
	id, name, bias := "bull-flag", "看涨旗形", "bullish"
	trigger := math.Ceil(upperAt(ready)*100) / 100
	stop := math.Floor((flagLow-volatility*.1)*100) / 100
	if !bullish {
		id, name, bias = "bear-flag", "看跌旗形", "bearish"
		trigger = math.Floor(lowerAt(ready)*100) / 100
		stop = math.Ceil((flagHigh+volatility*.1)*100) / 100
	}
	if !positiveMonitorPrice(stop) || !positiveMonitorPrice(trigger) || direction*(trigger-stop) <= 0 {
		return domain.ChartStructure{}, false
	}
	anchors := []domain.ChartAnchor{classicAnchor(bars, origin, "旗杆起点"), classicAnchor(bars, tip, "旗杆终点")}
	highCount, lowCount := 0, 1
	if bullish {
		highCount, lowCount = 1, 0
	}
	for _, pivot := range pivots[2:] {
		label := ""
		if pivot.high {
			highCount++
			label = fmt.Sprintf("旗高%d", highCount)
		} else {
			lowCount++
			label = fmt.Sprintf("旗低%d", lowCount)
		}
		anchors = append(anchors, classicAnchor(bars, pivot, label))
	}
	structure := domain.ChartStructure{
		ID: id, Name: name, Anchors: anchors,
		Pattern: &domain.ChartPattern{Version: "classic-v1", Bias: bias, ReadyOn: bars[ready].Date, TriggerPrice: trigger, InvalidationPrice: stop},
		Lines: []domain.ChartStructureLine{
			classicLine("pole", "旗杆", "outline", anchors[0], anchors[1]),
			classicLine("flag-upper", "旗面上沿", "trend", domain.ChartAnchor{Date: bars[tip.index].Date, Price: upperAt(tip.index)}, domain.ChartAnchor{Date: bars[ready].Date, Price: upperAt(ready)}),
			classicLine("flag-lower", "旗面下沿", "trend", domain.ChartAnchor{Date: bars[tip.index].Date, Price: lowerAt(tip.index)}, domain.ChartAnchor{Date: bars[ready].Date, Price: lowerAt(ready)}),
		},
		Evidence: []string{
			fmt.Sprintf("旗杆%d根日K，收盘方向幅度 %.1f%%，方向效率 %.2f；整理%d根日K，回撤旗杆 %.1f%%", poleDays, move/bars[origin.index].Close*100, move/travel, flagDays, retracement*100),
			fmt.Sprintf("旗面上下沿各3个拐点，逆向近乎平行；通道宽度 %.2f 至 %.2f", width, lastWidth),
			fmt.Sprintf("可识别日前整理日均量为旗杆期 %.2f 倍，要求不超过0.85倍；突破另需前20日有效均量确认", flagVolume/poleVolume),
			fmt.Sprintf("按可识别日通道边界冻结确认价 %.2f，旗面极值外侧失效位 %.2f；边界不随之后的通道投影移动", trigger, stop),
		},
	}
	finishClassicStructure(&structure, bars, complete, ready, volatility, "旗形确认线")
	return structure, true
}
