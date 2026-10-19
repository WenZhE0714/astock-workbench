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

func (app *App) runBacktestTradeReview(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("backtest trade-review", flag.ContinueOnError)
	flags.SetOutput(app.errOut)
	output := flags.String("output", "", "新建诊断目录")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("用法: astock backtest trade-review [--output 新目录] 冻结市场过滤研究目录")
	}
	input, err := filepath.Abs(flags.Arg(0))
	if err != nil {
		return err
	}
	planBytes, err := os.ReadFile(filepath.Join(input, "source-plan.json"))
	if err != nil {
		return err
	}
	var plan positionStudyPlan
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		return err
	}
	studyBytes, err := os.ReadFile(filepath.Join(input, "result.json"))
	if err != nil {
		return err
	}
	var study backtest.RegimeStudyResult
	if err := json.Unmarshal(studyBytes, &study); err != nil {
		return err
	}
	if !study.Completed || !study.BaselineReproduced || study.SourceID != plan.Source.ID || study.CandidateID != plan.Candidate || plan.CreatedAt.IsZero() || len(plan.Files) == 0 {
		return fmt.Errorf("冻结研究身份或完整性无效")
	}
	dir := *output
	if dir == "" {
		dir = filepath.Join(app.paths.BacktestsDir, "trade-reviews", "REV-"+time.Now().Format("20060102T150405"))
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
	if err := os.WriteFile(filepath.Join(dir, "source-result.json"), studyBytes, 0o600); err != nil {
		return err
	}
	provider := backtest.NewCachingDailyBarProvider(newOfflineStudyProvider(filepath.Join(dir, "cache"), plan.CreatedAt))
	result, err := backtest.ReviewStudyTrades(ctx, provider, study, func(message string) { fmt.Fprintln(app.errOut, message) })
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
	fmt.Fprintf(&report, "# 逐笔交易诊断\n\n源实验：%s；候选：%s；只读诊断，不改变策略。\n\n| 窗口 | 交易 | 盈利 | 亏损 | 已实现净盈亏 | 买入日就出现退出信号的亏损笔数 | 不超过2交易日的亏损笔数 | 首次浮盈收盘后下一可成交开盘仍盈利的亏损笔数 |\n|---|---:|---:|---:|---:|---:|---:|---:|\n", result.SourceID, result.CandidateID)
	for _, window := range result.Windows {
		s := window.Summary
		fmt.Fprintf(&report, "| %s | %d | %d | %d | %.2f | %d | %d | %d |\n", window.ID, s.Trades, s.Wins, s.Losses, s.NetProfit, s.SameDayExitSignalLosses, s.ShortHoldingLosses, s.LossesWithPositiveNextOpen)
	}
	for _, window := range result.Windows {
		fmt.Fprintf(&report, "\n## %s\n\n", window.ID)
		for _, dimension := range []struct {
			name   string
			groups []backtest.TradeReviewGroup
		}{{"开盘相对信号收盘", window.Summary.ByOpeningGap}, {"退出原因", window.Summary.ByExit}, {"亏损路径", window.Summary.ByLossPath}} {
			fmt.Fprintf(&report, "### %s\n\n| 分组 | 交易 | 盈利 | 亏损 | 净盈亏 |\n|---|---:|---:|---:|---:|\n", dimension.name)
			for _, g := range dimension.groups {
				fmt.Fprintf(&report, "| %s | %d | %d | %d | %.2f |\n", g.Label, g.Trades, g.Wins, g.Losses, g.NetProfit)
			}
		}
		if window.ID == "seen-period" {
			report.WriteString("\n### 近期逐笔\n\n| 股票 | 买入 | 卖出 | 开盘涨幅 | 净收益率 | 持有期最高收盘净浮盈 | 退出信号后价格变化影响 | 退出原因 |\n|---|---|---|---:|---:|---:|---:|---|\n")
			for _, row := range window.Rows {
				t := row.Trade
				fmt.Fprintf(&report, "| %s | %s | %s | %+.2f%% | %+.2f%% | %+.2f%% | %+.2f元 | %s |\n", t.Name, t.Entry.Date, t.Exit.Date, row.OpeningGap, t.ReturnPercent, row.MaxNetCloseReturn, row.ExitPriceChangePnL, row.ExitCategory)
			}
		}
	}
	report.WriteString("\n## 限制\n\n")
	for _, warning := range result.Warnings {
		fmt.Fprintf(&report, "- %s\n", warning)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(report.String()), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(app.out, "诊断完成：%d个窗口\n报告：%s\n", len(result.Windows), filepath.Join(dir, "report.md"))
	return nil
}
