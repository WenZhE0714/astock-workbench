package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

func TestPriceAlertStoreConcurrentTriggerAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "price-alerts.json")
	store := NewPriceAlertStore(path)
	now := time.Date(2026, 10, 20, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	alert := domain.PriceAlert{ID: "price-store-12345678", Rule: domain.PriceAlertRule{Symbol: "sh600110", Name: "test", Direction: "above", Price: 10, ExpiresOn: "2026-10-21"}, Revision: 1, CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute), EnabledAt: now.Add(-time.Minute), Enabled: true}
	if _, err := store.Update(func(s *domain.PriceAlertState) error { s.Alerts[alert.ID] = alert; return nil }); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := NewPriceAlertStore(path)
			_, err := other.Update(func(s *domain.PriceAlertState) error {
				s.Alerts[alert.ID] = strategy.AdvancePriceAlert(s.Alerts[alert.ID], now, domain.Quote{Symbol: alert.Rule.Symbol, Current: "11", QuoteTime: now.Format(time.RFC3339)}, "")
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	state, err := NewPriceAlertStore(path).Load()
	if err != nil || state.Alerts[alert.ID].Event == nil || state.Alerts[alert.ID].Enabled {
		t.Fatal("lost durable trigger", err)
	}
	if _, err := store.Update(func(s *domain.PriceAlertState) error {
		a := s.Alerts[alert.ID]
		a.Rule.Price = 12
		s.Alerts[alert.ID] = a
		return nil
	}); err == nil {
		t.Fatal("rule rewrite accepted")
	}
	if _, err := store.Update(func(s *domain.PriceAlertState) error {
		a := s.Alerts[alert.ID]
		a.Event.Price = 12
		s.Alerts[alert.ID] = a
		return nil
	}); err == nil {
		t.Fatal("event rewrite accepted")
	}
	unlock, err := store.AcquireWorker()
	if err != nil {
		t.Fatal(err)
	}
	if release, err := NewPriceAlertStore(path).AcquireWorker(); err == nil {
		release()
		t.Fatal("two workers acquired same lease")
	}
	unlock()
	release, err := NewPriceAlertStore(path).AcquireWorker()
	if err != nil {
		t.Fatal("worker lease did not recover", err)
	}
	release()
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("corrupt state accepted")
	}
}
