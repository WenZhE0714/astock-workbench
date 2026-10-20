package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/notify"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type priceAlertQuoteStub struct {
	mu    sync.Mutex
	price string
	now   func() time.Time
}

func (q *priceAlertQuoteStub) Fetch(_ context.Context, symbols []string) ([]domain.Quote, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var result []domain.Quote
	for _, symbol := range symbols {
		result = append(result, domain.Quote{Symbol: symbol, Name: "测试股票", Current: q.price, QuoteTime: q.now().Format(time.RFC3339Nano)})
	}
	return result, nil
}

// The browser fixture records submissions without invoking the desktop.
type priceAlertRecordingDispatcher struct {
	mu    sync.Mutex
	store *storage.NativeNotificationStore
	count int
}

func (d *priceAlertRecordingDispatcher) Run(ctx context.Context) { <-ctx.Done() }
func (d *priceAlertRecordingDispatcher) Offer(message notify.Message) {
	d.mu.Lock()
	defer d.mu.Unlock()
	state, err := d.store.Load()
	if err != nil {
		panic(err)
	}
	if state.Records == nil {
		state.Records = map[string]notify.Record{}
	}
	if _, exists := state.Records[message.ID]; exists {
		return
	}
	state.Records[message.ID] = notify.Record{Message: message, Status: "submitted", Attempts: 1, SubmittedAt: message.At}
	if err := d.store.Save(state); err != nil {
		panic(err)
	}
	d.count++
}

func priceAlertServer(t *testing.T) (*Server, *storage.PriceAlertStore, *priceAlertQuoteStub, *priceAlertRecordingDispatcher) {
	t.Helper()
	root := t.TempDir()
	start := time.Now()
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, realtimeWebLocation)
	now := func() time.Time { return base.Add(time.Since(start)) }
	quotes := &priceAlertQuoteStub{price: "9", now: now}
	store := storage.NewPriceAlertStore(filepath.Join(root, "price-alerts.json"))
	records := storage.NewNativeNotificationStore(filepath.Join(root, "mac.json"))
	dispatcher := &priceAlertRecordingDispatcher{store: records}
	s := NewServer(resolverStub{}, quotes, nil, nil, "sh600110", WithPriceAlerts(store), WithNotificationCenter(records, storage.NewNotificationReviewStore(filepath.Join(root, "reviews.json"))))
	s.now = now
	s.nativeNotifications = dispatcher
	s.priceAlertInterval = time.Second
	return s, store, quotes, dispatcher
}

func alertRequest(s *Server, method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/price-alerts", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func createAlertBody(id string) string {
	return fmt.Sprintf(`{"action":"create","client_id":%q,"confirmed":true,"rule":{"symbol":"sh600110","name":"测试价位","direction":"above","price":10,"expires_on":"2026-10-21"}}`, id)
}

func TestPriceAlertNilNativeDispatcherStaysDisabled(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, "", WithNativeNotifications(nil))
	if s.nativeNotifications != nil {
		t.Fatal("typed nil enabled native notifications")
	}
}

func TestPriceAlertsAPIExplicitConfirmationAndControls(t *testing.T) {
	s, store, _, _ := priceAlertServer(t)
	if r := alertRequest(s, "POST", strings.Replace(createAlertBody("api-test-1234"), `"confirmed":true`, `"confirmed":false`, 1)); r.Code != 400 {
		t.Fatal("implicit create allowed")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/price-alerts", strings.NewReader(createAlertBody("api-test-1234")))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://untrusted.invalid")
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin control allowed")
	}
	for i := 0; i < 2; i++ {
		if r := alertRequest(s, "POST", createAlertBody("api-test-1234")); r.Code != 200 {
			t.Fatal(r.Body.String())
		}
	}
	state, err := store.Load()
	if err != nil || len(state.Alerts) != 1 {
		t.Fatal("idempotency lost", err)
	}
	a := state.Alerts["price-api-test-1234"]
	if r := alertRequest(s, "POST", fmt.Sprintf(`{"action":"enable","id":%q,"revision":1}`, a.ID)); r.Code != 409 {
		t.Fatal("missing boolean accepted")
	}
	if r := alertRequest(s, "POST", fmt.Sprintf(`{"action":"enable","id":%q,"revision":1,"enabled":false}`, a.ID)); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if r := alertRequest(s, "POST", fmt.Sprintf(`{"action":"enable","id":%q,"revision":1,"enabled":true}`, a.ID)); r.Code != 409 {
		t.Fatal("stale revision accepted")
	}
	if r := alertRequest(s, "POST", fmt.Sprintf(`{"action":"delete","id":%q,"revision":2}`, a.ID)); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if r := alertRequest(s, "GET", ""); r.Code != 200 || strings.Contains(r.Body.String(), a.ID) {
		t.Fatal("deleted rule still active")
	}
	state, _ = store.Load()
	if state.Alerts[a.ID].DeletedAt.IsZero() {
		t.Fatal("delete removed audit tombstone")
	}
	if r := alertRequest(s, "POST", createAlertBody("api-test-1234")); r.Code != 409 {
		t.Fatal("deleted request silently reported active")
	}
}

func TestPriceAlertCyclePersistsEventAndRecoversOutboxWithoutBrowser(t *testing.T) {
	s, store, quotes, dispatcher := priceAlertServer(t)
	if r := alertRequest(s, "POST", createAlertBody("outbox-test-1234")); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	quotes.mu.Lock()
	quotes.price = "11"
	quotes.mu.Unlock()
	s.nativeNotifications = nil
	s.runPriceAlertCycle(t.Context())
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	event := state.Alerts["price-outbox-test-1234"].Event
	if event == nil {
		t.Fatal("no durable event without a browser")
	}
	// A new server starts after event persistence, before any desktop submission.
	s2 := NewServer(resolverStub{}, quotes, nil, nil, "", WithPriceAlerts(store))
	s2.now = s.now
	s2.nativeNotifications = dispatcher
	s2.runPriceAlertCycle(t.Context())
	s2.runPriceAlertCycle(t.Context())
	if dispatcher.count != 1 {
		t.Fatalf("outbox duplicated or lost: %d", dispatcher.count)
	}
	state, _ = store.Load()
	if state.Alerts["price-outbox-test-1234"].Event.ID != event.ID {
		t.Fatal("restart rewrote trigger")
	}
	if r := alertRequest(s2, "POST", `{"action":"enable","id":"price-outbox-test-1234","revision":1,"enabled":true}`); r.Code != 409 {
		t.Fatal("terminal rule rearmed implicitly")
	}
	if r := alertRequest(s2, "POST", `{"action":"enable","id":"price-outbox-test-1234","revision":1,"enabled":false}`); r.Code != 409 {
		t.Fatal("late pause rewrote a completed trigger")
	}
}

func TestPriceAlertWorkerStartsAndReleasesLease(t *testing.T) {
	s, store, _, _ := priceAlertServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); s.runPriceAlertLoop(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.priceAlertMu.Lock()
		running := s.priceAlertRuntime.Running
		s.priceAlertMu.Unlock()
		if running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if release, err := store.AcquireWorker(); err == nil {
		release()
		cancel()
		<-done
		t.Fatal("worker did not hold lease")
	}
	cancel()
	<-done
	release, err := store.AcquireWorker()
	if err != nil {
		t.Fatal("lease not released", err)
	}
	release()
}

func TestPriceAlertBrowserFixture(t *testing.T) {
	port := os.Getenv("ASTOCK_PRICE_ALERT_BROWSER_PORT")
	if port == "" {
		t.Skip("browser fixture opt-in")
	}
	s, _, quotes, dispatcher := priceAlertServer(t)
	if root := os.Getenv("ASTOCK_PRICE_ALERT_REPLAY_FIXTURE"); root != "" {
		WithFrozenReplay("historical", "历史演练", "historical", root)(s)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.runPriceAlertLoop(ctx) }()
	defer func() { cancel(); <-done }()
	stop := make(chan struct{})
	var once sync.Once
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test/quote":
			price, err := strconv.ParseFloat(r.URL.Query().Get("price"), 64)
			if err != nil {
				http.Error(w, "bad price", 400)
				return
			}
			quotes.mu.Lock()
			quotes.price = fmt.Sprint(price)
			quotes.mu.Unlock()
			s.quoteMu.Lock()
			s.quoteCache = map[string]quoteCacheEntry{}
			s.quoteMu.Unlock()
			writeJSON(w, 200, map[string]bool{"ok": true})
		case "/test/submissions":
			dispatcher.mu.Lock()
			count := dispatcher.count
			dispatcher.mu.Unlock()
			writeJSON(w, 200, map[string]int{"count": count})
		case "/test/stop":
			writeJSON(w, 200, map[string]bool{"ok": true})
			once.Do(func() { close(stop) })
		default:
			s.Handler().ServeHTTP(w, r)
		}
	})
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Close()
	t.Log("price-alert browser fixture ready on", port)
	select {
	case <-stop:
	case <-ctx.Done():
	case <-time.After(4 * time.Minute):
		t.Fatal("browser fixture timed out")
	}
}
