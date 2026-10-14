package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type forwardClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *forwardClock) Now() time.Time    { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *forwardClock) Set(now time.Time) { c.mu.Lock(); c.now = now; c.mu.Unlock() }

type forwardHistoryFixture struct {
	mu               sync.Mutex
	stock, benchmark []domain.DailyBar
	clock            *forwardClock
	calls            map[string]int
	ends             []string
	stale, block     string
	crossMidnight    bool
}

func (f *forwardHistoryFixture) FetchDailyBarsRange(ctx context.Context, symbol string, _, end time.Time, _ backtest.PriceAdjustment) ([]domain.DailyBar, error) {
	f.mu.Lock()
	f.calls[symbol]++
	f.ends = append(f.ends, end.Format(time.DateOnly))
	stale, block, cross := f.stale == symbol, f.block == symbol, f.crossMidnight && symbol != "sh000300"
	f.mu.Unlock()
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	bars := f.stock
	if symbol == "sh000300" {
		bars = f.benchmark
	}
	result := []domain.DailyBar{}
	for _, bar := range bars {
		if stale && bar.Date >= end.Format(time.DateOnly) {
			continue
		}
		bar.Symbol = symbol
		result = append(result, bar)
	}
	if cross {
		f.clock.Set(f.clock.Now().AddDate(0, 0, 1))
	}
	return result, nil
}
func (f *forwardHistoryFixture) count(symbol string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[symbol]
}

type forwardActionsFixture struct{ clock *forwardClock }

func (f forwardActionsFixture) FetchCorporateActions(_ context.Context, symbol, start, end string) (domain.CorporateActionSnapshot, error) {
	return domain.CorporateActionSnapshot{Symbol: symbol, Start: start, End: end, Status: "checked", Scope: domain.CorporateActionScope, Source: "前向测试数据", FetchedAt: f.clock.Now(), Events: []domain.CorporateAction{}}, nil
}

func forwardServerFixture(t *testing.T) (*Server, *forwardHistoryFixture, *forwardClock, []string, int) {
	t.Helper()
	s := patternValidationServerFixture(t.TempDir())
	base := s.history.(patternHistoryFixture)["sh600004"]
	stock := make([]domain.DailyBar, 80)
	for i := range stock {
		stock[i] = domain.DailyBar{Open: 100, Close: 100, High: 101, Low: 99, Volume: 1000, Source: "前向测试预热"}
	}
	stock = append(stock, base...)
	dates := []string{}
	date := time.Date(2026, 1, 2, 0, 0, 0, 0, realtimeWebLocation)
	benchmark := make([]domain.DailyBar, len(stock))
	for i := range stock {
		for date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
			date = date.AddDate(0, 0, 1)
		}
		d := date.Format(time.DateOnly)
		dates = append(dates, d)
		stock[i].Date, stock[i].Symbol = d, "sh600100"
		price := 3000 + float64(i)
		benchmark[i] = domain.DailyBar{Symbol: "sh000300", Date: d, Open: price, Close: price, High: price + 1, Low: price - 1, Volume: 100000, Source: "前向测试基准"}
		date = date.AddDate(0, 0, 1)
	}
	clock := &forwardClock{now: date.Add(16 * time.Hour)}
	actions := forwardActionsFixture{clock}
	a, _ := actions.FetchCorporateActions(context.Background(), "sh600100", dates[0], dates[len(dates)-1])
	report, err := strategy.BuildPatternValidation(context.Background(), domain.PatternValidationRequest{Symbols: []string{"sh600100"}, Start: dates[80], End: dates[len(dates)-1]}, []domain.PatternValidationInput{{Symbol: "sh600100", Bars: stock, Actions: &a}, {Symbol: "sh000300", Bars: benchmark}}, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	observed := ""
	for _, sample := range report.Samples {
		if sample.PatternID == "head-shoulders-bottom" {
			observed = sample.ObservedOn
			break
		}
	}
	if observed == "" {
		t.Fatal("forward fixture has no pattern")
	}
	index := 0
	for dates[index] != observed {
		index++
	}
	day, _ := time.ParseInLocation(time.DateOnly, observed, realtimeWebLocation)
	clock.Set(day.Add(14 * time.Hour))
	f := &forwardHistoryFixture{stock: stock, benchmark: benchmark, clock: clock, calls: map[string]int{}}
	s.resolver = market.NewResolver(nil)
	s.history = nil
	s.patternHistory = f
	s.patternActions = actions
	s.now = clock.Now
	s.patternBatches = storage.NewPatternBatchStore(t.TempDir())
	s.patternForwards = storage.NewPatternForwardStore(t.TempDir())
	s.tradingCalendarProvider = func(context.Context, time.Time) ([]string, error) { return dates, nil }
	return s, f, clock, dates, index
}

func createForwardFixture(t *testing.T, s *Server) domain.PatternForwardTask {
	t.Helper()
	pool := batchPoolFixture(t, s, 2)
	r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-forwards", fmt.Sprintf(`{"action":"create","name":"forward","pool_version":%q,"price_basis":"raw"}`, pool.Version))
	var view patternForwardView
	if r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &view) != nil {
		t.Fatalf("create: %d %s", r.Code, r.Body.String())
	}
	return view.Task
}

func TestForwardRetryRetainsFirstSuccessfulSnapshotAndRecoversIndex(t *testing.T) {
	s, f, clock, _, _ := forwardServerFixture(t)
	task := createForwardFixture(t, s)
	day := task.Config.StartOn
	clock.Set(clock.Now().Add(2 * time.Hour))
	f.stale = "sh600101"
	s.runPatternForwardCycle(context.Background())
	first, err := s.patternForwards.Day(task.ID, day)
	if err != nil || first.State != "open" || first.Items[0].State != "captured" || len(first.Items[0].SampleIDs) == 0 {
		t.Fatalf("first capture: %+v %v", first, err)
	}
	snapshot, _ := s.patternForwards.Snapshot(task.ID, day, "sh600100")
	originalHash := snapshot.Hash
	benchmark, _ := s.patternForwards.Benchmark(task.ID, day)
	first.Items[0].State = "pending"
	first.Items[0].SnapshotHash = ""
	first.Items[0].SampleIDs = nil
	if err = s.patternForwards.SaveDay(first); err != nil {
		t.Fatal(err)
	}
	f.stale = ""
	clock.Set(clock.Now().Add(11 * time.Minute))
	s.runPatternForwardCycle(context.Background())
	final, err := s.patternForwards.Day(task.ID, day)
	if err != nil || final.State != "captured" || final.SealedAt.IsZero() || final.Items[0].SnapshotHash != originalHash || f.count("sh600100") != 1 || f.count("sh600101") != 2 || f.count("sh000300") != 1 {
		t.Fatalf("retry altered immutable evidence: %+v %v", final, err)
	}
	latest, _ := s.patternForwards.Benchmark(task.ID, day)
	if latest.Hash != benchmark.Hash {
		t.Fatal("same-day retry replaced benchmark")
	}
	clock.Set(clock.Now().Add(11 * time.Minute))
	s.runPatternForwardCycle(context.Background())
	if f.count("sh600100") != 1 {
		t.Fatal("sealed day collected twice")
	}
	r := validationHTTPRequest(s, http.MethodGet, "/api/pattern-forwards?id="+task.ID+"&date="+day+"&download=1", "")
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var exported struct {
		Stocks []domain.PatternForwardSnapshot `json:"stocks"`
	}
	if json.Unmarshal(r.Body.Bytes(), &exported) != nil || len(exported.Stocks) != 2 || len(exported.Stocks[0].Input.Bars) == 0 {
		t.Fatal("export omitted input evidence")
	}
}

func TestForwardSkippedDatesBecomeMissedWithoutBackfill(t *testing.T) {
	s, f, clock, dates, index := forwardServerFixture(t)
	task := createForwardFixture(t, s)
	next, _ := time.ParseInLocation(time.DateOnly, dates[index+3], realtimeWebLocation)
	clock.Set(next.Add(16 * time.Hour))
	s.runPatternForwardCycle(context.Background())
	for _, date := range dates[index : index+3] {
		day, err := s.patternForwards.Day(task.ID, date)
		if err != nil || day.State != "missed" || day.Items[0].SnapshotHash != "" {
			t.Fatalf("missed day backfilled: %+v %v", day, err)
		}
	}
	for _, end := range f.ends {
		if end != dates[index+3] {
			t.Fatal("collector requested a historical observation date")
		}
	}
	today, _ := s.patternForwards.Day(task.ID, dates[index+3])
	if today.State != "captured" {
		t.Fatal("current day was not collected")
	}
	for _, item := range today.Items {
		snapshot, _ := s.patternForwards.Snapshot(task.ID, today.Date, item.Symbol)
		for _, sample := range snapshot.Samples {
			if sample.ObservedOn != today.Date {
				t.Fatal("old signals included in current observation")
			}
		}
	}
}

func TestForwardRejectsLateStartBackdatingAndUnknownCalendar(t *testing.T) {
	s, f, clock, _, _ := forwardServerFixture(t)
	clock.Set(clock.Now().Add(2 * time.Hour))
	task := createForwardFixture(t, s)
	if task.Config.StartOn <= clock.Now().Format(time.DateOnly) {
		t.Fatal("late task enabled same-day capture")
	}
	r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-forwards", fmt.Sprintf(`{"action":"collect","id":%q}`, task.ID))
	if r.Code != 409 {
		t.Fatal("before-start collection accepted")
	}
	r = validationHTTPRequest(s, http.MethodPost, "/api/pattern-forwards", fmt.Sprintf(`{"action":"collect","id":%q,"date":"2020-01-01"}`, task.ID))
	if r.Code != 400 {
		t.Fatal("backdated request accepted")
	}
	clock.Set(clock.Now().AddDate(0, 0, 1))
	s.tradingCalendarProvider = func(context.Context, time.Time) ([]string, error) { return nil, nil }
	s.runPatternForwardCycle(context.Background())
	days, _ := s.patternForwards.Days(task.ID)
	if len(days) != 0 || f.count("sh600100") != 0 {
		t.Fatal("unknown calendar guessed as trading day")
	}
}

func TestForwardMidnightAndPauseDoNotPublishLateSnapshots(t *testing.T) {
	s, f, clock, _, _ := forwardServerFixture(t)
	task := createForwardFixture(t, s)
	clock.Set(clock.Now().Add(2 * time.Hour))
	f.crossMidnight = true
	s.runPatternForwardCycle(context.Background())
	day, err := s.patternForwards.Day(task.ID, task.Config.StartOn)
	if err != nil || day.State != "missed" {
		t.Fatalf("late collection not sealed as missed: %+v %v", day, err)
	}
	if _, err = s.patternForwards.Snapshot(task.ID, task.Config.StartOn, "sh600100"); !os.IsNotExist(err) {
		t.Fatal("cross-midnight data published as prior-day evidence")
	}
}

func TestForwardPauseCancelsInFlightAcquisition(t *testing.T) {
	s, f, clock, _, _ := forwardServerFixture(t)
	task := createForwardFixture(t, s)
	clock.Set(clock.Now().Add(2 * time.Hour))
	f.block = "sh600100"
	done := make(chan struct{})
	go func() { defer close(done); s.runPatternForwardCycle(context.Background()) }()
	deadline := time.Now().Add(5 * time.Second)
	for f.count("sh600100") == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-forwards", fmt.Sprintf(`{"action":"pause","id":%q}`, task.ID))
	if r.Code != 202 {
		t.Fatal(r.Body.String())
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pause did not stop acquisition")
	}
	if _, err := s.patternForwards.Snapshot(task.ID, task.Config.StartOn, "sh600100"); !os.IsNotExist(err) {
		t.Fatal("cancelled acquisition published a snapshot")
	}
	loaded, _ := s.patternForwards.Load(task.ID)
	if loaded.ConfigHash != task.ConfigHash {
		t.Fatal("pause changed frozen configuration")
	}
}

func TestForwardFreshBenchmarkUpdatesCalendarCoverage(t *testing.T) {
	s, _, clock, dates, index := forwardServerFixture(t)
	task := createForwardFixture(t, s)
	clock.Set(clock.Now().Add(2 * time.Hour))
	s.tradingCalendarProvider = nil
	s.shadowCalendarDates = dates[:index]
	s.shadowCalendarFetchedAt = clock.Now()
	s.runPatternForwardCycle(context.Background())
	status, err := s.patternForwards.Status(task.ID)
	if err != nil || status.CalendarEnd != task.Config.StartOn || status.CalendarSource != "沪深300当日日K确认" {
		t.Fatalf("stale calendar coverage: %+v %v", status, err)
	}
}

func TestPatternForwardBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_PATTERN_FORWARD_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	s, history, clock, dates, index := forwardServerFixture(t)
	var calendarFailed atomic.Bool
	s.tradingCalendarProvider = func(context.Context, time.Time) ([]string, error) {
		if calendarFailed.Load() {
			return nil, fmt.Errorf("测试日历暂不可用")
		}
		return dates, nil
	}
	batchPoolFixture(t, s, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.runPatternForwardLoop(ctx) }()
	defer func() { cancel(); <-done }()
	mux := http.NewServeMux()
	mux.HandleFunc("/__test/forward-clock", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		day, _ := time.ParseInLocation(time.DateOnly, dates[index], realtimeWebLocation)
		switch r.URL.Query().Get("step") {
		case "calendar-fail", "calendar-recover":
			calendarFailed.Store(r.URL.Query().Get("step") == "calendar-fail")
			s.wakePatternForwards()
		case "partial":
			history.mu.Lock()
			history.stale = "sh600101"
			history.mu.Unlock()
			clock.Set(day.Add(16 * time.Hour))
			s.wakePatternForwards()
		case "retry":
			history.mu.Lock()
			history.stale = ""
			history.mu.Unlock()
			clock.Set(clock.Now().Add(11 * time.Minute))
			s.wakePatternForwards()
		case "close":
			clock.Set(day.Add(16 * time.Hour))
		case "later":
			day, _ = time.ParseInLocation(time.DateOnly, dates[index+3], realtimeWebLocation)
			clock.Set(day.Add(16 * time.Hour))
			s.wakePatternForwards()
		case "mature":
			for _, date := range dates[index+1:] {
				day, _ = time.ParseInLocation(time.DateOnly, date, realtimeWebLocation)
				clock.Set(day.Add(16 * time.Hour))
				s.runPatternForwardCycle(ctx)
			}
		default:
			w.WriteHeader(400)
			return
		}
		writeJSON(w, 200, map[string]string{"date": clock.Now().Format(time.DateOnly)})
	})
	mux.Handle("/", s.Handler())
	if err := http.ListenAndServe(address, mux); err != nil {
		t.Fatal(err)
	}
}
