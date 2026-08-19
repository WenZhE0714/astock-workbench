package app

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const (
	aiChatHistoryLimit       = 6
	aiChatStoredHistoryLimit = 100
)

type watchAIChat struct {
	generating      bool
	viewing         bool
	unread          bool
	symbol          string
	name            string
	pendingQuestion string
	progress        string
	error           string
	turns           []domain.AIChatTurn
}

func (state *watchAIChat) hydrate(symbol, name string, turns []domain.AIChatTurn) {
	state.symbol = symbol
	state.name = name
	state.turns = append([]domain.AIChatTurn(nil), turns...)
	state.generating = false
	state.viewing = false
	state.unread = false
	state.pendingQuestion = ""
	state.progress = ""
	state.error = ""
}

func (state *watchAIChat) begin(symbol, name, question string) {
	if state.symbol != symbol {
		state.turns = nil
	}
	state.generating = true
	state.viewing = false
	state.unread = false
	state.symbol = symbol
	state.name = name
	state.pendingQuestion = question
	state.progress = "采集当前股票多维数据"
	state.error = ""
}

func (state *watchAIChat) complete(answer string, at time.Time) {
	state.completeResearch(answer, at, time.Time{}, "", nil)
}

func (state *watchAIChat) completeResearch(
	answer string,
	at, factsAt time.Time,
	factsHash string,
	agents []domain.AgentResearchRun,
) {
	state.turns = append(state.turns, domain.AIChatTurn{
		AskedAt: at, FactsAt: factsAt, FactsHash: factsHash, Question: state.pendingQuestion,
		Answer: strings.TrimSpace(answer), Agents: append([]domain.AgentResearchRun(nil), agents...),
	})
	if len(state.turns) > aiChatStoredHistoryLimit {
		state.turns = append([]domain.AIChatTurn(nil), state.turns[len(state.turns)-aiChatStoredHistoryLimit:]...)
	}
	state.generating = false
	state.unread = true
	state.pendingQuestion = ""
	state.progress = ""
	state.error = ""
}

func (state *watchAIChat) fail(err error) {
	state.generating = false
	state.pendingQuestion = ""
	state.progress = ""
	state.error = ""
	if err != nil {
		state.error = err.Error()
	}
}

func (state *watchAIChat) open() bool {
	if len(state.turns) == 0 {
		return false
	}
	state.viewing = true
	state.unread = false
	state.error = ""
	return true
}

func (state *watchAIChat) close() {
	state.viewing = false
}

func (state watchAIChat) history() []domain.AIChatTurn {
	start := len(state.turns) - aiChatHistoryLimit
	if start < 0 {
		start = 0
	}
	return append([]domain.AIChatTurn(nil), state.turns[start:]...)
}

func (state watchAIChat) status(moyu bool) string {
	label := stockReportLabel(state.symbol, state.name)
	if state.generating {
		progress := strings.TrimSpace(state.progress)
		if progress == "" {
			progress = "Agent正在分析"
		}
		if moyu {
			return "AI CHAT " + label + ": " + strings.ToUpper(progress) + " | LIVE QUOTES CONTINUE"
		}
		return "AI问答处理中：" + label + " · " + progress + " · 行情继续刷新"
	}
	if state.unread {
		if moyu {
			if state.error != "" {
				return "AI ANSWER READY " + label + " | HISTORY SAVE FAILED: " + state.error + " | PRESS X TO OPEN"
			}
			return "AI ANSWER READY " + label + " | PRESS X TO OPEN"
		}
		if state.error != "" {
			return label + " AI回答已完成，但历史保存失败：" + state.error + "；按 x 查看"
		}
		return label + " AI回答已完成，按 x 查看"
	}
	if state.error != "" {
		if moyu {
			return "AI CHAT FAILED " + label + ": " + state.error
		}
		return fmt.Sprintf("%s AI问答失败：%s；按 x 重试", label, state.error)
	}
	return ""
}

func aiChatViewControls(moyu bool) string {
	if moyu {
		return "UP/DOWN SCROLL  [/] PAGE  G/G ENDPOINTS  X FOLLOW-UP  T STRATEGY  ESC BACK  Q QUIT | SAVED"
	}
	return "↑/↓滚动  [/]翻页  g/G首尾  x继续追问  t策略研究  Esc返回  q退出  · 已自动保存"
}

func aiChatPrompt(facts domain.StockReportFacts, history []domain.AIChatTurn, question string) (string, error) {
	facts = prequalifyStockReportFacts(facts)
	type historyContext struct {
		AskedAt  time.Time `json:"asked_at"`
		FactsAt  time.Time `json:"facts_at,omitempty"`
		Question string    `json:"question"`
		Answer   string    `json:"answer"`
	}
	intentHistory := make([]historyContext, 0, len(history))
	for _, turn := range history {
		intentHistory = append(intentHistory, historyContext{
			AskedAt: turn.AskedAt, FactsAt: turn.FactsAt, Question: boundedAgentText(turn.Question, 500),
			Answer: boundedAgentText(redactHistoricalMarketValues(turn.Answer), 1200),
		})
	}
	payload := struct {
		MarketStatus string                  `json:"market_status"`
		Facts        domain.StockReportFacts `json:"facts"`
		History      []historyContext        `json:"conversation_history_intent_only,omitempty"`
		Question     string                  `json:"current_question"`
	}{
		MarketStatus: marketSessionAt(facts.GeneratedAt).Label,
		Facts:        facts,
		History:      intentHistory,
		Question:     strings.TrimSpace(question),
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	prompt := `你是A股实时看盘咨询Agent。不要运行命令，不要读取文件，不要搜索网络，不要调用任何交易接口，只分析下面提供的结构化事实和对话历史。

硬性要求：
1. 用中文Markdown直接回答当前问题，不使用表格，通常控制在800个汉字内。
2. 用户询问是否买入、卖出、持有或加减仓时，必须先给条件式结论，再列出依据、触发条件、失效条件和主要风险；禁止给无条件买卖指令。
3. 用户没有提供持仓成本、仓位和周期时，不得自行假定；必要时分别说明“未持有”和“已持有”的观察方式。
4. 关键价位只能引用facts.technical中的support、resistance、buy_trigger、sell_trigger和invalidation，不得创造新价位。
5. price_boundary.available为true时，它是报价交易日的硬价格边界：任何高于limit_up或低于limit_down的技术位，只能标成“跨交易日结构位/后续交易日观察位”，不得描述为当日、今日、盘中或当日收盘能够完成的买卖、突破、跌破、止盈、止损或确认条件；回答涉及关键价位时必须明确当日涨停价和跌停价。available为false时不得自行推算涨跌停价，需说明边界缺失并降低关键点位置信度。
6. 资金结论必须区分当日累计main_net与fund_movement中的1/3/5分钟增量；不得把资金行为描述为确定性主力意图。
7. warnings提示数据缺失时必须降低置信度；缺失字段清洗后的0不得解释为资金持平、无风险或确定事实。
8. 板块判断应结合板块涨跌、资金量、广度和排名；新闻标题和公告索引仅作待核线索，不得补充标题之外的事实。
9. 集合竞价数据只能视为试盘和撮合线索，不能等同于开盘后的持续承接；盘中结论需说明观察周期。
10. 不得编造财务、估值、政策、产品、机构持仓或用户未提供的事实。
11. conversation_history只用于理解用户意图和上下文；其中的旧价格、旧资金、旧方向不得作为本轮事实，除非同一值也出现在当前facts中。发生冲突时以当前facts为准并指出变化。
12. 结尾用一句话说明回答不会触发自动交易，最终决策需由用户结合风险承受能力作出。

结构化上下文JSON：
` + string(data)
	return prompt, nil
}

var historicalMarketValuePattern = regexp.MustCompile(`[+-]?[0-9]+(?:\.[0-9]+)?\s*(?:元|块|%|％)`)

func redactHistoricalMarketValues(value string) string {
	return historicalMarketValuePattern.ReplaceAllString(value, "[历史数值已省略]")
}

func renderDeterministicAIChatFallback(facts domain.StockReportFacts, question, aiError string) string {
	var builder strings.Builder
	builder.WriteString("## 条件式结论\n\n")
	if aiError != "" {
		builder.WriteString("多角色 Agent 本轮未形成可用意见，以下只保留确定性数据，不据此给出买卖判断。\n\n")
	}
	fmt.Fprintf(&builder, "- 当前问题：%s。\n", strings.TrimSpace(question))
	fmt.Fprintf(&builder, "- 当前行情：%.2f 元，%+.2f%%；日K截至 %s，技术方向 %s。\n",
		facts.Quote.Price, facts.Quote.Percent, facts.Technical.DataDate, facts.Technical.Bias)
	if facts.PriceBoundary.Available {
		fmt.Fprintf(&builder, "- 当日价格边界：跌停 %.2f 元，涨停 %.2f 元。\n", facts.PriceBoundary.LimitDown, facts.PriceBoundary.LimitUp)
	}
	fmt.Fprintf(&builder, "- 观察条件：%s。\n- 风险/失效：%s。\n",
		qualifyTechnicalTextForBoundary(facts.Technical.BuyTrigger, facts),
		qualifyTechnicalTextForBoundary(facts.Technical.Invalidation, facts))
	if len(facts.Warnings) > 0 {
		fmt.Fprintf(&builder, "- 数据限制：%s。\n", strings.Join(facts.Warnings, "；"))
	}
	builder.WriteString("\n回答不会触发自动交易；证据恢复前只作观察，最终决策需结合持仓周期和风险承受能力。")
	return builder.String()
}

type aiChatAnswer struct {
	Answer    string
	FactsAt   time.Time
	FactsHash string
	Agents    []domain.AgentResearchRun
}

func (app *App) answerAIChatQuestion(
	ctx context.Context,
	symbol string,
	movement *domain.FundMovement,
	history []domain.AIChatTurn,
	question string,
	progress stockReportProgress,
) (string, error) {
	result, err := app.answerAIChatQuestionDetailed(ctx, symbol, movement, history, question, progress)
	return result.Answer, err
}

func (app *App) answerAIChatQuestionDetailed(
	ctx context.Context,
	symbol string,
	movement *domain.FundMovement,
	history []domain.AIChatTurn,
	question string,
	progress stockReportProgress,
) (aiChatAnswer, error) {
	if app.marketReportAI == nil {
		return aiChatAnswer{}, fmt.Errorf("Codex Agent 未初始化")
	}
	facts, err := app.collectStockReportFacts(ctx, symbol, movement, progress)
	if err != nil {
		return aiChatAnswer{}, err
	}
	facts = prequalifyStockReportFacts(facts)
	setSnapshotHash(&facts)
	prompt, err := aiChatPrompt(facts, history, question)
	if err != nil {
		return aiChatAnswer{}, err
	}
	freshness := stockFactsFreshness(facts)
	agents := app.runResearchAgents(ctx, "实时咨询", freshness, facts, aiChatAgentRoles, progress)
	result := aiChatAnswer{FactsAt: facts.GeneratedAt, FactsHash: facts.SnapshotHash, Agents: agents}
	if successfulAgentCount(agents) == 0 {
		failure := "AI咨询的多角色Agent均不可用: " + researchFailureSummary(agents)
		result.Answer = attachResearchFreshness(
			renderDeterministicAIChatFallback(facts, question, failure), freshness, agents,
		)
		return result, nil
	}
	reportStockProgress(progress, "主Agent正在结合当前问题校验角色意见")
	aiContext, cancel := context.WithTimeout(ctx, codexReportTimeout())
	defer cancel()
	answer, err := app.synthesizeWithPriceBoundary(
		aiContext, researchSupervisorPrompt(prompt, "实时咨询", freshness, agents), facts,
	)
	if err != nil {
		result.Answer = attachResearchFreshness(
			renderDeterministicAIChatFallback(facts, question, err.Error()), freshness, agents,
		)
		return result, nil
	}
	if strings.TrimSpace(answer) == "" {
		return result, fmt.Errorf("Codex 主Agent未返回回答")
	}
	result.Answer = attachResearchFreshness(answer, freshness, agents)
	return result, nil
}
