package realtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/marketregime"
)

const (
	defaultOutcomeSignalLimit               = 500
	defaultTargetReturn                     = 5.0
	minimumResearchSamples                  = 35
	minimumResearchDates                    = 5
	minimumFoldSamples                      = 5
	minimumActiveStrategyScore              = 8.0
	minimumCorrelationSamples               = 20
	minimumRegimeSamples                    = 8
	minimumComponentCoverage                = 70.0
	redundantRankCorrelation                = 0.75
	minimumComponentValidationSamples       = 35
	minimumComponentValidationActiveSamples = 5
	minimumComponentValidationFoldSamples   = 8
	minimumComponentValidationFolds         = 2
	maximumComponentWeightDrift             = 0.20
	minimumPortfolioSignalsPerDay           = 5
	minimumPortfolioDays                    = 5
	minimumPortfolioScore                   = 55.0
	minimumPortfolioIndustryCoverage        = 70.0
	maximumPortfolioIndustryConcentration   = 40.0
	maximumPortfolioComponentConcentration  = 60.0
	maximumPortfolioRedundantPairPercent    = 30.0
	minimumCalibrationComparisonSamples     = 20
	minimumCalibrationExcessUplift          = 0.20
	minimumCalibrationHitRateUplift         = -2.0
	maximumCalibrationExcessDrawdown        = -0.20
	maximumCalibrationHitRateDrawdown       = -5.0
)

const (
	MarketRegimeBull         = string(marketregime.Bull)
	MarketRegimeBear         = string(marketregime.Bear)
	MarketRegimeRange        = string(marketregime.Range)
	MarketRegimeHighVol      = string(marketregime.HighVol)
	MarketRegimeInsufficient = string(marketregime.Insufficient)
	MarketRegimeUnlabeled    = string(marketregime.Unlabeled)
)

// OutcomeOptions controls the point-in-time labeling pass. Horizons are
// trading-bar counts, never calendar-day counts.
type OutcomeOptions struct {
	Horizons     []int
	TargetReturn float64
	Now          func() time.Time
	SignalLimit  int
}

type OutcomeEvaluator struct {
	history         HistoryClient
	benchmark       HistoryClient
	store           OutcomeStore
	benchmarkSymbol string
	now             func() time.Time
}

func NewOutcomeEvaluator(history, benchmark HistoryClient, store OutcomeStore) *OutcomeEvaluator {
	return &OutcomeEvaluator{
		history: history, benchmark: benchmark, store: store,
		benchmarkSymbol: "sh000300", now: time.Now,
	}
}

func (e *OutcomeEvaluator) Evaluate(ctx context.Context, signals []Signal, options OutcomeOptions) (OutcomeReport, error) {
	if e == nil || e.history == nil {
		return OutcomeReport{}, fmt.Errorf("实时信号结果评估器未初始化")
	}
	now := e.now
	if options.Now != nil {
		now = options.Now
	}
	if now == nil {
		now = time.Now
	}
	horizons := normalizedHorizons(options.Horizons)
	targetReturn := options.TargetReturn
	if targetReturn <= 0 || !finite(targetReturn) {
		targetReturn = defaultTargetReturn
	}
	limit := options.SignalLimit
	if limit == 0 {
		limit = defaultOutcomeSignalLimit
	} else if limit < 0 {
		// A negative limit is an explicit full-archive request. The scheduler uses
		// this mode so older pending horizons get another chance to mature instead
		// of being permanently crowded out by frequent intraday scans.
		limit = 0
	}
	evaluatedAt := now()
	selected := representativeSignals(signals, limit)
	if len(selected) == 0 {
		return e.Report(limit, evaluatedAt)
	}

	bySymbol := make(map[string][]Signal)
	for _, signal := range selected {
		bySymbol[signal.Symbol] = append(bySymbol[signal.Symbol], signal)
	}
	barMap, warnings := e.fetchHistories(ctx, bySymbol)
	benchmarkBars := []domain.DailyBar(nil)
	if e.benchmark != nil {
		bars, err := e.benchmark.FetchDailyBars(ctx, e.benchmarkSymbol)
		if err != nil {
			warnings = append(warnings, "沪深300结果基准不可用: "+err.Error())
		} else {
			benchmarkBars = normalizedOutcomeBars(bars)
		}
	}
	dataThrough := latestOutcomeDataDate(benchmarkBars, barMap)

	outcomes := make([]SignalOutcome, 0, len(selected)*len(horizons))
	for _, signal := range selected {
		bars := barMap[signal.Symbol]
		for _, horizon := range horizons {
			outcomes = append(outcomes, labelSignal(signal, bars, benchmarkBars, horizon, targetReturn, evaluatedAt))
		}
	}
	existing := []SignalOutcome(nil)
	if e.store != nil {
		items, err := e.store.List(0)
		if err != nil {
			warnings = append(warnings, "读取既有信号结果失败: "+err.Error())
		} else {
			existing = items
		}
		changes, merged := mergeOutcomeRevisions(existing, outcomes)
		if err := e.store.Upsert(changes); err != nil {
			warnings = append(warnings, "信号结果留痕失败: "+err.Error())
		}
		outcomes = merged
	}
	report := BuildOutcomeReport(outcomes, evaluatedAt, warnings)
	report.DataThrough = dataThrough
	return report, nil
}

func latestOutcomeDataDate(benchmark []domain.DailyBar, histories map[string][]domain.DailyBar) string {
	if len(benchmark) > 0 {
		return benchmark[len(benchmark)-1].Date
	}
	latest := ""
	for _, bars := range histories {
		if len(bars) > 0 && bars[len(bars)-1].Date > latest {
			latest = bars[len(bars)-1].Date
		}
	}
	return latest
}

func (e *OutcomeEvaluator) Report(limit int, now time.Time) (OutcomeReport, error) {
	if now.IsZero() {
		now = time.Now()
	}
	if e == nil || e.store == nil {
		return OutcomeReport{GeneratedAt: now, AsOf: now.Format("2006-01-02"), Horizons: normalizedHorizons(nil), Summaries: emptySummaries(normalizedHorizons(nil))}, nil
	}
	items, err := e.store.List(limit)
	if err != nil {
		return OutcomeReport{}, err
	}
	if len(items) == 0 {
		return OutcomeReport{GeneratedAt: now, AsOf: now.Format("2006-01-02"), Horizons: normalizedHorizons(nil), Summaries: emptySummaries(normalizedHorizons(nil))}, nil
	}
	return BuildOutcomeReport(items, now, nil), nil
}

func (e *OutcomeEvaluator) fetchHistories(ctx context.Context, bySymbol map[string][]Signal) (map[string][]domain.DailyBar, []string) {
	barMap := make(map[string][]domain.DailyBar, len(bySymbol))
	warnings := make([]string, 0)
	var mu sync.Mutex
	limit := make(chan struct{}, 6)
	var group sync.WaitGroup
	for symbol := range bySymbol {
		group.Add(1)
		go func(symbol string) {
			defer group.Done()
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				warnings = append(warnings, fmt.Sprintf("%s 历史日K评估取消: %v", symbol, ctx.Err()))
				mu.Unlock()
				return
			}
			defer func() { <-limit }()
			bars, err := e.history.FetchDailyBars(ctx, symbol)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s 历史日K不可用: %v", symbol, err))
				return
			}
			barMap[symbol] = normalizedOutcomeBars(bars)
		}(symbol)
	}
	group.Wait()
	return barMap, warnings
}

func mergeOutcomeRevisions(existing, updates []SignalOutcome) ([]SignalOutcome, []SignalOutcome) {
	latest := make(map[string]SignalOutcome, len(existing)+len(updates))
	for _, item := range existing {
		if item.Key != "" {
			latest[item.Key] = item
		}
	}
	changes := make([]SignalOutcome, 0, len(updates))
	for _, item := range updates {
		previous, found := latest[item.Key]
		if found && previous.Status == OutcomeReady && item.Status != OutcomeReady {
			continue
		}
		if found && item.EvaluatedAt.Before(previous.EvaluatedAt) {
			continue
		}
		latest[item.Key] = item
		changes = append(changes, item)
	}
	merged := make([]SignalOutcome, 0, len(latest))
	for _, item := range latest {
		merged = append(merged, item)
	}
	return changes, merged
}

func normalizedHorizons(input []int) []int {
	if len(input) == 0 {
		return []int{OutcomeHorizon1D, OutcomeHorizon3D, OutcomeHorizon5D, OutcomeHorizon10D}
	}
	seen := make(map[int]bool)
	result := make([]int, 0, len(input))
	for _, value := range input {
		if value < 1 || value > 252 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Ints(result)
	if len(result) == 0 {
		return []int{OutcomeHorizon1D, OutcomeHorizon3D, OutcomeHorizon5D, OutcomeHorizon10D}
	}
	return result
}

func normalizedOutcomeBars(input []domain.DailyBar) []domain.DailyBar {
	result := make([]domain.DailyBar, 0, len(input))
	for _, bar := range input {
		if len(bar.Date) != len("2006-01-02") || bar.Open <= 0 || bar.Close <= 0 || bar.High <= 0 || bar.Low <= 0 ||
			!finite(bar.Open) || !finite(bar.Close) || !finite(bar.High) || !finite(bar.Low) {
			continue
		}
		result = append(result, bar)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	unique := result[:0]
	for _, bar := range result {
		if len(unique) > 0 && unique[len(unique)-1].Date == bar.Date {
			unique[len(unique)-1] = bar
			continue
		}
		unique = append(unique, bar)
	}
	return unique
}

func representativeSignals(input []Signal, limit int) []Signal {
	items := append([]Signal(nil), input...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].AsOf.Equal(items[j].AsOf) {
			if items[i].Score == items[j].Score {
				return items[i].ID > items[j].ID
			}
			return items[i].Score > items[j].Score
		}
		return items[i].AsOf.After(items[j].AsOf)
	})
	seen := make(map[string]bool)
	result := make([]Signal, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Symbol) == "" {
			continue
		}
		date := signalDate(item)
		key := item.Symbol + "|" + date
		if date == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].AsOf.Before(result[j].AsOf) })
	return result
}

func signalDate(signal Signal) string {
	if !signal.AsOf.IsZero() {
		return signal.AsOf.In(time.Local).Format("2006-01-02")
	}
	if len(signal.DataDate) == len("2006-01-02") {
		return signal.DataDate
	}
	return ""
}

func labelSignal(signal Signal, bars, benchmark []domain.DailyBar, horizon int, targetReturn float64, evaluatedAt time.Time) SignalOutcome {
	date := signalDate(signal)
	outcome := SignalOutcome{
		Key: signal.Symbol + ":" + date + ":" + fmt.Sprint(horizon), SignalID: signal.ID, Symbol: signal.Symbol,
		Name: signal.Name, Industry: signal.Industry, SignalDate: date, SignalAsOf: signal.AsOf,
		Score: signal.Score, RiskAdjustedScore: signal.RiskAdjustedScore, RiskMultiplier: signal.RiskMultiplier,
		State: signal.State, MarketRegime: classifyMarketRegime(benchmark, date), Horizon: horizon, Status: OutcomeInvalid,
		CalibrationID: signal.CalibrationID, CalibratedScore: signal.CalibratedScore,
		CalibratedRiskAdjustedScore: signal.CalibratedRiskAdjustedScore, CalibratedMinimumScore: signal.CalibrationMinimumScore,
		CalibratedState: signal.CalibratedState,
		EvaluatedAt:     evaluatedAt, DataSource: signal.DataSource,
		StrategyScores: make(map[string]float64), StrategyStates: make(map[string]string), StrategyNames: make(map[string]string),
	}
	for _, component := range signal.Components {
		outcome.StrategyScores[component.Key] = component.Score
		outcome.StrategyStates[component.Key] = component.State
		outcome.StrategyNames[component.Key] = component.Name
	}
	if date == "" || len(bars) == 0 {
		outcome.Warning = "信号日期或历史日K无效"
		return outcome
	}
	index := sort.Search(len(bars), func(i int) bool { return bars[i].Date > date })
	if index >= len(bars) {
		outcome.Status = OutcomePending
		outcome.Warning = "尚无信号日之后的交易日"
		return outcome
	}
	if len(bars)-index < horizon {
		outcome.Status = OutcomePending
		outcome.EntryPrice = bars[index].Open
		outcome.Warning = fmt.Sprintf("信号后仅有%d个交易日，尚不足%d日窗口", len(bars)-index, horizon)
		return outcome
	}
	entry := bars[index]
	exit := bars[index+horizon-1]
	if entry.Open <= 0 || exit.Close <= 0 {
		outcome.Warning = "未来窗口价格无效"
		return outcome
	}
	outcome.Status = OutcomeReady
	outcome.TargetDate = exit.Date
	outcome.EntryPrice = entry.Open
	outcome.ExitPrice = exit.Close
	outcome.ReturnPercent = (exit.Close/entry.Open - 1) * 100
	outcome.MaxFavorable, outcome.MaxAdverse = -math.MaxFloat64, math.MaxFloat64
	for _, bar := range bars[index : index+horizon] {
		outcome.MaxFavorable = math.Max(outcome.MaxFavorable, (bar.High/entry.Open-1)*100)
		outcome.MaxAdverse = math.Min(outcome.MaxAdverse, (bar.Low/entry.Open-1)*100)
	}
	if !finite(outcome.MaxFavorable) {
		outcome.MaxFavorable = 0
	}
	if !finite(outcome.MaxAdverse) {
		outcome.MaxAdverse = 0
	}
	outcome.HitTarget = outcome.MaxFavorable >= targetReturn
	if signal.TriggerPrice > 0 {
		for _, bar := range bars[index : index+horizon] {
			if bar.High >= signal.TriggerPrice {
				outcome.HitTrigger = true
				break
			}
		}
	}
	if signal.InvalidationPrice > 0 {
		for _, bar := range bars[index : index+horizon] {
			if bar.Low <= signal.InvalidationPrice {
				outcome.HitInvalidation = true
				break
			}
		}
	}
	if benchmarkEntry, benchmarkExit, ok := alignedBenchmark(benchmark, entry.Date, exit.Date); ok && benchmarkEntry.Open > 0 {
		outcome.BenchmarkAvailable = true
		outcome.BenchmarkReturn = (benchmarkExit.Close/benchmarkEntry.Open - 1) * 100
		outcome.ExcessReturn = outcome.ReturnPercent - outcome.BenchmarkReturn
	}
	return outcome
}

func alignedBenchmark(bars []domain.DailyBar, entryDate, exitDate string) (domain.DailyBar, domain.DailyBar, bool) {
	if len(bars) == 0 {
		return domain.DailyBar{}, domain.DailyBar{}, false
	}
	start := sort.Search(len(bars), func(i int) bool { return bars[i].Date >= entryDate })
	end := sort.Search(len(bars), func(i int) bool { return bars[i].Date >= exitDate })
	if start >= len(bars) || end >= len(bars) || start > end || bars[start].Date != entryDate || bars[end].Date != exitDate {
		return domain.DailyBar{}, domain.DailyBar{}, false
	}
	return bars[start], bars[end], true
}

func emptySummaries(horizons []int) []OutcomeSummary {
	result := make([]OutcomeSummary, 0, len(horizons))
	for _, horizon := range horizons {
		result = append(result, OutcomeSummary{Horizon: horizon})
	}
	return result
}

func BuildOutcomeReport(outcomes []SignalOutcome, now time.Time, warnings []string) OutcomeReport {
	horizons := make([]int, 0)
	seenHorizon := make(map[int]bool)
	for _, outcome := range outcomes {
		if !seenHorizon[outcome.Horizon] {
			seenHorizon[outcome.Horizon] = true
			horizons = append(horizons, outcome.Horizon)
		}
	}
	sort.Ints(horizons)
	report := OutcomeReport{GeneratedAt: now, AsOf: now.Format("2006-01-02"), Horizons: horizons, Warnings: uniqueStrings(warnings, 20)}
	for _, outcome := range outcomes {
		if outcome.TargetDate > report.DataThrough {
			report.DataThrough = outcome.TargetDate
		}
	}
	report.Summaries = summarizeOutcomes(outcomes, horizons)
	report.Strategies = strategyBreakdowns(outcomes, horizons)
	report.Scores = breakdownOutcomes(outcomes, horizons, func(item SignalOutcome) []OutcomeBreakdown {
		bucket := scoreBucket(item.Score)
		return []OutcomeBreakdown{{Key: bucket, Label: bucket}}
	}, func(item SignalOutcome) []string { return []string{scoreBucket(item.Score)} })
	report.States = breakdownOutcomes(outcomes, horizons, func(item SignalOutcome) []OutcomeBreakdown {
		label := outcomeStateLabel(item.State)
		return []OutcomeBreakdown{{Key: label, Label: label}}
	}, func(item SignalOutcome) []string { return []string{outcomeStateLabel(item.State)} })
	report.Regimes = breakdownOutcomes(outcomes, horizons, func(item SignalOutcome) []OutcomeBreakdown {
		regime := item.MarketRegime
		if strings.TrimSpace(regime) == "" {
			regime = MarketRegimeUnlabeled
		}
		return []OutcomeBreakdown{{Key: regime, Label: regime}}
	}, func(item SignalOutcome) []string {
		regime := item.MarketRegime
		if strings.TrimSpace(regime) == "" {
			regime = MarketRegimeUnlabeled
		}
		return []string{regime}
	})
	report.ComponentAnalysis = buildComponentAnalysis(outcomes, horizons)
	report.WalkForward = buildComponentWalkForwardAnalysis(outcomes, horizons)
	report.Portfolio = buildPortfolioConstraintAnalysis(outcomes, horizons, report.ComponentAnalysis)
	report.Assessment = buildOutcomeAssessment(outcomes, horizons, report.ComponentAnalysis, report.WalkForward, report.Portfolio)
	report.Calibration = buildCalibrationAnalysis(outcomes, horizons)
	report.Tuning = buildTuningAnalysis(outcomes, horizons, report.Assessment, report.ComponentAnalysis, report.WalkForward)
	report.Tuning.DataThrough = report.DataThrough
	report.Recent = recentOutcomes(outcomes, 24)
	return report
}

// classifyMarketRegime labels the market using only benchmark bars strictly
// before the signal date. This keeps the context label point-in-time even when
// the signal was emitted during an unfinished trading session.
func classifyMarketRegime(bars []domain.DailyBar, signalDate string) string {
	return string(marketregime.ClassifyBefore(bars, signalDate))
}

func summarizeOutcomes(outcomes []SignalOutcome, horizons []int) []OutcomeSummary {
	result := make([]OutcomeSummary, 0, len(horizons))
	for _, horizon := range horizons {
		items := make([]SignalOutcome, 0)
		for _, item := range outcomes {
			if item.Horizon == horizon {
				items = append(items, item)
			}
		}
		result = append(result, summarize(items, horizon))
	}
	return result
}

func summarize(items []SignalOutcome, horizon int) OutcomeSummary {
	return summarizeWithScores(items, horizon, func(item SignalOutcome) (float64, bool) {
		return item.Score, finite(item.Score)
	})
}

func summarizeWithScores(items []SignalOutcome, horizon int, scoreFor func(SignalOutcome) (float64, bool)) OutcomeSummary {
	result := OutcomeSummary{Horizon: horizon, Total: len(items)}
	returns, excess, favorable, adverse := []float64{}, []float64{}, []float64{}, []float64{}
	icReturns, scores := []float64{}, []float64{}
	for _, item := range items {
		switch item.Status {
		case OutcomeReady:
			result.Ready++
		case OutcomePending:
			result.Pending++
		default:
			result.Invalid++
		}
		if item.Status != OutcomeReady {
			continue
		}
		if item.ReturnPercent > 0 {
			result.Positive++
		}
		returns = append(returns, item.ReturnPercent)
		if score, ok := scoreFor(item); ok && finite(score) {
			scores = append(scores, score)
			icReturns = append(icReturns, item.ReturnPercent)
		}
		if item.BenchmarkAvailable {
			excess = append(excess, item.ExcessReturn)
			if item.ExcessReturn > 0 {
				result.PositiveExcess++
			}
		}
		favorable = append(favorable, item.MaxFavorable)
		adverse = append(adverse, item.MaxAdverse)
		if item.HitTrigger {
			result.TriggerHitRate++
		}
		if item.HitTarget {
			result.TargetHitRate++
		}
		if item.HitInvalidation {
			result.InvalidationRate++
		}
	}
	if result.Total > 0 {
		result.CoveragePercent = float64(result.Ready) / float64(result.Total) * 100
	}
	if result.Ready > 0 {
		result.HitRatePercent = float64(result.Positive) / float64(result.Ready) * 100
		result.TriggerHitRate = result.TriggerHitRate / float64(result.Ready) * 100
		result.TargetHitRate = result.TargetHitRate / float64(result.Ready) * 100
		result.InvalidationRate = result.InvalidationRate / float64(result.Ready) * 100
		result.AverageReturn, result.MedianReturn = meanMedian(returns)
		result.AverageExcess, _ = meanMedian(excess)
		result.AverageFavorable, _ = meanMedian(favorable)
		result.AverageAdverse, _ = meanMedian(adverse)
		result.InformationCoefficient = pearson(scores, icReturns)
		result.RankInformationCoefficient = pearson(averageRanks(scores), averageRanks(icReturns))
		result.SampleSufficient = result.Ready >= 30
	}
	if len(excess) > 0 {
		result.ExcessHitRate = float64(result.PositiveExcess) / float64(len(excess)) * 100
	}
	return result
}

func buildCalibrationAnalysis(outcomes []SignalOutcome, horizons []int) CalibrationAnalysis {
	horizon := OutcomeHorizon5D
	if !containsInt(horizons, horizon) && len(horizons) > 0 {
		horizon = horizons[len(horizons)-1]
	}
	analysis := CalibrationAnalysis{Horizon: horizon, Status: "暂无 Challenger", Recommendation: "等待通过滚动验证门禁的校准候选"}
	ready := make([]SignalOutcome, 0)
	latestID := ""
	var latestAt time.Time
	for _, item := range outcomes {
		if item.Horizon != horizon {
			continue
		}
		if item.Status == OutcomeReady && item.BenchmarkAvailable {
			ready = append(ready, item)
		}
		if strings.TrimSpace(item.CalibrationID) == "" {
			continue
		}
		at := item.SignalAsOf
		if at.IsZero() {
			at, _ = time.ParseInLocation("2006-01-02", item.SignalDate, time.Local)
		}
		if latestID == "" || at.After(latestAt) || (at.Equal(latestAt) && item.CalibrationID > latestID) {
			latestID = strings.TrimSpace(item.CalibrationID)
			latestAt = at
		}
	}
	analysis.ReadySamples = len(ready)
	analysis.ID = latestID
	if latestID == "" {
		return analysis
	}
	comparison := make([]SignalOutcome, 0)
	for _, item := range ready {
		if strings.TrimSpace(item.CalibrationID) != latestID || !finite(calibratedOutcomeScore(item)) {
			continue
		}
		comparison = append(comparison, item)
	}
	analysis.ComparisonSamples = len(comparison)
	championItems := filterCalibrationItems(comparison, false, 55)
	challengerThreshold := 55.0
	for _, item := range comparison {
		if item.CalibratedMinimumScore > 0 && finite(item.CalibratedMinimumScore) {
			challengerThreshold = item.CalibratedMinimumScore
			break
		}
	}
	challengerItems := filterCalibrationItems(comparison, true, challengerThreshold)
	analysis.Champion = calibrationMetric(championItems, func(item SignalOutcome) (float64, bool) {
		value := baselineOutcomeScore(item)
		return value, finite(value)
	})
	analysis.Challenger = calibrationMetric(challengerItems, func(item SignalOutcome) (float64, bool) {
		value := calibratedOutcomeScore(item)
		return value, finite(value)
	})
	// Rank IC is evaluated on the complete common universe, while return and
	// hit-rate metrics reflect the stocks each model would actually select.
	analysis.Champion.RankIC = calibrationRankIC(comparison, func(item SignalOutcome) (float64, bool) {
		value := baselineOutcomeScore(item)
		return value, finite(value)
	})
	analysis.Challenger.RankIC = calibrationRankIC(comparison, func(item SignalOutcome) (float64, bool) {
		value := calibratedOutcomeScore(item)
		return value, finite(value)
	})
	analysis.ExcessUplift = analysis.Challenger.AverageExcess - analysis.Champion.AverageExcess
	analysis.HitRateUplift = analysis.Challenger.HitRate - analysis.Champion.HitRate
	analysis.RankICUplift = analysis.Challenger.RankIC - analysis.Champion.RankIC
	if len(comparison) < minimumCalibrationComparisonSamples ||
		analysis.Champion.Samples < minimumCalibrationComparisonSamples || analysis.Challenger.Samples < minimumCalibrationComparisonSamples {
		analysis.Status = "观察中"
		analysis.Recommendation = fmt.Sprintf("继续积累两套模型的可交易成熟样本（共同%d、Champion%d、Challenger%d；门槛%d），暂不替换 Champion", len(comparison), analysis.Champion.Samples, analysis.Challenger.Samples, minimumCalibrationComparisonSamples)
		return analysis
	}
	if analysis.ExcessUplift >= minimumCalibrationExcessUplift && analysis.HitRateUplift >= minimumCalibrationHitRateUplift {
		analysis.Status = "候选领先"
		analysis.Recommendation = "候选在同样本上领先；保留并进入人工晋级复核，不自动修改 Champion"
		return analysis
	}
	if analysis.ExcessUplift <= maximumCalibrationExcessDrawdown || analysis.HitRateUplift <= maximumCalibrationHitRateDrawdown {
		analysis.Status = "候选落后"
		analysis.Recommendation = "候选表现落后；暂停晋级，继续使用 Champion 并检查权重、阈值和市场状态"
		return analysis
	}
	analysis.Status = "暂无显著差异"
	analysis.Recommendation = "Champion 与 Challenger 差异尚不显著，继续观察更多非重叠窗口"
	return analysis
}

func buildTuningAnalysis(outcomes []SignalOutcome, horizons []int, assessment OutcomeAssessment, components ComponentAnalysis, walkForward ComponentWalkForwardAnalysis) TuningAnalysis {
	horizon := OutcomeHorizon5D
	if !containsInt(horizons, horizon) && len(horizons) > 0 {
		horizon = horizons[len(horizons)-1]
	}
	items := uniqueOutcomeObservationsForHorizon(outcomes, horizon)
	dates := distinctOutcomeDates(items)
	analysis := TuningAnalysis{
		Horizon:     horizon,
		Status:      "样本收集中",
		MatureDates: len(dates),
		Summary:     fmt.Sprintf("当前 %d 日窗口已形成 %d 个成熟、具备基准覆盖的代表样本，覆盖 %d 个交易日", horizon, len(items), len(dates)),
	}
	if len(items) > 0 {
		summary := summarize(items, horizon)
		analysis.MatureSamples = summary.Ready
		analysis.AverageExcess = summary.AverageExcess
		analysis.HitRate = summary.HitRatePercent
		analysis.Summary = fmt.Sprintf("当前 %d 日窗口成熟样本 %d 个，覆盖 %d 个交易日，正收益率 %.1f%%，平均超额 %+.2f%%", horizon, summary.Ready, len(dates), summary.HitRatePercent, summary.AverageExcess)
	}
	if assessment.Threshold != nil && finite(assessment.Threshold.MinimumScore) && assessment.Threshold.MinimumScore > 0 {
		analysis.Summary += fmt.Sprintf("；当前滚动候选最低风险调整分 %.0f", assessment.Threshold.MinimumScore)
	}
	add := func(key, priority, action, evidence string) {
		if strings.TrimSpace(action) == "" {
			return
		}
		for _, item := range analysis.Recommendations {
			if item.Key == key {
				return
			}
		}
		analysis.Recommendations = append(analysis.Recommendations, TuningRecommendation{Key: key, Priority: priority, Action: action, Evidence: evidence})
	}
	if len(items) < minimumResearchSamples {
		add("sample", "high", fmt.Sprintf("继续采集 %d 日成熟结果后再调整阈值和组件权重", horizon), fmt.Sprintf("当前 %d/%d 个样本，尚不足滚动校准门槛", len(items), minimumResearchSamples))
	} else if len(dates) < minimumResearchDates {
		analysis.Status = "时间样本不足"
		add("dates", "high", fmt.Sprintf("继续积累至少 %d 个不同交易日的成熟结果，再进行时间顺序调优", minimumResearchDates), fmt.Sprintf("当前仅覆盖 %d/%d 个交易日；同一交易日的多只股票不能替代时间样本", len(dates), minimumResearchDates))
	} else if assessment.Verdict == "可作为下一轮候选" {
		analysis.Status = "Challenger待前向"
		add("challenger", "high", "将通过历史门禁的候选放入自适应影子账户，进行同样本前向对照；暂不自动替换 Champion", assessment.NextStage)
	} else {
		analysis.Status = "滚动验证中"
		add("champion", "medium", "继续使用当前 Champion，等待更多非重叠窗口后再决定是否调权", assessment.Verdict)
	}
	// Keep the daily review actionable even when a hard gate blocks a
	// Challenger. The recommendation names the failing evidence rather than
	// presenting a generic "等待" state, so the next data collection or model
	// change is explicit and auditable.
	for _, check := range assessment.Checks {
		if !check.Required || check.Passed {
			continue
		}
		switch check.Key {
		case "time-slices":
			add("gate:time-slices", "high", "继续积累独立交易日，禁止用同日横截面样本替代时间验证", check.Detail)
		case "component-correlation":
			add("gate:correlation", "high", "对高度相关组件做去重或收缩权重，再重新跑时间顺序验证", check.Detail)
		case "excess":
			add("gate:excess", "high", "暂不提高仓位；复核入场门槛、市场状态和负超额窗口", check.Detail)
		case "hit-rate":
			add("gate:hit-rate", "high", "降低低命中组件的影响并检查信号触发条件，等待后续窗口确认", check.Detail)
		case "benchmark":
			add("gate:benchmark", "high", "补齐沪深300同期基准数据后再进行调优", check.Detail)
		case "sample":
			add("gate:sample", "high", "继续收集成熟前向结果，当前不修改线上权重", check.Detail)
		}
	}
	for _, coverage := range components.Coverage {
		if coverage.AvailablePercent >= minimumComponentCoverage {
			continue
		}
		add("coverage:"+coverage.Key, "high", fmt.Sprintf("补齐%s数据后再把它用于调优", coverage.Name), fmt.Sprintf("覆盖率 %.1f%%，要求至少 %.0f%%", coverage.AvailablePercent, minimumComponentCoverage))
	}
	for _, metric := range walkForward.Metrics {
		if metric.Horizon != horizon || !metric.SampleSufficient {
			continue
		}
		switch metric.State {
		case "negative":
			add("negative:"+metric.ComponentKey, "high", fmt.Sprintf("降低或暂缓%s的权重，先复核其失效市场状态", metric.ComponentName), fmt.Sprintf("滚动验证平均超额 %+.2f%%，负向折 %d", metric.ValidationAverageExcess, metric.NegativeFolds))
		case "unstable":
			add("unstable:"+metric.ComponentKey, "medium", fmt.Sprintf("保持%s接近中性权重，等待权重漂移收敛", metric.ComponentName), fmt.Sprintf("权重漂移 %.1f%%，上限 %.1f%%", metric.WeightDrift*100, maximumComponentWeightDrift*100))
		}
	}
	if len(analysis.Recommendations) == 0 {
		add("observe", "low", "继续观察新的成熟结果和市场状态分层表现", "当前没有足够证据支持新的参数动作")
	}
	return analysis
}

func calibratedOutcomeScore(item SignalOutcome) float64 {
	if finite(item.CalibratedRiskAdjustedScore) && item.CalibratedRiskAdjustedScore > 0 {
		return item.CalibratedRiskAdjustedScore
	}
	return item.CalibratedScore
}

// baselineOutcomeScore keeps Champion and Challenger on the same scale. New
// signals carry the risk-adjusted score used by portfolio gates; older rows
// only have the raw composite score and remain backwards compatible.
func baselineOutcomeScore(item SignalOutcome) float64 {
	if finite(item.RiskAdjustedScore) && item.RiskAdjustedScore > 0 {
		return item.RiskAdjustedScore
	}
	return item.Score
}

func filterCalibrationItems(items []SignalOutcome, challenger bool, threshold float64) []SignalOutcome {
	result := make([]SignalOutcome, 0, len(items))
	for _, item := range items {
		score := baselineOutcomeScore(item)
		state := item.State
		if challenger {
			score = calibratedOutcomeScore(item)
			state = item.CalibratedState
		}
		if !finite(score) || score < threshold {
			continue
		}
		// Old outcome rows may predate explicit calibrated states. Their score is
		// still usable for comparison; new rows carry the stricter state gate.
		if state != "" && state != StateTriggered && state != StateWatching {
			continue
		}
		result = append(result, item)
	}
	return result
}

func calibrationMetric(items []SignalOutcome, scoreFor func(SignalOutcome) (float64, bool)) CalibrationMetric {
	metric := CalibrationMetric{}
	scores, returns, excess := make([]float64, 0, len(items)), make([]float64, 0, len(items)), make([]float64, 0, len(items))
	for _, item := range items {
		score, ok := scoreFor(item)
		if !ok || item.Status != OutcomeReady || !item.BenchmarkAvailable {
			continue
		}
		metric.Samples++
		if item.ReturnPercent > 0 {
			metric.Positive++
		}
		returns = append(returns, item.ReturnPercent)
		excess = append(excess, item.ExcessReturn)
		scores = append(scores, score)
	}
	if metric.Samples == 0 {
		return metric
	}
	metric.HitRate = float64(metric.Positive) / float64(metric.Samples) * 100
	metric.AverageReturn, _ = meanMedian(returns)
	metric.AverageExcess, _ = meanMedian(excess)
	metric.RankIC = pearson(averageRanks(scores), averageRanks(excess))
	return metric
}

func calibrationRankIC(items []SignalOutcome, scoreFor func(SignalOutcome) (float64, bool)) float64 {
	scores, excess := make([]float64, 0, len(items)), make([]float64, 0, len(items))
	for _, item := range items {
		if item.Status != OutcomeReady || !item.BenchmarkAvailable {
			continue
		}
		score, ok := scoreFor(item)
		if !ok || !finite(score) || !finite(item.ExcessReturn) {
			continue
		}
		scores = append(scores, score)
		excess = append(excess, item.ExcessReturn)
	}
	return pearson(averageRanks(scores), averageRanks(excess))
}

func strategyBreakdowns(outcomes []SignalOutcome, horizons []int) []OutcomeBreakdown {
	labels := make(map[string]string)
	for _, item := range outcomes {
		for _, key := range activeStrategyKeys(item) {
			label := strings.TrimSpace(item.StrategyNames[key])
			if label == "" {
				label = key
			}
			labels[key] = label
		}
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]OutcomeBreakdown, 0, len(keys))
	for _, key := range keys {
		items := make([]SignalOutcome, 0)
		for _, item := range outcomes {
			if activeStrategy(item, key) {
				items = append(items, item)
			}
		}
		summaries := make([]OutcomeSummary, 0, len(horizons))
		for _, horizon := range horizons {
			horizonItems := make([]SignalOutcome, 0)
			for _, item := range items {
				if item.Horizon == horizon {
					horizonItems = append(horizonItems, item)
				}
			}
			summaries = append(summaries, summarizeWithScores(horizonItems, horizon, func(item SignalOutcome) (float64, bool) {
				score, ok := item.StrategyScores[key]
				return score, ok
			}))
		}
		result = append(result, OutcomeBreakdown{Key: key, Label: labels[key], Signals: uniqueOutcomeSignals(items), Summaries: summaries})
	}
	return result
}

func buildComponentAnalysis(outcomes []SignalOutcome, horizons []int) ComponentAnalysis {
	observations := uniqueComponentObservations(outcomes)
	components := componentCatalog(outcomes)
	analysis := ComponentAnalysis{
		Signals: len(observations), MinimumCorrelationSamples: minimumCorrelationSamples,
		MinimumRegimeSamples: minimumRegimeSamples, Components: components,
	}
	analysis.Coverage = componentCoverage(observations, components)
	analysis.Correlations, analysis.SufficientPairs, analysis.RedundantPairs = componentCorrelations(observations, components)
	analysis.RegimeMetrics, analysis.SufficientRegimeCells, analysis.PositiveRegimeCells,
		analysis.NegativeRegimeCells, analysis.MixedRegimeCells = componentRegimeMetrics(outcomes, horizons, components)
	return analysis
}

func componentCatalog(outcomes []SignalOutcome) []ComponentDescriptor {
	seen := make(map[string]bool)
	result := make([]ComponentDescriptor, 0)
	for _, item := range DefaultStrategies() {
		seen[item.Key()] = true
		result = append(result, ComponentDescriptor{Key: item.Key(), Name: item.Name()})
	}
	extraNames := make(map[string]string)
	for _, outcome := range outcomes {
		for key := range outcome.StrategyScores {
			if seen[key] {
				continue
			}
			name := strings.TrimSpace(outcome.StrategyNames[key])
			if name == "" {
				name = key
			}
			extraNames[key] = name
		}
	}
	extraKeys := make([]string, 0, len(extraNames))
	for key := range extraNames {
		extraKeys = append(extraKeys, key)
	}
	sort.Strings(extraKeys)
	for _, key := range extraKeys {
		result = append(result, ComponentDescriptor{Key: key, Name: extraNames[key]})
	}
	return result
}

func uniqueComponentObservations(outcomes []SignalOutcome) []SignalOutcome {
	bySignal := make(map[string]SignalOutcome)
	for _, item := range outcomes {
		key := item.Symbol + "|" + item.SignalDate
		if strings.Trim(key, "|") == "" {
			key = item.SignalID
		}
		if key == "" {
			continue
		}
		previous, found := bySignal[key]
		if !found || len(item.StrategyScores) > len(previous.StrategyScores) ||
			(len(item.StrategyScores) == len(previous.StrategyScores) && item.EvaluatedAt.After(previous.EvaluatedAt)) {
			bySignal[key] = item
		}
	}
	result := make([]SignalOutcome, 0, len(bySignal))
	for _, item := range bySignal {
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].SignalAsOf.Equal(result[j].SignalAsOf) {
			return result[i].Symbol < result[j].Symbol
		}
		return result[i].SignalAsOf.Before(result[j].SignalAsOf)
	})
	return result
}

func componentAvailable(item SignalOutcome, key string) (float64, bool) {
	score, found := item.StrategyScores[key]
	if !found || !finite(score) || strings.TrimSpace(item.StrategyStates[key]) == "数据不足" {
		return 0, false
	}
	return score, true
}

func componentCoverage(observations []SignalOutcome, components []ComponentDescriptor) []ComponentCoverage {
	result := make([]ComponentCoverage, 0, len(components))
	for _, descriptor := range components {
		coverage := ComponentCoverage{Key: descriptor.Key, Name: descriptor.Name, Signals: len(observations)}
		totalScore := 0.0
		for _, item := range observations {
			score, available := componentAvailable(item, descriptor.Key)
			if !available {
				continue
			}
			coverage.Available++
			totalScore += score
			if score >= minimumActiveStrategyScore {
				coverage.Active++
			}
		}
		if coverage.Signals > 0 {
			coverage.AvailablePercent = float64(coverage.Available) / float64(coverage.Signals) * 100
			coverage.ActivePercent = float64(coverage.Active) / float64(coverage.Signals) * 100
		}
		if coverage.Available > 0 {
			coverage.AverageScore = totalScore / float64(coverage.Available)
		}
		result = append(result, coverage)
	}
	return result
}

func componentCorrelations(observations []SignalOutcome, components []ComponentDescriptor) ([]ComponentCorrelation, int, int) {
	result := make([]ComponentCorrelation, 0, len(components)*len(components))
	sufficientPairs, redundantPairs := 0, 0
	for leftIndex, left := range components {
		for rightIndex, right := range components {
			leftScores, rightScores := make([]float64, 0), make([]float64, 0)
			for _, item := range observations {
				leftScore, leftOK := componentAvailable(item, left.Key)
				rightScore, rightOK := componentAvailable(item, right.Key)
				if !leftOK || !rightOK {
					continue
				}
				leftScores = append(leftScores, leftScore)
				rightScores = append(rightScores, rightScore)
			}
			cell := ComponentCorrelation{LeftKey: left.Key, RightKey: right.Key, Samples: len(leftScores)}
			if left.Key == right.Key {
				if cell.Samples > 0 {
					cell.Correlation, cell.RankCorrelation = 1, 1
				}
				cell.SampleSufficient = cell.Samples >= minimumCorrelationSamples
				cell.Relation = "self"
				result = append(result, cell)
				continue
			}
			cell.Correlation = pearson(leftScores, rightScores)
			cell.RankCorrelation = pearson(averageRanks(leftScores), averageRanks(rightScores))
			cell.SampleSufficient = cell.Samples >= minimumCorrelationSamples
			switch {
			case !cell.SampleSufficient:
				cell.Relation = "insufficient"
			case cell.RankCorrelation >= redundantRankCorrelation:
				cell.Relation = "overlap"
			case cell.RankCorrelation <= -redundantRankCorrelation:
				cell.Relation = "inverse"
			default:
				cell.Relation = "distinct"
			}
			if leftIndex < rightIndex && cell.SampleSufficient {
				sufficientPairs++
				if cell.Relation == "overlap" {
					redundantPairs++
				}
			}
			result = append(result, cell)
		}
	}
	return result, sufficientPairs, redundantPairs
}

func componentRegimeMetrics(outcomes []SignalOutcome, horizons []int, components []ComponentDescriptor) ([]ComponentRegimeMetric, int, int, int, int) {
	regimes := []string{MarketRegimeBull, MarketRegimeBear, MarketRegimeRange, MarketRegimeHighVol}
	result := make([]ComponentRegimeMetric, 0, len(horizons)*len(components)*len(regimes))
	sufficient, positive, negative, mixed := 0, 0, 0, 0
	for _, horizon := range horizons {
		for _, component := range components {
			for _, regime := range regimes {
				scores, excess, activeExcess := make([]float64, 0), make([]float64, 0), make([]float64, 0)
				positiveActive := 0
				for _, item := range outcomes {
					if item.Horizon != horizon || item.Status != OutcomeReady || !item.BenchmarkAvailable || item.MarketRegime != regime {
						continue
					}
					score, available := componentAvailable(item, component.Key)
					if !available {
						continue
					}
					scores = append(scores, score)
					excess = append(excess, item.ExcessReturn)
					if score >= minimumActiveStrategyScore {
						activeExcess = append(activeExcess, item.ExcessReturn)
						if item.ExcessReturn > 0 {
							positiveActive++
						}
					}
				}
				metric := ComponentRegimeMetric{
					ComponentKey: component.Key, ComponentName: component.Name, Regime: regime, Horizon: horizon,
					Samples: len(scores), ActiveSamples: len(activeExcess), State: "insufficient",
				}
				if len(scores) >= 2 {
					metric.RankIC = pearson(averageRanks(scores), averageRanks(excess))
				}
				if len(activeExcess) > 0 {
					metric.AverageExcess, _ = meanMedian(activeExcess)
					metric.ActiveHitRate = float64(positiveActive) / float64(len(activeExcess)) * 100
				}
				metric.SampleSufficient = metric.Samples >= minimumRegimeSamples && metric.ActiveSamples >= minimumFoldSamples
				if metric.SampleSufficient {
					sufficient++
					switch {
					case metric.RankIC > 0 && metric.AverageExcess > 0:
						metric.State = "positive"
						positive++
					case metric.RankIC < 0 && metric.AverageExcess < 0:
						metric.State = "negative"
						negative++
					default:
						metric.State = "mixed"
						mixed++
					}
				}
				result = append(result, metric)
			}
		}
	}
	return result, sufficient, positive, negative, mixed
}

func buildComponentWalkForwardAnalysis(outcomes []SignalOutcome, horizons []int) ComponentWalkForwardAnalysis {
	analysis := ComponentWalkForwardAnalysis{
		MinimumSamples:       minimumComponentValidationSamples,
		MinimumActiveSamples: minimumComponentValidationActiveSamples,
		MinimumFolds:         minimumComponentValidationFolds,
		MaximumWeightDrift:   maximumComponentWeightDrift,
		Metrics:              make([]ComponentValidationMetric, 0),
	}
	components := componentCatalog(outcomes)
	for _, horizon := range horizons {
		observations := uniqueOutcomeObservationsForHorizon(outcomes, horizon)
		for _, component := range components {
			items := make([]SignalOutcome, 0)
			for _, item := range observations {
				if item.Status == OutcomeReady && item.BenchmarkAvailable {
					if _, ok := componentAvailable(item, component.Key); ok {
						items = append(items, item)
					}
				}
			}
			sort.SliceStable(items, func(i, j int) bool {
				if items[i].SignalDate == items[j].SignalDate {
					return items[i].SignalAsOf.Before(items[j].SignalAsOf)
				}
				return items[i].SignalDate < items[j].SignalDate
			})
			metric := ComponentValidationMetric{ComponentKey: component.Key, ComponentName: component.Name, Horizon: horizon, AvailableSamples: len(items), State: "insufficient"}
			folds := componentValidationFolds(observations, component.Key, components)
			metric.Folds = folds
			weightValues := make([]float64, 0, len(folds))
			for _, fold := range folds {
				metric.ValidationSamples += fold.ValidationSamples
				metric.ValidationActive += fold.ValidationActiveSamples
				if fold.SampleSufficient {
					metric.SufficientFolds++
					if fold.ValidationAverageExcess > 0 {
						metric.PositiveFolds++
					} else if fold.ValidationAverageExcess < 0 {
						metric.NegativeFolds++
					}
					if fold.WeightAvailable {
						weightValues = append(weightValues, fold.CandidateWeight)
					}
				}
				if fold.SampleSufficient {
					metric.ValidationAverageExcess += fold.ValidationAverageExcess * float64(fold.ValidationActiveSamples)
					metric.ValidationHitRate += fold.ValidationHitRate * float64(fold.ValidationActiveSamples)
					metric.ValidationRankIC += fold.ValidationRankIC * float64(fold.ValidationSamples)
				}
			}
			rankDenominator := 0.0
			if len(folds) > 0 {
				for _, fold := range folds {
					if fold.SampleSufficient {
						rankDenominator += float64(fold.ValidationSamples)
					}
				}
			}
			if rankDenominator > 0 {
				metric.ValidationRankIC /= rankDenominator
			}
			activeDenominator := 0
			for _, fold := range folds {
				if fold.SampleSufficient {
					activeDenominator += fold.ValidationActiveSamples
				}
			}
			if activeDenominator > 0 {
				metric.ValidationAverageExcess /= float64(activeDenominator)
				metric.ValidationHitRate /= float64(activeDenominator)
			}
			if len(weightValues) > 0 {
				metric.MinimumWeight, metric.MaximumWeight = weightValues[0], weightValues[0]
				for _, value := range weightValues[1:] {
					metric.MinimumWeight = math.Min(metric.MinimumWeight, value)
					metric.MaximumWeight = math.Max(metric.MaximumWeight, value)
				}
				metric.WeightDrift = metric.MaximumWeight - metric.MinimumWeight
			}
			metric.SampleSufficient = len(items) >= minimumComponentValidationSamples && metric.SufficientFolds >= minimumComponentValidationFolds && metric.ValidationActive >= minimumComponentValidationActiveSamples
			metric.WeightStable = len(weightValues) >= minimumComponentValidationFolds && metric.WeightDrift <= maximumComponentWeightDrift
			if metric.SampleSufficient && metric.WeightStable {
				switch {
				case metric.PositiveFolds >= minimumComponentValidationFolds && metric.ValidationAverageExcess > 0:
					metric.State = "positive"
				case metric.NegativeFolds >= minimumComponentValidationFolds && metric.ValidationAverageExcess < 0:
					metric.State = "negative"
				default:
					metric.State = "mixed"
				}
			} else if metric.SampleSufficient {
				metric.State = "unstable"
			}
			analysis.Metrics = append(analysis.Metrics, metric)
		}
	}
	return analysis
}

func componentValidationFolds(items []SignalOutcome, componentKey string, components []ComponentDescriptor) []ComponentValidationFold {
	if len(items) < minimumComponentValidationSamples {
		return nil
	}
	dates := make([]string, 0)
	seenDates := make(map[string]bool)
	for _, item := range items {
		if !seenDates[item.SignalDate] {
			seenDates[item.SignalDate] = true
			dates = append(dates, item.SignalDate)
		}
	}
	if len(dates) < minimumComponentValidationFolds+1 {
		return nil
	}
	trainDays := len(dates) / 2
	validationDays := (len(dates) - trainDays) / minimumComponentValidationFolds
	if trainDays < 1 || validationDays < 1 {
		return nil
	}
	folds := make([]ComponentValidationFold, 0, minimumComponentValidationFolds)
	for index := 0; index < minimumComponentValidationFolds; index++ {
		trainEnd := trainDays + index*validationDays
		validationEnd := trainEnd + validationDays
		if index == minimumComponentValidationFolds-1 || validationEnd > len(dates) {
			validationEnd = len(dates)
		}
		trainWindow := filterComponentDates(items, dates[:trainEnd])
		validationWindow := filterComponentDates(items, dates[trainEnd:validationEnd])
		train := availableComponentItems(trainWindow, componentKey)
		validation := availableComponentItems(validationWindow, componentKey)
		candidateWeight, weightAvailable := componentCandidateWeight(trainWindow, componentKey, components)
		validationActive := make([]SignalOutcome, 0)
		validationScores, validationExcess := make([]float64, 0), make([]float64, 0)
		for _, item := range validation {
			score, ok := componentAvailable(item, componentKey)
			if !ok {
				continue
			}
			validationScores = append(validationScores, score)
			if score >= minimumActiveStrategyScore {
				validationActive = append(validationActive, item)
			}
			validationExcess = append(validationExcess, item.ExcessReturn)
		}
		activeExcess := make([]float64, 0, len(validationActive))
		positive := 0
		for _, item := range validationActive {
			activeExcess = append(activeExcess, item.ExcessReturn)
			if item.ExcessReturn > 0 {
				positive++
			}
		}
		average := 0.0
		if len(activeExcess) > 0 {
			average, _ = meanMedian(activeExcess)
		}
		hitRate := 0.0
		if len(activeExcess) > 0 {
			hitRate = float64(positive) / float64(len(activeExcess)) * 100
		}
		fold := ComponentValidationFold{
			Index: index + 1, TrainEnd: dates[trainEnd-1], ValidationStart: dates[trainEnd], ValidationEnd: dates[validationEnd-1],
			TrainSamples: len(train), ValidationSamples: len(validationScores), ValidationActiveSamples: len(activeExcess),
			TrainRankIC: componentRankIC(train, componentKey), ValidationRankIC: pearson(averageRanks(validationScores), averageRanks(validationExcess)),
			ValidationAverageExcess: average, ValidationHitRate: hitRate, CandidateWeight: candidateWeight, WeightAvailable: weightAvailable,
			SampleSufficient: len(train) >= minimumCorrelationSamples && len(validationScores) >= minimumComponentValidationFoldSamples && len(activeExcess) >= minimumComponentValidationActiveSamples,
		}
		folds = append(folds, fold)
	}
	return folds
}

func uniqueOutcomeObservationsForHorizon(outcomes []SignalOutcome, horizon int) []SignalOutcome {
	bySignal := make(map[string]SignalOutcome)
	for _, item := range outcomes {
		if item.Horizon != horizon || item.Status != OutcomeReady || !item.BenchmarkAvailable {
			continue
		}
		key := item.Symbol + "|" + item.SignalDate
		if strings.Trim(key, "|") == "" {
			key = item.SignalID
		}
		if key == "" {
			continue
		}
		previous, found := bySignal[key]
		if !found || len(item.StrategyScores) > len(previous.StrategyScores) ||
			(len(item.StrategyScores) == len(previous.StrategyScores) && item.EvaluatedAt.After(previous.EvaluatedAt)) {
			bySignal[key] = item
		}
	}
	result := make([]SignalOutcome, 0, len(bySignal))
	for _, item := range bySignal {
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].SignalDate == result[j].SignalDate {
			return result[i].Symbol < result[j].Symbol
		}
		return result[i].SignalDate < result[j].SignalDate
	})
	return result
}

func distinctOutcomeDates(items []SignalOutcome) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		date := outcomeDate(item)
		if date == "" {
			continue
		}
		if _, ok := seen[date]; ok {
			continue
		}
		seen[date] = struct{}{}
		result = append(result, date)
	}
	sort.Strings(result)
	return result
}

func availableComponentItems(items []SignalOutcome, componentKey string) []SignalOutcome {
	result := make([]SignalOutcome, 0, len(items))
	for _, item := range items {
		if _, ok := componentAvailable(item, componentKey); ok {
			result = append(result, item)
		}
	}
	return result
}

func filterComponentDates(items []SignalOutcome, dates []string) []SignalOutcome {
	allowed := make(map[string]bool, len(dates))
	for _, date := range dates {
		allowed[date] = true
	}
	result := make([]SignalOutcome, 0)
	for _, item := range items {
		if allowed[item.SignalDate] {
			result = append(result, item)
		}
	}
	return result
}

func componentCandidateWeight(items []SignalOutcome, componentKey string, components []ComponentDescriptor) (float64, bool) {
	weights := make(map[string]float64, len(components))
	total := 0.0
	for _, component := range components {
		values := make([]float64, 0)
		for _, item := range items {
			if score, ok := componentAvailable(item, component.Key); ok && score >= minimumActiveStrategyScore {
				values = append(values, item.ExcessReturn)
			}
		}
		if len(values) < minimumComponentValidationActiveSamples {
			continue
		}
		average, _ := meanMedian(values)
		rankIC := componentRankIC(items, component.Key)
		excessQuality := clamp(math.Max(average, 0)/defaultTargetReturn, 0, 1)
		icQuality := clamp(math.Max(rankIC, 0), 0, 1)
		quality := (excessQuality + icQuality) / 2
		if quality == 0 {
			quality = 0.01
		}
		weights[component.Key] = quality
		total += quality
	}
	value, found := weights[componentKey]
	if !found || total <= 0 {
		return 0, false
	}
	return value / total, true
}

func componentRankIC(items []SignalOutcome, componentKey string) float64 {
	scores, excess := make([]float64, 0), make([]float64, 0)
	for _, item := range items {
		if score, ok := componentAvailable(item, componentKey); ok {
			scores = append(scores, score)
			excess = append(excess, item.ExcessReturn)
		}
	}
	return pearson(averageRanks(scores), averageRanks(excess))
}

func buildPortfolioConstraintAnalysis(outcomes []SignalOutcome, horizons []int, componentAnalysis ComponentAnalysis) PortfolioConstraintAnalysis {
	analysis := PortfolioConstraintAnalysis{
		MinimumSignalsPerDay:           minimumPortfolioSignalsPerDay,
		MinimumDays:                    minimumPortfolioDays,
		MinimumScore:                   minimumPortfolioScore,
		MinimumIndustryCoveragePercent: minimumPortfolioIndustryCoverage,
		MaximumIndustryConcentration:   maximumPortfolioIndustryConcentration,
		MaximumComponentConcentration:  maximumPortfolioComponentConcentration,
		MaximumRedundantPairPercent:    maximumPortfolioRedundantPairPercent,
		Horizons:                       make([]PortfolioHorizonAnalysis, 0),
	}
	for _, candidateHorizon := range horizons {
		items := make([]SignalOutcome, 0)
		for _, item := range outcomes {
			if item.Horizon == candidateHorizon && item.Status == OutcomeReady && item.BenchmarkAvailable && item.Score >= minimumPortfolioScore && (item.State == StateTriggered || item.State == StateWatching) {
				items = append(items, item)
			}
		}
		byDate := make(map[string][]SignalOutcome)
		for _, item := range items {
			byDate[item.SignalDate] = append(byDate[item.SignalDate], item)
		}
		metric := PortfolioHorizonAnalysis{Horizon: candidateHorizon, Recent: make([]PortfolioDayMetric, 0)}
		for date, dayItems := range byDate {
			if len(dayItems) < minimumPortfolioSignalsPerDay {
				continue
			}
			day := portfolioDayMetric(date, dayItems, componentAnalysis)
			metric.CandidateDays++
			if day.Signals >= minimumPortfolioSignalsPerDay && day.IndustryCoveragePercent >= minimumPortfolioIndustryCoverage {
				metric.SufficientDays++
			}
			if day.Passed {
				metric.PassedDays++
			} else {
				metric.ViolatingDays++
			}
			metric.AverageIndustryConcentration += day.LargestIndustryPercent
			metric.MaximumIndustryConcentration = math.Max(metric.MaximumIndustryConcentration, day.LargestIndustryPercent)
			metric.AverageComponentConcentration += day.LargestComponentPercent
			metric.MaximumComponentConcentration = math.Max(metric.MaximumComponentConcentration, day.LargestComponentPercent)
			metric.AverageRedundantPairPercent += day.RedundantPairPercent
			metric.MaximumRedundantPairPercent = math.Max(metric.MaximumRedundantPairPercent, day.RedundantPairPercent)
			metric.AverageIndustryCoveragePercent += day.IndustryCoveragePercent
			metric.Recent = append(metric.Recent, day)
		}
		if metric.CandidateDays > 0 {
			denominator := float64(metric.CandidateDays)
			metric.AverageIndustryConcentration /= denominator
			metric.AverageComponentConcentration /= denominator
			metric.AverageRedundantPairPercent /= denominator
			metric.AverageIndustryCoveragePercent /= denominator
		}
		metric.SampleSufficient = metric.CandidateDays >= minimumPortfolioDays
		metric.Passed = metric.SampleSufficient && metric.ViolatingDays == 0
		sort.SliceStable(metric.Recent, func(i, j int) bool { return metric.Recent[i].Date > metric.Recent[j].Date })
		if len(metric.Recent) > 14 {
			metric.Recent = metric.Recent[:14]
		}
		analysis.Horizons = append(analysis.Horizons, metric)
	}
	return analysis
}

func portfolioDayMetric(date string, items []SignalOutcome, componentAnalysis ComponentAnalysis) PortfolioDayMetric {
	day := PortfolioDayMetric{Date: date, Signals: len(items), Violations: make([]string, 0)}
	industries := make(map[string]int)
	components := make(map[string]int)
	for _, item := range items {
		industry := strings.TrimSpace(item.Industry)
		if industry != "" {
			industries[industry]++
		}
		for key, score := range item.StrategyScores {
			if score >= minimumActiveStrategyScore && strings.TrimSpace(item.StrategyStates[key]) != "数据不足" {
				components[key]++
			}
		}
	}
	day.IndustryLabeledSignals = 0
	for _, count := range industries {
		day.IndustryLabeledSignals += count
	}
	if day.Signals > 0 {
		day.IndustryCoveragePercent = float64(day.IndustryLabeledSignals) / float64(day.Signals) * 100
	}
	day.LargestIndustry, day.LargestIndustryPercent = largestShare(industries, day.Signals)
	day.LargestComponentKey, day.LargestComponentPercent = largestShare(components, day.Signals)
	for _, descriptor := range componentAnalysis.Components {
		if descriptor.Key == day.LargestComponentKey {
			day.LargestComponentName = descriptor.Name
			break
		}
	}
	activePairs := 0
	for _, item := range items {
		keys := activeStrategyKeys(item)
		for leftIndex := 0; leftIndex < len(keys); leftIndex++ {
			for rightIndex := leftIndex + 1; rightIndex < len(keys); rightIndex++ {
				left, right := keys[leftIndex], keys[rightIndex]
				for _, cell := range componentAnalysis.Correlations {
					if cell.LeftKey == left && cell.RightKey == right && cell.SampleSufficient {
						day.TotalPairs++
						if cell.Relation == "overlap" {
							activePairs++
						}
						break
					}
				}
			}
		}
	}
	day.RedundantPairs = activePairs
	if day.TotalPairs > 0 {
		day.RedundantPairPercent = float64(activePairs) / float64(day.TotalPairs) * 100
	}
	if day.IndustryCoveragePercent < minimumPortfolioIndustryCoverage {
		day.Violations = append(day.Violations, "行业信息覆盖不足")
	}
	if day.LargestIndustryPercent > maximumPortfolioIndustryConcentration {
		day.Violations = append(day.Violations, "单一行业集中度过高")
	}
	if day.LargestComponentPercent > maximumPortfolioComponentConcentration {
		day.Violations = append(day.Violations, "单一组件集中度过高")
	}
	if day.RedundantPairPercent > maximumPortfolioRedundantPairPercent {
		day.Violations = append(day.Violations, "高相关组件配对过多")
	}
	day.Passed = len(day.Violations) == 0
	return day
}

func largestShare(values map[string]int, total int) (string, float64) {
	key, count := "", 0
	for candidate, value := range values {
		if value > count || (value == count && candidate < key) {
			key, count = candidate, value
		}
	}
	if total <= 0 {
		return key, 0
	}
	return key, float64(count) / float64(total) * 100
}

func breakdownOutcomes(outcomes []SignalOutcome, horizons []int, groups func(SignalOutcome) []OutcomeBreakdown, keys func(SignalOutcome) []string) []OutcomeBreakdown {
	labels := make(map[string]string)
	for _, item := range outcomes {
		for _, group := range groups(item) {
			labels[group.Key] = group.Label
		}
	}
	ordered := make([]string, 0, len(labels))
	for key := range labels {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	result := make([]OutcomeBreakdown, 0, len(ordered))
	for _, key := range ordered {
		items := make([]SignalOutcome, 0)
		for _, item := range outcomes {
			for _, itemKey := range keys(item) {
				if itemKey == key {
					items = append(items, item)
					break
				}
			}
		}
		result = append(result, OutcomeBreakdown{Key: key, Label: labels[key], Signals: uniqueOutcomeSignals(items), Summaries: summarizeOutcomes(items, horizons)})
	}
	return result
}

func scoreBucket(score float64) string {
	switch {
	case score >= 80:
		return "80-100"
	case score >= 65:
		return "65-79.9"
	case score >= 50:
		return "50-64.9"
	default:
		return "0-49.9"
	}
}

func meanMedian(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	total := 0.0
	for _, value := range ordered {
		total += value
	}
	median := ordered[len(ordered)/2]
	if len(ordered)%2 == 0 {
		median = (ordered[len(ordered)/2-1] + ordered[len(ordered)/2]) / 2
	}
	return total / float64(len(ordered)), median
}

func pearson(left, right []float64) float64 {
	if len(left) != len(right) || len(left) < 2 {
		return 0
	}
	leftMean, rightMean := 0.0, 0.0
	for index := range left {
		leftMean += left[index]
		rightMean += right[index]
	}
	leftMean /= float64(len(left))
	rightMean /= float64(len(right))
	numerator, leftVariance, rightVariance := 0.0, 0.0, 0.0
	for index := range left {
		leftDelta := left[index] - leftMean
		rightDelta := right[index] - rightMean
		numerator += leftDelta * rightDelta
		leftVariance += leftDelta * leftDelta
		rightVariance += rightDelta * rightDelta
	}
	denominator := math.Sqrt(leftVariance * rightVariance)
	if denominator == 0 || !finite(denominator) {
		return 0
	}
	return numerator / denominator
}

func averageRanks(values []float64) []float64 {
	type rankedValue struct {
		index int
		value float64
	}
	items := make([]rankedValue, len(values))
	for index, value := range values {
		items[index] = rankedValue{index: index, value: value}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].value < items[j].value })
	ranks := make([]float64, len(values))
	for start := 0; start < len(items); {
		end := start + 1
		for end < len(items) && items[end].value == items[start].value {
			end++
		}
		rank := (float64(start+1) + float64(end)) / 2
		for index := start; index < end; index++ {
			ranks[items[index].index] = rank
		}
		start = end
	}
	return ranks
}

func uniqueOutcomeSignals(items []SignalOutcome) int {
	seen := make(map[string]bool)
	for _, item := range items {
		seen[item.Symbol+"|"+item.SignalDate] = true
	}
	return len(seen)
}

func activeStrategyKeys(item SignalOutcome) []string {
	keys := make([]string, 0, len(item.StrategyScores))
	for key := range item.StrategyScores {
		if activeStrategy(item, key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func activeStrategy(item SignalOutcome, key string) bool {
	score, ok := item.StrategyScores[key]
	if !ok || !finite(score) || score < minimumActiveStrategyScore {
		return false
	}
	return strings.TrimSpace(item.StrategyStates[key]) != "数据不足"
}

func outcomeStateLabel(state SignalState) string {
	switch state {
	case StateTriggered:
		return "触发"
	case StateWatching:
		return "观察"
	case StateInvalid:
		return "数据不足"
	default:
		return "偏弱"
	}
}

func buildOutcomeAssessment(
	outcomes []SignalOutcome,
	horizons []int,
	analysis ComponentAnalysis,
	walkForward ComponentWalkForwardAnalysis,
	portfolio PortfolioConstraintAnalysis,
) OutcomeAssessment {
	horizon := OutcomeHorizon5D
	if !containsInt(horizons, horizon) {
		if len(horizons) > 0 {
			horizon = horizons[len(horizons)-1]
		}
	}
	items := make([]SignalOutcome, 0)
	for _, item := range outcomes {
		if item.Horizon == horizon && item.Status == OutcomeReady && item.BenchmarkAvailable {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].SignalAsOf.Before(items[j].SignalAsOf) })
	dateCount := len(distinctOutcomeDates(items))
	assessment := OutcomeAssessment{
		Stage:          "信号校准",
		Verdict:        "样本收集中",
		Horizon:        horizon,
		MinimumSamples: minimumResearchSamples,
		ReadySamples:   len(items),
		NextStage:      "继续积累带沪深300基准的成熟结果",
		Checks: []OutcomeCheck{
			{Key: "sample", Name: "成熟样本量", Required: true, Passed: len(items) >= minimumResearchSamples, Detail: fmt.Sprintf("%d / %d 个 %d日结果", len(items), minimumResearchSamples, horizon)},
			{Key: "time-slices", Name: "独立交易日", Required: true, Passed: dateCount >= minimumResearchDates, Detail: fmt.Sprintf("%d / %d 个不同交易日；同日横截面不能替代时间样本", dateCount, minimumResearchDates)},
			{Key: "benchmark", Name: "基准覆盖", Required: true, Passed: len(items) >= minimumResearchSamples, Detail: "结果必须同时具备沪深300同期收益"},
		},
	}
	coverageReady := analysis.Signals >= minimumCorrelationSamples
	coveragePassed := len(analysis.Coverage) > 0
	coverageDetails := fmt.Sprintf("%d 个代表信号；要求每组件覆盖率 ≥ %.0f%%", analysis.Signals, minimumComponentCoverage)
	for _, item := range analysis.Coverage {
		if item.AvailablePercent < minimumComponentCoverage {
			coveragePassed = false
			break
		}
	}
	if !coverageReady {
		coveragePassed = false
	}
	assessment.Checks = append(assessment.Checks,
		OutcomeCheck{Key: "component-coverage", Name: "组件覆盖率", Required: coverageReady, Passed: coveragePassed, Detail: coverageDetails},
	)
	correlationReady := analysis.SufficientPairs > 0
	correlationPassed := correlationReady && analysis.RedundantPairs == 0
	correlationDetail := fmt.Sprintf("%d 个成对样本充分的组件组合；重叠 %d 对", analysis.SufficientPairs, analysis.RedundantPairs)
	assessment.Checks = append(assessment.Checks,
		OutcomeCheck{Key: "component-correlation", Name: "组件相关性", Required: correlationReady, Passed: correlationPassed, Detail: correlationDetail},
	)
	regimeSufficient := 0
	for _, metric := range analysis.RegimeMetrics {
		if metric.Horizon == horizon && metric.SampleSufficient {
			regimeSufficient++
		}
	}
	regimeTarget := len(analysis.Components) * 2
	regimePassed := regimeTarget > 0 && regimeSufficient >= regimeTarget
	assessment.Checks = append(assessment.Checks,
		OutcomeCheck{Key: "regime-samples", Name: "状态稳定性样本", Required: false, Passed: regimePassed, Detail: fmt.Sprintf("%d / %d 个 %d日充分状态单元", regimeSufficient, regimeTarget, horizon)},
	)
	validatedComponents, unstableComponents, negativeComponents := 0, 0, 0
	for _, metric := range walkForward.Metrics {
		if metric.Horizon != horizon || !metric.SampleSufficient {
			continue
		}
		validatedComponents++
		if !metric.WeightStable || metric.State == "unstable" {
			unstableComponents++
		}
		if metric.State == "negative" {
			negativeComponents++
		}
	}
	walkForwardReady := validatedComponents > 0
	assessment.Checks = append(assessment.Checks, OutcomeCheck{
		Key: "component-walk-forward", Name: "组件滚动样本外", Required: walkForwardReady,
		Passed: walkForwardReady && unstableComponents == 0 && negativeComponents == 0,
		Detail: fmt.Sprintf("%d 个组件充分；负向 %d 个，权重漂移 %d 个", validatedComponents, negativeComponents, unstableComponents),
	})
	portfolioMetric := portfolioHorizonMetric(portfolio, horizon)
	portfolioReady := portfolioMetric != nil && portfolioMetric.SampleSufficient
	portfolioDetail := fmt.Sprintf("至少需要 %d 个包含 %d 只候选的交易日", minimumPortfolioDays, minimumPortfolioSignalsPerDay)
	portfolioPassed := false
	if portfolioMetric != nil {
		portfolioPassed = portfolioMetric.Passed
		portfolioDetail = fmt.Sprintf("%d 个组合日；违规 %d 个，行业峰值 %.1f%%，组件峰值 %.1f%%", portfolioMetric.CandidateDays, portfolioMetric.ViolatingDays, portfolioMetric.MaximumIndustryConcentration, portfolioMetric.MaximumComponentConcentration)
	}
	assessment.Checks = append(assessment.Checks, OutcomeCheck{
		Key: "portfolio-constraints", Name: "组合集中度", Required: portfolioReady, Passed: portfolioPassed, Detail: portfolioDetail,
	})
	if len(items) < minimumResearchSamples {
		assessment.Notes = []string{
			"样本不足时不调整组件权重或阈值；先让每个交易日的代表信号自然成熟。",
			"同一股票同一交易日只保留最后一次扫描，避免30秒刷新造成重复计数。",
			fmt.Sprintf("滚动调优至少需要 %d 个不同交易日；同日多股票只用于横截面归因，不能替代时间顺序留出。", minimumResearchDates),
			fmt.Sprintf("组件相关性至少需要 %d 个成对完整样本；状态稳定性每格至少需要 %d 个可用样本和 %d 个激活样本。", minimumCorrelationSamples, minimumRegimeSamples, minimumFoldSamples),
			"滚动组件权重只使用此前日期训练，并在后续非重叠日期验证；组合集中度只用于研究门禁，不会自动删票或改分。",
		}
		return assessment
	}
	folds := buildWalkForwardFolds(items)
	if len(folds) < 2 {
		assessment.Notes = []string{"当前样本尚不足以形成两个独立的时间留出窗口。"}
		return assessment
	}
	proposal := aggregateThresholdProposal(folds)
	assessment.Threshold = &proposal
	assessment.Checks = append(assessment.Checks,
		OutcomeCheck{Key: "folds", Name: "时间留出窗口", Required: true, Passed: len(folds) >= 2, Detail: fmt.Sprintf("%d 个按时间顺序展开的验证窗口", len(folds))},
		OutcomeCheck{Key: "excess", Name: "留出超额收益", Required: true, Passed: proposal.ValidationAverageExcess > 0, Detail: fmt.Sprintf("验证集平均超额 %+.2f%%", proposal.ValidationAverageExcess)},
		OutcomeCheck{Key: "hit-rate", Name: "留出正收益率", Required: true, Passed: proposal.ValidationHitRate >= 50, Detail: fmt.Sprintf("验证集正收益率 %.1f%%", proposal.ValidationHitRate)},
	)
	if summary := findSummaryForHorizon(outcomes, horizon); summary != nil {
		assessment.Checks = append(assessment.Checks, OutcomeCheck{Key: "rank-ic", Name: "Rank IC", Required: false, Passed: summary.RankInformationCoefficient > 0, Detail: fmt.Sprintf("全样本 Rank IC %+.3f", summary.RankInformationCoefficient)})
	}
	allRequiredPassed := true
	for _, check := range assessment.Checks {
		if check.Required && !check.Passed {
			allRequiredPassed = false
			break
		}
	}
	assessment.StrategyWeights = walkForwardWeightProposals(walkForward, horizon)
	if allRequiredPassed {
		assessment.Verdict = "可作为下一轮候选"
		assessment.NextStage = "先接入统一组合策略接口，再做滚动样本外复核"
	} else {
		assessment.Verdict = "保留当前基线"
		assessment.NextStage = "继续收集并复核失败窗口，不应用候选参数"
	}
	assessment.Notes = []string{
		"阈值候选只在训练窗口选择，再在后续窗口验证；不会使用最终留出结果反向调参。",
		"组件权重是研究提案；只有通过独立交易日、覆盖、相关性和时间顺序门禁后，才会进入自适应 Challenger，不会直接改写 Champion。",
		fmt.Sprintf("滚动调优至少需要 %d 个不同交易日；阈值与组合使用同一风险调整分口径。", minimumResearchDates),
		fmt.Sprintf("组件相关性至少需要 %d 个成对完整样本；状态稳定性每格至少需要 %d 个可用样本和 %d 个激活样本。", minimumCorrelationSamples, minimumRegimeSamples, minimumFoldSamples),
		"组合集中度按每日候选检查行业覆盖、单一行业、活跃组件和高相关组件配对；研究结果不会自动删除候选。",
	}
	return assessment
}

func walkForwardWeightProposals(analysis ComponentWalkForwardAnalysis, horizon int) []StrategyWeightProposal {
	// A component weight is a research proposal, not a live signal.  The last
	// fold is deliberately not privileged: a short regime or a data glitch in
	// the newest window must not be able to replace the evidence accumulated in
	// earlier, non-overlapping windows.
	type aggregate struct {
		metric        ComponentValidationMetric
		weight        float64
		samples       int
		averageExcess float64
		hitRate       float64
	}
	aggregates := make([]aggregate, 0)
	for _, metric := range analysis.Metrics {
		if metric.Horizon != horizon || len(metric.Folds) == 0 {
			continue
		}
		valid := make([]ComponentValidationFold, 0, len(metric.Folds))
		for _, fold := range metric.Folds {
			if !fold.SampleSufficient || !fold.WeightAvailable || !finite(fold.CandidateWeight) || fold.CandidateWeight <= 0 {
				continue
			}
			valid = append(valid, fold)
		}
		if len(valid) < minimumComponentValidationFolds {
			continue
		}
		weights := make([]float64, 0, len(valid))
		weightTotal, excessTotal, hitTotal := 0.0, 0.0, 0.0
		samples := 0
		for _, fold := range valid {
			weights = append(weights, fold.CandidateWeight)
			foldSamples := fold.ValidationActiveSamples
			if foldSamples <= 0 {
				foldSamples = fold.ValidationSamples
			}
			if foldSamples <= 0 {
				foldSamples = 1
			}
			samples += foldSamples
			weightTotal += fold.CandidateWeight * float64(foldSamples)
			excessTotal += fold.ValidationAverageExcess * float64(foldSamples)
			hitTotal += fold.ValidationHitRate * float64(foldSamples)
		}
		if weightTotal <= 0 || !finite(weightTotal) {
			continue
		}
		meanWeight, medianWeight := meanMedian(weights)
		// Blending the sample-weighted mean with the median is a small robust
		// estimator: it keeps useful information from larger folds while
		// limiting the influence of one extreme fold.  The remaining shrinkage
		// toward a neutral weight is applied after all components are collected.
		robustWeight := (weightTotal/float64(samples) + medianWeight) / 2
		if !finite(robustWeight) || robustWeight <= 0 {
			robustWeight = meanWeight
		}
		aggregates = append(aggregates, aggregate{
			metric: metric, weight: robustWeight, samples: samples,
			averageExcess: excessTotal / float64(samples), hitRate: hitTotal / float64(samples),
		})
	}
	if len(aggregates) == 0 {
		return nil
	}
	neutral := 1 / float64(len(aggregates))
	result := make([]StrategyWeightProposal, 0, len(aggregates))
	weightTotal := 0.0
	for _, item := range aggregates {
		// Keep the calibration conservative even when a component has a very
		// strong but short-lived historical edge.
		weight := .80*item.weight + .20*neutral
		if !finite(weight) || weight <= 0 {
			continue
		}
		weightTotal += weight
		result = append(result, StrategyWeightProposal{
			Key: item.metric.ComponentKey, Name: item.metric.ComponentName, Samples: item.samples,
			AverageExcess: item.averageExcess, HitRate: item.hitRate, Weight: weight,
		})
	}
	if weightTotal > 0 {
		for index := range result {
			result[index].Weight /= weightTotal
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Weight == result[j].Weight {
			return result[i].Key < result[j].Key
		}
		return result[i].Weight > result[j].Weight
	})
	return result
}

// CalibrationFromOutcome converts only a fully gated walk-forward proposal
// into a challenger configuration. It never changes the champion score; the
// caller decides where the parallel challenger is observed.
func CalibrationFromOutcome(report OutcomeReport) (ScoreCalibration, bool) {
	assessment := report.Assessment
	if assessment.Verdict != "可作为下一轮候选" || assessment.Threshold == nil || len(assessment.StrategyWeights) < 2 {
		return ScoreCalibration{}, false
	}
	for _, check := range assessment.Checks {
		if check.Required && !check.Passed {
			return ScoreCalibration{}, false
		}
	}
	minimumScore := assessment.Threshold.MinimumScore
	if !finite(minimumScore) || minimumScore <= 0 {
		return ScoreCalibration{}, false
	}
	weights := make(map[string]float64, len(assessment.StrategyWeights))
	weightTotal := 0.0
	for _, proposal := range assessment.StrategyWeights {
		key := strings.TrimSpace(proposal.Key)
		if key == "" || !finite(proposal.Weight) || proposal.Weight <= 0 {
			continue
		}
		weights[key] = proposal.Weight
		weightTotal += proposal.Weight
	}
	if len(weights) < 2 || weightTotal <= 0 {
		return ScoreCalibration{}, false
	}
	for key := range weights {
		weights[key] /= weightTotal
	}
	dataThrough := strings.TrimSpace(report.DataThrough)
	if dataThrough == "" {
		dataThrough = strings.TrimSpace(report.AsOf)
	}
	calibration := ScoreCalibration{
		ID:          fmt.Sprintf("CAL-%s-H%d-N%d-%s", strings.ReplaceAll(dataThrough, "-", ""), assessment.Horizon, assessment.ReadySamples, calibrationFingerprint(minimumScore, weights)),
		DataThrough: dataThrough, Horizon: assessment.Horizon, ReadySamples: assessment.ReadySamples,
		MinimumScore: minimumScore, ComponentWeights: weights,
	}
	return calibration, true
}

func calibrationFingerprint(minimumScore float64, weights map[string]float64) string {
	data, err := json.Marshal(struct {
		MinimumScore float64            `json:"minimum_score"`
		Weights      map[string]float64 `json:"weights"`
	}{MinimumScore: minimumScore, Weights: weights})
	if err != nil {
		return "unknown"
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:4])
}

func portfolioHorizonMetric(analysis PortfolioConstraintAnalysis, horizon int) *PortfolioHorizonAnalysis {
	for index := range analysis.Horizons {
		if analysis.Horizons[index].Horizon == horizon {
			return &analysis.Horizons[index]
		}
	}
	return nil
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func findSummaryForHorizon(outcomes []SignalOutcome, horizon int) *OutcomeSummary {
	summary := summarizeOutcomes(outcomes, []int{horizon})
	if len(summary) == 0 {
		return nil
	}
	return &summary[0]
}

type outcomeDateGroup struct {
	date  string
	items []SignalOutcome
}

func buildWalkForwardFolds(items []SignalOutcome) []OutcomeValidationFold {
	if len(items) < minimumResearchSamples {
		return nil
	}
	// Split on trading-date boundaries. Multiple stocks emitted on one day must
	// stay in the same side of a fold; otherwise the validation set leaks the
	// day's cross-sectional information into training.
	ordered := append([]SignalOutcome(nil), items...)
	sort.SliceStable(ordered, func(left, right int) bool {
		leftDate, rightDate := outcomeDate(ordered[left]), outcomeDate(ordered[right])
		if leftDate == rightDate {
			return ordered[left].SignalAsOf.Before(ordered[right].SignalAsOf)
		}
		return leftDate < rightDate
	})
	groups := make([]outcomeDateGroup, 0)
	for _, item := range ordered {
		date := outcomeDate(item)
		if date == "" {
			continue
		}
		if len(groups) == 0 || groups[len(groups)-1].date != date {
			groups = append(groups, outcomeDateGroup{date: date})
		}
		groups[len(groups)-1].items = append(groups[len(groups)-1].items, item)
	}
	if len(groups) < minimumResearchDates {
		return nil
	}
	initialDays := len(groups) / 2
	remainingDays := len(groups) - initialDays
	maxFolds := remainingDays
	if maxFolds > 3 {
		maxFolds = 3
	}
	for foldCount := maxFolds; foldCount >= 2; foldCount-- {
		partitions := partitionOutcomeDateGroups(groups[initialDays:], foldCount)
		if len(partitions) != foldCount {
			continue
		}
		valid := true
		for _, partition := range partitions {
			if len(flattenOutcomeDateGroups(partition)) < minimumFoldSamples {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		folds := make([]OutcomeValidationFold, 0, foldCount)
		trainGroupEnd := initialDays
		for index, partition := range partitions {
			validation := flattenOutcomeDateGroups(partition)
			train := flattenOutcomeDateGroups(groups[:trainGroupEnd])
			threshold := chooseThreshold(train)
			if threshold.TrainSamples < minimumFoldSamples {
				valid = false
				break
			}
			_, validationAverage, validationHit := thresholdStats(validation, threshold.MinimumScore)
			_, baselineAverage, _ := thresholdStats(validation, 0)
			folds = append(folds, OutcomeValidationFold{
				Index: index + 1, TrainEnd: groups[trainGroupEnd-1].date,
				ValidationStart: partition[0].date, ValidationEnd: partition[len(partition)-1].date,
				MinimumScore: threshold.MinimumScore, TrainSamples: threshold.TrainSamples,
				ValidationSamples: len(validation), TrainAverageExcess: threshold.TrainAverageExcess, AverageExcess: validationAverage, HitRate: validationHit,
				BaselineExcess: baselineAverage,
			})
			trainGroupEnd += len(partition)
		}
		if valid && len(folds) >= 2 {
			return folds
		}
	}
	return nil
}

func outcomeDate(item SignalOutcome) string {
	date := strings.TrimSpace(item.SignalDate)
	if date == "" && !item.SignalAsOf.IsZero() {
		date = item.SignalAsOf.In(time.Local).Format("2006-01-02")
	}
	return date
}

func partitionOutcomeDateGroups(groups []outcomeDateGroup, count int) [][]outcomeDateGroup {
	if count <= 0 || len(groups) < count {
		return nil
	}
	result := make([][]outcomeDateGroup, 0, count)
	base, extra := len(groups)/count, len(groups)%count
	start := 0
	for index := 0; index < count; index++ {
		size := base
		if index < extra {
			size++
		}
		if size <= 0 {
			return nil
		}
		result = append(result, groups[start:start+size])
		start += size
	}
	return result
}

func flattenOutcomeDateGroups(groups []outcomeDateGroup) []SignalOutcome {
	result := make([]SignalOutcome, 0)
	for _, group := range groups {
		result = append(result, group.items...)
	}
	return result
}

func chooseThreshold(items []SignalOutcome) ThresholdProposal {
	// A zero threshold is useful for an unconditional baseline comparison, but
	// it is never a valid live entry gate. Keep research candidates on the same
	// risk-adjusted scale as the realtime portfolio gate and fall back to 55
	// when the higher score buckets do not yet have enough observations.
	best := ThresholdProposal{MinimumScore: minimumPortfolioScore, TrainSamples: len(items)}
	bestAverage := math.Inf(-1)
	for _, threshold := range []float64{minimumPortfolioScore, 60, 65, 70, 75} {
		samples, average, hitRate := thresholdStats(items, threshold)
		if samples < minimumFoldSamples {
			continue
		}
		if average > bestAverage || (average == bestAverage && hitRate > best.ValidationHitRate) {
			bestAverage = average
			best.MinimumScore = threshold
			best.TrainSamples = samples
			best.TrainAverageExcess = average
			best.ValidationHitRate = hitRate
		}
	}
	if math.IsInf(bestAverage, -1) {
		best.TrainSamples, best.TrainAverageExcess, best.ValidationHitRate = thresholdStats(items, minimumPortfolioScore)
		best.MinimumScore = minimumPortfolioScore
	}
	return best
}

func aggregateThresholdProposal(folds []OutcomeValidationFold) ThresholdProposal {
	proposal := ThresholdProposal{Folds: append([]OutcomeValidationFold(nil), folds...)}
	if len(folds) == 0 {
		return proposal
	}
	// Thresholds are selected independently inside each training window.  Use a
	// cross-fold median so the newest window cannot unilaterally push the live
	// gate to an extreme value.  The result remains one of the observed fold
	// values (or the midpoint of the two central values), which keeps the audit
	// trail easy to interpret.
	thresholds := make([]float64, 0, len(folds))
	for _, fold := range folds {
		if finite(fold.MinimumScore) {
			thresholds = append(thresholds, fold.MinimumScore)
		}
		proposal.TrainSamples += fold.TrainSamples
		proposal.ValidationSamples += fold.ValidationSamples
		proposal.TrainAverageExcess += fold.TrainAverageExcess * float64(fold.ValidationSamples)
		proposal.ValidationAverageExcess += fold.AverageExcess * float64(fold.ValidationSamples)
		proposal.ValidationHitRate += fold.HitRate * float64(fold.ValidationSamples)
		proposal.BaselineValidationExcess += fold.BaselineExcess * float64(fold.ValidationSamples)
	}
	if len(thresholds) > 0 {
		sort.Float64s(thresholds)
		middle := len(thresholds) / 2
		if len(thresholds)%2 == 0 {
			proposal.MinimumScore = (thresholds[middle-1] + thresholds[middle]) / 2
		} else {
			proposal.MinimumScore = thresholds[middle]
		}
	}
	denominator := 0
	for _, fold := range folds {
		denominator += fold.ValidationSamples
	}
	if denominator > 0 {
		proposal.TrainAverageExcess /= float64(denominator)
		proposal.ValidationAverageExcess /= float64(denominator)
		proposal.ValidationHitRate /= float64(denominator)
		proposal.BaselineValidationExcess /= float64(denominator)
	}
	return proposal
}

func thresholdStats(items []SignalOutcome, threshold float64) (int, float64, float64) {
	count, positive, total := 0, 0, 0.0
	for _, item := range items {
		if baselineOutcomeScore(item) < threshold {
			continue
		}
		count++
		value := item.ExcessReturn
		total += value
		if value > 0 {
			positive++
		}
	}
	if count == 0 {
		return 0, 0, 0
	}
	return count, total / float64(count), float64(positive) / float64(count) * 100
}

func strategyWeightProposals(items []SignalOutcome) []StrategyWeightProposal {
	type accumulator struct {
		name, key string
		values    []float64
		positive  int
	}
	groups := make(map[string]*accumulator)
	for _, item := range items {
		for _, key := range activeStrategyKeys(item) {
			group := groups[key]
			if group == nil {
				group = &accumulator{key: key, name: item.StrategyNames[key]}
				groups[key] = group
			}
			group.values = append(group.values, item.ExcessReturn)
			if item.ExcessReturn > 0 {
				group.positive++
			}
		}
	}
	result := make([]StrategyWeightProposal, 0, len(groups))
	totalWeight := 0.0
	for _, group := range groups {
		if len(group.values) < minimumFoldSamples {
			continue
		}
		average, _ := meanMedian(group.values)
		weight := math.Max(average, 0)
		if weight == 0 {
			weight = 0.01
		}
		totalWeight += weight
		result = append(result, StrategyWeightProposal{Key: group.key, Name: group.name, Samples: len(group.values), AverageExcess: average, HitRate: float64(group.positive) / float64(len(group.values)) * 100, Weight: weight})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].AverageExcess > result[j].AverageExcess })
	if totalWeight > 0 {
		for index := range result {
			result[index].Weight /= totalWeight
		}
	}
	return result
}

func recentOutcomes(items []SignalOutcome, limit int) []SignalOutcome {
	result := append([]SignalOutcome(nil), items...)
	sort.SliceStable(result, func(i, j int) bool { return result[i].SignalAsOf.After(result[j].SignalAsOf) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}
