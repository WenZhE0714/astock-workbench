package app

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func (app *App) runPaper(arguments []string) error {
	if len(arguments) == 0 || (len(arguments) == 1 && arguments[0] == "status") {
		fmt.Fprintln(app.out, "模拟盘状态: 领域模型与 Broker/RiskGate 接口已建立，撮合与持仓流水尚未启用。")
		fmt.Fprintln(app.out, "计划约束: A 股 100 股整手、T+1、涨跌停、停牌、手续费与滑点。")
		fmt.Fprintf(app.out, "预留账户文件: %s\n", app.paths.PaperFile)
		return nil
	}
	return fmt.Errorf("用法: astock paper status")
}

func parseBacktestDate(value string, fallback time.Time) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, shanghaiLocation)
	if err != nil {
		return time.Time{}, fmt.Errorf("日期 %q 无效，格式应为 YYYY-MM-DD", value)
	}
	return parsed, nil
}

func parseBacktestTickers(ctx context.Context, resolver *market.Resolver, values []string) ([]string, error) {
	inputs := make([]string, 0)
	for _, value := range values {
		inputs = append(inputs, splitInputs(value)...)
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("至少需要一个股票代码或名称")
	}
	return resolver.ResolveMany(ctx, inputs)
}

func backtestRequestFlags(set *flag.FlagSet) (map[string]*string, map[string]*float64, map[string]*int, map[string]*bool) {
	strings := map[string]*string{
		"strategy":   set.String("strategy", "technical-breakout", "策略名称"),
		"version":    set.String("version", "v1", "策略版本"),
		"start":      set.String("start", "", "开始日期 YYYY-MM-DD"),
		"end":        set.String("end", "", "结束日期 YYYY-MM-DD"),
		"adjustment": set.String("adjustment", "none", "复权口径，目前仅支持 none"),
		"benchmark":  set.String("benchmark", "sh000300", "基准股票代码"),
	}
	floats := map[string]*float64{
		"cash":           set.Float64("cash", 1000000, "初始资金"),
		"commission":     set.Float64("commission", 0.0003, "佣金费率"),
		"min-commission": set.Float64("min-commission", 5, "最低佣金"),
		"stamp-duty":     set.Float64("stamp-duty", 0.0005, "卖出印花税率"),
		"transfer-fee":   set.Float64("transfer-fee", 0.00001, "过户费率"),
		"slippage-bps":   set.Float64("slippage-bps", 5, "滑点，基点"),
		"volume-ratio":   set.Float64("volume-ratio", 1.2, "放量倍数门槛"),
		"stop-loss":      set.Float64("stop-loss", 0.08, "止损比例"),
		"take-profit":    set.Float64("take-profit", 0.20, "止盈比例"),
		"max-position":   set.Float64("max-position", 0.20, "单股最大仓位"),
	}
	ints := map[string]*int{
		"fast-ma":          set.Int("fast-ma", 20, "快均线周期"),
		"slow-ma":          set.Int("slow-ma", 60, "慢均线周期"),
		"breakout-days":    set.Int("breakout-days", 20, "突破回看天数"),
		"max-holding-days": set.Int("max-holding-days", 40, "最大持有交易日"),
	}
	bools := map[string]*bool{
		"liquidate": set.Bool("liquidate", true, "回测结束强制平仓"),
	}
	return strings, floats, ints, bools
}

func (app *App) runBacktest(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 || (len(arguments) == 1 && arguments[0] == "status") {
		fmt.Fprintln(app.out, "回测状态: 日线回测引擎已启用，交易流水和资金曲线会落盘。")
		fmt.Fprintln(app.out, "默认策略: 放量突破 + 快慢均线；成交安排为信号次日开盘，遵守 T+1 与 100 股整手。")
		fmt.Fprintf(app.out, "回测目录: %s\n", app.paths.BacktestsDir)
		return nil
	}
	switch arguments[0] {
	case "run":
		return app.runBacktestRun(ctx, arguments[1:])
	case "list", "ls":
		return app.runBacktestList(arguments[1:])
	case "show":
		return app.runBacktestShow(arguments[1:])
	case "trades":
		return app.runBacktestTrades(arguments[1:])
	case "trade":
		return app.runBacktestTrade(arguments[1:])
	case "optimize":
		return app.runBacktestOptimize(ctx, arguments[1:])
	case "optimize-list":
		return app.runBacktestOptimizeList(arguments[1:])
	case "optimize-show":
		return app.runBacktestOptimizeShow(arguments[1:])
	case "continuous", "auto-optimize":
		return app.runBacktestContinuous(ctx, arguments[1:])
	case "continuous-list":
		return app.runBacktestContinuousList(arguments[1:])
	case "continuous-show":
		return app.runBacktestContinuousShow(arguments[1:])
	default:
		return fmt.Errorf("用法: astock backtest [status | run | list | show | trades | trade | optimize | optimize-list | optimize-show | continuous | continuous-list | continuous-show]")
	}
}

func (app *App) runBacktestRun(ctx context.Context, arguments []string) error {
	set := flag.NewFlagSet("backtest run", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	stringFlags, floatFlags, intFlags, boolFlags := backtestRequestFlags(set)
	if err := set.Parse(arguments); err != nil {
		return err
	}
	tickers, err := parseBacktestTickers(ctx, app.resolver, set.Args())
	if err != nil {
		return err
	}
	now := time.Now().In(shanghaiLocation)
	start, err := parseBacktestDate(*stringFlags["start"], now.AddDate(-3, 0, 0))
	if err != nil {
		return err
	}
	end, err := parseBacktestDate(*stringFlags["end"], now.AddDate(0, 0, -1))
	if err != nil {
		return err
	}
	if start.After(end) {
		return fmt.Errorf("开始日期不能晚于结束日期")
	}
	adjustment := backtest.PriceAdjustment(strings.ToLower(strings.TrimSpace(*stringFlags["adjustment"])))
	if adjustment == "" {
		adjustment = backtest.AdjustmentNone
	}
	parameters := backtest.TechnicalParameters{
		FastMA: *intFlags["fast-ma"], SlowMA: *intFlags["slow-ma"], BreakoutDays: *intFlags["breakout-days"],
		VolumeRatioMin: *floatFlags["volume-ratio"], StopLoss: *floatFlags["stop-loss"], TakeProfit: *floatFlags["take-profit"],
		MaxHoldingDays: *intFlags["max-holding-days"], MaxPosition: *floatFlags["max-position"],
	}
	names := make(map[string]string, len(tickers))
	missingNames := false
	for _, ticker := range tickers {
		names[ticker] = app.names.LookupName(ticker)
		if names[ticker] == "" {
			missingNames = true
		}
	}
	if missingNames && app.quotes != nil {
		if quotes, quoteError := app.quotes.Fetch(ctx, tickers); quoteError == nil {
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
	request := backtest.Request{
		Strategy: *stringFlags["strategy"], StrategyVersion: *stringFlags["version"], Tickers: tickers, Names: names,
		Start: start, End: end, InitialCash: *floatFlags["cash"], CommissionRate: *floatFlags["commission"],
		MinimumCommission: *floatFlags["min-commission"], StampDutyRate: *floatFlags["stamp-duty"], TransferFeeRate: *floatFlags["transfer-fee"],
		SlippageBPS: *floatFlags["slippage-bps"], Adjustment: adjustment, Benchmark: *stringFlags["benchmark"],
		NoFutureData: true, PointInTimePool: false, LiquidateAtEnd: *boolFlags["liquidate"], Technical: parameters,
	}
	fmt.Fprintln(app.errOut, "回测: 正在获取历史日K并执行模拟...")
	engine := backtest.NewDailyEngine(market.EastmoneyClient{})
	result, err := engine.Run(ctx, request)
	if err != nil {
		return err
	}
	store := storage.NewBacktestStore(app.paths.BacktestsDir)
	result, err = store.Save(result)
	if err != nil {
		return err
	}
	fmt.Fprintf(app.out, "回测已完成 %s\n", result.RunID)
	fmt.Fprintf(app.out, "收益率 %+.2f%%  最大回撤 %+.2f%%  交易 %d 笔  胜率 %.2f%%\n", result.Metrics.TotalReturn, result.Metrics.MaxDrawdown, result.Metrics.Trades, result.Metrics.WinRate)
	fmt.Fprintf(app.out, "报告    %s\n交易流水 %s\n资金曲线 %s\n", result.ReportPath, result.Directory+"/trades.jsonl", result.Directory+"/equity.csv")
	return nil
}

func (app *App) backtestStore() *storage.BacktestStore {
	return storage.NewBacktestStore(app.paths.BacktestsDir)
}

func (app *App) runBacktestList(arguments []string) error {
	set := flag.NewFlagSet("backtest list", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	limit := set.Int("limit", 20, "最多显示运行次数")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	items, err := app.backtestStore().List(*limit)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintln(app.out, "暂无回测记录")
		return nil
	}
	fmt.Fprintln(app.out, "运行ID                 生成时间             策略              收益       最大回撤    交易")
	for _, item := range items {
		fmt.Fprintf(app.out, "%-22s %-20s %-17s %+.2f%%  %+.2f%%  %d\n", item.RunID, item.GeneratedAt.Format("2006-01-02 15:04:05"), item.Strategy, item.TotalReturn, item.MaxDrawdown, item.Trades)
	}
	return nil
}

func (app *App) runBacktestShow(arguments []string) error {
	if len(arguments) != 1 {
		return fmt.Errorf("用法: astock backtest show <run-id>")
	}
	result, err := app.backtestStore().Load(arguments[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(app.out, "回测 %s · %s\n", result.RunID, result.Request.Strategy)
	fmt.Fprintf(app.out, "区间 %s 至 %s  股票 %s  口径 %s\n", result.Request.Start.Format("2006-01-02"), result.Request.End.Format("2006-01-02"), strings.Join(result.Request.Tickers, ","), result.Request.Adjustment)
	fmt.Fprintf(app.out, "收益率 %+.2f%%  年化 %+.2f%%  最大回撤 %+.2f%%  夏普 %.2f", result.Metrics.TotalReturn, result.Metrics.AnnualizedReturn, result.Metrics.MaxDrawdown, result.Metrics.Sharpe)
	if result.Metrics.BenchmarkAvailable {
		fmt.Fprintf(app.out, "  基准 %+.2f%%  超额 %+.2f%%", result.Metrics.BenchmarkReturn, result.Metrics.ExcessReturn)
	} else if result.Request.Benchmark != "" {
		fmt.Fprint(app.out, "  基准 --")
	}
	fmt.Fprintln(app.out)
	fmt.Fprintf(app.out, "交易 %d  胜率 %.2f%%  盈亏比 %.2f  平均持有 %.1f天  费用 %.2f\n", result.Metrics.Trades, result.Metrics.WinRate, result.Metrics.ProfitFactor, result.Metrics.AverageHoldingDays, result.Metrics.TotalFees)
	fmt.Fprintf(app.out, "报告 %s\n", result.ReportPath)
	return nil
}

func (app *App) runBacktestTrades(arguments []string) error {
	if len(arguments) != 1 {
		return fmt.Errorf("用法: astock backtest trades <run-id>")
	}
	result, err := app.backtestStore().Load(arguments[0])
	if err != nil {
		return err
	}
	if len(result.Trades) == 0 {
		fmt.Fprintln(app.out, "该回测没有已平仓交易；可查看 show 了解未平仓")
		return nil
	}
	fmt.Fprintln(app.out, "ID     股票             买入日期    买入价    卖出日期    卖出价      净收益    收益率   持有  退出原因")
	for _, trade := range result.Trades {
		fmt.Fprintf(app.out, "%-6s %-16s %-10s %8.2f  %-10s %8.2f  %+.2f  %+.2f%%  %3d  %s\n", trade.ID, trade.Symbol[2:]+" "+trade.Name, trade.Entry.Date, trade.Entry.Price, trade.Exit.Date, trade.Exit.Price, trade.NetProfit, trade.ReturnPercent, trade.HoldingDays, trade.ExitReason)
	}
	return nil
}

func (app *App) runBacktestTrade(arguments []string) error {
	if len(arguments) != 2 {
		return fmt.Errorf("用法: astock backtest trade <run-id> <trade-id>")
	}
	result, err := app.backtestStore().Load(arguments[0])
	if err != nil {
		return err
	}
	for _, trade := range result.Trades {
		if trade.ID != arguments[1] {
			continue
		}
		fmt.Fprintf(app.out, "%s %s %s · %s\n\n", trade.ID, trade.Symbol[2:], trade.Name, trade.Strategy)
		fmt.Fprintf(app.out, "买入信号 %s：收盘 %.2f，MA%d %.2f，MA%d %.2f，前高 %.2f，量比 %.2f\n", trade.EntrySignal.Date, trade.EntrySignal.Close, result.Request.Technical.FastMA, trade.EntrySignal.FastMA, result.Request.Technical.SlowMA, trade.EntrySignal.SlowMA, trade.EntrySignal.PriorHigh, trade.EntrySignal.VolumeRatio)
		fmt.Fprintf(app.out, "买入理由：%s\n", strings.Join(trade.EntrySignal.Reasons, "；"))
		fmt.Fprintf(app.out, "实际成交：%s 开盘 %.2f → %.2f，%d股，成交额 %.2f，费用 %.2f\n\n", trade.Entry.Date, trade.Entry.RawPrice, trade.Entry.Price, trade.Entry.Quantity, trade.Entry.Amount, trade.Entry.TotalFee)
		fmt.Fprintf(app.out, "卖出信号 %s：%s\n", trade.ExitSignal.Date, strings.Join(trade.ExitSignal.Reasons, "；"))
		fmt.Fprintf(app.out, "实际成交：%s 开盘 %.2f → %.2f，成交额 %.2f，费用 %.2f\n", trade.Exit.Date, trade.Exit.RawPrice, trade.Exit.Price, trade.Exit.Amount, trade.Exit.TotalFee)
		fmt.Fprintf(app.out, "净收益：%+.2f（%+.2f%%）  最大浮盈：%+.2f%%  最大浮亏：%+.2f%%\n", trade.NetProfit, trade.ReturnPercent, trade.MaxFavorable, trade.MaxAdverse)
		return nil
	}
	return fmt.Errorf("未找到交易记录 %s", arguments[1])
}
