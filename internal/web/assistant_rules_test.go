package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type chartAwareAssistantStub struct {
	*assistantServiceStub
	chartMu  sync.Mutex
	received *domain.AssistantChartContext
}

func (stub *chartAwareAssistantStub) AskWithChart(ctx context.Context, symbol, question string, history []domain.AIChatTurn, chart *domain.AssistantChartContext, progress func(string)) (AIChatAnswer, error) {
	stub.chartMu.Lock()
	stub.received = cloneAssistantChartContext(chart)
	stub.chartMu.Unlock()
	return stub.Ask(ctx, symbol, question, history, progress)
}

func (stub *chartAwareAssistantStub) DraftRule(_ context.Context, symbol, question, expiresOn string, chart domain.AssistantChartContext) (domain.AssistantRuleDraft, error) {
	levels := chart.SelectedStructure.Plan
	proposal := domain.AssistantRuleProposal{
		Kind: "breakout", Name: "验证突破规则", Description: "完整日K收盘突破确认价并满足量能条件",
		EntryLow: levels.EntryLow, EntryHigh: levels.EntryHigh, Invalidation: levels.Invalidation,
		ConfirmationPrice: levels.EntryLow, VolumeDays: 20, MinimumVolumeRatio: 1.2,
	}
	createdAt, _ := time.ParseInLocation(time.DateOnly, chart.Analysis.DataDate, time.FixedZone("Asia/Shanghai", 8*60*60))
	return strategy.BuildAssistantRuleDraft(chart.Analysis, chart.SelectedStructure, question, expiresOn, proposal, createdAt.Add(16*time.Hour))
}

func assistantChartRequest(analysis domain.ChartAnalysis) assistantChartInput {
	return assistantChartInput{
		Timeframe: "1d", Through: analysis.DataDate, Fingerprint: analysis.Fingerprint,
		StructureID: analysis.Structures[0].ID, VisibleFrom: "2026-08-01", VisibleTo: analysis.DataDate,
	}
}

func postAssistantRule(t *testing.T, server *Server, input assistantRuleRequest) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/assistant/rule-drafts", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestAssistantRuleDraftRequiresVerifiedChartAndUserConfirmation(t *testing.T) {
	root := t.TempDir()
	server := chartServerFixture(filepath.Join(root, "plans"))
	service := &chartAwareAssistantStub{assistantServiceStub: &assistantServiceStub{context: AIChatContext{Facts: assistantTestFacts()}}}
	server.aiChatService = service
	analysis := chartRequest(t, server)
	chart := assistantChartRequest(analysis)
	recorder := postAssistantRule(t, server, assistantRuleRequest{
		Action: "draft", Symbol: analysis.Symbol, Question: "突破以后进入区间时提醒", ExpiresOn: "2026-09-25", Chart: &chart,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("draft failed: %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Draft domain.AssistantRuleDraft `json:"draft"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Draft.ID == "" {
		t.Fatalf("decode draft: %v %s", err, recorder.Body.String())
	}
	tampered := response.Draft
	tampered.Proposal.EntryHigh++
	recorder = postAssistantRule(t, server, assistantRuleRequest{Action: "confirm", Draft: &tampered})
	if recorder.Code != http.StatusConflict {
		t.Fatalf("tampered draft accepted: %d %s", recorder.Code, recorder.Body.String())
	}
	recorder = postAssistantRule(t, server, assistantRuleRequest{Action: "confirm", Draft: &response.Draft})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("confirmation failed: %d %s", recorder.Code, recorder.Body.String())
	}
	var saved struct {
		Plan domain.TradePlan `json:"plan"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &saved); err != nil || saved.Plan.MonitorRule == nil || saved.Plan.Structure.ID != "assistant-breakout" {
		t.Fatalf("confirmed plan did not preserve rule: %v %s", err, recorder.Body.String())
	}
	stale := chart
	stale.Fingerprint = "stale"
	recorder = postAssistantRule(t, server, assistantRuleRequest{
		Action: "draft", Symbol: analysis.Symbol, Question: "旧快照", ExpiresOn: "2026-09-25", Chart: &stale,
	})
	if recorder.Code != http.StatusConflict {
		t.Fatalf("stale chart accepted: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAssistantChatPassesOnlyRecomputedChartContext(t *testing.T) {
	server := chartServerFixture(t.TempDir())
	service := &chartAwareAssistantStub{assistantServiceStub: &assistantServiceStub{context: AIChatContext{Facts: assistantTestFacts()}}}
	server.aiChatService = service
	analysis := chartRequest(t, server)
	data, err := json.Marshal(assistantChatRequest{Symbol: analysis.Symbol, Question: "结合当前结构怎么看", Chart: func() *assistantChartInput { value := assistantChartRequest(analysis); return &value }()})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/assistant/chat", bytes.NewReader(data)))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("chat start failed: %d %s", recorder.Code, recorder.Body.String())
	}
	var started assistantChatResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		poll := httptest.NewRecorder()
		server.Handler().ServeHTTP(poll, httptest.NewRequest(http.MethodGet, "/api/assistant/chat?job_id="+started.JobID, nil))
		var completed assistantChatResponse
		if json.Unmarshal(poll.Body.Bytes(), &completed) == nil && completed.Status == "completed" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	service.chartMu.Lock()
	received := cloneAssistantChartContext(service.received)
	service.chartMu.Unlock()
	if received == nil || received.Analysis.Fingerprint != analysis.Fingerprint || received.SelectedStructure.ID != analysis.Structures[0].ID {
		t.Fatalf("verified chart context was not supplied: %+v", received)
	}
}

func TestTradePlanReviewAPIKeepsRevisionHistory(t *testing.T) {
	root := t.TempDir()
	server := chartServerFixture(filepath.Join(root, "plans"))
	server.planReviews = storage.NewTradePlanReviewStore(filepath.Join(root, "reviews"))
	analysis := chartRequest(t, server)
	recorder := postTradePlan(server, planRequestBody(t, analysis))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("save plan: %d %s", recorder.Code, recorder.Body.String())
	}
	var saved struct {
		Plan domain.TradePlan `json:"plan"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	postReview := func(note string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(tradePlanReviewRequest{Symbol: saved.Plan.Symbol, PlanID: saved.Plan.ID, Note: note, ExecutionStatus: "watching", Tags: []string{"等待确认"}})
		request := httptest.NewRequest(http.MethodPost, "/api/trade-plan-reviews", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		result := httptest.NewRecorder()
		server.Handler().ServeHTTP(result, request)
		return result
	}
	if recorder = postReview("第一版判断"); recorder.Code != http.StatusOK {
		t.Fatalf("first review failed: %d %s", recorder.Code, recorder.Body.String())
	}
	server.now = func() time.Time { return time.Date(2026, 9, 18, 16, 1, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60)) }
	if recorder = postReview("第二版判断"); recorder.Code != http.StatusOK {
		t.Fatalf("second review failed: %d %s", recorder.Code, recorder.Body.String())
	}
	var review domain.TradePlanReview
	if err := json.Unmarshal(recorder.Body.Bytes(), &review); err != nil || review.Sequence != 2 || review.Revisions[0].Note != "第一版判断" {
		t.Fatalf("review revisions missing: %v %s", err, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/trade-plan-reviews?symbol=600519", nil))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"sequence":2`)) {
		t.Fatalf("review list failed: %d %s", recorder.Code, recorder.Body.String())
	}
}
