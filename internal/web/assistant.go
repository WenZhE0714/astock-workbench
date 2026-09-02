package web

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

// AIChatContext is the frozen, structured snapshot supplied to the Web
// assistant. The Web layer never asks the model to fetch data itself.
type AIChatContext struct {
	Symbol string
	Name   string
	Facts  domain.StockReportFacts
}

// AIChatAnswer is the result of one read-only Agent run. Fallback is true when
// the deterministic answer was used because one or more model calls failed.
type AIChatAnswer struct {
	Answer    string
	FactsAt   time.Time
	FactsHash string
	Agents    []domain.AgentResearchRun
	Fallback  bool
}

type AIChatConversation struct {
	Name  string
	Turns []domain.AIChatTurn
}

// AIChatService is implemented by the embedding application. Keeping this
// contract in the Web package avoids an import cycle and makes HTTP tests use
// a small in-memory fake instead of a live Codex process.
type AIChatService interface {
	Prepare(context.Context, string) (AIChatContext, error)
	Load(context.Context, string) (AIChatConversation, error)
	Ask(context.Context, string, string, []domain.AIChatTurn, func(string)) (AIChatAnswer, error)
	Save(context.Context, string, string, []domain.AIChatTurn) error
}

const (
	assistantContextTimeout  = 90 * time.Second
	assistantContextCacheTTL = 3 * time.Minute
	assistantJobRetention    = 30 * time.Minute
	assistantDefaultTimeout  = 20 * time.Minute
	assistantMaxQuestion     = 4000
)

type assistantContextCacheEntry struct {
	context   AIChatContext
	fetchedAt time.Time
}

type assistantLevel struct {
	Kind      string  `json:"kind"`
	Label     string  `json:"label"`
	Text      string  `json:"text"`
	Value     float64 `json:"value,omitempty"`
	Available bool    `json:"available"`
}

type assistantAlert struct {
	ID                string               `json:"id"`
	Kind              string               `json:"kind"`
	Severity          string               `json:"severity"`
	Symbol            string               `json:"symbol"`
	Name              string               `json:"name,omitempty"`
	Industry          string               `json:"industry,omitempty"`
	Title             string               `json:"title"`
	Detail            string               `json:"detail"`
	State             realtime.SignalState `json:"state,omitempty"`
	Score             float64              `json:"score,omitempty"`
	Price             float64              `json:"price,omitempty"`
	Percent           float64              `json:"percent,omitempty"`
	Speed             float64              `json:"speed_percent,omitempty"`
	TriggerPrice      float64              `json:"trigger_price,omitempty"`
	InvalidationPrice float64              `json:"invalidation_price,omitempty"`
	Reasons           []string             `json:"reasons,omitempty"`
	Risks             []string             `json:"risks,omitempty"`
	AsOf              time.Time            `json:"as_of,omitempty"`
	QuoteTime         string               `json:"quote_time,omitempty"`
	DataDate          string               `json:"data_date,omitempty"`
}

type assistantContextResponse struct {
	Symbol       string                  `json:"symbol"`
	Name         string                  `json:"name,omitempty"`
	FactsAt      time.Time               `json:"facts_at"`
	FactsHash    string                  `json:"facts_hash,omitempty"`
	Facts        domain.StockReportFacts `json:"facts"`
	KeyLevels    []assistantLevel        `json:"key_levels,omitempty"`
	History      []domain.AIChatTurn     `json:"history,omitempty"`
	Alerts       []assistantAlert        `json:"alerts,omitempty"`
	AlertAt      time.Time               `json:"alert_at,omitempty"`
	MarketState  string                  `json:"market_state,omitempty"`
	TradingDate  string                  `json:"trading_date,omitempty"`
	AlertWarning string                  `json:"alert_warning,omitempty"`
}

type assistantAlertsResponse struct {
	Symbol      string           `json:"symbol,omitempty"`
	GeneratedAt time.Time        `json:"generated_at,omitempty"`
	MarketState string           `json:"market_state,omitempty"`
	TradingDate string           `json:"trading_date,omitempty"`
	Alerts      []assistantAlert `json:"alerts"`
	Warning     string           `json:"warning,omitempty"`
}

type assistantChatRequest struct {
	Symbol   string `json:"symbol"`
	Question string `json:"question"`
}

type assistantChatResponse struct {
	JobID      string                     `json:"job_id"`
	Status     string                     `json:"status"`
	Symbol     string                     `json:"symbol"`
	Name       string                     `json:"name,omitempty"`
	Question   string                     `json:"question,omitempty"`
	Progress   string                     `json:"progress,omitempty"`
	CreatedAt  time.Time                  `json:"created_at,omitempty"`
	StartedAt  time.Time                  `json:"started_at,omitempty"`
	FinishedAt time.Time                  `json:"finished_at,omitempty"`
	Error      string                     `json:"error,omitempty"`
	Response   *assistantChatAnswerResult `json:"response,omitempty"`
}

type assistantChatAnswerResult struct {
	Answer      string                    `json:"answer"`
	AskedAt     time.Time                 `json:"asked_at"`
	FactsAt     time.Time                 `json:"facts_at,omitempty"`
	FactsHash   string                    `json:"facts_hash,omitempty"`
	Agents      []domain.AgentResearchRun `json:"agents,omitempty"`
	Fallback    bool                      `json:"fallback,omitempty"`
	SaveWarning string                    `json:"save_warning,omitempty"`
}

type assistantChatJob struct {
	mu         sync.Mutex
	id         string
	symbol     string
	name       string
	question   string
	history    []domain.AIChatTurn
	status     string
	progress   string
	createdAt  time.Time
	startedAt  time.Time
	finishedAt time.Time
	err        string
	response   *assistantChatAnswerResult
	cancel     context.CancelFunc
}

// assistantJobView takes a consistent copy suitable for JSON encoding.
func (job *assistantChatJob) assistantJobView() assistantChatResponse {
	if job == nil {
		return assistantChatResponse{Status: "unknown"}
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	view := assistantChatResponse{
		JobID: job.id, Status: job.status, Symbol: job.symbol, Name: job.name,
		Question: job.question, Progress: job.progress, CreatedAt: job.createdAt,
		StartedAt: job.startedAt, FinishedAt: job.finishedAt, Error: job.err,
	}
	if job.response != nil {
		copyResult := *job.response
		copyResult.Agents = append([]domain.AgentResearchRun(nil), job.response.Agents...)
		view.Response = &copyResult
	}
	return view
}

func (job *assistantChatJob) setRunning(progress string, at time.Time) {
	job.mu.Lock()
	if job.status == "queued" {
		job.status = "running"
		job.startedAt = at
	}
	if strings.TrimSpace(progress) != "" {
		job.progress = strings.TrimSpace(progress)
	}
	job.mu.Unlock()
}

func (job *assistantChatJob) setProgress(progress string) {
	job.mu.Lock()
	if job.status == "queued" || job.status == "running" {
		job.progress = strings.TrimSpace(progress)
	}
	job.mu.Unlock()
}

func (job *assistantChatJob) fail(err error, at time.Time) {
	job.mu.Lock()
	if job.status != "canceled" {
		job.status = "failed"
		job.finishedAt = at
		if err != nil {
			job.err = err.Error()
		}
	}
	job.mu.Unlock()
}

func (job *assistantChatJob) complete(result *assistantChatAnswerResult, at time.Time) {
	job.mu.Lock()
	if job.status != "canceled" {
		job.status = "completed"
		job.finishedAt = at
		job.progress = ""
		job.response = result
	}
	job.mu.Unlock()
}

func (job *assistantChatJob) cancelJob() bool {
	job.mu.Lock()
	if job.status == "completed" || job.status == "failed" || job.status == "canceled" {
		job.mu.Unlock()
		return false
	}
	job.status = "canceled"
	job.finishedAt = time.Now()
	job.err = "用户取消了本轮咨询"
	cancel := job.cancel
	job.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}

func assistantChatTimeout() time.Duration {
	seconds := int(assistantDefaultTimeout / time.Second)
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ASTOCK_CODEX_TIMEOUT_SECONDS"))); err == nil && value >= 30 && value <= 1800 {
		// Agent roles run concurrently and the supervisor runs afterwards. Give
		// the Web job enough room for both phases while retaining a hard bound.
		seconds = value*2 + 30
	}
	if seconds < 120 {
		seconds = 120
	}
	if seconds > 3600 {
		seconds = 3600
	}
	return time.Duration(seconds) * time.Second
}

func (s *Server) handleAssistantContext(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "AI上下文只支持 GET"})
		return
	}
	if s.aiChatService == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "AI助手服务未初始化"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), assistantContextTimeout)
	defer cancel()
	symbol, err := s.resolveAssistantSymbol(ctx, request.URL.Query().Get("symbol"))
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	if market.AssetKindOf(symbol) == domain.AssetKindSector {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "AI助手当前只支持个股或指数，不支持行业板块"})
		return
	}
	prepared, err := s.aiChatService.Prepare(ctx, symbol)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, errorResponse{Error: "采集AI上下文失败: " + err.Error()})
		return
	}
	s.cacheAssistantContext(prepared)
	conversation, conversationErr := s.aiChatService.Load(ctx, symbol)
	if conversationErr != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取AI会话失败: " + conversationErr.Error()})
		return
	}
	latest, latestErr := s.latestRealtimeSnapshot()
	alerts := sortAssistantAlerts(append(buildAssistantAlerts(latest, symbol), assistantFactLevelAlerts(prepared.Facts)...), 12)
	response := assistantContextResponse{
		Symbol: prepared.Symbol, Name: prepared.Name, FactsAt: prepared.Facts.GeneratedAt,
		FactsHash: prepared.Facts.SnapshotHash, Facts: prepared.Facts,
		KeyLevels: assistantKeyLevels(prepared.Facts), History: assistantHistoryForWeb(conversation.Turns),
		Alerts: alerts, AlertAt: latest.GeneratedAt, MarketState: latest.MarketState, TradingDate: latest.TradingDate,
	}
	if response.Name == "" {
		response.Name = conversation.Name
	}
	if latestErr != nil {
		response.AlertWarning = latestErr.Error()
	}
	writeJSON(writer, http.StatusOK, response)
}

func (s *Server) handleAssistantAlerts(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "AI提醒只支持 GET"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	rawSymbol := strings.TrimSpace(request.URL.Query().Get("symbol"))
	symbol := ""
	if rawSymbol != "" {
		resolved, err := s.resolveAssistantSymbol(ctx, rawSymbol)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		symbol = resolved
	}
	result, err := s.latestRealtimeSnapshot()
	alerts := buildAssistantAlerts(result, symbol)
	alerts = sortAssistantAlerts(append(alerts, s.cachedAssistantFactAlerts(symbol)...), 24)
	response := assistantAlertsResponse{Symbol: symbol, Alerts: alerts}
	if !result.GeneratedAt.IsZero() {
		response.GeneratedAt = result.GeneratedAt
		response.MarketState = result.MarketState
		response.TradingDate = result.TradingDate
	}
	if err != nil {
		response.Warning = err.Error()
	}
	writeJSON(writer, http.StatusOK, response)
}

func (s *Server) cacheAssistantContext(contextSnapshot AIChatContext) {
	if s == nil || strings.TrimSpace(contextSnapshot.Symbol) == "" {
		return
	}
	s.assistantContextMu.Lock()
	if s.assistantContexts == nil {
		s.assistantContexts = make(map[string]assistantContextCacheEntry)
	}
	s.assistantContexts[strings.ToLower(strings.TrimSpace(contextSnapshot.Symbol))] = assistantContextCacheEntry{context: contextSnapshot, fetchedAt: s.currentTime()}
	for key, entry := range s.assistantContexts {
		if s.currentTime().Sub(entry.fetchedAt) > assistantContextCacheTTL {
			delete(s.assistantContexts, key)
		}
	}
	s.assistantContextMu.Unlock()
}

func (s *Server) cachedAssistantFactAlerts(symbol string) []assistantAlert {
	if s == nil {
		return nil
	}
	now := s.currentTime()
	s.assistantContextMu.Lock()
	defer s.assistantContextMu.Unlock()
	alerts := make([]assistantAlert, 0)
	for key, entry := range s.assistantContexts {
		if now.Sub(entry.fetchedAt) > assistantContextCacheTTL {
			delete(s.assistantContexts, key)
			continue
		}
		if strings.TrimSpace(symbol) != "" && !assistantSymbolsEqual(key, symbol) {
			continue
		}
		alerts = append(alerts, assistantFactLevelAlerts(entry.context.Facts)...)
	}
	return alerts
}

func (s *Server) handleAssistantChat(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodPost:
		s.startAssistantChat(writer, request)
	case http.MethodGet:
		s.readAssistantChat(writer, request)
	case http.MethodDelete:
		s.cancelAssistantChat(writer, request)
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "AI聊天只支持 POST、GET、DELETE"})
	}
}

func (s *Server) startAssistantChat(writer http.ResponseWriter, request *http.Request) {
	if s.aiChatService == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "AI助手服务未初始化"})
		return
	}
	var input assistantChatRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16<<10)).Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "AI聊天请求格式无效"})
		return
	}
	input.Question = strings.TrimSpace(input.Question)
	if input.Question == "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "请输入要咨询的问题"})
		return
	}
	if len([]rune(input.Question)) > assistantMaxQuestion {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: fmt.Sprintf("问题不能超过%d个字", assistantMaxQuestion)})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	symbol, err := s.resolveAssistantSymbol(ctx, input.Symbol)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	if market.AssetKindOf(symbol) == domain.AssetKindSector {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "AI助手当前只支持个股或指数，不支持行业板块"})
		return
	}
	conversation, err := s.aiChatService.Load(ctx, symbol)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取AI会话失败: " + err.Error()})
		return
	}
	history := assistantHistoryForModel(conversation.Turns)
	job, existing := s.createAssistantJob(symbol, conversation.Name, input.Question, history)
	if existing {
		writeJSON(writer, http.StatusConflict, job.assistantJobView())
		return
	}
	go s.runAssistantJob(job)
	writeJSON(writer, http.StatusAccepted, job.assistantJobView())
}

func (s *Server) readAssistantChat(writer http.ResponseWriter, request *http.Request) {
	id := strings.TrimSpace(request.URL.Query().Get("job_id"))
	if id == "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "缺少job_id"})
		return
	}
	job := s.findAssistantJob(id)
	if job == nil {
		writeJSON(writer, http.StatusNotFound, errorResponse{Error: "AI聊天任务不存在或已过期"})
		return
	}
	writeJSON(writer, http.StatusOK, job.assistantJobView())
}

func (s *Server) cancelAssistantChat(writer http.ResponseWriter, request *http.Request) {
	id := strings.TrimSpace(request.URL.Query().Get("job_id"))
	if id == "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "缺少job_id"})
		return
	}
	job := s.findAssistantJob(id)
	if job == nil {
		writeJSON(writer, http.StatusNotFound, errorResponse{Error: "AI聊天任务不存在或已过期"})
		return
	}
	job.cancelJob()
	writeJSON(writer, http.StatusOK, job.assistantJobView())
}

func (s *Server) resolveAssistantSymbol(ctx context.Context, raw string) (string, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		input = strings.TrimSpace(s.defaultSymbol)
	}
	if input == "" {
		return "", fmt.Errorf("缺少股票代码或名称")
	}
	if s.resolver == nil {
		if market.ValidPrefixedSymbol(input) {
			return strings.ToLower(input), nil
		}
		return "", fmt.Errorf("证券解析服务未初始化")
	}
	symbol, err := s.resolver.Resolve(ctx, input)
	if err != nil {
		return "", err
	}
	if !market.ValidPrefixedSymbol(symbol) {
		return "", fmt.Errorf("无法解析有效股票代码")
	}
	return strings.ToLower(strings.TrimSpace(symbol)), nil
}

func assistantHistoryForModel(turns []domain.AIChatTurn) []domain.AIChatTurn {
	if len(turns) > 6 {
		turns = turns[len(turns)-6:]
	}
	return append([]domain.AIChatTurn(nil), turns...)
}

func assistantHistoryForWeb(turns []domain.AIChatTurn) []domain.AIChatTurn {
	if len(turns) > 20 {
		turns = turns[len(turns)-20:]
	}
	result := make([]domain.AIChatTurn, len(turns))
	copy(result, turns)
	for index := range result {
		result[index].Agents = append([]domain.AgentResearchRun(nil), turns[index].Agents...)
		for agentIndex := range result[index].Agents {
			// The previous answer is useful in the transcript; repeating every
			// specialist's 3KB analysis makes the context endpoint unnecessarily
			// large and is not needed for the next model call.
			result[index].Agents[agentIndex].Analysis = ""
		}
	}
	return result
}

func (s *Server) createAssistantJob(symbol, name, question string, history []domain.AIChatTurn) (*assistantChatJob, bool) {
	if s == nil {
		return nil, false
	}
	now := s.currentTime()
	s.assistantJobsMu.Lock()
	defer s.assistantJobsMu.Unlock()
	if s.assistantJobs == nil {
		s.assistantJobs = make(map[string]*assistantChatJob)
	}
	for id, candidate := range s.assistantJobs {
		view := candidate.assistantJobView()
		if view.Status == "completed" || view.Status == "failed" || view.Status == "canceled" {
			if !view.FinishedAt.IsZero() && now.Sub(view.FinishedAt) > assistantJobRetention {
				delete(s.assistantJobs, id)
			}
			continue
		}
		if assistantSymbolsEqual(view.Symbol, symbol) {
			return candidate, true
		}
	}
	sequence := atomic.AddUint64(&s.assistantJobSequence, 1)
	id := fmt.Sprintf("ai-%d-%d", now.UnixNano(), sequence)
	job := &assistantChatJob{
		id: id, symbol: symbol, name: strings.TrimSpace(name), question: question,
		history: append([]domain.AIChatTurn(nil), history...), status: "queued", progress: "等待Agent启动", createdAt: now,
	}
	s.assistantJobs[id] = job
	return job, false
}

func (s *Server) findAssistantJob(id string) *assistantChatJob {
	if s == nil {
		return nil
	}
	s.assistantJobsMu.Lock()
	job := s.assistantJobs[id]
	s.assistantJobsMu.Unlock()
	return job
}

func (s *Server) runAssistantJob(job *assistantChatJob) {
	if job == nil || s == nil || s.aiChatService == nil {
		return
	}
	jobContext, cancel := context.WithTimeout(context.Background(), assistantChatTimeout())
	job.mu.Lock()
	if job.status == "canceled" {
		job.mu.Unlock()
		cancel()
		return
	}
	job.cancel = cancel
	job.mu.Unlock()
	defer cancel()
	job.setRunning("采集当前股票多维数据", s.currentTime())
	answer, err := s.aiChatService.Ask(jobContext, job.symbol, job.question, assistantHistoryForModel(job.history), func(progress string) {
		job.setProgress(progress)
	})
	if err != nil {
		job.fail(err, s.currentTime())
		return
	}
	if strings.TrimSpace(answer.Answer) == "" {
		job.fail(fmt.Errorf("AI Agent未返回回答"), s.currentTime())
		return
	}
	askedAt := s.currentTime()
	turn := domain.AIChatTurn{AskedAt: askedAt, FactsAt: answer.FactsAt, FactsHash: answer.FactsHash, Question: job.question, Answer: answer.Answer, Agents: append([]domain.AgentResearchRun(nil), answer.Agents...), Fallback: answer.Fallback}
	history := append([]domain.AIChatTurn(nil), job.history...)
	history = append(history, turn)
	saveWarning := ""
	if saveErr := s.aiChatService.Save(jobContext, job.symbol, job.name, history); saveErr != nil {
		saveWarning = saveErr.Error()
	}
	result := &assistantChatAnswerResult{Answer: answer.Answer, AskedAt: askedAt, FactsAt: answer.FactsAt, FactsHash: answer.FactsHash, Agents: append([]domain.AgentResearchRun(nil), answer.Agents...), Fallback: answer.Fallback, SaveWarning: saveWarning}
	job.complete(result, s.currentTime())
}

func (s *Server) cancelAssistantJobs() {
	if s == nil {
		return
	}
	s.assistantJobsMu.Lock()
	jobs := make([]*assistantChatJob, 0, len(s.assistantJobs))
	for _, job := range s.assistantJobs {
		jobs = append(jobs, job)
	}
	s.assistantJobsMu.Unlock()
	for _, job := range jobs {
		job.cancelJob()
	}
}

func assistantSymbolsEqual(left, right string) bool {
	normalize := func(value string) string {
		value = strings.ToLower(strings.TrimSpace(value))
		for _, prefix := range []string{"sh", "sz", "bj", "th"} {
			if strings.HasPrefix(value, prefix) {
				return strings.TrimPrefix(value, prefix)
			}
		}
		return value
	}
	return normalize(left) != "" && normalize(left) == normalize(right)
}

func assistantKeyLevels(facts domain.StockReportFacts) []assistantLevel {
	levels := make([]assistantLevel, 0, 7)
	appendText := func(kind, label, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		levels = append(levels, assistantLevel{Kind: kind, Label: label, Text: strings.TrimSpace(text), Available: true})
	}
	appendText("support", "支撑", facts.Technical.Support)
	appendText("resistance", "压力", facts.Technical.Resistance)
	appendText("buy_trigger", "观察触发", facts.Technical.BuyTrigger)
	appendText("sell_trigger", "减仓观察", facts.Technical.SellTrigger)
	appendText("invalidation", "失效", facts.Technical.Invalidation)
	if facts.PriceBoundary.Available {
		levels = append(levels,
			assistantLevel{Kind: "limit_down", Label: "当日跌停", Text: fmt.Sprintf("%.2f 元", facts.PriceBoundary.LimitDown), Value: facts.PriceBoundary.LimitDown, Available: true},
			assistantLevel{Kind: "limit_up", Label: "当日涨停", Text: fmt.Sprintf("%.2f 元", facts.PriceBoundary.LimitUp), Value: facts.PriceBoundary.LimitUp, Available: true},
		)
	}
	return levels
}

// assistantFactLevelAlerts turns numeric levels already present in the frozen
// snapshot into low-noise proximity reminders. It deliberately does not infer
// a trade direction or create a new price target.
func assistantFactLevelAlerts(facts domain.StockReportFacts) []assistantAlert {
	price := facts.Quote.Price
	if price <= 0 {
		price = facts.Technical.Price
	}
	if price <= 0 {
		return nil
	}
	type level struct {
		kind  string
		label string
		value float64
	}
	levels := []level{
		{kind: "ma20", label: "MA20", value: facts.Technical.MA20},
		{kind: "ma60", label: "MA60", value: facts.Technical.MA60},
		{kind: "high20", label: "20日高点", value: facts.Technical.High20},
		{kind: "low20", label: "20日低点", value: facts.Technical.Low20},
	}
	if facts.PriceBoundary.Available {
		levels = append(levels,
			level{kind: "limit_up", label: "当日涨停", value: facts.PriceBoundary.LimitUp},
			level{kind: "limit_down", label: "当日跌停", value: facts.PriceBoundary.LimitDown},
		)
	}
	alerts := make([]assistantAlert, 0, len(levels))
	for _, item := range levels {
		if item.value <= 0 {
			continue
		}
		distance := (price - item.value) / item.value * 100
		if distance < -1.5 || distance > 1.5 {
			continue
		}
		title := "接近" + item.label
		severity := "medium"
		if math.Abs(distance) <= .25 {
			title = "触及" + item.label
			severity = "high"
		}
		direction := "附近"
		if distance > 0 {
			direction = "上方"
		} else if distance < 0 {
			direction = "下方"
		}
		idDate := facts.Technical.DataDate
		if idDate == "" {
			idDate = facts.GeneratedAt.Format("2006-01-02")
		}
		alerts = append(alerts, assistantAlert{
			ID: fmt.Sprintf("facts:%s:%s:%s", facts.Quote.Symbol, item.kind, idDate), Kind: "fact-level", Severity: severity,
			Symbol: facts.Quote.Symbol, Name: facts.Quote.Name, Title: title,
			Detail: fmt.Sprintf("现价 %.2f 元 · %s %.2f 元 · 位于%s %.2f%%", price, item.label, item.value, direction, math.Abs(distance)),
			Price:  price, AsOf: facts.GeneratedAt, DataDate: facts.Technical.DataDate,
		})
	}
	return alerts
}

func buildAssistantAlerts(result realtime.ScanResult, symbol string) []assistantAlert {
	alerts := make([]assistantAlert, 0)
	for _, signal := range result.Signals {
		if signal.State != realtime.StateTriggered && signal.State != realtime.StateWatching {
			continue
		}
		if strings.TrimSpace(symbol) != "" && !assistantSymbolsEqual(signal.Symbol, symbol) {
			continue
		}
		severity := "medium"
		title := "实时选股观察"
		if signal.State == realtime.StateTriggered {
			severity = "high"
			title = "实时选股触发"
		}
		reason := "结构化因子满足观察条件"
		if len(signal.Reasons) > 0 && strings.TrimSpace(signal.Reasons[0]) != "" {
			reason = signal.Reasons[0]
		}
		detail := fmt.Sprintf("评分 %.1f · 涨跌 %+.2f%% · 涨速 %+.2f%% · %s", signal.Score, signal.Percent, signal.Speed, reason)
		if signal.Industry != "" {
			detail += " · " + signal.Industry
		}
		baseID := strings.TrimSpace(signal.ID)
		if baseID == "" {
			baseID = fmt.Sprintf("%s-%d", signal.Symbol, signal.AsOf.UnixNano())
		}
		alerts = append(alerts, assistantAlert{
			ID: baseID + ":signal", Kind: "signal", Severity: severity, Symbol: signal.Symbol, Name: signal.Name,
			Industry: signal.Industry, Title: title, Detail: detail, State: signal.State, Score: signal.Score,
			Price: signal.Price, Percent: signal.Percent, Speed: signal.Speed, TriggerPrice: signal.TriggerPrice,
			InvalidationPrice: signal.InvalidationPrice, Reasons: append([]string(nil), signal.Reasons...), Risks: append([]string(nil), signal.Risks...),
			AsOf: signal.AsOf, QuoteTime: signal.QuoteTime, DataDate: signal.DataDate,
		})
		if signal.Price > 0 && signal.TriggerPrice > 0 {
			distance := (signal.TriggerPrice - signal.Price) / signal.TriggerPrice * 100
			if signal.Price >= signal.TriggerPrice {
				alerts = append(alerts, assistantAlert{ID: baseID + ":trigger", Kind: "level", Severity: "high", Symbol: signal.Symbol, Name: signal.Name, Title: "已到观察触发价", Detail: fmt.Sprintf("现价 %.2f 元 · 触发价 %.2f 元", signal.Price, signal.TriggerPrice), State: signal.State, Price: signal.Price, TriggerPrice: signal.TriggerPrice, AsOf: signal.AsOf})
			} else if distance >= 0 && distance <= 1.2 {
				alerts = append(alerts, assistantAlert{ID: baseID + ":near-trigger", Kind: "level", Severity: "medium", Symbol: signal.Symbol, Name: signal.Name, Title: "接近观察触发价", Detail: fmt.Sprintf("现价 %.2f 元 · 距触发价 %.2f 元 %.2f%%", signal.Price, signal.TriggerPrice, distance), State: signal.State, Price: signal.Price, TriggerPrice: signal.TriggerPrice, AsOf: signal.AsOf})
			}
		}
		if signal.Price > 0 && signal.InvalidationPrice > 0 {
			distance := (signal.Price - signal.InvalidationPrice) / signal.InvalidationPrice * 100
			if signal.Price <= signal.InvalidationPrice {
				alerts = append(alerts, assistantAlert{ID: baseID + ":invalidated", Kind: "risk", Severity: "high", Symbol: signal.Symbol, Name: signal.Name, Title: "触及失效观察位", Detail: fmt.Sprintf("现价 %.2f 元 · 失效位 %.2f 元", signal.Price, signal.InvalidationPrice), State: signal.State, Price: signal.Price, InvalidationPrice: signal.InvalidationPrice, AsOf: signal.AsOf})
			} else if distance >= 0 && distance <= 1.2 {
				alerts = append(alerts, assistantAlert{ID: baseID + ":near-invalidated", Kind: "risk", Severity: "medium", Symbol: signal.Symbol, Name: signal.Name, Title: "接近失效观察位", Detail: fmt.Sprintf("现价 %.2f 元 · 距失效位 %.2f 元 %.2f%%", signal.Price, signal.InvalidationPrice, distance), State: signal.State, Price: signal.Price, InvalidationPrice: signal.InvalidationPrice, AsOf: signal.AsOf})
			}
		}
	}
	limit := 24
	if strings.TrimSpace(symbol) != "" {
		limit = 12
	}
	return sortAssistantAlerts(alerts, limit)
}

func sortAssistantAlerts(alerts []assistantAlert, limit int) []assistantAlert {
	severityRank := map[string]int{"high": 0, "medium": 1, "low": 2}
	sort.SliceStable(alerts, func(left, right int) bool {
		if severityRank[alerts[left].Severity] != severityRank[alerts[right].Severity] {
			return severityRank[alerts[left].Severity] < severityRank[alerts[right].Severity]
		}
		return alerts[left].AsOf.After(alerts[right].AsOf)
	})
	if limit > 0 && len(alerts) > limit {
		alerts = alerts[:limit]
	}
	return alerts
}
