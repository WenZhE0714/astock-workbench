package strategy

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func adjustmentFixture(bars []domain.DailyBar) domain.AdjustmentSnapshot {
	return domain.AdjustmentSnapshot{Symbol: "sh600519", Start: bars[0].Date, End: bars[len(bars)-1].Date, Status: "checked", Source: "fixture", SourceURL: "https://finance.sina.com.cn/", FetchedAt: chartTestTime(bars[len(bars)-1].Date, 16), Factors: []domain.AdjustmentFactor{{Date: "1900-01-01", Value: 2}}}
}

func TestPatternAdjustmentForwardPricesPreserveRawAndLiquidity(t *testing.T) {
	bars, _ := validationFixture("bull-flag")
	before := append([]domain.DailyBar{}, bars...)
	factors := adjustmentFixture(bars)
	factors.Factors = append(factors.Factors, domain.AdjustmentFactor{Date: bars[85].Date, Value: 2.5})
	adjusted, _, info := preparePatternAdjustment(&factors, "sh600519", bars[114].Date, bars)
	if info.Status != "applied" || info.AnchorFactor != 2 || len(info.FactorVersion) != 64 || !reflect.DeepEqual(bars, before) {
		t.Fatalf("raw input mutated or provenance missing: %+v", info)
	}
	for i, bar := range adjusted {
		ratio := 1.0
		if i >= 85 {
			ratio = 1.25
		}
		if bar.Open != bars[i].Open*ratio || bar.High != bars[i].High*ratio || bar.Low != bars[i].Low*ratio || bar.Close != bars[i].Close*ratio || bar.Volume != bars[i].Volume || bar.Amount != bars[i].Amount {
			t.Fatalf("incorrect price/liquidity at %d", i)
		}
	}
	earlier, _, a := preparePatternAdjustment(&factors, "sh600519", bars[84].Date, bars[:85])
	factors.Factors = append(factors.Factors, domain.AdjustmentFactor{Date: bars[100].Date, Value: 5})
	later, _, b := preparePatternAdjustment(&factors, "sh600519", bars[84].Date, bars[:85])
	if !reflect.DeepEqual(earlier, later) || a.FactorVersion != b.FactorVersion {
		t.Fatal("future factors rescaled earlier research")
	}
}

func TestAdjustedPatternWindowsMatchDividendsAndReplayArchive(t *testing.T) {
	bars, benchmark := validationFixture("bull-flag")
	actions := checkedPatternActions(bars[0].Date, bars[114].Date)
	actions.Events = []domain.CorporateAction{fixtureAction(bars[85].Date)}
	actions.Events[0].Terms = testDistributionTerms(bars[84].Close*2, 0, 0)
	factors := adjustmentFixture(bars)
	factors.Factors = append(factors.Factors, domain.AdjustmentFactor{Date: bars[85].Date, Value: 2.5})
	request := domain.PatternValidationRequest{Symbols: []string{"sh600519"}, Start: bars[24].Date, End: bars[114].Date, PriceBasis: domain.PatternPriceHFQ}
	inputs := []domain.PatternValidationInput{{Symbol: "sh600519", Bars: bars, Actions: &actions, Adjustment: &factors}, {Symbol: "sh000300", Bars: benchmark}}
	now := chartTestTime(bars[114].Date, 16)
	report, err := BuildPatternValidation(context.Background(), request, inputs, now)
	if err != nil {
		t.Fatal(err)
	}
	sample := validationSample(t, report, "bull-flag")
	if sample.Outcomes[0].ActionCheck.State != "clear" || sample.Outcomes[1].ActionCheck.State != "adjusted" || sample.Outcomes[2].ActionCheck.State != "adjusted" {
		t.Fatalf("window boundaries: %+v", sample.Outcomes)
	}
	expected := (bars[89].Close*1.25/bars[79].Close - 1) * 100
	if math.Abs(*sample.Outcomes[1].PriceReturn-expected) > 1e-10 {
		t.Fatal("return did not use adjusted closing price")
	}
	view, err := PatternValidationResult(report, PatternValidationFilter{Pattern: "bull-flag", Horizon: 10, Action: "adjusted"})
	if err != nil || view.Totals.Mature != 1 || view.Totals.ActionAdjusted != 1 {
		t.Fatalf("adjusted sample excluded: %+v %v", view.Totals, err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var archive domain.PatternValidationReport
	if err = json.Unmarshal(encoded, &archive); err != nil {
		t.Fatal(err)
	}
	replayed, err := BuildPatternValidation(context.Background(), archive.Request, archive.Inputs, now)
	if err != nil || replayed.InputHash != report.InputHash || !reflect.DeepEqual(replayed.Samples, report.Samples) || archive.Inputs[0].Bars[89].Close != bars[89].Close {
		t.Fatalf("archive replay changed: %v", err)
	}
}

func TestAdjustedPatternUnmatchedAndMissingTermsRemainExcluded(t *testing.T) {
	bars, _ := validationFixture("bull-flag")
	for _, kind := range []string{"missing-step", "unknown-step", "share", "unavailable"} {
		t.Run(kind, func(t *testing.T) {
			actions := checkedPatternActions(bars[0].Date, bars[114].Date)
			actions.Events = []domain.CorporateAction{fixtureAction(bars[85].Date)}
			factors := adjustmentFixture(bars).Factors
			if kind != "missing-step" {
				factors = append(factors, domain.AdjustmentFactor{Date: bars[85].Date, Value: 2.5})
			}
			if kind == "unknown-step" {
				actions.Events = nil
			}
			if kind == "share" {
				actions.Events[0].Kinds = append(actions.Events[0].Kinds, "bonus-share")
			}
			if kind == "unavailable" {
				actions.Status = "unavailable"
			}
			sample := domain.PatternValidationSample{Outcomes: []domain.PatternValidationOutcome{{Horizon: 10, State: "mature", ActionCheck: checkPatternActions(actions, bars[0].Date, bars[89].Date)}}}
			quality := validatePatternAdjustmentQuality(actions, factors, bars, bars, bars[114].Date)
			applyAdjustedPatternActionChecks(&sample, actions, quality)
			want := "unverified"
			if sample.Outcomes[0].ActionCheck.State != want {
				t.Fatalf("unsafe window admitted: %+v", sample.Outcomes[0])
			}
		})
	}
}

func TestPatternAdjustmentFailsClosedForBadFactors(t *testing.T) {
	bars, _ := validationFixture("bull-flag")
	for _, kind := range []string{"nil", "empty", "seed", "nan", "zero", "duplicate", "coverage", "wrong-symbol", "failure"} {
		t.Run(kind, func(t *testing.T) {
			factor := adjustmentFixture(bars)
			input := &factor
			switch kind {
			case "nil":
				input = nil
			case "empty":
				factor.Factors = nil
			case "seed":
				factor.Factors[0].Date = bars[1].Date
			case "nan":
				factor.Factors[0].Value = math.NaN()
			case "zero":
				factor.Factors[0].Value = 0
			case "duplicate":
				factor.Factors = append(factor.Factors, factor.Factors[0])
			case "coverage":
				factor.End = bars[100].Date
			case "wrong-symbol":
				factor.Symbol = "sh600000"
			case "failure":
				factor.Error = "offline"
			}
			result, snapshot, info := preparePatternAdjustment(input, "sh600519", bars[114].Date, bars)
			if result != nil || info.Status != "unavailable" || info.Error == "" {
				t.Fatalf("bad factors fell back to raw: %+v", info)
			}
			if _, err := json.Marshal(snapshot); err != nil {
				t.Fatalf("failed snapshot cannot be archived: %v", err)
			}
		})
	}
}
