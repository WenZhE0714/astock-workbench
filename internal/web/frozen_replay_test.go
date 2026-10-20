package web

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
)

type replayTestBars map[string][]domain.DailyBar

func (bars replayTestBars) FetchDailyBarsRange(_ context.Context, symbol string, start, end time.Time, _ backtest.PriceAdjustment) ([]domain.DailyBar, error) {
	var result []domain.DailyBar
	for _, bar := range bars[symbol] {
		if bar.Date >= start.Format(time.DateOnly) && bar.Date <= end.Format(time.DateOnly) {
			result = append(result, bar)
		}
	}
	return result, nil
}

func TestFrozenReplayUsesOnlySelectedSeal(t *testing.T) {
	store, plan, root := volumeStudyViewFixture(t)
	firstDate := plan.Baseline.Start
	bars := replayTestBars{}
	for _, symbol := range append([]string{plan.Baseline.Benchmark}, plan.Baseline.Tickers...) {
		for date := firstDate.AddDate(0, 0, -175); !date.After(firstDate.AddDate(0, 0, 1)); date = date.AddDate(0, 0, 1) {
			if date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
				continue
			}
			price := 10.0
			if date.After(firstDate) {
				price = 9999
			}
			bars[symbol] = append(bars[symbol], domain.DailyBar{Symbol: symbol, Source: "fixture", Date: date.Format(time.DateOnly), Open: price, High: price + 1, Low: price - 1, Close: price, Volume: 100})
		}
	}
	first, err := backtest.BuildVolumeForwardDay(t.Context(), plan, nil, bars, firstDate.Add(16*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(plan, first); err != nil {
		t.Fatal(err)
	}
	second, err := backtest.BuildVolumeForwardDay(t.Context(), plan, &first, bars, firstDate.AddDate(0, 0, 1).Add(16*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(plan, second); err != nil {
		t.Fatal(err)
	}
	s := NewServer(nil, nil, nil, nil, "", WithFrozenReplay("history", "历史演练", "historical", root))
	before, _ := os.ReadFile(filepath.Join(root, "config.json"))
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		s.Handler().ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		return r
	}
	catalog := get("/api/replay/catalog")
	if catalog.Code != 200 || strings.Contains(catalog.Body.String(), root) || !strings.Contains(catalog.Body.String(), second.Date) {
		t.Fatal("invalid replay catalog", catalog.Body.String())
	}
	path := "/api/replay/day?source=history&symbol=" + plan.Baseline.Tickers[0] + "&date=" + first.Date + "&fingerprint=" + plan.Fingerprint
	r := get(path)
	var response replayDayView
	if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil || r.Code != 200 {
		t.Fatalf("%d: %s: %v", r.Code, r.Body.String(), err)
	}
	if response.Date != first.Date || response.Bars[len(response.Bars)-1].Time != first.Date || response.Bars[len(response.Bars)-1].Close != 10 || response.Analysis.DataDate != first.Date || len(response.Runs[0].Equity) != 1 {
		t.Fatal("later data leaked into replay")
	}
	for _, bar := range response.Weekly {
		if bar.Time > first.Date {
			t.Fatal("future week leaked")
		}
	}
	for _, tc := range []struct {
		query  string
		status int
	}{{strings.Replace(path, plan.Fingerprint, "wrong", 1), 409}, {strings.Replace(path, first.Date, "../config", 1), 400}, {strings.Replace(path, "source=history", "source=unknown", 1), 404}} {
		if got := get(tc.query); got.Code != tc.status {
			t.Fatalf("got %d want %d", got.Code, tc.status)
		}
	}
	post := httptest.NewRecorder()
	s.Handler().ServeHTTP(post, httptest.NewRequest("POST", path, nil))
	if post.Code != 405 {
		t.Fatal("replay permits writes")
	}
	after, _ := os.ReadFile(filepath.Join(root, "config.json"))
	if string(before) != string(after) {
		t.Fatal("read changed config")
	}
	if err := os.WriteFile(filepath.Join(root, "days", first.Date+".json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if get(path).Code != 409 {
		t.Fatal("corrupt seal was accepted")
	}
}

func TestReplayRejectsFutureEconomicFields(t *testing.T) {
	base := func() backtest.VolumeForwardDay {
		return backtest.VolumeForwardDay{Date: "2026-10-20", Runs: []backtest.Result{{Request: backtest.Request{End: time.Date(2026, 10, 20, 0, 0, 0, 0, strategyLocation)}}}, Bars: map[string][]domain.DailyBar{}}
	}
	for _, mutate := range []func(*backtest.VolumeForwardDay){
		func(d *backtest.VolumeForwardDay) { d.Bars["x"] = []domain.DailyBar{{Date: "2026-10-21"}} },
		func(d *backtest.VolumeForwardDay) {
			d.Runs[0].Trades = []backtest.Trade{{Exit: backtest.Fill{Date: "2026-10-21"}}}
		},
		func(d *backtest.VolumeForwardDay) { d.Runs[0].Equity = []backtest.EquityPoint{{Date: "2026-10-21"}} },
		func(d *backtest.VolumeForwardDay) {
			d.Runs[0].PendingOrders = []backtest.ForwardPendingOrder{{Signal: backtest.SignalSnapshot{Date: "2026-10-21"}}}
		},
	} {
		d := base()
		mutate(&d)
		if validateReplayCutoff(d) == nil {
			t.Fatal("future record accepted")
		}
	}
}
