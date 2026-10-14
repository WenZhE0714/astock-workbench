package web

import (
	"context"
	"time"
)

type tradingCalendarSnapshotKey struct{}

type tradingCalendarSnapshot struct {
	server *Server
	date   string
	dates  []string
}

// Session checks and account checkpoints share a calendar within one cycle.
// A load error must not become an empty weekday calendar.
func (s *Server) withTradingCalendarSnapshot(ctx context.Context, now time.Time) context.Context {
	calendarCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	dates, err := s.tradingCalendar(calendarCtx, now)
	cancel()
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, tradingCalendarSnapshotKey{}, tradingCalendarSnapshot{
		server: s, date: now.In(realtimeWebLocation).Format("2006-01-02"), dates: append([]string(nil), dates...),
	})
}
