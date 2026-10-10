package strategy

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestPatternValidationPreservesUnavailableTurnover(t *testing.T) {
	bars, benchmark := validationFixture("bull-flag")
	for i := range bars {
		bars[i].Turnover = math.NaN()
		benchmark[i].Turnover = math.NaN()
	}
	report := validationReport(t, bars, benchmark, 114)
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded domain.PatternValidationReport
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(decoded.Inputs[0].Bars[0].Turnover) {
		t.Fatal("missing turnover was converted to zero")
	}
	reencoded, err := json.Marshal(decoded)
	if err != nil || string(encoded) != string(reencoded) {
		t.Fatalf("nullable input archive did not round-trip: %v", err)
	}
	if !reflect.DeepEqual(report.Samples, decoded.Samples) {
		t.Fatal("turnover serialization changed validation results")
	}
}

func validationFixture(kind string) ([]domain.DailyBar, []domain.DailyBar) {
	bars := classicPatternBars(kind)
	calendar := chartTestBars(115)
	direction := 1.0
	if kind == "bear-flag" || kind == "head-shoulders-top" || kind == "double-top" || kind == "descending-triangle" {
		direction = -1
	}
	base := bars[79].Close
	for i := 80; i < len(calendar); i++ {
		bar := calendar[i]
		price := base * (1 + direction*.002*float64(i-79))
		bar.Open, bar.Close, bar.High, bar.Low = price, price, price+.5, price-.5
		bars = append(bars, bar)
	}
	for i := range calendar {
		price := 100 + float64(i)*.1
		calendar[i].Symbol = "sh000300"
		calendar[i].Open, calendar[i].Close, calendar[i].High, calendar[i].Low = price, price, price+.5, price-.5
	}
	return bars, calendar
}

func validationReport(t *testing.T, bars, benchmark []domain.DailyBar, end int) domain.PatternValidationReport {
	t.Helper()
	request := domain.PatternValidationRequest{Symbols: []string{"sh600519"}, Start: bars[24].Date, End: bars[end].Date}
	report, err := BuildPatternValidation(context.Background(), request, []domain.PatternValidationInput{{Symbol: "sh600519", Bars: bars}, {Symbol: "sh000300", Bars: benchmark}}, chartTestTime(bars[len(bars)-1].Date, 16))
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func validationSample(t *testing.T, report domain.PatternValidationReport, kind string) domain.PatternValidationSample {
	t.Helper()
	var found []domain.PatternValidationSample
	for _, sample := range report.Samples {
		if sample.PatternID == kind {
			found = append(found, sample)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected one %s sample, got %d", kind, len(found))
	}
	return found[0]
}

func TestPatternValidationDeduplicatesAllEightPatterns(t *testing.T) {
	for _, kind := range []string{"double-bottom", "double-top", "ascending-triangle", "descending-triangle", "head-shoulders-bottom", "head-shoulders-top", "bull-flag", "bear-flag"} {
		t.Run(kind, func(t *testing.T) {
			bars, benchmark := validationFixture(kind)
			report := validationReport(t, bars, benchmark, 114)
			sample := validationSample(t, report, kind)
			if sample.ObservedOn != sample.Snapshot.Pattern.ReadyOn || sample.ConfirmedOn != bars[79].Date || sample.SnapshotFingerprint == "" {
				t.Fatalf("wrong frozen sample: %+v", sample)
			}
			for _, outcome := range sample.Outcomes {
				if outcome.State != "mature" || outcome.Through != bars[79+outcome.Horizon].Date || math.Abs(*outcome.DirectionReturn-float64(outcome.Horizon)*.2) > 1e-8 || *outcome.Favorable < *outcome.DirectionReturn || *outcome.Adverse > 0 {
					t.Fatalf("wrong mature window: %+v", outcome)
				}
				if sample.Bias == "bearish" && *outcome.PriceReturn >= 0 {
					t.Fatal("bearish price return was silently inverted")
				}
			}
		})
	}
}

func TestPatternValidationNoFutureDetectionOrRegimeLeak(t *testing.T) {
	bars, benchmark := validationFixture("bull-flag")
	before := validationReport(t, bars[:80], benchmark[:80], 79)
	for i := 80; i < len(bars); i++ {
		bars[i].High = math.NaN()
		bars[i].Source = "未来缓存"
		benchmark[i].High = math.NaN()
	}
	after := validationReport(t, bars, benchmark, 79)
	if before.InputHash != after.InputHash || !reflect.DeepEqual(before.Samples, after.Samples) {
		t.Fatal("future data changed historical snapshots or labels")
	}
	sample := validationSample(t, after, "bull-flag")
	for _, outcome := range sample.Outcomes {
		if outcome.State != "pending" || outcome.DirectionReturn != nil {
			t.Fatalf("unfinished window got a return: %+v", outcome)
		}
	}
}

func TestPatternValidationMaturityMissingBarsAndAbsentBenchmark(t *testing.T) {
	bars, benchmark := validationFixture("head-shoulders-bottom")
	sample := validationSample(t, validationReport(t, bars, benchmark, 84), "head-shoulders-bottom")
	if sample.Outcomes[0].State != "mature" || sample.Outcomes[1].State != "pending" || sample.Outcomes[2].State != "pending" {
		t.Fatalf("wrong maturity: %+v", sample.Outcomes)
	}
	bars = append(bars[:82:82], bars[83:]...)
	sample = validationSample(t, validationReport(t, bars, benchmark, len(bars)-1), "head-shoulders-bottom")
	for _, outcome := range sample.Outcomes {
		if outcome.State != "unavailable" || outcome.PriceReturn != nil {
			t.Fatal("missing trading day counted as a valid N-day window")
		}
	}
	sample = validationSample(t, validationReport(t, bars, nil, len(bars)-1), "head-shoulders-bottom")
	if sample.Regime != "数据不足" || sample.Outcomes[0].State != "unavailable" {
		t.Fatal("absent benchmark produced a market label or outcome")
	}
}

func TestPatternValidationFreezesFailureAndExpiresUnconfirmed(t *testing.T) {
	bars, benchmark := validationFixture("bull-flag")
	original := validationSample(t, validationReport(t, bars, benchmark, 114), "bull-flag")
	bars[75].Low = original.Snapshot.Pattern.InvalidationPrice - 1
	sample := validationSample(t, validationReport(t, bars, benchmark, 114), "bull-flag")
	if sample.ID != original.ID || sample.State != "invalidated" || sample.ConfirmedOn != "" || sample.InvalidatedOn != bars[75].Date {
		t.Fatalf("early invalidation was erased: %+v", sample)
	}
	if sample.Outcomes[0].State != "unconfirmed" {
		t.Fatal("unconfirmed failure received a forward return")
	}
	bars, benchmark = validationFixture("bull-flag")
	for i := 75; i < len(bars); i++ {
		bars[i].Volume = 700
	}
	sample = validationSample(t, validationReport(t, bars, benchmark, 114), "bull-flag")
	if sample.State != "expired" || sample.ConfirmedOn != "" || sample.ExpiredOn != bars[104].Date {
		t.Fatalf("confirmation window did not expire: %+v", sample)
	}
}

func TestPatternValidationFailureRateUsesMatureWindows(t *testing.T) {
	bars, benchmark := validationFixture("bull-flag")
	original := validationSample(t, validationReport(t, bars, benchmark, 114), "bull-flag")
	bars[87].Low = original.Snapshot.Pattern.InvalidationPrice - 1
	report := validationReport(t, bars, benchmark, 114)
	sample := validationSample(t, report, "bull-flag")
	if sample.InvalidatedOn != bars[87].Date || *sample.Outcomes[0].Invalidated || !*sample.Outcomes[1].Invalidated || !*sample.Outcomes[2].Invalidated {
		t.Fatal("invalidation was not bounded by the selected window")
	}
	view, err := PatternValidationResult(report, PatternValidationFilter{Pattern: "bull-flag", Horizon: 10})
	if err != nil || view.Totals.Mature != 1 || *view.Totals.InvalidationRate != 100 || view.Report.Inputs != nil || view.Total != 1 {
		t.Fatalf("invalid summary: %+v %v", view, err)
	}
	view, err = PatternValidationResult(report, PatternValidationFilter{Bias: "bearish", Horizon: 10})
	if err != nil || view.Total != 0 || view.Totals.AverageDirectionReturn != nil {
		t.Fatal("empty filter displayed a zero performance result")
	}
}

func TestPatternValidationExcludesIntradayAndRejectsConflictingBars(t *testing.T) {
	bars, benchmark := validationFixture("bull-flag")
	request := domain.PatternValidationRequest{Symbols: []string{"sh600519"}, Start: bars[24].Date, End: bars[79].Date}
	report, err := BuildPatternValidation(context.Background(), request, []domain.PatternValidationInput{{Symbol: "sh600519", Bars: bars}, {Symbol: "sh000300", Bars: benchmark}}, chartTestTime(bars[79].Date, 10))
	if err != nil {
		t.Fatal(err)
	}
	sample := validationSample(t, report, "bull-flag")
	if sample.ConfirmedOn != "" || report.Coverage[0].LastDate != bars[78].Date {
		t.Fatal("unfinished candle entered validation")
	}
	conflict := bars[50]
	conflict.Volume++
	bars = append(bars, conflict)
	_, err = BuildPatternValidation(context.Background(), request, []domain.PatternValidationInput{{Symbol: "sh600519", Bars: bars}}, chartTestTime(request.End, 16))
	if err == nil {
		t.Fatal("conflicting duplicate candles were silently selected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	clean, _ := validationFixture("bull-flag")
	_, err = BuildPatternValidation(ctx, request, []domain.PatternValidationInput{{Symbol: "sh600519", Bars: clean}}, chartTestTime(request.End, 16))
	if err != context.Canceled {
		t.Fatalf("cancellation ignored: %v", err)
	}
}
