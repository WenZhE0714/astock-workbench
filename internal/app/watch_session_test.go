package app

import (
	"testing"
	"time"
)

func TestWatchSessionContinuesRefreshingAfterEachBreak(t *testing.T) {
	for _, boundary := range []struct {
		hour, minute int
		label        string
	}{{11, 30, "午间休市"}, {15, 0, "已收盘"}} {
		closedAt := time.Date(2026, 10, 13, boundary.hour, boundary.minute, 0, 0, shanghaiLocation)
		for _, elapsed := range []time.Duration{0, time.Second, time.Minute, 5*time.Minute - time.Nanosecond} {
			at := closedAt.Add(elapsed)
			watch := watchSessionAt(at)
			if !watch.Poll || watch.Continuous || watch.Label != boundary.label {
				t.Fatalf("watch stopped before delayed snapshots settled at %s: %+v", at, watch)
			}
			trading := marketSessionAt(at)
			if trading.Poll || trading.Continuous || trading.Label != boundary.label {
				t.Fatalf("refresh window extended the trading session at %s: %+v", at, trading)
			}
		}
		for _, elapsed := range []time.Duration{5 * time.Minute, 5*time.Minute + time.Second, 30 * time.Minute} {
			if session := watchSessionAt(closedAt.Add(elapsed)); session.Poll || session.Continuous || session.Label != boundary.label {
				t.Fatalf("watch did not stop after refresh window: %+v", session)
			}
		}
	}
}

func TestWatchSessionRefreshesAtBreakAndBeforePausing(t *testing.T) {
	for _, hour := range []int{11, 15} {
		minute := 0
		if hour == 11 {
			minute = 30
		}
		closedAt := time.Date(2026, 10, 13, hour, minute, 0, 0, shanghaiLocation)
		previous := watchSessionAt(closedAt.Add(-time.Second))
		refreshes := 0
		for _, elapsed := range []time.Duration{0, time.Second, 4 * time.Minute, 5 * time.Minute, 5*time.Minute + time.Second, 10 * time.Minute} {
			next := watchSessionAt(closedAt.Add(elapsed))
			refresh := watchSessionNeedsRefresh(previous, next)
			wantRefresh := elapsed == 0 || elapsed == 5*time.Minute
			if refresh != wantRefresh {
				t.Fatalf("unexpected transition refresh at %s: %t", closedAt.Add(elapsed), refresh)
			}
			if refresh {
				refreshes++
			}
			previous = next
		}
		if refreshes != 2 {
			t.Fatalf("expected break and final refresh, got %d", refreshes)
		}
	}
}

func TestWatchSessionResumesAndUsesShanghaiClock(t *testing.T) {
	before := time.Date(2026, 10, 13, 12, 59, 59, 0, shanghaiLocation)
	after := before.Add(time.Second)
	if !watchSessionNeedsRefresh(watchSessionAt(before), watchSessionAt(after)) || !watchSessionAt(after).Continuous {
		t.Fatal("afternoon polling did not resume")
	}
	utc := time.Date(2026, 10, 13, 7, 2, 0, 0, time.UTC)
	if session := watchSessionAt(utc); !session.Poll || session.Label != "已收盘" || session.Continuous {
		t.Fatalf("refresh window used the process timezone: %+v", session)
	}
	weekend := time.Date(2026, 10, 17, 15, 2, 0, 0, shanghaiLocation)
	if session := watchSessionAt(weekend); session.Poll || session.Label != "休市" {
		t.Fatalf("weekend incorrectly gained a refresh window: %+v", session)
	}
	if watchSessionNeedsRefresh(watchSessionAt(utc), watchSessionAt(weekend)) {
		t.Fatal("resuming a sleeping process started a weekend refresh")
	}
}
