package realtime

import (
	"testing"
	"time"
)

func TestBuildSignalLifecyclesCollapsesSnapshotsAndAttachesOutcomes(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	at := func(hour, minute int) time.Time { return time.Date(2026, 9, 21, hour, minute, 0, 0, location) }
	signals := []Signal{
		{ID: "a", Symbol: "sh600519", Name: "贵州茅台", State: StateWatching, Score: 60, Price: 1450, AsOf: at(9, 35), Reasons: []string{"回踩观察"}},
		{ID: "b", Symbol: "sh600519", Name: "贵州茅台", State: StateWatching, Score: 61, Price: 1451, AsOf: at(9, 36), Reasons: []string{"回踩观察"}},
		{ID: "c", Symbol: "sh600519", Name: "贵州茅台", State: StateTriggered, Score: 74, Price: 1460, AsOf: at(9, 40), TriggerPrice: 1458, InvalidationPrice: 1410, Reasons: []string{"放量确认"}},
	}
	outcomes := []SignalOutcome{{Symbol: "sh600519", SignalDate: "2026-09-21", Horizon: 1, Status: OutcomeReady, ReturnPercent: 2.5}}
	items := BuildSignalLifecycles(signals, outcomes, 10)
	if len(items) != 1 {
		t.Fatalf("unexpected lifecycle count: %+v", items)
	}
	item := items[0]
	if item.LatestState != StateTriggered || item.PeakScore != 74 || item.MatureHorizon != 1 || item.MatureReturnPercent != 2.5 || item.Status != "matured" {
		t.Fatalf("unexpected lifecycle summary: %+v", item)
	}
	if len(item.Events) != 2 || item.Events[0].Kind != "first-seen" || item.Events[1].Kind != "state-change" {
		t.Fatalf("repeated snapshot was not collapsed: %+v", item.Events)
	}
}

func TestBuildSignalLifecyclesSeparatesTradingDates(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	items := BuildSignalLifecycles([]Signal{
		{ID: "a", Symbol: "sz000001", State: StateTriggered, Score: 75, AsOf: time.Date(2026, 9, 21, 10, 0, 0, 0, location)},
		{ID: "b", Symbol: "sz000001", State: StateWeak, Score: 40, AsOf: time.Date(2026, 9, 22, 10, 0, 0, 0, location)},
	}, nil, 10)
	if len(items) != 2 || items[0].SignalDate != "2026-09-22" || items[1].SignalDate != "2026-09-21" {
		t.Fatalf("trading dates were merged: %+v", items)
	}
}
