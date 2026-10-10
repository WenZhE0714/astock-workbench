package strategy

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

var reviewCalendarLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func ReviewCalendarMonth(value string) (time.Time, error) {
	month, err := time.ParseInLocation("2006-01", value, reviewCalendarLocation)
	if err != nil || month.Format("2006-01") != value || month.Year() < 1900 || month.Year() > 2200 {
		return time.Time{}, fmt.Errorf("月份格式应为 YYYY-MM，年份范围1900至2200")
	}
	return month, nil
}

// Only the latest validated review contributes an exit. Editing a review
// cannot move its outcome from the recorded exit date to the edit date.
func ManualCalendarEvents(report domain.TradePlaybookReport) []domain.ReviewCalendarEvent {
	events := make([]domain.ReviewCalendarEvent, 0, len(report.Recent)*3)
	for _, item := range report.Recent {
		base := domain.ReviewCalendarEvent{Symbol: item.Symbol, Name: item.Name, PlanID: item.PlanID,
			Label: item.StructureName, ExecutionStatus: item.ExecutionStatus, Discipline: item.Discipline, Tags: item.Tags}
		add := func(kind string, at time.Time, price, r *float64) {
			event := base
			event.ID, event.Kind, event.At, event.Price, event.R = item.PlanID+":"+kind, kind, at, price, r
			if !at.IsZero() {
				event.Date = at.In(reviewCalendarLocation).Format(time.DateOnly)
			}
			if event.Date == "" {
				event.Note = "实际日期缺失，未计入月度统计"
			}
			if kind == "review" {
				event.Note = strings.TrimSpace(item.ExitReason + " " + item.StatisticsNote)
			}
			events = append(events, event)
		}
		add("plan", item.CreatedAt, nil, nil)
		if playbookPriceValid(item.ActualEntry) {
			add("entry", item.EntryAt, item.ActualEntry, nil)
		}
		if item.RealizedR != nil {
			add("exit", item.ExitAt, item.ActualExit, item.RealizedR)
		}
		if item.Reviewed {
			add("review", item.ReviewUpdatedAt, nil, nil)
		}
	}
	return events
}

func calendarTotals(events []domain.ReviewCalendarEvent, source string) domain.ReviewCalendarTotals {
	result := domain.ReviewCalendarTotals{}
	values := []float64{}
	net, closed := 0.0, 0
	for _, event := range events {
		switch event.Kind {
		case "plan":
			result.Plans++
		case "entry":
			result.Entries++
		case "review":
			result.Reviews++
			if event.ExecutionStatus == "deviated" || event.Discipline == "deviated" {
				result.Deviations++
			}
		case "exit":
			result.Exits++
			if source == "manual" && event.R != nil {
				values = append(values, *event.R)
			}
			if source == "shadow" && event.NetProfit != nil {
				net += *event.NetProfit
				closed++
			}
		}
	}
	if len(values) > 0 {
		result.AverageR, _ = playbookMeanMedian(values)
	}
	if closed > 0 {
		value := playbookRounded(net)
		result.NetProfit = &value
	}
	return result
}

func BuildReviewCalendar(month time.Time, source string, events []domain.ReviewCalendarEvent, now time.Time) domain.ReviewCalendarReport {
	report := domain.ReviewCalendarReport{Month: month.Format("2006-01"), Source: source, Timezone: "Asia/Shanghai", GeneratedAt: now,
		Days: []domain.ReviewCalendarDay{}, Undated: []domain.ReviewCalendarEvent{}, Warnings: []string{}}
	byDate := map[string][]domain.ReviewCalendarEvent{}
	seen := map[string]bool{}
	monthly := []domain.ReviewCalendarEvent{}
	for _, event := range events {
		key := event.Symbol + ":" + event.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		if event.Date == "" {
			report.Undated = append(report.Undated, event)
			continue
		}
		if !strings.HasPrefix(event.Date, report.Month+"-") {
			continue
		}
		date, err := time.Parse(time.DateOnly, event.Date)
		if err != nil || date.Format(time.DateOnly) != event.Date {
			continue
		}
		byDate[event.Date] = append(byDate[event.Date], event)
		monthly = append(monthly, event)
	}
	for day := month; day.Month() == month.Month(); day = day.AddDate(0, 0, 1) {
		date := day.Format(time.DateOnly)
		items := byDate[date]
		if items == nil {
			items = []domain.ReviewCalendarEvent{}
		}
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].At.Equal(items[j].At) {
				return items[i].ID < items[j].ID
			}
			return items[i].At.Before(items[j].At)
		})
		report.Days = append(report.Days, domain.ReviewCalendarDay{Date: date, Events: items, Totals: calendarTotals(items, source)})
	}
	report.Totals = calendarTotals(monthly, source)
	return report
}
