package app

import (
	"context"
	"fmt"
	"math"

	"github.com/wenzhe/astock-workbench/internal/market"
)

func boardMetric(value float64, suffix string) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	return fmt.Sprintf("%.2f%s", value, suffix)
}

func boardMoney(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	absolute := math.Abs(value)
	switch {
	case absolute >= 1e8:
		return fmt.Sprintf("%.2f亿", value/1e8)
	case absolute >= 1e4:
		return fmt.Sprintf("%.2f万", value/1e4)
	default:
		return fmt.Sprintf("%.0f", value)
	}
}

func (app *App) runBoardSnapshots(ctx context.Context, symbols []string) error {
	client, ok := app.boards.(market.BoardDetailClient)
	if !ok || client == nil {
		return fmt.Errorf("板块查询服务未初始化")
	}
	for index, symbol := range symbols {
		flow, leaders, err := client.FetchBoard(ctx, symbol)
		if err != nil {
			return fmt.Errorf("查询板块 %s 失败: %w", symbol, err)
		}
		if index > 0 {
			fmt.Fprintln(app.out)
		}
		displayCode := symbol
		if market.IsTHSIndustrySymbol(symbol) {
			displayCode = market.THSIndustryCode(symbol)
		}
		fmt.Fprintf(app.out, "%s  %s  板块快照\n", flow.Name, displayCode)
		if market.IsTHSIndustrySymbol(symbol) {
			fmt.Fprintf(app.out, "涨跌 %s  主力净流入 %s\n", boardMetric(flow.Percent, "%"), boardMoney(flow.MainNet))
		} else {
			fmt.Fprintf(app.out, "涨跌 %s  主力净流入 %s  主力占比 %s  换手率 %s\n",
				boardMetric(flow.Percent, "%"), boardMoney(flow.MainNet), boardMetric(flow.MainRatio, "%"), boardMetric(flow.Turnover, "%"))
		}
		if flow.Quote != nil {
			fmt.Fprintf(app.out, "指数 %s  涨跌额 %s  今开 %s  昨收 %s  最高 %s  最低 %s\n",
				boardMetric(flow.Quote.Price, ""), boardMetric(flow.Quote.Delta, ""), boardMetric(flow.Quote.Open, ""), boardMetric(flow.Quote.PreviousClose, ""), boardMetric(flow.Quote.High, ""), boardMetric(flow.Quote.Low, ""))
			fmt.Fprintf(app.out, "成交量 %s万手  成交额 %s  涨幅排名 %d/%d\n",
				boardMetric(flow.Quote.Volume, ""), boardMoney(flow.Quote.Amount), flow.ChangeRank, flow.UniverseSize)
		}
		if market.IsTHSIndustrySymbol(symbol) {
			fmt.Fprintf(app.out, "上涨 %d  下跌 %d\n", flow.RiseCount, flow.FallCount)
		} else {
			fmt.Fprintf(app.out, "上涨 %d  下跌 %d  平盘 %d\n", flow.RiseCount, flow.FallCount, flow.FlatCount)
		}
		if len(leaders) == 0 {
			fmt.Fprintln(app.out, "龙头股暂不可用")
			continue
		}
		fmt.Fprintln(app.out, "成分股（按涨幅排序）")
		for _, leader := range leaders {
			displayCode := leader.Symbol
			if len(displayCode) == 8 {
				displayCode = displayCode[2:]
			}
			fmt.Fprintf(app.out, "  %s  %s  涨跌 %s  涨速 %s  换手 %s  量比 %s  成交额 %s\n",
				displayCode, leader.Name, boardMetric(leader.Percent, "%"), boardMetric(leader.Speed, "%"), boardMetric(leader.Turnover, "%"), boardMetric(leader.VolumeRatio, ""), boardMoney(leader.Amount))
		}
	}
	return nil
}
