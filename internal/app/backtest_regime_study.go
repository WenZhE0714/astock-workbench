package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

func (app *App) runBacktestRegimeStudy(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("backtest regime-study", flag.ContinueOnError)
	flags.SetOutput(app.errOut)
	output := flags.String("output", "", "新建研究目录")
	cap := flags.Float64("cap", .10, "既有仓位研究中的固定仓位")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("用法: astock backtest regime-study [--output 新目录] 冻结仓位研究目录")
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
		return fmt.Errorf("源仓位研究未完成、基线未复现或输入身份不一致")
	}
	var reference *backtest.PositionStudyVariant
	for _, item := range prior.Variants {
		if item.MaxPosition == *cap {
			copy := item
			reference = &copy
			break
		}
	}
	if reference == nil {
		return fmt.Errorf("源研究没有指定仓位")
	}
	dir := *output
	if dir == "" {
		dir = filepath.Join(app.paths.BacktestsDir, "regime-studies", "REG-"+time.Now().Format("20060102T150405"))
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
	protocol := map[string]any{"source_directory": input, "max_position": *cap, "filters": []string{"", backtest.EntryMarketNonBear}, "information_cutoff": "benchmark closes strictly before the opening day", "captured_at": plan.CreatedAt, "created_at": time.Now(), "promotion_allowed": false}
	if err := writeStudyJSON(filepath.Join(dir, "plan.json"), protocol); err != nil {
		return err
	}
	engine := backtest.NewDailyEngine(backtest.NewCachingDailyBarProvider(newOfflineStudyProvider(filepath.Join(dir, "cache"), plan.CreatedAt)))
	result, err := backtest.RunRegimeStudy(ctx, engine, plan.Source, plan.Candidate, *reference, func(message string) { fmt.Fprintln(app.errOut, message) })
	if saveErr := writeStudyJSON(filepath.Join(dir, "result.json"), result); saveErr != nil {
		return saveErr
	}
	if err != nil {
		if saveErr := writeStudyJSON(filepath.Join(dir, "error.json"), map[string]string{"error": err.Error()}); saveErr != nil {
			return saveErr
		}
		return fmt.Errorf("%w；证据保留于%s", err, dir)
	}
	var report strings.Builder
	fmt.Fprintf(&report, "# 市场阶段过滤对照\n\n固定仓位%.0f%%；基线复现：%v。过滤仅禁止熊市新开仓，基准不足时暂停开仓；高波动状态不额外禁止。\n\n| 版本 | 平均验证收益 | 最差回撤 | 正收益窗口 | 交易 | 常规检查 | 双倍成本平均收益 | 双倍成本检查 |\n|---|---:|---:|---:|---:|---|---:|---|\n", *cap*100, result.BaselineReproduced)
	for _, item := range result.Variants {
		label := "基线"
		if item.Filter != "" {
			label = "非熊市开仓"
		}
		n, s := item.Normal, item.DoubleCost
		fmt.Fprintf(&report, "| %s | %+.2f%% | %.2f%% | %.0f%% | %d | %v | %+.2f%% | %v |\n", label, n.AverageReturn, n.WorstDrawdown, n.PositiveRatio*100, n.Trades, n.Passed, s.AverageReturn, s.Passed)
	}
	report.WriteString("\n## 窗口与机会\n\n| 窗口 | 基线收益 | 过滤收益 | 双倍成本基线 | 双倍成本过滤 | 拦截尝试 | 避开亏损笔数 | 错过盈利笔数 | 基线平均仓位 | 过滤平均仓位 |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	base := append(append([]backtest.PositionStudyWindow{}, result.Variants[0].Windows...), result.Variants[0].Diagnostic)
	filtered := append(append([]backtest.PositionStudyWindow{}, result.Variants[1].Windows...), result.Variants[1].Diagnostic)
	for i, window := range base {
		impact := result.Comparisons[i].Normal
		fmt.Fprintf(&report, "| %s | %+.2f%% | %+.2f%% | %+.2f%% | %+.2f%% | %d | %d | %d | %.1f%% | %.1f%% |\n", window.ID, window.Normal.Metrics.TotalReturn, filtered[i].Normal.Metrics.TotalReturn, window.DoubleCost.Metrics.TotalReturn, filtered[i].DoubleCost.Metrics.TotalReturn, impact.BlockedAttempts, impact.AvoidedLosses, impact.MissedWins, impact.BaselineExposure, impact.FilteredExposure)
	}
	report.WriteString("\n## 限制\n\n")
	for _, warning := range append(result.Warnings, result.BaselineDifferences...) {
		fmt.Fprintf(&report, "- %s\n", warning)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(report.String()), 0o600); err != nil {
		return err
	}
	fmt.Fprintln(app.out, report.String())
	fmt.Fprintf(app.out, "报告：%s\n", filepath.Join(dir, "report.md"))
	return nil
}

func copyVerifiedStudyInputs(source, target string, files map[string]string) error {
	for relative, expected := range files {
		if filepath.IsAbs(relative) || filepath.Clean(relative) != relative || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("输入文件路径无效")
		}
		data, err := os.ReadFile(filepath.Join(source, relative))
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != expected {
			return fmt.Errorf("冻结输入指纹不一致: %s", relative)
		}
		destination := filepath.Join(target, relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(destination, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}
