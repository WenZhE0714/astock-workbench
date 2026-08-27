package realtime

import (
	"testing"
	"time"
)

func sessionTime(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, marketLocation)
}

func TestMarketSessionAtFollowsShanghaiTradingClock(t *testing.T) {
	tests := []struct {
		name          string
		now           time.Time
		state         string
		allowed       bool
		finalize      bool
		nextHour      int
		nextMinute    int
		nextDayOffset int
	}{
		{name: "preopen", now: sessionTime(2026, time.August, 20, 8, 30), state: MarketStateClosed, nextHour: 9, nextMinute: 15},
		{name: "auction", now: sessionTime(2026, time.August, 20, 9, 20), state: MarketStateAuction, allowed: true},
		{name: "morning", now: sessionTime(2026, time.August, 20, 10, 30), state: MarketStateTrading, allowed: true},
		{name: "lunch", now: sessionTime(2026, time.August, 20, 12, 0), state: MarketStateBreak, nextHour: 13},
		{name: "afternoon", now: sessionTime(2026, time.August, 20, 14, 30), state: MarketStateTrading, allowed: true},
		{name: "closing instant", now: sessionTime(2026, time.August, 20, 15, 0), state: MarketStateClosed, finalize: true, nextHour: 9, nextMinute: 15, nextDayOffset: 1},
		{name: "closing snapshot", now: sessionTime(2026, time.August, 20, 15, 2), state: MarketStateClosed, finalize: true, nextHour: 9, nextMinute: 15, nextDayOffset: 1},
		{name: "after close", now: sessionTime(2026, time.August, 20, 16, 0), state: MarketStateClosed, nextHour: 9, nextMinute: 15, nextDayOffset: 1},
		{name: "weekend", now: sessionTime(2026, time.August, 22, 10, 0), state: MarketStateClosed, nextHour: 9, nextMinute: 15, nextDayOffset: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := MarketSessionAt(test.now)
			if session.State != test.state || session.ScanAllowed != test.allowed || session.FinalizationAllowed != test.finalize {
				t.Fatalf("unexpected session: %+v", session)
			}
			if test.allowed {
				if !session.NextScanAt.IsZero() {
					t.Fatalf("active session should not schedule a reopen: %+v", session)
				}
				return
			}
			expectedDay := test.now.In(marketLocation).AddDate(0, 0, test.nextDayOffset)
			if session.NextScanAt.Day() != expectedDay.Day() || session.NextScanAt.Hour() != test.nextHour || session.NextScanAt.Minute() != test.nextMinute {
				t.Fatalf("unexpected next scan time: %s", session.NextScanAt)
			}
		})
	}
}

func TestMarketSessionFinalizesOnlyOnceAfterClose(t *testing.T) {
	session := MarketSessionAt(sessionTime(2026, time.August, 20, 15, 2))
	if !session.ShouldFinalize(sessionTime(2026, time.August, 20, 14, 59)) {
		t.Fatal("pre-close snapshot should be finalized")
	}
	if session.ShouldFinalize(sessionTime(2026, time.August, 20, 15, 1)) {
		t.Fatal("post-close snapshot should not be finalized twice")
	}
	late := MarketSessionAt(sessionTime(2026, time.August, 20, 15, 6))
	if late.ShouldFinalize(time.Time{}) {
		t.Fatal("late requests must not trigger an after-hours scan")
	}
}

func TestMarketSessionUsesExchangeCalendarForHolidayAndMakeupDay(t *testing.T) {
	calendar := []string{"2026-08-20", "2026-08-22", "2026-08-24"}
	holiday := MarketSessionAtWithCalendar(sessionTime(2026, time.August, 21, 10, 0), calendar)
	if holiday.TradingDay || !holiday.CalendarKnown || holiday.ScanAllowed || holiday.FinalizationAllowed {
		t.Fatalf("holiday was treated as tradable: %+v", holiday)
	}
	if holiday.NextScanAt.Day() != 22 || holiday.NextScanAt.Hour() != 9 || holiday.NextScanAt.Minute() != 15 {
		t.Fatalf("holiday did not advance to next exchange date: %s", holiday.NextScanAt)
	}
	if holiday.ShouldFinalize(time.Time{}) {
		t.Fatal("holiday must not finalize a snapshot")
	}

	makeupSaturday := MarketSessionAtWithCalendar(sessionTime(2026, time.August, 22, 10, 0), calendar)
	if !makeupSaturday.TradingDay || !makeupSaturday.CalendarKnown || !makeupSaturday.ScanAllowed || makeupSaturday.State != MarketStateTrading {
		t.Fatalf("make-up Saturday was not tradable: %+v", makeupSaturday)
	}
}

func TestNormalizeTradingDatesDropsInvalidRowsAndPreservesOrder(t *testing.T) {
	got := NormalizeTradingDates([]string{"2026-08-24", "bad", "2026-08-22", "2026-08-24", "2026-02-30"})
	want := []string{"2026-08-22", "2026-08-24"}
	if len(got) != len(want) {
		t.Fatalf("unexpected normalized dates: %#v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("unexpected normalized dates: %#v", got)
		}
	}
}
