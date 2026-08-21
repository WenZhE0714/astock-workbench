package realtime

import (
	"context"
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
	defaultOutcomeSignalLimit  = 500
	defaultTargetReturn        = 5.0
	minimumResearchSamples     = 35
	minimumFoldSamples         = 5
	minimumActiveStrategyScore = 8.0
	minimumCorrelationSamples  = 20
	minimumRegimeSamples       = 8
	minimumComponentCoverage   = 70.0
	redundantRankCorrelation   = 0.75
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
	if limit <= 0 {
		limit = defaultOutcomeSignalLimit
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
	return report, nil
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
		if len(result) >= limit {
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
		Score: signal.Score, State: signal.State, MarketRegime: classifyMarketRegime(benchmark, date), Horizon: horizon, Status: OutcomeInvalid,
		EvaluatedAt: evaluatedAt, DataSource: signal.DataSource,
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
	report.Assessment = buildOutcomeAssessment(outcomes, horizons, report.ComponentAnalysis)
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

func buildOutcomeAssessment(outcomes []SignalOutcome, horizons []int, analysis ComponentAnalysis) OutcomeAssessment {
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
	assessment := OutcomeAssessment{
		Stage:          "信号校准",
		Verdict:        "样本收集中",
		Horizon:        horizon,
		MinimumSamples: minimumResearchSamples,
		ReadySamples:   len(items),
		NextStage:      "继续积累带沪深300基准的成熟结果",
		Checks: []OutcomeCheck{
			{Key: "sample", Name: "成熟样本量", Required: true, Passed: len(items) >= minimumResearchSamples, Detail: fmt.Sprintf("%d / %d 个 %d日结果", len(items), minimumResearchSamples, horizon)},
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
	if len(items) < minimumResearchSamples {
		assessment.Notes = []string{
			"样本不足时不调整组件权重或阈值；先让每个交易日的代表信号自然成熟。",
			"同一股票同一交易日只保留最后一次扫描，避免30秒刷新造成重复计数。",
			fmt.Sprintf("组件相关性至少需要 %d 个成对完整样本；状态稳定性每格至少需要 %d 个可用样本和 %d 个激活样本。", minimumCorrelationSamples, minimumRegimeSamples, minimumFoldSamples),
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
	assessment.StrategyWeights = strategyWeightProposals(items)
	if allRequiredPassed {
		assessment.Verdict = "可作为下一轮候选"
		assessment.NextStage = "先接入统一组合策略接口，再做滚动样本外复核"
	} else {
		assessment.Verdict = "保留当前基线"
		assessment.NextStage = "继续收集并复核失败窗口，不应用候选参数"
	}
	assessment.Notes = []string{
		"阈值候选只在训练窗口选择，再在后续窗口验证；不会使用最终留出结果反向调参。",
		"组件权重是研究提案；当前回测引擎尚无多组件组合接口，不会自动改变实时评分或伪装成已完成回测验证。",
		fmt.Sprintf("组件相关性至少需要 %d 个成对完整样本；状态稳定性每格至少需要 %d 个可用样本和 %d 个激活样本。", minimumCorrelationSamples, minimumRegimeSamples, minimumFoldSamples),
	}
	return assessment
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

func buildWalkForwardFolds(items []SignalOutcome) []OutcomeValidationFold {
	if len(items) < minimumResearchSamples {
		return nil
	}
	initialTrain := len(items) / 2
	remaining := len(items) - initialTrain
	foldCount := remaining / minimumFoldSamples
	if foldCount > 3 {
		foldCount = 3
	}
	if foldCount < 2 {
		return nil
	}
	validationSize := remaining / foldCount
	if validationSize < minimumFoldSamples {
		return nil
	}
	folds := make([]OutcomeValidationFold, 0, foldCount)
	trainEnd := initialTrain
	for index := 0; index < foldCount; index++ {
		validationEnd := trainEnd + validationSize
		if index == foldCount-1 || validationEnd > len(items) {
			validationEnd = len(items)
		}
		if validationEnd-trainEnd < minimumFoldSamples {
			break
		}
		threshold := chooseThreshold(items[:trainEnd])
		validation := items[trainEnd:validationEnd]
		_, validationAverage, validationHit := thresholdStats(validation, threshold.MinimumScore)
		_, baselineAverage, _ := thresholdStats(validation, 0)
		folds = append(folds, OutcomeValidationFold{
			Index: index + 1, TrainEnd: items[trainEnd-1].SignalDate,
			ValidationStart: validation[0].SignalDate, ValidationEnd: validation[len(validation)-1].SignalDate,
			MinimumScore: threshold.MinimumScore, TrainSamples: threshold.TrainSamples,
			ValidationSamples: len(validation), TrainAverageExcess: threshold.TrainAverageExcess, AverageExcess: validationAverage, HitRate: validationHit,
			BaselineExcess: baselineAverage,
		})
		trainEnd = validationEnd
		if trainEnd >= len(items) {
			break
		}
	}
	return folds
}

func chooseThreshold(items []SignalOutcome) ThresholdProposal {
	best := ThresholdProposal{MinimumScore: 0, TrainSamples: len(items)}
	bestAverage := math.Inf(-1)
	for _, threshold := range []float64{0, 50, 65, 80} {
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
		_, best.TrainAverageExcess, best.ValidationHitRate = thresholdStats(items, 0)
		best.MinimumScore = 0
	}
	return best
}

func aggregateThresholdProposal(folds []OutcomeValidationFold) ThresholdProposal {
	proposal := ThresholdProposal{Folds: append([]OutcomeValidationFold(nil), folds...)}
	if len(folds) == 0 {
		return proposal
	}
	proposal.MinimumScore = folds[len(folds)-1].MinimumScore
	for _, fold := range folds {
		proposal.TrainSamples += fold.TrainSamples
		proposal.ValidationSamples += fold.ValidationSamples
		proposal.TrainAverageExcess += fold.TrainAverageExcess * float64(fold.ValidationSamples)
		proposal.ValidationAverageExcess += fold.AverageExcess * float64(fold.ValidationSamples)
		proposal.ValidationHitRate += fold.HitRate * float64(fold.ValidationSamples)
		proposal.BaselineValidationExcess += fold.BaselineExcess * float64(fold.ValidationSamples)
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
		if item.Score < threshold {
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
