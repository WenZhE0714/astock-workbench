package backtest

import (
	"context"
	"fmt"
	"math"
	"time"
)

type PositionStudyWindow struct {
	ID         string `json:"id"`
	Normal     Result `json:"normal"`
	DoubleCost Result `json:"double_cost"`
}

type PositionStudySummary struct {
	AverageReturn float64  `json:"average_return_percent"`
	WorstDrawdown float64  `json:"worst_drawdown_percent"`
	PositiveRatio float64  `json:"positive_fold_ratio"`
	Trades        int      `json:"trades"`
	Fees          float64  `json:"fees"`
	Passed        bool     `json:"passed"`
	Reasons       []string `json:"reasons"`
}

type PositionStudyVariant struct {
	MaxPosition float64               `json:"max_position"`
	Windows     []PositionStudyWindow `json:"windows"`
	Normal      PositionStudySummary  `json:"normal_summary"`
	DoubleCost  PositionStudySummary  `json:"double_cost_summary"`
	Diagnostic  PositionStudyWindow   `json:"seen_period_diagnostic"`
}

type PositionStudyResult struct {
	Completed                bool                   `json:"completed"`
	SourceID                 string                 `json:"source_id"`
	CandidateID              string                 `json:"candidate_id"`
	GeneratedAt              time.Time              `json:"generated_at"`
	SourceBaselineReproduced bool                   `json:"source_baseline_reproduced"`
	BaselineDifferences      []string               `json:"baseline_differences"`
	Variants                 []PositionStudyVariant `json:"variants"`
	Warnings                 []string               `json:"warnings"`
	PromotionAllowed         bool                   `json:"promotion_allowed"`
}

func RunPositionStudy(ctx context.Context, engine Engine, source ContinuousOptimizationResult, candidateID string, caps []float64, progress func(string)) (PositionStudyResult, error) {
	result := PositionStudyResult{SourceID: source.ID, CandidateID: candidateID, GeneratedAt: time.Now(), BaselineDifferences: []string{}, Variants: []PositionStudyVariant{}, Warnings: []string{"原近期区间已被观察，仅作诊断，不是新的最终留出", "本研究不会自动晋级或修改影子账户"}}
	if engine == nil || len(source.Request.Folds) < 2 || len(caps) < 2 || len(caps) > 8 {
		return result, fmt.Errorf("仓位研究需要引擎、至少2个窗口及2至8组仓位")
	}
	var original *ContinuousCandidateResult
	for _, candidate := range source.Candidates {
		if candidate.Proposal.ID == candidateID {
			copy := candidate
			original = &copy
			break
		}
	}
	if original == nil {
		return result, fmt.Errorf("源实验中没有候选 %s", candidateID)
	}
	seen := map[float64]bool{}
	for _, cap := range caps {
		if !finiteValues(cap) || cap <= 0 || cap > 1 || seen[cap] {
			return result, fmt.Errorf("仓位必须唯一且位于0至1之间")
		}
		seen[cap] = true
	}
	if !seen[original.Proposal.Parameters.MaxPosition] {
		return result, fmt.Errorf("仓位列表必须包含原候选基线")
	}
	if !source.Request.BaseRequest.NoFutureData || source.Request.BaseRequest.Adjustment != AdjustmentNone {
		return result, fmt.Errorf("源研究必须固定无未来数据与不复权口径")
	}
	if !source.Request.BaseRequest.PointInTimePool {
		result.Warnings = append(result.Warnings, "股票池为静态池，仍存在幸存者偏差，不能作为正式晋级证据")
	}
	for _, cap := range caps {
		variant := PositionStudyVariant{MaxPosition: cap, Windows: []PositionStudyWindow{}}
		for _, fold := range source.Request.Folds {
			if progress != nil {
				progress(fmt.Sprintf("仓位%.0f%%：%s验证与双倍成本", cap*100, fold.ID))
			}
			window, err := runPositionStudyWindow(ctx, engine, source.Request.BaseRequest, original.Proposal.Parameters, cap, fold.ID, fold.Validate)
			if err != nil {
				return result, err
			}
			variant.Windows = append(variant.Windows, window)
			if cap == original.Proposal.Parameters.MaxPosition {
				found := false
				for _, old := range original.Folds {
					if old.Fold.ID != fold.ID {
						continue
					}
					found = true
					if math.Abs(old.Validate.TotalReturn-window.Normal.Metrics.TotalReturn) > .01 || math.Abs(old.Validate.MaxDrawdown-window.Normal.Metrics.MaxDrawdown) > .01 || old.Validate.Trades != window.Normal.Metrics.Trades {
						result.BaselineDifferences = append(result.BaselineDifferences, fmt.Sprintf("%s 原收益%.4f%%/回撤%.4f%%/%d笔，复现%.4f%%/%.4f%%/%d笔", fold.ID, old.Validate.TotalReturn, old.Validate.MaxDrawdown, old.Validate.Trades, window.Normal.Metrics.TotalReturn, window.Normal.Metrics.MaxDrawdown, window.Normal.Metrics.Trades))
					}
				}
				if !found {
					result.BaselineDifferences = append(result.BaselineDifferences, fold.ID+"缺少原基线记录")
				}
			}
		}
		variant.Normal = summarizePositionWindows(variant.Windows, source.Request, false)
		variant.DoubleCost = summarizePositionWindows(variant.Windows, source.Request, true)
		result.Variants = append(result.Variants, variant)
	}
	result.SourceBaselineReproduced = len(result.BaselineDifferences) == 0
	// Every cap is fixed before this already-seen period is inspected. It is
	// displayed separately and cannot turn a rejected validation into a pass.
	for i := range result.Variants {
		variant := &result.Variants[i]
		if progress != nil {
			progress(fmt.Sprintf("仓位%.0f%%：已见近期区间诊断", variant.MaxPosition*100))
		}
		window, err := runPositionStudyWindow(ctx, engine, source.Request.BaseRequest, original.Proposal.Parameters, variant.MaxPosition, "seen-period", source.Request.Holdout)
		if err != nil {
			return result, err
		}
		variant.Diagnostic = window
	}
	result.Completed = true
	return result, nil
}

func runPositionStudyWindow(ctx context.Context, engine Engine, base Request, parameters TechnicalParameters, cap float64, id string, period Period) (PositionStudyWindow, error) {
	window := PositionStudyWindow{ID: id}
	request := base
	request.Technical, request.Start, request.End = parameters, period.Start, period.End
	request.Technical.MaxPosition = cap
	if err := ctx.Err(); err != nil {
		return window, err
	}
	var err error
	window.Normal, err = engine.Run(ctx, request)
	if err != nil {
		return window, fmt.Errorf("%.0f%% %s: %w", cap*100, id, err)
	}
	request.CommissionRate *= 2
	request.MinimumCommission *= 2
	request.StampDutyRate *= 2
	request.TransferFeeRate *= 2
	request.SlippageBPS *= 2
	if err := ctx.Err(); err != nil {
		return window, err
	}
	window.DoubleCost, err = engine.Run(ctx, request)
	if err != nil {
		return window, fmt.Errorf("%.0f%% %s双倍成本: %w", cap*100, id, err)
	}
	return window, nil
}

func summarizePositionWindows(windows []PositionStudyWindow, request ContinuousOptimizationRequest, stress bool) PositionStudySummary {
	summary := PositionStudySummary{Reasons: []string{}}
	positive := 0
	for _, window := range windows {
		run := window.Normal
		if stress {
			run = window.DoubleCost
		}
		m := run.Metrics
		if !finiteMetrics(m) {
			summary.Reasons = append(summary.Reasons, window.ID+"指标非有限")
		}
		if m.TotalReturn > 0 {
			positive++
		}
		summary.AverageReturn += m.TotalReturn
		summary.WorstDrawdown = math.Max(summary.WorstDrawdown, math.Abs(m.MaxDrawdown))
		summary.Trades += m.Trades
		summary.Fees += m.TotalFees
		if !m.BenchmarkAvailable {
			summary.Reasons = append(summary.Reasons, window.ID+"缺少基准")
		}
		if hasMissingEntryBenchmark(run) {
			summary.Reasons = append(summary.Reasons, window.ID+"开仓过滤存在基准数据缺口")
		}
		for _, symbol := range request.BaseRequest.Tickers {
			if run.DataCoverage[symbol].CoverageRatio < .8 {
				summary.Reasons = append(summary.Reasons, window.ID+" "+symbol+"数据覆盖不足80%")
			}
		}
	}
	if len(windows) > 0 {
		summary.AverageReturn /= float64(len(windows))
		summary.PositiveRatio = float64(positive) / float64(len(windows))
	}
	if summary.Trades < request.MinimumValidationTrades {
		summary.Reasons = append(summary.Reasons, "交易数量未达原门槛")
	}
	if summary.PositiveRatio < request.MinimumPositiveFoldRatio {
		summary.Reasons = append(summary.Reasons, "正收益窗口比例未达原门槛")
	}
	if summary.WorstDrawdown > request.MaximumValidationDrawdown {
		summary.Reasons = append(summary.Reasons, "最差回撤超过原门槛")
	}
	summary.Passed = len(summary.Reasons) == 0
	return summary
}
