package backtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

const VolumeStudyMinimum = 1.5

type VolumeStudyVariant struct {
	Minimum float64 `json:"entry_volume_floor"`
	PositionStudyVariant
}

type VolumeStudyEntry struct {
	Entry         Fill    `json:"entry"`
	Exit          *Fill   `json:"exit,omitempty"`
	Profit        float64 `json:"profit"`
	ReturnPercent float64 `json:"return_percent"`
	Closed        bool    `json:"closed"`
}

type VolumeOpportunity struct {
	Symbol      string            `json:"symbol"`
	Name        string            `json:"name"`
	SignalDate  string            `json:"signal_date"`
	Status      string            `json:"status"`
	Block       *EntryVolumeBlock `json:"block,omitempty"`
	Baseline    *VolumeStudyEntry `json:"baseline,omitempty"`
	Filtered    *VolumeStudyEntry `json:"filtered,omitempty"`
	ProfitDelta float64           `json:"profit_delta"`
}

type VolumeStudyImpact struct {
	BlockedSignals        int                 `json:"blocked_signals"`
	BaselineEntries       int                 `json:"baseline_entries"`
	FilteredEntries       int                 `json:"filtered_entries"`
	RetainedEntries       int                 `json:"retained_entries"`
	NewEntries            int                 `json:"new_entries"`
	OtherRemovedEntries   int                 `json:"other_removed_entries"`
	AvoidedLosses         int                 `json:"avoided_losses"`
	AvoidedLossAmount     float64             `json:"avoided_loss_amount"`
	MissedWins            int                 `json:"missed_wins"`
	MissedProfitAmount    float64             `json:"missed_profit_amount"`
	BaselineBigWins       int                 `json:"baseline_big_wins"`
	RetainedBigWinEntries int                 `json:"retained_big_win_entries"`
	BigWinProfitDelta     float64             `json:"big_win_profit_delta"`
	RetainedWinnerDelta   float64             `json:"retained_winner_profit_delta"`
	SavedFees             float64             `json:"saved_fees"`
	BaselineExposure      float64             `json:"baseline_average_exposure_percent"`
	FilteredExposure      float64             `json:"filtered_average_exposure_percent"`
	EquityDifference      float64             `json:"equity_difference"`
	ReconciliationError   float64             `json:"reconciliation_error"`
	Opportunities         []VolumeOpportunity `json:"opportunities"`
}

type VolumeStudyComparison struct {
	ID         string            `json:"id"`
	Normal     VolumeStudyImpact `json:"normal"`
	DoubleCost VolumeStudyImpact `json:"double_cost"`
}

type VolumeStudyResult struct {
	Completed           bool                    `json:"completed"`
	SourceID            string                  `json:"source_id"`
	CandidateID         string                  `json:"candidate_id"`
	MaxPosition         float64                 `json:"max_position"`
	GeneratedAt         time.Time               `json:"generated_at"`
	BaselineReproduced  bool                    `json:"baseline_reproduced"`
	BaselineDifferences []string                `json:"baseline_differences"`
	Variants            []VolumeStudyVariant    `json:"variants"`
	Comparisons         []VolumeStudyComparison `json:"comparisons"`
	PromotionAllowed    bool                    `json:"promotion_allowed"`
	Warnings            []string                `json:"warnings"`
}

func RunVolumeStudy(ctx context.Context, engine Engine, source ContinuousOptimizationResult, candidateID string, reference PositionStudyVariant, progress func(string)) (VolumeStudyResult, error) {
	result := VolumeStudyResult{SourceID: source.ID, CandidateID: candidateID, MaxPosition: reference.MaxPosition, GeneratedAt: time.Now(), BaselineDifferences: []string{}, Variants: []VolumeStudyVariant{}, Comparisons: []VolumeStudyComparison{}, Warnings: []string{
		"1.5倍门槛来自已见分组诊断，存在研究选择偏差；本轮不扫描相邻阈值",
		"近期区间是已见诊断数据，不是新的最终留出",
		"只在原信号条件上追加实际量比下限，不改变形态量能上下界或技术参数",
		"过滤后的组合重新计算现金、股数及入场路径，不能以原交易分组盈亏代替",
		"被拒绝信号按股票和原信号日期匹配；避开亏损、错过盈利已包含在净值差中",
		"未平仓采用期末盯市盈亏，各验证窗口独立重置本金，金额合计不是连续账户收益",
		"本研究不自动晋级、不修改影子账户；仍需未见样本或前瞻观察",
	}}
	base := source.Request.BaseRequest
	if engine == nil || len(source.Request.Folds) < 2 || len(reference.Windows) != len(source.Request.Folds) || !base.NoFutureData || base.Adjustment != AdjustmentNone || base.EntryMarketFilter != "" || base.EntryConfirmation != "" || base.ExitProtection != "" || base.EntryVolumeFloor != 0 || !finiteValues(reference.MaxPosition) || reference.MaxPosition <= 0 || reference.MaxPosition > 1 {
		return result, fmt.Errorf("量比对照需要完整且未附加过滤、确认或退出保护的冻结基线")
	}
	var parameters TechnicalParameters
	for _, candidate := range source.Candidates {
		if candidate.Proposal.ID == candidateID {
			parameters = candidate.Proposal.Parameters
			break
		}
	}
	if parameters.EffectiveEntryMode() != EntryModeReclaim {
		return result, fmt.Errorf("量比对照需要既有趋势重返候选")
	}
	for _, minimum := range []float64{0, VolumeStudyMinimum} {
		variant := VolumeStudyVariant{Minimum: minimum, PositionStudyVariant: PositionStudyVariant{MaxPosition: reference.MaxPosition, Windows: []PositionStudyWindow{}}}
		request := base
		request.EntryVolumeFloor = minimum
		label := "原入场条件"
		if minimum > 0 {
			label = "追加实际量比至少1.5倍"
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
			if minimum == 0 {
				matched := false
				for _, prior := range reference.Windows {
					if prior.ID == fold.ID {
						matched = sameConfirmationBaseline(prior.Normal, window.Normal) && sameConfirmationBaseline(prior.DoubleCost, window.DoubleCost)
						break
					}
				}
				if !matched {
					result.BaselineDifferences = append(result.BaselineDifferences, fold.ID+"基线成交或净值未复现")
				}
			}
		}
		variant.Normal = summarizePositionWindows(variant.Windows, source.Request, false)
		variant.DoubleCost = summarizePositionWindows(variant.Windows, source.Request, true)
		if progress != nil {
			progress(label + "：已见近期区间及双倍成本")
		}
		window, err := runPositionStudyWindow(ctx, engine, request, parameters, reference.MaxPosition, "seen-period", source.Request.Holdout)
		if err != nil {
			return result, err
		}
		variant.Diagnostic = window
		result.Variants = append(result.Variants, variant)
		if minimum == 0 {
			if !sameConfirmationBaseline(reference.Diagnostic.Normal, window.Normal) || !sameConfirmationBaseline(reference.Diagnostic.DoubleCost, window.DoubleCost) {
				result.BaselineDifferences = append(result.BaselineDifferences, "近期诊断基线未复现")
			}
			result.BaselineReproduced = len(result.BaselineDifferences) == 0
			if !result.BaselineReproduced {
				return result, fmt.Errorf("基线未完整复现，停止量比对照")
			}
		}
	}
	baseline := append(append([]PositionStudyWindow{}, result.Variants[0].Windows...), result.Variants[0].Diagnostic)
	filtered := append(append([]PositionStudyWindow{}, result.Variants[1].Windows...), result.Variants[1].Diagnostic)
	for i, window := range baseline {
		item := VolumeStudyComparison{ID: window.ID, Normal: volumeStudyImpact(window.Normal, filtered[i].Normal, parameters.TakeProfit*100), DoubleCost: volumeStudyImpact(window.DoubleCost, filtered[i].DoubleCost, parameters.TakeProfit*100)}
		result.Comparisons = append(result.Comparisons, item)
		if math.Abs(item.Normal.ReconciliationError) > .01 || math.Abs(item.DoubleCost.ReconciliationError) > .01 {
			return result, fmt.Errorf("%s 量比机会归因与净值差未对平", window.ID)
		}
	}
	if !base.PointInTimePool {
		result.Warnings = append(result.Warnings, "静态股票池存在幸存者偏差，不能作为正式晋级证据")
	}
	result.Completed = true
	return result, nil
}

func volumeStudyImpact(baseline, filtered Result, bigWinThreshold float64) VolumeStudyImpact {
	result := VolumeStudyImpact{BlockedSignals: len(filtered.VolumeBlockedEntries), EquityDifference: filtered.Metrics.FinalEquity - baseline.Metrics.FinalEquity, SavedFees: baseline.Metrics.TotalFees - filtered.Metrics.TotalFees, Opportunities: []VolumeOpportunity{}}
	rows := map[string]*VolumeOpportunity{}
	for i, run := range []Result{baseline, filtered} {
		assign := func(symbol, name, date string, entry VolumeStudyEntry) {
			key := symbol + "|" + date
			if rows[key] == nil {
				rows[key] = &VolumeOpportunity{Symbol: symbol, Name: name, SignalDate: date}
			}
			if i == 0 {
				rows[key].Baseline = &entry
				result.BaselineEntries++
			} else {
				rows[key].Filtered = &entry
				result.FilteredEntries++
			}
		}
		for _, trade := range run.Trades {
			exit := trade.Exit
			assign(trade.Symbol, trade.Name, trade.EntrySignal.Date, VolumeStudyEntry{Entry: trade.Entry, Exit: &exit, Profit: trade.NetProfit, ReturnPercent: trade.ReturnPercent, Closed: true})
		}
		for _, position := range run.OpenPositions {
			assign(position.Symbol, position.Name, position.EntrySignal.Date, VolumeStudyEntry{Entry: position.Entry, Profit: position.UnrealizedProfit, ReturnPercent: position.ReturnPercent})
		}
	}
	for _, block := range filtered.VolumeBlockedEntries {
		if row := rows[block.Symbol+"|"+block.SignalDate]; row != nil {
			copy := block
			row.Block = &copy
		}
	}
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	total := 0.0
	for _, key := range keys {
		row := rows[key]
		a, b := row.Baseline, row.Filtered
		if a != nil {
			row.ProfitDelta -= a.Profit
		}
		if b != nil {
			row.ProfitDelta += b.Profit
		}
		switch {
		case a == nil:
			row.Status = "new-entry"
			result.NewEntries++
		case b != nil:
			row.Status = "retained"
			result.RetainedEntries++
			if a.Closed && a.Profit > 0 {
				result.RetainedWinnerDelta += row.ProfitDelta
			}
		case row.Block != nil:
			row.Status = "volume-rejected"
			if a.Closed && a.Profit < 0 {
				result.AvoidedLosses++
				result.AvoidedLossAmount -= a.Profit
			} else if a.Closed && a.Profit > 0 {
				result.MissedWins++
				result.MissedProfitAmount += a.Profit
			}
		default:
			row.Status = "other-removed"
			result.OtherRemovedEntries++
		}
		if a != nil && a.Closed && a.ReturnPercent >= bigWinThreshold {
			result.BaselineBigWins++
			result.BigWinProfitDelta += row.ProfitDelta
			if b != nil {
				result.RetainedBigWinEntries++
			}
		}
		total += row.ProfitDelta
		result.Opportunities = append(result.Opportunities, *row)
	}
	exposure := func(run Result) float64 {
		total := 0.0
		for _, point := range run.Equity {
			if point.Equity > 0 {
				total += point.Positions / point.Equity * 100
			}
		}
		if len(run.Equity) == 0 {
			return 0
		}
		return total / float64(len(run.Equity))
	}
	result.BaselineExposure, result.FilteredExposure = exposure(baseline), exposure(filtered)
	result.ReconciliationError = result.EquityDifference - total
	return result
}
