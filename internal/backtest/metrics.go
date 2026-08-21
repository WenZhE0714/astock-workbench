package backtest

import (
	"math"
	"sort"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/marketregime"
)

// EnrichRiskMetrics recalculates metrics that can be derived solely from the
// persisted equity curve. It keeps the original trade/fee fields intact so
// older archived runs can be rendered with the current risk vocabulary.
func EnrichRiskMetrics(result *Result) {
	if result == nil || len(result.Equity) < 2 {
		return
	}
	metrics := &result.Metrics
	returns := make([]float64, 0, len(result.Equity)-1)
	for index := 1; index < len(result.Equity); index++ {
		previous := result.Equity[index-1].Equity
		current := result.Equity[index].Equity
		if previous > 0 && current > 0 {
			returns = append(returns, current/previous-1)
		}
	}
	if len(returns) == 0 {
		return
	}
	mean := 0.0
	metrics.BestDay = returns[0] * 100
	metrics.WorstDay = returns[0] * 100
	downsideSquares := 0.0
	for _, value := range returns {
		mean += value
		metrics.BestDay = math.Max(metrics.BestDay, value*100)
		metrics.WorstDay = math.Min(metrics.WorstDay, value*100)
		if value < 0 {
			downsideSquares += value * value
		}
	}
	mean /= float64(len(returns))
	if len(returns) > 1 {
		variance := 0.0
		for _, value := range returns {
			variance += (value - mean) * (value - mean)
		}
		variance /= float64(len(returns) - 1)
		if variance > 0 {
			volatility := math.Sqrt(variance)
			metrics.AnnualizedVolatility = volatility * math.Sqrt(252) * 100
			metrics.Sharpe = mean / volatility * math.Sqrt(252)
		}
	}
	downsideDeviation := math.Sqrt(downsideSquares / float64(len(returns)))
	if downsideDeviation > 0 {
		metrics.Sortino = mean / downsideDeviation * math.Sqrt(252)
	}
	if metrics.MaxDrawdown < 0 {
		metrics.Calmar = metrics.AnnualizedReturn / math.Abs(metrics.MaxDrawdown)
	}
}

// EnrichMarketRegimeMetrics lets older archives participate in the current
// research view when their persisted benchmark curve is available. New runs
// persist a richer calculation that includes pre-period benchmark warmup.
func EnrichMarketRegimeMetrics(result *Result) {
	if result == nil || len(result.MarketRegimes) > 0 || len(result.BenchmarkEquity) == 0 {
		return
	}
	bars := make([]domain.DailyBar, 0, len(result.BenchmarkEquity))
	for _, point := range result.BenchmarkEquity {
		bars = append(bars, domain.DailyBar{Date: point.Date, Close: point.Close})
	}
	result.MarketRegimes = calculateMarketRegimeMetrics(result.Equity, result.Trades, bars)
}

type regimeAccumulator struct {
	regime                marketregime.Regime
	days                  int
	trades                int
	benchmarkDays         int
	strategyFactor        float64
	matchedStrategyFactor float64
	benchmarkFactor       float64
	peak                  float64
	maxDrawdown           float64
}

func newRegimeAccumulator(regime marketregime.Regime) *regimeAccumulator {
	return &regimeAccumulator{
		regime: regime, strategyFactor: 1, matchedStrategyFactor: 1,
		benchmarkFactor: 1, peak: 1,
	}
}

func calculateMarketRegimeMetrics(equity []EquityPoint, trades []Trade, benchmarkBars []domain.DailyBar) []MarketRegimeMetrics {
	benchmarkBars = normalizedRegimeBars(benchmarkBars)
	if len(equity) < 2 || len(benchmarkBars) == 0 {
		return nil
	}
	closes := make(map[string]float64, len(benchmarkBars))
	for _, bar := range benchmarkBars {
		closes[bar.Date] = bar.Close
	}
	groups := make(map[marketregime.Regime]*regimeAccumulator)
	groupFor := func(regime marketregime.Regime) *regimeAccumulator {
		group := groups[regime]
		if group == nil {
			group = newRegimeAccumulator(regime)
			groups[regime] = group
		}
		return group
	}
	for index := 1; index < len(equity); index++ {
		previous := equity[index-1]
		current := equity[index]
		if previous.Equity <= 0 || current.Equity <= 0 {
			continue
		}
		regime := marketregime.ClassifyBefore(benchmarkBars, current.Date)
		group := groupFor(regime)
		strategyReturn := current.Equity/previous.Equity - 1
		group.days++
		group.strategyFactor *= 1 + strategyReturn
		if group.strategyFactor > group.peak {
			group.peak = group.strategyFactor
		}
		if group.peak > 0 {
			group.maxDrawdown = math.Min(group.maxDrawdown, group.strategyFactor/group.peak-1)
		}
		previousClose, previousOK := closes[previous.Date]
		currentClose, currentOK := closes[current.Date]
		if previousOK && currentOK && previousClose > 0 && currentClose > 0 {
			group.benchmarkDays++
			group.matchedStrategyFactor *= 1 + strategyReturn
			group.benchmarkFactor *= currentClose / previousClose
		}
	}
	for _, trade := range trades {
		if trade.Entry.Date == "" {
			continue
		}
		groupFor(marketregime.ClassifyBefore(benchmarkBars, trade.Entry.Date)).trades++
	}
	result := make([]MarketRegimeMetrics, 0, len(groups))
	for _, regime := range marketregime.Ordered() {
		group := groups[regime]
		if group == nil || (group.days == 0 && group.trades == 0) {
			continue
		}
		item := MarketRegimeMetrics{
			Key: string(regime), Label: string(regime), Days: group.days, Trades: group.trades,
			ReturnPercent: (group.strategyFactor - 1) * 100,
			MaxDrawdown:   group.maxDrawdown * 100,
			BenchmarkDays: group.benchmarkDays,
		}
		if group.benchmarkDays > 0 {
			item.BenchmarkAvailable = true
			item.BenchmarkReturn = (group.benchmarkFactor - 1) * 100
			// Match the portfolio-level and realtime contracts: excess is the
			// percentage-point difference between strategy and benchmark returns.
			item.ExcessReturn = (group.matchedStrategyFactor - group.benchmarkFactor) * 100
		}
		result = append(result, item)
	}
	return result
}

func normalizedRegimeBars(input []domain.DailyBar) []domain.DailyBar {
	result := make([]domain.DailyBar, 0, len(input))
	for _, bar := range input {
		if len(bar.Date) != len("2006-01-02") || bar.Close <= 0 || math.IsNaN(bar.Close) || math.IsInf(bar.Close, 0) {
			continue
		}
		result = append(result, bar)
	}
	sort.SliceStable(result, func(left, right int) bool { return result[left].Date < result[right].Date })
	unique := result[:0]
	for _, bar := range result {
		if len(unique) > 0 && unique[len(unique)-1].Date == bar.Date {
			unique[len(unique)-1] = bar
			continue
		}
		unique = append(unique, bar)
	}
	return unique
}

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
		if len(returns) > 0 {
			mean := 0.0
			for _, value := range returns {
				mean += value
			}
			mean /= float64(len(returns))
			variance := 0.0
			downsideSquares := 0.0
			metrics.BestDay = returns[0] * 100
			metrics.WorstDay = returns[0] * 100
			for _, value := range returns {
				variance += (value - mean) * (value - mean)
				if value < 0 {
					downsideSquares += value * value
				}
				metrics.BestDay = math.Max(metrics.BestDay, value*100)
				metrics.WorstDay = math.Min(metrics.WorstDay, value*100)
			}
			if len(returns) > 1 {
				variance /= float64(len(returns) - 1)
				if variance > 0 {
					volatility := math.Sqrt(variance)
					metrics.AnnualizedVolatility = volatility * math.Sqrt(252) * 100
					metrics.Sharpe = mean / volatility * math.Sqrt(252)
				}
			}
			downsideDeviation := math.Sqrt(downsideSquares / float64(len(returns)))
			if downsideDeviation > 0 {
				metrics.Sortino = mean / downsideDeviation * math.Sqrt(252)
			}
		}
	}
	for _, point := range equity {
		if point.Drawdown < metrics.MaxDrawdown {
			metrics.MaxDrawdown = point.Drawdown
		}
	}
	if metrics.MaxDrawdown < 0 {
		metrics.Calmar = metrics.AnnualizedReturn / math.Abs(metrics.MaxDrawdown)
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
