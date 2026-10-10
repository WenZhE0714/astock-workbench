package strategy

import (
	"fmt"
	"math"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const (
	classicPivotRadius = 2
	classicLookback    = 120
	classicMaximumAge  = 30
)

type classicPivot struct {
	index int
	price float64
	high  bool
}

// Pivots require two completed candles on each side. A candle containing
// both extremes is ambiguous without intraday ordering and is excluded.
func classicPivots(bars []domain.DailyBar, end int) []classicPivot {
	start := max(classicPivotRadius, end-classicLookback)
	result := make([]classicPivot, 0)
	for index := start; index+classicPivotRadius < end; index++ {
		high, low := true, true
		for offset := -classicPivotRadius; offset <= classicPivotRadius; offset++ {
			if offset == 0 {
				continue
			}
			high = high && bars[index].High > bars[index+offset].High
			low = low && bars[index].Low < bars[index+offset].Low
		}
		if high == low {
			continue
		}
		pivot := classicPivot{index: index, price: bars[index].Low, high: high}
		if high {
			pivot.price = bars[index].High
		}
		if len(result) > 0 && result[len(result)-1].high == high {
			previous := &result[len(result)-1]
			if high && pivot.price > previous.price || !high && pivot.price < previous.price {
				*previous = pivot
			}
			continue
		}
		result = append(result, pivot)
	}
	return result
}

func classicATR(bars []domain.DailyBar, end int) float64 {
	values := closes(bars[:end])
	highs, lows := make([]float64, end), make([]float64, end)
	for i := 0; i < end; i++ {
		highs[i], lows[i] = bars[i].High, bars[i].Low
	}
	return atr(values, highs, lows, 14)
}

func classicVolumeRatio(bars []domain.DailyBar, index int) *float64 {
	if index < 20 || !positiveMonitorPrice(bars[index].Volume) {
		return nil
	}
	total := 0.0
	for _, bar := range bars[index-20 : index] {
		if !positiveMonitorPrice(bar.Volume) {
			return nil
		}
		total += bar.Volume
	}
	return chartNumber(bars[index].Volume / (total / 20))
}

func classicAnchor(bars []domain.DailyBar, pivot classicPivot, label string) domain.ChartAnchor {
	return domain.ChartAnchor{Date: bars[pivot.index].Date, Price: pivot.price, Label: label}
}

func classicLine(key, label, role string, from, to domain.ChartAnchor) domain.ChartStructureLine {
	return domain.ChartStructureLine{Key: key, Label: label, Role: role, From: from, To: to}
}

func classicChartStructures(bars []domain.DailyBar, complete bool) []domain.ChartStructure {
	end := len(bars)
	if !complete {
		end--
	}
	if end < 25 {
		return nil
	}
	pivots := classicPivots(bars, end)
	result := make([]domain.ChartStructure, 0, 6)
	for _, bottom := range []bool{true, false} {
		for i := len(pivots) - 3; i >= 0; i-- {
			left, middle, right := pivots[i], pivots[i+1], pivots[i+2]
			if left.high == bottom || right.high == bottom || middle.high != bottom {
				continue
			}
			if structure, ok := classicDouble(bars, complete, end, left, middle, right, bottom); ok {
				result = append(result, structure)
				break
			}
		}
	}
	for _, ascending := range []bool{true, false} {
		for i := len(pivots) - 6; i >= 0; i-- {
			if structure, ok := classicTriangle(bars, complete, end, pivots[i:i+6], ascending); ok {
				result = append(result, structure)
				break
			}
		}
	}
	result = append(result, classicHeadShoulderStructures(bars, complete, end)...)
	return result
}

func classicDouble(bars []domain.DailyBar, complete bool, end int, left, middle, right classicPivot, bottom bool) (domain.ChartStructure, bool) {
	span, ready := right.index-left.index, right.index+classicPivotRadius
	if left.index < 5 || ready < 20 || span < 8 || span > 60 || middle.index-left.index < 3 || right.index-middle.index < 3 || end-1-ready > classicMaximumAge {
		return domain.ChartStructure{}, false
	}
	volatility := classicATR(bars, ready+1)
	if !positiveMonitorPrice(volatility) {
		return domain.ChartStructure{}, false
	}
	reference := (left.price + right.price) / 2
	tolerance := math.Max(.01, math.Min(reference*.02, volatility*.75))
	if math.Abs(left.price-right.price) > tolerance {
		return domain.ChartStructure{}, false
	}
	height := math.Min(left.price, right.price) - middle.price
	priorMove := left.price - bars[left.index-5].Close
	if bottom {
		height = middle.price - math.Max(left.price, right.price)
		priorMove = bars[left.index-5].Close - left.price
	}
	if height < math.Max(volatility*1.5, reference*.02) || height > reference*.4 || priorMove < height*.6 {
		return domain.ChartStructure{}, false
	}
	id, name, bias := "double-top", "双顶", "bearish"
	leftLabel, rightLabel := "左顶", "右顶"
	trigger := math.Floor(middle.price*100) / 100
	stop := math.Ceil((math.Max(left.price, right.price)+volatility*.1)*100) / 100
	if bottom {
		id, name, bias = "double-bottom", "双底", "bullish"
		leftLabel, rightLabel = "左底", "右底"
		trigger = math.Ceil(middle.price*100) / 100
		stop = math.Floor((math.Min(left.price, right.price)-volatility*.1)*100) / 100
	}
	anchors := []domain.ChartAnchor{classicAnchor(bars, left, leftLabel), classicAnchor(bars, middle, "颈线拐点"), classicAnchor(bars, right, rightLabel)}
	structure := domain.ChartStructure{
		ID: id, Name: name, Anchors: anchors,
		Pattern: &domain.ChartPattern{Version: "classic-v1", Bias: bias, ReadyOn: bars[ready].Date, TriggerPrice: trigger, InvalidationPrice: stop},
		Lines: []domain.ChartStructureLine{
			classicLine("left-leg", "左侧结构", "outline", anchors[0], anchors[1]),
			classicLine("right-leg", "右侧结构", "outline", anchors[1], anchors[2]),
		},
		Evidence: []string{fmt.Sprintf("两次极值相隔%d根日K，价差 %.2f，容差 %.2f；颈线高度 %.2f", span, math.Abs(left.price-right.price), tolerance, height)},
	}
	finishClassicStructure(&structure, bars, complete, ready, volatility, "颈线")
	return structure, true
}

func classicTriangle(bars []domain.DailyBar, complete bool, end int, pivots []classicPivot, ascending bool) (domain.ChartStructure, bool) {
	first, last := pivots[0].index, pivots[len(pivots)-1].index
	ready := last + classicPivotRadius
	if ready < 20 || last-first < 15 || last-first > 80 || end-1-ready > classicMaximumAge {
		return domain.ChartStructure{}, false
	}
	highs, lows := make([]classicPivot, 0, 3), make([]classicPivot, 0, 3)
	for _, pivot := range pivots {
		if pivot.high {
			highs = append(highs, pivot)
		} else {
			lows = append(lows, pivot)
		}
	}
	if len(highs) != 3 || len(lows) != 3 {
		return domain.ChartStructure{}, false
	}
	flat, slope := highs, lows
	if !ascending {
		flat, slope = lows, highs
	}
	volatility := classicATR(bars, ready+1)
	if !positiveMonitorPrice(volatility) {
		return domain.ChartStructure{}, false
	}
	minFlat := math.Min(flat[0].price, math.Min(flat[1].price, flat[2].price))
	maxFlat := math.Max(flat[0].price, math.Max(flat[1].price, flat[2].price))
	level := maxFlat
	if !ascending {
		level = minFlat
	}
	tolerance := math.Max(.01, math.Min(level*.015, volatility*.75))
	if maxFlat-minFlat > tolerance {
		return domain.ChartStructure{}, false
	}
	// A boundary already broken before the last pivot became observable is no
	// longer a pending triangle setup.
	for _, bar := range bars[first:ready] {
		if ascending && bar.Close > maxFlat+tolerance || !ascending && bar.Close < minFlat-tolerance {
			return domain.ChartStructure{}, false
		}
	}
	movement := slope[2].price - slope[0].price
	if !ascending {
		movement = -movement
	}
	if movement < math.Max(level*.01, volatility*.75) {
		return domain.ChartStructure{}, false
	}
	for i := 1; i < len(slope); i++ {
		if slope[i].index-slope[i-1].index < 4 || (ascending && slope[i].price <= slope[i-1].price) || (!ascending && slope[i].price >= slope[i-1].price) {
			return domain.ChartStructure{}, false
		}
	}
	projected := slope[0].price + (slope[2].price-slope[0].price)*float64(slope[1].index-slope[0].index)/float64(slope[2].index-slope[0].index)
	if math.Abs(projected-slope[1].price) > tolerance {
		return domain.ChartStructure{}, false
	}
	initialWidth, finalWidth := math.Abs(level-slope[0].price), math.Abs(level-slope[2].price)
	if initialWidth < math.Max(2*volatility, level*.025) || finalWidth < volatility*.5 || finalWidth > initialWidth*.8 || (ascending && slope[2].price >= minFlat) || (!ascending && slope[2].price <= maxFlat) {
		return domain.ChartStructure{}, false
	}
	id, name, bias, boundary := "ascending-triangle", "上升三角形", "bullish", "水平压力"
	trigger := math.Ceil(level*100) / 100
	stop := math.Floor((slope[2].price-volatility*.1)*100) / 100
	if !ascending {
		id, name, bias, boundary = "descending-triangle", "下降三角形", "bearish", "水平支撑"
		trigger = math.Floor(level*100) / 100
		stop = math.Ceil((slope[2].price+volatility*.1)*100) / 100
	}
	anchors := make([]domain.ChartAnchor, 0, len(pivots))
	highCount, lowCount := 0, 0
	for _, pivot := range pivots {
		label := ""
		if pivot.high {
			highCount++
			label = fmt.Sprintf("高%d", highCount)
		} else {
			lowCount++
			label = fmt.Sprintf("低%d", lowCount)
		}
		anchors = append(anchors, classicAnchor(bars, pivot, label))
	}
	structure := domain.ChartStructure{
		ID: id, Name: name, Anchors: anchors,
		Pattern: &domain.ChartPattern{Version: "classic-v1", Bias: bias, ReadyOn: bars[ready].Date, TriggerPrice: trigger, InvalidationPrice: stop},
		Lines: []domain.ChartStructureLine{
			classicLine("trend-boundary", "收敛趋势线", "trend", classicAnchor(bars, slope[0], "趋势起点"), classicAnchor(bars, slope[2], "趋势终点")),
		},
		Evidence: []string{fmt.Sprintf("3次水平边界测试，价差 %.2f；另一侧拐点逐次%s，区间收窄至 %.0f%%", maxFlat-minFlat, map[bool]string{true: "抬高", false: "降低"}[ascending], finalWidth/initialWidth*100)},
	}
	finishClassicStructure(&structure, bars, complete, ready, volatility, boundary)
	return structure, true
}

func finishClassicStructure(structure *domain.ChartStructure, bars []domain.DailyBar, complete bool, ready int, volatility float64, boundary string) {
	pattern := structure.Pattern
	bullish := pattern.Bias == "bullish"
	structure.State = "watching"
	confirmationIndex := -1
	for index := ready; index < len(bars); index++ {
		bar := bars[index]
		if bullish && bar.Low <= pattern.InvalidationPrice || !bullish && bar.High >= pattern.InvalidationPrice {
			structure.State, pattern.InvalidatedOn = "invalidated", bar.Date
			structure.Anchors = append(structure.Anchors, domain.ChartAnchor{Date: bar.Date, Price: pattern.InvalidationPrice, Label: "失效"})
			break
		}
		if !complete && index == len(bars)-1 {
			continue
		}
		crossed := bullish && bar.Close > pattern.TriggerPrice || !bullish && bar.Close < pattern.TriggerPrice
		if pattern.ConfirmedOn == "" && crossed {
			ratio := classicVolumeRatio(bars, index)
			if ratio != nil && *ratio >= 1.2 {
				structure.State, pattern.ConfirmedOn, confirmationIndex = "confirmed", bar.Date, index
				structure.Anchors = append(structure.Anchors, domain.ChartAnchor{Date: bar.Date, Price: bar.Close, Label: "收盘确认"})
			}
		}
	}
	latest := bars[len(bars)-1]
	if structure.State == "watching" && (bullish && latest.Close > pattern.TriggerPrice || !bullish && latest.Close < pattern.TriggerPrice) {
		structure.State = "forming"
	}
	from, to := structure.Anchors[0].Date, latest.Date
	structure.Lines = append(structure.Lines,
		classicLine("trigger", boundary, "trigger", domain.ChartAnchor{Date: from, Price: pattern.TriggerPrice}, domain.ChartAnchor{Date: to, Price: pattern.TriggerPrice}),
		classicLine("invalidation", "结构失效", "invalidation", domain.ChartAnchor{Date: pattern.ReadyOn, Price: pattern.InvalidationPrice}, domain.ChartAnchor{Date: to, Price: pattern.InvalidationPrice}),
	)
	structure.Evidence = append(structure.Evidence, fmt.Sprintf("拐点经后续%d根完整日K确认；最早可识别日 %s", classicPivotRadius, pattern.ReadyOn))
	if confirmationIndex >= 0 {
		structure.Evidence = append(structure.Evidence, fmt.Sprintf("%s 收盘突破%s，成交量为此前20日均量 %.2f 倍", pattern.ConfirmedOn, boundary, *classicVolumeRatio(bars, confirmationIndex)))
	} else {
		structure.Evidence = append(structure.Evidence, "尚无完整日K与前20日有效成交量同时确认突破（量比至少1.20倍）")
	}
	if pattern.InvalidatedOn != "" {
		structure.Evidence = append(structure.Evidence, pattern.InvalidatedOn+" 触及结构失效边界")
	}
	if bullish {
		confirmation := fmt.Sprintf("自%s起，完整日K收盘高于冻结%s %.2f 且量比至少1.20倍；先触及 %.2f 则形态失效", pattern.ReadyOn, boundary, pattern.TriggerPrice, pattern.InvalidationPrice)
		structure.Plan = chartPlanLevels(pattern.TriggerPrice, pattern.TriggerPrice+volatility*.25, pattern.InvalidationPrice, confirmation)
	} else {
		structure.Evidence = append(structure.Evidence, "看跌结构仅作风险观察")
	}
}
