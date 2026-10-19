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

func (app *App) runBacktestExitStudy(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("backtest exit-study", flag.ContinueOnError)
	flags.SetOutput(app.errOut)
	output := flags.String("output", "", "新建退出研究目录")
	cap := flags.Float64("cap", .10, "源仓位研究中已有的固定仓位")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("用法: astock backtest exit-study [--output 新目录] 冻结仓位研究目录")
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
		dir = filepath.Join(app.paths.BacktestsDir, "exit-studies", "EXIT-"+time.Now().Format("20060102T150405"))
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
		"exit_protections": []string{"", backtest.ExitProtectHalfProfit}, "entry_market_filter": "", "entry_confirmation": "",
		"activation_rule": "estimated net closing return reaches original stop-loss percentage (8% for P008)",
		"exit_rule":       "after activation, current estimated net closing return <= half of highest net closing return since entry; original exits have priority",
		"cost_rule":       "net close return includes entry fees and estimated exit fees/slippage; double-cost runs recompute net thresholds",
		"execution_rule":  "next executable opening after signal, preserving T+1 and one-price deferral; no intraday peak or same-close execution",
		"diagnostic_rule": "keep baseline entry and quantity; stop searching after original exit signal; report separately from portfolio returns",
		"captured_at":     plan.CreatedAt, "created_at": time.Now(), "promotion_allowed": false,
	}
	if err := writeStudyJSON(filepath.Join(dir, "plan.json"), protocol); err != nil {
		return err
	}
	provider := backtest.NewCachingDailyBarProvider(newOfflineStudyProvider(filepath.Join(dir, "cache"), plan.CreatedAt))
	result, runErr := backtest.RunExitStudy(ctx, backtest.NewDailyEngine(provider), provider, plan.Source, plan.Candidate, *reference, func(message string) { fmt.Fprintln(app.errOut, message) })
	if err := writeStudyJSON(filepath.Join(dir, "result.json"), result); err != nil {
		return err
	}
	if runErr != nil {
		if err := writeStudyJSON(filepath.Join(dir, "error.json"), map[string]string{"error": runErr.Error()}); err != nil {
			return err
		}
		return fmt.Errorf("%w；证据保留于%s", runErr, dir)
	}
	report := exitStudyReport(result)
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(report), 0o600); err != nil {
		return err
	}
	fmt.Fprintln(app.out, report)
	fmt.Fprintf(app.out, "报告：%s\n", filepath.Join(dir, "report.md"))
	return nil
}

func exitStudyReport(result backtest.ExitStudyResult) string {
	var report strings.Builder
	fmt.Fprintf(&report, "# 盈利回吐与退出效率研究\n\n源实验 %s / %s，仓位上限 %.0f%%，原成交与净值复现：%v。\n\n净收盘浮盈达到原止损比例（P008 为 8%%）后，回吐至历史净收盘浮盈峰值的一半则发出卖出信号，下一可成交开盘执行。原止损、止盈、均线及持有期退出优先。不增加入场确认或市场过滤。\n\n| 版本 | 平均验证收益 | 最差回撤幅度 | 正收益窗口 | 交易 | 常规检查 | 双倍成本平均收益 | 双倍成本检查 |\n|---|---:|---:|---:|---:|---|---:|---|\n", result.SourceID, result.CandidateID, result.MaxPosition*100, result.BaselineReproduced)
	for _, variant := range result.Variants {
		label := "原始退出"
		if variant.Protection != "" {
			label = "利润回吐保护"
		}
		n, s := variant.Normal, variant.DoubleCost
		fmt.Fprintf(&report, "| %s | %+.4f%% | %.4f%% | %.0f%% | %d | %v | %+.4f%% | %v |\n", label, n.AverageReturn, n.WorstDrawdown, n.PositiveRatio*100, n.Trades, n.Passed, s.AverageReturn, s.Passed)
	}
	report.WriteString("\n## 窗口对照\n\n| 窗口 | 原收益 | 保护收益 | 原回撤 | 保护回撤 | 双倍成本原收益 | 双倍成本保护收益 | 保护退出数 | 净值差 |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	base := append(append([]backtest.PositionStudyWindow{}, result.Variants[0].Windows...), result.Variants[0].Diagnostic)
	protected := append(append([]backtest.PositionStudyWindow{}, result.Variants[1].Windows...), result.Variants[1].Diagnostic)
	for i, window := range base {
		a, b := window.Normal.Metrics, protected[i].Normal.Metrics
		fmt.Fprintf(&report, "| %s | %+.4f%% | %+.4f%% | %.4f%% | %.4f%% | %+.4f%% | %+.4f%% | %d | %+.2f |\n", window.ID, a.TotalReturn, b.TotalReturn, a.MaxDrawdown, b.MaxDrawdown, window.DoubleCost.Metrics.TotalReturn, protected[i].DoubleCost.Metrics.TotalReturn, result.Windows[i].Normal.ProtectionExits, result.Windows[i].Normal.EquityDifference)
	}
	report.WriteString("\n## 固定原入场的逐笔诊断\n\n以下为常规成本，金额单位元；不含重新入场或资金路径变化，不可当作组合收益或与净值差相加。浮盈即退只作诊断，不作为本轮组合候选。\n\n| 窗口 | 亏损笔数 | 曾收盘浮盈的亏损 | 首次浮盈后开盘仍盈利的亏损 | 浮盈即退盈亏差 | 其中原赢家盈亏差 | 保护触发 | 保护实际亏损退出 | 保护盈亏差 |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, window := range result.Windows {
		s := window.NormalDiagnostic.Summary
		fmt.Fprintf(&report, "| %s | %d | %d | %d | %+.2f | %+.2f | %d | %d | %+.2f |\n", window.ID, s.Losses, s.LossesWithCloseProfit, s.LossesWithFirstOpenProfit, s.FirstPositiveProfitDelta, s.FirstPositiveWinnerDelta, s.ProtectionSignals, s.ProtectionLosingFills, s.ProtectionProfitDelta)
	}
	report.WriteString("\n## 组合机会归因\n\n按股票与原始信号日期匹配，包含实际资金路径及期末盯市盈亏。大赢家为原已平仓净收益率至少达到原止盈比例的交易。\n\n| 窗口 | 原亏损组盈亏差 | 原盈利组盈亏差 | 原大赢家保留入场 | 原大赢家盈亏差 | 新增/消失入场 | 对账误差 |\n|---|---:|---:|---:|---:|---:|---:|\n")
	for _, window := range result.Windows {
		s := window.Normal
		fmt.Fprintf(&report, "| %s | %+.2f | %+.2f | %d/%d | %+.2f | %d/%d | %.8f |\n", window.ID, s.BaselineLossDelta, s.BaselineWinnerDelta, s.RetainedBigWinEntries, s.BaselineBigWins, s.BigWinProfitDelta, s.NewEntries, s.RemovedEntries, s.ReconciliationError)
	}
	report.WriteString("\n## 检查与限制\n\n")
	for _, variant := range result.Variants {
		for _, reason := range variant.Normal.Reasons {
			fmt.Fprintf(&report, "- 规则 %q 常规成本：%s\n", variant.Protection, reason)
		}
		for _, reason := range variant.DoubleCost.Reasons {
			fmt.Fprintf(&report, "- 规则 %q 双倍成本：%s\n", variant.Protection, reason)
		}
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(&report, "- %s\n", warning)
	}
	return report.String()
}
