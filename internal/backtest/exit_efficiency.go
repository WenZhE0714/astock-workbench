package backtest

import (
	"context"
	"fmt"
	"sort"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type ExitEfficiencyRow struct {
	Review             TradeReviewRow  `json:"review"`
	FirstPositiveDelta float64         `json:"first_positive_exit_profit_delta"`
	ProtectionSignal   *SignalSnapshot `json:"protection_signal,omitempty"`
	ProtectionFill     *Fill           `json:"protection_fill,omitempty"`
	ProtectionProfit   *float64        `json:"protection_profit,omitempty"`
	ProtectionDelta    float64         `json:"protection_profit_delta"`
}

type ExitEfficiencySummary struct {
	Trades                    int     `json:"trades"`
	Losses                    int     `json:"losses"`
	LossesWithCloseProfit     int     `json:"losses_with_close_profit"`
	LossesWithFirstOpenProfit int     `json:"losses_with_first_open_profit"`
	FirstPositiveProfitDelta  float64 `json:"first_positive_exit_profit_delta"`
	FirstPositiveWinnerDelta  float64 `json:"first_positive_winner_profit_delta"`
	ProtectionSignals         int     `json:"protection_signals"`
	ProtectionLosingFills     int     `json:"protection_losing_fills"`
	ProtectionProfitDelta     float64 `json:"protection_profit_delta"`
	ProtectionLossDelta       float64 `json:"protection_baseline_loss_profit_delta"`
	ProtectionWinnerDelta     float64 `json:"protection_baseline_winner_profit_delta"`
	ExitGapProfitChange       float64 `json:"exit_signal_to_fill_price_pnl"`
}

type ExitEfficiencyReview struct {
	Summary ExitEfficiencySummary `json:"summary"`
	Rows    []ExitEfficiencyRow   `json:"rows"`
}

func reviewExitEfficiency(ctx context.Context, provider DailyBarProvider, baseline Result) (ExitEfficiencyReview, error) {
	result := ExitEfficiencyReview{Rows: []ExitEfficiencyRow{}}
	barsBySymbol := map[string][]domain.DailyBar{}
	for _, trade := range baseline.Trades {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		bars, ok := barsBySymbol[trade.Symbol]
		if !ok {
			warmup := max(baseline.Request.Technical.SlowMA, baseline.Request.Technical.BreakoutDays+1, 21)
			var err error
			bars, err = provider.FetchDailyBarsRange(ctx, trade.Symbol, baseline.Request.Start.AddDate(0, 0, -warmup*3), baseline.Request.End, baseline.Request.Adjustment)
			if err != nil {
				return result, err
			}
			bars = append([]domain.DailyBar(nil), bars...)
			sort.SliceStable(bars, func(i, j int) bool { return bars[i].Date < bars[j].Date })
			barsBySymbol[trade.Symbol] = bars
		}
		row, err := reviewExitEfficiencyTrade(baseline.Request, trade, bars)
		if err != nil {
			return result, fmt.Errorf("%s: %w", trade.ID, err)
		}
		result.Rows = append(result.Rows, row)
		s := &result.Summary
		s.Trades++
		s.ExitGapProfitChange += row.Review.ExitPriceChangePnL
		s.FirstPositiveProfitDelta += row.FirstPositiveDelta
		s.ProtectionProfitDelta += row.ProtectionDelta
		if trade.NetProfit < 0 {
			s.Losses++
			if row.Review.MaxNetCloseReturn > 0 {
				s.LossesWithCloseProfit++
			}
			if row.Review.FirstPositiveNextOpenReturn != nil && *row.Review.FirstPositiveNextOpenReturn > 0 {
				s.LossesWithFirstOpenProfit++
			}
			s.ProtectionLossDelta += row.ProtectionDelta
		} else if trade.NetProfit > 0 {
			s.FirstPositiveWinnerDelta += row.FirstPositiveDelta
			s.ProtectionWinnerDelta += row.ProtectionDelta
		}
		if row.ProtectionFill != nil {
			s.ProtectionSignals++
			if *row.ProtectionProfit < 0 {
				s.ProtectionLosingFills++
			}
		}
	}
	return result, nil
}

func reviewExitEfficiencyTrade(request Request, trade Trade, bars []domain.DailyBar) (ExitEfficiencyRow, error) {
	review, err := reviewTradePath(request, trade, bars)
	row := ExitEfficiencyRow{Review: review}
	if err != nil {
		return row, err
	}
	entryCost := trade.Entry.Amount + trade.Entry.TotalFee
	if review.FirstPositiveNextOpenReturn != nil {
		row.FirstPositiveDelta = *review.FirstPositiveNextOpenReturn/100*entryCost - trade.NetProfit
	}
	position := &positionState{symbol: trade.Symbol, quantity: trade.Entry.Quantity, entrySignal: trade.EntrySignal, entry: trade.Entry, entryCost: entryCost}
	request.ExitProtection = ExitProtectHalfProfit
	for i, bar := range bars {
		// Once the original exit is pending, an overlay must not replace it.
		if bar.Date < trade.Entry.Date || bar.Date >= trade.ExitSignal.Date {
			continue
		}
		updateNetClosePeak(request, position, bar.Close, bar.Date)
		snapshot := SignalSnapshot{Date: bar.Date, Close: bar.Close}
		signal, _, triggered := profitProtectionSignal(snapshot, position, request)
		if !triggered {
			continue
		}
		for j := i + 1; j < len(bars) && bars[j].Date <= trade.Exit.Date; j++ {
			if onePriceBar(bars[j]) {
				continue
			}
			fill := makeFill(request, trade.Symbol, "sell", bars[j].Date, bars[j].Open, trade.Entry.Quantity, false)
			profit := fill.Amount - fill.TotalFee - entryCost
			row.ProtectionSignal, row.ProtectionFill, row.ProtectionProfit = &signal, &fill, &profit
			row.ProtectionDelta = profit - trade.NetProfit
			return row, nil
		}
		return row, nil
	}
	return row, nil
}
