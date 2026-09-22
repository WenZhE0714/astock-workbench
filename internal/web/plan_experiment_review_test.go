package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func experimentReviewFixture(t *testing.T) (paper.PlanExperiment, []domain.TradePlan) {
	t.Helper()
	at := time.Date(2026, 9, 21, 9, 29, 0, 0, realtimeWebLocation)
	state, err := paper.ConfigurePlanExperiment(paper.PlanExperiment{}, true, at)
	if err != nil {
		t.Fatal(err)
	}
	symbols := []string{"sh600519", "sz000001", "sh601318", "sz002594"}
	plans := make([]domain.TradePlan, 0, len(symbols))
	for index, symbol := range symbols {
		plan := domain.TradePlan{ID: strings.Repeat(string(rune('a'+index)), 64), Version: 1, Symbol: symbol, CreatedAt: at.AddDate(0, 0, -3), ExpiresOn: "2026-09-28",
			Analysis:  domain.ChartAnalysis{Version: "chart-v1", Symbol: symbol, Fingerprint: strings.Repeat("e", 64), DataDate: "2026-09-18"},
			Structure: domain.ChartStructure{ID: "range-breakout", Name: "区间突破", Plan: &domain.ChartPlanLevels{EntryLow: 100, EntryHigh: 102, Invalidation: 95, Target1: 109, Target2: 116, Confirmation: "完整日K确认"}},
		}
		state, err = paper.SelectPlanExperiment(state, plan, true, at)
		if err != nil {
			t.Fatal(err)
		}
		plans = append(plans, plan)
	}
	bars := make([]domain.DailyBar, 0, 20)
	date := at.AddDate(0, 0, -28)
	for len(bars) < 20 {
		if date.Weekday() != time.Saturday && date.Weekday() != time.Sunday {
			bars = append(bars, domain.DailyBar{Date: date.Format(time.DateOnly), Open: 100, High: 101, Low: 99, Close: 100, Volume: 100000, Amount: 1e8})
		}
		date = date.AddDate(0, 0, 1)
	}
	step := func(when time.Time, prices []float64) {
		t.Helper()
		inputs := make([]paper.PlanExperimentInput, 0, len(prices))
		for index, price := range prices {
			plan := plans[index]
			phase := "confirmed"
			if price >= 100 && price <= 102 {
				phase = "in_zone"
			}
			history := append([]domain.DailyBar(nil), bars...)
			if index == 3 {
				history[0].Amount = 0
			}
			inputs = append(inputs, paper.PlanExperimentInput{Symbol: plan.Symbol, Qualified: true,
				Quote: paper.PositionQuote{Symbol: plan.Symbol, Price: price, PreviousClose: 100, LimitUp: 110, LimitDown: 90, Volume: 10000, Amount: 1e7, QuoteTime: when.Format("2006-01-02 15:04:05"), Source: "隔离测试行情"},
				Bars:  history, CalendarDates: []string{"2026-09-18", "2026-09-21", "2026-09-22"},
				Monitors: map[string]domain.PlanMonitor{plan.ID: {Version: 1, PlanID: plan.ID, Symbol: plan.Symbol, Enabled: true, Fingerprint: plan.Analysis.Fingerprint, Phase: phase, DataStatus: "healthy", ConfirmedOn: "2026-09-18", HistoryDate: "2026-09-18", LastQuoteAt: when}},
			})
		}
		state, err = paper.AdvancePlanExperiment(state, inputs, when.Add(time.Second), "trading")
		if err != nil {
			t.Fatal(err)
		}
	}
	at = time.Date(2026, 9, 21, 10, 0, 0, 0, realtimeWebLocation)
	for _, price := range []float64{103, 103.1, 101, 101.1} {
		step(at, []float64{price})
		at = at.Add(30 * time.Second)
	}
	step(at, []float64{94})
	at = time.Date(2026, 9, 22, 10, 0, 0, 0, realtimeWebLocation)
	step(at, []float64{96, 103, 99, 101})
	step(at.Add(30*time.Second), []float64{96, 103.1, 99, 101})
	state, err = paper.SelectPlanExperiment(state, plans[0], false, at.Add(31*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	state, err = paper.ConfigurePlanExperiment(state, false, at.Add(32*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if paper.BuildPlanExperimentReview(state).PairedCompleted != 1 {
		t.Fatal("fixture did not produce a completed pair")
	}
	return state, plans
}

func storeReviewFixture(t *testing.T, root string, state paper.PlanExperiment) *storage.PlanExperimentStore {
	t.Helper()
	store := storage.NewPlanExperimentStore(filepath.Join(root, "plan-experiment.json"))
	if _, err := store.Update(func(paper.PlanExperiment) (paper.PlanExperiment, error) { return state, nil }); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestPlanExperimentReviewEndpointPersistsAndDoesNotTruncateHistory(t *testing.T) {
	state, _ := experimentReviewFixture(t)
	for index := range state.Arms {
		for i := 0; i < 140; i++ {
			state.Arms[index].Report.Rejections = append(state.Arms[index].Report.Rejections, paper.ShadowRejection{OrderID: fmt.Sprintf("%s-rejected-%d", state.Arms[index].ID, i), PlanID: strings.Repeat("d", 64), Symbol: "sz002594", Side: "buy", Reason: "容量不足"})
		}
	}
	root := t.TempDir()
	store := storeReviewFixture(t, root, state)
	file := filepath.Join(root, "plan-experiment.json")
	before, _ := os.ReadFile(file)
	server := NewServer(nil, nil, nil, nil, "", WithPlanExperiment(store))
	get := func(url string) (*httptest.ResponseRecorder, paper.PlanExperimentReview) {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, url, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("review failed: %d %s", recorder.Code, recorder.Body.String())
		}
		var response struct {
			Review paper.PlanExperimentReview `json:"review"`
			State  *paper.PlanExperiment      `json:"state"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if url == "/api/plan-experiment" && (response.State == nil || len(response.State.Arms[0].Report.Rejections) != 100) {
			t.Fatal("regular account response no longer trims long history")
		}
		return recorder, response.Review
	}
	_, first := get("/api/plan-experiment")
	if first.PairedCompleted != 1 || first.Arms[0].Rejections != 141 || first.SingleSided < 1 {
		t.Fatalf("complete history was truncated before review: %+v", first)
	}
	server = NewServer(nil, nil, nil, nil, "", WithPlanExperiment(storage.NewPlanExperimentStore(file)))
	recorder, restored := get("/api/plan-experiment?view=review&download=1")
	firstJSON, _ := json.Marshal(first)
	restoredJSON, _ := json.Marshal(restored)
	if !bytes.Equal(firstJSON, restoredJSON) || !strings.Contains(recorder.Header().Get("Content-Disposition"), "plan-experiment-review.json") {
		t.Fatal("restart/export changed the review")
	}
	after, _ := os.ReadFile(file)
	if !bytes.Equal(before, after) {
		t.Fatal("GET rewrote experiment ledger")
	}
}

func TestPlanExperimentReviewEmptyGETDoesNotInitializeAccount(t *testing.T) {
	file := filepath.Join(t.TempDir(), "experiment.json")
	server := NewServer(nil, nil, nil, nil, "", WithPlanExperiment(storage.NewPlanExperimentStore(file)))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/plan-experiment?view=review", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"initialized":false`) {
		t.Fatalf("empty review: %d %s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("read initialized an account")
	}
}

func TestPlanExperimentReviewBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_EXPERIMENT_REVIEW_TEST_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	state, plans := experimentReviewFixture(t)
	root := t.TempDir()
	server := chartServerFixture(filepath.Join(root, "plans"))
	for _, plan := range plans {
		if _, _, err := server.tradePlans.Save(plan); err != nil {
			t.Fatal(err)
		}
	}
	server.planReviews = storage.NewTradePlanReviewStore(filepath.Join(root, "reviews"))
	server.planExperiment = storeReviewFixture(t, root, state)
	server.nameCacheFile = filepath.Join(root, "names.tsv")
	names, err := storage.LoadNameCache(server.nameCacheFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := names.Remember([]domain.Candidate{{Symbol: "sh600519", Name: "贵州茅台"}, {Symbol: "sz000001", Name: "平安银行"}, {Symbol: "sh601318", Name: "中国平安"}, {Symbol: "sz002594", Name: "比亚迪"}}); err != nil {
		t.Fatal(err)
	}
	if err := http.ListenAndServe(address, server.Handler()); err != nil {
		t.Fatal(err)
	}
}
