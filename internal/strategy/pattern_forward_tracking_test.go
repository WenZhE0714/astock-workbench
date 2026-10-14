package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func forwardTrackingFixture(t *testing.T, basis string) (domain.PatternForwardTask, []domain.PatternForwardEvidence, []string, string) {
	t.Helper()
	task, input, ref, observed := forwardStrategyFixture(t)
	var err error
	task, err = NewPatternForwardTask(task.Name, task.Pool, basis, task.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	task.ID = "task"
	if basis == domain.PatternPriceHFQ {
		factors := adjustmentFixture(input.Bars)
		input.Adjustment = &factors
	}
	evidence := []domain.PatternForwardEvidence{}
	dates := []string{}
	seen := map[string]bool{}
	id := ""
	for _, bar := range input.Bars {
		if bar.Date < observed {
			continue
		}
		date := bar.Date
		dates = append(dates, date)
		benchmark := ref
		benchmark.TaskID, benchmark.Date, benchmark.Hash = task.ID, date, "benchmark-"+date
		benchmark.Input.Bars = nil
		for _, b := range ref.Input.Bars {
			if b.Date <= date {
				benchmark.Input.Bars = append(benchmark.Input.Bars, b)
			}
		}
		now := chartTestTime(date, 16)
		snapshot, err := BuildPatternForwardSnapshot(context.Background(), task, date, input, benchmark, now, seen)
		if err != nil {
			t.Fatal(err)
		}
		snapshot.Hash = "stock-" + date
		for _, sample := range snapshot.Samples {
			seen[sample.ID] = true
			if sample.PatternID == "bull-flag" {
				id = sample.ID
			}
		}
		evidence = append(evidence, domain.PatternForwardEvidence{Day: domain.PatternForwardDay{TaskID: task.ID, Date: date, State: "captured", SealedAt: now}, Benchmark: &benchmark, Stocks: []domain.PatternForwardSnapshot{snapshot}})
	}
	if id == "" {
		t.Fatal("missing tracking fixture sample")
	}
	return task, evidence, dates, id
}

func runForwardTrackingFixture(t *testing.T, task domain.PatternForwardTask, evidence []domain.PatternForwardEvidence, dates []string) []domain.PatternForwardTracking {
	t.Helper()
	logs := []domain.PatternForwardTracking{}
	for i, entry := range evidence {
		ledger, err := BuildPatternForwardTracking(context.Background(), task, evidence[:i+1], logs, dates, chartTestTime(entry.Day.Date, 17))
		if err != nil {
			t.Fatal(err)
		}
		ledger.Hash = fmt.Sprintf("ledger-%d", i)
		logs = append(logs, ledger)
	}
	return logs
}

func trackingRecord(t *testing.T, logs []domain.PatternForwardTracking, id string) domain.PatternForwardProgress {
	t.Helper()
	var result domain.PatternForwardProgress
	for _, ledger := range logs {
		for _, record := range ledger.Records {
			if record.Sample.ID == id {
				result = record
			}
		}
	}
	if result.Sample.ID == "" {
		t.Fatal("sample not in tracking ledger")
	}
	return result
}

func TestForwardTrackingMaturesFrozenWindowsWithoutMutatingObservation(t *testing.T) {
	for _, basis := range []string{domain.PatternPriceRaw, domain.PatternPriceHFQ} {
		t.Run(basis, func(t *testing.T) {
			task, evidence, dates, id := forwardTrackingFixture(t, basis)
			before, _ := json.Marshal(evidence[0])
			logs := runForwardTrackingFixture(t, task, evidence, dates)
			first := trackingRecord(t, logs[:1], id)
			if first.Sample.ConfirmedOn != "" || first.Sample.Outcomes[0].State != "unconfirmed" {
				t.Fatal("future confirmation leaked into first observation")
			}
			last := trackingRecord(t, logs, id)
			if !last.Finished || last.Sample.ConfirmedOn == "" || last.Sample.State != "confirmed" {
				t.Fatalf("lifecycle did not complete: %+v", last)
			}
			for _, outcome := range last.Sample.Outcomes {
				if outcome.State != "mature" || outcome.PriceReturn == nil || math.Abs(*outcome.PriceReturn-float64(outcome.Horizon)*.2) > 1e-8 || !validPatternBenchmark(outcome.Benchmark) || outcome.ActionCheck.State != "clear" {
					t.Fatalf("wrong frozen outcome: %+v", outcome)
				}
				var frozen *domain.PatternValidationOutcome
				for _, ledger := range logs {
					for _, record := range ledger.Records {
						if record.Sample.ID != id {
							continue
						}
						for _, candidate := range record.Sample.Outcomes {
							if candidate.Horizon != outcome.Horizon {
								continue
							}
							if frozen != nil && !reflect.DeepEqual(*frozen, candidate) {
								t.Fatal("mature outcome changed on a later day")
							}
							if candidate.State == "mature" && frozen == nil {
								copy := candidate
								frozen = &copy
							}
						}
					}
				}
			}
			after, _ := json.Marshal(evidence[0])
			if string(before) != string(after) {
				t.Fatal("tracking mutated immutable observation")
			}
		})
	}
}

func TestForwardTrackingMissingDatesAreNotBackfilled(t *testing.T) {
	for _, gap := range []int{1, 12} {
		t.Run(fmt.Sprint(gap), func(t *testing.T) {
			task, evidence, dates, id := forwardTrackingFixture(t, "raw")
			evidence[gap].Stocks = nil
			evidence[gap].Day.State = "missed"
			logs := runForwardTrackingFixture(t, task, evidence, dates)
			last := trackingRecord(t, logs, id)
			if last.Sample.State != "unavailable" || len(last.MissingDates) == 0 {
				t.Fatalf("gap silently filled from later history: %+v", last)
			}
			if gap == 1 {
				if last.Sample.ConfirmedOn != "" || !last.Finished {
					t.Fatal("confirmation inferred across missing observations")
				}
			} else if last.Sample.Outcomes[0].State != "mature" || last.Sample.Outcomes[1].State != "unavailable" || last.Sample.Outcomes[2].State != "unavailable" {
				t.Fatal("missing session shifted or contaminated another outcome window")
			}
			for _, outcome := range last.Sample.Outcomes {
				if outcome.State == "unavailable" && (outcome.PriceReturn != nil || outcome.Benchmark.Return != nil) {
					t.Fatal("unavailable window counted as zero return")
				}
			}
		})
	}
}

func TestForwardTrackingIgnoresLaterHistoricalPriceRevisions(t *testing.T) {
	task, evidence, dates, id := forwardTrackingFixture(t, "raw")
	baseline := trackingRecord(t, runForwardTrackingFixture(t, task, evidence, dates), id)
	for i := 1; i < len(evidence); i++ {
		stock := &evidence[i].Stocks[0]
		for j := 0; j < len(stock.Input.Bars)-1; j++ {
			bar := &stock.Input.Bars[j]
			bar.Open, bar.Close, bar.High, bar.Low, bar.Volume = 1, 1, 2, .5, 1
		}
	}
	revised := trackingRecord(t, runForwardTrackingFixture(t, task, evidence, dates), id)
	if !reflect.DeepEqual(baseline, revised) {
		t.Fatal("later historical prices rewrote frozen lifecycle or returns")
	}
}

func TestForwardTrackingBlocksRevisedFactorsAndCalendar(t *testing.T) {
	task, evidence, dates, id := forwardTrackingFixture(t, "hfq")
	for i := 1; i < len(evidence); i++ {
		factors := evidence[i].Stocks[0].Input.Adjustment
		factors.Factors = append(factors.Factors, domain.AdjustmentFactor{Date: dates[0], Value: 4})
	}
	last := trackingRecord(t, runForwardTrackingFixture(t, task, evidence, dates), id)
	if last.Sample.ConfirmedOn != "" || last.Sample.State != "unavailable" {
		t.Fatal("revised historical factor spliced into frozen prices")
	}
	first, err := BuildPatternForwardTracking(context.Background(), task, evidence[:2], nil, dates, chartTestTime(dates[1], 17))
	if err != nil {
		t.Fatal(err)
	}
	first.TradingDates = first.TradingDates[1:]
	if _, err = BuildPatternForwardTracking(context.Background(), task, evidence[:3], []domain.PatternForwardTracking{first}, dates, chartTestTime(dates[2], 17)); err == nil {
		t.Fatal("revised session clock rewrote previous ledger")
	}
}

func TestForwardTrackingInvalidationAndExpiry(t *testing.T) {
	for _, mode := range []string{"invalidated", "expired"} {
		t.Run(mode, func(t *testing.T) {
			task, evidence, dates, id := forwardTrackingFixture(t, "raw")
			original := evidence[0].Stocks[0].Samples[0]
			for _, sample := range evidence[0].Stocks[0].Samples {
				if sample.ID == id {
					original = sample
				}
			}
			price := (original.Snapshot.Pattern.TriggerPrice + original.Snapshot.Pattern.InvalidationPrice) / 2
			for i := 1; i < len(evidence); i++ {
				bars := evidence[i].Stocks[0].Input.Bars
				bar := &bars[len(bars)-1]
				bar.Open, bar.Close, bar.High, bar.Low, bar.Volume = price, price, price+.01, price-.01, 1000
				if mode == "invalidated" {
					bar.Low = original.Snapshot.Pattern.InvalidationPrice - 1
				}
			}
			last := trackingRecord(t, runForwardTrackingFixture(t, task, evidence, dates), id)
			if !last.Finished || last.Sample.State != mode || last.Sample.ConfirmedOn != "" {
				t.Fatalf("wrong terminal lifecycle: %+v", last)
			}
		})
	}
}

func TestForwardTrackingSplitKeepsFrozenPriceAndVolumeUnits(t *testing.T) {
	task, evidence, dates, id := forwardTrackingFixture(t, "hfq")
	baseline := trackingRecord(t, runForwardTrackingFixture(t, task, evidence, dates), id)
	exDate := dates[12]
	for i := 12; i < len(evidence); i++ {
		stock := &evidence[i].Stocks[0]
		for j := range stock.Input.Bars {
			bar := &stock.Input.Bars[j]
			if bar.Date >= exDate {
				bar.Open /= 2
				bar.Close /= 2
				bar.High /= 2
				bar.Low /= 2
				bar.Volume *= 2
			}
		}
		event := fixtureAction(exDate)
		event.Kinds = []string{"bonus-share"}
		event.RecordDate = dates[11]
		event.Terms = testDistributionTerms(0, 10, 0)
		stock.Input.Actions.Events = []domain.CorporateAction{event}
		stock.Input.Adjustment.Factors = append(stock.Input.Adjustment.Factors, domain.AdjustmentFactor{Date: exDate, Value: 4})
	}
	last := trackingRecord(t, runForwardTrackingFixture(t, task, evidence, dates), id)
	if last.Sample.ConfirmedOn != baseline.Sample.ConfirmedOn || len(last.MissingDates) != 0 {
		t.Fatalf("valid split broke frozen units: %+v", last)
	}
	for i, outcome := range last.Sample.Outcomes {
		if outcome.State != "mature" || !reflect.DeepEqual(outcome.PriceReturn, baseline.Sample.Outcomes[i].PriceReturn) || (i > 0 && outcome.ActionCheck.State != "adjusted") {
			t.Fatalf("split changed outcome or failed quality checks: %+v", outcome)
		}
	}
}

func TestForwardTrackingUnverifiedDayStaysExcludedFromStatistics(t *testing.T) {
	task, evidence, dates, id := forwardTrackingFixture(t, "raw")
	evidence[8].Stocks[0].Input.Actions.Status = "unavailable"
	last := trackingRecord(t, runForwardTrackingFixture(t, task, evidence, dates), id)
	for _, outcome := range last.Sample.Outcomes {
		if outcome.State != "mature" || outcome.ActionCheck.State != "unverified" {
			t.Fatal("later quality data upgraded earlier unverified evidence")
		}
	}
	totals := SummarizePatternValidation([]domain.PatternValidationSample{last.Sample}, 5)
	if totals.Mature != 0 || totals.ActionUnverified != 1 || totals.AveragePriceReturn != nil {
		t.Fatal("unverified forward window entered statistics")
	}
}

func TestForwardTrackingSessionClockDoesNotCountAbsentCalendarDates(t *testing.T) {
	task, evidence, dates, id := forwardTrackingFixture(t, "raw")
	evidence = append(evidence[:7:7], evidence[8:]...)
	dates = append(dates[:7:7], dates[8:]...)
	last := trackingRecord(t, runForwardTrackingFixture(t, task, evidence, dates), id)
	confirmed := 0
	for dates[confirmed] != last.Sample.ConfirmedOn {
		confirmed++
	}
	for _, outcome := range last.Sample.Outcomes {
		if outcome.State != "mature" || outcome.Through != dates[confirmed+outcome.Horizon] || len(last.MissingDates) != 0 {
			t.Fatal("calendar holiday treated as a missing observation or counted in the window")
		}
	}
}
