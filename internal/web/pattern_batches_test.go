package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type batchHistoryFixture struct {
	mu              sync.Mutex
	bars, benchmark []domain.DailyBar
	calls           map[string]int
	fail, block     string
	failOnce        bool
	delay           time.Duration
}

func (f *batchHistoryFixture) FetchDailyBarsRange(ctx context.Context, symbol string, _, _ time.Time, _ backtest.PriceAdjustment) ([]domain.DailyBar, error) {
	f.mu.Lock()
	f.calls[symbol]++
	fail, block := f.fail == symbol, f.block == symbol
	if fail && f.failOnce {
		f.fail = ""
	}
	f.mu.Unlock()
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.delay):
		}
	}
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if fail {
		return nil, errors.New("fixture unavailable")
	}
	bars := f.bars
	if symbol == "sh000300" {
		bars = f.benchmark
	}
	result := append([]domain.DailyBar{}, bars...)
	for i := range result {
		result[i].Symbol = symbol
	}
	return result, nil
}
func (f *batchHistoryFixture) count(symbol string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[symbol]
}
func (f *batchHistoryFixture) unblock() { f.mu.Lock(); f.fail, f.block = "", ""; f.mu.Unlock() }

func batchServerFixture(t *testing.T, root string) (*Server, *batchHistoryFixture) {
	t.Helper()
	s := patternValidationServerFixture(t.TempDir())
	s.resolver = market.NewResolver(nil)
	base := s.history.(patternHistoryFixture)
	f := &batchHistoryFixture{bars: base["sh600004"], benchmark: base["sh000300"], calls: map[string]int{}}
	s.history = nil
	s.patternHistory = f
	s.patternBatches = storage.NewPatternBatchStore(root)
	t.Cleanup(s.StopPatternBatches)
	return s, f
}

func batchPoolFixture(t *testing.T, s *Server, count int) domain.PatternPool {
	t.Helper()
	members := []domain.PatternPoolMember{}
	for i := 0; i < count; i++ {
		members = append(members, domain.PatternPoolMember{Symbol: fmt.Sprintf("sh%06d", 600100+i), Name: fmt.Sprint(i), Industry: fmt.Sprintf("sector-%d", i%3)})
	}
	data, _ := json.Marshal(map[string]any{"name": "pool", "source": "test labels", "members": members})
	r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-pools", string(data))
	var pool domain.PatternPool
	if r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &pool) != nil {
		t.Fatalf("pool: %d %s", r.Code, r.Body.String())
	}
	return pool
}

func createBatch(t *testing.T, s *Server, pool domain.PatternPool) domain.PatternBatchJob {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"action": "create", "pool_version": pool.Version, "request": map[string]string{"start": "2026-06-01", "end": "2026-09-25"}})
	r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-batches", string(data))
	var job domain.PatternBatchJob
	if r.Code != 202 || json.Unmarshal(r.Body.Bytes(), &job) != nil {
		t.Fatalf("create: %d %s", r.Code, r.Body.String())
	}
	return job
}

func awaitBatch(t *testing.T, s *Server, id string) domain.PatternBatchJob {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		job, err := s.patternBatches.LoadJob(id)
		if err != nil {
			t.Fatal(err)
		}
		job = s.batchJobView(job)
		if job.Status != "running" && job.Status != "cancelling" {
			s.StopPatternBatches()
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("batch did not finish")
	return domain.PatternBatchJob{}
}

func TestPatternBatchTwentyStocksAndFrozenDistribution(t *testing.T) {
	s, f := batchServerFixture(t, t.TempDir())
	pool := batchPoolFixture(t, s, 20)
	job := createBatch(t, s, pool)
	job = awaitBatch(t, s, job.ID)
	if job.Status != "completed" || len(job.RunIDs) != 1 || f.count("sh000300") != 1 {
		t.Fatalf("batch failed: %+v", job)
	}
	for _, item := range job.Items {
		if item.State != "ready" || item.Attempts != 1 || item.InputHash == "" {
			t.Fatalf("bad checkpoint: %+v", item)
		}
	}
	report, err := s.patternValidations.Load(job.RunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	view, err := strategy.PatternValidationResult(report, strategy.PatternValidationFilter{})
	if err != nil || len(view.Stocks) != 20 || len(view.Industries) != 3 || report.Pool.Version != pool.Version || len(report.Request.Symbols) != 20 {
		t.Fatal("pool distribution or provenance lost")
	}
	total := 0.0
	for _, stock := range view.Stocks {
		total += stock.SampleShare
	}
	if total != 100 {
		t.Fatalf("sample shares do not total 100: %v", total)
	}
}

func TestPatternBatchRetriesOnlyFailuresWithSameBenchmark(t *testing.T) {
	s, f := batchServerFixture(t, t.TempDir())
	f.fail = "sh600101"
	pool := batchPoolFixture(t, s, 3)
	job := awaitBatch(t, s, createBatch(t, s, pool).ID)
	if job.Status != "partial" || len(job.RunIDs) != 1 {
		t.Fatalf("expected partial: %+v", job)
	}
	oldID, oldHash := job.RunIDs[0], job.Items[0].InputHash
	f.unblock()
	r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-batches", fmt.Sprintf(`{"action":"retry-failed","id":%q}`, job.ID))
	if r.Code != 202 {
		t.Fatal(r.Body.String())
	}
	job = awaitBatch(t, s, job.ID)
	if job.Status != "completed" || len(job.RunIDs) != 2 || job.RunIDs[0] != oldID || job.Items[0].InputHash != oldHash || f.count("sh600100") != 1 || f.count("sh600101") != 2 || f.count("sh600102") != 1 || f.count("sh000300") != 1 {
		t.Fatalf("retry refetched completed evidence: %+v", job)
	}
	old, _ := s.patternValidations.Load(oldID)
	if old.Coverage[1].Error == "" {
		t.Fatal("retry overwrote original failure archive")
	}
}

func TestPatternBatchCancellationAndRestartResume(t *testing.T) {
	root := t.TempDir()
	s, f := batchServerFixture(t, root)
	f.block = "sh600101"
	pool := batchPoolFixture(t, s, 2)
	job := createBatch(t, s, pool)
	deadline := time.Now().Add(5 * time.Second)
	for f.count("sh600101") == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond * 10)
	}
	if f.count("sh600101") == 0 {
		t.Fatal("blocking item not started")
	}
	other, _ := batchServerFixture(t, root)
	if visible, _ := other.patternBatches.LoadJob(job.ID); other.batchJobView(visible).Status != "running" {
		t.Fatal("other active process treated as restart")
	}
	r := validationHTTPRequest(other, http.MethodPost, "/api/pattern-batches", fmt.Sprintf(`{"action":"resume","id":%q}`, job.ID))
	if r.Code != 409 {
		t.Fatal("second worker admitted")
	}
	r = validationHTTPRequest(s, http.MethodPost, "/api/pattern-batches", fmt.Sprintf(`{"action":"cancel","id":%q}`, job.ID))
	if r.Code != 202 {
		t.Fatal(r.Body.String())
	}
	job = awaitBatch(t, s, job.ID)
	if job.Status != "cancelled" || job.Items[0].State != "ready" {
		t.Fatal("cancel lost completed checkpoint")
	}
	job.Status = "running"
	job.Items[1].State = "running"
	if err := s.patternBatches.SaveJob(job); err != nil {
		t.Fatal(err)
	}
	restarted, newHistory := batchServerFixture(t, root)
	if restarted.batchJobView(job).Status != "paused" {
		t.Fatal("interrupted job not recoverable")
	}
	originalVersion := job.AlgorithmVersion
	job.AlgorithmVersion = "previous-version"
	if err := s.patternBatches.SaveJob(job); err != nil {
		t.Fatal(err)
	}
	if rejected := validationHTTPRequest(restarted, http.MethodPost, "/api/pattern-batches", fmt.Sprintf(`{"action":"resume","id":%q}`, job.ID)); rejected.Code != 409 {
		t.Fatal("algorithm change allowed resume")
	}
	job.AlgorithmVersion = originalVersion
	if err := s.patternBatches.SaveJob(job); err != nil {
		t.Fatal(err)
	}
	r = validationHTTPRequest(restarted, http.MethodPost, "/api/pattern-batches", fmt.Sprintf(`{"action":"resume","id":%q}`, job.ID))
	if r.Code != 202 {
		t.Fatal(r.Body.String())
	}
	resumed := awaitBatch(t, restarted, job.ID)
	if resumed.Status != "completed" || resumed.Request.End != job.Request.End || !resumed.CreatedAt.Equal(job.CreatedAt) || newHistory.count("sh600100") != 0 || newHistory.count("sh000300") != 0 || newHistory.count("sh600101") != 1 {
		t.Fatalf("resume changed frozen evidence: %+v", resumed)
	}
}

type failingBatchStore struct{ patternBatchArchive }

func (f failingBatchStore) SaveInput(string, domain.PatternValidationInput) (string, error) {
	return "", errors.New("disk full")
}
func TestPatternBatchStopsOnCheckpointWriteFailure(t *testing.T) {
	s, f := batchServerFixture(t, t.TempDir())
	pool := batchPoolFixture(t, s, 2)
	s.patternBatches = failingBatchStore{s.patternBatches}
	job := awaitBatch(t, s, createBatch(t, s, pool).ID)
	if job.Status != "failed" || job.Error != "disk full" || f.count("sh600100") != 0 {
		t.Fatalf("persistence failure ignored: %+v", job)
	}
}

func TestPatternBatchBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_PATTERN_BATCH_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	s, f := batchServerFixture(t, t.TempDir())
	f.fail = "sz000858"
	f.failOnce = true
	f.delay = 100 * time.Millisecond
	if err := http.ListenAndServe(address, s.Handler()); err != nil {
		t.Fatal(err)
	}
}
