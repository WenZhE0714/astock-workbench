package realtime

import (
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const calendarDateLayout = "2006-01-02"

// NormalizeTradingDates returns a sorted, de-duplicated set of valid exchange
// dates. Dates outside the ISO calendar format are ignored so an incomplete
// upstream row cannot change session classification.
func NormalizeTradingDates(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	result := make([]string, 0, len(input))
	for _, raw := range input {
		value := strings.TrimSpace(raw)
		if _, err := time.Parse(calendarDateLayout, value); err != nil {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// TradingDatesFromBars extracts the exchange dates from a benchmark series.
// A benchmark K-line is a useful fallback calendar because it includes
// exchange make-up Saturdays and omits statutory holidays.
func TradingDatesFromBars(bars []domain.DailyBar) []string {
	dates := make([]string, 0, len(bars))
	for _, bar := range bars {
		dates = append(dates, bar.Date)
	}
	return NormalizeTradingDates(dates)
}

// tradingDateStatus reports whether a date is known to the supplied calendar
// and whether it is a trading date. Outside the covered range we deliberately
// return unknown so a short historical window does not classify every future
// weekday as a holiday.
func tradingDateStatus(date string, calendarDates []string) (trading, known bool) {
	if len(calendarDates) == 0 {
		return dateWeekday(date), false
	}
	index := sort.SearchStrings(calendarDates, date)
	if index < len(calendarDates) && calendarDates[index] == date {
		return true, true
	}
	if date < calendarDates[0] || date > calendarDates[len(calendarDates)-1] {
		return dateWeekday(date), false
	}
	return false, true
}

func dateWeekday(value string) bool {
	date, err := time.ParseInLocation(calendarDateLayout, value, marketLocation)
	if err != nil {
		return false
	}
	return date.Weekday() != time.Saturday && date.Weekday() != time.Sunday
}

func nextTradingAt(day time.Time, calendarDates []string, hour, minute int) time.Time {
	if len(calendarDates) > 0 {
		date := day.Format(calendarDateLayout)
		index := sort.SearchStrings(calendarDates, date)
		for index < len(calendarDates) {
			candidate, err := time.ParseInLocation(calendarDateLayout, calendarDates[index], marketLocation)
			if err == nil && candidate.After(day) {
				return time.Date(candidate.Year(), candidate.Month(), candidate.Day(), hour, minute, 0, 0, marketLocation)
			}
			index++
		}
	}
	return nextWeekdayAt(day.AddDate(0, 0, 1), hour, minute)
}
