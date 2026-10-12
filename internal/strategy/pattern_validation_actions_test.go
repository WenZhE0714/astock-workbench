package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func checkedPatternActions(start, end string) domain.CorporateActionSnapshot {
	return domain.CorporateActionSnapshot{Symbol: "sh600519", Start: start, End: end, Status: "checked", Scope: domain.CorporateActionScope, Source: "fixture", FetchedAt: chartTestTime("2026-06-12", 16), Events: []domain.CorporateAction{}}
}

func actionValidationReport(t *testing.T, actions *domain.CorporateActionSnapshot, end int) domain.PatternValidationReport {
	t.Helper()
	bars, benchmark := validationFixture("bull-flag")
	request := domain.PatternValidationRequest{Symbols: []string{"sh600519"}, Start: bars[24].Date, End: bars[end].Date}
	report, err := BuildPatternValidation(context.Background(), request, []domain.PatternValidationInput{{Symbol: "sh600519", Bars: bars, Actions: actions}, {Symbol: "sh000300", Bars: benchmark}}, chartTestTime(bars[114].Date, 16))
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func fixtureAction(date string) domain.CorporateAction {
	return domain.CorporateAction{ID: "distribution-1", Symbol: "sh600519", ExDate: date, Kinds: []string{"cash-dividend"}, Description: "10派1元", Source: "fixture", SourceURL: "https://data.eastmoney.com/yjfp/detail/600519.html"}
}

func TestPatternActionIsolationUsesEachWindowAndPreservesRawResults(t *testing.T) {
	bars, benchmark := validationFixture("bull-flag")
	original := validationSample(t, validationReport(t, bars, benchmark, 114), "bull-flag")
	for _, at := range []int{0, 30, 75, 79, 80, 84, 85, 89, 90, 99, 100, 114} {
		t.Run(fmt.Sprint(at), func(t *testing.T) {
			actions := checkedPatternActions(bars[0].Date, bars[114].Date)
			actions.Events = []domain.CorporateAction{fixtureAction(bars[at].Date)}
			report := actionValidationReport(t, &actions, 114)
			sample := validationSample(t, report, "bull-flag")
			if !reflect.DeepEqual(sample.Snapshot, original.Snapshot) || sample.ID != original.ID || sample.ConfirmedOn != original.ConfirmedOn {
				t.Fatal("isolation changed detector evidence")
			}
			for i, outcome := range sample.Outcomes {
				want := "clear"
				if at <= 79+outcome.Horizon {
					want = "affected"
				}
				if outcome.ActionCheck.State != want {
					t.Fatalf("day=%d horizon=%d: %+v", at, outcome.Horizon, outcome.ActionCheck)
				}
				raw, before := outcome, original.Outcomes[i]
				raw.ActionCheck, before.ActionCheck = nil, nil
				if !reflect.DeepEqual(raw, before) {
					t.Fatal("raw price outcome overwritten")
				}
				view, err := PatternValidationResult(report, PatternValidationFilter{Pattern: "bull-flag", Horizon: outcome.Horizon, Action: want})
				if err != nil || view.Total != 1 {
					t.Fatalf("isolation filter: %v", err)
				}
				if want == "affected" && (view.Totals.Mature != 0 || view.Totals.ActionAffected != 1 || view.Totals.AveragePriceReturn != nil || view.Totals.InvalidationRate != nil) {
					t.Fatal("affected result entered ordinary statistics")
				}
				if want == "clear" && (view.Totals.Mature != 1 || view.Totals.ActionAffected != 0) {
					t.Fatal("unaffected window was excluded")
				}
			}
		})
	}
}

func TestPatternActionsUnavailablePartialAndConflictingStayUnverified(t *testing.T) {
	bars, _ := validationFixture("bull-flag")
	for _, kind := range []string{"nil", "failure", "partial", "scope", "conflict", "invalid-date"} {
		t.Run(kind, func(t *testing.T) {
			actions := checkedPatternActions(bars[0].Date, bars[114].Date)
			snapshot := &actions
			switch kind {
			case "nil":
				snapshot = nil
			case "failure":
				actions.Status, actions.Error = "unavailable", "offline"
			case "partial":
				actions.Start = bars[10].Date
			case "scope":
				actions.Scope = "cash-dividend"
			case "conflict":
				one, two := fixtureAction(bars[100].Date), fixtureAction(bars[101].Date)
				actions.Events = []domain.CorporateAction{one, two}
			case "invalid-date":
				actions.Events = []domain.CorporateAction{fixtureAction("invalid")}
			}
			report := actionValidationReport(t, snapshot, 114)
			view, err := PatternValidationResult(report, PatternValidationFilter{Pattern: "bull-flag"})
			if err != nil || view.Totals.ActionUnverified != 1 || view.Totals.Mature != 0 || view.Totals.AverageDirectionReturn != nil {
				t.Fatalf("unverified evidence accepted: %+v %v", view.Totals, err)
			}
		})
	}
}

func TestPatternActionsIgnoreFutureEventsAndArchiveEvidence(t *testing.T) {
	bars, _ := validationFixture("bull-flag")
	actions := checkedPatternActions(bars[0].Date, bars[114].Date)
	before := actionValidationReport(t, &actions, 84)
	actions.Events = []domain.CorporateAction{fixtureAction(bars[85].Date)}
	after := actionValidationReport(t, &actions, 84)
	if before.InputHash != after.InputHash || !reflect.DeepEqual(before.Samples, after.Samples) {
		t.Fatal("future event changed earlier audit window")
	}
	report := actionValidationReport(t, &actions, 114)
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var loaded domain.PatternValidationReport
	if err := json.Unmarshal(encoded, &loaded); err != nil {
		t.Fatal(err)
	}
	roundtrip, marshalErr := json.Marshal(loaded)
	if marshalErr != nil || string(encoded) != string(roundtrip) || !reflect.DeepEqual(report.Samples, loaded.Samples) {
		t.Fatal("archive lost event evidence or eligibility")
	}
}
