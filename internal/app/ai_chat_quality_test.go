package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestAIChatPriceReferencesCompareCurrentQuoteWithoutConfirmingSignal(t *testing.T) {
	facts := domain.StockReportFacts{
		Quote: domain.StockQuoteSnapshot{Price: 100},
		Technical: domain.TechnicalSignal{Status: domain.TechnicalStatusReady, DataDate: "2026-10-20",
			MA5: 99, MA20: 105, MA60: 100, High20: 120, Low20: 85},
		PriceBoundary: domain.StockPriceBoundary{Available: true, LimitDown: 90, LimitUp: 110},
	}
	refs := aiChatPriceReferences(facts)
	if len(refs) != 5 || refs[0].Label != "MA60" || refs[1].Label != "MA5" {
		t.Fatalf("references must be nearest-first: %+v", refs)
	}
	for _, r := range refs {
		if r.DataDate != "2026-10-20" {
			t.Fatal("lost daily time basis")
		}
		if r.Label == "MA5" && (r.Distance != -1 || r.Position != "现价高于该位") {
			t.Fatalf("already reclaimed level misclassified: %+v", r)
		}
		if r.Label == "MA20" && (r.Distance != 5 || r.Position != "现价低于该位") {
			t.Fatalf("unreclaimed level misclassified: %+v", r)
		}
		if r.Price > 110 || r.Price < 90 {
			if !strings.Contains(r.Session, "不可作为今日触发") {
				t.Fatalf("out-of-range level actionable: %+v", r)
			}
		} else if !strings.Contains(r.Session, "不代表已收盘确认") {
			t.Fatalf("quote comparison lost confirmation caveat: %+v", r)
		}
	}
	facts.PriceBoundary.Available = false
	if !strings.Contains(aiChatPriceReferences(facts)[0].Session, "边界缺失") {
		t.Fatal("missing boundary silently treated as available")
	}
	facts.Technical.Status = domain.TechnicalStatusUnavailable
	if len(aiChatPriceReferences(facts)) != 0 {
		t.Fatal("unavailable technical data used as evidence")
	}
	facts.Technical.Status = domain.TechnicalStatusReady
	for _, price := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		facts.Quote.Price = price
		if len(aiChatPriceReferences(facts)) != 0 {
			t.Fatal("invalid quote used for distance")
		}
	}
}

type questionAwareSynthesizer struct {
	prompts        []string
	failSupervisor bool
	cancel         context.CancelFunc
}

func (s *questionAwareSynthesizer) Synthesize(_ context.Context, prompt string) (string, error) {
	s.prompts = append(s.prompts, prompt)
	if s.cancel != nil {
		s.cancel()
		return "", context.Canceled
	}
	if s.failSupervisor && strings.Contains(prompt, "多角色研究的主Agent") {
		return "", fmt.Errorf("supervisor unavailable")
	}
	return "## 当前判断\n\n短线偏强，现价144.50高于MA20；仍需完整日K确认。", nil
}

func TestAIChatEveryRoleReceivesQuestionHistoryChartAndSameSnapshot(t *testing.T) {
	synth := &questionAwareSynthesizer{}
	app := &App{quotes: stockReportQuoteMock{}, flows: stockReportFlowMock{}, history: stockReportHistoryMock{},
		boards: stockReportBoardMock{}, dragonTiger: stockReportDragonMock{}, marketScan: stockReportScanMock{},
		news: stockReportNewsMock{}, marketReportAI: synth}
	history := []domain.AIChatTurn{{Question: "未持有，计划观察一周", Answer: "前次MA20 777.77，资金1亿元，待确认"}}
	chart := &domain.AssistantChartContext{Symbol: "sh600519", Version: "assistant-chart-v1"}
	result, err := app.answerAIChatQuestionDetailedWithChart(context.Background(), "sh600519", nil, history,
		"当前判断最强的反证是什么？", chart, nil)
	if err != nil || result.Fallback || len(synth.prompts) != 4 {
		t.Fatalf("unexpected result: %+v, %v, calls=%d", result, err, len(synth.prompts))
	}
	for _, prompt := range synth.prompts {
		for _, required := range []string{"当前判断最强的反证是什么？", "未持有，计划观察一周", "verified_chart_context", "price_references", result.FactsHash} {
			if !strings.Contains(prompt, required) {
				t.Fatalf("a role lost context %q: %s", required, prompt)
			}
		}
		if strings.Contains(prompt, "777.77") || strings.Contains(prompt, "1亿元") {
			t.Fatal("historical market numbers leaked into a specialist")
		}
	}
	for _, agent := range result.Agents {
		if agent.FactsHash != result.FactsHash {
			t.Fatal("conversation context changed market snapshot identity")
		}
	}
}

func TestAIChatContextBoundsHistoryAndPreservesUserStatements(t *testing.T) {
	history := make([]domain.AIChatTurn, 9)
	for i := range history {
		history[i] = domain.AIChatTurn{Question: fmt.Sprintf("question-%d，成本63.20，仓位20%%", i), Answer: "MA20 67.94；资金2亿元；上涨3%"}
	}
	input, _ := json.Marshal(history)
	value := newAIChatResearchContext(domain.StockReportFacts{}, history, "继续", nil)
	if len(value.History) != 6 || !strings.HasPrefix(value.History[0].Question, "question-3") || !strings.Contains(value.History[0].Question, "成本63.20") {
		t.Fatalf("user context lost or unbounded: %+v", value.History)
	}
	if strings.Contains(value.History[0].Answer, "67.94") || strings.Contains(value.History[0].Answer, "2亿元") {
		t.Fatal("old assistant prices survived redaction")
	}
	after, _ := json.Marshal(history)
	if string(input) != string(after) {
		t.Fatal("stored conversation was mutated")
	}
}

func TestAIChatSupervisorFailureKeepsSuccessfulRoleEvidence(t *testing.T) {
	synth := &questionAwareSynthesizer{failSupervisor: true}
	app := &App{quotes: stockReportQuoteMock{}, flows: stockReportFlowMock{}, history: stockReportHistoryMock{},
		boards: stockReportBoardMock{}, dragonTiger: stockReportDragonMock{}, marketScan: stockReportScanMock{},
		news: stockReportNewsMock{}, marketReportAI: synth}
	result, err := app.answerAIChatQuestionDetailed(context.Background(), "sh600519", nil, nil, "能介入吗", nil)
	if err != nil || !result.Fallback || successfulAgentCount(result.Agents) != 3 || !strings.Contains(result.Answer, "综合回答未完成") {
		t.Fatalf("failed synthesis misrepresented as failed specialists: %+v, %v", result, err)
	}
}

func TestAIChatCancellationDoesNotBecomeSuccessfulFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	synth := &questionAwareSynthesizer{cancel: cancel}
	app := &App{quotes: stockReportQuoteMock{}, flows: stockReportFlowMock{}, history: stockReportHistoryMock{},
		boards: stockReportBoardMock{}, dragonTiger: stockReportDragonMock{}, marketScan: stockReportScanMock{},
		news: stockReportNewsMock{}, marketReportAI: synth}
	result, err := app.answerAIChatQuestionDetailed(ctx, "sh600519", nil, nil, "能介入吗", nil)
	if !errors.Is(err, context.Canceled) || result.Fallback || result.Answer != "" {
		t.Fatalf("cancellation saved as an answer: %+v, %v", result, err)
	}
}
