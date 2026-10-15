package web

import (
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

func TestCandidateNotificationsRequireFreshTradableTriggers(t *testing.T) {
	now := realtimeWebTime(2026, 10, 15, 10, 30)
	base := realtime.Signal{Symbol: "sh600000", Name: "测试股票", State: realtime.StateTriggered, PortfolioEligible: true, Price: 12.3, Score: 75, AsOf: now, QuoteTime: now.Format("2006-01-02 15:04:05")}
	for _, test := range []struct {
		name   string
		change func(*realtime.Signal)
		want   int
	}{
		{"qualified", func(s *realtime.Signal) {}, 1},
		{"watching", func(s *realtime.Signal) { s.State = realtime.StateWatching }, 0},
		{"ineligible", func(s *realtime.Signal) { s.PortfolioEligible = false }, 0},
		{"stale quote", func(s *realtime.Signal) { s.QuoteTime = now.Add(-10 * time.Minute).Format("2006-01-02 15:04:05") }, 0},
		{"missing quote time", func(s *realtime.Signal) { s.QuoteTime = "" }, 0},
		{"stale signal", func(s *realtime.Signal) { s.AsOf = now.Add(-10 * time.Minute) }, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			signal := base
			test.change(&signal)
			result := realtime.ScanResult{GeneratedAt: now, MarketState: realtime.MarketStateTrading, Signals: []realtime.Signal{signal}}
			got := candidateNotifications(result, now)
			if len(got) != test.want {
				t.Fatalf("got %+v", got)
			}
			if len(got) == 1 {
				result.Signals[0].AsOf = now.Add(30 * time.Second)
				next := candidateNotifications(result, now.Add(30*time.Second))
				if len(next) != 1 || next[0].ID != got[0].ID {
					t.Fatal("each scan creates a new dedup key")
				}
			}
			result.MarketState = realtime.MarketStateClosed
			if len(candidateNotifications(result, now)) != 0 {
				t.Fatal("closed-market scan emitted a candidate")
			}
		})
	}
}

func TestShadowNotificationsOnlyIncludeNewLiveFillsAndDistinguishAccounts(t *testing.T) {
	now := realtimeWebTime(2026, 10, 15, 10, 30)
	base := paper.ShadowOrder{ID: "buy", EventSource: "realtime-signal", Symbol: "sh600000", Name: "测试股票", Side: "buy", Status: paper.OrderFilled, Quantity: 100, Price: 12.3, PositionAction: "open", ExecutionTime: now.Format("2006-01-02 15:04:05")}
	for _, test := range []struct {
		name   string
		change func(*paper.ShadowOrder)
		want   int
	}{
		{"buy", func(o *paper.ShadowOrder) {}, 1},
		{"sell", func(o *paper.ShadowOrder) {
			o.Side = "sell"
			o.PositionAction = "risk_exit"
			o.Reason = "跌破失效位"
		}, 1},
		{"rejected", func(o *paper.ShadowOrder) { o.Status = paper.OrderRejected }, 0},
		{"historical", func(o *paper.ShadowOrder) { o.EventSource = "daily" }, 0},
		{"stale", func(o *paper.ShadowOrder) { o.ExecutionTime = now.Add(-10 * time.Minute).Format(time.RFC3339) }, 0},
		{"future", func(o *paper.ShadowOrder) { o.ExecutionTime = now.Add(time.Minute).Format(time.RFC3339) }, 0},
		{"missing time", func(o *paper.ShadowOrder) { o.ExecutionTime = "" }, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			order := base
			test.change(&order)
			report := paper.Report{Orders: []paper.ShadowOrder{order}}
			got := shadowOrderNotifications("balanced", "均衡型", paper.Report{}, report, now)
			if len(got) != test.want {
				t.Fatalf("got %+v", got)
			}
			if len(got) == 1 {
				if !strings.Contains(got[0].Title, "模拟") || !strings.Contains(got[0].Subtitle, "均衡型") {
					t.Fatalf("missing simulated account identity: %+v", got[0])
				}
				other := shadowOrderNotifications("monster", "抓妖", paper.Report{}, report, now)
				if other[0].ID == got[0].ID {
					t.Fatal("independent accounts share notification IDs")
				}
			}
			if len(shadowOrderNotifications("balanced", "均衡型", report, report, now)) != 0 {
				t.Fatal("existing ledger was notified again")
			}
		})
	}
}
