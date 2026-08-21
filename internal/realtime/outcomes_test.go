package realtime

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type outcomeHistoryStub struct{ bars map[string][]domain.DailyBar }

func (stub outcomeHistoryStub) FetchDailyBars(_ context.Context, symbol string) ([]domain.DailyBar, error) {
	return stub.bars[symbol], nil
}

type outcomeStoreStub struct{ items []SignalOutcome }

func (stub *outcomeStoreStub) Upsert(items []SignalOutcome) error {
	stub.items = append(stub.items, items...)
	return nil
}

func (stub *outcomeStoreStub) List(int) ([]SignalOutcome, error) { return stub.items, nil }

func outcomeBars(symbol string, values ...struct {
	date                   string
	open, close, high, low float64
}) []domain.DailyBar {
	result := make([]domain.DailyBar, 0, len(values))
	for _, value := range values {
		result = append(result, domain.DailyBar{Symbol: symbol, Source: "test", Date: value.date, Open: value.open, Close: value.close, High: value.high, Low: value.low})
	}
	return result
}

func TestOutcomeEvaluatorUsesOnlyTradingBarsAfterSignal(t *testing.T) {
	stock := outcomeBars("sh600000",
		struct {
			date                   string
			open, close, high, low float64
		}{"2026-08-20", 100, 200, 210, 90},
		struct {
			date                   string
			open, close, high, low float64
		}{"2026-08-21", 110, 112, 116, 104},
		struct {
			date                   string
			open, close, high, low float64
		}{"2026-08-24", 113, 118, 120, 108},
		struct {
			date                   string
			open, close, high, low float64
		}{"2026-08-25", 119, 121, 124, 107},
	)
	benchmark := outcomeBars("sh000300",
		struct {
			date                   string
			open, close, high, low float64
		}{"2026-08-21", 100, 101, 102, 99},
		struct {
			date                   string
			open, close, high, low float64
		}{"2026-08-24", 101, 102, 103, 100},
		struct {
			date                   string
			open, close, high, low float64
		}{"2026-08-25", 102, 103, 104, 101},
	)
	store := &outcomeStoreStub{}
	evaluator := NewOutcomeEvaluator(outcomeHistoryStub{bars: map[string][]domain.DailyBar{"sh600000": stock, "sh000300": benchmark}}, outcomeHistoryStub{bars: map[string][]domain.DailyBar{"sh000300": benchmark}}, store)
	signalTime := time.Date(2026, 8, 20, 14, 30, 0, 0, time.Local)
	signal := Signal{
		ID: "scan-1", Symbol: "sh600000", Name: "浦发银行", AsOf: signalTime, DataDate: "2026-08-19",
		Score: 82, State: StateTriggered, TriggerPrice: 115, InvalidationPrice: 105,
		Components: []Component{{Key: "trend-breakout", Name: "趋势突破", Score: 18, State: "触发"}},
	}
	report, err := evaluator.Evaluate(context.Background(), []Signal{signal}, OutcomeOptions{Horizons: []int{1, 3}, TargetReturn: 8, Now: func() time.Time { return time.Date(2026, 8, 26, 9, 0, 0, 0, time.Local) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.items) != 2 || len(report.Summaries) != 2 {
		t.Fatalf("unexpected outcomes: %+v", store.items)
	}
	one := store.items[0]
	if one.SignalDate != "2026-08-20" || one.TargetDate != "2026-08-21" || one.EntryPrice != 110 {
		t.Fatalf("signal-day bar leaked into forward window: %+v", one)
	}
	if math.Abs(one.ReturnPercent-(112.0/110.0-1)*100) > 1e-9 || !one.HitTrigger || !one.HitInvalidation {
		t.Fatalf("unexpected one-day label: %+v", one)
	}
	three := store.items[1]
	if three.TargetDate != "2026-08-25" || !three.HitTarget || !three.HitInvalidation || !three.BenchmarkAvailable {
		t.Fatalf("unexpected three-day label: %+v", three)
	}
	wantBenchmark := (103.0/100.0 - 1) * 100
	if math.Abs(three.BenchmarkReturn-wantBenchmark) > 1e-9 || math.Abs(three.ExcessReturn-(three.ReturnPercent-wantBenchmark)) > 1e-9 {
		t.Fatalf("unexpected benchmark alignment: %+v", three)
	}
}

func TestOutcomeEvaluatorDeduplicatesSameSymbolAndSignalDay(t *testing.T) {
	first := Signal{ID: "morning", Symbol: "sh600000", AsOf: time.Date(2026, 8, 20, 10, 0, 0, 0, time.Local), Score: 60}
	second := Signal{ID: "afternoon", Symbol: "sh600000", AsOf: time.Date(2026, 8, 20, 14, 0, 0, 0, time.Local), Score: 85}
	selected := representativeSignals([]Signal{first, second}, 10)
	if len(selected) != 1 || selected[0].ID != "afternoon" {
		t.Fatalf("unexpected representative signals: %+v", selected)
	}
}

func TestOutcomeEvaluatorKeepsImmatureHorizonsPending(t *testing.T) {
	bars := outcomeBars("sh600000",
		struct {
			date                   string
			open, close, high, low float64
		}{"2026-08-21", 10, 11, 12, 9},
		struct {
			date                   string
			open, close, high, low float64
		}{"2026-08-24", 11, 12, 13, 10},
	)
	signal := Signal{ID: "pending", Symbol: "sh600000", AsOf: time.Date(2026, 8, 20, 10, 0, 0, 0, time.Local), Score: 70}
	outcome := labelSignal(signal, bars, nil, 3, 5, time.Now())
	if outcome.Status != OutcomePending || outcome.ReturnPercent != 0 || outcome.TargetDate != "" {
		t.Fatalf("future data was inferred for immature horizon: %+v", outcome)
	}
}

func TestMarketRegimeUsesOnlyBarsBeforeSignalDate(t *testing.T) {
	baseDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	bars := make([]domain.DailyBar, 0, 90)
	for index := 0; index < 70; index++ {
		closePrice := 100 + float64(index)*0.5
		bars = append(bars, domain.DailyBar{Date: baseDate.AddDate(0, 0, index).Format("2006-01-02"), Open: closePrice, Close: closePrice, High: closePrice + 1, Low: closePrice - 1})
	}
	// A severe future move must not relabel a signal emitted before it.
	bars = append(bars, domain.DailyBar{Date: baseDate.AddDate(0, 0, 75).Format("2006-01-02"), Open: 20, Close: 20, High: 21, Low: 19})
	signalDate := baseDate.AddDate(0, 0, 70).Format("2006-01-02")
	if regime := classifyMarketRegime(normalizedOutcomeBars(bars), signalDate); regime != MarketRegimeBull {
		t.Fatalf("future benchmark bar leaked into regime: %s", regime)
	}
	if regime := classifyMarketRegime(normalizedOutcomeBars(bars), baseDate.Format("2006-01-02")); regime != MarketRegimeInsufficient {
		t.Fatalf("expected insufficient regime, got %s", regime)
	}
}

func TestLabelSignalCarriesPointInTimeMarketRegime(t *testing.T) {
	stock := outcomeBars("sh600000",
		struct {
			date                   string
			open, close, high, low float64
		}{"2026-04-01", 100, 101, 102, 99},
		struct {
			date                   string
			open, close, high, low float64
		}{"2026-04-02", 101, 102, 103, 100},
	)
	benchmark := make([]domain.DailyBar, 0, 70)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	for index := 0; index < 70; index++ {
		closePrice := 100 + float64(index)*0.5
		benchmark = append(benchmark, domain.DailyBar{Symbol: "sh000300", Date: start.AddDate(0, 0, index).Format("2006-01-02"), Open: closePrice, Close: closePrice, High: closePrice + 1, Low: closePrice - 1})
	}
	signal := Signal{ID: "regime", Symbol: "sh600000", AsOf: time.Date(2026, 3, 12, 10, 0, 0, 0, time.Local)}
	outcome := labelSignal(signal, normalizedOutcomeBars(stock), normalizedOutcomeBars(benchmark), 1, 5, time.Now())
	if outcome.MarketRegime != MarketRegimeBull {
		t.Fatalf("unexpected point-in-time regime: %+v", outcome)
	}
}

func TestMergeOutcomeRevisionsDoesNotRegressReadyLabel(t *testing.T) {
	now := time.Date(2026, 8, 25, 16, 0, 0, 0, time.Local)
	ready := SignalOutcome{Key: "sh600000:2026-08-20:3", Status: OutcomeReady, ReturnPercent: 4, EvaluatedAt: now}
	invalid := SignalOutcome{Key: ready.Key, Status: OutcomeInvalid, EvaluatedAt: now.Add(time.Hour)}
	changes, merged := mergeOutcomeRevisions([]SignalOutcome{ready}, []SignalOutcome{invalid})
	if len(changes) != 0 || len(merged) != 1 || merged[0].Status != OutcomeReady {
		t.Fatalf("ready label regressed: changes=%+v merged=%+v", changes, merged)
	}
}

func TestBuildOutcomeReportGeneratesWalkForwardResearchProposal(t *testing.T) {
	items := make([]SignalOutcome, 0, 40)
	start := time.Date(2026, 1, 5, 15, 0, 0, 0, time.Local)
	for index := 0; index < 40; index++ {
		score := 40 + float64(index)*2.5
		excess := float64(index-20) / 4
		asOf := start.AddDate(0, 0, index)
		items = append(items, SignalOutcome{
			Key: "sh600000:" + asOf.Format("2006-01-02") + ":5", SignalID: fmt.Sprintf("signal-%02d", index),
			Symbol: "sh600000", Name: "测试股票", SignalDate: asOf.Format("2006-01-02"), SignalAsOf: asOf,
			Score: score, State: StateTriggered, Horizon: OutcomeHorizon5D, Status: OutcomeReady,
			ReturnPercent: excess + 1, BenchmarkAvailable: true, BenchmarkReturn: 1, ExcessReturn: excess,
			StrategyScores: map[string]float64{
				"trend-breakout": strategyTestScore(index >= 20, 8+float64(index-20)/2),
				"ma-pullback":    strategyTestScore(index < 20, 8+float64(19-index)/2),
			},
			StrategyStates: map[string]string{"trend-breakout": "观察", "ma-pullback": "观察"},
			StrategyNames:  map[string]string{"trend-breakout": "趋势突破", "ma-pullback": "均线回踩"},
		})
	}

	report := BuildOutcomeReport(items, start.AddDate(0, 0, 41), nil)
	if len(report.Summaries) != 1 || report.Summaries[0].Ready != 40 {
		t.Fatalf("unexpected summary: %+v", report.Summaries)
	}
	if report.Summaries[0].RankInformationCoefficient < 0.99 {
		t.Fatalf("expected strong rank IC, got %.4f", report.Summaries[0].RankInformationCoefficient)
	}
	assessment := report.Assessment
	if assessment.ReadySamples != 40 || assessment.Threshold == nil || len(assessment.Threshold.Folds) < 2 {
		t.Fatalf("walk-forward proposal missing: %+v", assessment)
	}
	if assessment.Threshold.ValidationAverageExcess <= 0 || assessment.Threshold.ValidationHitRate <= 50 {
		t.Fatalf("unexpected validation metrics: %+v", assessment.Threshold)
	}
	if len(assessment.StrategyWeights) != 2 {
		t.Fatalf("unexpected strategy proposals: %+v", assessment.StrategyWeights)
	}
	if len(report.Strategies) != 2 || report.Strategies[0].Signals != 20 || report.Strategies[1].Signals != 20 {
		t.Fatalf("strategy attribution did not isolate active votes: %+v", report.Strategies)
	}
	strategyByKey := make(map[string]OutcomeBreakdown, len(report.Strategies))
	for _, breakdown := range report.Strategies {
		strategyByKey[breakdown.Key] = breakdown
	}
	if strategyByKey["trend-breakout"].Summaries[0].RankInformationCoefficient < 0.99 || strategyByKey["ma-pullback"].Summaries[0].RankInformationCoefficient > -0.99 {
		t.Fatalf("strategy IC did not use component scores: %+v", report.Strategies)
	}
	weightTotal := 0.0
	for _, proposal := range assessment.StrategyWeights {
		weightTotal += proposal.Weight
	}
	if math.Abs(weightTotal-1) > 1e-9 {
		t.Fatalf("strategy weights are not normalized: %.12f", weightTotal)
	}
	if assessment.StrategyWeights[0].Key != "trend-breakout" || assessment.StrategyWeights[0].Weight <= assessment.StrategyWeights[1].Weight {
		t.Fatalf("candidate weights ignored active-signal excess: %+v", assessment.StrategyWeights)
	}
}

func TestBuildOutcomeReportBreaksDownMarketRegimes(t *testing.T) {
	now := time.Date(2026, 8, 20, 16, 0, 0, 0, time.Local)
	items := []SignalOutcome{
		{Key: "bull", Symbol: "sh600000", SignalDate: "2026-08-01", SignalAsOf: now.Add(-48 * time.Hour), Score: 80, Horizon: 5, Status: OutcomeReady, MarketRegime: MarketRegimeBull, ReturnPercent: 4, BenchmarkAvailable: true, ExcessReturn: 2},
		{Key: "bear", Symbol: "sz000001", SignalDate: "2026-08-02", SignalAsOf: now.Add(-24 * time.Hour), Score: 60, Horizon: 5, Status: OutcomeReady, MarketRegime: MarketRegimeBear, ReturnPercent: -3, BenchmarkAvailable: true, ExcessReturn: -1},
		{Key: "legacy", Symbol: "sz000002", SignalDate: "2026-08-03", SignalAsOf: now, Score: 55, Horizon: 5, Status: OutcomeReady, ReturnPercent: 1},
	}
	report := BuildOutcomeReport(items, now, nil)
	if len(report.Regimes) != 3 {
		t.Fatalf("unexpected regime groups: %+v", report.Regimes)
	}
	byKey := make(map[string]OutcomeBreakdown, len(report.Regimes))
	for _, breakdown := range report.Regimes {
		byKey[breakdown.Key] = breakdown
	}
	if byKey[MarketRegimeBull].Summaries[0].AverageReturn != 4 || byKey[MarketRegimeBear].Summaries[0].AverageReturn != -3 || byKey[MarketRegimeUnlabeled].Signals != 1 {
		t.Fatalf("unexpected regime summaries: %+v", report.Regimes)
	}
}

func TestComponentAnalysisDeduplicatesHorizonsAndClassifiesRelations(t *testing.T) {
	items := make([]SignalOutcome, 0, 48)
	for index := 0; index < 24; index++ {
		date := fmt.Sprintf("2026-07-%02d", index+1)
		trend := float64(index + 1)
		components := map[string]float64{
			"trend-breakout":    trend,
			"ma-pullback":       trend * 2,
			"relative-momentum": float64(25 - index),
		}
		states := map[string]string{
			"trend-breakout":    "观察",
			"ma-pullback":       "观察",
			"relative-momentum": "观察",
		}
		for _, horizon := range []int{OutcomeHorizon1D, OutcomeHorizon5D} {
			items = append(items, SignalOutcome{
				Key: fmt.Sprintf("sh600%03d:%s:%d", index, date, horizon), Symbol: fmt.Sprintf("sh600%03d", index),
				SignalDate: date, SignalAsOf: time.Date(2026, 7, index+1, 15, 0, 0, 0, time.Local),
				Horizon: horizon, Status: OutcomeReady, BenchmarkAvailable: true,
				Score: 50 + trend, ReturnPercent: trend / 10, ExcessReturn: trend / 20,
				StrategyScores: components, StrategyStates: states,
				StrategyNames: map[string]string{"trend-breakout": "趋势突破", "ma-pullback": "均线回踩", "relative-momentum": "相对动量"},
			})
		}
	}

	report := BuildOutcomeReport(items, time.Date(2026, 8, 21, 16, 0, 0, 0, time.Local), nil)
	if report.ComponentAnalysis.Signals != 24 {
		t.Fatalf("coverage signal count repeated across horizons: got %d", report.ComponentAnalysis.Signals)
	}
	if len(report.ComponentAnalysis.Correlations) != len(report.ComponentAnalysis.Components)*len(report.ComponentAnalysis.Components) {
		t.Fatalf("expected complete correlation matrix, got %d cells", len(report.ComponentAnalysis.Correlations))
	}
	find := func(left, right string) ComponentCorrelation {
		for _, cell := range report.ComponentAnalysis.Correlations {
			if cell.LeftKey == left && cell.RightKey == right {
				return cell
			}
		}
		t.Fatalf("correlation cell missing: %s/%s", left, right)
		return ComponentCorrelation{}
	}
	overlap := find("trend-breakout", "ma-pullback")
	if !overlap.SampleSufficient || overlap.Relation != "overlap" || overlap.RankCorrelation < 0.99 {
		t.Fatalf("expected overlap relation, got %+v", overlap)
	}
	inverse := find("trend-breakout", "relative-momentum")
	if !inverse.SampleSufficient || inverse.Relation != "inverse" || inverse.RankCorrelation > -0.99 {
		t.Fatalf("expected inverse relation, got %+v", inverse)
	}
	coverageCheck := outcomeCheckByKey(t, report.Assessment.Checks, "component-coverage")
	if !coverageCheck.Required || coverageCheck.Passed {
		t.Fatalf("component coverage gate did not activate after 20 representative signals: %+v", coverageCheck)
	}
	correlationCheck := outcomeCheckByKey(t, report.Assessment.Checks, "component-correlation")
	if !correlationCheck.Required || correlationCheck.Passed || report.ComponentAnalysis.RedundantPairs != 1 {
		t.Fatalf("redundant component pair did not fail correlation gate: check=%+v analysis=%+v", correlationCheck, report.ComponentAnalysis)
	}
}

func TestComponentRegimeMetricsRequireAvailableAndActiveSamples(t *testing.T) {
	items := make([]SignalOutcome, 0, 16)
	appendRegime := func(regime string, sign float64, offset int) {
		for index := 0; index < 8; index++ {
			date := fmt.Sprintf("2026-06-%02d", offset+index+1)
			score := float64(index + 8)
			items = append(items, SignalOutcome{
				Key: fmt.Sprintf("%s:%s:5", regime, date), Symbol: fmt.Sprintf("%s-%02d", regime, index),
				SignalDate: date, SignalAsOf: time.Date(2026, 6, offset+index+1, 15, 0, 0, 0, time.Local),
				Horizon: OutcomeHorizon5D, Status: OutcomeReady, BenchmarkAvailable: true, MarketRegime: regime,
				Score: 60 + score, ReturnPercent: sign * score / 10, ExcessReturn: sign * score / 10,
				StrategyScores: map[string]float64{"trend-breakout": score},
				StrategyStates: map[string]string{"trend-breakout": "触发"},
				StrategyNames:  map[string]string{"trend-breakout": "趋势突破"},
			})
		}
	}
	appendRegime(MarketRegimeBull, 1, 0)
	appendRegime(MarketRegimeRange, -1, 8)

	report := BuildOutcomeReport(items, time.Date(2026, 8, 21, 16, 0, 0, 0, time.Local), nil)
	analysis := report.ComponentAnalysis
	if analysis.SufficientRegimeCells != 2 || analysis.PositiveRegimeCells != 1 || analysis.NegativeRegimeCells != 1 {
		t.Fatalf("unexpected regime counters: %+v", analysis)
	}
	find := func(regime string) ComponentRegimeMetric {
		for _, metric := range analysis.RegimeMetrics {
			if metric.ComponentKey == "trend-breakout" && metric.Regime == regime && metric.Horizon == OutcomeHorizon5D {
				return metric
			}
		}
		t.Fatalf("regime metric missing: %s", regime)
		return ComponentRegimeMetric{}
	}
	bull := find(MarketRegimeBull)
	if !bull.SampleSufficient || bull.Samples != 8 || bull.ActiveSamples != 8 || bull.State != "positive" {
		t.Fatalf("unexpected bull metric: %+v", bull)
	}
	rangeMetric := find(MarketRegimeRange)
	if !rangeMetric.SampleSufficient || rangeMetric.State != "negative" {
		t.Fatalf("unexpected range metric: %+v", rangeMetric)
	}
}

func TestComponentWalkForwardUsesDateSeparatedFolds(t *testing.T) {
	items := make([]SignalOutcome, 0, 40)
	start := time.Date(2026, 1, 5, 15, 0, 0, 0, time.Local)
	for index := 0; index < 40; index++ {
		asOf := start.AddDate(0, 0, index)
		items = append(items, SignalOutcome{
			Key: fmt.Sprintf("sh600000:%s:5", asOf.Format("2006-01-02")), SignalID: fmt.Sprintf("wf-%02d", index),
			Symbol: "sh600000", SignalDate: asOf.Format("2006-01-02"), SignalAsOf: asOf,
			Score: 55 + float64(index), State: StateTriggered, Horizon: OutcomeHorizon5D, Status: OutcomeReady,
			BenchmarkAvailable: true, ExcessReturn: float64(index+1) / 10,
			StrategyScores: map[string]float64{"trend-breakout": 10 + float64(index)},
			StrategyStates: map[string]string{"trend-breakout": "触发"},
			StrategyNames:  map[string]string{"trend-breakout": "趋势突破"},
		})
	}

	report := BuildOutcomeReport(items, start.AddDate(0, 0, 50), nil)
	metric := report.WalkForward.Metrics[0]
	if metric.ComponentKey != "trend-breakout" || len(metric.Folds) != 2 {
		t.Fatalf("unexpected walk-forward metric: %+v", report.WalkForward)
	}
	if !metric.SampleSufficient || !metric.WeightStable || metric.State != "positive" {
		t.Fatalf("expected stable positive component validation, got %+v", metric)
	}
	if metric.Folds[0].ValidationEnd >= metric.Folds[1].ValidationStart {
		t.Fatalf("validation folds overlap or are not ordered: %+v", metric.Folds)
	}
	if metric.Folds[0].TrainEnd >= metric.Folds[0].ValidationStart || metric.Folds[1].TrainEnd >= metric.Folds[1].ValidationStart {
		t.Fatalf("training data leaked into validation window: %+v", metric.Folds)
	}
}

func TestPortfolioConstraintAnalysisFlagsIndustryAndComponentConcentration(t *testing.T) {
	items := make([]SignalOutcome, 0, 5)
	for index := 0; index < 5; index++ {
		date := "2026-08-20"
		items = append(items, SignalOutcome{
			Key: fmt.Sprintf("sh60000%d:%s:5", index, date), SignalID: fmt.Sprintf("portfolio-%d", index),
			Symbol: fmt.Sprintf("sh60000%d", index), Name: "测试股票", Industry: "银行", SignalDate: date,
			SignalAsOf: time.Date(2026, 8, 20, 14, index, 0, 0, time.Local), Score: 70, State: StateTriggered,
			Horizon: OutcomeHorizon5D, Status: OutcomeReady, BenchmarkAvailable: true, ExcessReturn: 1,
			StrategyScores: map[string]float64{"trend-breakout": 16},
			StrategyStates: map[string]string{"trend-breakout": "触发"},
			StrategyNames:  map[string]string{"trend-breakout": "趋势突破"},
		})
	}

	report := BuildOutcomeReport(items, time.Date(2026, 8, 28, 16, 0, 0, 0, time.Local), nil)
	if len(report.Portfolio.Horizons) != 1 {
		t.Fatalf("unexpected portfolio horizons: %+v", report.Portfolio)
	}
	metric := report.Portfolio.Horizons[0]
	if metric.CandidateDays != 1 || metric.SampleSufficient || metric.Passed {
		t.Fatalf("five signals on one day should remain below multi-day gate: %+v", metric)
	}
	if len(metric.Recent) != 1 || metric.Recent[0].LargestIndustry != "银行" || metric.Recent[0].LargestIndustryPercent != 100 || metric.Recent[0].LargestComponentPercent != 100 {
		t.Fatalf("unexpected concentration metrics: %+v", metric.Recent)
	}
	if len(metric.Recent[0].Violations) < 2 {
		t.Fatalf("expected industry and component concentration violations: %+v", metric.Recent[0])
	}
}

func TestPortfolioConstraintAnalysisPassesDiversifiedCandidateDays(t *testing.T) {
	items := make([]SignalOutcome, 0, 25)
	components := []string{"trend-breakout", "ma-pullback", "relative-momentum", "price-volume", "fund-support"}
	industries := []string{"银行", "半导体", "医药", "食品饮料", "通信"}
	start := time.Date(2026, 8, 3, 14, 0, 0, 0, time.Local)
	for dayIndex := 0; dayIndex < 5; dayIndex++ {
		date := start.AddDate(0, 0, dayIndex).Format("2006-01-02")
		for itemIndex := 0; itemIndex < 5; itemIndex++ {
			key := components[itemIndex]
			items = append(items, SignalOutcome{
				Key: fmt.Sprintf("sh60%d%d:%s:5", dayIndex, itemIndex, date), SignalID: fmt.Sprintf("diversified-%d-%d", dayIndex, itemIndex),
				Symbol: fmt.Sprintf("sh60%d%d", dayIndex, itemIndex), Name: "测试股票", Industry: industries[itemIndex], SignalDate: date,
				SignalAsOf: start.AddDate(0, 0, dayIndex).Add(time.Duration(itemIndex) * time.Minute), Score: 70, State: StateTriggered,
				Horizon: OutcomeHorizon5D, Status: OutcomeReady, BenchmarkAvailable: true, ExcessReturn: 1,
				StrategyScores: map[string]float64{key: 16}, StrategyStates: map[string]string{key: "触发"}, StrategyNames: map[string]string{key: key},
			})
		}
	}

	report := BuildOutcomeReport(items, time.Date(2026, 8, 20, 16, 0, 0, 0, time.Local), nil)
	metric := report.Portfolio.Horizons[0]
	if !metric.SampleSufficient || !metric.Passed || metric.CandidateDays != 5 || metric.PassedDays != 5 || metric.ViolatingDays != 0 {
		t.Fatalf("diversified portfolio days should pass: %+v", metric)
	}
	check := outcomeCheckByKey(t, report.Assessment.Checks, "portfolio-constraints")
	if !check.Required || !check.Passed {
		t.Fatalf("portfolio gate did not pass after sufficient diversified days: %+v", check)
	}
}

func strategyTestScore(active bool, score float64) float64 {
	if active {
		return score
	}
	return 4
}

func outcomeCheckByKey(t *testing.T, checks []OutcomeCheck, key string) OutcomeCheck {
	t.Helper()
	for _, check := range checks {
		if check.Key == key {
			return check
		}
	}
	t.Fatalf("outcome check missing: %s", key)
	return OutcomeCheck{}
}
