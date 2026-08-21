package realtime

import "time"

const (
	MarketStateAuction = "auction"
	MarketStateTrading = "trading"
	MarketStateBreak   = "break"
	MarketStateClosed  = "closed"

	closingSnapshotWindow = 5 * time.Minute
)

var marketLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

// MarketSession is the shared scheduling contract for realtime scans. It uses
// Shanghai exchange clock time and intentionally does not claim holiday
// awareness until a point-in-time exchange calendar is available.
type MarketSession struct {
	State               string
	ScanAllowed         bool
	FinalizationAllowed bool
	TradingDate         string
	CloseAt             time.Time
	NextScanAt          time.Time
}

func MarketSessionAt(now time.Time) MarketSession {
	local := now.In(marketLocation)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, marketLocation)
	closeAt := day.Add(15 * time.Hour)
	session := MarketSession{
		State: MarketStateClosed, TradingDate: day.Format("2006-01-02"), CloseAt: closeAt,
	}
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
		session.NextScanAt = nextWeekdayAt(day.AddDate(0, 0, 1), 9, 15)
		return session
	}
	auctionAt := day.Add(9*time.Hour + 15*time.Minute)
	openAt := day.Add(9*time.Hour + 30*time.Minute)
	morningCloseAt := day.Add(11*time.Hour + 30*time.Minute)
	afternoonAt := day.Add(13 * time.Hour)
	switch {
	case local.Before(auctionAt):
		session.NextScanAt = auctionAt
	case local.Before(openAt):
		session.State = MarketStateAuction
		session.ScanAllowed = true
	case local.Before(morningCloseAt):
		session.State = MarketStateTrading
		session.ScanAllowed = true
	case local.Before(afternoonAt):
		session.State = MarketStateBreak
		session.NextScanAt = afternoonAt
	case local.Before(closeAt):
		session.State = MarketStateTrading
		session.ScanAllowed = true
	default:
		session.FinalizationAllowed = !local.After(closeAt.Add(closingSnapshotWindow))
		session.NextScanAt = nextWeekdayAt(day.AddDate(0, 0, 1), 9, 15)
	}
	return session
}

func (session MarketSession) ShouldFinalize(previous time.Time) bool {
	if !session.FinalizationAllowed {
		return false
	}
	if previous.IsZero() {
		return true
	}
	local := previous.In(marketLocation)
	return local.Format("2006-01-02") != session.TradingDate || local.Before(session.CloseAt)
}

func nextWeekdayAt(day time.Time, hour, minute int) time.Time {
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, 1)
	}
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, marketLocation)
}
