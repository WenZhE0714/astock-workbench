package backtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type StabilityStats struct {
	ClosedTrades     int     `json:"closed_trades"`
	OpenPositions    int     `json:"open_positions"`
	Wins             int     `json:"wins"`
	Losses           int     `json:"losses"`
	RealizedProfit   float64 `json:"realized_profit"`
	UnrealizedProfit float64 `json:"unrealized_profit"`
	TotalProfit      float64 `json:"total_profit"`
	WinningProfit    float64 `json:"winning_profit"`
	LosingAmount     float64 `json:"losing_amount"`
	Fees             float64 `json:"fees"`
	AverageReturn    float64 `json:"average_closed_trade_return_percent"`
}

type StabilityRow struct {
	WindowID  string         `json:"window_id"`
	ID        string         `json:"id"`
	Symbol    string         `json:"symbol"`
	Name      string         `json:"name"`
	Signal    SignalSnapshot `json:"entry_signal"`
	EntryDate string         `json:"entry_date"`
	EndDate   string         `json:"end_date"`
	Closed    bool           `json:"closed"`
	Profit    float64        `json:"profit"`
	Return    float64        `json:"return_percent"`
	Fees      float64        `json:"fees"`
}

type StabilityGroup struct {
	Key   string         `json:"key"`
	Label string         `json:"label"`
	Stats StabilityStats `json:"stats"`
}

type StabilityDimension struct {
	Key    string           `json:"key"`
	Label  string           `json:"label"`
	Groups []StabilityGroup `json:"groups"`
}

type StabilityContribution struct {
	WindowID string  `json:"window_id"`
	Date     string  `json:"date"`
	Symbol   string  `json:"symbol"`
	Profit   float64 `json:"profit"`
}

type StabilityYear struct {
	Year    string   `json:"year"`
	Start   string   `json:"start"`
	End     string   `json:"end"`
	Days    int      `json:"observed_trading_days"`
	Windows []string `json:"windows"`
	Profit  float64  `json:"marked_profit"`
}

type StabilityConcentration struct {
	TopWinners                 []StabilityRow   `json:"top_five_winners"`
	TopOneWinnerShare          float64          `json:"top_one_share_of_winning_profit"`
	TopThreeWinnerShare        float64          `json:"top_three_share_of_winning_profit"`
	TopFiveWinnerShare         float64          `json:"top_five_share_of_winning_profit"`
	ProfitMinusTopFive         float64          `json:"profit_minus_top_five_winner_contributions"`
	TopSymbols                 []StabilityGroup `json:"top_three_positive_symbols"`
	PositiveSymbolProfit       float64          `json:"positive_symbol_profit"`
	TopOneSymbolShare          float64          `json:"top_one_share_of_positive_symbol_profit"`
	TopThreeSymbolShare        float64          `json:"top_three_share_of_positive_symbol_profit"`
	ProfitMinusTopThreeSymbols float64          `json:"profit_minus_top_three_symbol_contributions"`
}

type StabilityAnalysis struct {
	Stats         StabilityStats          `json:"stats"`
	Symbols       []StabilityGroup        `json:"symbols"`
	Years         []StabilityYear         `json:"years"`
	Signals       []StabilityDimension    `json:"signals"`
	Concentration StabilityConcentration  `json:"concentration"`
	Rows          []StabilityRow          `json:"rows"`
	Contributions []StabilityContribution `json:"daily_contributions"`
	MaxDailyError float64                 `json:"max_daily_reconciliation_error"`
}

func addStabilityRow(stats *StabilityStats, row StabilityRow) {
	stats.TotalProfit += row.Profit
	stats.Fees += row.Fees
	if !row.Closed {
		stats.OpenPositions++
		stats.UnrealizedProfit += row.Profit
		return
	}
	stats.AverageReturn = (stats.AverageReturn*float64(stats.ClosedTrades) + row.Return) / float64(stats.ClosedTrades+1)
	stats.ClosedTrades++
	stats.RealizedProfit += row.Profit
	if row.Profit > 0 {
		stats.Wins++
		stats.WinningProfit += row.Profit
	} else if row.Profit < 0 {
		stats.Losses++
		stats.LosingAmount -= row.Profit
	}
}

func stabilitySignalDimensions() []StabilityDimension {
	return []StabilityDimension{
		{Key: "ma_distance", Label: "信号收盘高于快均线", Groups: []StabilityGroup{{Key: "low", Label: "不足3%"}, {Key: "middle", Label: "3%至不足5%"}, {Key: "high", Label: "至少5%"}, {Key: "unknown", Label: "缺失"}}},
		{Key: "trend_spread", Label: "快均线高于慢均线", Groups: []StabilityGroup{{Key: "low", Label: "不足5%"}, {Key: "middle", Label: "5%至不足10%"}, {Key: "high", Label: "至少10%"}, {Key: "unknown", Label: "缺失"}}},
		{Key: "return_20", Label: "信号日前20日涨幅", Groups: []StabilityGroup{{Key: "low", Label: "负涨幅"}, {Key: "middle", Label: "0%至不足10%"}, {Key: "high", Label: "至少10%"}, {Key: "unknown", Label: "缺失"}}},
		{Key: "volume_ratio", Label: "信号日量比", Groups: []StabilityGroup{{Key: "low", Label: "不足1倍"}, {Key: "middle", Label: "1至不足1.5倍"}, {Key: "high", Label: "至少1.5倍"}, {Key: "unknown", Label: "缺失"}}},
	}
}

func stabilitySignalBucket(key string, signal SignalSnapshot) string {
	var value, lower, upper float64
	switch key {
	case "ma_distance":
		value, lower, upper = relativeDistance(signal.Close, signal.FastMA)*100, 3, 5
	case "trend_spread":
		value, lower, upper = relativeDistance(signal.FastMA, signal.SlowMA)*100, 5, 10
	case "return_20":
		value, lower, upper = signal.Return20*100, 0, 10
	case "volume_ratio":
		if signal.VolumeRatio <= 0 {
			return "unknown"
		}
		value, lower, upper = signal.VolumeRatio, 1, 1.5
	default:
		return "unknown"
	}
	if !finiteValues(value) {
		return "unknown"
	}
	if value < lower {
		return "low"
	}
	if value < upper {
		return "middle"
	}
	return "high"
}

func summarizeStabilityRows(rows []StabilityRow, names map[string]string) StabilityAnalysis {
	result := StabilityAnalysis{Rows: rows, Symbols: []StabilityGroup{}, Signals: stabilitySignalDimensions()}
	symbols := map[string]*StabilityGroup{}
	for symbol, name := range names {
		symbols[symbol] = &StabilityGroup{Key: symbol, Label: name}
	}
	winners := []StabilityRow{}
	for _, row := range rows {
		addStabilityRow(&result.Stats, row)
		if symbols[row.Symbol] == nil {
			symbols[row.Symbol] = &StabilityGroup{Key: row.Symbol, Label: row.Name}
		}
		addStabilityRow(&symbols[row.Symbol].Stats, row)
		for i := range result.Signals {
			d := &result.Signals[i]
			key := stabilitySignalBucket(d.Key, row.Signal)
			for j := range d.Groups {
				if d.Groups[j].Key == key {
					addStabilityRow(&d.Groups[j].Stats, row)
				}
			}
		}
		if row.Closed && row.Profit > 0 {
			winners = append(winners, row)
		}
	}
	for _, group := range symbols {
		result.Symbols = append(result.Symbols, *group)
	}
	sort.Slice(result.Symbols, func(i, j int) bool { return result.Symbols[i].Key < result.Symbols[j].Key })
	sort.Slice(winners, func(i, j int) bool {
		if winners[i].Profit == winners[j].Profit {
			return winners[i].WindowID+"|"+winners[i].ID < winners[j].WindowID+"|"+winners[j].ID
		}
		return winners[i].Profit > winners[j].Profit
	})
	c := &result.Concentration
	c.TopWinners = winners[:min(5, len(winners))]
	c.ProfitMinusTopFive = result.Stats.TotalProfit
	for i, row := range c.TopWinners {
		share := row.Profit / result.Stats.WinningProfit
		if i == 0 {
			c.TopOneWinnerShare += share
		}
		if i < 3 {
			c.TopThreeWinnerShare += share
		}
		c.TopFiveWinnerShare += share
		c.ProfitMinusTopFive -= row.Profit
	}
	positiveSymbols := []StabilityGroup{}
	for _, group := range result.Symbols {
		if group.Stats.TotalProfit > 0 {
			positiveSymbols = append(positiveSymbols, group)
			c.PositiveSymbolProfit += group.Stats.TotalProfit
		}
	}
	sort.Slice(positiveSymbols, func(i, j int) bool {
		if positiveSymbols[i].Stats.TotalProfit == positiveSymbols[j].Stats.TotalProfit {
			return positiveSymbols[i].Key < positiveSymbols[j].Key
		}
		return positiveSymbols[i].Stats.TotalProfit > positiveSymbols[j].Stats.TotalProfit
	})
	c.TopSymbols = positiveSymbols[:min(3, len(positiveSymbols))]
	c.ProfitMinusTopThreeSymbols = result.Stats.TotalProfit
	for i, group := range c.TopSymbols {
		share := group.Stats.TotalProfit / c.PositiveSymbolProfit
		if i == 0 {
			c.TopOneSymbolShare = share
		}
		c.TopThreeSymbolShare += share
		c.ProfitMinusTopThreeSymbols -= group.Stats.TotalProfit
	}
	return result
}

func analyzeStabilityRun(ctx context.Context, provider DailyBarProvider, run Result, windowID string) (StabilityAnalysis, error) {
	rows := []StabilityRow{}
	barsBySymbol := map[string]map[string]domain.DailyBar{}
	daily := map[string]map[string]float64{}
	dates := map[string]bool{}
	names := map[string]string{}
	for _, symbol := range run.Request.Tickers {
		names[symbol] = run.Request.Names[symbol]
	}
	previousDate := ""
	for _, point := range run.Equity {
		if _, err := time.Parse(time.DateOnly, point.Date); err != nil || point.Date <= previousDate || !dateInRange(point.Date, run.Request.Start, run.Request.End) || !finiteValues(point.Equity, point.Cash, point.Positions) {
			return StabilityAnalysis{}, fmt.Errorf("每日净值日期或数值无效")
		}
		dates[point.Date] = true
		previousDate = point.Date
	}
	if len(run.Equity) == 0 {
		return StabilityAnalysis{}, fmt.Errorf("缺少每日净值")
	}
	attribute := func(row StabilityRow, entry Fill, exit *Fill) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Quantity <= 0 || !dates[entry.Date] || !dates[row.EndDate] || row.EndDate < entry.Date || !finiteValues(row.Profit, row.Return, row.Fees, entry.Amount, entry.TotalFee) || entry.Amount <= 0 {
			return fmt.Errorf("%s 的成交或估值区间无效", row.ID)
		}
		bars, ok := barsBySymbol[row.Symbol]
		if !ok {
			warmup := max(run.Request.Technical.SlowMA, run.Request.Technical.BreakoutDays+1, 21)
			items, err := provider.FetchDailyBarsRange(ctx, row.Symbol, run.Request.Start.AddDate(0, 0, -warmup*3), run.Request.End, run.Request.Adjustment)
			if err != nil {
				return err
			}
			bars = map[string]domain.DailyBar{}
			for _, bar := range items {
				bars[bar.Date] = bar
			}
			barsBySymbol[row.Symbol] = bars
		}
		entryBar, entryOK := bars[entry.Date]
		signalBar, signalOK := bars[row.Signal.Date]
		if !entryOK || !signalOK || row.Signal.Date >= entry.Date || math.Abs(entry.RawPrice-entryBar.Open) > 1e-6 || math.Abs(row.Signal.Close-signalBar.Close) > 1e-6 {
			return fmt.Errorf("%s 原入场与冻结日K不一致", row.ID)
		}
		if exit != nil {
			exitBar, ok := bars[exit.Date]
			price := exitBar.Open
			if exit.Forced {
				price = exitBar.Close
			}
			if !ok || exit.Date <= entry.Date || exit.Quantity != entry.Quantity || math.Abs(exit.RawPrice-price) > 1e-6 {
				return fmt.Errorf("%s 原退出与冻结日K不一致", row.ID)
			}
		}
		previous, total := entry.Amount+entry.TotalFee, 0.0
		for _, point := range run.Equity {
			if point.Date < entry.Date || point.Date > row.EndDate {
				continue
			}
			marked := previous
			// A normal opening exit ends exposure before this day's close.
			if exit != nil && point.Date == exit.Date {
				marked = exit.Amount - exit.TotalFee
			} else if bar, ok := bars[point.Date]; ok {
				marked = bar.Close * float64(entry.Quantity)
			}
			change := marked - previous
			if !finiteValues(marked, change) {
				return fmt.Errorf("%s 盯市价格非有限", row.ID)
			}
			if daily[point.Date] == nil {
				daily[point.Date] = map[string]float64{}
			}
			daily[point.Date][row.Symbol] += change
			total += change
			previous = marked
		}
		if math.Abs(total-row.Profit) > .01 {
			return fmt.Errorf("%s 逐日盯市与原盈亏不一致", row.ID)
		}
		rows = append(rows, row)
		return nil
	}
	for _, trade := range run.Trades {
		row := StabilityRow{WindowID: windowID, ID: trade.ID, Symbol: trade.Symbol, Name: trade.Name, Signal: trade.EntrySignal, EntryDate: trade.Entry.Date, EndDate: trade.Exit.Date, Closed: true, Profit: trade.NetProfit, Return: trade.ReturnPercent, Fees: trade.Entry.TotalFee + trade.Exit.TotalFee}
		if err := attribute(row, trade.Entry, &trade.Exit); err != nil {
			return StabilityAnalysis{}, err
		}
	}
	for _, position := range run.OpenPositions {
		if position.LastDate != run.Equity[len(run.Equity)-1].Date {
			return StabilityAnalysis{}, fmt.Errorf("未平仓估值日期未覆盖区间末尾")
		}
		row := StabilityRow{WindowID: windowID, ID: "open:" + position.Symbol, Symbol: position.Symbol, Name: position.Name, Signal: position.EntrySignal, EntryDate: position.Entry.Date, EndDate: position.LastDate, Profit: position.UnrealizedProfit, Return: position.ReturnPercent, Fees: position.Entry.TotalFee}
		if err := attribute(row, position.Entry, nil); err != nil {
			return StabilityAnalysis{}, err
		}
	}
	result := summarizeStabilityRows(rows, names)
	result.Contributions, result.Years = []StabilityContribution{}, []StabilityYear{}
	years := map[string]*StabilityYear{}
	previous := run.Request.InitialCash
	for _, point := range run.Equity {
		year := point.Date[:4]
		if years[year] == nil {
			years[year] = &StabilityYear{Year: year, Start: point.Date, Windows: []string{windowID}}
		}
		years[year].Days++
		years[year].End = point.Date
		total := 0.0
		for _, group := range result.Symbols {
			profit := daily[point.Date][group.Key]
			if profit != 0 {
				result.Contributions = append(result.Contributions, StabilityContribution{WindowID: windowID, Date: point.Date, Symbol: group.Key, Profit: profit})
			}
			total += profit
		}
		years[year].Profit += total
		delta := math.Abs(total - (point.Equity - previous))
		if delta > .01 {
			return result, fmt.Errorf("%s 每日归因与组合净值不平衡", point.Date)
		}
		result.MaxDailyError = math.Max(result.MaxDailyError, delta)
		previous = point.Equity
	}
	if !finiteValues(run.Metrics.FinalEquity, run.Request.InitialCash) || math.Abs(result.Stats.TotalProfit-(run.Metrics.FinalEquity-run.Request.InitialCash)) > .01 || math.Abs(previous-run.Metrics.FinalEquity) > .01 || math.Abs(result.Stats.Fees-run.Metrics.TotalFees) > .01 || result.Stats.ClosedTrades != run.Metrics.Trades {
		return result, fmt.Errorf("总归因与期末净值不平衡")
	}
	for _, year := range years {
		result.Years = append(result.Years, *year)
	}
	sort.Slice(result.Years, func(i, j int) bool { return result.Years[i].Year < result.Years[j].Year })
	return result, nil
}
