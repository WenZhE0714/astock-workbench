package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func volumeStudyViewFixture(t *testing.T) (*storage.VolumeForwardStore, backtest.VolumeForwardPlan, string) {
	t.Helper()
	p := backtest.DefaultTechnicalParameters()
	p.EntryMode = backtest.EntryModeReclaim
	p.MaxPosition = .1
	start := time.Date(2022, 7, 16, 0, 0, 0, 0, strategyLocation)
	base := backtest.Request{Strategy: "technical-breakout", StrategyVersion: "v1", Tickers: []string{"sh600000", "sz000001"}, Names: map[string]string{"sh600000": "浦发银行", "sz000001": "平安银行"}, Start: start, End: start.AddDate(1, 0, -1), InitialCash: 1000000, Benchmark: "sh000300", NoFutureData: true, Adjustment: backtest.AdjustmentNone, Technical: p, LiquidateAtEnd: true}
	candidate := base
	candidate.EntryVolumeFloor = 1.5
	source := backtest.VolumeStudyResult{Completed: true, SourceID: "AUTO-source", CandidateID: "P008", MaxPosition: .1, BaselineReproduced: true}
	for index, request := range []backtest.Request{base, candidate} {
		variant := backtest.VolumeStudyVariant{Minimum: request.EntryVolumeFloor, PositionStudyVariant: backtest.PositionStudyVariant{MaxPosition: .1, Windows: []backtest.PositionStudyWindow{{ID: "F01", Normal: backtest.Result{Request: request}}, {ID: "F02", Normal: backtest.Result{Request: request}}}, Normal: backtest.PositionStudySummary{Passed: true, AverageReturn: 6.52, WorstDrawdown: 4.75, PositiveRatio: 1, Trades: 76}, DoubleCost: backtest.PositionStudySummary{Passed: index == 1, AverageReturn: 6.1, WorstDrawdown: 4.89, PositiveRatio: 1, Trades: 76}}}
		source.Variants = append(source.Variants, variant)
	}
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	plan, err := backtest.NewVolumeForwardPlan(source, hex.EncodeToString(sum[:]), strings.Repeat("b", 64), time.Date(2026, 10, 19, 19, 45, 0, 0, strategyLocation))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	store := storage.NewVolumeForwardStore(root)
	if err := store.Initialize(plan, data); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveStatus(storage.VolumeForwardStatus{CheckedAt: plan.CreatedAt, Status: "waiting", Detail: "等待冻结起点之后的新交易日"}); err != nil {
		t.Fatal(err)
	}
	return store, plan, root
}

func TestVolumeForwardStudyReadOnlyVerifiedAndNoFabricatedReturns(t *testing.T) {
	store, plan, root := volumeStudyViewFixture(t)
	s := NewServer(nil, nil, nil, nil, "", WithVolumeForwardStudy(store))
	s.now = func() time.Time { return plan.CreatedAt.Add(time.Minute) }
	page := httptest.NewRecorder()
	s.Handler().ServeHTTP(page, httptest.NewRequest("GET", "/?view=strategy", nil))
	if page.Code != 200 || !strings.Contains(page.Body.String(), "volume-forward-study-view") {
		t.Fatalf("research page unavailable: %d %s", page.Code, page.Body.String())
	}
	before, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	s.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/api/strategy/volume-forward?download=1", nil))
	var response volumeForwardStudyResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || recorder.Code != 200 {
		t.Fatalf("read: %d %s %v", recorder.Code, recorder.Body.String(), err)
	}
	if !response.Verified || !response.ReadOnly || !response.Configured || response.Plan.Fingerprint != plan.Fingerprint || response.SealedDays != 0 || response.Plan.PromotionAllowed || len(response.Groups) != 4 || len(response.Evidence) != 2 {
		t.Fatalf("invalid view: %+v", response)
	}
	for _, group := range response.Groups {
		if group.Return != nil || group.Drawdown != nil || group.Cash != 1000000 {
			t.Fatalf("fabricated observation: %+v", group)
		}
	}
	if !response.Evidence[1].Normal.Passed || !response.Evidence[1].DoubleCost.Passed || response.StatusStale {
		t.Fatal("verified evidence or runtime status lost")
	}
	if !strings.Contains(recorder.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("export header missing")
	}
	post := httptest.NewRecorder()
	s.Handler().ServeHTTP(post, httptest.NewRequest("POST", "/api/strategy/volume-forward", strings.NewReader(`{}`)))
	if post.Code != 405 {
		t.Fatal("view permits writes")
	}
	after, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil || string(before) != string(after) {
		t.Fatal("read changed frozen plan")
	}
	if _, err := os.Stat(filepath.Join(root, "days")); !os.IsNotExist(err) {
		t.Fatal("read created observation days")
	}
}

func TestVolumeForwardStudyReadsDuringCollectionAndRejectsCorruptArchive(t *testing.T) {
	store, _, root := volumeStudyViewFixture(t)
	s := NewServer(nil, nil, nil, nil, "", WithVolumeForwardStudy(store))
	unlock, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	s.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/api/strategy/volume-forward", nil))
	unlock()
	if recorder.Code != 200 {
		t.Fatal("read-only view should not compete for the collector lock")
	}
	if err := os.WriteFile(filepath.Join(root, "source-result.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	s.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/api/strategy/volume-forward", nil))
	if recorder.Code != 500 {
		t.Fatal("corrupt evidence accepted")
	}
	missing := filepath.Join(t.TempDir(), "not-created")
	s = NewServer(nil, nil, nil, nil, "", WithVolumeForwardStudy(storage.NewVolumeForwardStore(missing)))
	recorder = httptest.NewRecorder()
	s.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/api/strategy/volume-forward", nil))
	if recorder.Code != 503 {
		t.Fatal("missing config accepted")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("read created missing study directory")
	}
}

func TestVolumeForwardStudyProjectsSealedProgress(t *testing.T) {
	store, plan, _ := volumeStudyViewFixture(t)
	source, err := store.ReadStudy(plan)
	if err != nil {
		t.Fatal(err)
	}
	day := backtest.VolumeForwardDay{Date: "2026-10-20", Runs: make([]backtest.Result, 4)}
	for index := range day.Runs {
		day.Runs[index] = backtest.Result{Metrics: backtest.Metrics{TotalReturn: float64(index), Trades: index}, Equity: []backtest.EquityPoint{{Date: day.Date, Cash: 900000}}}
	}
	result := summarizeVolumeForwardStudy(plan, &day, 1, source)
	if result.SealedDays != 1 || result.LastDate != day.Date || result.SampleReady || *result.Groups[1].Return != 1 || result.Groups[1].Trades != 1 {
		t.Fatalf("sealed progress lost: %+v", result)
	}
}

func TestVolumeForwardStudyBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_FORWARD_VIEW_TEST_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	store, plan, _ := volumeStudyViewFixture(t)
	archive := storage.NewContinuousOptimizationStore(t.TempDir())
	if _, err := archive.Save(rejectedCandidateFixture()); err != nil {
		t.Fatal(err)
	}
	s := NewServer(nil, nil, nil, nil, "600000", WithVolumeForwardStudy(store), WithStrategyCandidateLifecycle(&candidateObservationEngineStub{}, archive))
	s.now = func() time.Time { return plan.CreatedAt.Add(time.Minute) }
	if err := http.ListenAndServe(address, s.Handler()); err != nil {
		t.Fatal(err)
	}
}
