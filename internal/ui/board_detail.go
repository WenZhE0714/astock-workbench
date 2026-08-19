package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func boardDetailMetric(value float64, format string) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	return fmt.Sprintf(format, value)
}

func boardDetailCode(symbol string) string {
	if len(symbol) == 8 && (strings.HasPrefix(symbol, "sh") || strings.HasPrefix(symbol, "sz")) {
		return symbol[2:]
	}
	if strings.HasPrefix(strings.ToLower(symbol), "th") {
		return symbol[2:]
	}
	return symbol
}

func boardDetailColumns(values []string) string {
	return strings.Join(values, "  ")
}

// BuildBoardDetailFrame renders a standalone board asset view. It is used for
// 88xxxx Tonghuashun industries, which cannot be sent through stock quotes.
func BuildBoardDetailFrame(
	flow domain.BoardFlow,
	leaders []domain.MarketStockSnapshot,
	symbol, status, controls string,
	terminalWidth int,
	moyu, color bool,
) string {
	if terminalWidth < 32 {
		terminalWidth = 32
	}
	name := flow.Name
	if name == "" {
		name = boardDetailCode(symbol)
	}
	title := fmt.Sprintf("ASTOCK INDUSTRY BOARD  ·  %s  ·  %s", name, boardDetailCode(symbol))
	if !moyu {
		title = fmt.Sprintf("ASTOCK 同花顺行业板块  ·  %s  ·  %s", name, boardDetailCode(symbol))
	}
	lines := []string{
		style(truncateWidth(title, terminalWidth), "1;36", color && !moyu),
		style(truncateWidth(controls, terminalWidth), "90", color && !moyu),
		"",
		style("板块概览", "1;36", color && !moyu),
	}
	change := boardDetailMetric(flow.Percent, "%+.2f%%")
	quote := domain.BoardQuoteSnapshot{
		Price: math.NaN(), Delta: math.NaN(), Open: math.NaN(), PreviousClose: math.NaN(),
		High: math.NaN(), Low: math.NaN(), Volume: math.NaN(), Amount: math.NaN(),
	}
	if flow.Quote != nil {
		quote = *flow.Quote
	}
	delta := boardDetailMetric(quote.Delta, "%+.2f")
	flowMoney := humanFundMovementAmount(flow.MainNet)
	if moyu {
		lines = append(lines, fmt.Sprintf("INDEX %-8s  DELTA %-8s  CHANGE %-8s  MAIN NET %-12s", boardDetailMetric(quote.Price, "%.2f"), delta, change, flowMoney))
		lines = append(lines, fmt.Sprintf("OPEN %-8s  PREV %-8s  HIGH %-8s  LOW %-8s  AMOUNT %-10s", boardDetailMetric(quote.Open, "%.2f"), boardDetailMetric(quote.PreviousClose, "%.2f"), boardDetailMetric(quote.High, "%.2f"), boardDetailMetric(quote.Low, "%.2f"), humanAmountYuan(quote.Amount)))
		lines = append(lines, fmt.Sprintf("VOLUME %-10s  RANK %d/%d  UP/DOWN %d/%d", boardDetailMetric(quote.Volume, "%.2f万手"), flow.ChangeRank, flow.UniverseSize, flow.RiseCount, flow.FallCount))
	} else {
		rank := "--"
		if flow.ChangeRank > 0 && flow.UniverseSize > 0 {
			rank = fmt.Sprintf("%d/%d", flow.ChangeRank, flow.UniverseSize)
		}
		direction := trendCode(flow.Percent, false)
		lines = append(lines, fmt.Sprintf("指数 %s    涨跌额 %s    涨幅 %s    主力净流入 %s",
			style(boardDetailMetric(quote.Price, "%.2f"), direction, color), style(delta, direction, color),
			style(change, direction, color), style(flowMoney, trendCode(flow.MainNet, false), color)))
		lines = append(lines, fmt.Sprintf("今开 %s    昨收 %s    最高 %s    最低 %s    成交额 %s", boardDetailMetric(quote.Open, "%.2f"), boardDetailMetric(quote.PreviousClose, "%.2f"), boardDetailMetric(quote.High, "%.2f"), boardDetailMetric(quote.Low, "%.2f"), humanAmountYuan(quote.Amount)))
		lines = append(lines, fmt.Sprintf("成交量 %s    涨幅排名 %s    上涨 %d    下跌 %d    数据源 同花顺行业", boardDetailMetric(quote.Volume, "%.2f万手"), rank, flow.RiseCount, flow.FallCount))
		optional := make([]string, 0, 2)
		if !math.IsNaN(flow.MainRatio) && !math.IsInf(flow.MainRatio, 0) {
			optional = append(optional, "主力占比 "+boardDetailMetric(flow.MainRatio, "%.2f%%"))
		}
		if !math.IsNaN(flow.Turnover) && !math.IsInf(flow.Turnover, 0) {
			optional = append(optional, "换手率 "+boardDetailMetric(flow.Turnover, "%.2f%%"))
		}
		if len(optional) > 0 {
			lines = append(lines, strings.Join(optional, "    "))
		}
	}
	lines = append(lines, "", style("成分股 · 涨幅排序前十 · 方向数据按同花顺行业页面", "1;36", color && !moyu))
	if len(leaders) == 0 {
		lines = append(lines, style("暂无成分股数据", "33", color && !moyu))
	} else {
		wide := terminalWidth >= 96
		if wide {
			lines = append(lines, style(boardDetailColumns([]string{
				padWidth("序", 2, "right"), padWidth("代码", 6, "left"), padWidth("名称", 16, "left"),
				padWidth("现价", 9, "right"), padWidth("涨跌", 9, "right"), padWidth("涨速", 9, "right"),
				padWidth("换手", 9, "right"), padWidth("量比", 8, "right"), padWidth("成交额", 11, "right"),
			}), "90", color && !moyu))
		} else {
			lines = append(lines, style(boardDetailColumns([]string{
				padWidth("序", 2, "right"), padWidth("代码", 6, "left"), padWidth("名称", 12, "left"),
				padWidth("现价", 9, "right"), padWidth("涨跌", 9, "right"), padWidth("成交额", 11, "right"),
			}), "90", color && !moyu))
		}
		for index, leader := range leaders {
			nameWidth := 12
			if wide {
				nameWidth = 16
			}
			prefix := []string{
				padWidth(strconv.Itoa(index+1), 2, "right"),
				padWidth(boardDetailCode(leader.Symbol), 6, "left"),
				padWidth(truncateWidth(leader.Name, nameWidth), nameWidth, "left"),
			}
			percent := style(padWidth(signedPercent(leader.Percent), 9, "right"), trendCode(leader.Percent, false), color && !moyu)
			if wide {
				line := boardDetailColumns(append(prefix,
					padWidth(boardDetailMetric(leader.Price, "%.2f"), 9, "right"), percent,
					style(padWidth(signedPercent(leader.Speed), 9, "right"), trendCode(leader.Speed, false), color && !moyu),
					style(padWidth(boardDetailMetric(leader.Turnover, "%.2f%%"), 9, "right"), "90", color && !moyu),
					style(padWidth(boardDetailMetric(leader.VolumeRatio, "%.2f"), 8, "right"), "90", color && !moyu),
					padWidth(humanAmountYuan(leader.Amount), 11, "right"),
				))
				lines = append(lines, truncateWidth(line, terminalWidth))
				continue
			}
			line := boardDetailColumns(append(prefix,
				padWidth(boardDetailMetric(leader.Price, "%.2f"), 9, "right"), percent,
				padWidth(humanAmountYuan(leader.Amount), 11, "right"),
			))
			lines = append(lines, truncateWidth(line, terminalWidth))
		}
	}
	if status != "" {
		lines = append(lines, "", style(truncateWidth(status, terminalWidth), "33", color && !moyu))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func humanAmountYuan(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) || value == 0 {
		return "--"
	}
	if math.Abs(value) >= 1e8 {
		return fmt.Sprintf("%.2f亿", value/1e8)
	}
	return fmt.Sprintf("%.0f万", value/1e4)
}
