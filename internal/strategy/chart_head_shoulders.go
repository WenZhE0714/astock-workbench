package strategy

import (
	"fmt"
	"math"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func classicHeadShoulderStructures(bars []domain.DailyBar, complete bool, end int) []domain.ChartStructure {
	seen := make(map[[4]int]bool)
	latest := make(map[bool]domain.ChartStructure)
	// Replay pivot availability so a later, more extreme right shoulder cannot
	// replace the first valid one and erase a prior invalidation of the same core.
	for observedEnd := max(25, end-classicLookback); observedEnd <= end; observedEnd++ {
		pivots := classicPivots(bars, observedEnd)
		if len(pivots) < 5 {
			continue
		}
		pivots = pivots[len(pivots)-5:]
		if pivots[4].index+classicPivotRadius != observedEnd-1 || pivots[0].index < end-classicLookback {
			continue
		}
		key := [4]int{pivots[0].index, pivots[1].index, pivots[2].index, pivots[3].index}
		if seen[key] {
			continue
		}
		bottom := !pivots[0].high
		structure, ok := classicHeadShoulders(bars, complete, observedEnd, pivots, bottom)
		if !ok {
			continue
		}
		seen[key] = true
		if end-observedEnd <= classicMaximumAge {
			latest[bottom] = structure
		}
	}
	result := make([]domain.ChartStructure, 0, 2)
	for _, bottom := range []bool{true, false} {
		if structure, ok := latest[bottom]; ok {
			result = append(result, structure)
		}
	}
	return result
}

// Near-horizontal necklines keep the existing frozen-price monitor contract.
// The outer neckline pivot is the conservative confirmation boundary.
func classicHeadShoulders(bars []domain.DailyBar, complete bool, end int, pivots []classicPivot, bottom bool) (domain.ChartStructure, bool) {
	if len(pivots) != 5 {
		return domain.ChartStructure{}, false
	}
	left, neckLeft, head, neckRight, right := pivots[0], pivots[1], pivots[2], pivots[3], pivots[4]
	span, ready := right.index-left.index, right.index+classicPivotRadius
	if left.index < 5 || ready < 20 || ready >= end || span < 16 || span > 80 || end-1-ready > classicMaximumAge {
		return domain.ChartStructure{}, false
	}
	for i, pivot := range pivots {
		expectedHigh := !bottom
		if i%2 == 1 {
			expectedHigh = bottom
		}
		if pivot.high != expectedHigh || (i > 0 && pivot.index-pivots[i-1].index < 3) {
			return domain.ChartStructure{}, false
		}
	}
	leftSpan, rightSpan := head.index-left.index, right.index-head.index
	if leftSpan > 2*rightSpan || rightSpan > 2*leftSpan {
		return domain.ChartStructure{}, false
	}
	volatility := classicATR(bars, ready+1)
	if !positiveMonitorPrice(volatility) {
		return domain.ChartStructure{}, false
	}
	reference := (left.price + right.price) / 2
	shoulderTolerance := math.Min(reference*.03, volatility*1.5)
	neckTolerance := math.Min(reference*.02, volatility*.75)
	if math.Abs(left.price-right.price) > shoulderTolerance || math.Abs(neckLeft.price-neckRight.price) > neckTolerance {
		return domain.ChartStructure{}, false
	}
	prominence := head.price - math.Max(left.price, right.price)
	shoulderDepth := math.Min(left.price, right.price) - math.Max(neckLeft.price, neckRight.price)
	priorMove := left.price - bars[left.index-5].Close
	if bottom {
		prominence = math.Min(left.price, right.price) - head.price
		shoulderDepth = math.Min(neckLeft.price, neckRight.price) - math.Max(left.price, right.price)
		priorMove = bars[left.index-5].Close - left.price
	}
	if prominence < math.Max(reference*.025, volatility*1.5) || shoulderDepth < math.Max(reference*.01, volatility) || prominence+shoulderDepth > reference*.4 || priorMove < shoulderDepth*.6 {
		return domain.ChartStructure{}, false
	}
	id, name, bias := "head-shoulders-top", "头肩顶", "bearish"
	trigger := math.Floor(math.Min(neckLeft.price, neckRight.price)*100) / 100
	stop := math.Ceil((right.price+volatility*.1)*100) / 100
	if bottom {
		id, name, bias = "head-shoulders-bottom", "头肩底", "bullish"
		trigger = math.Ceil(math.Max(neckLeft.price, neckRight.price)*100) / 100
		stop = math.Floor((right.price-volatility*.1)*100) / 100
	}
	if !positiveMonitorPrice(stop) {
		return domain.ChartStructure{}, false
	}
	anchors := []domain.ChartAnchor{
		classicAnchor(bars, left, "左肩"), classicAnchor(bars, neckLeft, "左颈点"),
		classicAnchor(bars, head, "头部"), classicAnchor(bars, neckRight, "右颈点"), classicAnchor(bars, right, "右肩"),
	}
	structure := domain.ChartStructure{
		ID: id, Name: name, Anchors: anchors,
		Pattern: &domain.ChartPattern{Version: "classic-v1", Bias: bias, ReadyOn: bars[ready].Date, TriggerPrice: trigger, InvalidationPrice: stop},
		Lines:   []domain.ChartStructureLine{classicLine("neckline", "颈线", "trend", anchors[1], anchors[3])},
		Evidence: []string{
			fmt.Sprintf("双肩跨度%d根日K，头部两侧跨度%d/%d；双肩价差 %.2f，容差 %.2f", span, leftSpan, rightSpan, math.Abs(left.price-right.price), shoulderTolerance),
			fmt.Sprintf("头部突出 %.2f，肩颈距离 %.2f；左肩前5根日K方向幅度 %.2f", prominence, shoulderDepth, priorMove),
			fmt.Sprintf("近水平颈线两点价差 %.2f，容差 %.2f；采用外侧颈点冻结确认价 %.2f，右肩外侧失效位 %.2f", math.Abs(neckLeft.price-neckRight.price), neckTolerance, trigger, stop),
		},
	}
	for i := 1; i < len(anchors); i++ {
		structure.Lines = append(structure.Lines, classicLine(fmt.Sprintf("outline-%d", i), "头肩结构", "outline", anchors[i-1], anchors[i]))
	}
	finishClassicStructure(&structure, bars, complete, ready, volatility, "颈线确认线")
	return structure, true
}
