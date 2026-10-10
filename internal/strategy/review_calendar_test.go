package strategy

import (
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestManualCalendarUsesExitDateInsteadOfRevisionDate(t *testing.T) {
	entry, exit, r := 100.0, 104.0, 2.0
	report := domain.TradePlaybookReport{Recent: []domain.TradePlaybookItem{{
		PlanID: "plan", Symbol: "sh600519", StructureName: "breakout", Reviewed: true, ExecutionStatus: "deviated", Discipline: "deviated",
		CreatedAt: time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC), EntryAt: time.Date(2026, 9, 29, 2, 0, 0, 0, time.UTC),
		ExitAt: time.Date(2026, 9, 30, 16, 30, 0, 0, time.UTC), ReviewUpdatedAt: time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC),
		ActualEntry: &entry, ActualExit: &exit, RealizedR: &r,
	}}}
	events := ManualCalendarEvents(report)
	month, _ := ReviewCalendarMonth("2026-10")
	calendar := BuildReviewCalendar(month, "manual", events, month)
	if calendar.Days[0].Totals.Exits != 1 || *calendar.Days[0].Totals.AverageR != 2 || calendar.Days[2].Totals.Exits != 0 || calendar.Days[2].Totals.Deviations != 1 {
		t.Fatalf("exit/edit dates mixed: %+v", calendar.Days[:3])
	}
	if calendar.Totals.NetProfit != nil || calendar.Totals.Plans != 0 {
		t.Fatalf("units or months mixed: %+v", calendar.Totals)
	}
	report.Recent[0].ExitAt = time.Time{}
	calendar = BuildReviewCalendar(month, "manual", ManualCalendarEvents(report), month)
	if calendar.Totals.Exits != 0 || calendar.Totals.AverageR != nil || len(calendar.Undated) != 1 || calendar.Undated[0].Kind != "exit" {
		t.Fatalf("undated R credited to edit date: %+v", calendar)
	}
}

func TestCalendarMonthsAndEmptyMetrics(t *testing.T) {
	for _, invalid := range []string{"2026-00", "2026-13", "2026-2", "2026-02-01", "../2026", "0000-01"} {
		if _, err := ReviewCalendarMonth(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	for month, count := range map[string]int{"2024-02": 29, "2100-02": 28, "2026-12": 31} {
		start, _ := ReviewCalendarMonth(month)
		report := BuildReviewCalendar(start, "manual", nil, start)
		if len(report.Days) != count || report.Totals.AverageR != nil || report.Totals.NetProfit != nil {
			t.Fatalf("bad calendar %s: %+v", month, report)
		}
	}
}

func TestCalendarWeightedAverageAndDuplicateEvents(t *testing.T) {
	month, _ := ReviewCalendarMonth("2026-10")
	values := []float64{1, 3, -1}
	events := []domain.ReviewCalendarEvent{
		{ID: "a", Date: "2026-10-01", Kind: "exit", Symbol: "a", R: &values[0]},
		{ID: "b", Date: "2026-10-01", Kind: "exit", Symbol: "b", R: &values[1]},
		{ID: "c", Date: "2026-10-02", Kind: "exit", Symbol: "c", R: &values[2]},
	}
	events = append(events, events[0])
	report := BuildReviewCalendar(month, "manual", events, month)
	if report.Totals.Exits != 3 || *report.Totals.AverageR != 1 {
		t.Fatalf("not weighted by exits: %+v", report.Totals)
	}
}
