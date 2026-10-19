package backtest

import (
	"math"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func tradeReviewFixture() (Request, Trade, []domain.DailyBar) {
	request := testRequest()
	bars := []domain.DailyBar{
		{Date: "2026-01-01", Open: 10, Close: 10, High: 10.1, Low: 9.9},
		{Date: "2026-01-02", Open: 10, Close: 11, High: 12, Low: 9.9},
		{Date: "2026-01-03", Open: 9, Close: 9, High: 9.1, Low: 8.9},
		{Date: "2026-01-10", Open: 8.5, Close: 100, High: 101, Low: 8},
	}
	entry := makeFill(request, "sh600000", "buy", bars[1].Date, bars[1].Open, 100, false)
	exit := makeFill(request, "sh600000", "sell", bars[3].Date, bars[3].Open, 100, false)
	trade := Trade{Symbol: "sh600000", EntrySignal: SignalSnapshot{Date: bars[0].Date, Close: 10}, Entry: entry, ExitSignal: SignalSnapshot{Date: bars[2].Date, Close: 9}, Exit: exit, HoldingDays: 2, NetProfit: exit.Amount - exit.TotalFee - entry.Amount - entry.TotalFee, ExitReason: "趋势收复失效"}
	return request, trade, bars
}

func TestTradeReviewSeparatesLockedProfitFromNextOpen(t *testing.T) {
	request, trade, bars := tradeReviewFixture()
	row, err := reviewTradePath(request, trade, bars)
	if err != nil {
		t.Fatal(err)
	}
	if row.LossPath != "仅买入当日收盘净浮盈" || row.MaxNetCloseReturn <= 0 || row.MaxLaterNetCloseReturn == nil || *row.MaxLaterNetCloseReturn >= 0 {
		t.Fatalf("locked profit misclassified: %+v", row)
	}
	if row.FirstPositiveNextOpenReturn == nil || *row.FirstPositiveNextOpenReturn >= 0 || row.FirstPositiveNextOpenDate != "2026-01-03" {
		t.Fatal("overnight loss hidden")
	}
	if len(row.Path) != 2 || !row.Path[0].TPlusOneLocked || row.Path[1].TPlusOneLocked || row.MaxNetCloseReturn > 20 {
		t.Fatal("post-exit close or T+1 leaked")
	}
	if row.ExitLagSessions != 1 || row.ExitLagCalendarDays != 7 {
		t.Fatal("calendar gap mistaken for trading delay")
	}
	if math.Abs(row.SignalClosePnL+row.ExitPriceChangePnL-row.SlippageCost-row.Fees-trade.NetProfit) > .02 {
		t.Fatal("pnl decomposition failed")
	}
}

func TestTradeReviewSkipsLockedLimitBarAndRejectsChangedPrices(t *testing.T) {
	request, trade, bars := tradeReviewFixture()
	bars[2].High, bars[2].Low = 9, 9
	row, err := reviewTradePath(request, trade, bars)
	if err != nil {
		t.Fatal(err)
	}
	if row.FirstPositiveNextOpenDate != "2026-01-10" {
		t.Fatal("one-price bar treated as executable exit")
	}
	trade.EntrySignal.Close = 9
	if _, err := reviewTradePath(request, trade, bars); err == nil {
		t.Fatal("mismatched signal price accepted")
	}
}

func TestTradeReviewGroupsReconcileWithoutDoubleCounting(t *testing.T) {
	request, trade, bars := tradeReviewFixture()
	row, err := reviewTradePath(request, trade, bars)
	if err != nil {
		t.Fatal(err)
	}
	summary := summarizeTradeReviews([]TradeReviewRow{row})
	for _, groups := range [][]TradeReviewGroup{summary.ByOpeningGap, summary.ByExit, summary.ByLossPath} {
		net, count := 0.0, 0
		for _, g := range groups {
			net += g.NetProfit
			count += g.Trades
		}
		if math.Abs(net-summary.NetProfit) > .02 || count != summary.Trades {
			t.Fatal("diagnostic dimension does not reconcile")
		}
	}
	if summary.ShortHoldingLosses != 1 || summary.LossesWithPositiveNextOpen != 0 {
		t.Fatal("loss counters incorrect")
	}
}
