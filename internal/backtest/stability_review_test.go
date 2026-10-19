package backtest

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func stabilityFixture(year int, open bool) (Result, []domain.DailyBar) {
	start := time.Date(year, 12, 30, 0, 0, 0, 0, time.UTC)
	date := func(offset int) string { return start.AddDate(0, 0, offset).Format(time.DateOnly) }
	bars := []domain.DailyBar{
		{Date: date(-1), Open: 10, Close: 10.2, High: 10.3, Low: 9.9},
		{Date: date(0), Open: 10, Close: 11, High: 11.2, Low: 9.9},
		{Date: date(4), Open: 11, Close: 12, High: 12.2, Low: 10.9},
		{Date: date(5), Open: 11.5, Close: 100, High: 101, Low: 11},
	}
	r := testRequest()
	r.Tickers, r.Names = []string{"sh600000"}, map[string]string{"sh600000": "test"}
	r.Start, r.End, r.InitialCash = start, start.AddDate(0, 0, 5), 10000
	r.Technical.EntryMode, r.Technical.MaxPosition, r.NoFutureData = EntryModeReclaim, .1, true
	entry := makeFill(r, "sh600000", "buy", date(0), 10, 100, false)
	exit := makeFill(r, "sh600000", "sell", date(5), 11.5, 100, false)
	cost := entry.Amount + entry.TotalFee
	profit := exit.Amount - exit.TotalFee - cost
	signal := SignalSnapshot{Date: date(-1), Close: 10.2, FastMA: 10, SlowMA: 9.8, VolumeRatio: 1.2, Return20: .05}
	trade := Trade{ID: "T0001", Symbol: "sh600000", Name: "test", EntrySignal: signal, Entry: entry, ExitSignal: SignalSnapshot{Date: date(4), Close: 12}, Exit: exit, NetProfit: profit, ReturnPercent: profit / cost * 100}
	run := Result{Request: r, Trades: []Trade{trade}, Equity: []EquityPoint{{Date: date(0), Cash: r.InitialCash - cost, Positions: 1100, Equity: r.InitialCash - cost + 1100}, {Date: date(4), Cash: r.InitialCash - cost, Positions: 1200, Equity: r.InitialCash - cost + 1200}, {Date: date(5), Cash: r.InitialCash + profit, Equity: r.InitialCash + profit}}, Metrics: Metrics{FinalEquity: r.InitialCash + profit, TotalFees: entry.TotalFee + exit.TotalFee, Trades: 1, TotalReturn: profit / r.InitialCash * 100}}
	if open {
		run.Request.End = start.AddDate(0, 0, 4)
		run.Trades = []Trade{}
		run.Equity = run.Equity[:2]
		run.OpenPositions = []OpenPosition{{Symbol: "sh600000", Name: "test", EntrySignal: signal, Entry: entry, LastDate: date(4), LastPrice: 12, Quantity: 100, MarketValue: 1200, UnrealizedProfit: 1200 - cost, ReturnPercent: (1200 - cost) / cost * 100}}
		run.Metrics = Metrics{FinalEquity: r.InitialCash + 1200 - cost, TotalFees: entry.TotalFee, TotalReturn: (1200 - cost) / r.InitialCash * 100}
	}
	return run, bars
}

func TestStabilitySplitsCrossYearProfitAndExcludesExitDayClose(t *testing.T) {
	run, bars := stabilityFixture(2022, false)
	result, err := analyzeStabilityRun(context.Background(), historyRangeMock{bars: bars}, run, "F01")
	if err != nil {
		t.Fatal(err)
	}
	firstYear := 1100 - run.Trades[0].Entry.Amount - run.Trades[0].Entry.TotalFee
	if len(result.Years) != 2 || math.Abs(result.Years[0].Profit-firstYear) > 1e-8 || math.Abs(result.Years[1].Profit-(run.Trades[0].NetProfit-firstYear)) > 1e-8 || result.MaxDailyError > 1e-8 {
		t.Fatalf("wrong calendar attribution: %+v", result.Years)
	}
	if math.Abs(result.Stats.TotalProfit-run.Trades[0].NetProfit) > 1e-8 || result.Stats.TotalProfit > 200 {
		t.Fatal("exit day's close contaminated the position's profit")
	}
	for _, dimension := range result.Signals {
		profit, count := 0.0, 0
		for _, group := range dimension.Groups {
			profit += group.Stats.TotalProfit
			count += group.Stats.ClosedTrades
		}
		if count != 1 || math.Abs(profit-result.Stats.TotalProfit) > 1e-8 {
			t.Fatal("signal dimension does not reconcile")
		}
	}
	run.Equity[1].Equity++
	if _, err := analyzeStabilityRun(context.Background(), historyRangeMock{bars: bars}, run, "F01"); err == nil {
		t.Fatal("intermediate equity drift was hidden")
	}
}

func TestStabilityIncludesOpenPositionsWithoutInventingWinsOrExitFees(t *testing.T) {
	run, bars := stabilityFixture(2022, true)
	result, err := analyzeStabilityRun(context.Background(), historyRangeMock{bars: bars}, run, "F01")
	if err != nil || result.Stats.OpenPositions != 1 || result.Stats.ClosedTrades != 0 || result.Stats.Wins != 0 || result.Stats.Fees != run.Metrics.TotalFees || len(result.Concentration.TopWinners) != 0 || math.Abs(result.Stats.UnrealizedProfit-run.OpenPositions[0].UnrealizedProfit) > 1e-8 {
		t.Fatalf("open position accounting wrong: %+v %v", result.Stats, err)
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatal("zero-winner concentration produced non-finite values")
	}
}

func TestStabilityFixedSignalBinsAndConcentrationDenominators(t *testing.T) {
	signal := SignalSnapshot{Close: 103, FastMA: 100, SlowMA: 100 / 1.1, Return20: .1, VolumeRatio: 1.5}
	if stabilitySignalBucket("ma_distance", signal) != "middle" || stabilitySignalBucket("return_20", signal) != "high" || stabilitySignalBucket("volume_ratio", signal) != "high" {
		t.Fatal("fixed bucket boundaries changed")
	}
	signal.FastMA = 0
	if stabilitySignalBucket("ma_distance", signal) != "unknown" {
		t.Fatal("missing MA was classified as an ordinary signal")
	}
	rows := []StabilityRow{{ID: "a", Symbol: "winner", Closed: true, Profit: 100}, {ID: "b", Symbol: "winner", Closed: true, Profit: -20}, {ID: "c", Symbol: "other", Closed: true, Profit: 50}, {ID: "d", Symbol: "loser", Closed: true, Profit: -100}}
	result := summarizeStabilityRows(rows, nil)
	c := result.Concentration
	if math.Abs(c.TopOneWinnerShare-100.0/150) > 1e-8 || math.Abs(c.TopOneSymbolShare-80.0/130) > 1e-8 || c.ProfitMinusTopFive != -120 || c.ProfitMinusTopThreeSymbols != -100 {
		t.Fatalf("concentration used net profit as denominator: %+v", c)
	}
}

func TestStabilityRejectsSparseGroupsAndWindowDirectionChanges(t *testing.T) {
	windows := []StabilityWindow{}
	for _, id := range []string{"F01", "F02", "F03", "F04"} {
		rows := []StabilityRow{}
		for i := 0; i < 5; i++ {
			rows = append(rows, StabilityRow{WindowID: id, Symbol: "loss", Closed: true, Profit: -10, Signal: SignalSnapshot{VolumeRatio: 1.2}})
		}
		a := summarizeStabilityRows(rows, nil)
		windows = append(windows, StabilityWindow{ID: id, Normal: a, DoubleCost: a})
	}
	check := func() StabilityGroupCheck {
		a, b := aggregateStabilityWindows(windows, false), aggregateStabilityWindows(windows, true)
		for _, group := range stabilityGroupChecks(windows, a, b) {
			if group.Dimension == "volume_ratio" && group.Key == "middle" {
				return group
			}
		}
		t.Fatal("missing group")
		return StabilityGroupCheck{}
	}
	if g := check(); !g.SufficientSample || !g.AllNegative || g.AllPositive {
		t.Fatal("adequate consistently losing group not identified")
	}
	windows[3].Normal = summarizeStabilityRows([]StabilityRow{}, nil)
	if g := check(); g.SufficientSample || g.AllNegative {
		t.Fatal("empty window counted as stable loss")
	}
	rows := append([]StabilityRow(nil), windows[2].Normal.Rows...)
	for i := range rows {
		rows[i].Profit = 10
	}
	windows[3].Normal = summarizeStabilityRows(rows, nil)
	if g := check(); !g.SufficientSample || g.AllNegative || g.AllPositive {
		t.Fatal("direction change hidden by aggregate losses")
	}
}

type stabilityReplayEngine struct {
	runs  []Result
	calls int
}

func (engine *stabilityReplayEngine) Run(_ context.Context, request Request) (Result, error) {
	result := engine.runs[engine.calls]
	engine.calls++
	return result, nil
}

func TestStabilityReviewReplaysBaselineAndKeepsSeenPeriodSeparate(t *testing.T) {
	reference := PositionStudyVariant{MaxPosition: .1}
	engine := &stabilityReplayEngine{}
	bars := []domain.DailyBar{}
	for i, id := range []string{"F01", "F02", "seen-period"} {
		run, history := stabilityFixture(2022+i, false)
		window := PositionStudyWindow{ID: id, Normal: run, DoubleCost: run}
		if i < 2 {
			reference.Windows = append(reference.Windows, window)
		} else {
			reference.Diagnostic = window
		}
		engine.runs = append(engine.runs, run, run)
		bars = append(bars, history...)
	}
	result, err := RunStabilityReview(context.Background(), engine, historyRangeMock{bars: bars}, "source", "P008", reference, nil)
	if err != nil || !result.Completed || !result.BaselineReproduced || result.PromotionAllowed || engine.calls != 6 || result.NormalValidation.Stats.ClosedTrades != 2 || len(result.Windows) != 3 {
		t.Fatalf("invalid replay or seen-period isolation: %+v %v", result.NormalValidation.Stats, err)
	}
	if !reflect.DeepEqual(result.Baseline, reference) {
		t.Fatal("source baseline mutated")
	}
	engine.calls = 0
	engine.runs[0].Metrics.TotalReturn++
	result, err = RunStabilityReview(context.Background(), engine, historyRangeMock{bars: bars}, "source", "P008", reference, nil)
	if err == nil || result.Completed || result.BaselineReproduced || engine.calls != 1 {
		t.Fatal("baseline drift accepted")
	}
}
