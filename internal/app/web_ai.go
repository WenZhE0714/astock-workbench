package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/analysis"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/strategy"
	"github.com/wenzhe/astock-workbench/internal/web"
)

// webAIChatService adapts the existing CLI research flow to the Web job
// contract. Keeping the adapter here preserves the package boundary: Web
// handles HTTP and job lifecycle, while App owns market clients and Codex.
type webAIChatService struct {
	app *App
}

func (service webAIChatService) Prepare(ctx context.Context, symbol string) (web.AIChatContext, error) {
	if service.app == nil {
		return web.AIChatContext{}, fmt.Errorf("应用服务未初始化")
	}
	facts, err := service.app.collectStockReportFacts(ctx, strings.TrimSpace(symbol), nil, nil)
	if err != nil {
		return web.AIChatContext{}, err
	}
	facts = prequalifyStockReportFacts(facts)
	if facts.Quote.Symbol == "" {
		facts.Quote.Symbol = strings.TrimSpace(symbol)
	}
	setSnapshotHash(&facts)
	return web.AIChatContext{Symbol: facts.Quote.Symbol, Name: facts.Quote.Name, Facts: facts}, nil
}

func (service webAIChatService) Load(_ context.Context, symbol string) (web.AIChatConversation, error) {
	if service.app == nil || service.app.aiChats == nil {
		return web.AIChatConversation{}, fmt.Errorf("AI会话存储未初始化")
	}
	name, turns, err := service.app.aiChats.Load(strings.TrimSpace(symbol))
	if err != nil {
		return web.AIChatConversation{}, err
	}
	return web.AIChatConversation{Name: name, Turns: turns}, nil
}

func (service webAIChatService) Ask(
	ctx context.Context,
	symbol, question string,
	history []domain.AIChatTurn,
	progress func(string),
) (web.AIChatAnswer, error) {
	if service.app == nil {
		return web.AIChatAnswer{}, fmt.Errorf("应用服务未初始化")
	}
	result, err := service.app.answerAIChatQuestionDetailed(ctx, strings.TrimSpace(symbol), nil, history, question, progress)
	return web.AIChatAnswer{
		Answer: result.Answer, FactsAt: result.FactsAt, FactsHash: result.FactsHash,
		Agents: result.Agents, Fallback: result.Fallback,
	}, err
}

func (service webAIChatService) AskWithChart(
	ctx context.Context,
	symbol, question string,
	history []domain.AIChatTurn,
	chart *domain.AssistantChartContext,
	progress func(string),
) (web.AIChatAnswer, error) {
	if service.app == nil {
		return web.AIChatAnswer{}, fmt.Errorf("应用服务未初始化")
	}
	result, err := service.app.answerAIChatQuestionDetailedWithChart(ctx, strings.TrimSpace(symbol), nil, history, question, chart, progress)
	return web.AIChatAnswer{
		Answer: result.Answer, FactsAt: result.FactsAt, FactsHash: result.FactsHash,
		Agents: result.Agents, Fallback: result.Fallback,
	}, err
}

const assistantRuleProposalSchema = `{
  "type":"object",
  "properties":{
    "kind":{"type":"string","enum":["breakout","pullback"]},
    "name":{"type":"string","minLength":1,"maxLength":24},
    "description":{"type":"string","minLength":1,"maxLength":240},
    "entry_low":{"type":"number"},
    "entry_high":{"type":"number"},
    "invalidation":{"type":"number"},
    "confirmation_price":{"type":"number"},
    "volume_days":{"type":"integer","minimum":5,"maximum":60},
    "minimum_volume_ratio":{"type":"number","minimum":0.5,"maximum":5},
    "require_trend":{"type":"boolean"}
  },
  "required":["kind","name","description","entry_low","entry_high","invalidation","confirmation_price","volume_days","minimum_volume_ratio","require_trend"],
  "additionalProperties":false
}`

func (service webAIChatService) DraftRule(ctx context.Context, symbol, question, expiresOn string, chart domain.AssistantChartContext) (domain.AssistantRuleDraft, error) {
	if service.app == nil || service.app.marketReportAI == nil {
		return domain.AssistantRuleDraft{}, fmt.Errorf("AI Agent 未初始化")
	}
	structured, ok := service.app.marketReportAI.(analysis.StructuredSynthesizer)
	if !ok {
		return domain.AssistantRuleDraft{}, fmt.Errorf("当前AI通道不支持严格JSON草案")
	}
	if chart.Version != "assistant-chart-v1" || chart.Symbol != symbol || chart.Analysis.Fingerprint == "" {
		return domain.AssistantRuleDraft{}, fmt.Errorf("图表上下文未通过服务端校验")
	}
	payload := struct {
		Question string                       `json:"user_monitoring_intent"`
		Chart    domain.AssistantChartContext `json:"verified_chart_context"`
	}{Question: strings.TrimSpace(question), Chart: chart}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return domain.AssistantRuleDraft{}, err
	}
	prompt := `你是A股观察规则草案Agent。只能把用户意图映射为一个受限、可验证的日线规则，不执行交易、不创建订单、不补充上下文之外的价位。

规则只有两类：breakout（完整日K收盘突破确认价，并满足量比）或 pullback（完整日K最低进入区间且收盘守住区间下沿，可选MA20高于MA60）。
入场下沿必须不高于上沿，失效位必须低于入场下沿。所有价格须来自或紧邻已验证图表中的关键位、结构锚点或ATR区间，并保持未复权口径。若用户意图模糊，优先沿用 selected_structure 的现有计划价位。description 必须说明完整日K确认条件，不得使用“保证、必涨、机构确定吸筹”等表述。

结构化输入：
` + string(data)
	var proposal domain.AssistantRuleProposal
	if err := structured.SynthesizeJSON(ctx, prompt, []byte(assistantRuleProposalSchema), &proposal); err != nil {
		return domain.AssistantRuleDraft{}, err
	}
	return strategy.BuildAssistantRuleDraft(chart.Analysis, chart.SelectedStructure, question, expiresOn, proposal, time.Now())
}

func (service webAIChatService) Save(_ context.Context, symbol, name string, turns []domain.AIChatTurn) error {
	if service.app == nil || service.app.aiChats == nil {
		return fmt.Errorf("AI会话存储未初始化")
	}
	return service.app.aiChats.Save(strings.TrimSpace(symbol), strings.TrimSpace(name), turns)
}
