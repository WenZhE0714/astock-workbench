package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/marketregime"
)

const PatternValidationVersion = "pattern-validation-v1"

func ValidatePatternValidationRequest(request domain.PatternValidationRequest, now time.Time) (string, error) {
	if now.IsZero() || len(request.Symbols) == 0 || len(request.Symbols) > 10 {
		return "", fmt.Errorf("形态验证需要1至10只股票及有效时间")
	}
	start, startErr := time.Parse(time.DateOnly, request.Start)
	end, endErr := time.Parse(time.DateOnly, request.End)
	local := now.In(chartLocation)
	if startErr != nil || endErr != nil || start.After(end) || request.End > local.Format(time.DateOnly) || end.Sub(start) > 3*366*24*time.Hour {
		return "", fmt.Errorf("验证日期须为有效的起止日期，跨度不超过3年，结束日不得晚于今天")
	}
	if local.Hour() < 15 {
		local = local.AddDate(0, 0, -1)
	}
	return min(request.End, local.Format(time.DateOnly)), nil
}

func patternValidationBars(input domain.PatternValidationInput, cutoff string) ([]domain.DailyBar, domain.PatternValidationCoverage) {
	coverage := domain.PatternValidationCoverage{Symbol: input.Symbol, Error: input.Error}
	if input.Error != "" {
		return nil, coverage
	}
	bars := make([]domain.DailyBar, 0, len(input.Bars))
	for _, bar := range input.Bars {
		_, err := time.Parse(time.DateOnly, bar.Date)
		if err == nil && bar.Date > cutoff {
			continue
		}
		if err != nil || !validBar(bar) || bar.High < math.Max(bar.Open, bar.Close) || bar.Low > math.Min(bar.Open, bar.Close) || (bar.Symbol != "" && bar.Symbol != input.Symbol) {
			coverage.Error = "包含无效日期、证券或OHLC数据，未纳入验证"
			return nil, coverage
		}
		coverage.Cached = coverage.Cached || strings.Contains(bar.Source, "缓存")
		bars = append(bars, bar)
	}
	sort.SliceStable(bars, func(i, j int) bool { return bars[i].Date < bars[j].Date })
	clean := bars[:0]
	for _, bar := range bars {
		if len(clean) > 0 && clean[len(clean)-1].Date == bar.Date {
			previous := clean[len(clean)-1]
			if previous.Open != bar.Open || previous.High != bar.High || previous.Low != bar.Low || previous.Close != bar.Close || previous.Volume != bar.Volume || previous.Amount != bar.Amount {
				coverage.Error = "同一日期存在冲突日K，未纳入验证"
				return nil, coverage
			}
			continue
		}
		clean = append(clean, bar)
	}
	if len(clean) == 0 {
		coverage.Error = "截止日期前没有已收盘日K"
		return nil, coverage
	}
	if len(clean) > 600 {
		clean = clean[len(clean)-600:]
	}
	coverage.Bars, coverage.FirstDate, coverage.LastDate = len(clean), clean[0].Date, clean[len(clean)-1].Date
	sources := []string{}
	for _, bar := range clean {
		coverage.Cached = coverage.Cached || strings.Contains(bar.Source, "缓存")
		if bar.Source != "" && !containsPatternSource(sources, bar.Source) {
			sources = append(sources, bar.Source)
		}
	}
	coverage.Source = strings.Join(sources, " / ")
	return clean, coverage
}

func containsPatternSource(sources []string, source string) bool {
	for _, item := range sources {
		if item == source {
			return true
		}
	}
	return false
}

// Identity excludes mutable state, confirmation anchors and moving line ends.
func patternSampleID(symbol string, structure domain.ChartStructure) string {
	anchors := []domain.ChartAnchor{}
	for _, anchor := range structure.Anchors {
		if anchor.Label != "失效" && anchor.Label != "收盘确认" {
			anchors = append(anchors, anchor)
		}
	}
	coreSize := len(anchors)
	switch structure.ID {
	case "double-bottom", "double-top", "bull-flag", "bear-flag":
		coreSize = 2
	case "head-shoulders-bottom", "head-shoulders-top", "ascending-triangle", "descending-triangle":
		coreSize = 4
	}
	anchors = anchors[:min(coreSize, len(anchors))]
	data, _ := json.Marshal(struct {
		Symbol, Kind string
		Anchors      []domain.ChartAnchor
	}{symbol, structure.ID, anchors})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func BuildPatternValidation(ctx context.Context, request domain.PatternValidationRequest, inputs []domain.PatternValidationInput, now time.Time) (domain.PatternValidationReport, error) {
	cutoff, err := ValidatePatternValidationRequest(request, now)
	if err != nil {
		return domain.PatternValidationReport{}, err
	}
	report := domain.PatternValidationReport{Version: PatternValidationVersion, GeneratedAt: now, Request: request, Cutoff: cutoff, Horizons: []int{5, 10, 20}, Coverage: []domain.PatternValidationCoverage{}, Samples: []domain.PatternValidationSample{}, Inputs: []domain.PatternValidationInput{}, Warnings: []string{
		"使用本次取得的历史日K逐日回放，属于历史重建，不是当时已留存的前向信号；股票池为手动选择，存在选择偏差。",
		"窗口采用沪深300日K日期，确认日之后第5/10/20个基准交易日；缺少对应股票日K则不计算该窗口，停牌与数据缺失未区分。",
		"价格涨跌以确认日收盘为基准；看跌形态的方向表现为价格涨跌取反，不代表可做空收益。未计费用、滑点、涨跌停与成交可行性。",
		"价格未复权，除权除息可能影响形态及涨跌幅；缓存状态、实际数据覆盖和未成熟窗口单独展示。",
		"同一冻结结构只计一次；识别后30根日K内未确认则观察到期。统计为描述性结果，小样本不能证明形态有效。",
	}}
	clean := map[string][]domain.DailyBar{}
	bySymbol := map[string]domain.PatternValidationInput{}
	for _, input := range inputs {
		bySymbol[input.Symbol] = input
	}
	symbols := append(append([]string{}, request.Symbols...), "sh000300")
	seenSymbols := map[string]bool{}
	for _, symbol := range symbols {
		if seenSymbols[symbol] {
			continue
		}
		seenSymbols[symbol] = true
		input, exists := bySymbol[symbol]
		if !exists {
			input = domain.PatternValidationInput{Symbol: symbol, Error: "未取得日K"}
		}
		bars, coverage := patternValidationBars(input, cutoff)
		clean[symbol] = bars
		report.Coverage = append(report.Coverage, coverage)
		report.Inputs = append(report.Inputs, domain.PatternValidationInput{Symbol: symbol, Bars: bars, Error: coverage.Error})
	}
	encoded, err := json.Marshal(report.Inputs)
	if err != nil {
		return report, err
	}
	sum := sha256.Sum256(encoded)
	report.InputHash = hex.EncodeToString(sum[:])
	usable := 0
	for coverageIndex := range report.Coverage {
		coverage := &report.Coverage[coverageIndex]
		if coverage.Symbol == "sh000300" {
			continue
		}
		bars := clean[coverage.Symbol]
		if len(bars) < 25 {
			if coverage.Error == "" {
				coverage.Error = "有效日K少于25根"
			}
			continue
		}
		usable++
		seen := map[string]bool{}
		for i := 24; i < len(bars); i++ {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			for _, structure := range classicChartStructures(bars[:i+1], true) {
				id := patternSampleID(coverage.Symbol, structure)
				if seen[id] {
					continue
				}
				seen[id] = true
				if structure.Pattern.ReadyOn != bars[i].Date {
					coverage.LateDiscoveries++
					continue
				}
				if bars[i].Date < request.Start {
					continue
				}
				at, _ := time.ParseInLocation(time.DateOnly, bars[i].Date, chartLocation)
				analysis, analysisErr := AnalyzeChart(coverage.Symbol, bars[:i+1], at.Add(16*time.Hour), bars[i].Date)
				if analysisErr != nil {
					return report, analysisErr
				}
				sample := buildPatternSample(id, coverage.Symbol, structure, analysis.Fingerprint, bars, i, clean["sh000300"])
				report.Samples = append(report.Samples, sample)
				if len(report.Samples) > 2000 {
					return report, fmt.Errorf("样本超过2000条，请缩小股票池或日期区间")
				}
			}
		}
	}
	if usable == 0 {
		return report, fmt.Errorf("没有可验证的股票日K，请检查日期及数据源")
	}
	sort.Slice(report.Samples, func(i, j int) bool {
		if report.Samples[i].ObservedOn != report.Samples[j].ObservedOn {
			return report.Samples[i].ObservedOn > report.Samples[j].ObservedOn
		}
		return report.Samples[i].ID < report.Samples[j].ID
	})
	return report, ctx.Err()
}

func buildPatternSample(id, symbol string, structure domain.ChartStructure, fingerprint string, bars []domain.DailyBar, ready int, benchmark []domain.DailyBar) domain.PatternValidationSample {
	sample := domain.PatternValidationSample{ID: id, Symbol: symbol, PatternID: structure.ID, Name: structure.Name, Bias: structure.Pattern.Bias, ObservedOn: bars[ready].Date, Snapshot: structure, SnapshotFingerprint: fingerprint}
	// Reuse the chart lifecycle against the first observable, frozen geometry.
	pattern := *structure.Pattern
	pattern.ConfirmedOn, pattern.InvalidatedOn = "", ""
	frozen := domain.ChartStructure{Pattern: &pattern, Anchors: []domain.ChartAnchor{structure.Anchors[0]}}
	confirmationEnd := min(len(bars), ready+classicMaximumAge+1)
	finishClassicStructure(&frozen, bars[:confirmationEnd], true, ready, classicATR(bars, ready+1), "冻结边界")
	if pattern.ConfirmedOn != "" {
		pattern.ConfirmedOn, pattern.InvalidatedOn = "", ""
		frozen = domain.ChartStructure{Pattern: &pattern, Anchors: []domain.ChartAnchor{structure.Anchors[0]}}
		finishClassicStructure(&frozen, bars, true, ready, classicATR(bars, ready+1), "冻结边界")
	}
	sample.State, sample.ConfirmedOn, sample.InvalidatedOn = frozen.State, pattern.ConfirmedOn, pattern.InvalidatedOn
	if sample.ConfirmedOn == "" && sample.InvalidatedOn == "" && len(bars) >= ready+classicMaximumAge+1 {
		sample.State, sample.ExpiredOn = "expired", bars[ready+classicMaximumAge].Date
	}
	sample.RegimeDate = sample.ObservedOn
	confirmedIndex := -1
	if sample.ConfirmedOn != "" {
		sample.RegimeDate = sample.ConfirmedOn
		confirmedIndex = sort.Search(len(bars), func(i int) bool { return bars[i].Date >= sample.ConfirmedOn })
		sample.ConfirmationClose = chartNumber(bars[confirmedIndex].Close)
	}
	sample.Regime = string(marketregime.ClassifyBefore(benchmark, sample.RegimeDate))
	for _, horizon := range []int{5, 10, 20} {
		sample.Outcomes = append(sample.Outcomes, patternOutcome(sample, bars, confirmedIndex, benchmark, horizon))
	}
	return sample
}

func patternOutcome(sample domain.PatternValidationSample, bars []domain.DailyBar, confirmed int, benchmark []domain.DailyBar, horizon int) domain.PatternValidationOutcome {
	outcome := domain.PatternValidationOutcome{Horizon: horizon, State: "unconfirmed"}
	if confirmed < 0 {
		return outcome
	}
	index := sort.Search(len(benchmark), func(i int) bool { return benchmark[i].Date >= sample.ConfirmedOn })
	if index == len(benchmark) || benchmark[index].Date != sample.ConfirmedOn {
		outcome.State, outcome.Detail = "unavailable", "基准日期未覆盖确认日"
		return outcome
	}
	available := min(horizon, len(benchmark)-index-1)
	outcome.ObservedDays = available
	if available < horizon {
		outcome.State, outcome.Detail = "pending", "等待后续基准交易日或日K补齐"
		return outcome
	}
	outcome.Through = benchmark[index+horizon].Date
	lookup := map[string]domain.DailyBar{}
	for _, bar := range bars[confirmed+1:] {
		lookup[bar.Date] = bar
	}
	base, high, low := bars[confirmed].Close, bars[confirmed].Close, bars[confirmed].Close
	failed := false
	var last domain.DailyBar
	for _, day := range benchmark[index+1 : index+horizon+1] {
		bar, exists := lookup[day.Date]
		if !exists {
			outcome.State, outcome.Detail = "unavailable", "缺少股票日K: "+day.Date
			return outcome
		}
		high, low, last = math.Max(high, bar.High), math.Min(low, bar.Low), bar
		if sample.Bias == "bullish" {
			failed = failed || bar.Low <= sample.Snapshot.Pattern.InvalidationPrice
		} else {
			failed = failed || bar.High >= sample.Snapshot.Pattern.InvalidationPrice
		}
	}
	priceReturn, favorable, adverse := (last.Close/base-1)*100, (high/base-1)*100, (low/base-1)*100
	directionReturn := priceReturn
	if sample.Bias == "bearish" {
		directionReturn, favorable, adverse = -priceReturn, (1-low/base)*100, (1-high/base)*100
	}
	outcome.State, outcome.PriceReturn, outcome.DirectionReturn, outcome.Favorable, outcome.Adverse, outcome.Invalidated = "mature", chartNumber(priceReturn), chartNumber(directionReturn), chartNumber(favorable), chartNumber(adverse), &failed
	if outcome.PriceReturn == nil || outcome.DirectionReturn == nil || outcome.Favorable == nil || outcome.Adverse == nil {
		return domain.PatternValidationOutcome{Horizon: horizon, State: "unavailable", Through: outcome.Through, ObservedDays: available, Detail: "价格计算超出有效范围"}
	}
	return outcome
}
