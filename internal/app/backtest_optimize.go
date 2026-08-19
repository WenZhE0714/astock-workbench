package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func requiredOptimizationPeriod(name, startValue, endValue string) (backtest.Period, error) {
	if strings.TrimSpace(startValue) == "" || strings.TrimSpace(endValue) == "" {
		return backtest.Period{}, fmt.Errorf("必须同时指定 --%s-start 和 --%s-end", name, name)
	}
	start, err := parseBacktestDate(startValue, time.Time{})
	if err != nil {
		return backtest.Period{}, err
	}
	end, err := parseBacktestDate(endValue, time.Time{})
	if err != nil {
		return backtest.Period{}, err
	}
	return backtest.Period{Start: start, End: end}, nil
}

func (app *App) resolveBacktestNames(ctx context.Context, tickers []string) map[string]string {
	names := make(map[string]string, len(tickers))
	missing := false
	for _, ticker := range tickers {
		names[ticker] = app.names.LookupName(ticker)
		missing = missing || names[ticker] == ""
	}
	if missing && app.quotes != nil {
		if quotes, err := app.quotes.Fetch(ctx, tickers); err == nil {
			for _, quote := range quotes {
				if quote.Name != "" {
					names[quote.Symbol] = quote.Name
				}
			}
		}
	}
	for _, ticker := range tickers {
		if names[ticker] == "" {
			names[ticker] = ticker[2:]
		}
	}
	return names
}

func (app *App) runBacktestOptimize(ctx context.Context, arguments []string) error {
	set := flag.NewFlagSet("backtest optimize", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	strategy := set.String("strategy", "technical-breakout", "策略名称")
	strategyVersion := set.String("version", "v1", "策略版本")
	benchmark := set.String("benchmark", "sh000300", "基准股票代码")
	trainStart := set.String("train-start", "", "训练开始日期 YYYY-MM-DD")
	trainEnd := set.String("train-end", "", "训练结束日期 YYYY-MM-DD")
	validateStart := set.String("validate-start", "", "验证开始日期 YYYY-MM-DD")
	validateEnd := set.String("validate-end", "", "验证结束日期 YYYY-MM-DD")
	oosStart := set.String("oos-start", "", "样本外开始日期 YYYY-MM-DD")
	oosEnd := set.String("oos-end", "", "样本外结束日期 YYYY-MM-DD")
	cash := set.Float64("cash", 1000000, "初始资金")
	commission := set.Float64("commission", .0003, "佣金费率")
	minimumCommission := set.Float64("min-commission", 5, "最低佣金")
	stampDuty := set.Float64("stamp-duty", .0005, "卖出印花税率")
	transferFee := set.Float64("transfer-fee", .00001, "过户费率")
	slippageBPS := set.Float64("slippage-bps", 5, "单边滑点，基点")
	maxPosition := set.Float64("max-position", .20, "单股最大仓位")
	fastMA := set.Int("fast-ma", 20, "基线快均线")
	slowMA := set.Int("slow-ma", 60, "基线慢均线")
	breakoutDays := set.Int("breakout-days", 20, "基线突破回看日数")
	volumeRatio := set.Float64("volume-ratio", 1.2, "基线放量门槛")
	stopLoss := set.Float64("stop-loss", .08, "基线止损比例")
	takeProfit := set.Float64("take-profit", .20, "基线止盈比例")
	maxHoldingDays := set.Int("max-holding-days", 40, "基线最大持有交易日")
	maxCandidates := set.Int("max-candidates", backtest.DefaultMaxOptimizationCandidates, "最多候选数量，1-200")
	minimumTrades := set.Int("min-validation-trades", 3, "验证集最低交易笔数")
	maximumDrawdown := set.Float64("max-validation-drawdown", 30, "验证集最大允许回撤百分比")
	maximumGap := set.Float64("max-performance-gap", 40, "训练/验证年化最大差值")
	minimumReturn := set.Float64("min-validation-return", 0, "验证集最低收益百分比")
	liquidate := set.Bool("liquidate", true, "每个区间结束按收盘价估值平仓")
	noAI := set.Bool("no-ai", false, "不调用 Codex 生成只读复盘")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	tickers, err := parseBacktestTickers(ctx, app.resolver, set.Args())
	if err != nil {
		return err
	}
	train, err := requiredOptimizationPeriod("train", *trainStart, *trainEnd)
	if err != nil {
		return err
	}
	validate, err := requiredOptimizationPeriod("validate", *validateStart, *validateEnd)
	if err != nil {
		return err
	}
	outOfSample, err := requiredOptimizationPeriod("oos", *oosStart, *oosEnd)
	if err != nil {
		return err
	}
	base := backtest.Request{
		Strategy: *strategy, StrategyVersion: *strategyVersion, Tickers: tickers,
		Names: app.resolveBacktestNames(ctx, tickers), Start: train.Start, End: train.End,
		InitialCash: *cash, CommissionRate: *commission, MinimumCommission: *minimumCommission,
		StampDutyRate: *stampDuty, TransferFeeRate: *transferFee, SlippageBPS: *slippageBPS,
		Adjustment: backtest.AdjustmentNone, Benchmark: *benchmark, NoFutureData: true,
		PointInTimePool: false, LiquidateAtEnd: *liquidate,
		Technical: backtest.TechnicalParameters{
			FastMA: *fastMA, SlowMA: *slowMA, BreakoutDays: *breakoutDays,
			VolumeRatioMin: *volumeRatio, StopLoss: *stopLoss, TakeProfit: *takeProfit,
			MaxHoldingDays: *maxHoldingDays, MaxPosition: *maxPosition,
		},
	}
	request := backtest.DefaultOptimizationRequest(base, train, validate, outOfSample)
	request.MaxCandidates = *maxCandidates
	request.MinimumValidationTrades = *minimumTrades
	request.MaximumValidationDrawdown = *maximumDrawdown
	request.MaximumPerformanceGap = *maximumGap
	request.MinimumValidationReturn = *minimumReturn
	request.UseAI = !*noAI

	fmt.Fprintf(app.errOut, "策略优化: 正在运行 %d 个训练/验证候选，锁定后仅执行一次样本外检验...\n", request.MaxCandidates)
	provider := backtest.NewCachingDailyBarProvider(market.EastmoneyClient{})
	optimizer := backtest.NewOptimizer(backtest.NewDailyEngine(provider))
	result, err := optimizer.Optimize(ctx, request)
	if err != nil {
		return err
	}
	if request.UseAI {
		result = app.reviewOptimization(ctx, result)
	}
	result, err = app.optimizationStore().Save(result)
	if err != nil {
		return err
	}
	fmt.Fprintf(app.out, "策略优化已完成 %s\n", result.ID)
	if result.Selected == nil {
		fmt.Fprintf(app.out, "候选 %d 个，全部未通过验证门禁；样本外未运行。\n", len(result.Candidates))
	} else {
		selected := result.Selected
		fmt.Fprintf(app.out, "入选 %s  验证评分 %.4f  验证收益 %+.2f%%  回撤 %+.2f%%  交易 %d\n",
			selected.ID, selected.Score, selected.Validate.TotalReturn, selected.Validate.MaxDrawdown, selected.Validate.Trades)
		if result.OutOfSample != nil {
			metrics := result.OutOfSample.Metrics
			fmt.Fprintf(app.out, "样本外收益 %+.2f%%  最大回撤 %+.2f%%  交易 %d  胜率 %.2f%%\n",
				metrics.TotalReturn, metrics.MaxDrawdown, metrics.Trades, metrics.WinRate)
		}
	}
	if result.AIError != "" {
		fmt.Fprintf(app.out, "AI复盘失败（确定性实验已保留）: %s\n", result.AIError)
	}
	fmt.Fprintf(app.out, "报告 %s\n候选 %s\n", result.ReportPath, result.Directory+"/candidates.jsonl")
	return nil
}

func (app *App) reviewOptimization(ctx context.Context, result backtest.OptimizationResult) backtest.OptimizationResult {
	if app.marketReportAI == nil {
		return result
	}
	prompt, err := optimizationReviewPrompt(result)
	if err != nil {
		result.AIError = err.Error()
		return result
	}
	aiContext, cancel := context.WithTimeout(ctx, codexReportTimeout())
	review, err := app.marketReportAI.Synthesize(aiContext, prompt)
	cancel()
	if err != nil {
		result.AIError = err.Error()
		return result
	}
	result.AIReview = review
	return result
}

func optimizationReviewPrompt(result backtest.OptimizationResult) (string, error) {
	facts := struct {
		ID                 string                       `json:"id"`
		Request            backtest.OptimizationRequest `json:"request"`
		Candidates         []backtest.CandidateResult   `json:"candidates"`
		Selected           *backtest.CandidateResult    `json:"selected,omitempty"`
		SelectedTrain      *backtest.Metrics            `json:"selected_train,omitempty"`
		SelectedValidation *backtest.Metrics            `json:"selected_validation,omitempty"`
		OutOfSample        *backtest.Metrics            `json:"out_of_sample,omitempty"`
		Warnings           []string                     `json:"warnings"`
	}{ID: result.ID, Request: result.Request, Candidates: result.Candidates, Selected: result.Selected, Warnings: result.Warnings}
	if result.SelectedTrain != nil {
		facts.SelectedTrain = &result.SelectedTrain.Metrics
	}
	if result.SelectedValidation != nil {
		facts.SelectedValidation = &result.SelectedValidation.Metrics
	}
	if result.OutOfSample != nil {
		facts.OutOfSample = &result.OutOfSample.Metrics
	}
	data, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	return `你是A股量化研究复盘员。不要运行命令、读取文件、搜索网络，也不要修改下面的结构化事实。

硬性要求：
1. 用中文输出简洁Markdown，依次给出“结论”“泛化差距”“参数敏感性”“市场状态与失效场景”“下一轮实验建议”“风险声明”。
2. 候选选择只使用训练和验证；样本外是在参数锁定后一次性执行。不得用样本外结果重新挑选、解释性更换或推荐参数。
3. 引用具体训练、验证和样本外指标；区分相关性与因果，不虚构行情、公司、财务或事件事实。
4. 检查交易笔数、回撤、换手、训练验证差距和邻近参数稳定性。样本不足时明确降低置信度。
5. 下一轮只建议可验证的实验假设，不得直接修改成交、资金曲线、费用、撮合或门禁结果。
6. 不宣称稳定收益、保证盈利或确定性买卖点；明确报告不触发自动交易。

不可变实验事实JSON：
` + string(data), nil
}

func (app *App) optimizationStore() *storage.OptimizationStore {
	root := app.paths.OptimizationsDir
	if root == "" {
		root = app.paths.BacktestsDir + "/optimizations"
	}
	return storage.NewOptimizationStore(root)
}

func (app *App) runBacktestOptimizeList(arguments []string) error {
	set := flag.NewFlagSet("backtest optimize-list", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	limit := set.Int("limit", 20, "最多显示实验数")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	items, err := app.optimizationStore().List(*limit)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintln(app.out, "暂无策略优化实验")
		return nil
	}
	fmt.Fprintln(app.out, "实验ID                   生成时间             候选  入选    验证评分    样本外收益   AI")
	for _, item := range items {
		oos := "--"
		if item.OOSAvailable {
			oos = fmt.Sprintf("%+.2f%%", item.OOSReturn)
		}
		ai := "否"
		if item.AIUsed {
			ai = "是"
		}
		selected := item.SelectedID
		if selected == "" {
			selected = "--"
		}
		fmt.Fprintf(app.out, "%-26s %-20s %4d  %-6s %10.4f  %10s   %s\n",
			item.ID, item.GeneratedAt.Format("2006-01-02 15:04:05"), item.Candidates,
			selected, item.ValidationScore, oos, ai)
	}
	return nil
}

func (app *App) runBacktestOptimizeShow(arguments []string) error {
	if len(arguments) != 1 {
		return fmt.Errorf("用法: astock backtest optimize-show <experiment-id>")
	}
	result, err := app.optimizationStore().Load(arguments[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(app.out, "策略优化 %s  候选 %d\n", result.ID, len(result.Candidates))
	fmt.Fprintf(app.out, "训练 %s~%s  验证 %s~%s  样本外 %s~%s\n",
		result.Request.Train.Start.Format("2006-01-02"), result.Request.Train.End.Format("2006-01-02"),
		result.Request.Validate.Start.Format("2006-01-02"), result.Request.Validate.End.Format("2006-01-02"),
		result.Request.OutOfSample.Start.Format("2006-01-02"), result.Request.OutOfSample.End.Format("2006-01-02"))
	if result.Selected == nil {
		fmt.Fprintln(app.out, "没有候选通过验证门禁，样本外未执行。")
	} else {
		p := result.Selected.Parameters
		fmt.Fprintf(app.out, "入选 %s  MA%d/%d 突破%d日 量比%.2f 止损%.0f%% 止盈%.0f%% 持有%d日 仓位%.0f%%\n",
			result.Selected.ID, p.FastMA, p.SlowMA, p.BreakoutDays, p.VolumeRatioMin,
			p.StopLoss*100, p.TakeProfit*100, p.MaxHoldingDays, p.MaxPosition*100)
		fmt.Fprintf(app.out, "验证评分 %.4f  收益 %+.2f%%  回撤 %+.2f%%  交易 %d\n",
			result.Selected.Score, result.Selected.Validate.TotalReturn,
			result.Selected.Validate.MaxDrawdown, result.Selected.Validate.Trades)
		if result.OutOfSample != nil {
			metrics := result.OutOfSample.Metrics
			fmt.Fprintf(app.out, "样本外收益 %+.2f%%  年化 %+.2f%%  回撤 %+.2f%%  夏普 %.2f  交易 %d\n",
				metrics.TotalReturn, metrics.AnnualizedReturn, metrics.MaxDrawdown, metrics.Sharpe, metrics.Trades)
		}
	}
	if result.AIReview != "" {
		fmt.Fprintln(app.out, "AI复盘 已生成")
	} else if result.AIError != "" {
		fmt.Fprintf(app.out, "AI复盘失败 %s\n", result.AIError)
	}
	fmt.Fprintf(app.out, "报告 %s\n", result.ReportPath)
	return nil
}
