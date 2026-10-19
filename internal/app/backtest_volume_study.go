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

func (app *App) runBacktestVolumeStudy(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("backtest volume-study", flag.ContinueOnError)
	flags.SetOutput(app.errOut)
	output := flags.String("output", "", "新建研究目录")
	cap := flags.Float64("cap", .10, "既有仓位研究中的固定仓位")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("用法: astock backtest volume-study [--output 新目录] 冻结仓位研究目录")
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
		dir = filepath.Join(app.paths.BacktestsDir, "volume-studies", "VOL-"+time.Now().Format("20060102T150405"))
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
		"entry_volume_floors": []float64{0, backtest.VolumeStudyMinimum},
		"entry_market_filter": "", "entry_confirmation": "", "exit_protection": "",
		"entry_rule":        "retain original signal shape and volume bounds; require actual signal-day volume / prior-20-session mean >= 1.5",
		"execution_rule":    "original next executable opening; no same-close fill or future volume; preserve T+1 and one-price deferral",
		"attribution_rule":  "match symbol and original signal date; separate rejected losses, missed wins, retained winners and changed position paths",
		"hypothesis_source": "already-seen stability-review volume group; not an unseen validation",
		"captured_at":       plan.CreatedAt, "created_at": time.Now(), "promotion_allowed": false,
	}
	if err := writeStudyJSON(filepath.Join(dir, "plan.json"), protocol); err != nil {
		return err
	}
	engine := backtest.NewDailyEngine(backtest.NewCachingDailyBarProvider(newOfflineStudyProvider(filepath.Join(dir, "cache"), plan.CreatedAt)))
	result, runErr := backtest.RunVolumeStudy(ctx, engine, plan.Source, plan.Candidate, *reference, func(message string) { fmt.Fprintln(app.errOut, message) })
	if err := writeStudyJSON(filepath.Join(dir, "result.json"), result); err != nil {
		return err
	}
	if runErr != nil {
		if err := writeStudyJSON(filepath.Join(dir, "error.json"), map[string]string{"error": runErr.Error()}); err != nil {
			return err
		}
		return fmt.Errorf("%w；证据保留于%s", runErr, dir)
	}
	report := volumeStudyReport(result)
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(report), 0o600); err != nil {
		return err
	}
	fmt.Fprintln(app.out, report)
	fmt.Fprintf(app.out, "报告：%s\n", filepath.Join(dir, "report.md"))
	return nil
}

func volumeStudyReport(result backtest.VolumeStudyResult) string {
	var report strings.Builder
	fmt.Fprintf(&report, "# 实际量比至少1.5倍的组合对照\n\n%s / %s，单股仓位上限 %.0f%%，原基线复现：%v。\n\n保留原始入场条件及形态量能上下界，仅追加信号收盘量比至少1.5倍。成交、退出、费用和股票池不变，不叠加此前的确认或退出保护。\n\n| 版本 | 平均验证收益 | 最差回撤幅度 | 正收益窗口比例 | 平仓交易 | 常规检查 | 双倍成本平均收益 | 双倍成本检查 |\n|---|---:|---:|---:|---:|---|---:|---|\n", result.SourceID, result.CandidateID, result.MaxPosition*100, result.BaselineReproduced)
	for _, variant := range result.Variants {
		label := "原入场"
		if variant.Minimum > 0 {
			label = "实际量比至少1.5倍"
		}
		n, s := variant.Normal, variant.DoubleCost
		fmt.Fprintf(&report, "| %s | %+.4f%% | %.4f%% | %.0f%% | %d | %v | %+.4f%% | %v |\n", label, n.AverageReturn, n.WorstDrawdown, n.PositiveRatio*100, n.Trades, n.Passed, s.AverageReturn, s.Passed)
	}
	report.WriteString("\n## 窗口对照\n\n| 窗口 | 原收益 | 过滤收益 | 原回撤 | 过滤回撤 | 双倍成本原收益 | 双倍成本过滤收益 | 原交易数 | 过滤交易数 | 原平均仓位 | 过滤平均仓位 |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	base := append(append([]backtest.PositionStudyWindow{}, result.Variants[0].Windows...), result.Variants[0].Diagnostic)
	filtered := append(append([]backtest.PositionStudyWindow{}, result.Variants[1].Windows...), result.Variants[1].Diagnostic)
	for i, window := range base {
		a, b := window.Normal.Metrics, filtered[i].Normal.Metrics
		impact := result.Comparisons[i].Normal
		fmt.Fprintf(&report, "| %s | %+.4f%% | %+.4f%% | %.4f%% | %.4f%% | %+.4f%% | %+.4f%% | %d | %d | %.2f%% | %.2f%% |\n", window.ID, a.TotalReturn, b.TotalReturn, a.MaxDrawdown, b.MaxDrawdown, window.DoubleCost.Metrics.TotalReturn, filtered[i].DoubleCost.Metrics.TotalReturn, a.Trades, b.Trades, impact.BaselineExposure, impact.FilteredExposure)
	}
	report.WriteString("\n## 机会与资金路径\n\n金额单位元。避开亏损或错过盈利仅计量比明确拒绝的基线平仓；期末持仓纳入净值差。各项已包含在净值差中，不能重复相加。大赢家沿用原止盈比例定义。\n\n| 窗口 | 拒绝信号 | 避开亏损笔数/金额 | 错过盈利笔数/金额 | 保留赢家盈亏差 | 原大赢家保留入场 | 原大赢家盈亏差 | 新增/其他消失入场 | 节省费用 | 净值差 | 对账误差 |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, comparison := range result.Comparisons {
		v := comparison.Normal
		fmt.Fprintf(&report, "| %s | %d | %d / %.2f | %d / %.2f | %+.2f | %d/%d | %+.2f | %d/%d | %+.2f | %+.2f | %.8f |\n", comparison.ID, v.BlockedSignals, v.AvoidedLosses, v.AvoidedLossAmount, v.MissedWins, v.MissedProfitAmount, v.RetainedWinnerDelta, v.RetainedBigWinEntries, v.BaselineBigWins, v.BigWinProfitDelta, v.NewEntries, v.OtherRemovedEntries, v.SavedFees, v.EquityDifference, v.ReconciliationError)
	}
	report.WriteString("\n## 已见近期逐笔\n\n| 股票 | 原信号 | 归因 | 原盈亏 | 过滤后盈亏 | 差额 |\n|---|---|---|---:|---:|---:|\n")
	for _, comparison := range result.Comparisons {
		if comparison.ID != "seen-period" {
			continue
		}
		for _, row := range comparison.Normal.Opportunities {
			a, b := 0.0, 0.0
			if row.Baseline != nil {
				a = row.Baseline.Profit
			}
			if row.Filtered != nil {
				b = row.Filtered.Profit
			}
			label := map[string]string{"new-entry": "新增入场", "retained": "保留入场", "volume-rejected": "量比拒绝", "other-removed": "其他未成交"}[row.Status]
			fmt.Fprintf(&report, "| %s | %s | %s | %+.2f | %+.2f | %+.2f |\n", row.Name, row.SignalDate, label, a, b, row.ProfitDelta)
		}
	}
	report.WriteString("\n## 检查与限制\n\n")
	for _, variant := range result.Variants {
		for _, reason := range variant.Normal.Reasons {
			fmt.Fprintf(&report, "- 量比下限 %.1f，常规成本：%s\n", variant.Minimum, reason)
		}
		for _, reason := range variant.DoubleCost.Reasons {
			fmt.Fprintf(&report, "- 量比下限 %.1f，双倍成本：%s\n", variant.Minimum, reason)
		}
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(&report, "- %s\n", warning)
	}
	return report.String()
}
