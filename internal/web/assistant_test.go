package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

type assistantResolverStub struct{}

func (assistantResolverStub) Resolve(_ context.Context, value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "600519" {
		return "sh600519", nil
	}
	return value, nil
}

type assistantServiceStub struct {
	mu       sync.Mutex
	context  AIChatContext
	turns    []domain.AIChatTurn
	tasks    int
	saved    []domain.AIChatTurn
	canceled bool
}

func (stub *assistantServiceStub) Prepare(_ context.Context, symbol string) (AIChatContext, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	result := stub.context
	result.Symbol = symbol
	result.Facts.Quote.Symbol = symbol
	return result, nil
}

func (stub *assistantServiceStub) Load(_ context.Context, _ string) (AIChatConversation, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return AIChatConversation{Name: "贵州茅台", Turns: append([]domain.AIChatTurn(nil), stub.turns...)}, nil
}

func (stub *assistantServiceStub) Ask(_ context.Context, _ string, question string, history []domain.AIChatTurn, progress func(string)) (AIChatAnswer, error) {
	stub.mu.Lock()
	stub.tasks++
	stub.mu.Unlock()
	if progress != nil {
		progress("测试Agent已完成事实校验")
	}
	return AIChatAnswer{Answer: "基于当前快照，先观察承接和失效条件。问题：" + question, FactsAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.Local), FactsHash: "sha256:test", Agents: []domain.AgentResearchRun{{Role: "technical", Label: "技术与量价", Status: "ok"}}, Fallback: false}, nil
}

func (stub *assistantServiceStub) Save(_ context.Context, _ string, _ string, turns []domain.AIChatTurn) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.saved = append([]domain.AIChatTurn(nil), turns...)
	return nil
}

type assistantArchiveStub struct {
	latest realtime.ScanResult
}

type blockingAssistantService struct {
	started chan struct{}
}

func (stub *blockingAssistantService) Prepare(_ context.Context, symbol string) (AIChatContext, error) {
	return AIChatContext{Symbol: symbol, Facts: assistantTestFacts()}, nil
}

func (stub *blockingAssistantService) Load(_ context.Context, _ string) (AIChatConversation, error) {
	return AIChatConversation{Name: "贵州茅台"}, nil
}

func (stub *blockingAssistantService) Ask(ctx context.Context, _ string, _ string, _ []domain.AIChatTurn, _ func(string)) (AIChatAnswer, error) {
	select {
	case <-stub.started:
	default:
		close(stub.started)
	}
	<-ctx.Done()
	return AIChatAnswer{}, ctx.Err()
}

func (stub *blockingAssistantService) Save(_ context.Context, _ string, _ string, _ []domain.AIChatTurn) error {
	return nil
}

func (stub assistantArchiveStub) List(_ int) ([]realtime.Signal, error) {
	return append([]realtime.Signal(nil), stub.latest.Signals...), nil
}

func (stub assistantArchiveStub) Latest() (realtime.ScanResult, error) {
	return stub.latest, nil
}

func assistantTestFacts() domain.StockReportFacts {
	return domain.StockReportFacts{
		GeneratedAt:   time.Date(2026, 9, 1, 10, 0, 0, 0, time.Local),
		Quote:         domain.StockQuoteSnapshot{Symbol: "sh600519", Name: "贵州茅台", Price: 1500, Percent: 1.2, VolumeRatio: 1.5},
		PriceBoundary: domain.StockPriceBoundary{Available: true, LimitDown: 1350, LimitUp: 1650, TradeDate: "2026-09-01"},
		Technical:     domain.TechnicalSignal{Bias: "看涨", DataDate: "2026-08-31", Support: "MA20 1450 元", Resistance: "前高 1560 元", BuyTrigger: "放量站上 1520 元", Invalidation: "跌破 1450 元"},
	}
}

func TestAssistantContextIncludesFactsHistoryLevelsAndAlerts(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.Local)
	service := &assistantServiceStub{context: AIChatContext{Facts: assistantTestFacts()}, turns: []domain.AIChatTurn{{Question: "怎么看", Answer: "等待确认"}}}
	server := NewServer(assistantResolverStub{}, nil, nil, nil, "600519", WithAIChatService(service), WithRealtimeStrategy(nil, assistantArchiveStub{latest: realtime.ScanResult{
		GeneratedAt: now, Signals: []realtime.Signal{{ID: "sig-1", Symbol: "sh600519", Name: "贵州茅台", State: realtime.StateTriggered, Score: 82, Price: 1521, TriggerPrice: 1520, InvalidationPrice: 1450, AsOf: now, Reasons: []string{"量价共振"}}},
	}}))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/assistant/context?symbol=600519", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("context status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response assistantContextResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode context: %v", err)
	}
	if response.Symbol != "sh600519" || response.Name != "贵州茅台" {
		t.Fatalf("unexpected identity: %+v", response)
	}
	if len(response.History) != 1 || len(response.KeyLevels) < 3 {
		t.Fatalf("facts/history/levels missing: history=%d levels=%d", len(response.History), len(response.KeyLevels))
	}
	if len(response.Alerts) < 2 {
		t.Fatalf("expected signal and trigger alerts, got %d", len(response.Alerts))
	}
}

func TestAssistantAlertsSkipWeakSignalsAndFilterBySymbol(t *testing.T) {
	now := time.Now()
	archive := assistantArchiveStub{latest: realtime.ScanResult{GeneratedAt: now, Signals: []realtime.Signal{
		{ID: "triggered", Symbol: "sh600519", State: realtime.StateTriggered, Price: 10, TriggerPrice: 9, AsOf: now},
		{ID: "watching", Symbol: "sz000001", State: realtime.StateWatching, Price: 10, TriggerPrice: 12, AsOf: now},
		{ID: "weak", Symbol: "sh600519", State: realtime.StateWeak, Price: 10, AsOf: now},
	}}}
	server := NewServer(assistantResolverStub{}, nil, nil, nil, "600519", WithRealtimeStrategy(nil, archive))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/assistant/alerts?symbol=600519", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("alerts status = %d", recorder.Code)
	}
	var response assistantAlertsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode alerts: %v", err)
	}
	for _, alert := range response.Alerts {
		if !assistantSymbolsEqual(alert.Symbol, "sh600519") {
			t.Fatalf("unfiltered alert: %+v", alert)
		}
	}
	if len(response.Alerts) != 2 {
		t.Fatalf("expected signal and level alert, got %d", len(response.Alerts))
	}
}

func TestAssistantChatJobCompletesAndPersistsTurn(t *testing.T) {
	service := &assistantServiceStub{context: AIChatContext{Facts: assistantTestFacts()}}
	server := NewServer(assistantResolverStub{}, nil, nil, nil, "600519", WithAIChatService(service))
	request := httptest.NewRequest(http.MethodPost, "/api/assistant/chat", strings.NewReader(`{"symbol":"600519","question":"现在怎么看？"}`))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("chat start status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var started assistantChatResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if started.JobID == "" {
		t.Fatal("missing job id")
	}
	var completed assistantChatResponse
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		poll := httptest.NewRecorder()
		server.Handler().ServeHTTP(poll, httptest.NewRequest(http.MethodGet, "/api/assistant/chat?job_id="+started.JobID, nil))
		if err := json.Unmarshal(poll.Body.Bytes(), &completed); err != nil {
			t.Fatalf("decode poll: %v", err)
		}
		if completed.Status == "completed" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if completed.Status != "completed" || completed.Response == nil {
		t.Fatalf("job did not complete: %+v", completed)
	}
	service.mu.Lock()
	saved := len(service.saved)
	tasks := service.tasks
	service.mu.Unlock()
	if tasks != 1 || saved != 1 {
		t.Fatalf("expected one task and saved turn, tasks=%d saved=%d", tasks, saved)
	}
}

func TestAssistantChatJobCanBeCanceled(t *testing.T) {
	service := &blockingAssistantService{started: make(chan struct{})}
	server := NewServer(assistantResolverStub{}, nil, nil, nil, "600519", WithAIChatService(service))
	startRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(startRecorder, httptest.NewRequest(http.MethodPost, "/api/assistant/chat", strings.NewReader(`{"symbol":"600519","question":"等待吗？"}`)))
	if startRecorder.Code != http.StatusAccepted {
		t.Fatalf("chat start status = %d", startRecorder.Code)
	}
	var started assistantChatResponse
	if err := json.Unmarshal(startRecorder.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("blocking service did not start")
	}
	cancelRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(cancelRecorder, httptest.NewRequest(http.MethodDelete, "/api/assistant/chat?job_id="+started.JobID, nil))
	if cancelRecorder.Code != http.StatusOK {
		t.Fatalf("cancel status = %d", cancelRecorder.Code)
	}
	var canceled assistantChatResponse
	if err := json.Unmarshal(cancelRecorder.Body.Bytes(), &canceled); err != nil {
		t.Fatalf("decode cancel: %v", err)
	}
	if canceled.Status != "canceled" {
		t.Fatalf("expected canceled status, got %+v", canceled)
	}
}
