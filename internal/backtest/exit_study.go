package backtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

type ExitStudyVariant struct {
	Protection string `json:"exit_protection"`
	PositionStudyVariant
}

type ExitStudyEntry struct {
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name"`
	SignalDate    string  `json:"signal_date"`
	EntryDate     string  `json:"entry_date"`
	ExitDate      string  `json:"exit_date,omitempty"`
	ExitReason    string  `json:"exit_reason,omitempty"`
	Profit        float64 `json:"profit"`
	ReturnPercent float64 `json:"return_percent"`
	Closed        bool    `json:"closed"`
}

type ExitStudyOpportunity struct {
	Baseline    *ExitStudyEntry `json:"baseline,omitempty"`
	Protected   *ExitStudyEntry `json:"protected,omitempty"`
	ProfitDelta float64         `json:"profit_delta"`
}

type ExitStudyImpact struct {
	ProtectionExits       int                    `json:"protection_exits"`
	MatchedEntries        int                    `json:"matched_entries"`
	NewEntries            int                    `json:"new_entries"`
	RemovedEntries        int                    `json:"removed_entries"`
	BaselineBigWins       int                    `json:"baseline_big_wins"`
	RetainedBigWinEntries int                    `json:"retained_big_win_entries"`
	BigWinProfitDelta     float64                `json:"big_win_profit_delta"`
	BaselineLossDelta     float64                `json:"baseline_loss_profit_delta"`
	BaselineWinnerDelta   float64                `json:"baseline_winner_profit_delta"`
	EquityDifference      float64                `json:"equity_difference"`
	ReconciliationError   float64                `json:"reconciliation_error"`
	Opportunities         []ExitStudyOpportunity `json:"opportunities"`
}

type ExitStudyWindowReview struct {
	ID                   string               `json:"id"`
	NormalDiagnostic     ExitEfficiencyReview `json:"normal_diagnostic"`
	DoubleCostDiagnostic ExitEfficiencyReview `json:"double_cost_diagnostic"`
	Normal               ExitStudyImpact      `json:"normal"`
	DoubleCost           ExitStudyImpact      `json:"double_cost"`
}

type ExitStudyResult struct {
	Completed           bool                    `json:"completed"`
	SourceID            string                  `json:"source_id"`
	CandidateID         string                  `json:"candidate_id"`
	MaxPosition         float64                 `json:"max_position"`
	GeneratedAt         time.Time               `json:"generated_at"`
	BaselineReproduced  bool                    `json:"baseline_reproduced"`
	BaselineDifferences []string                `json:"baseline_differences"`
	Variants            []ExitStudyVariant      `json:"variants"`
	Windows             []ExitStudyWindowReview `json:"windows"`
	PromotionAllowed    bool                    `json:"promotion_allowed"`
	Warnings            []string                `json:"warnings"`
}

func RunExitStudy(ctx context.Context, engine Engine, provider DailyBarProvider, source ContinuousOptimizationResult, candidateID string, reference PositionStudyVariant, progress func(string)) (ExitStudyResult, error) {
	result := ExitStudyResult{SourceID: source.ID, CandidateID: candidateID, MaxPosition: reference.MaxPosition, GeneratedAt: time.Now(), BaselineDifferences: []string{}, Variants: []ExitStudyVariant{}, Windows: []ExitStudyWindowReview{}, Warnings: []string{
		"已见近期区间不是新的最终留出；本轮规则来自已见诊断，存在研究选择偏差",
		"逐笔退出诊断固定原入场及数量，不包含重新入场和资金路径变化，不能当作组合收益",
		"收盘浮盈已估算双边费用和滑点，但不代表可以在该收盘价卖出；隔夜跳空仍可能亏损",
		"组合归因包含期末未平仓盯市盈亏；各窗口独立起始，跨窗口金额不是连续账户收益",
		"不自动晋级或修改影子账户；不复权日K、固定历史费率和期末强平限制仍然存在",
	}}
	base := source.Request.BaseRequest
	if engine == nil || provider == nil || len(source.Request.Folds) < 2 || len(reference.Windows) != len(source.Request.Folds) || !base.NoFutureData || base.Adjustment != AdjustmentNone || base.EntryMarketFilter != "" || base.EntryConfirmation != "" || base.ExitProtection != "" || !finiteValues(reference.MaxPosition) || reference.MaxPosition <= 0 || reference.MaxPosition > 1 {
		return result, fmt.Errorf("退出对照需要完整、未过滤且未附加入场确认或退出保护的冻结基线")
	}
	var parameters TechnicalParameters
	for _, candidate := range source.Candidates {
		if candidate.Proposal.ID == candidateID {
			parameters = candidate.Proposal.Parameters
			break
		}
	}
	if parameters.EffectiveEntryMode() != EntryModeReclaim {
		return result, fmt.Errorf("退出对照需要原趋势重返候选")
	}
	for _, protection := range []string{"", ExitProtectHalfProfit} {
		variant := ExitStudyVariant{Protection: protection, PositionStudyVariant: PositionStudyVariant{MaxPosition: reference.MaxPosition, Windows: []PositionStudyWindow{}}}
		request := base
		request.ExitProtection = protection
		label := "原始退出"
		if protection != "" {
			label = "收盘利润回吐保护"
		}
		for _, fold := range source.Request.Folds {
			if progress != nil {
				progress(label + "：" + fold.ID + "及双倍成本")
			}
			window, err := runPositionStudyWindow(ctx, engine, request, parameters, reference.MaxPosition, fold.ID, fold.Validate)
			if err != nil {
				return result, err
			}
			variant.Windows = append(variant.Windows, window)
			if protection == "" {
				matched := false
				for _, prior := range reference.Windows {
					if prior.ID == fold.ID {
						matched = sameConfirmationBaseline(prior.Normal, window.Normal) && sameConfirmationBaseline(prior.DoubleCost, window.DoubleCost)
						break
					}
				}
				if !matched {
					result.BaselineDifferences = append(result.BaselineDifferences, fold.ID+"成交或净值未复现")
				}
			}
		}
		variant.Normal, variant.DoubleCost = summarizePositionWindows(variant.Windows, source.Request, false), summarizePositionWindows(variant.Windows, source.Request, true)
		if progress != nil {
			progress(label + "：已见近期区间及双倍成本")
		}
		window, err := runPositionStudyWindow(ctx, engine, request, parameters, reference.MaxPosition, "seen-period", source.Request.Holdout)
		if err != nil {
			return result, err
		}
		variant.Diagnostic = window
		result.Variants = append(result.Variants, variant)
		if protection == "" {
			if !sameConfirmationBaseline(reference.Diagnostic.Normal, window.Normal) || !sameConfirmationBaseline(reference.Diagnostic.DoubleCost, window.DoubleCost) {
				result.BaselineDifferences = append(result.BaselineDifferences, "近期诊断基线未复现")
			}
			result.BaselineReproduced = len(result.BaselineDifferences) == 0
			if !result.BaselineReproduced {
				return result, fmt.Errorf("基线未完整复现，停止退出对照")
			}
		}
	}
	baseline := append(append([]PositionStudyWindow{}, result.Variants[0].Windows...), result.Variants[0].Diagnostic)
	protected := append(append([]PositionStudyWindow{}, result.Variants[1].Windows...), result.Variants[1].Diagnostic)
	for i, window := range baseline {
		if progress != nil {
			progress(window.ID + "：固定入场的退出效率诊断")
		}
		normal, err := reviewExitEfficiency(ctx, provider, window.Normal)
		if err != nil {
			return result, err
		}
		stress, err := reviewExitEfficiency(ctx, provider, window.DoubleCost)
		if err != nil {
			return result, err
		}
		item := ExitStudyWindowReview{ID: window.ID, NormalDiagnostic: normal, DoubleCostDiagnostic: stress, Normal: exitStudyImpact(window.Normal, protected[i].Normal, parameters.TakeProfit*100), DoubleCost: exitStudyImpact(window.DoubleCost, protected[i].DoubleCost, parameters.TakeProfit*100)}
		result.Windows = append(result.Windows, item)
		if math.Abs(item.Normal.ReconciliationError) > .01 || math.Abs(item.DoubleCost.ReconciliationError) > .01 {
			return result, fmt.Errorf("%s 退出归因与组合净值未对平", window.ID)
		}
	}
	if !base.PointInTimePool {
		result.Warnings = append(result.Warnings, "静态股票池存在幸存者偏差，不能作为正式晋级证据")
	}
	result.Completed = true
	return result, nil
}

func exitStudyImpact(baseline, protected Result, bigWinThreshold float64) ExitStudyImpact {
	result := ExitStudyImpact{EquityDifference: protected.Metrics.FinalEquity - baseline.Metrics.FinalEquity, Opportunities: []ExitStudyOpportunity{}}
	entries := func(run Result) map[string]ExitStudyEntry {
		items := map[string]ExitStudyEntry{}
		for _, trade := range run.Trades {
			items[trade.Symbol+"|"+trade.EntrySignal.Date] = ExitStudyEntry{Symbol: trade.Symbol, Name: trade.Name, SignalDate: trade.EntrySignal.Date, EntryDate: trade.Entry.Date, ExitDate: trade.Exit.Date, ExitReason: trade.ExitReason, Profit: trade.NetProfit, ReturnPercent: trade.ReturnPercent, Closed: true}
		}
		for _, position := range run.OpenPositions {
			items[position.Symbol+"|"+position.EntrySignal.Date] = ExitStudyEntry{Symbol: position.Symbol, Name: position.Name, SignalDate: position.EntrySignal.Date, EntryDate: position.Entry.Date, Profit: position.UnrealizedProfit, ReturnPercent: position.ReturnPercent}
		}
		return items
	}
	for _, trade := range protected.Trades {
		if trade.ExitSignal.ProfitProtection != nil {
			result.ProtectionExits++
		}
	}
	a, b := entries(baseline), entries(protected)
	keys := make([]string, 0, len(a)+len(b))
	for key := range a {
		keys = append(keys, key)
	}
	for key := range b {
		if _, ok := a[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	totalDelta := 0.0
	for _, key := range keys {
		row := ExitStudyOpportunity{}
		left, hasLeft := a[key]
		right, hasRight := b[key]
		if hasLeft {
			row.Baseline = &left
			row.ProfitDelta -= left.Profit
		}
		if hasRight {
			row.Protected = &right
			row.ProfitDelta += right.Profit
		}
		switch {
		case !hasLeft:
			result.NewEntries++
		case !hasRight:
			result.RemovedEntries++
		default:
			result.MatchedEntries++
		}
		if hasLeft && left.Closed {
			if left.Profit > 0 {
				result.BaselineWinnerDelta += row.ProfitDelta
			} else if left.Profit < 0 {
				result.BaselineLossDelta += row.ProfitDelta
			}
			if left.ReturnPercent >= bigWinThreshold {
				result.BaselineBigWins++
				result.BigWinProfitDelta += row.ProfitDelta
				if hasRight {
					result.RetainedBigWinEntries++
				}
			}
		}
		totalDelta += row.ProfitDelta
		result.Opportunities = append(result.Opportunities, row)
	}
	result.ReconciliationError = result.EquityDifference - totalDelta
	return result
}
