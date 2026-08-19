package backtest

import "math"

func calculateMetrics(request Request, equity []EquityPoint, trades []Trade, turnover, fees, benchmarkReturn float64, benchmarkAvailable bool) Metrics {
	metrics := Metrics{Trades: len(trades), TotalFees: fees, BenchmarkReturn: benchmarkReturn, BenchmarkAvailable: benchmarkAvailable}
	if len(equity) == 0 {
		return metrics
	}
	metrics.FinalEquity = equity[len(equity)-1].Equity
	metrics.TotalReturn = (metrics.FinalEquity/request.InitialCash - 1) * 100
	if len(equity) > 1 {
		years := float64(len(equity)-1) / 252
		if years > 0 && metrics.FinalEquity > 0 {
			metrics.AnnualizedReturn = (math.Pow(metrics.FinalEquity/request.InitialCash, 1/years) - 1) * 100
		}
		returns := make([]float64, 0, len(equity)-1)
		for index := 1; index < len(equity); index++ {
			if equity[index-1].Equity > 0 {
				returns = append(returns, equity[index].Equity/equity[index-1].Equity-1)
			}
		}
		if len(returns) > 1 {
			mean := 0.0
			for _, value := range returns {
				mean += value
			}
			mean /= float64(len(returns))
			variance := 0.0
			for _, value := range returns {
				variance += (value - mean) * (value - mean)
			}
			variance /= float64(len(returns) - 1)
			if variance > 0 {
				metrics.Sharpe = mean / math.Sqrt(variance) * math.Sqrt(252)
			}
		}
	}
	for _, point := range equity {
		if point.Drawdown < metrics.MaxDrawdown {
			metrics.MaxDrawdown = point.Drawdown
		}
	}
	profit, loss, tradeReturn, holding := 0.0, 0.0, 0.0, 0
	for _, trade := range trades {
		tradeReturn += trade.ReturnPercent
		holding += trade.HoldingDays
		if trade.NetProfit > 0 {
			metrics.Wins++
			profit += trade.NetProfit
		} else if trade.NetProfit < 0 {
			metrics.Losses++
			loss -= trade.NetProfit
		}
	}
	if len(trades) > 0 {
		metrics.WinRate = float64(metrics.Wins) / float64(len(trades)) * 100
		metrics.AverageTrade = tradeReturn / float64(len(trades))
		metrics.AverageHoldingDays = float64(holding) / float64(len(trades))
	}
	if loss > 0 {
		metrics.ProfitFactor = profit / loss
	}
	metrics.Turnover = turnover / request.InitialCash * 100
	if benchmarkAvailable && !math.IsNaN(benchmarkReturn) && request.Benchmark != "" {
		metrics.ExcessReturn = metrics.TotalReturn - benchmarkReturn
	}
	return metrics
}
