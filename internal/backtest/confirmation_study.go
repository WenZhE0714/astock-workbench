package backtest

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"slices"
	"sort"
	"time"
)

type ConfirmationStudyVariant struct {
	Confirmation string `json:"entry_confirmation"`
	PositionStudyVariant
}

// Open positions use marked PnL; only closed positions represent realized gains.
type ConfirmationEntryOutcome struct {
	Entry         Fill    `json:"entry"`
	Exit          *Fill   `json:"exit,omitempty"`
	Profit        float64 `json:"profit"`
	ReturnPercent float64 `json:"return_percent"`
	Closed        bool    `json:"closed"`
}

type ConfirmationOpportunity struct {
	Symbol       string                    `json:"symbol"`
	Name         string                    `json:"name"`
	SignalDate   string                    `json:"signal_date"`
	Status       string                    `json:"status"`
	Confirmation *EntryConfirmationEvent   `json:"confirmation,omitempty"`
	Baseline     *ConfirmationEntryOutcome `json:"baseline,omitempty"`
	Confirmed    *ConfirmationEntryOutcome `json:"confirmed,omitempty"`
	ProfitDelta  float64                   `json:"profit_delta"`
}

type ConfirmationImpact struct {
	ConfirmedSignals      int                       `json:"confirmed_signals"`
	RejectedSignals       int                       `json:"rejected_signals"`
	UnresolvedSignals     int                       `json:"unresolved_signals"`
	AvoidedLosses         int                       `json:"avoided_losses"`
	AvoidedLossAmount     float64                   `json:"avoided_loss_amount"`
	MissedWins            int                       `json:"missed_wins"`
	MissedProfitAmount    float64                   `json:"missed_profit_amount"`
	MatchedEntries        int                       `json:"matched_entries"`
	OtherRemovedEntries   int                       `json:"other_removed_entries"`
	NewEntries            int                       `json:"new_entries"`
	ReducedMatchedWins    int                       `json:"reduced_matched_wins"`
	MatchedWinProfitDelta float64                   `json:"matched_win_profit_delta"`
	BigWinThreshold       float64                   `json:"big_win_threshold_percent"`
	BaselineBigWins       int                       `json:"baseline_big_wins"`
	RetainedBigWinEntries int                       `json:"retained_big_win_entries"`
	BigWinProfitDelta     float64                   `json:"big_win_profit_delta"`
	BaselineExposure      float64                   `json:"baseline_average_exposure_percent"`
	ConfirmedExposure     float64                   `json:"confirmed_average_exposure_percent"`
	EquityDifference      float64                   `json:"equity_difference"`
	AttributedProfitDelta float64                   `json:"attributed_profit_delta"`
	ReconciliationError   float64                   `json:"reconciliation_error"`
	Opportunities         []ConfirmationOpportunity `json:"opportunities"`
}

type ConfirmationStudyComparison struct {
	ID         string             `json:"id"`
	Normal     ConfirmationImpact `json:"normal"`
	DoubleCost ConfirmationImpact `json:"double_cost"`
}

type ConfirmationStudyResult struct {
	Completed           bool                          `json:"completed"`
	SourceID            string                        `json:"source_id"`
	CandidateID         string                        `json:"candidate_id"`
	MaxPosition         float64                       `json:"max_position"`
	GeneratedAt         time.Time                     `json:"generated_at"`
	BaselineReproduced  bool                          `json:"baseline_reproduced"`
	BaselineDifferences []string                      `json:"baseline_differences"`
	Variants            []ConfirmationStudyVariant    `json:"variants"`
	Comparisons         []ConfirmationStudyComparison `json:"comparisons"`
	PromotionAllowed    bool                          `json:"promotion_allowed"`
	Warnings            []string                      `json:"warnings"`
}

func RunConfirmationStudy(ctx context.Context, engine Engine, source ContinuousOptimizationResult, candidateID string, reference PositionStudyVariant, progress func(string)) (ConfirmationStudyResult, error) {
	result := ConfirmationStudyResult{SourceID: source.ID, CandidateID: candidateID, MaxPosition: reference.MaxPosition, GeneratedAt: time.Now(), BaselineDifferences: []string{}, Variants: []ConfirmationStudyVariant{}, Comparisons: []ConfirmationStudyComparison{}, Warnings: []string{
		"近期区间为已见数据诊断，不是新的最终留出；确认规则来自已见亏损诊断，也存在研究选择偏差",
		"机会以股票和原始信号日期匹配；未成交归因需区分确认拒绝与持仓路径变化",
		"避开亏损、错过盈利和保留赢家的盈亏变化已包含在净值差中，不能重复相加",
		"未平仓使用期末盯市盈亏；不是可成交的已实现收益；仓位数量随资金路径变化",
		"大赢家定义固定为基线已平仓净收益率达到原止盈比例；不据此筛选交易",
		"本研究不自动晋级或修改影子账户",
	}}
	base := source.Request.BaseRequest
	if engine == nil || len(source.Request.Folds) < 2 || len(reference.Windows) != len(source.Request.Folds) || !base.NoFutureData || base.Adjustment != AdjustmentNone || base.EntryMarketFilter != "" || base.EntryConfirmation != "" || !finiteValues(reference.MaxPosition) || reference.MaxPosition <= 0 || reference.MaxPosition > 1 {
		return result, fmt.Errorf("确认对照需要完整且未过滤、未确认的固定研究基线")
	}
	var parameters TechnicalParameters
	for _, candidate := range source.Candidates {
		if candidate.Proposal.ID == candidateID {
			parameters = candidate.Proposal.Parameters
			break
		}
	}
	if parameters.EffectiveEntryMode() != EntryModeReclaim {
		return result, fmt.Errorf("确认对照需要既有趋势重返候选")
	}
	for _, confirmation := range []string{"", EntryConfirmReclaimTwoClose} {
		variant := ConfirmationStudyVariant{Confirmation: confirmation, PositionStudyVariant: PositionStudyVariant{MaxPosition: reference.MaxPosition, Windows: []PositionStudyWindow{}}}
		request := base
		request.EntryConfirmation = confirmation
		label := "基线"
		if confirmation != "" {
			label = "双收盘确认"
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
			if confirmation == "" {
				matched := false
				for _, prior := range reference.Windows {
					if prior.ID == fold.ID {
						matched = sameConfirmationBaseline(prior.Normal, window.Normal) && sameConfirmationBaseline(prior.DoubleCost, window.DoubleCost)
						break
					}
				}
				if !matched {
					result.BaselineDifferences = append(result.BaselineDifferences, fold.ID+"基线成交、净值或成本压力未复现")
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
		if confirmation == "" {
			if !sameConfirmationBaseline(reference.Diagnostic.Normal, window.Normal) || !sameConfirmationBaseline(reference.Diagnostic.DoubleCost, window.DoubleCost) {
				result.BaselineDifferences = append(result.BaselineDifferences, "近期诊断基线未复现")
			}
			result.BaselineReproduced = len(result.BaselineDifferences) == 0
			if !result.BaselineReproduced {
				return result, fmt.Errorf("基线未完整复现，停止确认对照")
			}
		}
	}
	baseline := append(append([]PositionStudyWindow{}, result.Variants[0].Windows...), result.Variants[0].Diagnostic)
	confirmed := append(append([]PositionStudyWindow{}, result.Variants[1].Windows...), result.Variants[1].Diagnostic)
	for i, window := range baseline {
		comparison := ConfirmationStudyComparison{ID: window.ID, Normal: confirmationImpact(window.Normal, confirmed[i].Normal, parameters.TakeProfit*100), DoubleCost: confirmationImpact(window.DoubleCost, confirmed[i].DoubleCost, parameters.TakeProfit*100)}
		result.Comparisons = append(result.Comparisons, comparison)
		if math.Abs(comparison.Normal.ReconciliationError) > .01 || math.Abs(comparison.DoubleCost.ReconciliationError) > .01 {
			return result, fmt.Errorf("%s 逐笔盈亏与净值差未对平", window.ID)
		}
	}
	if !base.PointInTimePool {
		result.Warnings = append(result.Warnings, "静态股票池存在幸存者偏差，不能作为正式晋级证据")
	}
	result.Completed = true
	return result, nil
}

func sameConfirmationBaseline(a, b Result) bool {
	// An omitted empty open_positions array decodes as nil in persisted results.
	samePositions := slices.EqualFunc(a.OpenPositions, b.OpenPositions, func(a, b OpenPosition) bool { return reflect.DeepEqual(a, b) })
	return sameStudyMetrics(a, b) && reflect.DeepEqual(a.Request, b.Request) && reflect.DeepEqual(a.Trades, b.Trades) && samePositions && reflect.DeepEqual(a.Equity, b.Equity) && reflect.DeepEqual(a.DataCoverage, b.DataCoverage)
}

func confirmationImpact(baseline, confirmed Result, bigWinThreshold float64) ConfirmationImpact {
	result := ConfirmationImpact{BigWinThreshold: bigWinThreshold, EquityDifference: confirmed.Metrics.FinalEquity - baseline.Metrics.FinalEquity, Opportunities: []ConfirmationOpportunity{}}
	rows := map[string]*ConfirmationOpportunity{}
	key := func(symbol, date string) string { return symbol + "|" + date }
	rowFor := func(symbol, name, date string) *ConfirmationOpportunity {
		k := key(symbol, date)
		if rows[k] == nil {
			rows[k] = &ConfirmationOpportunity{Symbol: symbol, Name: name, SignalDate: date}
		}
		return rows[k]
	}
	for i, run := range []Result{baseline, confirmed} {
		assign := func(symbol, name, date string, outcome ConfirmationEntryOutcome) {
			row := rowFor(symbol, name, date)
			if i == 0 {
				row.Baseline = &outcome
			} else {
				row.Confirmed = &outcome
			}
		}
		for _, trade := range run.Trades {
			exit := trade.Exit
			assign(trade.Symbol, trade.Name, trade.EntrySignal.Date, ConfirmationEntryOutcome{Entry: trade.Entry, Exit: &exit, Profit: trade.NetProfit, ReturnPercent: trade.ReturnPercent, Closed: true})
		}
		for _, position := range run.OpenPositions {
			assign(position.Symbol, position.Name, position.EntrySignal.Date, ConfirmationEntryOutcome{Entry: position.Entry, Profit: position.UnrealizedProfit, ReturnPercent: position.ReturnPercent})
		}
	}
	for _, event := range confirmed.EntryConfirmations {
		switch event.Status {
		case "confirmed":
			result.ConfirmedSignals++
		case "rejected":
			result.RejectedSignals++
		case "unresolved":
			result.UnresolvedSignals++
		}
		if row := rows[key(event.Symbol, event.SignalDate)]; row != nil {
			copy := event
			row.Confirmation = &copy
		}
	}
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		row := rows[k]
		a, b := row.Baseline, row.Confirmed
		if b != nil {
			row.ProfitDelta += b.Profit
		}
		if a != nil {
			row.ProfitDelta -= a.Profit
		}
		switch {
		case a == nil:
			row.Status = "new-entry"
			result.NewEntries++
		case b != nil:
			row.Status = "matched-entry"
			result.MatchedEntries++
			if a.Closed && a.Profit > 0 {
				result.MatchedWinProfitDelta += row.ProfitDelta
				if row.ProfitDelta < 0 {
					result.ReducedMatchedWins++
				}
			}
		case row.Confirmation != nil && row.Confirmation.Status == "rejected":
			row.Status = "confirmation-rejected"
			if a.Closed && a.Profit < 0 {
				result.AvoidedLosses++
				result.AvoidedLossAmount -= a.Profit
			}
			if a.Closed && a.Profit > 0 {
				result.MissedWins++
				result.MissedProfitAmount += a.Profit
			}
		default:
			row.Status = "other-removed-entry"
			result.OtherRemovedEntries++
		}
		if a != nil && a.Closed && a.ReturnPercent >= bigWinThreshold {
			result.BaselineBigWins++
			result.BigWinProfitDelta += row.ProfitDelta
			if b != nil {
				result.RetainedBigWinEntries++
			}
		}
		result.AttributedProfitDelta += row.ProfitDelta
		result.Opportunities = append(result.Opportunities, *row)
	}
	exposure := func(run Result) float64 {
		value := 0.0
		for _, day := range run.Equity {
			if day.Equity > 0 {
				value += day.Positions / day.Equity * 100
			}
		}
		if len(run.Equity) == 0 {
			return 0
		}
		return value / float64(len(run.Equity))
	}
	result.BaselineExposure, result.ConfirmedExposure = exposure(baseline), exposure(confirmed)
	result.ReconciliationError = result.EquityDifference - result.AttributedProfitDelta
	return result
}
