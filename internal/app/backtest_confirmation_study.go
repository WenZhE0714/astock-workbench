package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

func (app *App) runBacktestConfirmationStudy(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("backtest confirmation-study", flag.ContinueOnError)
	flags.SetOutput(app.errOut)
	output := flags.String("output", "", "新建研究目录")
	cap := flags.Float64("cap", .10, "既有仓位研究中的固定仓位")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("用法: astock backtest confirmation-study [--output 新目录] 冻结仓位研究目录")
	}
	input, err := filepath.Abs(flags.Arg(0))
	if err != nil {
		return err
	}
	planBytes, err := os.ReadFile(filepath.Join(input, "plan.json"))
	if err != nil {
		return err
	}
	var plan positionStudyPlan
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		return err
	}
	resultBytes, err := os.ReadFile(filepath.Join(input, "result.json"))
	if err != nil {
		return err
	}
	var prior backtest.PositionStudyResult
	if err := json.Unmarshal(resultBytes, &prior); err != nil {
		return err
	}
	if !prior.Completed || !prior.SourceBaselineReproduced || prior.SourceID != plan.Source.ID || prior.CandidateID != plan.Candidate || plan.CreatedAt.IsZero() || len(plan.Files) == 0 {
		return fmt.Errorf("冻结仓位研究身份或完整性无效")
	}
	var reference *backtest.PositionStudyVariant
	for _, variant := range prior.Variants {
		if variant.MaxPosition == *cap {
			copy := variant
			reference = &copy
			break
		}
	}
	if reference == nil {
		return fmt.Errorf("源研究没有指定仓位")
	}
	dir := *output
	if dir == "" {
		dir = filepath.Join(app.paths.BacktestsDir, "confirmation-studies", "CONF-"+time.Now().Format("20060102T150405"))
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("输出目录已存在")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := copyVerifiedStudyInputs(filepath.Join(input, "cache"), filepath.Join(dir, "cache"), plan.Files); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "source-plan.json"), planBytes, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "source-result.json"), resultBytes, 0o600); err != nil {
		return err
	}
	protocol := map[string]any{
		"source_directory": input, "source_id": plan.Source.ID, "candidate_id": plan.Candidate, "max_position": *cap,
		"confirmations": []string{"", backtest.EntryConfirmReclaimTwoClose}, "entry_market_filter": "",
		"confirmation_rule": "only the next stock trading close: close > fast MA > slow MA; no second volume or distance condition; failed signals expire",
		"execution_rule":    "fill no earlier than the open after the confirmation close; preserve original signal date and existing one-price/T+1 rules",
		"big_winner_rule":   "baseline closed net return >= original take-profit percentage",
		"captured_at":       plan.CreatedAt, "created_at": time.Now(), "promotion_allowed": false,
	}
	if err := writeStudyJSON(filepath.Join(dir, "plan.json"), protocol); err != nil {
		return err
	}
	engine := backtest.NewDailyEngine(backtest.NewCachingDailyBarProvider(newOfflineStudyProvider(filepath.Join(dir, "cache"), plan.CreatedAt)))
	result, runErr := backtest.RunConfirmationStudy(ctx, engine, plan.Source, plan.Candidate, *reference, func(message string) { fmt.Fprintln(app.errOut, message) })
	if err := writeStudyJSON(filepath.Join(dir, "result.json"), result); err != nil {
		return err
	}
	if runErr != nil {
		if err := writeStudyJSON(filepath.Join(dir, "error.json"), map[string]string{"error": runErr.Error()}); err != nil {
			return err
		}
		return fmt.Errorf("%w；证据保留于%s", runErr, dir)
	}
	report := confirmationStudyReport(result)
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(report), 0o600); err != nil {
		return err
	}
	fmt.Fprintln(app.out, report)
	fmt.Fprintf(app.out, "报告：%s\n", filepath.Join(dir, "report.md"))
	return nil
}

func confirmationStudyReport(result backtest.ConfirmationStudyResult) string {
	var report strings.Builder
	fmt.Fprintf(&report, "# 双收盘确认对照\n\n源实验：%s；候选：%s；仓位上限 %.0f%%；基线成交与净值复现：%v。\n\n原信号后下一股票交易日收盘仍满足价格高于快线、快线高于慢线才确认，最早随后开盘成交；确认失败即作废。不叠加市场过滤，不增加第二次量能门槛。\n\n| 版本 | 平均验证收益 | 最差回撤 | 正收益窗口比例 | 交易 | 常规检查 | 双倍成本平均收益 | 双倍成本检查 |\n|---|---:|---:|---:|---:|---|---:|---|\n", result.SourceID, result.CandidateID, result.MaxPosition*100, result.BaselineReproduced)
	for _, variant := range result.Variants {
		label := "单收盘基线"
		if variant.Confirmation != "" {
			label = "双收盘确认"
		}
		n, s := variant.Normal, variant.DoubleCost
		fmt.Fprintf(&report, "| %s | %+.4f%% | %.4f%% | %.0f%% | %d | %v | %+.4f%% | %v |\n", label, n.AverageReturn, n.WorstDrawdown, n.PositiveRatio*100, n.Trades, n.Passed, s.AverageReturn, s.Passed)
	}
	for _, variant := range result.Variants {
		for _, reason := range variant.Normal.Reasons {
			fmt.Fprintf(&report, "\n规则 %q 常规检查：%s。\n", variant.Confirmation, reason)
		}
		for _, reason := range variant.DoubleCost.Reasons {
			fmt.Fprintf(&report, "\n规则 %q 双倍成本检查：%s。\n", variant.Confirmation, reason)
		}
	}
	report.WriteString("\n## 窗口对照\n\n| 窗口 | 基线收益 | 确认收益 | 基线回撤 | 确认回撤 | 双倍成本基线 | 双倍成本确认 | 基线交易 | 确认交易 |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	base := append(append([]backtest.PositionStudyWindow{}, result.Variants[0].Windows...), result.Variants[0].Diagnostic)
	confirmed := append(append([]backtest.PositionStudyWindow{}, result.Variants[1].Windows...), result.Variants[1].Diagnostic)
	for i, window := range base {
		a, b := window.Normal.Metrics, confirmed[i].Normal.Metrics
		fmt.Fprintf(&report, "| %s | %+.4f%% | %+.4f%% | %.4f%% | %.4f%% | %+.4f%% | %+.4f%% | %d | %d |\n", window.ID, a.TotalReturn, b.TotalReturn, a.MaxDrawdown, b.MaxDrawdown, window.DoubleCost.Metrics.TotalReturn, confirmed[i].DoubleCost.Metrics.TotalReturn, a.Trades, b.Trades)
	}
	report.WriteString("\n## 常规成本机会归因\n\n仅把确认明确拒绝的基线成交计为避开或错过。保留赢家计入延迟成交后的盈亏变化，包含实际仓位数量变化。金额单位为元。\n\n| 窗口 | 拒绝信号 | 避开亏损笔数/金额 | 错过盈利笔数/金额 | 保留赢家盈亏变化 | 原大赢家保留数 | 原大赢家盈亏变化 | 新增/其他消失成交 | 净值差 | 对账误差 |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, comparison := range result.Comparisons {
		v := comparison.Normal
		fmt.Fprintf(&report, "| %s | %d | %d / %.2f | %d / %.2f | %+.2f | %d/%d | %+.2f | %d/%d | %+.2f | %.8f |\n", comparison.ID, v.RejectedSignals, v.AvoidedLosses, v.AvoidedLossAmount, v.MissedWins, v.MissedProfitAmount, v.MatchedWinProfitDelta, v.RetainedBigWinEntries, v.BaselineBigWins, v.BigWinProfitDelta, v.NewEntries, v.OtherRemovedEntries, v.EquityDifference, v.ReconciliationError)
	}
	report.WriteString("\n## 近期机会逐笔\n\n盈亏包含已平仓净盈亏或期末盯市浮盈亏；未平仓另行标记。\n\n| 股票 | 原信号 | 归因 | 基线买入 | 确认买入 | 基线盈亏 | 确认盈亏 | 差额 |\n|---|---|---|---|---|---:|---:|---:|\n")
	for _, comparison := range result.Comparisons {
		if comparison.ID != "seen-period" {
			continue
		}
		for _, row := range comparison.Normal.Opportunities {
			entry := func(outcome *backtest.ConfirmationEntryOutcome) (string, float64) {
				if outcome == nil {
					return "无", 0
				}
				date := outcome.Entry.Date
				if !outcome.Closed {
					date += "（未平仓）"
				}
				return date, outcome.Profit
			}
			aDate, aProfit := entry(row.Baseline)
			bDate, bProfit := entry(row.Confirmed)
			label := map[string]string{"new-entry": "新增成交", "matched-entry": "保留并延迟", "confirmation-rejected": "确认拒绝", "other-removed-entry": "其他未成交"}[row.Status]
			fmt.Fprintf(&report, "| %s | %s | %s | %s | %s | %.2f | %.2f | %+.2f |\n", row.Name, row.SignalDate, label, aDate, bDate, aProfit, bProfit, row.ProfitDelta)
		}
	}
	report.WriteString("\n## 限制\n\n")
	for _, warning := range result.Warnings {
		fmt.Fprintf(&report, "- %s\n", warning)
	}
	return report.String()
}
