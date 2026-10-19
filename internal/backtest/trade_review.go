package backtest

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type TradePathPoint struct {
	Date           string  `json:"date"`
	Close          float64 `json:"close"`
	NetCloseReturn float64 `json:"estimated_net_close_return_percent"`
	TPlusOneLocked bool    `json:"t_plus_one_locked"`
	OnePrice       bool    `json:"one_price"`
}

type TradeReviewRow struct {
	Trade                       Trade            `json:"trade"`
	OpeningGap                  float64          `json:"opening_gap_percent"`
	ExitCategory                string           `json:"exit_category"`
	SignalClosePnL              float64          `json:"signal_close_price_pnl"`
	ExitPriceChangePnL          float64          `json:"exit_signal_to_fill_price_pnl"`
	SlippageCost                float64          `json:"slippage_cost"`
	Fees                        float64          `json:"fees"`
	MaxNetCloseReturn           float64          `json:"max_estimated_net_close_return_percent"`
	MaxLaterNetCloseReturn      *float64         `json:"max_later_estimated_net_close_return_percent"`
	FirstPositiveCloseDate      string           `json:"first_positive_close_date,omitempty"`
	FirstPositiveNextOpenDate   string           `json:"first_positive_next_open_date,omitempty"`
	FirstPositiveNextOpenReturn *float64         `json:"first_positive_next_open_return_percent"`
	LossPath                    string           `json:"loss_path"`
	ExitLagSessions             int              `json:"exit_lag_sessions"`
	ExitLagCalendarDays         int              `json:"exit_lag_calendar_days"`
	Path                        []TradePathPoint `json:"path"`
}

type TradeReviewGroup struct {
	Label     string  `json:"label"`
	Trades    int     `json:"trades"`
	Wins      int     `json:"wins"`
	Losses    int     `json:"losses"`
	NetProfit float64 `json:"net_profit"`
}

type TradeReviewSummary struct {
	Trades                     int                `json:"trades"`
	Wins                       int                `json:"wins"`
	Losses                     int                `json:"losses"`
	NetProfit                  float64            `json:"net_profit"`
	GrossLoss                  float64            `json:"gross_loss"`
	Fees                       float64            `json:"fees"`
	NegativeExitPriceChange    float64            `json:"negative_exit_price_change"`
	PositiveExitPriceChange    float64            `json:"positive_exit_price_change"`
	SameDayExitSignalLosses    int                `json:"same_day_exit_signal_losses"`
	ShortHoldingLosses         int                `json:"holding_at_most_two_sessions_losses"`
	LossesWithPositiveNextOpen int                `json:"losses_with_positive_first_close_next_open"`
	ByOpeningGap               []TradeReviewGroup `json:"by_opening_gap"`
	ByExit                     []TradeReviewGroup `json:"by_exit"`
	ByLossPath                 []TradeReviewGroup `json:"by_loss_path"`
}

type TradeReviewWindow struct {
	ID      string             `json:"id"`
	Request Request            `json:"request"`
	Metrics Metrics            `json:"metrics"`
	Summary TradeReviewSummary `json:"summary"`
	Rows    []TradeReviewRow   `json:"rows"`
}

type TradeReviewResult struct {
	Completed   bool                `json:"completed"`
	SourceID    string              `json:"source_id"`
	CandidateID string              `json:"candidate_id"`
	GeneratedAt time.Time           `json:"generated_at"`
	Windows     []TradeReviewWindow `json:"windows"`
	Warnings    []string            `json:"warnings"`
}

func ReviewStudyTrades(ctx context.Context, provider DailyBarProvider, study RegimeStudyResult, progress func(string)) (TradeReviewResult, error) {
	result := TradeReviewResult{SourceID: study.SourceID, CandidateID: study.CandidateID, GeneratedAt: time.Now(), Windows: []TradeReviewWindow{}, Warnings: []string{"收盘净浮盈为估算标记，不代表能够在该收盘价成交", "盘中最高价与买入日T+1锁定的浮盈不能当作可兑现收益", "首次收盘浮盈后次日开盘退出仅为逐笔事后诊断，不是组合策略回测", "已见数据上的归因不能充当新最终留出"}}
	if provider == nil || !study.Completed || !study.BaselineReproduced {
		return result, fmt.Errorf("需要完整且基线复现成功的冻结研究")
	}
	var baseline *RegimeStudyVariant
	for _, variant := range study.Variants {
		if variant.Filter == "" {
			copy := variant
			baseline = &copy
			break
		}
	}
	if baseline == nil {
		return result, fmt.Errorf("未找到未过滤基线")
	}
	windows := append(append([]PositionStudyWindow{}, baseline.Windows...), baseline.Diagnostic)
	for _, window := range windows {
		if progress != nil {
			progress(window.ID + "逐笔诊断")
		}
		review := TradeReviewWindow{ID: window.ID, Request: window.Normal.Request, Metrics: window.Normal.Metrics, Rows: []TradeReviewRow{}}
		barsBySymbol := map[string][]domain.DailyBar{}
		for _, trade := range window.Normal.Trades {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			bars, ok := barsBySymbol[trade.Symbol]
			if !ok {
				warmup := max(review.Request.Technical.SlowMA, review.Request.Technical.BreakoutDays+1, 21)
				var err error
				bars, err = provider.FetchDailyBarsRange(ctx, trade.Symbol, review.Request.Start.AddDate(0, 0, -warmup*3), review.Request.End, review.Request.Adjustment)
				if err != nil {
					return result, fmt.Errorf("%s %s: %w", window.ID, trade.ID, err)
				}
				bars = append([]domain.DailyBar(nil), bars...)
				sort.SliceStable(bars, func(i, j int) bool { return bars[i].Date < bars[j].Date })
				barsBySymbol[trade.Symbol] = bars
			}
			row, err := reviewTradePath(review.Request, trade, bars)
			if err != nil {
				return result, fmt.Errorf("%s %s: %w", window.ID, trade.ID, err)
			}
			review.Rows = append(review.Rows, row)
		}
		review.Summary = summarizeTradeReviews(review.Rows)
		result.Windows = append(result.Windows, review)
	}
	result.Completed = true
	return result, nil
}

func reviewTradePath(request Request, trade Trade, bars []domain.DailyBar) (TradeReviewRow, error) {
	row := TradeReviewRow{Trade: trade, ExitCategory: tradeExitCategory(trade.ExitReason), Path: []TradePathPoint{}}
	if trade.Entry.Quantity <= 0 || trade.Entry.Quantity != trade.Exit.Quantity || trade.EntrySignal.Close <= 0 || trade.Entry.Amount <= 0 || trade.Entry.Price <= 0 || trade.Exit.RawPrice <= 0 {
		return row, fmt.Errorf("成交数量或入场信号价无效")
	}
	byDate := map[string]int{}
	for i, bar := range bars {
		byDate[bar.Date] = i
	}
	entry, entryOK := byDate[trade.Entry.Date]
	exit, exitOK := byDate[trade.Exit.Date]
	signal, signalOK := byDate[trade.ExitSignal.Date]
	entrySignal, entrySignalOK := byDate[trade.EntrySignal.Date]
	if !entryOK || !exitOK || !signalOK || !entrySignalOK || entrySignal >= entry || exit <= entry || signal < entry || signal > exit {
		return row, fmt.Errorf("冻结日K未覆盖交易路径")
	}
	exitPrice := bars[exit].Open
	if trade.Exit.Forced {
		exitPrice = bars[exit].Close
	}
	if math.Abs(trade.EntrySignal.Close-bars[entrySignal].Close) > 1e-6 || math.Abs(trade.Entry.RawPrice-bars[entry].Open) > 1e-6 || math.Abs(trade.Exit.RawPrice-exitPrice) > 1e-6 || math.Abs(trade.ExitSignal.Close-bars[signal].Close) > 1e-6 {
		return row, fmt.Errorf("成交价格与冻结日K不一致")
	}
	quantity := float64(trade.Entry.Quantity)
	row.OpeningGap = (trade.Entry.RawPrice/trade.EntrySignal.Close - 1) * 100
	row.SignalClosePnL = (trade.ExitSignal.Close - trade.Entry.RawPrice) * quantity
	row.ExitPriceChangePnL = (trade.Exit.RawPrice - trade.ExitSignal.Close) * quantity
	row.SlippageCost = ((trade.Entry.Price - trade.Entry.RawPrice) + (trade.Exit.RawPrice - trade.Exit.Price)) * quantity
	row.Fees = trade.Entry.TotalFee + trade.Exit.TotalFee
	if math.Abs(row.SignalClosePnL+row.ExitPriceChangePnL-row.SlippageCost-row.Fees-trade.NetProfit) > .02 {
		return row, fmt.Errorf("逐笔盈亏分解不平衡")
	}
	row.ExitLagSessions = exit - signal
	signalDate, _ := time.Parse(time.DateOnly, trade.ExitSignal.Date)
	exitDate, _ := time.Parse(time.DateOnly, trade.Exit.Date)
	row.ExitLagCalendarDays = int(exitDate.Sub(signalDate) / (24 * time.Hour))
	entryCost := trade.Entry.Amount + trade.Entry.TotalFee
	netReturn := func(price float64, date string) float64 {
		fill := makeFill(request, trade.Symbol, "sell", date, price, trade.Entry.Quantity, false)
		return (fill.Amount - fill.TotalFee - entryCost) / entryCost * 100
	}
	last := exit - 1
	if trade.Exit.Forced {
		last = exit
	}
	firstPositive := -1
	for i := entry; i <= last; i++ {
		bar := bars[i]
		value := netReturn(bar.Close, bar.Date)
		if i == entry || value > row.MaxNetCloseReturn {
			row.MaxNetCloseReturn = value
		}
		if i > entry && (row.MaxLaterNetCloseReturn == nil || value > *row.MaxLaterNetCloseReturn) {
			copy := value
			row.MaxLaterNetCloseReturn = &copy
		}
		if value > 0 && firstPositive < 0 {
			firstPositive = i
			row.FirstPositiveCloseDate = bar.Date
		}
		row.Path = append(row.Path, TradePathPoint{Date: bar.Date, Close: bar.Close, NetCloseReturn: value, TPlusOneLocked: i == entry, OnePrice: onePriceBar(bar)})
	}
	if firstPositive >= 0 {
		for i := firstPositive + 1; i <= exit; i++ {
			if onePriceBar(bars[i]) {
				continue
			}
			value := netReturn(bars[i].Open, bars[i].Date)
			row.FirstPositiveNextOpenDate, row.FirstPositiveNextOpenReturn = bars[i].Date, &value
			break
		}
	}
	if trade.NetProfit < 0 {
		switch {
		case row.MaxLaterNetCloseReturn != nil && *row.MaxLaterNetCloseReturn > 0:
			row.LossPath = "后续交易日收盘曾净浮盈"
		case row.MaxNetCloseReturn > 0:
			row.LossPath = "仅买入当日收盘净浮盈"
		default:
			row.LossPath = "持有期收盘未曾净浮盈"
		}
	} else {
		row.LossPath = "盈利或持平"
	}
	return row, nil
}

func tradeExitCategory(reason string) string {
	switch {
	case strings.Contains(reason, "止损"):
		return "止损"
	case strings.Contains(reason, "止盈"):
		return "止盈"
	case strings.Contains(reason, "趋势收复失效"):
		return "跌回快均线"
	case strings.Contains(reason, "持有达到"):
		return "持有期届满"
	case strings.Contains(reason, "回测期末"):
		return "期末清仓"
	default:
		return "其他"
	}
}

func summarizeTradeReviews(rows []TradeReviewRow) TradeReviewSummary {
	summary := TradeReviewSummary{Trades: len(rows)}
	for _, row := range rows {
		profit := row.Trade.NetProfit
		summary.NetProfit += profit
		summary.Fees += row.Fees
		if row.ExitPriceChangePnL < 0 {
			summary.NegativeExitPriceChange -= row.ExitPriceChangePnL
		} else {
			summary.PositiveExitPriceChange += row.ExitPriceChangePnL
		}
		if profit > 0 {
			summary.Wins++
		}
		if profit < 0 {
			summary.Losses++
			summary.GrossLoss -= profit
			if row.Trade.ExitSignal.Date == row.Trade.Entry.Date {
				summary.SameDayExitSignalLosses++
			}
			if row.Trade.HoldingDays <= 2 {
				summary.ShortHoldingLosses++
			}
			if row.FirstPositiveNextOpenReturn != nil && *row.FirstPositiveNextOpenReturn > 0 {
				summary.LossesWithPositiveNextOpen++
			}
		}
	}
	group := func(key func(TradeReviewRow) string) []TradeReviewGroup {
		groups := map[string]*TradeReviewGroup{}
		for _, row := range rows {
			label := key(row)
			if groups[label] == nil {
				groups[label] = &TradeReviewGroup{Label: label}
			}
			g := groups[label]
			g.Trades++
			g.NetProfit += row.Trade.NetProfit
			if row.Trade.NetProfit > 0 {
				g.Wins++
			}
			if row.Trade.NetProfit < 0 {
				g.Losses++
			}
		}
		out := []TradeReviewGroup{}
		for _, g := range groups {
			out = append(out, *g)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
		return out
	}
	summary.ByOpeningGap = group(func(row TradeReviewRow) string {
		switch {
		case row.OpeningGap <= 0:
			return "不高开"
		case row.OpeningGap <= 3:
			return "高开0至3%"
		case row.OpeningGap <= 5:
			return "高开3至5%"
		default:
			return "高开超过5%"
		}
	})
	summary.ByExit = group(func(row TradeReviewRow) string { return row.ExitCategory })
	summary.ByLossPath = group(func(row TradeReviewRow) string { return row.LossPath })
	return summary
}
