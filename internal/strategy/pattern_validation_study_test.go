package strategy

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func studyFixture(t *testing.T, boundary int, bars, benchmark []domain.DailyBar) domain.PatternValidationReport {
	t.Helper()
	actions := checkedPatternActions(bars[0].Date, bars[114].Date)
	request := domain.PatternValidationRequest{Symbols: []string{"sh600519"}, Start: bars[24].Date, End: bars[114].Date, ValidationStart: bars[boundary].Date}
	report, err := BuildPatternValidation(context.Background(), request, []domain.PatternValidationInput{{Symbol: "sh600519", Bars: bars, Actions: &actions}, {Symbol: "sh000300", Bars: benchmark}}, chartTestTime(bars[114].Date, 16))
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestPatternStudyPurgesEachBoundaryWindow(t *testing.T) {
	bars, benchmark := validationFixture("bull-flag")
	for _, boundary := range []int{79, 85, 90, 100} {
		report := studyFixture(t, boundary, bars, benchmark)
		sample := validationSample(t, report, "bull-flag")
		if report.Study == nil || report.Study.Kind != "retrospective" || len(report.Study.ConfigHash) != 64 || report.Study.Research.InputHash == "" || sample.Period != "research" {
			t.Fatal("study manifest or membership missing")
		}
		for _, outcome := range sample.Outcomes {
			want := "within"
			if boundary <= 79+outcome.Horizon {
				want = "excluded"
			}
			if outcome.PeriodCheck.State != want {
				t.Fatalf("boundary=%d horizon=%d: %+v", boundary, outcome.Horizon, outcome)
			}
			view, err := PatternValidationResult(report, PatternValidationFilter{Period: "research", Pattern: "bull-flag", Horizon: outcome.Horizon})
			if err != nil || view.Report.Cutoff != report.Study.Research.Cutoff {
				t.Fatalf("research evidence not selected: %v", err)
			}
			if want == "excluded" && (view.Totals.BoundaryExcluded != 1 || view.Totals.Mature != 0 || view.Totals.BenchmarkPaired != 0 || view.Totals.AverageExcessReturn != nil) {
				t.Fatalf("cross-boundary outcome entered research: %+v", view.Totals)
			}
			if want == "within" && view.Totals.BenchmarkPaired != 1 {
				t.Fatal("completed research window excluded")
			}
		}
		view, err := PatternValidationResult(report, PatternValidationFilter{Pattern: "bull-flag"})
		if err != nil || view.Filter.Period != "validation" || view.Total != 0 || len(view.Periods) != 2 {
			t.Fatal("pre-boundary sample migrated to holdout")
		}
		if boundary == 79 && (sample.ConfirmedOn != "" || sample.ConfirmationClose != nil || sample.RegimeDate >= report.Request.ValidationStart) {
			t.Fatal("later confirmation or regime leaked into research")
		}
	}
}

func TestPatternStudyResearchIgnoresLaterPricesAndLifecycle(t *testing.T) {
	bars, benchmark := validationFixture("bull-flag")
	before := studyFixture(t, 85, bars, benchmark)
	original := validationSample(t, before, "bull-flag")
	bars[90].Low = original.Snapshot.Pattern.InvalidationPrice - 1
	for i := 85; i < len(benchmark); i++ {
		benchmark[i].Open, benchmark[i].Close, benchmark[i].High, benchmark[i].Low = 200, 200, 201, 199
	}
	after := studyFixture(t, 85, bars, benchmark)
	if before.InputHash == after.InputHash {
		t.Fatal("test did not change full inputs")
	}
	if before.Study.ConfigHash != after.Study.ConfigHash || before.Study.Research.InputHash != after.Study.Research.InputHash || !reflect.DeepEqual(before.Samples, after.Samples) {
		t.Fatal("future data changed frozen research evidence")
	}
	view, err := PatternValidationResult(after, PatternValidationFilter{Period: "research", State: "invalidated"})
	if err != nil || view.Total != 0 {
		t.Fatal("future invalidation leaked through filters")
	}
}

func TestPatternStudyHoldoutStartsAtFirstObservationAndReplays(t *testing.T) {
	bars, benchmark := validationFixture("bull-flag")
	report := studyFixture(t, 74, bars, benchmark)
	sample := validationSample(t, report, "bull-flag")
	baseline := validationSample(t, validationReport(t, bars, benchmark, 114), "bull-flag")
	if sample.Period != "validation" || sample.ObservedOn != report.Request.ValidationStart || sample.ID != baseline.ID || sample.SnapshotFingerprint != baseline.SnapshotFingerprint {
		t.Fatal("holdout lost earlier warmup or first-observation boundary")
	}
	view, err := PatternValidationResult(report, PatternValidationFilter{Pattern: "bull-flag", Horizon: 20})
	if err != nil || view.Totals.BenchmarkPaired != 1 || view.Periods[0].Totals.Samples != 0 || view.Periods[1].Totals.Samples != 1 {
		t.Fatalf("wrong period comparison: %+v %v", view.Periods, err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var archived domain.PatternValidationReport
	if err = json.Unmarshal(encoded, &archived); err != nil {
		t.Fatal(err)
	}
	replayed, err := BuildPatternValidation(context.Background(), archived.Request, archived.Inputs, report.GeneratedAt)
	if err != nil || replayed.Study.ConfigHash != report.Study.ConfigHash || replayed.Study.Research.InputHash != report.Study.Research.InputHash || !reflect.DeepEqual(replayed.Samples, report.Samples) {
		t.Fatalf("study archive replay changed: %v", err)
	}
	_, _ = PatternValidationResult(report, PatternValidationFilter{Period: "research"})
	after, _ := json.Marshal(report)
	if string(encoded) != string(after) {
		t.Fatal("period display mutated frozen archive")
	}
}

func TestPatternStudyConfigHashAndRequestValidation(t *testing.T) {
	bars, _ := validationFixture("bull-flag")
	request := domain.PatternValidationRequest{Symbols: []string{"sh600519", "sz002080"}, Start: bars[24].Date, End: bars[114].Date, ValidationStart: bars[85].Date}
	now := chartTestTime(request.End, 16)
	first := freezePatternStudy(request, request.End, now)
	request.Symbols = []string{"sz002080", "sh600519"}
	request.PriceBasis = "raw"
	if freezePatternStudy(request, request.End, now).ConfigHash != first.ConfigHash {
		t.Fatal("equivalent configuration changed identity")
	}
	request.PriceBasis = "hfq"
	if freezePatternStudy(request, request.End, now).ConfigHash == first.ConfigHash {
		t.Fatal("price basis absent from frozen config")
	}
	for _, date := range []string{"bad", request.Start, "2099-01-01"} {
		request.ValidationStart = date
		if _, err := ValidatePatternValidationRequest(request, now); err == nil {
			t.Fatalf("invalid boundary accepted: %s", date)
		}
	}
	request.ValidationStart = request.End
	if _, err := ValidatePatternValidationRequest(request, chartTestTime(request.End, 10)); err == nil {
		t.Fatal("unclosed validation date accepted")
	}
	if _, err := PatternValidationResult(domain.PatternValidationReport{}, PatternValidationFilter{Period: "research"}); err == nil {
		t.Fatal("legacy archive silently split")
	}
}

func TestPatternStudyBothPeriodsKeepResearchIndependent(t *testing.T) {
	first, _ := validationFixture("bull-flag")
	second, _ := validationFixture("bull-flag")
	calendar := chartTestBars(180)
	for i := range second {
		second[i].Symbol = "sh600000"
		second[i].Date = calendar[i+40].Date
	}
	benchmark := append([]domain.DailyBar{}, calendar...)
	for i := range benchmark {
		price := 100 + float64(i)*.1
		benchmark[i].Symbol = "sh000300"
		benchmark[i].Open, benchmark[i].Close, benchmark[i].High, benchmark[i].Low = price, price, price+1, price-1
	}
	actions := checkedPatternActions(first[0].Date, calendar[154].Date)
	otherActions := actions
	otherActions.Symbol = "sh600000"
	inputs := []domain.PatternValidationInput{{Symbol: "sh600519", Bars: first, Actions: &actions}, {Symbol: "sh600000", Bars: second, Actions: &otherActions}, {Symbol: "sh000300", Bars: benchmark}}
	request := domain.PatternValidationRequest{Symbols: []string{"sh600519", "sh600000"}, Start: calendar[24].Date, End: calendar[154].Date, ValidationStart: calendar[110].Date}
	now := chartTestTime(request.End, 16)
	before, err := BuildPatternValidation(context.Background(), request, inputs, now)
	if err != nil {
		t.Fatal(err)
	}
	view, err := PatternValidationResult(before, PatternValidationFilter{Pattern: "bull-flag", Horizon: 20})
	if err != nil || len(view.Samples) != 1 || view.Periods[0].Totals.BenchmarkPaired != 1 || view.Periods[1].Totals.BenchmarkPaired != 1 {
		t.Fatalf("two-period comparison incomplete: %+v %v", view.Periods, err)
	}
	second[90].Low = view.Samples[0].Snapshot.Pattern.InvalidationPrice - 1
	after, err := BuildPatternValidation(context.Background(), request, inputs, now)
	if err != nil {
		t.Fatal(err)
	}
	validation, _ := PatternValidationResult(after, PatternValidationFilter{Pattern: "bull-flag", Horizon: 20})
	if validation.Samples[0].InvalidatedOn != second[90].Date {
		t.Fatal("holdout mutation did not affect validation")
	}
	researchBefore, _ := PatternValidationResult(before, PatternValidationFilter{Period: "research", Pattern: "bull-flag", Horizon: 20})
	researchAfter, _ := PatternValidationResult(after, PatternValidationFilter{Period: "research", Pattern: "bull-flag", Horizon: 20})
	if before.Study.ConfigHash != after.Study.ConfigHash || before.Study.Research.InputHash != after.Study.Research.InputHash || !reflect.DeepEqual(researchBefore.Samples, researchAfter.Samples) || !reflect.DeepEqual(researchBefore.Totals, researchAfter.Totals) {
		t.Fatal("holdout changes leaked into research statistics")
	}
}
