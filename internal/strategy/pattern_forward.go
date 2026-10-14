package strategy

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const PatternForwardVersion = "pattern-forward-v1"

func NewPatternForwardTask(name string, pool domain.PatternPool, basis string, now time.Time) (domain.PatternForwardTask, error) {
	if basis == "" {
		basis = domain.PatternPriceHFQ
	}
	if now.IsZero() || (basis != domain.PatternPriceHFQ && basis != domain.PatternPriceRaw) || len(pool.Members) == 0 || len(pool.Members) > domain.PatternPoolLimit || pool.Version == "" {
		return domain.PatternForwardTask{}, fmt.Errorf("前向任务需要有效股票池、价格口径及创建时间")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = pool.Name
	}
	if len(name) > 200 {
		return domain.PatternForwardTask{}, fmt.Errorf("任务名称过长")
	}
	start := now.In(chartLocation)
	if start.Hour() >= 15 {
		start = start.AddDate(0, 0, 1)
	}
	config := domain.PatternForwardConfig{Version: PatternForwardVersion, EngineVersion: PatternValidationVersion, PatternVersion: "classic-v1", Patterns: classicPatternIDs(), PriceBasis: basis,
		StartOn: start.Format(time.DateOnly), HistoryStart: start.AddDate(0, 0, -PatternValidationWarmupDays).Format(time.DateOnly), WarmupDays: PatternValidationWarmupDays, LookbackBars: classicLookback, PivotRadius: classicPivotRadius, ConfirmationBars: classicMaximumAge,
		PriceMethod: "unadjusted", VolumeMethod: "raw", QualityMethod: domain.CorporateActionScope, OpenMinute: 15*60 + 30, CloseMinute: 18 * 60, RetryMinutes: 10}
	if basis == domain.PatternPriceHFQ {
		config.PriceMethod, config.VolumeMethod, config.QualityMethod = domain.PatternAdjustmentMethod, domain.PatternVolumeMethod, domain.PatternAdjustmentQualityMethod
	}
	task := domain.PatternForwardTask{Name: name, Pool: pool, Config: config, CreatedAt: now}
	task.ConfigHash = task.Fingerprint()
	return task, nil
}

func PatternForwardWindow(task domain.PatternForwardTask, now time.Time) bool {
	local := now.In(chartLocation)
	minutes := local.Hour()*60 + local.Minute()
	return local.Format(time.DateOnly) >= task.Config.StartOn && minutes >= task.Config.OpenMinute && minutes < task.Config.CloseMinute
}

func PatternForwardCompatible(task domain.PatternForwardTask) bool {
	current, err := NewPatternForwardTask(task.Name, task.Pool, task.Config.PriceBasis, task.CreatedAt)
	return err == nil && task.ConfigHash == task.Fingerprint() && current.ConfigHash == task.ConfigHash
}

func BuildPatternForwardSnapshot(ctx context.Context, task domain.PatternForwardTask, day string, input domain.PatternValidationInput, benchmark domain.PatternForwardBenchmark, now time.Time, seen map[string]bool) (domain.PatternForwardSnapshot, error) {
	result := domain.PatternForwardSnapshot{TaskID: task.ID, Date: day, Symbol: input.Symbol, ConfigHash: task.ConfigHash, CollectedAt: now, BenchmarkHash: benchmark.Hash, Samples: []domain.PatternValidationSample{}}
	if !PatternForwardCompatible(task) {
		return result, fmt.Errorf("冻结规则版本或配置不匹配，需新建任务")
	}
	if !PatternForwardWindow(task, now) || now.In(chartLocation).Format(time.DateOnly) != day || benchmark.Date != day {
		return result, fmt.Errorf("仅能在当日收盘采集窗口留存快照")
	}
	member := false
	for _, item := range task.Pool.Members {
		member = member || item.Symbol == input.Symbol
	}
	if !member {
		return result, fmt.Errorf("证券不属于冻结股票池")
	}
	request := domain.PatternValidationRequest{Symbols: []string{input.Symbol}, Start: day, End: day, PriceBasis: task.Config.PriceBasis}
	report, err := BuildPatternValidation(ctx, request, []domain.PatternValidationInput{input, benchmark.Input}, now)
	if err != nil {
		return result, err
	}
	coverage := report.Coverage[0]
	if coverage.LastDate != day || coverage.WarmupBars < task.Config.LookbackBars {
		return result, fmt.Errorf("当日日K尚未就绪或预热不足%d根", task.Config.LookbackBars)
	}
	bars := report.Inputs[0].Bars
	if len(bars) == 0 || bars[len(bars)-1].Volume <= 0 {
		return result, fmt.Errorf("当日无有效成交量，停牌或数据缺失待核验")
	}
	result.Input, result.InputHash, result.Coverage = report.Inputs[0], report.InputHash, coverage
	if len(report.Adjustments) > 0 {
		result.Adjustment = &report.Adjustments[0]
	}
	for _, sample := range report.Samples {
		if sample.ObservedOn != day || seen[sample.ID] {
			continue
		}
		// Forward observation archives the information available on this day.
		// Subsequent outcomes belong in a separate append-only lifecycle ledger.
		sample.Outcomes = nil
		result.Samples = append(result.Samples, sample)
	}
	return result, nil
}

func PreparePatternForwardBenchmark(input domain.PatternValidationInput, day string) (domain.PatternValidationInput, error) {
	if input.Symbol != "sh000300" {
		return input, fmt.Errorf("前向基准须为沪深300")
	}
	bars, coverage := patternValidationBars(input, day)
	if coverage.Error != "" || len(bars) == 0 || coverage.LastDate != day || bars[len(bars)-1].Volume <= 0 {
		return input, fmt.Errorf("沪深300当日日K尚未就绪或数据无效")
	}
	input.Bars = bars
	return input, nil
}

func classicPatternIDs() []string {
	return []string{"ascending-triangle", "bear-flag", "bull-flag", "descending-triangle", "double-bottom", "double-top", "head-shoulders-bottom", "head-shoulders-top"}
}
