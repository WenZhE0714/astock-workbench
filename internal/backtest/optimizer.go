package backtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

const DefaultMaxOptimizationCandidates = 30

type Optimizer struct {
	engine Engine
	now    func() time.Time
}

func NewOptimizer(engine Engine) *Optimizer {
	return &Optimizer{engine: engine, now: time.Now}
}

func DefaultOptimizationRequest(base Request, train, validate, outOfSample Period) OptimizationRequest {
	return OptimizationRequest{
		BaseRequest: base, Train: train, Validate: validate, OutOfSample: outOfSample,
		MaxCandidates: DefaultMaxOptimizationCandidates, MinimumValidationTrades: 3,
		MaximumValidationDrawdown: 30, MaximumPerformanceGap: 40,
		MinimumValidationReturn: 0, UseAI: true,
	}
}

func validatePeriod(name string, period Period) error {
	if period.Start.IsZero() || period.End.IsZero() || period.Start.After(period.End) {
		return fmt.Errorf("%s区间无效", name)
	}
	return nil
}

func validateOptimizationRequest(request OptimizationRequest) error {
	if request.MaxCandidates < 1 || request.MaxCandidates > 200 {
		return fmt.Errorf("候选数量必须在 1 到 200 之间")
	}
	if request.MinimumValidationTrades < 0 || !finiteValues(
		request.MaximumValidationDrawdown, request.MaximumPerformanceGap, request.MinimumValidationReturn,
	) || request.MaximumValidationDrawdown <= 0 || request.MaximumPerformanceGap <= 0 {
		return fmt.Errorf("验证门禁参数无效")
	}
	if err := validatePeriod("训练", request.Train); err != nil {
		return err
	}
	if err := validatePeriod("验证", request.Validate); err != nil {
		return err
	}
	if err := validatePeriod("样本外", request.OutOfSample); err != nil {
		return err
	}
	if !request.Train.End.Before(request.Validate.Start) || !request.Validate.End.Before(request.OutOfSample.Start) {
		return fmt.Errorf("训练、验证和样本外区间必须按时间先后排列且不能重叠")
	}
	base := request.BaseRequest
	base.Start, base.End = request.Train.Start, request.Train.End
	if err := validateRequest(base); err != nil {
		return err
	}
	return validateTechnicalBounds(base.Technical)
}

func validateTechnicalBounds(parameters TechnicalParameters) error {
	mode := parameters.EffectiveEntryMode()
	if !finiteValues(parameters.VolumeRatioMin, parameters.StopLoss, parameters.TakeProfit, parameters.MaxPosition) ||
		(mode != EntryModeBreakout && mode != EntryModeReclaim && mode != EntryModePullback) ||
		parameters.FastMA < 2 || parameters.FastMA > 60 ||
		parameters.SlowMA <= parameters.FastMA || parameters.SlowMA > 250 ||
		parameters.BreakoutDays < 2 || parameters.BreakoutDays > 120 ||
		parameters.VolumeRatioMin < .5 || parameters.VolumeRatioMin > 5 ||
		parameters.StopLoss < .01 || parameters.StopLoss > .5 ||
		parameters.TakeProfit < .02 || parameters.TakeProfit > 2 ||
		parameters.MaxHoldingDays < 1 || parameters.MaxHoldingDays > 252 ||
		parameters.MaxPosition < .01 || parameters.MaxPosition > 1 {
		return fmt.Errorf("候选参数超出受控搜索边界")
	}
	return nil
}

func ValidateTechnicalParameters(parameters TechnicalParameters) error {
	return validateTechnicalBounds(parameters)
}

func finiteValues(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func parameterKey(parameters TechnicalParameters) string {
	return strings.Join([]string{
		parameters.EffectiveEntryMode(),
		strconv.Itoa(parameters.FastMA), strconv.Itoa(parameters.SlowMA), strconv.Itoa(parameters.BreakoutDays),
		strconv.FormatFloat(parameters.VolumeRatioMin, 'g', -1, 64),
		strconv.FormatFloat(parameters.StopLoss, 'g', -1, 64),
		strconv.FormatFloat(parameters.TakeProfit, 'g', -1, 64),
		strconv.Itoa(parameters.MaxHoldingDays), strconv.FormatFloat(parameters.MaxPosition, 'g', -1, 64),
	}, "/")
}

// GenerateCandidates samples a fixed grid without executing its Cartesian
// product. It always includes the supplied baseline and returns a stable order.
func GenerateCandidates(baseline TechnicalParameters, limit int) []TechnicalParameters {
	if limit < 1 {
		return nil
	}
	fastValues := []int{10, 20, 30}
	slowValues := []int{40, 60, 90}
	breakoutValues := []int{10, 20, 40}
	volumeValues := []float64{1.0, 1.2, 1.5}
	stopValues := []float64{.05, .08, .10}
	takeValues := []float64{.12, .20, .30}
	holdingValues := []int{20, 40, 60}

	all := make([]TechnicalParameters, 0, 2187)
	seen := make(map[string]bool)
	add := func(parameters TechnicalParameters) {
		if validateTechnicalBounds(parameters) != nil {
			return
		}
		key := parameterKey(parameters)
		if seen[key] {
			return
		}
		seen[key] = true
		all = append(all, parameters)
	}
	for _, fast := range fastValues {
		for _, slow := range slowValues {
			for _, breakout := range breakoutValues {
				for _, volume := range volumeValues {
					for _, stop := range stopValues {
						for _, take := range takeValues {
							for _, holding := range holdingValues {
								add(TechnicalParameters{
									FastMA: fast, SlowMA: slow, BreakoutDays: breakout, VolumeRatioMin: volume,
									StopLoss: stop, TakeProfit: take, MaxHoldingDays: holding, MaxPosition: baseline.MaxPosition,
								})
							}
						}
					}
				}
			}
		}
	}

	selected := make([]TechnicalParameters, 0, limit)
	selectedKeys := make(map[string]bool)
	appendSelected := func(parameters TechnicalParameters) {
		key := parameterKey(parameters)
		if len(selected) >= limit || selectedKeys[key] || validateTechnicalBounds(parameters) != nil {
			return
		}
		selectedKeys[key] = true
		selected = append(selected, parameters)
	}
	appendSelected(baseline)
	remaining := limit - len(selected)
	if remaining > 0 && len(all) > 0 {
		for index := 0; index < remaining; index++ {
			position := 0
			if remaining > 1 {
				position = index * (len(all) - 1) / (remaining - 1)
			}
			appendSelected(all[position])
		}
	}
	for _, candidate := range all {
		appendSelected(candidate)
		if len(selected) == limit {
			break
		}
	}
	// Running the widest indicator window first lets the range cache satisfy
	// every narrower candidate without another market-data request.
	sort.SliceStable(selected, func(left, right int) bool {
		leftWarmup := max(selected[left].SlowMA, selected[left].BreakoutDays+1, 21)
		rightWarmup := max(selected[right].SlowMA, selected[right].BreakoutDays+1, 21)
		if leftWarmup != rightWarmup {
			return leftWarmup > rightWarmup
		}
		return parameterKey(selected[left]) < parameterKey(selected[right])
	})
	return selected
}

func periodRequest(base Request, period Period, parameters TechnicalParameters) Request {
	base.Start = period.Start
	base.End = period.End
	base.Technical = parameters
	return base
}

func finiteMetrics(metrics Metrics) bool {
	values := []float64{
		metrics.TotalReturn, metrics.AnnualizedReturn, metrics.MaxDrawdown, metrics.Sharpe,
		metrics.BenchmarkReturn, metrics.ExcessReturn, metrics.WinRate, metrics.ProfitFactor,
		metrics.AverageTrade, metrics.AverageHoldingDays, metrics.Turnover, metrics.TotalFees, metrics.FinalEquity,
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func scoreCandidate(request OptimizationRequest, train, validate Metrics) (float64, []string) {
	reasons := make([]string, 0)
	if !finiteMetrics(train) || !finiteMetrics(validate) {
		reasons = append(reasons, "训练或验证指标包含非有限数值")
	}
	if validate.Trades < request.MinimumValidationTrades {
		reasons = append(reasons, fmt.Sprintf("验证交易数 %d 少于门槛 %d", validate.Trades, request.MinimumValidationTrades))
	}
	if math.Abs(validate.MaxDrawdown) > request.MaximumValidationDrawdown {
		reasons = append(reasons, fmt.Sprintf("验证最大回撤 %.2f%% 超过门槛 %.2f%%", math.Abs(validate.MaxDrawdown), request.MaximumValidationDrawdown))
	}
	gap := math.Abs(train.AnnualizedReturn - validate.AnnualizedReturn)
	if gap > request.MaximumPerformanceGap {
		reasons = append(reasons, fmt.Sprintf("训练/验证年化差 %.2f 个百分点超过门槛 %.2f", gap, request.MaximumPerformanceGap))
	}
	if validate.TotalReturn < request.MinimumValidationReturn {
		reasons = append(reasons, fmt.Sprintf("验证收益 %.2f%% 低于门槛 %.2f%%", validate.TotalReturn, request.MinimumValidationReturn))
	}
	excess := 0.0
	if validate.BenchmarkAvailable {
		excess = validate.ExcessReturn
	}
	score := validate.AnnualizedReturn + validate.Sharpe*5 + excess*.25 -
		math.Abs(validate.MaxDrawdown)*.60 - gap*.25 - validate.Turnover*.01
	if len(reasons) > 0 {
		score -= 1000
	}
	return score, reasons
}

func candidateBetter(left, right CandidateResult) bool {
	if left.Rejected != right.Rejected {
		return !left.Rejected
	}
	if math.Abs(left.Score-right.Score) > 1e-9 {
		return left.Score > right.Score
	}
	return left.ID < right.ID
}

func (optimizer *Optimizer) Optimize(ctx context.Context, request OptimizationRequest) (OptimizationResult, error) {
	return optimizer.OptimizeWithProgress(ctx, request, nil)
}

func (optimizer *Optimizer) OptimizeWithProgress(
	ctx context.Context,
	request OptimizationRequest,
	progress func(OptimizationProgress),
) (OptimizationResult, error) {
	if optimizer == nil || optimizer.engine == nil {
		return OptimizationResult{}, fmt.Errorf("优化回测引擎未初始化")
	}
	if err := validateOptimizationRequest(request); err != nil {
		return OptimizationResult{}, err
	}
	generatedAt := optimizer.now()
	result := OptimizationResult{
		ID: generatedAt.Format("OPT-20060102T150405"), GeneratedAt: generatedAt, Request: request,
		Warnings: []string{
			"候选排名只读取训练集和验证集；样本外数据在参数锁定前不运行、不参与排名",
			"每个区间独立从初始资金开始，不跨区间携带持仓；指标预热只读取该区间开始日之前的历史日K",
			"参数优化结果可能过拟合历史市场状态，不能视为稳定收益或未来收益保证",
		},
	}
	candidates := GenerateCandidates(request.BaseRequest.Technical, request.MaxCandidates)
	trainResults := make(map[string]Result, len(candidates))
	validationResults := make(map[string]Result, len(candidates))
	baselineKey := parameterKey(request.BaseRequest.Technical)
	for index, parameters := range candidates {
		candidateID := fmt.Sprintf("C%03d", index+1)
		train, err := optimizer.engine.Run(ctx, periodRequest(request.BaseRequest, request.Train, parameters))
		if err != nil {
			return result, fmt.Errorf("候选 %s 训练回测失败: %w", candidateID, err)
		}
		validate, err := optimizer.engine.Run(ctx, periodRequest(request.BaseRequest, request.Validate, parameters))
		if err != nil {
			return result, fmt.Errorf("候选 %s 验证回测失败: %w", candidateID, err)
		}
		score, reasons := scoreCandidate(request, train.Metrics, validate.Metrics)
		candidate := CandidateResult{
			ID: candidateID, Baseline: parameterKey(parameters) == baselineKey, Parameters: parameters,
			Train: train.Metrics, Validate: validate.Metrics, Score: score,
			Rejected: len(reasons) > 0, Reasons: reasons,
		}
		result.Candidates = append(result.Candidates, candidate)
		trainResults[candidateID] = train
		validationResults[candidateID] = validate
		if progress != nil {
			progress(OptimizationProgress{Phase: "candidate", Completed: index + 1, Total: len(candidates)})
		}
	}
	sort.SliceStable(result.Candidates, func(left, right int) bool {
		return candidateBetter(result.Candidates[left], result.Candidates[right])
	})
	for index := range result.Candidates {
		if result.Candidates[index].Rejected {
			continue
		}
		selected := result.Candidates[index]
		result.Selected = &selected
		train := trainResults[selected.ID]
		validation := validationResults[selected.ID]
		result.SelectedTrain = &train
		result.SelectedValidation = &validation
		break
	}
	if result.Selected == nil {
		result.Warnings = append(result.Warnings, "所有候选均未通过验证门禁；未执行样本外回测，实验仅用于诊断")
		return result, nil
	}
	// This is deliberately the only out-of-sample invocation and occurs after
	// ranking and selection are complete.
	if progress != nil {
		progress(OptimizationProgress{Phase: "out-of-sample", Completed: 1, Total: 1})
	}
	outOfSample, err := optimizer.engine.Run(ctx, periodRequest(request.BaseRequest, request.OutOfSample, result.Selected.Parameters))
	if err != nil {
		return result, fmt.Errorf("样本外回测失败: %w", err)
	}
	result.OutOfSample = &outOfSample
	return result, nil
}
