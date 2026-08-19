package app

import (
	"context"
	"flag"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type continuousOptimizationOptions struct {
	Folds           int
	TrainYears      int
	ValidateYears   int
	HoldoutMonths   int
	MinimumTrades   int
	MinimumPositive float64
	MaximumDrawdown float64
	UseAI           bool
}

func defaultContinuousOptimizationOptions() continuousOptimizationOptions {
	return continuousOptimizationOptions{
		Folds: 4, TrainYears: 4, ValidateYears: 1, HoldoutMonths: 3,
		MinimumTrades: 20, MinimumPositive: .67, MaximumDrawdown: 15, UseAI: true,
	}
}

func buildWalkForwardPeriods(end time.Time, options continuousOptimizationOptions) ([]backtest.WalkForwardFold, backtest.Period) {
	end = dateOnly(end)
	holdout := backtest.Period{Start: end.AddDate(0, -options.HoldoutMonths, 1), End: end}
	folds := make([]backtest.WalkForwardFold, 0, options.Folds)
	for index := options.Folds - 1; index >= 0; index-- {
		validateEnd := holdout.Start.AddDate(-index*options.ValidateYears, 0, -1)
		validate := backtest.Period{Start: validateEnd.AddDate(-options.ValidateYears, 0, 1), End: validateEnd}
		trainEnd := validate.Start.AddDate(0, 0, -1)
		train := backtest.Period{Start: trainEnd.AddDate(-options.TrainYears, 0, 1), End: trainEnd}
		folds = append(folds, backtest.WalkForwardFold{ID: fmt.Sprintf("F%02d", len(folds)+1), Train: train, Validate: validate})
	}
	return folds, holdout
}

func sameTickerPool(left, right []string) bool {
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}

func (app *App) continuousOptimizationStore() *storage.ContinuousOptimizationStore {
	root := app.paths.ContinuousOptimizationsDir
	if root == "" {
		root = app.paths.BacktestsDir + "/continuous"
	}
	return storage.NewContinuousOptimizationStore(root)
}

func continuousLessons(previous backtest.ContinuousOptimizationResult) string {
	if previous.Selected == nil {
		return "上一轮没有候选通过滚动验证门禁。"
	}
	p := previous.Selected.Proposal.Parameters
	parts := []string{fmt.Sprintf(
		"上一轮%s，参数%s MA%d/%d B%d V%.2f SL%.0f%% TP%.0f%% H%d",
		previous.Stage, p.EffectiveEntryMode(), p.FastMA, p.SlowMA, p.BreakoutDays,
		p.VolumeRatioMin, p.StopLoss*100, p.TakeProfit*100, p.MaxHoldingDays,
	)}
	if previous.Holdout != nil {
		m := previous.Holdout.Metrics
		parts = append(parts, fmt.Sprintf("最终留出收益%+.2f%%、回撤%+.2f%%、夏普%.2f、交易%d", m.TotalReturn, m.MaxDrawdown, m.Sharpe, m.Trades))
	}
	if len(previous.GateReasons) > 0 {
		parts = append(parts, "未晋级原因："+strings.Join(previous.GateReasons, "；"))
	}
	return strings.Join(parts, "。") + "。"
}

func (app *App) latestContinuousBaseline(symbols []string, cutoff time.Time, holdoutMonths int) (backtest.TechnicalParameters, string, int, string, string, error) {
	baseline := backtest.DefaultTechnicalParameters()
	results, err := app.continuousOptimizationStore().All()
	if err != nil {
		return baseline, "", 1, "", "", err
	}
	for _, previous := range results {
		if !sameTickerPool(previous.Request.BaseRequest.Tickers, symbols) {
			continue
		}
		if previous.Holdout == nil && previous.Manifest.SchemaVersion > 0 {
			continue
		}
		newHoldoutStart := dateOnly(cutoff).AddDate(0, -holdoutMonths, 1)
		previousHoldoutEnd := dateOnly(previous.Request.Holdout.End)
		if previousHoldoutEnd.IsZero() || previous.Request.Holdout.End.IsZero() {
			// Archives written before the explicit holdout field used DataCutoff
			// as the inspected end date. Preserve the no-reuse rule for them too.
			var parseError error
			previousHoldoutEnd, parseError = time.ParseInLocation("2006-01-02", previous.DataCutoff, shanghaiLocation)
			if parseError != nil {
				return baseline, previous.ID, previous.Cycle + 1, "", "", fmt.Errorf("历史实验 %s 缺少可验证的最终留出边界", previous.ID)
			}
			previousHoldoutEnd = dateOnly(previousHoldoutEnd)
		}
		if !newHoldoutStart.After(previousHoldoutEnd) {
			nextCutoff := previousHoldoutEnd.AddDate(0, holdoutMonths, 0)
			return baseline, previous.ID, previous.Cycle + 1, "", previousHoldoutEnd.Format("2006-01-02"), fmt.Errorf(
				"上一轮最终留出截至 %s；为避免重复窥视测试集，下一轮最早数据截止日为 %s",
				previousHoldoutEnd.Format("2006-01-02"), nextCutoff.Format("2006-01-02"),
			)
		}
		if previous.Stage == backtest.ContinuousStageShadow && previous.Selected != nil {
			baseline = previous.Selected.Proposal.Parameters
		}
		return baseline, previous.ID, previous.Cycle + 1, continuousLessons(previous), previousHoldoutEnd.Format("2006-01-02"), nil
	}
	return baseline, "", 1, "", "", nil
}

func (app *App) runContinuousOptimization(
	ctx context.Context,
	symbols []string,
	names map[string]string,
	end time.Time,
	options continuousOptimizationOptions,
	progress func(string),
) (backtest.ContinuousOptimizationResult, error) {
	baseline, parentID, cycle, priorLessons, previousHoldoutEnd, err := app.latestContinuousBaseline(symbols, end, options.HoldoutMonths)
	if err != nil {
		return backtest.ContinuousOptimizationResult{}, err
	}
	folds, holdout := buildWalkForwardPeriods(end, options)
	base := defaultResearchRequest(symbols, names, folds[0].Train)
	base.Technical = baseline
	if progress != nil {
		progress("主Agent启动3个子Agent并行提出受控候选")
	}
	agentRuns, proposals := app.collectStrategyAgentProposals(ctx, base, priorLessons, options.UseAI)
	if progress != nil {
		progress(fmt.Sprintf("子Agent完成，合并去重后评估%d个候选", len(proposals)))
	}
	request := backtest.ContinuousOptimizationRequest{
		BaseRequest: base, Folds: folds, Holdout: holdout, Proposals: proposals,
		MinimumValidationTrades: options.MinimumTrades, MinimumPositiveFoldRatio: options.MinimumPositive,
		MaximumValidationDrawdown: options.MaximumDrawdown,
	}
	provider := backtest.NewCachingDailyBarProvider(market.EastmoneyClient{})
	optimizer := backtest.NewContinuousOptimizer(backtest.NewDailyEngine(provider))
	result, err := optimizer.Optimize(ctx, request, agentRuns, func(item backtest.OptimizationProgress) {
		if progress == nil {
			return
		}
		switch item.Phase {
		case "walk-forward":
			progress(fmt.Sprintf("滚动训练/验证候选 %d/%d", item.Completed, item.Total))
		case "holdout":
			progress("参数已锁定，执行最终留出和双倍成本压力测试")
		}
	})
	if err != nil {
		return result, err
	}
	result.ParentID = parentID
	result.Cycle = cycle
	result.DataCutoff = dateOnly(end).Format("2006-01-02")
	result.PriorLessons = priorLessons
	result.Manifest = backtest.BuildExperimentManifest(request, result.DataCutoff, parentID, previousHoldoutEnd)
	if options.UseAI {
		if progress != nil {
			progress("主Agent监督子Agent分歧与确定性门禁")
		}
		reviewContext, cancel := context.WithTimeout(ctx, codexReportTimeout())
		review, reviewError := app.reviewContinuousOptimization(reviewContext, result)
		cancel()
		if reviewError != nil {
			result.SupervisorError = reviewError.Error()
		} else {
			result.SupervisorReview = review
		}
	}
	return app.continuousOptimizationStore().Save(result)
}

func (app *App) runBacktestContinuous(ctx context.Context, arguments []string) error {
	defaults := defaultContinuousOptimizationOptions()
	set := flag.NewFlagSet("backtest continuous", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	folds := set.Int("folds", defaults.Folds, "滚动验证折数，2-12")
	trainYears := set.Int("train-years", defaults.TrainYears, "每折训练年数")
	validateYears := set.Int("validate-years", defaults.ValidateYears, "每折验证年数")
	holdoutMonths := set.Int("holdout-months", defaults.HoldoutMonths, "最终留出月数，默认3")
	minimumTrades := set.Int("min-validation-trades", defaults.MinimumTrades, "全部滚动验证最低交易数")
	minimumPositive := set.Float64("min-positive-fold-ratio", defaults.MinimumPositive, "正收益验证窗口比例")
	maximumDrawdown := set.Float64("max-validation-drawdown", defaults.MaximumDrawdown, "验证窗口最大允许回撤百分比")
	noAI := set.Bool("no-ai", false, "关闭子Agent候选和主Agent复盘，仅运行确定性候选")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	if *folds < 2 || *folds > 12 || *trainYears < 1 || *validateYears < 1 || *holdoutMonths < 1 {
		return fmt.Errorf("滚动折数必须为2-12，训练/验证年数和留出月数必须大于0")
	}
	symbols, err := parseBacktestTickers(ctx, app.resolver, set.Args())
	if err != nil {
		return err
	}
	end := dateOnly(time.Now()).AddDate(0, 0, -1)
	fmt.Fprintln(app.errOut, "多Agent持续优化: 子Agent提案、滚动验证、留出检验和压力测试将在后台顺序执行...")
	result, err := app.runContinuousOptimization(ctx, symbols, app.resolveBacktestNames(ctx, symbols), end, continuousOptimizationOptions{
		Folds: *folds, TrainYears: *trainYears, ValidateYears: *validateYears, HoldoutMonths: *holdoutMonths,
		MinimumTrades: *minimumTrades, MinimumPositive: *minimumPositive, MaximumDrawdown: *maximumDrawdown, UseAI: !*noAI,
	}, func(message string) { fmt.Fprintln(app.errOut, message) })
	if err != nil {
		return err
	}
	stage := "研究候选"
	if result.Stage == backtest.ContinuousStageShadow {
		stage = "模拟观察候选"
	}
	fmt.Fprintf(app.out, "持续优化已完成 %s  第%d轮  阶段 %s\n", result.ID, result.Cycle, stage)
	if result.Selected != nil {
		fmt.Fprintf(app.out, "锁定 %s  滚动正收益窗口 %.0f%%  验证交易 %d\n", result.Selected.Proposal.ID, result.Selected.PositiveFoldRatio*100, result.Selected.ValidationTrades)
	}
	if result.Holdout != nil {
		fmt.Fprintf(app.out, "最终留出收益 %+.2f%%  回撤 %+.2f%%  夏普 %.2f  交易 %d\n", result.Holdout.Metrics.TotalReturn, result.Holdout.Metrics.MaxDrawdown, result.Holdout.Metrics.Sharpe, result.Holdout.Metrics.Trades)
	}
	fmt.Fprintf(app.out, "数据质量 %s（硬门 %v）  候选指纹 %s  配置指纹 %s\n", result.Quality.Grade, result.Quality.Passed, shortHash(result.Manifest.CandidateSetHash), shortHash(result.Manifest.ConfigurationHash))
	if result.Selected != nil {
		fmt.Fprintf(app.out, "Agent共识 %d  邻域候选 %d  邻域均分 %.2f\n", result.Selected.ConsensusAgents, result.Selected.NeighborhoodSize, result.Selected.NeighborhoodScore)
		for _, fold := range result.Selected.Folds {
			fmt.Fprintf(app.out, "%s 训练%+.2f%%/回撤%+.2f%%/夏普%.2f/交易%d  验证%+.2f%%/回撤%+.2f%%/夏普%.2f/交易%d\n",
				fold.Fold.ID, fold.Train.TotalReturn, fold.Train.MaxDrawdown, fold.Train.Sharpe, fold.Train.Trades,
				fold.Validate.TotalReturn, fold.Validate.MaxDrawdown, fold.Validate.Sharpe, fold.Validate.Trades)
		}
	}
	if result.PriorLessons != "" {
		fmt.Fprintf(app.out, "历史反思：%s\n", result.PriorLessons)
	}
	if len(result.GateReasons) > 0 {
		fmt.Fprintf(app.out, "未晋级：%s\n", strings.Join(result.GateReasons, "；"))
	}
	fmt.Fprintf(app.out, "报告 %s\n", result.ReportPath)
	return nil
}

func shortHash(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func (app *App) runBacktestContinuousList(arguments []string) error {
	set := flag.NewFlagSet("backtest continuous-list", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	limit := set.Int("limit", 20, "最多显示实验数")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	items, err := app.continuousOptimizationStore().List(*limit)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintln(app.out, "暂无多Agent持续优化实验")
		return nil
	}
	fmt.Fprintln(app.out, "实验ID                   生成时间             阶段          候选  入选    留出收益")
	for _, item := range items {
		stage := "研究候选"
		if item.Stage == backtest.ContinuousStageShadow {
			stage = "模拟观察"
		}
		fmt.Fprintf(app.out, "%-26s %-20s %-12s %4d  %-7s %+.2f%%\n", item.ID, item.GeneratedAt.Format("2006-01-02 15:04:05"), stage, item.Candidates, item.SelectedID, item.HoldoutReturn)
	}
	return nil
}

func (app *App) runBacktestContinuousShow(arguments []string) error {
	if len(arguments) != 1 {
		return fmt.Errorf("用法: astock backtest continuous-show <experiment-id>")
	}
	result, err := app.continuousOptimizationStore().Load(arguments[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(app.out, "持续优化 %s  第%d轮  数据截止 %s  候选 %d\n", result.ID, result.Cycle, result.DataCutoff, len(result.Candidates))
	if result.Selected != nil {
		p := result.Selected.Proposal.Parameters
		fmt.Fprintf(app.out, "入选 %s  %s MA%d/%d B%d V%.2f SL%.0f%% TP%.0f%% H%d\n", result.Selected.Proposal.ID, p.EffectiveEntryMode(), p.FastMA, p.SlowMA, p.BreakoutDays, p.VolumeRatioMin, p.StopLoss*100, p.TakeProfit*100, p.MaxHoldingDays)
	}
	if result.Holdout != nil {
		fmt.Fprintf(app.out, "留出收益 %+.2f%%  回撤 %+.2f%%  夏普 %.2f  交易 %d\n", result.Holdout.Metrics.TotalReturn, result.Holdout.Metrics.MaxDrawdown, result.Holdout.Metrics.Sharpe, result.Holdout.Metrics.Trades)
	}
	fmt.Fprintf(app.out, "数据质量 %s（硬门 %v）  候选指纹 %s  配置指纹 %s\n", result.Quality.Grade, result.Quality.Passed, shortHash(result.Manifest.CandidateSetHash), shortHash(result.Manifest.ConfigurationHash))
	if result.Selected != nil {
		fmt.Fprintf(app.out, "Agent共识 %d  邻域候选 %d  邻域均分 %.2f\n", result.Selected.ConsensusAgents, result.Selected.NeighborhoodSize, result.Selected.NeighborhoodScore)
	}
	if result.PriorLessons != "" {
		fmt.Fprintf(app.out, "历史反思：%s\n", result.PriorLessons)
	}
	if len(result.GateReasons) > 0 {
		fmt.Fprintf(app.out, "未晋级：%s\n", strings.Join(result.GateReasons, "；"))
	}
	fmt.Fprintf(app.out, "阶段 %s\n报告 %s\n", result.Stage, result.ReportPath)
	return nil
}
