package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type patternBatchArchive interface {
	SavePool(domain.PatternPool) (domain.PatternPool, error)
	LoadPool(string) (domain.PatternPool, error)
	Pools() ([]domain.PatternPool, error)
	SaveJob(domain.PatternBatchJob) error
	LoadJob(string) (domain.PatternBatchJob, error)
	Jobs() ([]domain.PatternBatchJob, error)
	SaveInput(string, domain.PatternValidationInput) (string, error)
	LoadInput(string, string, string) (domain.PatternValidationInput, error)
	Acquire() (func(), error)
}

type patternBatchExecution struct {
	id         string
	cancel     context.CancelFunc
	done       chan struct{}
	cancelling bool
}

func WithPatternBatches(store patternBatchArchive) ServerOption {
	return func(s *Server) { s.patternBatches = store }
}

func decodePatternBatch(w http.ResponseWriter, r *http.Request, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("请求格式无效: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("请求只能包含一个JSON对象")
	}
	return nil
}

func (s *Server) handlePatternPools(w http.ResponseWriter, r *http.Request) {
	if s.patternBatches == nil {
		writeJSON(w, 503, errorResponse{Error: "股票池存储未初始化"})
		return
	}
	if r.Method == http.MethodGet {
		pools, err := s.patternBatches.Pools()
		if err != nil {
			writeJSON(w, 500, errorResponse{Error: err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"items": pools})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, errorResponse{Error: "只支持GET、POST"})
		return
	}
	var input struct {
		Name    string                     `json:"name"`
		Source  string                     `json:"source"`
		Members []domain.PatternPoolMember `json:"members"`
	}
	if err := decodePatternBatch(w, r, &input); err != nil {
		writeJSON(w, 400, errorResponse{Error: err.Error()})
		return
	}
	if s.resolver == nil || len(input.Members) < 1 || len(input.Members) > domain.PatternPoolLimit {
		writeJSON(w, 400, errorResponse{Error: "股票池需要1至50只A股"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	for i := range input.Members {
		m := &input.Members[i]
		symbol, err := s.resolver.Resolve(ctx, strings.TrimSpace(m.Symbol))
		if err != nil || market.AssetKindOf(symbol) != domain.AssetKindStock || strategyIndexSymbol(symbol) {
			writeJSON(w, 400, errorResponse{Error: m.Symbol + " 不是有效A股代码"})
			return
		}
		m.Symbol = symbol
	}
	pool, err := s.patternBatches.SavePool(domain.PatternPool{Name: input.Name, Source: input.Source, Members: input.Members})
	if err != nil {
		writeJSON(w, 400, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, 201, pool)
}

func patternBatchDigest(job domain.PatternBatchJob) string {
	data, _ := json.Marshal(struct {
		Pool, Algorithm string
		Request         domain.PatternValidationRequest
	}{job.Pool.Version, job.AlgorithmVersion, job.Request})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (s *Server) batchJobView(job domain.PatternBatchJob) domain.PatternBatchJob {
	s.patternBatchMu.Lock()
	defer s.patternBatchMu.Unlock()
	if latest, err := s.patternBatches.LoadJob(job.ID); err == nil {
		job = latest
	}
	if message := s.patternBatchErrors[job.ID]; message != "" {
		job.Status, job.Error = "failed", message
		return job
	}
	if s.patternBatchActive != nil && s.patternBatchActive.id == job.ID {
		if s.patternBatchActive.cancelling {
			job.Status = "cancelling"
		}
		return job
	}
	if job.Status == "running" {
		if unlock, err := s.patternBatches.Acquire(); err == nil {
			// A worker may have completed between the initial read and lease acquisition.
			if latest, err := s.patternBatches.LoadJob(job.ID); err == nil {
				job = latest
			}
			unlock()
			if job.Status == "running" {
				job.Status = "paused"
				job.Error = "服务中断，已保存的检查点可继续使用"
			}
		}
	}
	return job
}

func (s *Server) handlePatternBatches(w http.ResponseWriter, r *http.Request) {
	if s.patternBatches == nil || s.patternValidations == nil {
		writeJSON(w, 503, errorResponse{Error: "批量研究存储未初始化"})
		return
	}
	if r.Method == http.MethodGet {
		if id := r.URL.Query().Get("id"); id != "" {
			job, err := s.patternBatches.LoadJob(id)
			if err != nil {
				writeJSON(w, 404, errorResponse{Error: err.Error()})
				return
			}
			writeJSON(w, 200, s.batchJobView(job))
			return
		}
		jobs, err := s.patternBatches.Jobs()
		if err != nil {
			writeJSON(w, 500, errorResponse{Error: err.Error()})
			return
		}
		for i := range jobs {
			jobs[i] = s.batchJobView(jobs[i])
		}
		writeJSON(w, 200, map[string]any{"items": jobs})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, errorResponse{Error: "只支持GET、POST"})
		return
	}
	var input struct {
		ID          string                          `json:"id"`
		Action      string                          `json:"action"`
		PoolVersion string                          `json:"pool_version"`
		Request     domain.PatternValidationRequest `json:"request"`
	}
	if err := decodePatternBatch(w, r, &input); err != nil {
		writeJSON(w, 400, errorResponse{Error: err.Error()})
		return
	}
	if input.Action == "cancel" {
		s.patternBatchMu.Lock()
		active := s.patternBatchActive
		if active == nil || active.id != input.ID {
			s.patternBatchMu.Unlock()
			writeJSON(w, 409, errorResponse{Error: "此任务未在当前服务运行"})
			return
		}
		if job, err := s.patternBatches.LoadJob(input.ID); err != nil || job.Status != "running" {
			s.patternBatchMu.Unlock()
			writeJSON(w, 409, errorResponse{Error: "任务已结束或状态不可用"})
			return
		}
		active.cancelling = true
		active.cancel()
		s.patternBatchMu.Unlock()
		writeJSON(w, 202, map[string]string{"status": "cancelling"})
		return
	}
	var job domain.PatternBatchJob
	var err error
	fresh := input.Action == "create"
	if fresh {
		if s.resolver == nil || (s.history == nil && s.patternHistory == nil) {
			writeJSON(w, 503, errorResponse{Error: "日K数据服务未初始化"})
			return
		}
		job.Pool, err = s.patternBatches.LoadPool(input.PoolVersion)
		if err != nil {
			writeJSON(w, 400, errorResponse{Error: err.Error()})
			return
		}
		if len(input.Request.Symbols) > 0 {
			writeJSON(w, 400, errorResponse{Error: "批量股票名单由冻结版本确定"})
			return
		}
		job.Request = input.Request
		for _, m := range job.Pool.Members {
			job.Request.Symbols = append(job.Request.Symbols, m.Symbol)
			job.Items = append(job.Items, domain.PatternBatchItem{Symbol: m.Symbol, State: "pending"})
		}
		job.CreatedAt = s.currentTime()
		cutoff, err := strategy.ValidatePatternValidationRequest(job.Request, job.CreatedAt)
		if err != nil {
			writeJSON(w, 400, errorResponse{Error: err.Error()})
			return
		}
		job.Request.End = cutoff
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			writeJSON(w, 500, errorResponse{Error: err.Error()})
			return
		}
		job.ID, job.AlgorithmVersion, job.RunIDs = hex.EncodeToString(id[:]), strategy.PatternValidationVersion, []string{}
		job.ConfigHash = patternBatchDigest(job)
	} else if input.Action == "resume" || input.Action == "retry-failed" {
		job, err = s.patternBatches.LoadJob(input.ID)
		if err != nil {
			writeJSON(w, 404, errorResponse{Error: err.Error()})
			return
		}
	} else {
		writeJSON(w, 400, errorResponse{Error: "无效任务操作"})
		return
	}
	job, err = s.startPatternBatch(job, fresh, input.Action == "retry-failed")
	if err != nil {
		writeJSON(w, 409, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, 202, job)
}

func (s *Server) startPatternBatch(job domain.PatternBatchJob, fresh, retry bool) (domain.PatternBatchJob, error) {
	s.patternBatchMu.Lock()
	defer s.patternBatchMu.Unlock()
	if s.history == nil && s.patternHistory == nil {
		return job, fmt.Errorf("日K数据服务未初始化")
	}
	if s.patternBatchActive != nil || !s.patternValidationMu.TryLock() {
		return job, fmt.Errorf("已有形态验证任务正在运行")
	}
	unlock, err := s.patternBatches.Acquire()
	if err != nil {
		s.patternValidationMu.Unlock()
		return job, err
	}
	release := func() { unlock(); s.patternValidationMu.Unlock() }
	if !fresh {
		job, err = s.patternBatches.LoadJob(job.ID)
		if err != nil {
			release()
			return job, err
		}
	}
	if job.AlgorithmVersion != strategy.PatternValidationVersion || job.ConfigHash != patternBatchDigest(job) {
		release()
		return job, fmt.Errorf("任务算法或配置版本不同，请新建任务")
	}
	if len(job.Items) != len(job.Request.Symbols) {
		release()
		return job, fmt.Errorf("任务证券检查点数量不一致")
	}
	for i, item := range job.Items {
		if item.Symbol != job.Request.Symbols[i] {
			release()
			return job, fmt.Errorf("任务证券检查点顺序不一致")
		}
	}
	if job.Status == "completed" || (job.Status == "partial" && !retry) {
		release()
		return job, fmt.Errorf("任务已结束，部分失败请使用重试失败项")
	}
	for i := range job.Items {
		if job.Items[i].State == "running" || (retry && job.Items[i].State == "failed") {
			job.Items[i].State, job.Items[i].Error, job.Items[i].InputHash = "pending", "", ""
		}
	}
	job.Status, job.Error, job.UpdatedAt = "running", "", s.currentTime()
	if err = s.patternBatches.SaveJob(job); err != nil {
		release()
		return job, err
	}
	if s.patternBatchErrors == nil {
		s.patternBatchErrors = map[string]string{}
	}
	delete(s.patternBatchErrors, job.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	active := &patternBatchExecution{id: job.ID, cancel: cancel, done: make(chan struct{})}
	s.patternBatchActive = active
	executionJob := job
	executionJob.Items = append([]domain.PatternBatchItem{}, job.Items...)
	executionJob.RunIDs = append([]string{}, job.RunIDs...)
	go s.runPatternBatch(ctx, executionJob, active, release)
	return job, nil
}

func (s *Server) StopPatternBatches() {
	s.patternBatchMu.Lock()
	active := s.patternBatchActive
	if active != nil {
		active.cancel()
	}
	s.patternBatchMu.Unlock()
	if active != nil {
		<-active.done
	}
}

func patternBatchInputError(input domain.PatternValidationInput, basis string) string {
	if input.Error != "" {
		return input.Error
	}
	if len(input.Bars) < 25 {
		return "有效日K不足25根"
	}
	if input.Symbol == "sh000300" {
		return ""
	}
	if input.Actions == nil {
		return "公司行动未核验"
	}
	if input.Actions.Status != "checked" || input.Actions.Error != "" {
		return "公司行动未核验: " + input.Actions.Error
	}
	if basis == domain.PatternPriceHFQ && (input.Adjustment == nil || input.Adjustment.Status != "checked" || input.Adjustment.Error != "") {
		return "复权因子未核验"
	}
	return ""
}

func (s *Server) runPatternBatch(ctx context.Context, job domain.PatternBatchJob, active *patternBatchExecution, release func()) {
	defer func() {
		active.cancel()
		release()
		s.patternBatchMu.Lock()
		s.patternBatchActive = nil
		close(active.done)
		s.patternBatchMu.Unlock()
	}()
	save := func() error { job.UpdatedAt = s.currentTime(); return s.patternBatches.SaveJob(job) }
	fail := func(err error) {
		job.Status, job.Error = "failed", err.Error()
		if ctx.Err() != nil {
			job.Status, job.Error = "cancelled", "任务已停止，已完成检查点保留"
			if ctx.Err() == context.DeadlineExceeded {
				job.Status, job.Error = "paused", "任务执行达到30分钟，已保存检查点可续跑"
			}
		}
		if e := save(); e != nil {
			s.patternBatchMu.Lock()
			s.patternBatchErrors[job.ID] = "检查点写入失败: " + e.Error()
			s.patternBatchMu.Unlock()
		}
	}
	job.Phase, job.CurrentSymbol = "benchmark", "sh000300"
	if err := save(); err != nil {
		fail(err)
		return
	}
	benchmark, err := s.patternBatches.LoadInput(job.ID, "sh000300", job.BenchmarkHash)
	if job.BenchmarkHash != "" && err != nil {
		fail(fmt.Errorf("基准检查点损坏，须新建任务: %w", err))
		return
	}
	if job.BenchmarkHash == "" {
		benchmark = s.fetchPatternValidationInput(ctx, "sh000300", job.Request)
		if message := patternBatchInputError(benchmark, job.Request.PriceBasis); message != "" {
			fail(fmt.Errorf("基准获取失败: %s", message))
			return
		}
		job.BenchmarkHash, err = s.patternBatches.SaveInput(job.ID, benchmark)
		if err != nil {
			fail(err)
			return
		}
		if err = save(); err != nil {
			fail(err)
			return
		}
	}
	for i := range job.Items {
		item := &job.Items[i]
		if item.State == "ready" || item.State == "failed" {
			continue
		}
		if ctx.Err() != nil {
			fail(ctx.Err())
			return
		}
		job.Phase, job.CurrentSymbol = "fetching", item.Symbol
		item.State = "running"
		item.Attempts++
		if err = save(); err != nil {
			fail(err)
			return
		}
		input := s.fetchPatternValidationInput(ctx, item.Symbol, job.Request)
		if ctx.Err() != nil {
			item.State = "pending"
			fail(ctx.Err())
			return
		}
		item.InputHash, err = s.patternBatches.SaveInput(job.ID, input)
		if err != nil {
			fail(err)
			return
		}
		item.Error = patternBatchInputError(input, job.Request.PriceBasis)
		item.State = "ready"
		if item.Error != "" {
			item.State = "failed"
		}
		if err = save(); err != nil {
			fail(err)
			return
		}
	}
	inputs := []domain.PatternValidationInput{}
	for i := range job.Items {
		item := &job.Items[i]
		input, loadErr := s.patternBatches.LoadInput(job.ID, item.Symbol, item.InputHash)
		if loadErr != nil {
			item.State, item.Error = "failed", loadErr.Error()
			input = domain.PatternValidationInput{Symbol: item.Symbol, Error: item.Error}
		}
		inputs = append(inputs, input)
	}
	inputs = append(inputs, benchmark)
	job.Phase, job.CurrentSymbol = "validating", ""
	if err = save(); err != nil {
		fail(err)
		return
	}
	report, err := strategy.BuildPatternValidation(ctx, job.Request, inputs, job.CreatedAt)
	if err != nil {
		fail(err)
		return
	}
	report.Pool, report.BatchID = &job.Pool, job.ID
	report.Warnings = append(report.Warnings, "股票池为冻结的静态名单，行业标签来自股票池声明来源，并非历史点时成份；失败证券保留在覆盖诊断中，续跑复用已完成数据检查点及同一份基准，采集时间可能不同。")
	for _, coverage := range report.Coverage {
		if coverage.Error != "" {
			for i := range job.Items {
				if job.Items[i].Symbol == coverage.Symbol {
					job.Items[i].State, job.Items[i].Error = "failed", coverage.Error
				}
			}
		}
	}
	// Serialize cancellation against the final immutable archive publication.
	s.patternBatchMu.Lock()
	if ctx.Err() != nil {
		s.patternBatchMu.Unlock()
		fail(ctx.Err())
		return
	}
	report, err = s.patternValidations.Save(report)
	if err != nil {
		s.patternBatchMu.Unlock()
		fail(err)
		return
	}
	job.RunIDs = append(job.RunIDs, report.RunID)
	job.Status, job.Phase, job.Error = "completed", "complete", ""
	for _, item := range job.Items {
		if item.State == "failed" {
			job.Status = "partial"
		}
	}
	if err = save(); err != nil {
		s.patternBatchErrors[job.ID] = "任务状态写入失败，验证归档已生成: " + err.Error()
	}
	s.patternBatchMu.Unlock()
}
