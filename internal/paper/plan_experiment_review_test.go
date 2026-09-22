package paper

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func closedReviewExperiment(t *testing.T) (PlanExperiment, domain.TradePlan) {
	t.Helper()
	state, plan, bars := experimentFixture(t)
	at := time.Date(2026, 9, 21, 10, 0, 0, 0, shanghaiLocation)
	for _, price := range []float64{103, 103.1, 101, 101.1, 94, 94} {
		state = advanceExperimentTest(t, state, experimentInput(plan, bars, at, price), at.Add(time.Second))
		at = at.Add(30 * time.Second)
	}
	at = time.Date(2026, 9, 22, 9, 31, 0, 0, shanghaiLocation)
	state = advanceExperimentTest(t, state, experimentInput(plan, bars, at, 96), at.Add(time.Second))
	for _, arm := range state.Arms {
		if arm.Trials[plan.ID].Status != "closed" || len(arm.Report.Trades) != 1 {
			t.Fatal("fixture has not closed both positions")
		}
	}
	return state, plan
}

func TestPlanExperimentReviewUsesActualFeesAndMatchedCompletedPlans(t *testing.T) {
	state, plan := closedReviewExperiment(t)
	before, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	review := BuildPlanExperimentReview(state)
	if review.PairedCompleted != 1 || review.RangeHigher != 1 || len(review.Plans) != 1 || len(review.Warnings) != 0 {
		t.Fatalf("paired review failed: %+v", review)
	}
	row := review.Plans[0]
	if row.Range.EntryAt == row.Confirmed.EntryAt || row.Range.EntryPrice == nil || row.Range.ExitAt != "2026-09-22 09:31:00" || row.Range.ExitReason == "" {
		t.Fatalf("entry/exit evidence lost: %+v", row)
	}
	for index, fill := range []PlanExperimentReviewFill{row.Range, row.Confirmed} {
		buy, sell := state.Arms[index].Report.Orders[0], state.Arms[index].Report.Orders[1]
		cfg := state.Arms[index].Report.Config
		entryFee := math.Max(cfg.MinimumCommission, buy.Amount*cfg.CommissionRate) + buy.Amount*cfg.TransferFeeRate
		exitFee := math.Max(cfg.MinimumCommission, sell.Amount*cfg.CommissionRate) + sell.Amount*(cfg.TransferFeeRate+cfg.StampDutyRate)
		net := (sell.Price-buy.Price)*float64(buy.Quantity) - entryFee - exitFee
		risk := (buy.Price - plan.Structure.Plan.Invalidation) * float64(buy.Quantity)
		if fill.NetProfit == nil || fill.Fees == nil || fill.NetR == nil || math.Abs(*fill.NetProfit-net) > 1e-7 || math.Abs(*fill.NetR-net/risk) > 1e-9 || math.Abs(*fill.Fees-entryFee-exitFee) > 1e-7 {
			t.Fatalf("fee-adjusted results incorrect: %+v", fill)
		}
		if math.Abs(*fill.NetProfit-*review.Arms[index].NetProfit) > 1e-7 {
			t.Fatal("arm aggregation differs from completed ledger")
		}
	}
	if row.NetRDifference == nil || math.Abs(*review.AverageNetRDifference-*row.NetRDifference) > 1e-9 || review.NetRSamples != 1 {
		t.Fatalf("paired R aggregation: %+v", review)
	}
	after, _ := json.Marshal(state)
	if string(before) != string(after) {
		t.Fatal("read-only review rewrote the ledger")
	}
	state.ValuationComplete = false
	selection := state.Selections[plan.ID]
	selection.Enabled = false
	state.Selections[plan.ID] = selection
	review = BuildPlanExperimentReview(state)
	if review.PairedCompleted != 1 || review.Plans[0].Selected {
		t.Fatal("removed plan or stale current quotes erased a completed review")
	}
}

func TestPlanExperimentReviewDoesNotLabelOpenOrOneSidedPlansAsZeroReturn(t *testing.T) {
	state, plan, bars := experimentFixture(t)
	review := BuildPlanExperimentReview(state)
	if review.Waiting != 1 || review.ProfitDifference != nil || review.Arms[0].NetProfit != nil {
		t.Fatalf("untraded plan became a zero-return sample: %+v", review)
	}
	at := time.Date(2026, 9, 21, 10, 0, 0, 0, shanghaiLocation)
	for _, price := range []float64{103, 103.1} {
		state = advanceExperimentTest(t, state, experimentInput(plan, bars, at, price), at.Add(time.Second))
		at = at.Add(30 * time.Second)
	}
	review = BuildPlanExperimentReview(state)
	if review.SingleSided != 1 || review.PairedCompleted != 0 || review.AverageReturnDifference != nil || review.Plans[0].Range.NetR != nil || review.Plans[0].Confirmed.NetR != nil {
		t.Fatalf("single-sided entry entered the comparison: %+v", review)
	}
	for _, price := range []float64{101, 101.1} {
		state = advanceExperimentTest(t, state, experimentInput(plan, bars, at, price), at.Add(time.Second))
		at = at.Add(30 * time.Second)
	}
	review = BuildPlanExperimentReview(state)
	if review.OpenPairs != 1 || review.PairedCompleted != 0 || review.Arms[0].Open != 1 || review.Arms[0].Fees == nil || *review.Arms[0].Fees <= 0 {
		t.Fatalf("open plan comparison or fees incorrect: %+v", review)
	}
}

func TestPlanExperimentReviewExcludesIncompleteOrMismatchedLedger(t *testing.T) {
	original, plan := closedReviewExperiment(t)
	for _, kind := range []string{"missing-order", "missing-trade", "duplicate-trade", "fees", "quantity", "date", "same-day", "before-observation", "config", "plan-id", "symbol"} {
		t.Run(kind, func(t *testing.T) {
			state, err := clonePlanExperiment(original)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing-order":
				state.Arms[0].Report.Orders = state.Arms[0].Report.Orders[1:]
			case "missing-trade":
				state.Arms[0].Report.Trades = nil
			case "duplicate-trade":
				state.Arms[0].Report.Trades = append(state.Arms[0].Report.Trades, state.Arms[0].Report.Trades[0])
			case "fees":
				state.Arms[0].Report.Trades[0].TotalFee = 0
			case "quantity":
				state.Arms[0].Report.Orders[1].Quantity--
			case "date":
				state.Arms[0].Report.Orders[0].ExecutionTime = ""
			case "same-day":
				state.Arms[0].Report.Orders[1].ExecutionTime = "2026-09-21 14:00:00"
			case "before-observation":
				trial := state.Arms[0].Trials[plan.ID]
				entryAt, _ := parseRealtimeTimestamp(state.Arms[0].Report.Orders[0].ExecutionTime)
				trial.EntryTriggerAt = entryAt.Add(time.Second)
				state.Arms[0].Trials[plan.ID] = trial
			case "config":
				state.Arms[0].Report.Config.CommissionRate *= 2
			case "plan-id":
				state.Arms[0].Report.Orders[0].PlanID = strings.Repeat("f", 64)
			case "symbol":
				state.Arms[0].Report.Trades[0].Symbol = "sz000001"
			}
			review := BuildPlanExperimentReview(state)
			if review.Unavailable != 1 || review.PairedCompleted != 0 || review.ProfitDifference != nil || review.Plans[0].PlanID != plan.ID || len(review.Warnings) == 0 {
				t.Fatalf("unqualified result became comparable: %+v", review)
			}
		})
	}
}

func TestPlanExperimentReviewPreservesBreakEvenAsMeasuredZero(t *testing.T) {
	state, _ := closedReviewExperiment(t)
	for index := range state.Arms {
		report := &state.Arms[index].Report
		buy, sell := report.Orders[0], &report.Orders[1]
		cfg := report.Config
		buyFee := transactionFee(buy.Amount, "buy", cfg)
		sell.Amount = (buy.Amount + buyFee) / (1 - cfg.CommissionRate - cfg.StampDutyRate - cfg.TransferFeeRate)
		sell.Price = sell.Amount / float64(sell.Quantity)
		trade := &report.Trades[0]
		trade.ExitPrice = sell.Price
		trade.TotalFee = buyFee + transactionFee(sell.Amount, "sell", cfg)
		trade.GrossProfit, trade.NetProfit = sell.Amount-buy.Amount, 0
	}
	review := BuildPlanExperimentReview(state)
	if review.PairedCompleted != 1 || review.EqualReturn != 1 || review.AverageNetRDifference == nil || *review.AverageNetRDifference != 0 {
		t.Fatalf("break-even comparison treated as missing: %+v", review)
	}
	for _, arm := range review.Arms {
		if arm.Completed != 1 || arm.NetProfit == nil || *arm.NetProfit != 0 || arm.AverageNetR == nil || *arm.AverageNetR != 0 || arm.Fees == nil || *arm.Fees <= 0 {
			t.Fatalf("break-even arm lost actual zero: %+v", arm)
		}
	}
}

func TestPlanExperimentReviewMissingRiskDoesNotFabricateR(t *testing.T) {
	state, plan := closedReviewExperiment(t)
	selection := state.Selections[plan.ID]
	selection.Plan.Structure.Plan = nil
	state.Selections[plan.ID] = selection
	review := BuildPlanExperimentReview(state)
	if review.PairedCompleted != 1 || review.NetRSamples != 0 || review.AverageNetRDifference != nil || review.Plans[0].Range.NetR != nil {
		t.Fatalf("missing risk was converted into zero R: %+v", review)
	}
}

func TestPlanExperimentReviewCountsRejectionsWithoutCreatingFills(t *testing.T) {
	state, plan, bars := experimentFixture(t)
	at := time.Date(2026, 9, 21, 10, 0, 0, 0, shanghaiLocation)
	state = advanceExperimentTest(t, state, experimentInput(plan, bars, at, 101), at.Add(time.Second))
	at = at.Add(30 * time.Second)
	input := experimentInput(plan, bars, at, 101)
	input.Bars[0].Amount = 0
	state = advanceExperimentTest(t, state, input, at.Add(time.Second))
	review := BuildPlanExperimentReview(state)
	if review.Waiting != 1 || review.Arms[0].Rejections != 1 || review.Plans[0].Range.LastRejection == "" || review.Plans[0].Range.NetProfit != nil {
		t.Fatalf("rejected attempt became a trade: %+v", review)
	}
}
