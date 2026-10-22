package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
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
	return aiChatPromptWithChart(facts, history, question, nil)
}

func aiChatPromptWithChart(facts domain.StockReportFacts, history []domain.AIChatTurn, question string, chart *domain.AssistantChartContext) (string, error) {
	return aiChatPromptForContext(newAIChatResearchContext(facts, history, question, chart))
}

type aiChatHistoryContext struct {
	AskedAt  time.Time `json:"asked_at"`
	FactsAt  time.Time `json:"facts_at,omitempty"`
	Question string    `json:"question"`
	Answer   string    `json:"answer"`
}

type aiChatPriceReference struct {
	Label    string  `json:"label"`
	Price    float64 `json:"price"`
	Distance float64 `json:"distance_from_quote_percent"`
	Position string  `json:"quote_position"`
	Session  string  `json:"session_scope"`
	DataDate string  `json:"daily_data_date"`
}

type aiChatResearchContext struct {
	MarketStatus string                        `json:"market_status"`
	Facts        domain.StockReportFacts       `json:"facts"`
	Chart        *domain.AssistantChartContext `json:"verified_chart_context,omitempty"`
	History      []aiChatHistoryContext        `json:"conversation_history_intent_only,omitempty"`
	Question     string                        `json:"current_question"`
	Prices       []aiChatPriceReference        `json:"price_references,omitempty"`
}

func newAIChatResearchContext(facts domain.StockReportFacts, history []domain.AIChatTurn, question string, chart *domain.AssistantChartContext) aiChatResearchContext {
	facts = prequalifyStockReportFacts(facts)
	if len(history) > aiChatHistoryLimit {
		history = history[len(history)-aiChatHistoryLimit:]
	}
	intentHistory := make([]aiChatHistoryContext, 0, len(history))
	for _, turn := range history {
		intentHistory = append(intentHistory, aiChatHistoryContext{
			AskedAt: turn.AskedAt, FactsAt: turn.FactsAt, Question: boundedAgentText(turn.Question, 500),
			Answer: boundedAgentText(redactHistoricalMarketValues(turn.Answer), 1200),
		})
	}
	return aiChatResearchContext{
		MarketStatus: marketSessionAt(facts.GeneratedAt).Label,
		Facts:        facts,
		Chart:        chart,
		History:      intentHistory,
		Question:     strings.TrimSpace(question),
		Prices:       aiChatPriceReferences(facts),
	}
}

// These are comparisons with completed daily indicators, not live trade signals.
func aiChatPriceReferences(facts domain.StockReportFacts) []aiChatPriceReference {
	price := facts.Quote.Price
	if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) || facts.Technical.Status != domain.TechnicalStatusReady {
		return nil
	}
	var result []aiChatPriceReference
	for _, level := range []struct {
		label string
		value float64
	}{{"MA5", facts.Technical.MA5}, {"MA20", facts.Technical.MA20}, {"MA60", facts.Technical.MA60}, {"20日高点", facts.Technical.High20}, {"20日低点", facts.Technical.Low20}} {
		if level.value <= 0 || math.IsNaN(level.value) || math.IsInf(level.value, 0) {
			continue
		}
		position := "现价低于该位"
		if price > level.value {
			position = "现价高于该位"
		} else if price == level.value {
			position = "现价等于该位"
		}
		scope := "涨跌停边界缺失，无法确认当日可达"
		if facts.PriceBoundary.Available {
			scope = "报价交易日价格区间内，不代表已收盘确认"
			if priceOutsideBoundary(level.value, facts.PriceBoundary) {
				scope = "跨交易日结构位，不可作为今日触发"
			}
		}
		result = append(result, aiChatPriceReference{Label: level.label, Price: level.value,
			Distance: math.Round((level.value/price-1)*10000) / 100, Position: position,
			Session: scope, DataDate: facts.Technical.DataDate})
	}
	sort.SliceStable(result, func(i, j int) bool { return math.Abs(result[i].Distance) < math.Abs(result[j].Distance) })
	return result
}

func aiChatPromptForContext(payload aiChatResearchContext) (string, error) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	prompt := `你是A股实时看盘咨询Agent。不要运行命令，不要读取文件，不要搜索网络，不要调用任何交易接口，只分析下面提供的结构化事实和对话历史。

硬性要求：
1. 用中文Markdown直接回答current_question，不使用表格，通常控制在800个汉字内。首句必须回答问题，不能只说“关注量价”“等待确认”“注意风险”。
2. 用户询问是否买入、卖出、持有或加减仓时，必须先给条件式结论，再列出依据、触发条件、失效条件和主要风险；禁止给无条件买卖指令。
3. 用户没有提供持仓成本、仓位和周期时，不得自行假定；必要时分别说明“未持有”和“已持有”的观察方式。
4. 关键价位只能引用facts.technical的均线、support、resistance、buy_trigger、sell_trigger、invalidation和price_references，不得创造新价位。price_references是同一行情与完整日K的确定性比较，距离是从现价到该位的百分比，不是预测收益；不能把现价已高于某位仍写成“等待首次收复”，也不能把现价触及当作完整日K确认。不同周期的条件必须分开。
4a. verified_chart_context存在时，可同时引用其中analysis.levels、selected_structure锚点和计划价位，但必须说明图表周期、分析日和结构状态；它由服务端按fingerprint核验。visible_from/visible_to只表示用户当时查看范围，不得据此假设范围之外不存在其他结构。
5. price_boundary.available为true时，它是报价交易日的硬价格边界：任何高于limit_up或低于limit_down的技术位，只能标成“跨交易日结构位/后续交易日观察位”，不得描述为当日、今日、盘中或当日收盘能够完成的买卖、突破、跌破、止盈、止损或确认条件；回答涉及关键价位时必须明确当日涨停价和跌停价。available为false时不得自行推算涨跌停价，需说明边界缺失并降低关键点位置信度。
6. 资金结论必须区分当日累计main_net与fund_movement中的1/3/5分钟增量；不得把资金行为描述为确定性主力意图。
7. warnings提示数据缺失时必须降低置信度；缺失字段清洗后的0不得解释为资金持平、无风险或确定事实。
8. 板块判断应结合板块涨跌、资金量、广度和排名；新闻标题和公告索引仅作待核线索，不得补充标题之外的事实。
9. 集合竞价数据只能视为试盘和撮合线索，不能等同于开盘后的持续承接；盘中结论需说明观察周期。
10. 不得编造财务、估值、政策、产品、机构持仓或用户未提供的事实。
11. conversation_history只用于理解用户意图和上下文；其中的旧价格、旧资金、旧方向不得作为本轮事实，除非同一值也出现在当前facts中。发生冲突时以当前facts为准并指出变化。
12. 结尾用一句话说明回答不会触发自动交易，最终决策需由用户结合风险承受能力作出。

回答质量要求：
- 综合分析或买卖问题按“当前判断 / 关键依据 / 改变判断的条件 / 反证与缺口”组织。当前判断选出证据最支持的方向（偏强、偏弱、震荡、暂无法判断），明确当前适用周期和最关键原因；观察也要说明被哪条证据阻塞。不得用同时罗列所有可能性回避判断。
- 关键依据只选最相关的2至3项，每项写“可核对的事实 → 对当前问题的影响”。用当前数值和时间或已有[E##]定位证据，不机械复述每个角色。区分已核验事实、解释和待验证假设。
- 改变判断的条件最多给两条最有用的路径，写已有价位、确认周期、当前是否满足、失效条件；如涉及价位，优先使用贴近现价的参考位及其距离。没有资金增量时不能要求用户等待一个当前无法观测的资金数值；改用可观测条件或明确需补的数据。
- 反证先写最可能推翻主判断的一项，缺口只写会实质改变结论的内容及其影响。缺龙虎榜或未核验新闻不自动否定已有量价证据，也不强迫所有问题都输出低置信度。
- 追问只回答新增问题，引用本轮事实解释哪些前提变化，避免重写整份报告。问财务、估值、公告时直接回答证据是否足够，缺财务或同行数据就说明无法判断便宜/贵及需要哪项数据，不能用技术面代替基本面结论。
- 持仓成本、仓位、周期只可采用用户明确提供的内容；历史用户陈述是当时的自述，未确认是否仍适用时标为假设。缺少这些信息时最多追问一个最影响判断的问题，先完成当前证据支持的分析。

表述示例（仅示范写法，不提供行情事实）：
差：“等待量价确认，注意风险。”
好：“短线偏弱，关键原因是现价仍低于已提供的近端均线。收盘收复该位才会改变判断；盘中短暂触及不算。若现价已经高于该位，应改为观察收盘能否守住。”实际回答须替换为本轮已有数值，不照抄示例的方向。

结构化上下文JSON：
` + string(data)
	return prompt, nil
}

var historicalMarketValuePattern = regexp.MustCompile(`[+-]?[0-9]+(?:\.[0-9]+)?\s*(?:亿元|万元|元|块|%|％)|[+-]?[0-9]+\.[0-9]+`)

func redactHistoricalMarketValues(value string) string {
	return historicalMarketValuePattern.ReplaceAllString(value, "[历史数值已省略]")
}

func renderDeterministicAIChatFallback(facts domain.StockReportFacts, question, aiError string) string {
	var builder strings.Builder
	builder.WriteString("## 条件式结论\n\n")
	if aiError != "" {
		builder.WriteString(aiError + "，以下只保留确定性数据，不据此给出买卖判断。\n\n")
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
	for index, reference := range aiChatPriceReferences(facts) {
		if index == 3 {
			break
		}
		fmt.Fprintf(&builder, "- %s %.2f 元：%s；距现价 %+.2f%%，%s（日K %s）。\n",
			reference.Label, reference.Price, reference.Position, reference.Distance, reference.Session, reference.DataDate)
	}
	if len(facts.Warnings) > 0 {
		fmt.Fprintf(&builder, "- 数据限制：%s。\n", strings.Join(facts.Warnings, "；"))
	}
	builder.WriteString("\n回答不会触发自动交易；证据恢复前只作观察，最终决策需结合持仓周期和风险承受能力。")
	return builder.String()
}

type aiChatAnswer struct {
	ModelError string
	Answer     string
	FactsAt    time.Time
	FactsHash  string
	Agents     []domain.AgentResearchRun
	Fallback   bool
	Assessment *domain.MarketChatAssessment
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
	return app.answerAIChatQuestionDetailedWithChart(ctx, symbol, movement, history, question, nil, progress)
}

func (app *App) answerAIChatQuestionDetailedWithChart(
	ctx context.Context,
	symbol string,
	movement *domain.FundMovement,
	history []domain.AIChatTurn,
	question string,
	chart *domain.AssistantChartContext,
	progress stockReportProgress,
) (aiChatAnswer, error) {
	if app.marketReportAI == nil {
		return aiChatAnswer{}, fmt.Errorf("Codex Agent 未初始化")
	}
	if symbol == domain.MarketChatSymbol || market.IsBroadMarketSymbol(symbol) {
		return app.answerMarketChat(ctx, history, question, progress)
	}
	facts, err := app.collectStockReportFacts(ctx, symbol, movement, progress)
	if err != nil {
		return aiChatAnswer{}, err
	}
	facts = prequalifyStockReportFacts(facts)
	setSnapshotHash(&facts)
	if chart != nil && chart.Symbol != facts.Quote.Symbol {
		return aiChatAnswer{}, fmt.Errorf("图表上下文与实时事实股票不一致")
	}
	researchContext := newAIChatResearchContext(facts, history, question, chart)
	prompt, err := aiChatPromptForContext(researchContext)
	if err != nil {
		return aiChatAnswer{}, err
	}
	freshness := stockFactsFreshness(facts)
	agents := app.runResearchAgents(ctx, "实时咨询", freshness, researchContext, aiChatAgentRoles, progress)
	result := aiChatAnswer{FactsAt: facts.GeneratedAt, FactsHash: facts.SnapshotHash, Agents: agents}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if successfulAgentCount(agents) == 0 {
		failure := "专业分析均未成功，尚未生成AI判断"
		result.Fallback = true
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
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		result.Fallback = true
		result.Answer = attachResearchFreshness(
			renderDeterministicAIChatFallback(facts, question, "综合回答未完成或未通过价格校验，已保留专业分析记录"), freshness, agents,
		)
		return result, nil
	}
	if strings.TrimSpace(answer) == "" {
		return result, fmt.Errorf("Codex 主Agent未返回回答")
	}
	result.Answer = attachResearchFreshness(answer, freshness, agents)
	return result, nil
}
