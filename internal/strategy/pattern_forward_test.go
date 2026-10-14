package strategy

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func forwardStrategyFixture(t *testing.T) (domain.PatternForwardTask, domain.PatternValidationInput, domain.PatternForwardBenchmark, string) {
	t.Helper()
	stock, _ := validationFixture("bull-flag")
	calendar := chartTestBars(80 + len(stock))
	bars := append([]domain.DailyBar{}, calendar[:80]...)
	for i := range bars {
		bars[i].Open, bars[i].Close, bars[i].High, bars[i].Low, bars[i].Volume = 100, 100, 101, 99, 1000
	}
	bars = append(bars, stock...)
	for i := range bars {
		bars[i].Date, bars[i].Symbol = calendar[i].Date, "sh600519"
	}
	benchmark := append([]domain.DailyBar{}, calendar...)
	for i := range benchmark {
		price := 3000 + float64(i)
		benchmark[i].Symbol = "sh000300"
		benchmark[i].Open, benchmark[i].Close, benchmark[i].High, benchmark[i].Low, benchmark[i].Volume = price, price, price+1, price-1, 100000
	}
	actions := checkedPatternActions(bars[0].Date, bars[len(bars)-1].Date)
	input := domain.PatternValidationInput{Symbol: "sh600519", Bars: bars, Actions: &actions}
	ref := domain.PatternForwardBenchmark{TaskID: "task", Input: domain.PatternValidationInput{Symbol: "sh000300", Bars: benchmark}, Hash: "benchmark"}
	report, err := BuildPatternValidation(context.Background(), domain.PatternValidationRequest{Symbols: []string{input.Symbol}, Start: bars[80].Date, End: bars[len(bars)-1].Date}, []domain.PatternValidationInput{input, ref.Input}, chartTestTime(bars[len(bars)-1].Date, 16))
	if err != nil {
		t.Fatal(err)
	}
	sample := validationSample(t, report, "bull-flag")
	date := sample.ObservedOn
	task, err := NewPatternForwardTask("forward", domain.PatternPool{Version: "pool", Members: []domain.PatternPoolMember{{Symbol: input.Symbol}}}, "raw", chartTestTime(date, 14))
	if err != nil {
		t.Fatal(err)
	}
	task.ID = "task"
	ref.Date = date
	return task, input, ref, date
}

func TestForwardSnapshotOnlyRecordsCurrentObservation(t *testing.T) {
	task, input, benchmark, date := forwardStrategyFixture(t)
	now := chartTestTime(date, 16)
	first, err := BuildPatternForwardSnapshot(context.Background(), task, date, input, benchmark, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Samples) == 0 {
		t.Fatal("fixture has no forward observation")
	}
	seen := map[string]bool{}
	for _, sample := range first.Samples {
		if sample.ObservedOn != date || sample.Outcomes != nil {
			t.Fatal("historical or future outcomes mixed into observation")
		}
		seen[sample.ID] = true
	}
	for _, bar := range first.Input.Bars {
		if bar.Date > date {
			t.Fatal("future bar archived")
		}
	}
	input.Bars[len(input.Bars)-1].High = math.NaN()
	again, err := BuildPatternForwardSnapshot(context.Background(), task, date, input, benchmark, now, nil)
	if err != nil || first.InputHash != again.InputHash || !reflect.DeepEqual(first.Samples, again.Samples) {
		t.Fatal("future prices affected current observation")
	}
	deduplicated, err := BuildPatternForwardSnapshot(context.Background(), task, date, input, benchmark, now, seen)
	if err != nil || len(deduplicated.Samples) != 0 {
		t.Fatal("previously captured identity was registered again")
	}
}

func TestForwardWindowRejectsBackdatingAndAfterCloseCreation(t *testing.T) {
	task, input, benchmark, date := forwardStrategyFixture(t)
	for _, now := range []time.Time{chartTestTime(date, 14), chartTestTime(date, 15), chartTestTime(date, 18), chartTestTime(date, 16).AddDate(0, 0, 1)} {
		if _, err := BuildPatternForwardSnapshot(context.Background(), task, date, input, benchmark, now, nil); err == nil {
			t.Fatalf("invalid collection window accepted: %s", now)
		}
	}
	late, err := NewPatternForwardTask("late", task.Pool, "raw", chartTestTime(date, 15))
	if err != nil {
		t.Fatal(err)
	}
	if late.Config.StartOn <= date || PatternForwardWindow(late, chartTestTime(date, 16)) {
		t.Fatal("post-close configuration admitted same-day signals")
	}
	task.Config.PriceBasis = "hfq"
	if _, err := BuildPatternForwardSnapshot(context.Background(), task, date, input, benchmark, chartTestTime(date, 16), nil); err == nil {
		t.Fatal("changed config accepted")
	}
	task.ConfigHash = task.Fingerprint()
	if PatternForwardCompatible(task) {
		t.Fatal("altered frozen methods accepted as current defaults")
	}
}

func TestForwardSnapshotRequiresCurrentBarAndWarmup(t *testing.T) {
	task, input, benchmark, date := forwardStrategyFixture(t)
	filtered := []domain.DailyBar{}
	for _, bar := range input.Bars {
		if bar.Date < date {
			filtered = append(filtered, bar)
		}
	}
	missing := input
	missing.Bars = filtered
	if _, err := BuildPatternForwardSnapshot(context.Background(), task, date, missing, benchmark, chartTestTime(date, 16), nil); err == nil {
		t.Fatal("stale history admitted as current")
	}
	for _, bar := range input.Bars {
		if bar.Date == date {
			filtered = append(filtered, bar)
		}
	}
	input.Bars = filtered[len(filtered)-50:]
	if _, err := BuildPatternForwardSnapshot(context.Background(), task, date, input, benchmark, chartTestTime(date, 16), nil); err == nil {
		t.Fatal("short warmup admitted")
	}
}
