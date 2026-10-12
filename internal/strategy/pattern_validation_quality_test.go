package strategy

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func testDistributionTerms(cash, bonus, transfer float64) *domain.DistributionTerms {
	return &domain.DistributionTerms{CashPer10: &cash, BonusPer10: &bonus, CapitalizationPer10: &transfer}
}

func qualityFixture(cash, bonus, transfer, exPrice, shares float64) ([]domain.DailyBar, []domain.DailyBar, domain.CorporateActionSnapshot, domain.AdjustmentSnapshot) {
	bars := []domain.DailyBar{}
	for _, date := range []string{"2026-06-01", "2026-06-02", "2026-06-03", "2026-06-04", "2026-06-05", "2026-06-08", "2026-06-09", "2026-06-10"} {
		price, volume := 100.0, 1000.0
		if date >= "2026-06-04" {
			price, volume = exPrice, 1000*shares
		}
		bars = append(bars, domain.DailyBar{Symbol: "sh600519", Date: date, Open: price, Close: price, High: price + .1, Low: price - .1, Volume: volume, Amount: 100000, Turnover: 2, Source: "fixture"})
	}
	benchmark := append([]domain.DailyBar{}, bars...)
	actions := checkedPatternActions(bars[0].Date, bars[7].Date)
	event := fixtureAction(bars[3].Date)
	event.RecordDate = bars[2].Date
	event.Terms = testDistributionTerms(cash, bonus, transfer)
	event.Kinds = nil
	for i, number := range []float64{cash, bonus, transfer} {
		if number > 0 {
			event.Kinds = append(event.Kinds, []string{"cash-dividend", "bonus-share", "capitalization"}[i])
		}
	}
	actions.Events = []domain.CorporateAction{event}
	factors := adjustmentFixture(bars)
	factors.Factors = append(factors.Factors, domain.AdjustmentFactor{Date: event.ExDate, Value: 2 * 100 / exPrice})
	return bars, benchmark, actions, factors
}

func TestAdjustmentQualityCashBonusCapitalizationAndMixed(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		cash, bonus, transfer, price, shares float64
	}{
		{"cash", 20, 0, 0, 98, 1},
		{"bonus", 0, 5, 0, 100 / 1.5, 1.5},
		{"capitalization", 0, 0, 10, 50, 2},
		{"mixed", 20, 3, 7, 49, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bars, benchmark, actions, factors := qualityFixture(tc.cash, tc.bonus, tc.transfer, tc.price, tc.shares)
			original := append([]domain.DailyBar{}, bars...)
			adjusted, snapshot, info := preparePatternAdjustment(&factors, "sh600519", bars[7].Date, bars)
			info.Quality = validatePatternAdjustmentQuality(actions, snapshot.Factors, bars, benchmark, bars[7].Date)
			applyPatternShareVolumes(adjusted, &info, actions)
			if len(info.Quality) != 1 || info.Quality[0].State != "passed" || math.Abs(*info.Quality[0].ExpectedExPrice-tc.price) > 1e-10 || *info.Quality[0].ShareRatio != tc.shares || info.QualityStatus != "passed" || len(info.VolumeVersion) != 64 {
				t.Fatalf("quality failed: %+v", info)
			}
			for i, bar := range adjusted {
				if math.Abs(bar.Close-100) > 1e-10 || math.Abs(bar.Volume-1000) > 1e-10 || bar.Amount != bars[i].Amount || bar.Turnover != bars[i].Turnover {
					t.Fatalf("incorrect price/volume normalization at %d: %+v", i, bar)
				}
			}
			if !reflect.DeepEqual(bars, original) {
				t.Fatal("raw bars mutated")
			}
			sample := domain.PatternValidationSample{ActionCheck: checkPatternActions(actions, bars[0].Date, bars[7].Date)}
			applyAdjustedPatternActionChecks(&sample, actions, info.Quality)
			if sample.ActionCheck.State != "adjusted" {
				t.Fatalf("verified distribution not eligible: %+v", sample.ActionCheck)
			}
		})
	}
}

func TestAdjustmentQualityRejectsIncompleteOrConflictingEvidence(t *testing.T) {
	for _, tc := range []struct{ kind, code string }{
		{"missing-terms", "missing-terms"}, {"missing-number", "invalid-terms"}, {"negative-number", "invalid-terms"}, {"wrong-kind", "inconsistent-terms"},
		{"price-mismatch", "factor-price-mismatch"}, {"event-bar", "missing-event-bar"}, {"previous-gap", "previous-bar-gap"}, {"calendar-gap", "missing-calendar"},
		{"zero-volume", "unverified-trading"}, {"record-date", "record-date-mismatch"}, {"conflict", "conflicting-events"}, {"factor-step", "missing-factor-step"}, {"empty-events", "unmatched-factor"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			bars, benchmark, actions, factors := qualityFixture(20, 0, 0, 98, 1)
			switch tc.kind {
			case "missing-terms":
				actions.Events[0].Terms = nil
			case "missing-number":
				actions.Events[0].Terms.BonusPer10 = nil
			case "negative-number":
				actions.Events[0].Terms = testDistributionTerms(-1, 0, 0)
			case "wrong-kind":
				actions.Events[0].Kinds = []string{"bonus-share"}
			case "price-mismatch":
				factors.Factors[1].Value = 3
			case "event-bar":
				bars = append(bars[:3:3], bars[4:]...)
			case "previous-gap":
				bars = append(bars[:2:2], bars[3:]...)
			case "calendar-gap":
				benchmark = append(benchmark[:3:3], benchmark[4:]...)
			case "zero-volume":
				bars[3].Volume = 0
			case "record-date":
				actions.Events[0].RecordDate = bars[1].Date
			case "conflict":
				other := actions.Events[0]
				other.ID = "other-plan"
				other.Terms = testDistributionTerms(10, 0, 0)
				actions.Events = append(actions.Events, other)
			case "factor-step":
				factors.Factors = factors.Factors[:1]
			case "empty-events":
				actions.Events = nil
			}
			quality := validatePatternAdjustmentQuality(actions, factors.Factors, bars, benchmark, "2026-06-10")
			if len(quality) != 1 || quality[0].State == "passed" || quality[0].Code != tc.code {
				t.Fatalf("invalid evidence accepted: %+v", quality)
			}
			sample := domain.PatternValidationSample{ActionCheck: checkPatternActions(actions, "2026-06-01", "2026-06-10")}
			applyAdjustedPatternActionChecks(&sample, actions, quality)
			if sample.ActionCheck.State != "unverified" {
				t.Fatal("invalid interval admitted to statistics")
			}
		})
	}
}

func TestAdjustmentQualityPriceTickTolerance(t *testing.T) {
	for _, tc := range []struct {
		difference float64
		state      string
	}{{.009, "passed"}, {.01, "passed"}, {.011, "mismatch"}} {
		bars, benchmark, actions, factors := qualityFixture(20, 0, 0, 98, 1)
		factors.Factors[1].Value = 2 * 100 / (98 + tc.difference)
		quality := validatePatternAdjustmentQuality(actions, factors.Factors, bars, benchmark, bars[7].Date)
		if quality[0].State != tc.state {
			t.Fatalf("tolerance %.3f: %+v", tc.difference, quality[0])
		}
	}
}

func TestShareVolumeCompoundsAndDoesNotAffectEarlierBars(t *testing.T) {
	bars, benchmark, actions, factors := qualityFixture(0, 0, 10, 50, 2)
	second := actions.Events[0]
	second.ID, second.ExDate, second.RecordDate = "second", bars[5].Date, bars[4].Date
	actions.Events = append(actions.Events, second)
	factors.Factors = append(factors.Factors, domain.AdjustmentFactor{Date: second.ExDate, Value: 8})
	for i := 5; i < len(bars); i++ {
		bars[i].Open, bars[i].Close, bars[i].High, bars[i].Low, bars[i].Volume = 25, 25, 25.1, 24.9, 4000
	}
	adjusted, snapshot, info := preparePatternAdjustment(&factors, "sh600519", bars[7].Date, bars)
	info.Quality = validatePatternAdjustmentQuality(actions, snapshot.Factors, bars, benchmark, bars[7].Date)
	applyPatternShareVolumes(adjusted, &info, actions)
	for _, bar := range adjusted {
		if bar.Volume != 1000 || math.Abs(bar.Close-100) > 1e-10 {
			t.Fatalf("cumulative share adjustment wrong: %+v", bar)
		}
	}
	early, snapshot, earlyInfo := preparePatternAdjustment(&factors, "sh600519", bars[4].Date, bars[:5])
	earlyInfo.Quality = validatePatternAdjustmentQuality(actions, snapshot.Factors, bars[:5], benchmark[:5], bars[4].Date)
	applyPatternShareVolumes(early, &earlyInfo, actions)
	if !reflect.DeepEqual(early, adjusted[:5]) || len(earlyInfo.Quality) != 1 {
		t.Fatal("future share event changed earlier bars")
	}
}

func TestAdjustmentMismatchOnlyIsolatesWindowsReachingEvent(t *testing.T) {
	bars, benchmark := validationFixture("bull-flag")
	actions := checkedPatternActions(bars[0].Date, bars[114].Date)
	event := fixtureAction(bars[85].Date)
	event.Terms = testDistributionTerms(1, 0, 0)
	actions.Events = []domain.CorporateAction{event}
	factors := adjustmentFixture(bars)
	factors.Factors = append(factors.Factors, domain.AdjustmentFactor{Date: event.ExDate, Value: 2.5})
	request := domain.PatternValidationRequest{Symbols: []string{"sh600519"}, Start: bars[24].Date, End: bars[114].Date, PriceBasis: domain.PatternPriceHFQ}
	report, err := BuildPatternValidation(context.Background(), request, []domain.PatternValidationInput{{Symbol: "sh600519", Bars: bars, Actions: &actions, Adjustment: &factors}, {Symbol: "sh000300", Bars: benchmark}}, chartTestTime(bars[114].Date, 16))
	if err != nil {
		t.Fatal(err)
	}
	sample := validationSample(t, report, "bull-flag")
	if sample.Outcomes[0].ActionCheck.State != "clear" || sample.Outcomes[1].ActionCheck.State != "unverified" || sample.Outcomes[2].ActionCheck.State != "unverified" {
		t.Fatalf("wrong window isolation: %+v", sample.Outcomes)
	}
	if sample.Outcomes[1].PriceReturn == nil {
		t.Fatal("isolated research result was erased")
	}
	for _, horizon := range []int{10, 20} {
		totals := SummarizePatternValidation([]domain.PatternValidationSample{sample}, horizon)
		if totals.Mature != 0 || totals.ActionUnverified != 1 || totals.AveragePriceReturn != nil {
			t.Fatal("mismatch entered ordinary statistics")
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var archive domain.PatternValidationReport
	if err = json.Unmarshal(encoded, &archive); err != nil {
		t.Fatal(err)
	}
	roundtrip, err := json.Marshal(archive)
	if err != nil || string(encoded) != string(roundtrip) {
		t.Fatal("quality diagnostics lost in archive")
	}
}
