package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

func (app *App) runBacktestStabilityReview(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("backtest stability-review", flag.ContinueOnError)
	flags.SetOutput(app.errOut)
	output := flags.String("output", "", "新建集中度与稳定性诊断目录")
	cap := flags.Float64("cap", .10, "源研究中已有的仓位")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("用法: astock backtest stability-review [--output 新目录] 冻结仓位研究目录")
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
		dir = filepath.Join(app.paths.BacktestsDir, "stability-reviews", "STAB-"+time.Now().Format("20060102T150405"))
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
		"ma_distance_percent_boundaries": []float64{3, 5}, "trend_spread_percent_boundaries": []float64{5, 10},
		"return_20_percent_boundaries": []float64{0, 10}, "volume_ratio_boundaries": []float64{1, 1.5},
		"minimum_closed_trades_per_window_and_cost": 5, "minimum_validation_windows": 3, "minimum_total_closed_trades_per_cost": 20,
		"year_attribution": "daily marked PnL including fees and open positions; normal opening exits exclude the later close; independent window cash resets",
		"concentration":    "top 1/3/5 closed winners divided by sum of winning PnL; top 1/3 positive symbol contributions divided by sum of positive symbol PnL",
		"stability":        "all validation windows must have the same sign of realized PnL under both costs; sample minimum required; seen period excluded",
		"captured_at":      plan.CreatedAt, "created_at": time.Now(), "promotion_allowed": false,
	}
	if err := writeStudyJSON(filepath.Join(dir, "plan.json"), protocol); err != nil {
		return err
	}
	provider := backtest.NewCachingDailyBarProvider(newOfflineStudyProvider(filepath.Join(dir, "cache"), plan.CreatedAt))
	result, runErr := backtest.RunStabilityReview(ctx, backtest.NewDailyEngine(provider), provider, plan.Source.ID, plan.Candidate, *reference, func(message string) { fmt.Fprintln(app.errOut, message) })
	if err := writeStudyJSON(filepath.Join(dir, "result.json"), result); err != nil {
		return err
	}
	if runErr != nil {
		if err := writeStudyJSON(filepath.Join(dir, "error.json"), map[string]string{"error": runErr.Error()}); err != nil {
			return err
		}
		return fmt.Errorf("%w；证据保留于%s", runErr, dir)
	}
	report := stabilityReviewReport(result)
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(report), 0o600); err != nil {
		return err
	}
	fmt.Fprintln(app.out, report)
	fmt.Fprintf(app.out, "报告：%s\n", filepath.Join(dir, "report.md"))
	return nil
}

func stabilityReviewReport(result backtest.StabilityReviewResult) string {
	var report strings.Builder
	fmt.Fprintf(&report, "# 收益集中度与入场信号稳定性\n\n%s / %s，单股上限 %.0f%%；10 次原策略回放基线复现：%v。无策略参数修改。\n\n金额均为元；验证窗独立重置本金，合计盈亏不是连续账户收益。未平仓计入贡献但不计胜负。\n\n## 集中度\n\n| 成本 | 验证总盈亏 | 已实现 | 未平仓 | 最大盈利占全部盈利 | 前3笔占全部盈利 | 前5笔占全部盈利 | 减去前5笔贡献后余量 | 前3只占正贡献股票 | 减去前3只贡献后余量 |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n", result.SourceID, result.CandidateID, result.Baseline.MaxPosition*100, result.BaselineReproduced)
	for i, analysis := range []backtest.StabilityAnalysis{result.NormalValidation, result.StressValidation} {
		label := []string{"常规", "双倍成本"}[i]
		s, c := analysis.Stats, analysis.Concentration
		fmt.Fprintf(&report, "| %s | %+.2f | %+.2f | %+.2f | %.2f%% | %.2f%% | %.2f%% | %+.2f | %.2f%% | %+.2f |\n", label, s.TotalProfit, s.RealizedProfit, s.UnrealizedProfit, c.TopOneWinnerShare*100, c.TopThreeWinnerShare*100, c.TopFiveWinnerShare*100, c.ProfitMinusTopFive, c.TopThreeSymbolShare*100, c.ProfitMinusTopThreeSymbols)
	}
	report.WriteString("\n扣减贡献为事后账面敏感性，不是删除股票或交易后重新回测。\n\n## 逐股与窗口\n\n| 股票 | 常规总贡献 | 双倍成本总贡献 | 常规各窗贡献（按验证窗顺序） | 平仓数 | 同向判定 |\n|---|---:|---:|---|---:|---|\n")
	checks := append([]backtest.StabilityGroupCheck(nil), result.GroupChecks...)
	sort.SliceStable(checks, func(i, j int) bool { return checks[i].Normal.TotalProfit > checks[j].Normal.TotalProfit })
	status := func(check backtest.StabilityGroupCheck) string {
		if !check.SufficientSample {
			return "样本不足"
		}
		if check.AllNegative {
			return "各窗双成本均亏损"
		}
		if check.AllPositive {
			return "各窗双成本均盈利"
		}
		return "方向不一致"
	}
	for _, check := range checks {
		if check.Dimension != "symbol" {
			continue
		}
		values := []string{}
		for _, window := range check.Windows {
			values = append(values, fmt.Sprintf("%s %+.2f", window.ID, window.Normal.TotalProfit))
		}
		fmt.Fprintf(&report, "| %s %s | %+.2f | %+.2f | %s | %d | %s |\n", check.Key, check.Label, check.Normal.TotalProfit, check.DoubleCost.TotalProfit, strings.Join(values, " / "), check.Normal.ClosedTrades, status(check))
	}
	report.WriteString("\n## 日历年盯市贡献\n\n仅汇总验证窗。按实际持有日期切分跨年盈亏，不按卖出年份整笔归类。窗口间本金会重置；以下不是年度收益率。\n\n| 年份 | 覆盖起止 | 观察交易日 | 涉及窗口 | 常规贡献 | 双倍成本贡献 |\n|---|---|---:|---|---:|---:|\n")
	for i, year := range result.NormalValidation.Years {
		fmt.Fprintf(&report, "| %s | %s 至 %s | %d | %s | %+.2f | %+.2f |\n", year.Year, year.Start, year.End, year.Days, strings.Join(year.Windows, ", "), year.Profit, result.StressValidation.Years[i].Profit)
	}
	report.WriteString("\n## 去掉单个窗口后的统计\n\n仅算剩余验证窗的收益均值，不重新运行策略。\n\n| 不计窗口 | 剩余常规平均收益 | 剩余双倍成本平均收益 | 常规盈利窗比例 | 双倍成本盈利窗比例 |\n|---|---:|---:|---:|---:|\n")
	for _, item := range result.LeaveOneOut {
		fmt.Fprintf(&report, "| %s | %+.4f%% | %+.4f%% | %.0f%% | %.0f%% |\n", item.OmittedWindow, item.NormalAverageReturn, item.StressAverageReturn, item.NormalPositiveRatio*100, item.StressPositiveRatio*100)
	}
	report.WriteString("\n## 入场信号分组\n\n使用信号当日收盘已知数据。各维度重复覆盖同一批交易，维度之间不能相加。判定要求每窗每种成本至少5笔平仓、合计至少20笔、至少3窗；同向仅指已实现盈亏。\n\n| 维度 / 分组 | 平仓数 | 常规总贡献 | 双倍成本总贡献 | 平均单笔净收益率 | 常规各窗已实现盈亏 / 笔数 | 判定 |\n|---|---:|---:|---:|---:|---|---|\n")
	labels := map[string]string{"ma_distance": "均线距离", "trend_spread": "趋势强度", "return_20": "20日涨幅", "volume_ratio": "量比"}
	for _, check := range result.GroupChecks {
		if check.Dimension == "symbol" || check.Key == "unknown" && check.Normal.ClosedTrades == 0 && check.Normal.OpenPositions == 0 {
			continue
		}
		values := []string{}
		for _, window := range check.Windows {
			values = append(values, fmt.Sprintf("%s %+.2f / %d", window.ID, window.Normal.RealizedProfit, window.Normal.ClosedTrades))
		}
		fmt.Fprintf(&report, "| %s / %s | %d | %+.2f | %+.2f | %+.2f%% | %s | %s |\n", labels[check.Dimension], check.Label, check.Normal.ClosedTrades, check.Normal.TotalProfit, check.DoubleCost.TotalProfit, check.Normal.AverageReturn, strings.Join(values, "；"), status(check))
	}
	report.WriteString("\n## 已见近期区间\n\n")
	recent := result.Windows[len(result.Windows)-1]
	fmt.Fprintf(&report, "%s：常规 %+.2f 元，双倍成本 %+.2f 元；常规平仓 %d 笔。该区间没有参与上面的验证集中度或同向判定。\n", recent.ID, recent.Normal.Stats.TotalProfit, recent.DoubleCost.Stats.TotalProfit, recent.Normal.Stats.ClosedTrades)
	report.WriteString("\n## 限制\n\n")
	for _, warning := range result.Warnings {
		fmt.Fprintf(&report, "- %s\n", warning)
	}
	return report.String()
}
