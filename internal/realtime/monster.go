package realtime

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/marketregime"
)

const (
	monsterMinimumScore        = 58.0
	monsterHighConfidence      = 72.0
	monsterStrongDayThreshold  = 3.0
	monsterNearBreakout        = -1.5
	monsterNearLimit           = 1.5
	monsterExtremeTurnover     = 15.0
	monsterExtremeVolumeRatio  = 5.0
	monsterOpeningRangeMinutes = 15
	monsterMeaningfulGap       = 0.5
	monsterOverheatedGap       = 4.0
	monsterBreakoutTouch       = 0.2
	monsterBreakoutFailure     = 0.5
	monsterQuoteMaxAge         = 10 * time.Minute
	monsterQuoteFutureSlack    = 2 * time.Minute
)

var monsterStageLabels = map[MonsterStage]string{
	MonsterStageDormant:      "结构蓄势",
	MonsterStageStarting:     "启动确认",
	MonsterStageAccelerating: "强势加速",
	MonsterStageDiverging:    "高位分歧",
	MonsterStageEbbing:       "趋势退潮",
	MonsterStageInsufficient: "数据不足",
}

// EvaluateMonsterRadar evaluates the independent high-volatility radar for a
// snapshot. It is useful to callers that want the radar without creating a
// full Signal; Scanner.evaluate uses the same function after calculating its
// normal nine-factor indicators.
func EvaluateMonsterRadar(input Snapshot) MonsterRadar {
	value, ok := calculateIndicators(input)
	return classifyMonsterRadar(input, value, ok)
}

// classifyMonsterRadar deliberately receives the already calculated indicator
// set so the scanner can keep all slow features on completed daily bars. The
// current quote is used only for the fast, point-in-time part of the radar.
func classifyMonsterRadar(input Snapshot, value indicators, indicatorsOK bool) MonsterRadar {
	radar := MonsterRadar{
		Stage:      MonsterStageInsufficient,
		Label:      monsterStageLabels[MonsterStageInsufficient],
		Confidence: "不可用",
		Reasons:    make([]string, 0, 8),
		Risks:      make([]string, 0, 8),
		Warnings:   make([]string, 0, 8),
	}
	if !indicatorsOK {
		radar.Warnings = append(radar.Warnings, "有效日K不足65根，无法识别抓妖阶段")
		return radar
	}

	price := currentPrice(input, value.latest.Close)
	if !(finite(price) && price > 0) {
		radar.Warnings = append(radar.Warnings, "当前价格不可用，无法识别抓妖阶段")
		return radar
	}
	var quoteWarning string
	radar.QuoteFresh, radar.QuoteAgeSeconds, quoteWarning = monsterQuoteFreshness(input)
	if !radar.QuoteFresh {
		if quoteWarning == "" {
			quoteWarning = "实时报价缺失或日期未对齐，雷达仅保留历史结构"
		}
		radar.Warnings = append(radar.Warnings, quoteWarning)
	}

	radar.BreakoutLevelReady = finite(value.prior20High) && value.prior20High > 0
	if radar.BreakoutLevelReady {
		radar.BreakoutDistance = (price/value.prior20High - 1) * 100
		radar.TriggerPrice = value.prior20High
	}
	if finite(value.ma20) && value.ma20 > 0 {
		invalidation := value.ma20
		if finite(value.prior20Low) && value.prior20Low > invalidation {
			invalidation = value.prior20Low
		}
		radar.InvalidationPrice = invalidation
	}

	completed := completedBarsWithCalendar(input.Bars, input.Now, input.CalendarDates)
	radar.RecentStrongDays, radar.ConsecutiveStrongDays = recentMonsterStrongDaysDetailed(completed, 10)
	// A live quote is not part of completed daily history. Count it as the
	// current strong day only when its date is not already represented by the
	// latest completed bar.
	if radar.QuoteFresh && monsterQuoteIsLive(input, completed) && finite(input.Stock.Percent) && input.Stock.Percent >= monsterStrongDayThreshold {
		radar.RecentStrongDays++
		radar.ConsecutiveStrongDays++
	}
	minutePoints := monsterCurrentSessionMinutes(input)
	radar.MinuteMomentum, radar.VWAPSupport, radar.MinuteDataReady = monsterMinuteEvidence(minutePoints)

	limitUp, limitDown := monsterLimitPrices(input)
	if limitUp > 0 {
		radar.LimitUpDistance = math.Max(0, (limitUp-price)/price*100)
	}
	if limitUp > 0 && limitDown > 0 && limitUp > limitDown {
		radar.LimitBoundaryReady = true
	} else {
		radar.Warnings = append(radar.Warnings, "涨跌停边界缺失，不能确认高置信度加速")
	}

	currentPercent := input.Stock.Percent
	if !finite(currentPercent) {
		currentPercent = 0
	}
	speed := input.Stock.Speed
	if !finite(speed) {
		speed = 0
	}
	volumeRatio := effectiveVolumeRatio(input, value.volumeRatio)
	turnover := input.Stock.Turnover
	if !(finite(turnover) && turnover > 0) {
		turnover = value.latest.Turnover
	}
	intradayHigh := input.Stock.High
	if !(finite(intradayHigh) && intradayHigh > 0) && input.Quote != nil {
		intradayHigh = parseQuoteNumber(input.Quote.High)
	}
	if finite(intradayHigh) && intradayHigh > price {
		radar.IntradayPullback = (intradayHigh/price - 1) * 100
	}
	fast := computeMonsterFastEvidence(input, value, price, intradayHigh, volumeRatio, radar.MinuteDataReady, radar.VWAPSupport)
	if fast.openingGapReady {
		radar.OpeningGapReady = true
		radar.OpeningGapPercent = math.Round(fast.openingGap*100) / 100
		radar.OpeningGapHeld = fast.openingGapHeld
		radar.OpeningGapRecovered = fast.openingGapRecovered
	}
	radar.OpeningRangeReady = fast.openingRangeReady
	radar.OpeningRangeBreakout = fast.openingRangeBreakout
	if fast.intradayPositionReady {
		radar.IntradayPosition = math.Round(fast.intradayPosition*100) / 100
		radar.IntradayPositionReady = true
	}
	radar.BreakoutFailure = fast.breakoutFailure
	radar.VolumePriceDivergence = fast.volumePriceDivergence

	breadth, boardPositive, boardLeader := monsterBoardEvidence(input)
	if input.Board == nil {
		radar.Warnings = append(radar.Warnings, "未匹配到实时行业板块，板块共振无法确认")
	} else if !boardPositive {
		radar.Risks = append(radar.Risks, "所属行业板块承接偏弱")
	}
	if !finite(volumeRatio) || volumeRatio <= 0 {
		radar.Warnings = append(radar.Warnings, "量比不可用，量价确认降级")
	}
	if !finite(turnover) || turnover <= 0 {
		radar.Warnings = append(radar.Warnings, "换手率不可用，流动性确认降级")
	}
	if value.marketRegime == marketregime.Insufficient {
		radar.Warnings = append(radar.Warnings, "沪深300市场状态不可用，不能授予观察资格")
	}

	score := 0.0
	add := func(points float64, reason string) {
		if points > 0 {
			score += points
		}
		if strings.TrimSpace(reason) != "" {
			radar.Reasons = append(radar.Reasons, reason)
		}
	}
	penalize := func(points float64, risk string) {
		if points > 0 {
			score -= points
		}
		if strings.TrimSpace(risk) != "" {
			radar.Risks = append(radar.Risks, risk)
		}
	}

	trend := finite(value.ma20) && finite(value.ma60) && price >= value.ma20 && value.ma20 > value.ma60
	if price >= value.ma20 && finite(value.ma20) {
		add(9, fmt.Sprintf("价格位于MA20 %.2f上方", value.ma20))
	}
	if finite(value.ma20) && finite(value.ma60) && value.ma20 > value.ma60 {
		add(8, fmt.Sprintf("MA20 %.2f高于MA60 %.2f", value.ma20, value.ma60))
	}
	if radar.BreakoutLevelReady {
		switch {
		case radar.BreakoutDistance >= 0:
			add(16, fmt.Sprintf("现价突破前20日高点 %.2f（%+.2f%%）", value.prior20High, radar.BreakoutDistance))
		case radar.BreakoutDistance >= monsterNearBreakout:
			add(8, fmt.Sprintf("现价接近前20日高点 %.2f（%+.2f%%）", value.prior20High, radar.BreakoutDistance))
		}
	}
	if finite(value.return20) {
		switch {
		case value.return20 >= 8 && value.return20 <= 35:
			add(9, fmt.Sprintf("20日收益 %+.2f%%，动量处于可跟踪区间", value.return20))
		case value.return20 > 35:
			add(4, fmt.Sprintf("20日收益 %+.2f%%，趋势强但追高风险上升", value.return20))
		}
	}
	if finite(value.benchmark20) && finite(value.return20) && value.return20 > value.benchmark20 {
		add(7, fmt.Sprintf("20日跑赢沪深300 %+.2f个百分点", value.return20-value.benchmark20))
	}
	if currentPercent >= 5 {
		add(9, fmt.Sprintf("当前涨幅 %+.2f%%，短线动能明显", currentPercent))
	} else if currentPercent >= 2 {
		add(5, fmt.Sprintf("当前涨幅 %+.2f%%，动能开始释放", currentPercent))
	}
	if speed > 0 {
		add(clamp(speed*2, 1, 5), fmt.Sprintf("盘中涨速 %+.2f%%", speed))
	}
	if radar.RecentStrongDays > 0 {
		add(clamp(float64(radar.RecentStrongDays)*4, 4, 12), fmt.Sprintf("近10个完整交易日出现%d个强势日", radar.RecentStrongDays))
	}
	if radar.ConsecutiveStrongDays >= 2 {
		add(6, fmt.Sprintf("连续%d个交易日保持强势", radar.ConsecutiveStrongDays))
	}
	if radar.MinuteDataReady {
		if radar.MinuteMomentum > .5 {
			add(4, fmt.Sprintf("分时价格较首个有效点上涨 %+.2f%%", radar.MinuteMomentum))
		}
		if radar.VWAPSupport {
			add(3, "最新分时价格位于均价线上方")
		}
	} else {
		radar.Warnings = append(radar.Warnings, "分时数据未补充，实时斜率未计入雷达")
	}

	healthyVolume := finite(volumeRatio) && volumeRatio >= 1.2 && volumeRatio <= 4.5
	if healthyVolume {
		add(10, fmt.Sprintf("量比 %.2f，放量但未达极端", volumeRatio))
	} else if finite(volumeRatio) && volumeRatio >= 1 {
		add(4, fmt.Sprintf("量比 %.2f，量能仅部分确认", volumeRatio))
	}
	if finite(turnover) {
		switch {
		case turnover >= .8 && turnover <= 10:
			add(5, fmt.Sprintf("换手率 %.2f%%处于可承接区间", turnover))
		case turnover >= .3 && turnover <= 14:
			add(2, fmt.Sprintf("换手率 %.2f%%", turnover))
		}
	}
	if finite(input.Stock.Amount) && input.Stock.Amount >= 3e8 {
		add(4, fmt.Sprintf("当日累计成交额 %.2f亿元", input.Stock.Amount/1e8))
	}
	if boardPositive {
		add(6, "所属行业板块涨幅与资金方向支持")
	}
	if finite(breadth) && breadth >= .6 {
		add(4, fmt.Sprintf("板块上涨家数占比 %.0f%%", breadth*100))
	}
	if boardLeader {
		add(3, "为所属板块涨幅龙头")
	}
	if finite(value.rangeCompression) && value.rangeCompression <= .65 {
		add(6, fmt.Sprintf("10日/20日波动区间 %.0f%%，存在蓄势结构", value.rangeCompression*100))
	}
	if radar.LimitBoundaryReady && radar.LimitUpDistance <= monsterNearLimit {
		add(5, fmt.Sprintf("距涨停约 %.2f%%", radar.LimitUpDistance))
	}
	if fast.openingGapReady {
		switch {
		case fast.openingGap >= monsterMeaningfulGap && fast.openingGap <= 3 && fast.openingGapHeld:
			add(5, fmt.Sprintf("开盘跳空 %+0.2f%% 且守住开盘价，承接有效", fast.openingGap))
		case fast.openingGap > 3 && fast.openingGap < monsterOverheatedGap && fast.openingGapHeld:
			add(2, fmt.Sprintf("开盘高开 %+0.2f%%，当前仍守住开盘价", fast.openingGap))
		case fast.openingGap >= monsterOverheatedGap && !fast.openingGapHeld:
			penalize(7, fmt.Sprintf("高开 %+0.2f%% 后跌破开盘价，开盘承接失败", fast.openingGap))
		case fast.openingGap <= -monsterMeaningfulGap && fast.openingGapRecovered:
			add(3, fmt.Sprintf("低开 %+0.2f%% 后收复开盘价，反包承接增强", fast.openingGap))
		case fast.openingGap <= -2:
			penalize(6, fmt.Sprintf("低开 %+0.2f%%，开盘风险偏高", fast.openingGap))
		}
	}
	if fast.openingRangeReady {
		if fast.openingRangeBreakout {
			add(6, "突破开盘15分钟区间上沿，短线主动性增强")
		} else if fast.openingGapHeld {
			add(2, "开盘15分钟区间内价格保持承接")
		}
	}
	if fast.intradayPositionReady {
		switch {
		case fast.intradayPosition >= 75:
			add(4, fmt.Sprintf("现价位于日内区间上方 %.0f%%，收盘位置强", fast.intradayPosition))
		case fast.intradayPosition <= 35 && finite(volumeRatio) && volumeRatio >= 1.5:
			penalize(7, fmt.Sprintf("现价仅处日内区间 %.0f%%，放量但承接偏弱", fast.intradayPosition))
		}
	}
	if fast.breakoutFailure {
		penalize(14, "盘中触及前20日高点后跌回其下，突破失败")
	}
	if fast.volumePriceDivergence {
		penalize(8, "量能放大但价格位于日内弱势区，量价出现背离")
	}

	overheated := false
	if finite(volumeRatio) && volumeRatio > monsterExtremeVolumeRatio {
		overheated = true
		penalize(14, fmt.Sprintf("量比 %.2f极端放大，容易出现冲高回落", volumeRatio))
	}
	if finite(turnover) && turnover > monsterExtremeTurnover {
		overheated = true
		penalize(12, fmt.Sprintf("换手率 %.2f%%极端，筹码分歧扩大", turnover))
	}
	if finite(value.rsi14) && value.rsi14 > 82 {
		overheated = true
		penalize(8, fmt.Sprintf("RSI14 %.1f进入过热区", value.rsi14))
	}
	if radar.IntradayPullback >= 3 {
		penalize(8, fmt.Sprintf("较日内高点回落 %.2f%%，承接转弱", radar.IntradayPullback))
	}
	if radar.MinuteDataReady && radar.MinuteMomentum < -1 {
		penalize(6, fmt.Sprintf("分时价格较首个有效点下跌 %.2f%%", math.Abs(radar.MinuteMomentum)))
	}
	if radar.MinuteDataReady && !radar.VWAPSupport && radar.MinuteMomentum < 0 {
		penalize(4, "分时价格跌破均价线，短线承接转弱")
	}
	if finite(value.drawdown20) && value.drawdown20 < -10 {
		penalize(10, fmt.Sprintf("较20日高点回撤 %.2f%%", value.drawdown20))
	}
	if price < value.ma20 {
		penalize(10, fmt.Sprintf("现价低于MA20 %.2f", value.ma20))
	}
	if value.marketRegime == marketregime.Bear {
		penalize(10, "市场处于熊市，短线高波动暴露受限")
	} else if value.marketRegime == marketregime.HighVol {
		penalize(12, "市场处于高波动状态，追涨容错率较低")
	}
	if input.Board != nil && input.Board.Percent < -1 {
		penalize(8, fmt.Sprintf("板块涨幅 %.2f%%，行业环境偏弱", input.Board.Percent))
	}

	hardRisk := false
	if fast.breakoutFailure && radar.BreakoutDistance <= -1 {
		hardRisk = true
		radar.Risks = append(radar.Risks, "突破失败后仍低于前20日高点1%以上")
	}
	if fast.openingGapReady && fast.openingGap <= -3 && !fast.openingGapRecovered && currentPercent <= 0 {
		hardRisk = true
		radar.Risks = append(radar.Risks, "低开后未能收复开盘价，开盘结构失效")
	}
	if limitDown > 0 && price <= limitDown*1.01 {
		hardRisk = true
		radar.Risks = append(radar.Risks, "价格接近跌停边界，禁止抓妖观察")
	}
	if radar.BreakoutLevelReady && price < value.prior20Low*.99 {
		hardRisk = true
		radar.Risks = append(radar.Risks, "价格跌破前20日结构低点")
	}
	if currentPercent <= -5 && speed < 0 {
		hardRisk = true
		radar.Risks = append(radar.Risks, "盘中快速下跌，短线结构已破坏")
	}
	specialRule := monsterSpecialRule(input)
	if specialRule {
		hardRisk = true
		radar.Warnings = append(radar.Warnings, "特殊涨跌幅规则或北交所标的，涨停边界需人工核验")
	}

	nearBreakout := radar.BreakoutLevelReady && radar.BreakoutDistance >= monsterNearBreakout
	nearLimit := radar.LimitBoundaryReady && radar.LimitUpDistance <= monsterNearLimit
	boardWeak := input.Board != nil && !boardPositive
	intradayWeak := radar.MinuteDataReady && (radar.MinuteMomentum < -1 || (!radar.VWAPSupport && radar.MinuteMomentum < 0))
	highPosition := nearLimit || nearBreakout || radar.RecentStrongDays >= 3 ||
		(finite(value.return20) && value.return20 >= 25) || (finite(value.drawdown20) && value.drawdown20 < -8)
	openingWeak := fast.openingGapReady && fast.openingGap <= -2 && !fast.openingGapRecovered
	divergence := fast.breakoutFailure || fast.volumePriceDivergence ||
		(highPosition && (overheated || radar.IntradayPullback >= 3 || currentPercent < 0 || speed < 0 || boardWeak || intradayWeak || openingWeak))
	ebbing := hardRisk ||
		(fast.breakoutFailure && radar.BreakoutDistance <= -1) ||
		(openingWeak && fast.openingGap <= -3 && currentPercent <= 0) ||
		(price < value.ma20 && (currentPercent <= 0 || speed < 0 || (finite(value.drawdown20) && value.drawdown20 < -8))) ||
		(currentPercent <= -5 && speed < 0)
	acceleration := !divergence && trend && (nearBreakout || nearLimit || currentPercent >= 5 || fast.openingRangeBreakout) &&
		currentPercent >= 2 && speed >= 0 && (healthyVolume || currentPercent >= 7)
	starting := !divergence && !acceleration && trend &&
		((nearBreakout && currentPercent >= 1.5) || fast.openingRangeBreakout || fast.openingGapHeld || (finite(value.rangeCompression) && value.rangeCompression <= .65 && currentPercent >= 1) || radar.RecentStrongDays >= 1 || radar.ConsecutiveStrongDays >= 1)

	stage := MonsterStageDormant
	switch {
	case ebbing:
		stage = MonsterStageEbbing
	case divergence:
		stage = MonsterStageDiverging
	case acceleration:
		stage = MonsterStageAccelerating
	case starting:
		stage = MonsterStageStarting
	}
	if stage == MonsterStageEbbing {
		score = math.Min(score, 35)
	} else if stage == MonsterStageDiverging {
		score = math.Min(score, 64)
	}
	if score < 0 {
		score = 0
	}
	radar.Score = math.Round(math.Min(100, score)*10) / 10
	radar.Stage = stage
	radar.Label = monsterStageLabels[stage]

	// A complete boundary, market regime and sector context are required for
	// an eligible observation. Missing context remains visible but cannot be
	// promoted by the radar or used by the shadow account.
	radar.Confidence = "低"
	if radar.Score >= monsterMinimumScore && radar.BreakoutLevelReady {
		radar.Confidence = "中"
	}
	if radar.Score >= monsterHighConfidence && radar.LimitBoundaryReady && radar.QuoteFresh && input.Board != nil && value.marketRegime != marketregime.Insufficient && !hardRisk {
		radar.Confidence = "高"
	}
	radar.Eligible = radar.Score >= monsterMinimumScore &&
		(stage == MonsterStageDormant || stage == MonsterStageStarting || stage == MonsterStageAccelerating) &&
		radar.LimitBoundaryReady && radar.BreakoutLevelReady && radar.QuoteFresh && input.Board != nil &&
		value.marketRegime != marketregime.Insufficient && value.marketRegime != marketregime.Bear &&
		value.marketRegime != marketregime.HighVol && !hardRisk
	if !radar.Eligible {
		switch stage {
		case MonsterStageDiverging:
			radar.Risks = append(radar.Risks, "高位分歧阶段只保留观察，不进入抓妖候选")
		case MonsterStageEbbing:
			radar.Risks = append(radar.Risks, "退潮阶段不进入抓妖候选")
		}
	}
	radar.Reasons = uniqueStrings(radar.Reasons, 8)
	radar.Risks = uniqueStrings(radar.Risks, 8)
	radar.Warnings = uniqueStrings(radar.Warnings, 8)
	return radar
}

func recentMonsterStrongDays(bars []domain.DailyBar, lookback int) int {
	count, _ := recentMonsterStrongDaysDetailed(bars, lookback)
	return count
}

func recentMonsterStrongDaysDetailed(bars []domain.DailyBar, lookback int) (count, consecutive int) {
	if lookback < 1 || len(bars) < 2 {
		return 0, 0
	}
	start := len(bars) - lookback
	if start < 1 {
		start = 1
	}
	for index := start; index < len(bars); index++ {
		previous, current := bars[index-1].Close, bars[index].Close
		if previous <= 0 || current <= 0 || !finite(previous) || !finite(current) {
			consecutive = 0
			continue
		}
		change := (current/previous - 1) * 100
		if change >= monsterStrongDayThreshold {
			count++
			consecutive++
		} else {
			consecutive = 0
		}
	}
	return count, consecutive
}

func monsterMinuteEvidence(points []domain.MinutePoint) (momentum float64, vwapSupport, ready bool) {
	valid := make([]domain.MinutePoint, 0, len(points))
	for _, point := range points {
		if point.Price > 0 && finite(point.Price) {
			valid = append(valid, point)
		}
	}
	if len(valid) < 2 {
		return 0, false, false
	}
	first, last := valid[0].Price, valid[len(valid)-1].Price
	if first <= 0 || last <= 0 {
		return 0, false, false
	}
	momentum = (last/first - 1) * 100
	average := valid[len(valid)-1].Average
	if !finite(average) || average <= 0 {
		// A provider may omit VWAP while still supplying a usable price path.
		return momentum, false, true
	}
	return momentum, last >= average, true
}

type monsterFastEvidenceResult struct {
	openingGapReady       bool
	openingGap            float64
	openingGapHeld        bool
	openingGapRecovered   bool
	openingRangeReady     bool
	openingRangeBreakout  bool
	intradayPositionReady bool
	intradayPosition      float64
	breakoutFailure       bool
	volumePriceDivergence bool
}

// monsterFastEvidence extracts point-in-time structure that is commonly used
// by opening-range and momentum strategies. It intentionally uses only fields
// available at the scan clock; no future daily bar is consulted.
func computeMonsterFastEvidence(input Snapshot, value indicators, price, intradayHigh, volumeRatio float64, minuteReady, vwapSupport bool) monsterFastEvidenceResult {
	result := monsterFastEvidenceResult{}
	open := input.Stock.Open
	if !(finite(open) && open > 0) && input.Quote != nil {
		open = parseQuoteNumber(input.Quote.Open)
	}
	previousClose := input.Stock.PreviousClose
	if !(finite(previousClose) && previousClose > 0) && input.Quote != nil {
		previousClose = parseQuoteNumber(input.Quote.PreviousClose)
	}
	if !(finite(previousClose) && previousClose > 0) && finite(value.latest.Close) && value.latest.Close > 0 {
		// The latest completed bar is the prior close for a live session. This
		// fallback is kept explicit so a provider missing f18 still gets a
		// useful, auditable gap calculation.
		previousClose = value.latest.Close
	}
	if finite(open) && open > 0 && finite(previousClose) && previousClose > 0 {
		result.openingGapReady = true
		result.openingGap = (open/previousClose - 1) * 100
		result.openingGapHeld = result.openingGap >= monsterMeaningfulGap && finite(price) && price >= open
		result.openingGapRecovered = result.openingGap <= -monsterMeaningfulGap && finite(price) && price >= open
	}

	low := input.Stock.Low
	if !(finite(low) && low > 0) && input.Quote != nil {
		low = parseQuoteNumber(input.Quote.Low)
	}
	if finite(intradayHigh) && intradayHigh > 0 && finite(low) && low > 0 && intradayHigh > low && finite(price) && price > 0 {
		result.intradayPositionReady = true
		result.intradayPosition = clamp((price-low)/(intradayHigh-low)*100, 0, 100)
	}

	minutePoints := monsterCurrentSessionMinutes(input)
	rangeHigh, _, rangeReady := monsterOpeningRange(minutePoints)
	result.openingRangeReady = rangeReady && monsterOpeningRangeComplete(input)
	if result.openingRangeReady && finite(price) && price > 0 {
		// A small buffer avoids classifying a one-tick touch as a real range
		// break. The buffer is deliberately smaller than the daily breakout
		// tolerance because minute data is already a short-horizon observation.
		result.openingRangeBreakout = price >= rangeHigh*1.002
	}

	if finite(value.prior20High) && value.prior20High > 0 && finite(intradayHigh) && intradayHigh > 0 && finite(price) && price > 0 {
		touched := intradayHigh >= value.prior20High*(1-monsterBreakoutTouch/100)
		fellBack := price < value.prior20High*(1-monsterBreakoutFailure/100)
		result.breakoutFailure = touched && fellBack && intradayHigh > price
	}
	if finite(volumeRatio) && volumeRatio >= 2.5 {
		weakPosition := result.intradayPositionReady && result.intradayPosition <= 40
		weakVWAP := minuteReady && !vwapSupport && monsterMinuteVWAPAvailable(minutePoints)
		result.volumePriceDivergence = weakPosition || weakVWAP
	}
	return result
}

// monsterCurrentSessionMinutes removes stale or future minute points before
// they reach the radar. Providers commonly return a cached full-day timeline;
// using points after the quote clock would leak future information into a live
// signal and make the paper account impossible to reproduce.
func monsterCurrentSessionMinutes(input Snapshot) []domain.MinutePoint {
	points := input.Minutes
	if len(points) == 0 {
		return nil
	}
	expectedDate := ""
	if input.Quote != nil {
		value := strings.TrimSpace(input.Quote.QuoteTime)
		if len(value) >= len("2006-01-02") && !strings.HasPrefix(value, "--") {
			expectedDate = normalizeMonsterDate(value[:len("2006-01-02")])
		}
	}
	cutoff := -1
	expectedNowDate := ""
	if !input.Now.IsZero() {
		local := input.Now.In(marketLocation)
		expectedNowDate = local.Format("2006-01-02")
		if len(input.CalendarDates) > 0 {
			session := MarketSessionAtWithCalendar(input.Now, input.CalendarDates)
			if session.TradingDate != "" {
				expectedNowDate = session.TradingDate
			}
		}
		if expectedDate == "" {
			expectedDate = expectedNowDate
		}
		if expectedDate == expectedNowDate && expectedNowDate == local.Format("2006-01-02") {
			cutoff = local.Hour()*60 + local.Minute()
		}
	}
	if input.Quote != nil {
		if quoteTime, ok := parseMonsterQuoteTime(strings.TrimSpace(input.Quote.QuoteTime)); ok {
			quoteDate := quoteTime.In(marketLocation).Format("2006-01-02")
			if expectedDate == "" {
				expectedDate = quoteDate
			}
			if input.Now.IsZero() || !quoteTime.After(input.Now.In(marketLocation)) {
				if expectedDate == quoteDate {
					quoteCutoff := quoteTime.Hour()*60 + quoteTime.Minute()
					if cutoff < 0 || quoteCutoff < cutoff {
						cutoff = quoteCutoff
					}
				}
			}
		}
	}
	if !input.Now.IsZero() && expectedDate != "" && expectedDate != expectedNowDate {
		// A quote from a prior session cannot make its minute timeline current.
		return nil
	}
	result := make([]domain.MinutePoint, 0, len(points))
	for _, point := range points {
		if point.TradeDate != "" && expectedDate != "" && normalizeMonsterDate(point.TradeDate) != expectedDate {
			continue
		}
		if cutoff >= 0 {
			minute, ok := monsterMinuteClock(point.Time)
			if ok && minute > cutoff {
				continue
			}
		}
		result = append(result, point)
	}
	sort.SliceStable(result, func(left, right int) bool {
		leftMinute, leftOK := monsterMinuteClock(result[left].Time)
		rightMinute, rightOK := monsterMinuteClock(result[right].Time)
		if leftOK && rightOK && leftMinute != rightMinute {
			return leftMinute < rightMinute
		}
		return strings.TrimSpace(result[left].Time) < strings.TrimSpace(result[right].Time)
	})
	return result
}

func normalizeMonsterDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == len("20060102") && !strings.Contains(value, "-") {
		return value[:4] + "-" + value[4:6] + "-" + value[6:]
	}
	return value
}

func monsterOpeningRange(points []domain.MinutePoint) (high, low float64, ready bool) {
	const openMinutes = 9*60 + 30
	closeMinutes := openMinutes + monsterOpeningRangeMinutes
	count := 0
	for _, point := range points {
		if !finite(point.Price) || point.Price <= 0 {
			continue
		}
		minute, ok := monsterMinuteClock(point.Time)
		if !ok || minute < openMinutes || minute >= closeMinutes {
			continue
		}
		if count == 0 {
			high, low = point.Price, point.Price
		} else {
			high = math.Max(high, point.Price)
			low = math.Min(low, point.Price)
		}
		count++
	}
	return high, low, count >= 2 && finite(high) && finite(low) && high >= low && low > 0
}

func monsterOpeningRangeComplete(input Snapshot) bool {
	cutoff := -1
	if !input.Now.IsZero() {
		local := input.Now.In(marketLocation)
		cutoff = local.Hour()*60 + local.Minute()
	}
	if input.Quote != nil {
		if quoteTime, ok := parseMonsterQuoteTime(strings.TrimSpace(input.Quote.QuoteTime)); ok {
			quoteCutoff := quoteTime.In(marketLocation).Hour()*60 + quoteTime.In(marketLocation).Minute()
			if cutoff < 0 || quoteCutoff < cutoff {
				cutoff = quoteCutoff
			}
		}
	}
	// Without a clock (for example a pure unit-test snapshot), the caller has
	// already supplied a bounded minute path and we leave the decision to it.
	return cutoff < 0 || cutoff >= 9*60+30+monsterOpeningRangeMinutes
}

func monsterMinuteClock(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if !strings.Contains(value, ":") && len(value) >= 4 {
		value = value[:2] + ":" + value[2:]
	}
	parts := strings.Split(value, ":")
	if len(parts) < 2 {
		return 0, false
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1][:minInt(len(parts[1]), 2)])
	if hourErr != nil || minuteErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

func monsterMinuteVWAPAvailable(points []domain.MinutePoint) bool {
	for _, point := range points {
		if finite(point.Average) && point.Average > 0 {
			return true
		}
	}
	return false
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func monsterQuoteIsLive(input Snapshot, completed []domain.DailyBar) bool {
	if len(completed) == 0 {
		return true
	}
	quoteDate := ""
	if input.Quote != nil && len(strings.TrimSpace(input.Quote.QuoteTime)) >= len("2006-01-02") {
		quoteDate = strings.TrimSpace(input.Quote.QuoteTime)[:len("2006-01-02")]
	}
	if quoteDate == "" && !input.Now.IsZero() {
		quoteDate = input.Now.In(marketLocation).Format("2006-01-02")
	}
	return quoteDate == "" || quoteDate > completed[len(completed)-1].Date
}

func monsterQuoteFresh(input Snapshot) bool {
	fresh, _, _ := monsterQuoteFreshness(input)
	return fresh
}

// monsterQuoteFreshness rejects a quote that is merely from the same calendar
// day but too old to drive a high-volatility entry. Public quote fallbacks can
// lag while still carrying today's date; treating those rows as live is worse
// than leaving a visible, non-eligible radar observation.
func monsterQuoteFreshness(input Snapshot) (bool, int, string) {
	if input.Quote == nil {
		return false, 0, "实时报价缺失或日期未对齐，雷达仅保留历史结构"
	}
	value := strings.TrimSpace(input.Quote.QuoteTime)
	if len(value) < len("2006-01-02") || strings.HasPrefix(value, "--") {
		return false, 0, "实时报价缺失或日期未对齐，雷达仅保留历史结构"
	}
	quoteTime, parsed := parseMonsterQuoteTime(value)
	quoteDate := value[:len("2006-01-02")]
	if input.Now.IsZero() {
		return true, 0, ""
	}
	expected := input.Now.In(marketLocation).Format("2006-01-02")
	if len(input.CalendarDates) > 0 {
		session := MarketSessionAtWithCalendar(input.Now, input.CalendarDates)
		if session.TradingDate != "" {
			expected = session.TradingDate
		}
	}
	if quoteDate != expected {
		return false, 0, "实时报价缺失或日期未对齐，雷达仅保留历史结构"
	}
	if !parsed {
		return false, 0, "实时报价时间格式无效，雷达仅保留历史结构"
	}
	delta := input.Now.In(marketLocation).Sub(quoteTime)
	if delta < -monsterQuoteFutureSlack {
		return false, 0, "实时报价时间超前，雷达仅保留历史结构"
	}
	if delta > monsterQuoteMaxAge {
		return false, int(delta / time.Second), fmt.Sprintf("实时报价已延迟%d分钟，超过%d分钟实时门槛", int(delta/time.Minute), int(monsterQuoteMaxAge/time.Minute))
	}
	if delta < 0 {
		return true, 0, ""
	}
	return true, int(delta / time.Second), ""
}

func parseMonsterQuoteTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		parsed, err := time.ParseInLocation(layout, value, marketLocation)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func monsterLimitPrices(input Snapshot) (float64, float64) {
	if input.Quote == nil {
		return 0, 0
	}
	up := parseQuoteNumber(input.Quote.LimitUp)
	down := parseQuoteNumber(input.Quote.LimitDown)
	if !finite(up) || up <= 0 {
		up = 0
	}
	if !finite(down) || down <= 0 {
		down = 0
	}
	return up, down
}

func monsterBoardEvidence(input Snapshot) (breadth float64, positive, leader bool) {
	if input.Board == nil {
		return math.NaN(), false, false
	}
	board := input.Board
	positive = board.Percent > 0 || board.MainNet > 0
	total := board.RiseCount + board.FallCount + board.FlatCount
	if total > 0 {
		breadth = float64(board.RiseCount) / float64(total)
		if breadth >= .55 {
			positive = true
		}
	}
	leaderCode := normalizeCandidateSymbol(board.LeaderCode)
	stockSymbol := normalizeCandidateSymbol(input.Stock.Symbol)
	leader = leaderCode != "" && (leaderCode == stockSymbol || leaderCode == trimMarket(stockSymbol))
	return breadth, positive, leader
}

func monsterSpecialRule(input Snapshot) bool {
	name := strings.ToUpper(strings.TrimSpace(input.Stock.Name))
	if strings.Contains(name, "ST") || strings.Contains(name, "退") || strings.HasPrefix(strings.ToLower(input.Stock.Symbol), "bj") {
		return true
	}
	return false
}
