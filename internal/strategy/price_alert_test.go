package strategy

import (
	"reflect"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func priceAlertFixture() (domain.PriceAlert, time.Time, domain.Quote) {
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, priceAlertLocation)
	alert := domain.PriceAlert{ID: "price-test-12345678", Rule: domain.PriceAlertRule{Symbol: "sh600110", Name: "test", Direction: "above", Price: 10, ExpiresOn: "2026-10-21"}, Revision: 1, CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute), EnabledAt: now.Add(-time.Minute), Enabled: true}
	quote := domain.Quote{Symbol: "sh600110", Current: "10", QuoteTime: now.Format(time.RFC3339)}
	return alert, now, quote
}

func TestPriceAlertLevelInclusiveAndSingleTerminalEvent(t *testing.T) {
	for _, direction := range []string{"above", "below"} {
		alert, now, quote := priceAlertFixture()
		alert.Rule.Direction = direction
		triggered := AdvancePriceAlert(alert, now, quote, "")
		if triggered.Event == nil || triggered.Enabled || triggered.Event.Price != 10 {
			t.Fatal("boundary failed", triggered)
		}
		if err := ValidatePriceAlert(triggered); err != nil {
			t.Fatal(err)
		}
		if again := AdvancePriceAlert(triggered, now.Add(time.Second), quote, ""); !reflect.DeepEqual(again, triggered) {
			t.Fatal("terminal trigger changed")
		}
	}
}

func TestPriceAlertRejectsStaleFutureWrongSymbolAndPreEnableQuotes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*domain.PriceAlert, *domain.Quote, *time.Time)
	}{
		{"stale", func(a *domain.PriceAlert, q *domain.Quote, n *time.Time) {
			q.QuoteTime = n.Add(-91 * time.Second).Format(time.RFC3339)
		}},
		{"future", func(a *domain.PriceAlert, q *domain.Quote, n *time.Time) {
			q.QuoteTime = n.Add(time.Second).Format(time.RFC3339)
		}},
		{"before-enable", func(a *domain.PriceAlert, q *domain.Quote, n *time.Time) {
			a.EnabledAt = n.Add(-time.Second)
			q.QuoteTime = n.Add(-2 * time.Second).Format(time.RFC3339)
		}},
		{"wrong-symbol", func(a *domain.PriceAlert, q *domain.Quote, n *time.Time) { q.Symbol = "sz002413" }},
		{"nonfinite", func(a *domain.PriceAlert, q *domain.Quote, n *time.Time) { q.Current = "NaN" }},
		{"paused", func(a *domain.PriceAlert, q *domain.Quote, n *time.Time) { a.Enabled = false }},
		{"expired", func(a *domain.PriceAlert, q *domain.Quote, n *time.Time) { a.Rule.ExpiresOn = "2026-10-19" }},
		{"deleted", func(a *domain.PriceAlert, q *domain.Quote, n *time.Time) { a.DeletedAt = *n; a.Enabled = false }},
		{"lunch", func(a *domain.PriceAlert, q *domain.Quote, n *time.Time) {
			*n = n.Add(2 * time.Hour)
			q.QuoteTime = n.Format(time.RFC3339)
		}},
		{"weekend", func(a *domain.PriceAlert, q *domain.Quote, n *time.Time) {
			*n = n.AddDate(0, 0, 4)
			a.Rule.ExpiresOn = "2026-11-01"
			q.QuoteTime = n.Format(time.RFC3339)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, n, q := priceAlertFixture()
			tc.mutate(&a, &q, &n)
			got := AdvancePriceAlert(a, n, q, "")
			if got.Event != nil {
				t.Fatal("invalid observation triggered")
			}
		})
	}
	alert, now, quote := priceAlertFixture()
	if AdvancePriceAlert(alert, now, quote, "offline").Event != nil {
		t.Fatal("failed fetch triggered")
	}
}

func TestPriceAlertDuplicateQuoteAndConcurrentPause(t *testing.T) {
	alert, now, quote := priceAlertFixture()
	quote.Current = "9"
	observed := AdvancePriceAlert(alert, now, quote, "")
	quote.Current = "11"
	if AdvancePriceAlert(observed, now.Add(time.Second), quote, "").Event != nil {
		t.Fatal("same quote changed its result")
	}
	observed.Enabled = false
	observed.UpdatedAt = now.Add(3 * time.Second)
	if result := AdvancePriceAlert(observed, now.Add(2*time.Second), quote, ""); !reflect.DeepEqual(result, observed) {
		t.Fatal("older fetch overwrote newer control")
	}
}

func TestPriceAlertExpiryDoesNotRewriteTerminalState(t *testing.T) {
	alert, now, quote := priceAlertFixture()
	alert.Rule.ExpiresOn = "2026-10-19"
	expired := AdvancePriceAlert(alert, now, quote, "")
	if expired.Enabled || expired.CheckState != "expired" {
		t.Fatal("rule did not expire")
	}
	if next := AdvancePriceAlert(expired, now.Add(time.Minute), quote, ""); !reflect.DeepEqual(next, expired) {
		t.Fatal("expired state was rewritten")
	}
	alert.Rule.Symbol = "bj899050"
	if ValidatePriceAlertRule(alert.Rule) == nil {
		t.Fatal("index accepted as an A-share stock")
	}
}
