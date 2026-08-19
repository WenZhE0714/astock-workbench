package backtest

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
)

func canonicalHash(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func BuildExperimentManifest(request ContinuousOptimizationRequest, cutoff, previousID, previousHoldoutEnd string) ExperimentManifest {
	tickers := append([]string(nil), request.BaseRequest.Tickers...)
	sort.Strings(tickers)
	candidates := make([]TechnicalParameters, 0, len(request.Proposals))
	for _, proposal := range request.Proposals {
		parameters := proposal.Parameters
		parameters.EntryMode = parameters.EffectiveEntryMode()
		candidates = append(candidates, parameters)
	}
	sort.SliceStable(candidates, func(i, j int) bool { return parameterKey(candidates[i]) < parameterKey(candidates[j]) })
	// A manifest identifies the experiment inputs, not the order in which
	// callers happened to provide them. Keep fold order for the walk-forward
	// schedule, but canonicalize the portfolio names before hashing.
	base := request.BaseRequest
	base.Tickers = tickers
	if base.Names != nil {
		names := make(map[string]string, len(base.Names))
		for _, ticker := range tickers {
			if name, ok := base.Names[ticker]; ok {
				names[ticker] = name
			}
		}
		base.Names = names
	}
	configuration := struct {
		Request                   Request           `json:"request"`
		Folds                     []WalkForwardFold `json:"folds"`
		Holdout                   Period            `json:"holdout"`
		MinimumValidationTrades   int               `json:"minimum_validation_trades"`
		MinimumPositiveFoldRatio  float64           `json:"minimum_positive_fold_ratio"`
		MaximumValidationDrawdown float64           `json:"maximum_validation_drawdown"`
	}{base, request.Folds, request.Holdout, request.MinimumValidationTrades, request.MinimumPositiveFoldRatio, request.MaximumValidationDrawdown}
	return ExperimentManifest{
		SchemaVersion: 1, Strategy: request.BaseRequest.Strategy, StrategyVersion: request.BaseRequest.StrategyVersion,
		Tickers: tickers, DataCutoff: cutoff, Folds: request.Folds, Holdout: request.Holdout,
		CandidateSetHash: canonicalHash(candidates), ConfigurationHash: canonicalHash(configuration),
		PreviousExperiment: previousID, PreviousHoldoutEnd: previousHoldoutEnd,
	}
}

func parameterDistance(left, right TechnicalParameters) float64 {
	distance := 0.0
	if left.EffectiveEntryMode() != right.EffectiveEntryMode() {
		distance += 3
	}
	distance += math.Abs(float64(left.FastMA-right.FastMA)) / 10
	distance += math.Abs(float64(left.SlowMA-right.SlowMA)) / 30
	distance += math.Abs(float64(left.BreakoutDays-right.BreakoutDays)) / 10
	distance += math.Abs(left.VolumeRatioMin-right.VolumeRatioMin) / .3
	distance += math.Abs(left.StopLoss-right.StopLoss) / .03
	distance += math.Abs(left.TakeProfit-right.TakeProfit) / .08
	distance += math.Abs(float64(left.MaxHoldingDays-right.MaxHoldingDays)) / 20
	return distance
}

func enrichCandidateStability(candidates []ContinuousCandidateResult, agents []AgentResearchRun) {
	evaluated := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		evaluated[parameterKey(candidate.Proposal.Parameters)] = true
	}
	origins := make(map[string]map[string]bool)
	for _, agent := range agents {
		if agent.Status != "ok" {
			continue
		}
		for _, proposal := range agent.Proposals {
			key := parameterKey(proposal.Parameters)
			if !evaluated[key] {
				continue
			}
			if origins[key] == nil {
				origins[key] = make(map[string]bool)
			}
			origins[key][agent.Agent] = true
		}
	}
	baseScores := make([]float64, len(candidates))
	for index := range candidates {
		baseScores[index] = candidates[index].Score
	}
	for index := range candidates {
		candidate := &candidates[index]
		if candidate.Rejected {
			continue
		}
		candidate.ConsensusAgents = len(origins[parameterKey(candidate.Proposal.Parameters)])
		neighborScores := make([]float64, 0)
		for otherIndex := range candidates {
			if index == otherIndex || candidates[otherIndex].Rejected {
				continue
			}
			if parameterDistance(candidate.Proposal.Parameters, candidates[otherIndex].Proposal.Parameters) <= 2.5 {
				candidate.NeighborhoodSize++
				neighborScores = append(neighborScores, baseScores[otherIndex])
			}
		}
		if len(neighborScores) > 0 {
			for _, score := range neighborScores {
				candidate.NeighborhoodScore += score
			}
			candidate.NeighborhoodScore /= float64(len(neighborScores))
		}
		candidate.Score = baseScores[index] + float64(min(candidate.ConsensusAgents, 3))*.35 + float64(min(candidate.NeighborhoodSize, 4))*.20
		if candidate.NeighborhoodSize == 0 {
			candidate.Score -= 1
		}
	}
}

func evaluateContinuousDataQuality(result *ContinuousOptimizationResult) DataQualitySummary {
	quality := DataQualitySummary{Grade: "A", Passed: true}
	add := func(name string, passed bool, detail string, warning bool) {
		quality.Checks = append(quality.Checks, DataQualityCheck{Name: name, Passed: passed, Detail: detail, Warning: warning})
		if !passed && !warning {
			quality.Passed = false
			quality.Grade = "D"
		} else if !passed && quality.Grade == "A" {
			quality.Grade = "B"
		}
	}
	add("股票池", len(result.Request.BaseRequest.Tickers) >= 3, fmt.Sprintf("股票池%d只；组合稳定性建议至少3只", len(result.Request.BaseRequest.Tickers)), true)
	add("滚动窗口", len(result.Request.Folds) >= 3, fmt.Sprintf("滚动验证%d折", len(result.Request.Folds)), false)
	add("无未来数据", result.Request.BaseRequest.NoFutureData, fmt.Sprintf("no_future_data=%v", result.Request.BaseRequest.NoFutureData), false)
	add("点时股票池", result.Request.BaseRequest.PointInTimePool, "当前股票池是否为历史点时成份", true)
	add("复权口径", result.Request.BaseRequest.Adjustment == AdjustmentNone, "当前引擎要求全程不复权", false)
	if result.Selected != nil {
		foldEvidence := len(result.Selected.Folds) == len(result.Request.Folds)
		for _, fold := range result.Selected.Folds {
			for _, ticker := range result.Request.BaseRequest.Tickers {
				train := fold.TrainCoverage[ticker]
				validate := fold.ValidateCoverage[ticker]
				if train.Bars == 0 || validate.Bars == 0 || train.CoverageRatio < .8 || validate.CoverageRatio < .8 {
					foldEvidence = false
				}
			}
		}
		add("滚动数据覆盖", foldEvidence, "入选候选每折、每只股票至少覆盖组合交易日的80%", false)
	}
	if result.Holdout != nil {
		available := 0
		missing := make([]string, 0)
		for _, ticker := range result.Request.BaseRequest.Tickers {
			if source := result.Holdout.DataSources[ticker]; source != "" {
				available++
			} else {
				missing = append(missing, ticker)
			}
		}
		detail := fmt.Sprintf("可用数据源%d/%d", available, len(result.Request.BaseRequest.Tickers))
		if len(missing) > 0 {
			detail += "；缺失 " + strings.Join(missing, ",")
		}
		add("行情数据源", len(missing) == 0, detail, false)
		coverageOK := true
		coverageDetail := make([]string, 0, len(result.Request.BaseRequest.Tickers))
		for _, ticker := range result.Request.BaseRequest.Tickers {
			coverage := result.Holdout.DataCoverage[ticker]
			coverageDetail = append(coverageDetail, fmt.Sprintf("%s %.0f%%/%d", ticker, coverage.CoverageRatio*100, coverage.Bars))
			if coverage.Bars == 0 || coverage.CoverageRatio < .8 {
				coverageOK = false
			}
		}
		add("留出数据覆盖", coverageOK, strings.Join(coverageDetail, "；"), false)
		add("基准", result.Holdout.Metrics.BenchmarkAvailable, "沪深300基准是否可用", true)
	} else {
		add("最终留出", false, "最终留出尚未执行，不能形成晋级证据", false)
	}
	return quality
}

const (
	ContinuousStageResearch = "research-candidate"
	ContinuousStageShadow   = "shadow-ready"
	rejectedCandidateScore  = -1e12
)

type ContinuousOptimizer struct {
	engine Engine
	now    func() time.Time
}

func NewContinuousOptimizer(engine Engine) *ContinuousOptimizer {
	return &ContinuousOptimizer{engine: engine, now: time.Now}
}

func validateContinuousRequest(request ContinuousOptimizationRequest) error {
	if len(request.Proposals) == 0 || len(request.Proposals) > 100 {
		return fmt.Errorf("持续优化候选数量必须在 1 到 100 之间")
	}
	if len(request.Folds) < 2 || len(request.Folds) > 12 {
		return fmt.Errorf("滚动验证折数必须在 2 到 12 之间")
	}
	if request.MinimumValidationTrades < 0 || !finiteValues(request.MinimumPositiveFoldRatio, request.MaximumValidationDrawdown) || request.MinimumPositiveFoldRatio < 0 ||
		request.MinimumPositiveFoldRatio > 1 || request.MaximumValidationDrawdown <= 0 {
		return fmt.Errorf("持续优化门禁参数无效")
	}
	if err := validatePeriod("最终留出", request.Holdout); err != nil {
		return err
	}
	seen := make(map[string]bool, len(request.Proposals))
	seenParameters := make(map[string]bool, len(request.Proposals))
	for _, proposal := range request.Proposals {
		if proposal.ID == "" || seen[proposal.ID] {
			return fmt.Errorf("策略候选 ID 为空或重复")
		}
		seen[proposal.ID] = true
		if err := validateTechnicalBounds(proposal.Parameters); err != nil {
			return fmt.Errorf("候选 %s: %w", proposal.ID, err)
		}
		key := parameterKey(proposal.Parameters)
		if seenParameters[key] {
			return fmt.Errorf("候选 %s 与其他候选参数重复", proposal.ID)
		}
		seenParameters[key] = true
	}
	seenFolds := make(map[string]bool, len(request.Folds))
	for _, fold := range request.Folds {
		if fold.ID == "" || seenFolds[fold.ID] {
			return fmt.Errorf("滚动验证折 ID 为空或重复")
		}
		seenFolds[fold.ID] = true
		if err := validatePeriod(fold.ID+"训练", fold.Train); err != nil {
			return err
		}
		if err := validatePeriod(fold.ID+"验证", fold.Validate); err != nil {
			return err
		}
		if !fold.Train.End.Before(fold.Validate.Start) || !fold.Validate.End.Before(request.Holdout.Start) {
			return fmt.Errorf("%s 的训练、验证和最终留出区间顺序无效", fold.ID)
		}
	}
	for index := 1; index < len(request.Folds); index++ {
		previous, current := request.Folds[index-1], request.Folds[index]
		if !previous.Validate.End.Before(current.Validate.Start) {
			return fmt.Errorf("滚动验证窗口必须按时间递增且不能重复或重叠")
		}
	}
	for index, left := range request.Folds {
		for otherIndex := index + 1; otherIndex < len(request.Folds); otherIndex++ {
			right := request.Folds[otherIndex]
			if left.Validate.Start.Equal(right.Validate.Start) && left.Validate.End.Equal(right.Validate.End) {
				return fmt.Errorf("滚动验证窗口 %s 与 %s 重复", left.ID, right.ID)
			}
		}
	}
	for _, proposal := range request.Proposals {
		if proposal.Parameters.MaxPosition != request.BaseRequest.Technical.MaxPosition {
			return fmt.Errorf("候选 %s 不能修改账户最大仓位", proposal.ID)
		}
	}
	base := request.BaseRequest
	base.Start, base.End = request.Folds[0].Train.Start, request.Folds[0].Train.End
	return validateRequest(base)
}

func continuousCandidateScore(request ContinuousOptimizationRequest, folds []WalkForwardFoldResult) (float64, float64, int, float64, float64, []string) {
	if len(folds) == 0 {
		return rejectedCandidateScore, 0, 0, 0, 0, []string{"没有滚动验证结果"}
	}
	positive := 0
	trades := 0
	averageReturn := 0.0
	averageAnnualized := 0.0
	averageSharpe := 0.0
	averageTurnover := 0.0
	worstDrawdown := 0.0
	annualizedValues := make([]float64, 0, len(folds))
	reasons := make([]string, 0)
	for _, fold := range folds {
		metrics := fold.Validate
		if !finiteMetrics(fold.Train) || !finiteMetrics(metrics) {
			reasons = append(reasons, fold.Fold.ID+"指标包含非有限数值")
		}
		if metrics.TotalReturn > 0 {
			positive++
		}
		trades += metrics.Trades
		averageReturn += metrics.TotalReturn
		averageAnnualized += metrics.AnnualizedReturn
		averageSharpe += metrics.Sharpe
		averageTurnover += metrics.Turnover
		annualizedValues = append(annualizedValues, metrics.AnnualizedReturn)
		if metrics.MaxDrawdown < worstDrawdown {
			worstDrawdown = metrics.MaxDrawdown
		}
	}
	count := float64(len(folds))
	positiveRatio := float64(positive) / count
	averageReturn /= count
	averageAnnualized /= count
	averageSharpe /= count
	averageTurnover /= count
	variance := 0.0
	for _, value := range annualizedValues {
		variance += math.Pow(value-averageAnnualized, 2)
	}
	dispersion := math.Sqrt(variance / count)
	if trades < request.MinimumValidationTrades {
		reasons = append(reasons, fmt.Sprintf("滚动验证交易数 %d 少于门槛 %d", trades, request.MinimumValidationTrades))
	}
	if positiveRatio < request.MinimumPositiveFoldRatio {
		reasons = append(reasons, fmt.Sprintf("正收益窗口 %.0f%% 低于门槛 %.0f%%", positiveRatio*100, request.MinimumPositiveFoldRatio*100))
	}
	if math.Abs(worstDrawdown) > request.MaximumValidationDrawdown {
		reasons = append(reasons, fmt.Sprintf("最差验证回撤 %.2f%% 超过门槛 %.2f%%", math.Abs(worstDrawdown), request.MaximumValidationDrawdown))
	}
	score := averageAnnualized + averageSharpe*5 - math.Abs(worstDrawdown)*.6 - dispersion*.4 - averageTurnover*.01
	if len(reasons) > 0 {
		score -= 1000
	}
	return score, positiveRatio, trades, averageReturn, worstDrawdown, reasons
}

func continuousCandidateBetter(left, right ContinuousCandidateResult) bool {
	if left.Rejected != right.Rejected {
		return !left.Rejected
	}
	if math.Abs(left.Score-right.Score) > 1e-9 {
		return left.Score > right.Score
	}
	return left.Proposal.ID < right.Proposal.ID
}

func bestTradeProfitShare(result Result) float64 {
	best := 0.0
	positiveTotal := 0.0
	for _, trade := range result.Trades {
		if trade.NetProfit > best {
			best = trade.NetProfit
		}
		if trade.NetProfit > 0 {
			positiveTotal += trade.NetProfit
		}
	}
	if positiveTotal <= 0 {
		return 1
	}
	return best / positiveTotal
}

func continuousGate(result *ContinuousOptimizationResult) {
	result.Stage = ContinuousStageResearch
	if result.Selected == nil || result.Holdout == nil || result.Stress.DoubleCost == nil {
		result.GateReasons = append(result.GateReasons, "没有完整的入选、最终留出和压力测试结果")
		return
	}
	selected := result.Selected
	holdout := result.Holdout.Metrics
	stress := result.Stress.DoubleCost.Metrics
	finiteHoldout := finiteMetrics(holdout)
	finiteStress := finiteMetrics(stress)
	finiteConcentration := finiteValues(result.Stress.BestTradeProfitShare) && result.Stress.BestTradeProfitShare >= 0 && result.Stress.BestTradeProfitShare <= 1
	checks := []struct {
		failed bool
		reason string
	}{
		{len(selected.Folds) < 3, "滚动验证少于3折"},
		{selected.PositiveFoldRatio < result.Request.MinimumPositiveFoldRatio, fmt.Sprintf("正收益滚动窗口不足%.0f%%", result.Request.MinimumPositiveFoldRatio*100)},
		{selected.ValidationTrades < result.Request.MinimumValidationTrades, fmt.Sprintf("滚动验证交易少于%d笔", result.Request.MinimumValidationTrades)},
		{!finiteHoldout, "最终留出指标包含非有限数值"},
		{holdout.Trades < 10, "最终留出交易少于10笔"},
		{holdout.TotalReturn <= 0, "最终留出收益不为正"},
		{holdout.Sharpe < .5, "最终留出夏普低于0.5"},
		{math.Abs(holdout.MaxDrawdown) > 15, "最终留出回撤超过15%"},
		{!finiteStress, "双倍成本压力测试指标包含非有限数值"},
		{stress.TotalReturn <= 0, "双倍成本压力测试收益不为正"},
		{!finiteConcentration, "最佳单笔利润集中度无效"},
		{result.Stress.BestTradeProfitShare > .5, "最佳单笔占全部正收益超过50%"},
		{!result.Quality.Passed, "数据质量硬检查未通过"},
		{selected.NeighborhoodSize < 1, "入选参数缺少邻域稳定平台"},
		{selected.NeighborhoodSize > 0 && selected.NeighborhoodScore < selected.Score-5, "入选参数邻域表现显著弱于中心参数"},
	}
	for _, check := range checks {
		if check.failed {
			result.GateReasons = append(result.GateReasons, check.reason)
		}
	}
	if len(result.GateReasons) == 0 {
		result.Stage = ContinuousStageShadow
	}
}

func (optimizer *ContinuousOptimizer) Optimize(
	ctx context.Context,
	request ContinuousOptimizationRequest,
	agents []AgentResearchRun,
	progress func(OptimizationProgress),
) (ContinuousOptimizationResult, error) {
	if optimizer == nil || optimizer.engine == nil {
		return ContinuousOptimizationResult{}, fmt.Errorf("持续优化回测引擎未初始化")
	}
	if err := validateContinuousRequest(request); err != nil {
		return ContinuousOptimizationResult{}, err
	}
	result := ContinuousOptimizationResult{
		ID: "AUTO-" + optimizer.now().Format("20060102T150405"), GeneratedAt: optimizer.now(),
		Request: request, Agents: agents, Stage: ContinuousStageResearch,
		Warnings: []string{
			"子Agent只提出受控参数候选，Go回测引擎独立完成评分、留出检验和压力测试",
			"最终留出只在滚动候选锁定后运行，不参与候选排名或重新选参",
			"历史晋级仅表示可进入模拟观察，不代表稳定收益或未来收益保证",
		},
	}
	for proposalIndex, proposal := range request.Proposals {
		candidate := ContinuousCandidateResult{Proposal: proposal}
		for _, fold := range request.Folds {
			train, err := optimizer.engine.Run(ctx, periodRequest(request.BaseRequest, fold.Train, proposal.Parameters))
			if err != nil {
				if ctx.Err() != nil {
					return result, ctx.Err()
				}
				candidate.Rejected = true
				candidate.Reasons = append(candidate.Reasons, fmt.Sprintf("%s训练失败：%s", fold.ID, err))
				break
			}
			validate, err := optimizer.engine.Run(ctx, periodRequest(request.BaseRequest, fold.Validate, proposal.Parameters))
			if err != nil {
				if ctx.Err() != nil {
					return result, ctx.Err()
				}
				candidate.Rejected = true
				candidate.Reasons = append(candidate.Reasons, fmt.Sprintf("%s验证失败：%s", fold.ID, err))
				break
			}
			candidate.Folds = append(candidate.Folds, WalkForwardFoldResult{
				Fold: fold, Train: train.Metrics, Validate: validate.Metrics,
				TrainSources: train.DataSources, ValidateSources: validate.DataSources,
				TrainCoverage: train.DataCoverage, ValidateCoverage: validate.DataCoverage,
				Warnings: append(append([]string(nil), train.Warnings...), validate.Warnings...),
			})
		}
		if !candidate.Rejected {
			candidate.Score, candidate.PositiveFoldRatio, candidate.ValidationTrades,
				candidate.AverageValidation, candidate.WorstDrawdown, candidate.Reasons =
				continuousCandidateScore(request, candidate.Folds)
			candidate.Rejected = len(candidate.Reasons) > 0
		} else {
			candidate.Score = rejectedCandidateScore
		}
		result.Candidates = append(result.Candidates, candidate)
		if progress != nil {
			progress(OptimizationProgress{Phase: "walk-forward", Completed: proposalIndex + 1, Total: len(request.Proposals)})
		}
	}
	sort.SliceStable(result.Candidates, func(i, j int) bool {
		return continuousCandidateBetter(result.Candidates[i], result.Candidates[j])
	})
	enrichCandidateStability(result.Candidates, agents)
	for index := range result.Candidates {
		candidate := &result.Candidates[index]
		if !candidate.Rejected && candidate.NeighborhoodSize == 0 {
			candidate.Rejected = true
			candidate.Reasons = append(candidate.Reasons, "参数缺少通过滚动门禁的邻域稳定平台")
			candidate.Score -= 1000
		}
	}
	sort.SliceStable(result.Candidates, func(i, j int) bool {
		return continuousCandidateBetter(result.Candidates[i], result.Candidates[j])
	})
	for index := range result.Candidates {
		if !result.Candidates[index].Rejected {
			selected := result.Candidates[index]
			result.Selected = &selected
			break
		}
	}
	if result.Selected == nil {
		result.Quality = evaluateContinuousDataQuality(&result)
		result.GateReasons = append(result.GateReasons, "所有候选均未通过滚动验证门禁")
		return result, nil
	}
	if progress != nil {
		progress(OptimizationProgress{Phase: "holdout", Completed: 1, Total: 1})
	}
	holdoutRequest := periodRequest(request.BaseRequest, request.Holdout, result.Selected.Proposal.Parameters)
	holdout, err := optimizer.engine.Run(ctx, holdoutRequest)
	if err != nil {
		return result, fmt.Errorf("最终留出回测失败: %w", err)
	}
	result.Holdout = &holdout
	stressRequest := holdoutRequest
	stressRequest.CommissionRate *= 2
	stressRequest.MinimumCommission *= 2
	stressRequest.StampDutyRate *= 2
	stressRequest.TransferFeeRate *= 2
	stressRequest.SlippageBPS *= 2
	doubleCost, err := optimizer.engine.Run(ctx, stressRequest)
	if err != nil {
		return result, fmt.Errorf("双倍成本压力测试失败: %w", err)
	}
	result.Stress = StressResult{DoubleCost: &doubleCost, BestTradeProfitShare: bestTradeProfitShare(holdout)}
	result.Quality = evaluateContinuousDataQuality(&result)
	continuousGate(&result)
	return result, nil
}
