package app

import "fmt"

func (app *App) runPaper(arguments []string) error {
	if len(arguments) == 0 || (len(arguments) == 1 && arguments[0] == "status") {
		fmt.Fprintln(app.out, "模拟盘状态: 领域模型与 Broker/RiskGate 接口已建立，撮合与持仓流水尚未启用。")
		fmt.Fprintln(app.out, "计划约束: A 股 100 股整手、T+1、涨跌停、停牌、手续费与滑点。")
		fmt.Fprintf(app.out, "预留账户文件: %s\n", app.paths.PaperFile)
		return nil
	}
	return fmt.Errorf("用法: astock paper status")
}

func (app *App) runBacktest(arguments []string) error {
	if len(arguments) == 0 || (len(arguments) == 1 && arguments[0] == "status") {
		fmt.Fprintln(app.out, "回测状态: Engine/Request/Metrics 接口已建立，历史数据适配器尚未启用。")
		fmt.Fprintln(app.out, "强制设计项: 复权口径固定、禁止未来函数、点时股票池、手续费/滑点和基准对照。")
		fmt.Fprintln(app.out, "下一阶段可接入 TradingAgents 已有 Backtrader 依赖或独立 Python 回测服务。")
		return nil
	}
	return fmt.Errorf("用法: astock backtest status")
}
