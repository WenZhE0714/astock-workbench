package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type aiChatSynthesizerMock struct {
	prompt string
}

func (mock *aiChatSynthesizerMock) Synthesize(_ context.Context, prompt string) (string, error) {
	mock.prompt = prompt
	return "## 条件式结论\n\n等待关键位和资金共同确认。\n", nil
}

func TestWatchAIChatPreservesSameStockHistoryAndResetsForAnotherStock(t *testing.T) {
	state := watchAIChat{}
	state.begin("sh600519", "贵州茅台", "现在能买吗？")
	state.complete("等待放量确认。", time.Now())
	state.begin("sh600519", "贵州茅台", "什么条件确认？")
	if len(state.turns) != 1 || len(state.history()) != 1 {
		t.Fatalf("same-stock follow-up lost history: %#v", state)
	}
	state.fail(nil)
	state.begin("sz000001", "平安银行", "怎么看？")
	if len(state.turns) != 0 {
		t.Fatalf("new stock should start a new conversation: %#v", state.turns)
	}
}

func TestWatchAIChatFailureKeepsHistoryButRequiresAnotherQuestion(t *testing.T) {
	state := watchAIChat{}
	state.begin("sh600519", "贵州茅台", "第一问")
	state.complete("第一答", time.Now())
	state.begin("sh600519", "贵州茅台", "第二问")
	state.fail(fmt.Errorf("temporary failure"))
	if len(state.turns) != 1 || state.error == "" || state.generating {
		t.Fatalf("failed follow-up should retain prior history and expose retry state: %#v", state)
	}
}

func TestAIChatPromptRequiresConditionalTradingAdviceAndUsesContext(t *testing.T) {
	facts := domain.StockReportFacts{
		GeneratedAt: time.Date(2026, 8, 12, 9, 20, 0, 0, shanghaiLocation),
		Quote:       domain.StockQuoteSnapshot{Symbol: "sh600519", Name: "贵州茅台", Price: 1500},
		Warnings:    []string{"个股主力资金快照缺失"},
	}
	prompt, err := aiChatPrompt(facts, []domain.AIChatTurn{{Question: "短线怎么看", Answer: "等待确认"}}, "现在可以买入吗？")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"集合竞价", "现在可以买入吗？", "禁止给无条件买卖指令", "缺失字段清洗后的0", "conversation_history"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("AI chat prompt missing %q:\n%s", expected, prompt)
		}
	}
}

func TestAnswerAIChatQuestionCollectsLiveStockContextForAgent(t *testing.T) {
	synthesizer := &aiChatSynthesizerMock{}
	app := &App{
		quotes: stockReportQuoteMock{}, flows: stockReportFlowMock{}, history: stockReportHistoryMock{},
		boards: stockReportBoardMock{}, dragonTiger: stockReportDragonMock{}, marketScan: stockReportScanMock{},
		news: stockReportNewsMock{}, marketReportAI: synthesizer,
	}
	movement := domain.FundMovement{
		Symbol: "sh600519", Industry: "白酒", State: "持续流入",
		Delta1Minute: 1e8, Delta3Minutes: 2e8, Delta5Minutes: 3e8, IndustryNet: 5e9,
	}
	history := []domain.AIChatTurn{{Question: "短线怎么看？", Answer: "等待量价确认。"}}
	answer, err := app.answerAIChatQuestion(
		context.Background(), "sh600519", &movement, history, "现在能否买入？", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "条件式结论") {
		t.Fatalf("unexpected AI answer: %q", answer)
	}
	for _, expected := range []string{
		"现在能否买入？", "贵州茅台", "technical", "白酒", "industry_main_net_yuan",
		"delta_1m_yuan", "短线怎么看？", "等待量价确认", "新闻标题和公告索引仅作待核线索",
	} {
		if !strings.Contains(synthesizer.prompt, expected) {
			t.Fatal(fmt.Sprintf("agent prompt missing %q", expected))
		}
	}
}
