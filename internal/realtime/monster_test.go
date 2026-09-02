package realtime

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/marketregime"
)

func monsterTestIndicators() indicators {
	return indicators{
		latest:           domain.DailyBar{Date: "2026-08-28", Close: 10.7, Low: 10.2},
		previous:         domain.DailyBar{Close: 10.5},
		ma20:             10,
		ma60:             9,
		prior20High:      10.5,
		prior20Low:       8.8,
		return20:         .20,
		benchmark20:      .05,
		volatility20:     2,
		drawdown20:       -1,
		rsi14:            64,
		rangeCompression: .5,
		volumeRatio:      2,
		marketRegime:     marketregime.Bull,
	}
}

func monsterTestInput() Snapshot {
	return Snapshot{
		Now: time.Date(2026, 8, 31, 10, 30, 0, 0, marketLocation),
		Stock: domain.MarketStockSnapshot{
			Symbol: "sh600000", Name: "测试股份", Industry: "半导体", Price: 10.8,
			Percent: 6, Speed: 1, Amount: 5e8, Turnover: 5, VolumeRatio: 2, High: 10.9,
		},
		Quote: &domain.Quote{Current: "10.80", LimitUp: "11.88", LimitDown: "9.72", QuoteTime: "2026-08-31 10:30:00"},
		Board: &domain.BoardFlow{Name: "半导体", Percent: 2, MainNet: 8e8, RiseCount: 18, FallCount: 5, FlatCount: 1, LeaderCode: "sh600000"},
	}
}

func TestMonsterRadarMarksSupportedBreakoutAsAcceleration(t *testing.T) {
	radar := classifyMonsterRadar(monsterTestInput(), monsterTestIndicators(), true)
	if radar.Stage != MonsterStageAccelerating || !radar.Eligible || radar.Confidence != "高" {
		t.Fatalf("supported breakout should be an eligible acceleration: %+v", radar)
	}
	if !radar.LimitBoundaryReady || !radar.BreakoutLevelReady || radar.LimitUpDistance <= 0 || radar.BreakoutDistance <= 0 {
		t.Fatalf("boundary or breakout evidence missing: %+v", radar)
	}
	if radar.Score < monsterHighConfidence || len(radar.Reasons) == 0 {
		t.Fatalf("acceleration score/evidence too weak: %+v", radar)
	}
}

func TestMonsterRadarDoesNotConfirmAccelerationWithoutLimitBoundary(t *testing.T) {
	input := monsterTestInput()
	input.Quote.LimitUp = "--"
	input.Quote.LimitDown = "--"
	radar := classifyMonsterRadar(input, monsterTestIndicators(), true)
	if radar.LimitBoundaryReady || radar.Eligible || radar.Confidence == "高" {
		t.Fatalf("missing limit boundary was promoted: %+v", radar)
	}
	joined := strings.Join(radar.Warnings, "；")
	if !strings.Contains(joined, "涨跌停边界") {
		t.Fatalf("boundary warning missing: %+v", radar.Warnings)
	}
}

func TestMonsterRadarRejectsStaleQuoteDate(t *testing.T) {
	input := monsterTestInput()
	input.Quote.QuoteTime = "2026-08-28 10:30:00"
	radar := classifyMonsterRadar(input, monsterTestIndicators(), true)
	if radar.QuoteFresh || radar.Eligible || radar.Confidence == "高" {
		t.Fatalf("stale quote was promoted by radar: %+v", radar)
	}
	if !strings.Contains(strings.Join(radar.Warnings, "；"), "日期未对齐") {
		t.Fatalf("stale quote warning missing: %+v", radar.Warnings)
	}
}

func TestMonsterRadarRejectsSameDayQuoteBeyondFreshnessWindow(t *testing.T) {
	input := monsterTestInput()
	input.Now = time.Date(2026, 8, 31, 11, 0, 0, 0, marketLocation)
	input.Quote.QuoteTime = "2026-08-31 10:30:00"
	radar := classifyMonsterRadar(input, monsterTestIndicators(), true)
	if radar.QuoteFresh || radar.Eligible {
		t.Fatalf("same-day stale quote was promoted by radar: %+v", radar)
	}
	if radar.QuoteAgeSeconds != 1800 {
		t.Fatalf("unexpected quote age: got %d want 1800", radar.QuoteAgeSeconds)
	}
	if !strings.Contains(strings.Join(radar.Warnings, "；"), "超过10分钟") {
		t.Fatalf("freshness warning missing: %+v", radar.Warnings)
	}
}

func TestMonsterRadarAcceptsQuoteWithinFreshnessWindow(t *testing.T) {
	input := monsterTestInput()
	input.Now = time.Date(2026, 8, 31, 10, 35, 0, 0, marketLocation)
	input.Quote.QuoteTime = "2026-08-31 10:30:00"
	radar := classifyMonsterRadar(input, monsterTestIndicators(), true)
	if !radar.QuoteFresh || radar.QuoteAgeSeconds != 300 {
		t.Fatalf("quote inside freshness window was rejected: %+v", radar)
	}
}

func TestMonsterRadarDowngradesExtremeVolumeToDivergence(t *testing.T) {
	input := monsterTestInput()
	input.Stock.VolumeRatio = 6
	input.Stock.Turnover = 18
	input.Stock.High = 12
	input.Stock.Price = 10.8
	input.Quote.Current = "10.80"
	value := monsterTestIndicators()
	value.rsi14 = 86
	radar := classifyMonsterRadar(input, value, true)
	if radar.Stage != MonsterStageDiverging || radar.Eligible {
		t.Fatalf("extreme turnover should be divergence only: %+v", radar)
	}
	if !strings.Contains(strings.Join(radar.Risks, "；"), "极端") {
		t.Fatalf("extreme-volume risk missing: %+v", radar.Risks)
	}
}

func TestMonsterRadarMarksBrokenStructureAsEbbing(t *testing.T) {
	input := monsterTestInput()
	input.Stock.Price = 9
	input.Stock.Percent = -6
	input.Stock.Speed = -1
	input.Stock.High = 10
	input.Quote.Current = "9.00"
	value := monsterTestIndicators()
	value.prior20Low = 9.5
	value.return20 = -0.04
	value.drawdown20 = -14
	radar := classifyMonsterRadar(input, value, true)
	if radar.Stage != MonsterStageEbbing || radar.Eligible || radar.Score > 35 {
		t.Fatalf("broken structure should be capped ebbing: %+v", radar)
	}
}

func TestMonsterRadarTreatsUnreclaimedMA20AsEbbing(t *testing.T) {
	input := monsterTestInput()
	input.Stock.Price = 9.8
	input.Stock.Percent = 0
	input.Stock.Speed = 0
	input.Quote.Current = "9.80"
	value := monsterTestIndicators()
	value.drawdown20 = -4
	if radar := classifyMonsterRadar(input, value, true); radar.Stage != MonsterStageEbbing {
		t.Fatalf("price below MA20 without a reclaim should ebb: %+v", radar)
	}
}

func TestMonsterRadarReturnsInsufficientWithoutCompletedIndicators(t *testing.T) {
	radar := classifyMonsterRadar(monsterTestInput(), indicators{}, false)
	if radar.Stage != MonsterStageInsufficient || radar.Score != 0 || radar.Confidence != "不可用" || radar.Eligible {
		t.Fatalf("insufficient history produced a usable radar: %+v", radar)
	}
}

func TestMonsterMinuteEvidenceUsesPricePathAndVWAP(t *testing.T) {
	momentum, support, ready := monsterMinuteEvidence([]domain.MinutePoint{
		{Price: 10, Average: 10}, {Price: 10.2, Average: 10.1}, {Price: 10.4, Average: 10.2},
	})
	if !ready || !support || momentum <= 3 {
		t.Fatalf("unexpected minute evidence: momentum=%.2f support=%v ready=%v", momentum, support, ready)
	}
	momentum, support, ready = monsterMinuteEvidence([]domain.MinutePoint{{Price: 10}, {Price: 9.8, Average: 9.9}})
	if !ready || support || momentum >= 0 {
		t.Fatalf("downward minute evidence was not detected: momentum=%.2f support=%v ready=%v", momentum, support, ready)
	}
}

func TestMonsterFastEvidenceDetectsHeldGapAndOpeningRangeBreakout(t *testing.T) {
	input := Snapshot{
		Stock: domain.MarketStockSnapshot{
			Open: 10.2, PreviousClose: 10, Price: 10.65, High: 10.8, Low: 10.1,
		},
		Minutes: []domain.MinutePoint{
			{Time: "09:30", Price: 10.2, Average: 10.2},
			{Time: "09:35", Price: 10.35, Average: 10.25},
			{Time: "09:44", Price: 10.4, Average: 10.3},
		},
	}
	value := indicators{latest: domain.DailyBar{Close: 10}}
	fast := computeMonsterFastEvidence(input, value, 10.65, 10.8, 1.8, true, true)
	if !fast.openingGapReady || fast.openingGap < 1.9 || !fast.openingGapHeld {
		t.Fatalf("held opening gap was not detected: %+v", fast)
	}
	if !fast.openingRangeReady || !fast.openingRangeBreakout {
		t.Fatalf("opening-range breakout was not detected: %+v", fast)
	}
	if !fast.intradayPositionReady || fast.intradayPosition < 70 {
		t.Fatalf("intraday position was not calculated: %+v", fast)
	}
}

func TestMonsterFastEvidenceDistinguishesRecoveredGapDown(t *testing.T) {
	input := Snapshot{Stock: domain.MarketStockSnapshot{Open: 9.6, PreviousClose: 10, Price: 9.7}}
	fast := computeMonsterFastEvidence(input, indicators{latest: domain.DailyBar{Close: 10}}, 9.7, 9.8, 1.2, false, false)
	if !fast.openingGapReady || fast.openingGap >= -3.9 || !fast.openingGapRecovered || fast.openingGapHeld {
		t.Fatalf("gap-down recovery was classified incorrectly: %+v", fast)
	}
}

func TestMonsterOpeningRangeWaitsUntilFifteenMinutesHaveElapsed(t *testing.T) {
	input := Snapshot{
		Now:   time.Date(2026, 8, 31, 9, 35, 0, 0, marketLocation),
		Quote: &domain.Quote{QuoteTime: "2026-08-31 09:35:00"},
		Stock: domain.MarketStockSnapshot{Open: 10, PreviousClose: 10, Price: 10.5, High: 10.5, Low: 10},
		Minutes: []domain.MinutePoint{
			{TradeDate: "2026-08-31", Time: "09:30", Price: 10},
			{TradeDate: "2026-08-31", Time: "09:35", Price: 10.4},
		},
	}
	fast := computeMonsterFastEvidence(input, indicators{latest: domain.DailyBar{Close: 10}}, 10.5, 10.5, 1.5, true, true)
	if fast.openingRangeReady || fast.openingRangeBreakout {
		t.Fatalf("opening range was promoted before the observation window elapsed: %+v", fast)
	}
	input.Now = time.Date(2026, 8, 31, 9, 50, 0, 0, marketLocation)
	input.Quote.QuoteTime = "2026-08-31 09:50:00"
	fast = computeMonsterFastEvidence(input, indicators{latest: domain.DailyBar{Close: 10}}, 10.5, 10.5, 1.5, true, true)
	if !fast.openingRangeReady || !fast.openingRangeBreakout {
		t.Fatalf("opening range was not available after the observation window: %+v", fast)
	}
}

func TestMonsterFastEvidenceDetectsFailedBreakoutAndVolumePriceDivergence(t *testing.T) {
	input := Snapshot{
		Stock: domain.MarketStockSnapshot{
			Open: 10.5, PreviousClose: 10, Price: 10.2, High: 11.1, Low: 10,
		},
		Minutes: []domain.MinutePoint{
			{Time: "09:30", Price: 10.5, Average: 10.5},
			{Time: "09:40", Price: 10.25, Average: 10.45},
			{Time: "10:00", Price: 10.2, Average: 10.4},
		},
	}
	value := indicators{latest: domain.DailyBar{Close: 10}, prior20High: 10.5}
	fast := computeMonsterFastEvidence(input, value, 10.2, 11.1, 3.2, true, false)
	if !fast.breakoutFailure {
		t.Fatalf("failed breakout was not detected: %+v", fast)
	}
	if !fast.volumePriceDivergence {
		t.Fatalf("volume-price divergence was not detected: %+v", fast)
	}
	if fast.intradayPosition >= 40 {
		t.Fatalf("weak intraday close position expected, got %.2f", fast.intradayPosition)
	}
}

func TestMonsterRadarDowngradesFailedBreakoutWithOpeningWeakness(t *testing.T) {
	input := monsterTestInput()
	input.Stock.Open = 10.5
	input.Stock.PreviousClose = 10
	input.Stock.Price = 10.2
	input.Stock.Percent = 2
	input.Stock.Speed = -0.4
	input.Stock.High = 11.1
	input.Stock.Low = 10
	input.Stock.VolumeRatio = 3
	input.Quote.Current = "10.20"
	input.Quote.Open = "10.50"
	input.Quote.PreviousClose = "10.00"
	input.Quote.High = "11.10"
	input.Quote.Low = "10.00"
	value := monsterTestIndicators()
	value.prior20High = 10.5
	radar := classifyMonsterRadar(input, value, true)
	if !radar.BreakoutFailure || !radar.VolumePriceDivergence {
		t.Fatalf("expected structural failure flags: %+v", radar)
	}
	if radar.Stage != MonsterStageEbbing || radar.Eligible || radar.Score > 35 {
		t.Fatalf("failed breakout should be capped as ebbing: %+v", radar)
	}
	joined := strings.Join(radar.Risks, "；")
	if !strings.Contains(joined, "突破失败") || !strings.Contains(joined, "低于前20日高点") {
		t.Fatalf("failed-breakout evidence missing: %v", radar.Risks)
	}
}

func TestMonsterMinuteClockAcceptsCompactAndColonTimes(t *testing.T) {
	for _, value := range []string{"0930", "09:44", "9:45"} {
		minute, ok := monsterMinuteClock(value)
		if !ok || minute < 9*60+30 {
			t.Fatalf("minute clock did not parse %q: %d %v", value, minute, ok)
		}
	}
	if _, ok := monsterMinuteClock("bad"); ok {
		t.Fatal("invalid minute clock was accepted")
	}
}

func TestMonsterCurrentSessionMinutesDropsStaleAndFuturePoints(t *testing.T) {
	now := time.Date(2026, 8, 31, 10, 30, 0, 0, marketLocation)
	input := Snapshot{
		Now:   now,
		Quote: &domain.Quote{QuoteTime: "2026-08-31 10:30:00"},
		Minutes: []domain.MinutePoint{
			{TradeDate: "2026-08-30", Time: "10:20", Price: 9},
			{TradeDate: "2026-08-31", Time: "10:20", Price: 10},
			{TradeDate: "2026-08-31", Time: "10:31", Price: 11},
			{TradeDate: "", Time: "10:25", Price: 10.2},
		},
	}
	points := monsterCurrentSessionMinutes(input)
	if len(points) != 2 || points[0].Price != 10 || points[1].Price != 10.2 {
		t.Fatalf("stale/future minute points were not filtered: %+v", points)
	}
}

func TestMonsterRadarSerializesReadyZeroMetrics(t *testing.T) {
	raw, err := json.Marshal(MonsterRadar{OpeningGapReady: true, OpeningGapPercent: 0, IntradayPositionReady: true, IntradayPosition: 0})
	if err != nil {
		t.Fatalf("marshal radar: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `"opening_gap_percent":0`) || !strings.Contains(text, `"intraday_position_percent":0`) {
		t.Fatalf("ready zero metrics were dropped from JSON: %s", text)
	}
}

func TestMonsterCurrentSessionMinutesRejectsPriorDayQuote(t *testing.T) {
	input := Snapshot{
		Now:     time.Date(2026, 8, 31, 10, 30, 0, 0, marketLocation),
		Quote:   &domain.Quote{QuoteTime: "2026-08-30 15:00:00"},
		Minutes: []domain.MinutePoint{{TradeDate: "2026-08-30", Time: "14:59", Price: 10}},
	}
	if points := monsterCurrentSessionMinutes(input); len(points) != 0 {
		t.Fatalf("prior-day quote should not supply current-session minutes: %+v", points)
	}
}

func TestMonsterRadarIsIndependentFromCompositeScore(t *testing.T) {
	strategies := []Strategy{
		strategyFunc{key: "trend-breakout", name: "趋势突破", fn: func(Snapshot) Component { return component("trend-breakout", "趋势突破", 20, nil, nil) }},
		strategyFunc{key: "relative-momentum", name: "相对动量", fn: func(Snapshot) Component { return component("relative-momentum", "相对动量", 20, nil, nil) }},
		strategyFunc{key: "price-volume", name: "量价确认", fn: func(Snapshot) Component { return component("price-volume", "量价确认", 20, nil, nil) }},
		strategyFunc{key: "fund-support", name: "资金承接", fn: func(Snapshot) Component { return component("fund-support", "资金承接", 20, nil, nil) }},
		strategyFunc{key: "sector-rotation", name: "板块轮动", fn: func(Snapshot) Component { return component("sector-rotation", "板块轮动", 20, nil, nil) }},
		strategyFunc{key: "market-regime", name: "市场适配", fn: func(Snapshot) Component { return component("market-regime", "市场适配", 20, nil, nil) }},
		strategyFunc{key: "volatility-risk", name: "波动风险", fn: func(Snapshot) Component { return component("volatility-risk", "波动风险", 20, nil, nil) }},
		strategyFunc{key: "liquidity-quality", name: "流动性质量", fn: func(Snapshot) Component { return component("liquidity-quality", "流动性质量", 20, nil, nil) }},
	}
	scanner := &Scanner{strategies: strategies, now: func() time.Time { return time.Date(2026, 8, 31, 10, 30, 0, 0, marketLocation) }}
	input := monsterTestInput()
	input.Now = scanner.now()
	input.Bars = risingBars(80)
	input.Benchmark = risingBars(80)
	result := scanner.evaluate(input)
	if math.Abs(result.Score-100) > 1e-9 || result.Monster.Stage == "" {
		t.Fatalf("radar changed or failed to attach composite evidence: %+v", result)
	}
}

func TestMonsterRankingCandidateAndSourceSummary(t *testing.T) {
	item := domain.MarketStockSnapshot{Symbol: "SH600000", Name: "测试股份", Percent: 4}
	if !monsterRankingCandidate(item, domain.MarketScanByPercent) {
		t.Fatal("positive percent ranking row should be a candidate")
	}
	if monsterRankingCandidate(domain.MarketStockSnapshot{Symbol: "sh600001", Name: "测试股份"}, domain.MarketScanByAmount) {
		t.Fatal("amount ranking row without turnover or directional evidence should be ignored")
	}
	signals := []Signal{
		{CandidateSources: []string{"自选", "涨幅榜"}},
		{CandidateSources: []string{"涨幅榜", "主力净流入榜"}},
	}
	sources := summarizeCandidateSources(signals)
	if sources["自选"] != 1 || sources["涨幅榜"] != 2 || sources["主力净流入榜"] != 1 {
		t.Fatalf("unexpected source summary: %+v", sources)
	}
}

func TestStockSymbolsCanonicalizeExchangePrefix(t *testing.T) {
	got := stockSymbols([]string{" SH600000 ", "SZ000001", "bj830000", "600519", "bad"})
	want := []string{"sh600000", "sz000001", "bj830000"}
	if len(got) != len(want) {
		t.Fatalf("unexpected canonical symbol count: got=%v want=%v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("unexpected canonical symbol at %d: got=%v want=%v", index, got, want)
		}
	}
}

func TestMonsterBoardEvidenceCanonicalizesLeaderCode(t *testing.T) {
	input := monsterTestInput()
	input.Board.LeaderCode = "SH600000"
	_, _, leader := monsterBoardEvidence(input)
	if !leader {
		t.Fatal("leader code should match case-insensitively")
	}
}

func TestInterleaveCandidateSymbolsPreservesFeedDiversity(t *testing.T) {
	got := interleaveCandidateSymbols([][]string{{"percent-1", "percent-2", "percent-3"}, {"amount-1"}, {"flow-1", "flow-2"}})
	want := []string{"percent-1", "amount-1", "flow-1", "percent-2", "flow-2", "percent-3"}
	if len(got) != len(want) {
		t.Fatalf("unexpected interleaved length: got=%v want=%v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("unexpected interleaving at %d: got=%v want=%v", index, got, want)
		}
	}
}

func TestMonsterStagesAreArchivedForForwardValidation(t *testing.T) {
	signal := Signal{
		ID: "monster", Symbol: "sh600000", AsOf: time.Date(2026, 8, 20, 10, 0, 0, 0, marketLocation),
		Score: 70, Monster: MonsterRadar{Score: 82, Stage: MonsterStageAccelerating, Eligible: true},
	}
	bars := risingBars(80)
	// Ensure there is at least one strictly later bar for a ready outcome.
	report := BuildOutcomeReport([]SignalOutcome{labelSignal(signal, bars, bars, 1, 5, time.Now())}, time.Now(), nil)
	if len(report.MonsterStages) != 1 || report.MonsterStages[0].Key != string(MonsterStageAccelerating) {
		t.Fatalf("monster stage was not included in outcome breakdown: %+v", report.MonsterStages)
	}
}

func TestMonsterStageBreakdownUsesMonsterScoreForRankIC(t *testing.T) {
	outcomes := []SignalOutcome{
		{Key: "a", Symbol: "sh600001", SignalDate: "2026-08-20", Horizon: 1, Status: OutcomeReady, Score: 90, MonsterScore: 60, MonsterStage: MonsterStageStarting, ReturnPercent: 1},
		{Key: "b", Symbol: "sh600002", SignalDate: "2026-08-20", Horizon: 1, Status: OutcomeReady, Score: 80, MonsterScore: 70, MonsterStage: MonsterStageStarting, ReturnPercent: 2},
		{Key: "c", Symbol: "sh600003", SignalDate: "2026-08-20", Horizon: 1, Status: OutcomeReady, Score: 70, MonsterScore: 80, MonsterStage: MonsterStageStarting, ReturnPercent: 3},
	}
	report := BuildOutcomeReport(outcomes, time.Now(), nil)
	if len(report.MonsterStages) != 1 || len(report.MonsterStages[0].Summaries) != 1 {
		t.Fatalf("unexpected monster stage report: %+v", report.MonsterStages)
	}
	summary := report.MonsterStages[0].Summaries[0]
	if summary.RankInformationCoefficient < .99 {
		t.Fatalf("monster stage Rank IC should use monster score, got %.3f", summary.RankInformationCoefficient)
	}
}
