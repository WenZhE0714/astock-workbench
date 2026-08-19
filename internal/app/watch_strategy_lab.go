package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/ui"
)

type strategyLabView string

const (
	strategyLabMenu                strategyLabView = "menu"
	strategyLabSettings            strategyLabView = "settings"
	strategyLabBacktestHistory     strategyLabView = "backtest-history"
	strategyLabBacktestDetail      strategyLabView = "backtest-detail"
	strategyLabTradeDetail         strategyLabView = "trade-detail"
	strategyLabOptimizationHistory strategyLabView = "optimization-history"
	strategyLabOptimizationDetail  strategyLabView = "optimization-detail"
	strategyLabContinuousHistory   strategyLabView = "continuous-history"
	strategyLabContinuousDetail    strategyLabView = "continuous-detail"
)

type strategyLabScope int

const (
	strategyLabCurrentStock strategyLabScope = iota
	strategyLabCurrentList
)

type strategyLabSplit struct {
	name          string
	trainYears    int
	validateYears int
	oosYears      int
}

var strategyLabSplits = []strategyLabSplit{
	{name: "快速 3/1/1年", trainYears: 3, validateYears: 1, oosYears: 1},
	{name: "标准 4/2/2年", trainYears: 4, validateYears: 2, oosYears: 2},
	{name: "长周期 6/3/2年", trainYears: 6, validateYears: 3, oosYears: 2},
}

var strategyLabBacktestYears = []int{1, 3, 5}
var strategyLabCandidateCounts = []int{10, 20, 30, 50}
var strategyLabMinimumTrades = []int{0, 3, 5, 10}

type watchStrategyLab struct {
	viewing  bool
	view     strategyLabView
	selected int

	symbol       string
	name         string
	groupName    string
	groupSymbols []string
	names        map[string]string

	scope              strategyLabScope
	backtestYearsIndex int
	splitIndex         int
	candidateIndex     int
	minimumTradesIndex int
	useAI              bool

	running  bool
	taskKind string
	progress string
	unread   bool
	error    string

	backtests     []storage.BacktestIndexEntry
	optimizations []storage.OptimizationIndexEntry
	continuous    []storage.ContinuousOptimizationIndexEntry
	run           *backtest.Result
	trade         *backtest.Trade
	optimization  *backtest.OptimizationResult
	continuousRun *backtest.ContinuousOptimizationResult
}

func newWatchStrategyLab() watchStrategyLab {
	return watchStrategyLab{
		view: strategyLabMenu, backtestYearsIndex: 1, splitIndex: 1,
		candidateIndex: 2, minimumTradesIndex: 1, useAI: true,
	}
}

func isStrategyLabShortcut(event terminalEvent, modalActive, viewing bool) bool {
	return !modalActive && !viewing && event.Key == terminalKeyNone && (event.Text == "t" || event.Text == "T")
}

func (state *watchStrategyLab) updateContext(symbol, name, groupName string, groupSymbols []string, names map[string]string) {
	state.symbol = symbol
	state.name = name
	state.groupName = groupName
	state.groupSymbols = append([]string(nil), groupSymbols...)
	state.names = make(map[string]string, len(names))
	for key, value := range names {
		state.names[key] = value
	}
}

func (state *watchStrategyLab) open() {
	state.viewing = true
	state.selected = 0
	if state.unread {
		state.unread = false
		switch state.taskKind {
		case "backtest":
			if state.run != nil {
				state.view = strategyLabBacktestDetail
				return
			}
		case "optimization":
			if state.optimization != nil {
				state.view = strategyLabOptimizationDetail
				return
			}
		case "continuous":
			if state.continuousRun != nil {
				state.view = strategyLabContinuousDetail
				return
			}
		}
	}
	state.view = strategyLabMenu
}

func (state *watchStrategyLab) close() {
	state.viewing = false
	state.view = strategyLabMenu
	state.selected = 0
}

func (state *watchStrategyLab) back() bool {
	switch state.view {
	case strategyLabMenu:
		state.close()
		return false
	case strategyLabTradeDetail:
		state.view = strategyLabBacktestDetail
		state.trade = nil
	case strategyLabBacktestDetail:
		if len(state.backtests) > 0 {
			state.view = strategyLabBacktestHistory
		} else {
			state.view = strategyLabMenu
		}
	case strategyLabOptimizationDetail:
		if len(state.optimizations) > 0 {
			state.view = strategyLabOptimizationHistory
		} else {
			state.view = strategyLabMenu
		}
	case strategyLabContinuousDetail:
		if len(state.continuous) > 0 {
			state.view = strategyLabContinuousHistory
		} else {
			state.view = strategyLabMenu
		}
	default:
		state.view = strategyLabMenu
	}
	state.selected = 0
	return true
}

func (state *watchStrategyLab) itemCount() int {
	switch state.view {
	case strategyLabMenu:
		return 7
	case strategyLabSettings:
		return 7
	case strategyLabBacktestHistory:
		return len(state.backtests)
	case strategyLabBacktestDetail:
		if state.run != nil {
			return len(state.run.Trades)
		}
	case strategyLabOptimizationHistory:
		return len(state.optimizations)
	case strategyLabContinuousHistory:
		return len(state.continuous)
	}
	return 0
}

func (state *watchStrategyLab) move(delta int) {
	count := state.itemCount()
	if count == 0 {
		state.selected = 0
		return
	}
	state.selected += delta
	if state.selected < 0 {
		state.selected = 0
	}
	if state.selected >= count {
		state.selected = count - 1
	}
}

func (state *watchStrategyLab) selectEndpoint(end bool) {
	if end {
		state.selected = state.itemCount() - 1
	} else {
		state.selected = 0
	}
	state.move(0)
}

func (state *watchStrategyLab) cycleSetting() {
	switch state.selected {
	case 0:
		if state.scope == strategyLabCurrentStock {
			state.scope = strategyLabCurrentList
		} else {
			state.scope = strategyLabCurrentStock
		}
	case 1:
		state.backtestYearsIndex = (state.backtestYearsIndex + 1) % len(strategyLabBacktestYears)
	case 2:
		state.splitIndex = (state.splitIndex + 1) % len(strategyLabSplits)
	case 3:
		state.candidateIndex = (state.candidateIndex + 1) % len(strategyLabCandidateCounts)
	case 4:
		state.minimumTradesIndex = (state.minimumTradesIndex + 1) % len(strategyLabMinimumTrades)
	case 5:
		state.useAI = !state.useAI
	case 6:
		defaults := newWatchStrategyLab()
		state.scope = defaults.scope
		state.backtestYearsIndex = defaults.backtestYearsIndex
		state.splitIndex = defaults.splitIndex
		state.candidateIndex = defaults.candidateIndex
		state.minimumTradesIndex = defaults.minimumTradesIndex
		state.useAI = defaults.useAI
	}
}

func (state watchStrategyLab) scopeText() string {
	if state.scope == strategyLabCurrentList {
		return "当前列表"
	}
	return "当前股票"
}

func (state watchStrategyLab) targets() ([]string, map[string]string, error) {
	var symbols []string
	if state.scope == strategyLabCurrentList {
		symbols = append(symbols, state.groupSymbols...)
	} else if state.symbol != "" {
		symbols = []string{state.symbol}
	}
	if len(symbols) == 0 {
		return nil, nil, fmt.Errorf("当前范围没有可研究的股票")
	}
	names := make(map[string]string, len(symbols))
	for _, symbol := range symbols {
		name := state.names[symbol]
		if name == "" && symbol == state.symbol {
			name = state.name
		}
		if name == "" && len(symbol) > 2 {
			name = symbol[2:]
		}
		names[symbol] = name
	}
	return symbols, names, nil
}

func strategyLabBacktestPeriod(end time.Time, years int) backtest.Period {
	end = dateOnly(end)
	return backtest.Period{Start: end.AddDate(-years, 0, 1), End: end}
}

func strategyLabOptimizationPeriods(end time.Time, split strategyLabSplit) (backtest.Period, backtest.Period, backtest.Period) {
	end = dateOnly(end)
	oos := backtest.Period{Start: end.AddDate(-split.oosYears, 0, 1), End: end}
	validateEnd := oos.Start.AddDate(0, 0, -1)
	validate := backtest.Period{Start: validateEnd.AddDate(-split.validateYears, 0, 1), End: validateEnd}
	trainEnd := validate.Start.AddDate(0, 0, -1)
	train := backtest.Period{Start: trainEnd.AddDate(-split.trainYears, 0, 1), End: trainEnd}
	return train, validate, oos
}

func dateOnly(value time.Time) time.Time {
	local := value.In(shanghaiLocation)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, shanghaiLocation)
}

func defaultResearchRequest(symbols []string, names map[string]string, period backtest.Period) backtest.Request {
	return backtest.Request{
		Strategy: "technical-breakout", StrategyVersion: "v1", Tickers: append([]string(nil), symbols...),
		Names: names, Start: period.Start, End: period.End, InitialCash: 1000000,
		CommissionRate: .0003, MinimumCommission: 5, StampDutyRate: .0005,
		TransferFeeRate: .00001, SlippageBPS: 5, Adjustment: backtest.AdjustmentNone,
		Benchmark: "sh000300", NoFutureData: true, PointInTimePool: false,
		LiquidateAtEnd: true, Technical: backtest.DefaultTechnicalParameters(),
	}
}

func (state *watchStrategyLab) begin(kind, progress string) {
	state.running = true
	state.taskKind = kind
	state.progress = progress
	state.unread = false
	state.error = ""
	state.viewing = false
}

func (state *watchStrategyLab) completeBacktest(result backtest.Result) {
	state.running = false
	state.taskKind = "backtest"
	state.progress = ""
	state.error = ""
	state.unread = true
	state.run = &result
	state.optimization = nil
	state.continuousRun = nil
}

func (state *watchStrategyLab) completeOptimization(result backtest.OptimizationResult) {
	state.running = false
	state.taskKind = "optimization"
	state.progress = ""
	state.error = ""
	state.unread = true
	state.optimization = &result
	state.run = nil
	state.continuousRun = nil
}

func (state *watchStrategyLab) completeContinuous(result backtest.ContinuousOptimizationResult) {
	state.running = false
	state.taskKind = "continuous"
	state.progress = ""
	state.error = ""
	state.unread = true
	state.continuousRun = &result
	state.optimization = nil
	state.run = nil
}

func (state *watchStrategyLab) fail(err error) {
	state.running = false
	state.progress = ""
	state.unread = false
	if err != nil {
		state.error = err.Error()
	}
}

func (state watchStrategyLab) status(moyu bool) string {
	if state.running {
		progress := strings.TrimSpace(state.progress)
		if progress == "" {
			progress = "执行历史模拟"
		}
		if moyu {
			return "STRATEGY LAB: " + strings.ToUpper(progress) + " | LIVE QUOTES CONTINUE"
		}
		return "策略研究后台运行中：" + progress + " · 行情继续刷新"
	}
	if state.unread {
		if moyu {
			return "STRATEGY LAB RESULT READY | PRESS T TO OPEN"
		}
		return "策略研究已完成，按 t 查看结果"
	}
	if state.error != "" {
		if moyu {
			return "STRATEGY LAB FAILED: " + state.error + " | PRESS T"
		}
		return "策略研究失败：" + state.error + "；按 t 查看或重试"
	}
	return ""
}

func (state watchStrategyLab) controls(moyu bool) string {
	if state.view == strategyLabTradeDetail || state.view == strategyLabOptimizationDetail || state.view == strategyLabContinuousDetail {
		if moyu {
			return "UP/DOWN SCROLL  [/] PAGE  G/G ENDPOINTS  ESC BACK  Q QUIT"
		}
		return "↑/↓滚动  [/]翻页  g/G首尾  Esc返回  q退出"
	}
	if state.view == strategyLabSettings {
		if moyu {
			return "UP/DOWN SELECT  ENTER CHANGE  ESC BACK  Q QUIT"
		}
		return "↑/↓选择  Enter切换  Esc返回  q退出"
	}
	if moyu {
		return "UP/DOWN SELECT  [/]/PGUP/PGDN JUMP  ENTER OPEN/RUN  ESC BACK  Q QUIT"
	}
	return "↑/↓选择  [/]跳选  Enter进入/运行  Esc返回  q退出"
}

func strategyLabCode(symbol string) string {
	if len(symbol) == 8 {
		return symbol[2:]
	}
	return symbol
}

func strategyLabDate(period backtest.Period) string {
	return period.Start.Format("2006-01-02") + " 至 " + period.End.Format("2006-01-02")
}

func (state watchStrategyLab) contextText(now time.Time) string {
	target := strategyLabCode(state.symbol)
	if state.name != "" {
		target += " " + state.name
	}
	if target == "" {
		target = "未选择股票"
	}
	symbols, _, err := state.targets()
	count := 0
	if err == nil {
		count = len(symbols)
	}
	end := dateOnly(now).AddDate(0, 0, -1)
	backtestPeriod := strategyLabBacktestPeriod(end, strategyLabBacktestYears[state.backtestYearsIndex])
	train, validate, oos := strategyLabOptimizationPeriods(end, strategyLabSplits[state.splitIndex])
	ai := "开启"
	if !state.useAI {
		ai = "关闭"
	}
	return fmt.Sprintf(
		"当前：%s  ·  范围：%s（%d只）  ·  来源：%s\n单次回测：%s  ·  优化：训练 %s / 验证 %s / 样本外 %s\n候选：%d  ·  验证最低交易：%d  ·  AI只读复盘：%s",
		target, state.scopeText(), count, state.groupName, strategyLabDate(backtestPeriod), strategyLabDate(train),
		strategyLabDate(validate), strategyLabDate(oos), strategyLabCandidateCounts[state.candidateIndex],
		strategyLabMinimumTrades[state.minimumTradesIndex], ai,
	)
}

func (state watchStrategyLab) frame(now time.Time, width int, moyu, color bool) string {
	data := ui.StrategyLabFrame{
		Context: state.contextText(now), Controls: state.controls(moyu), Selected: state.selected,
	}
	switch state.view {
	case strategyLabMenu:
		data.Status = "统一入口：运行任务、修改设置、查看历史和逐笔买卖记录。历史模拟不触发自动交易。"
		if state.running {
			data.Status = "后台运行中：" + state.progress + "。行情在后台继续刷新，Esc 可返回看盘。"
			data.StatusTone = "running"
		} else if state.error != "" {
			data.Status = "最近任务失败：" + state.error
			data.StatusTone = "error"
		}
		data.Items = []string{
			"运行单次回测",
			"运行训练/验证/样本外优化",
			"运行多Agent持续优化",
			"研究设置",
			"单次回测历史",
			"策略优化历史",
			"持续优化历史",
		}
	case strategyLabSettings:
		data.StatusTone = "warning"
		ai := "开启"
		if !state.useAI {
			ai = "关闭"
		}
		data.Status = "Enter 切换当前设置；日期区间以最近完整日线（昨天）为终点。"
		data.Items = []string{
			"研究范围：" + state.scopeText(),
			fmt.Sprintf("单次回测区间：最近 %d 年", strategyLabBacktestYears[state.backtestYearsIndex]),
			"优化日期方案：" + strategyLabSplits[state.splitIndex].name,
			fmt.Sprintf("优化候选数量：%d", strategyLabCandidateCounts[state.candidateIndex]),
			fmt.Sprintf("验证最低交易数：%d", strategyLabMinimumTrades[state.minimumTradesIndex]),
			"Codex只读复盘：" + ai,
			"恢复默认设置",
		}
	case strategyLabBacktestHistory:
		data.Status = fmt.Sprintf("单次回测历史：%d 条；Enter 查看指标和逐笔交易。", len(state.backtests))
		for _, item := range state.backtests {
			data.Items = append(data.Items, fmt.Sprintf(
				"%s  %s  收益 %+.2f%%  回撤 %+.2f%%  交易 %d",
				item.RunID, item.GeneratedAt.Format("01-02 15:04"), item.TotalReturn, item.MaxDrawdown, item.Trades,
			))
		}
		if len(data.Items) == 0 {
			data.Body = "暂无单次回测历史。"
		}
		data.Items, data.Selected = strategyLabVisibleItems(data.Items, state.selected, 15)
	case strategyLabBacktestDetail:
		data.StatusTone = "success"
		if state.run == nil {
			data.Body = "回测详情不可用。"
			break
		}
		run := state.run
		benchmark := ""
		if run.Metrics.BenchmarkAvailable {
			benchmark = fmt.Sprintf("  基准 %+.2f%%  超额 %+.2f%%", run.Metrics.BenchmarkReturn, run.Metrics.ExcessReturn)
		}
		data.Status = fmt.Sprintf("回测 %s  ·  %s  ·  %s", run.RunID, run.Request.Strategy, strategyLabDate(backtest.Period{Start: run.Request.Start, End: run.Request.End}))
		data.Body = fmt.Sprintf(
			"收益 %+.2f%%  年化 %+.2f%%  回撤 %+.2f%%  夏普 %.2f%s\n胜率 %.2f%%  交易 %d  费用 %.2f  报告 %s",
			run.Metrics.TotalReturn, run.Metrics.AnnualizedReturn, run.Metrics.MaxDrawdown,
			run.Metrics.Sharpe, benchmark, run.Metrics.WinRate, run.Metrics.Trades,
			run.Metrics.TotalFees, run.ReportPath,
		)
		for _, trade := range run.Trades {
			data.Items = append(data.Items, fmt.Sprintf(
				"%s  %s %s  %s %.2f → %s %.2f  %+.2f%%  %s",
				trade.ID, strategyLabCode(trade.Symbol), trade.Name, trade.Entry.Date, trade.Entry.Price,
				trade.Exit.Date, trade.Exit.Price, trade.ReturnPercent, trade.ExitReason,
			))
		}
		if len(run.Trades) > 0 {
			data.Status += "  ·  Enter 查看所选交易证据"
		}
		data.Items, data.Selected = strategyLabVisibleItems(data.Items, state.selected, 15)
	case strategyLabTradeDetail:
		if state.trade == nil || state.run == nil {
			data.Body = "交易详情不可用。"
			break
		}
		trade := state.trade
		data.Status = fmt.Sprintf("%s  %s %s  ·  %s", trade.ID, strategyLabCode(trade.Symbol), trade.Name, trade.Strategy)
		data.Body = fmt.Sprintf(
			"## 买入\n信号 %s：%s\n收盘 %.2f  快线 %.2f  慢线 %.2f  前高 %.2f  量比 %.2f\n成交 %s：开盘 %.2f → 滑点后 %.2f，%d股，费用 %.2f\n\n## 卖出\n信号 %s：%s\n成交 %s：开盘 %.2f → 滑点后 %.2f，费用 %.2f\n\n净收益 %+.2f（%+.2f%%）  持有 %d日  最大浮盈 %+.2f%%  最大浮亏 %+.2f%%",
			trade.EntrySignal.Date, strings.Join(trade.EntrySignal.Reasons, "；"), trade.EntrySignal.Close,
			trade.EntrySignal.FastMA, trade.EntrySignal.SlowMA, trade.EntrySignal.PriorHigh,
			trade.EntrySignal.VolumeRatio, trade.Entry.Date, trade.Entry.RawPrice, trade.Entry.Price,
			trade.Entry.Quantity, trade.Entry.TotalFee, trade.ExitSignal.Date,
			strings.Join(trade.ExitSignal.Reasons, "；"), trade.Exit.Date, trade.Exit.RawPrice,
			trade.Exit.Price, trade.Exit.TotalFee, trade.NetProfit, trade.ReturnPercent,
			trade.HoldingDays, trade.MaxFavorable, trade.MaxAdverse,
		)
	case strategyLabOptimizationHistory:
		data.Status = fmt.Sprintf("策略优化历史：%d 条；Enter 查看入选参数、验证和样本外结果。", len(state.optimizations))
		for _, item := range state.optimizations {
			oos := "样本外 --"
			if item.OOSAvailable {
				oos = fmt.Sprintf("样本外 %+.2f%%", item.OOSReturn)
			}
			selected := item.SelectedID
			if selected == "" {
				selected = "未入选"
			}
			data.Items = append(data.Items, fmt.Sprintf(
				"%s  %s  候选%d  %s  评分 %.2f  %s",
				item.ID, item.GeneratedAt.Format("01-02 15:04"), item.Candidates,
				selected, item.ValidationScore, oos,
			))
		}
		if len(data.Items) == 0 {
			data.Body = "暂无策略优化历史。"
		}
		data.Items, data.Selected = strategyLabVisibleItems(data.Items, state.selected, 15)
	case strategyLabOptimizationDetail:
		data.StatusTone = "success"
		if state.optimization == nil {
			data.Body = "优化详情不可用。"
			break
		}
		result := state.optimization
		data.Status = fmt.Sprintf("优化实验 %s  ·  候选 %d", result.ID, len(result.Candidates))
		var builder strings.Builder
		fmt.Fprintf(&builder, "训练 %s\n验证 %s\n样本外 %s\n\n", strategyLabDate(result.Request.Train), strategyLabDate(result.Request.Validate), strategyLabDate(result.Request.OutOfSample))
		if result.Selected == nil {
			builder.WriteString("没有候选通过验证门禁，样本外未执行。\n")
		} else {
			p := result.Selected.Parameters
			fmt.Fprintf(&builder, "入选 %s：MA%d/%d  突破%d日  量比%.2f  止损%.0f%%  止盈%.0f%%  持有%d日  仓位%.0f%%\n", result.Selected.ID, p.FastMA, p.SlowMA, p.BreakoutDays, p.VolumeRatioMin, p.StopLoss*100, p.TakeProfit*100, p.MaxHoldingDays, p.MaxPosition*100)
			fmt.Fprintf(&builder, "训练收益 %+.2f%% / 回撤 %+.2f%% / 交易 %d\n验证收益 %+.2f%% / 回撤 %+.2f%% / 交易 %d / 评分 %.2f\n", result.Selected.Train.TotalReturn, result.Selected.Train.MaxDrawdown, result.Selected.Train.Trades, result.Selected.Validate.TotalReturn, result.Selected.Validate.MaxDrawdown, result.Selected.Validate.Trades, result.Selected.Score)
			if result.OutOfSample != nil {
				metrics := result.OutOfSample.Metrics
				fmt.Fprintf(&builder, "样本外收益 %+.2f%% / 年化 %+.2f%% / 回撤 %+.2f%% / 夏普 %.2f / 交易 %d / 胜率 %.2f%%\n", metrics.TotalReturn, metrics.AnnualizedReturn, metrics.MaxDrawdown, metrics.Sharpe, metrics.Trades, metrics.WinRate)
			}
		}
		if result.AIReview != "" {
			builder.WriteString("\n## AI只读复盘\n")
			builder.WriteString(strings.TrimSpace(result.AIReview))
		} else if result.AIError != "" {
			fmt.Fprintf(&builder, "\nAI复盘失败：%s\n", result.AIError)
		}
		builder.WriteString("\n## 候选排名\n")
		limit := min(20, len(result.Candidates))
		for index, candidate := range result.Candidates[:limit] {
			status := "通过"
			if candidate.Rejected {
				status = "拒绝：" + strings.Join(candidate.Reasons, "；")
			}
			p := candidate.Parameters
			fmt.Fprintf(&builder, "%d. %s  %s  评分 %.2f  训练 %+.2f%%  验证 %+.2f%%  回撤 %+.2f%%  交易%d  MA%d/%d B%d V%.1f SL%.0f%% TP%.0f%% H%d\n",
				index+1, candidate.ID, status, candidate.Score, candidate.Train.TotalReturn,
				candidate.Validate.TotalReturn, candidate.Validate.MaxDrawdown, candidate.Validate.Trades,
				p.FastMA, p.SlowMA, p.BreakoutDays, p.VolumeRatioMin, p.StopLoss*100,
				p.TakeProfit*100, p.MaxHoldingDays)
		}
		if result.OutOfSample != nil && len(result.OutOfSample.Trades) > 0 {
			builder.WriteString("\n## 样本外逐笔交易\n")
			for _, trade := range result.OutOfSample.Trades {
				fmt.Fprintf(&builder, "- %s  %s %s  %s %.2f → %s %.2f  %+.2f%%  %s\n",
					trade.ID, strategyLabCode(trade.Symbol), trade.Name, trade.Entry.Date,
					trade.Entry.Price, trade.Exit.Date, trade.Exit.Price, trade.ReturnPercent, trade.ExitReason)
			}
		}
		fmt.Fprintf(&builder, "\n报告 %s", result.ReportPath)
		data.Body = builder.String()
	case strategyLabContinuousHistory:
		data.Status = fmt.Sprintf("多Agent持续优化历史：%d 条；Enter 查看滚动、留出和压力测试。", len(state.continuous))
		for _, item := range state.continuous {
			stage := "研究候选"
			if item.Stage == backtest.ContinuousStageShadow {
				stage = "模拟观察"
			}
			selected := item.SelectedID
			if selected == "" {
				selected = "未入选"
			}
			data.Items = append(data.Items, fmt.Sprintf(
				"%s  %s  %s  候选%d  %s  留出%+.2f%%",
				item.ID, item.GeneratedAt.Format("01-02 15:04"), stage, item.Candidates, selected, item.HoldoutReturn,
			))
		}
		if len(data.Items) == 0 {
			data.Body = "暂无多Agent持续优化历史。"
		}
		data.Items, data.Selected = strategyLabVisibleItems(data.Items, state.selected, 15)
	case strategyLabContinuousDetail:
		data.StatusTone = "warning"
		if state.continuousRun == nil {
			data.Body = "持续优化详情不可用。"
			break
		}
		result := state.continuousRun
		stage := "研究候选，尚不可作为操作参考"
		if result.Stage == backtest.ContinuousStageShadow {
			stage = "模拟观察候选，仍需真实时间影子运行"
			data.StatusTone = "success"
		}
		data.Status = fmt.Sprintf("持续优化 %s  ·  第%d轮  ·  %s", result.ID, result.Cycle, stage)
		var builder strings.Builder
		fmt.Fprintf(&builder, "数据截止 %s  ·  主Agent监督%d个子Agent  ·  候选%d  ·  滚动%d折\n", result.DataCutoff, len(result.Agents), len(result.Candidates), len(result.Request.Folds))
		fmt.Fprintf(&builder, "数据质量 %s（硬门 %v）  候选指纹 %s  配置指纹 %s\n", result.Quality.Grade, result.Quality.Passed, shortHash(result.Manifest.CandidateSetHash), shortHash(result.Manifest.ConfigurationHash))
		for _, check := range result.Quality.Checks {
			status := "通过"
			if !check.Passed {
				status = "警告"
				if !check.Warning {
					status = "失败"
				}
			}
			fmt.Fprintf(&builder, "- 数据门 %s：%s，%s\n", check.Name, status, check.Detail)
		}
		if result.ParentID != "" {
			fmt.Fprintf(&builder, "上一轮 %s，入选参数作为本轮基线后重新验证。\n", result.ParentID)
		}
		if result.Selected == nil {
			builder.WriteString("\n没有候选通过滚动验证门禁；最终留出未运行。\n")
		} else {
			candidate := result.Selected
			p := candidate.Proposal.Parameters
			fmt.Fprintf(&builder, "\n## 锁定候选\n%s（%s）：%s\n模式 %s  MA%d/%d  突破%d日  量比%.2f  止损%.0f%%  止盈%.0f%%  持有%d日\n", candidate.Proposal.ID, candidate.Proposal.Agent, candidate.Proposal.Thesis, p.EffectiveEntryMode(), p.FastMA, p.SlowMA, p.BreakoutDays, p.VolumeRatioMin, p.StopLoss*100, p.TakeProfit*100, p.MaxHoldingDays)
			fmt.Fprintf(&builder, "滚动正收益窗口 %.0f%%  验证交易%d  平均收益%+.2f%%  最差回撤%+.2f%%  评分%.2f\n", candidate.PositiveFoldRatio*100, candidate.ValidationTrades, candidate.AverageValidation, candidate.WorstDrawdown, candidate.Score)
			fmt.Fprintf(&builder, "Agent共识%d  邻域候选%d  邻域均分%.2f\n", candidate.ConsensusAgents, candidate.NeighborhoodSize, candidate.NeighborhoodScore)
			builder.WriteString("\n## 滚动折证据\n")
			for _, fold := range candidate.Folds {
				fmt.Fprintf(&builder, "%s 训练%+.2f%%/回撤%+.2f%%/夏普%.2f/交易%d  验证%+.2f%%/回撤%+.2f%%/夏普%.2f/交易%d\n",
					fold.Fold.ID, fold.Train.TotalReturn, fold.Train.MaxDrawdown, fold.Train.Sharpe, fold.Train.Trades,
					fold.Validate.TotalReturn, fold.Validate.MaxDrawdown, fold.Validate.Sharpe, fold.Validate.Trades)
				for _, ticker := range result.Request.BaseRequest.Tickers {
					train := fold.TrainCoverage[ticker]
					validate := fold.ValidateCoverage[ticker]
					fmt.Fprintf(&builder, "- %s 覆盖 训练%.0f%%/%d 验证%.0f%%/%d  来源 %s/%s\n", strategyLabCode(ticker), train.CoverageRatio*100, train.Bars, validate.CoverageRatio*100, validate.Bars, fold.TrainSources[ticker], fold.ValidateSources[ticker])
				}
			}
			if result.Holdout != nil {
				m := result.Holdout.Metrics
				fmt.Fprintf(&builder, "\n## 最终留出\n收益%+.2f%%  年化%+.2f%%  回撤%+.2f%%  夏普%.2f  交易%d\n", m.TotalReturn, m.AnnualizedReturn, m.MaxDrawdown, m.Sharpe, m.Trades)
			}
			if result.Stress.DoubleCost != nil {
				m := result.Stress.DoubleCost.Metrics
				fmt.Fprintf(&builder, "\n## 压力测试\n双倍费用和滑点：收益%+.2f%%  回撤%+.2f%%\n最佳单笔占全部正收益 %.0f%%\n", m.TotalReturn, m.MaxDrawdown, result.Stress.BestTradeProfitShare*100)
			}
		}
		if len(result.GateReasons) > 0 {
			builder.WriteString("\n## 未晋级原因\n")
			for _, reason := range result.GateReasons {
				fmt.Fprintf(&builder, "- %s\n", reason)
			}
		}
		if result.PriorLessons != "" {
			builder.WriteString("\n## 历史反思上下文\n" + strings.TrimSpace(result.PriorLessons) + "\n")
		}
		builder.WriteString("\n## 子Agent状态\n")
		for _, agent := range result.Agents {
			fmt.Fprintf(&builder, "- %s：%s，候选%d", agent.Agent, agent.Status, len(agent.Proposals))
			if agent.Error != "" {
				fmt.Fprintf(&builder, "，%s", agent.Error)
			}
			builder.WriteString("\n")
		}
		if result.SupervisorReview != "" {
			builder.WriteString("\n## 主Agent监督复盘\n" + strings.TrimSpace(result.SupervisorReview) + "\n")
		} else if result.SupervisorError != "" {
			fmt.Fprintf(&builder, "\n主Agent复盘失败：%s\n", result.SupervisorError)
		}
		fmt.Fprintf(&builder, "\n报告 %s", result.ReportPath)
		data.Body = builder.String()
	}
	return ui.BuildStrategyLabFrame(data, width, moyu, color)
}

func strategyLabVisibleItems(items []string, selected, size int) ([]string, int) {
	if len(items) <= size || size < 1 {
		return items, selected
	}
	start := selected - size/2
	if start < 0 {
		start = 0
	}
	if start+size > len(items) {
		start = len(items) - size
	}
	return items[start : start+size], selected - start
}

type strategyLabTask struct {
	kind          string
	symbols       []string
	names         map[string]string
	period        backtest.Period
	train         backtest.Period
	validate      backtest.Period
	oos           backtest.Period
	maxCandidates int
	minimumTrades int
	useAI         bool
}

func (app *App) runStrategyLabBacktest(ctx context.Context, task strategyLabTask) (backtest.Result, error) {
	request := defaultResearchRequest(task.symbols, task.names, task.period)
	engine := backtest.NewDailyEngine(backtest.NewCachingDailyBarProvider(market.EastmoneyClient{}))
	result, err := engine.Run(ctx, request)
	if err != nil {
		return backtest.Result{}, err
	}
	return app.backtestStore().Save(result)
}

func (app *App) runStrategyLabOptimization(
	ctx context.Context,
	task strategyLabTask,
	progress func(string),
) (backtest.OptimizationResult, error) {
	base := defaultResearchRequest(task.symbols, task.names, task.train)
	request := backtest.DefaultOptimizationRequest(base, task.train, task.validate, task.oos)
	request.MaxCandidates = task.maxCandidates
	request.MinimumValidationTrades = task.minimumTrades
	request.UseAI = task.useAI
	provider := backtest.NewCachingDailyBarProvider(market.EastmoneyClient{})
	optimizer := backtest.NewOptimizer(backtest.NewDailyEngine(provider))
	result, err := optimizer.OptimizeWithProgress(ctx, request, func(item backtest.OptimizationProgress) {
		if progress == nil {
			return
		}
		switch item.Phase {
		case "candidate":
			progress(fmt.Sprintf("训练/验证候选 %d/%d", item.Completed, item.Total))
		case "out-of-sample":
			progress("参数已锁定，执行一次性样本外检验")
		}
	})
	if err != nil {
		return backtest.OptimizationResult{}, err
	}
	if request.UseAI {
		if progress != nil {
			progress("Codex只读复盘已锁定结果")
		}
		result = app.reviewOptimization(ctx, result)
	}
	return app.optimizationStore().Save(result)
}
