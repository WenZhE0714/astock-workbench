package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const aiChatHistoryLimit = 6

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
	state.turns = append(state.turns, domain.AIChatTurn{
		AskedAt: at, Question: state.pendingQuestion, Answer: strings.TrimSpace(answer),
	})
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
			return "AI ANSWER READY " + label + " | PRESS X TO OPEN"
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
		return "UP/DOWN SCROLL  [/] PAGE  G/G ENDPOINTS  X FOLLOW-UP  ESC BACK  Q QUIT"
	}
	return "↑/↓滚动  [/]翻页  g/G首尾  x继续追问  Esc返回  q退出"
}

func aiChatPrompt(facts domain.StockReportFacts, history []domain.AIChatTurn, question string) (string, error) {
	payload := struct {
		MarketStatus string                  `json:"market_status"`
		Facts        domain.StockReportFacts `json:"facts"`
		History      []domain.AIChatTurn     `json:"conversation_history,omitempty"`
		Question     string                  `json:"current_question"`
	}{
		MarketStatus: marketSessionAt(facts.GeneratedAt).Label,
		Facts:        facts,
		History:      history,
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
5. 资金结论必须区分当日累计main_net与fund_movement中的1/3/5分钟增量；不得把资金行为描述为确定性主力意图。
6. warnings提示数据缺失时必须降低置信度；缺失字段清洗后的0不得解释为资金持平、无风险或确定事实。
7. 板块判断应结合板块涨跌、资金量、广度和排名；新闻标题和公告索引仅作待核线索，不得补充标题之外的事实。
8. 集合竞价数据只能视为试盘和撮合线索，不能等同于开盘后的持续承接；盘中结论需说明观察周期。
9. 不得编造财务、估值、政策、产品、机构持仓或用户未提供的事实。
10. 结尾用一句话说明回答不会触发自动交易，最终决策需由用户结合风险承受能力作出。

结构化上下文JSON：
` + string(data)
	return prompt, nil
}

func (app *App) answerAIChatQuestion(
	ctx context.Context,
	symbol string,
	movement *domain.FundMovement,
	history []domain.AIChatTurn,
	question string,
	progress stockReportProgress,
) (string, error) {
	if app.marketReportAI == nil {
		return "", fmt.Errorf("Codex Agent 未初始化")
	}
	facts, err := app.collectStockReportFacts(ctx, symbol, movement, progress)
	if err != nil {
		return "", err
	}
	reportStockProgress(progress, "Codex Agent正在后台回答")
	prompt, err := aiChatPrompt(facts, history, question)
	if err != nil {
		return "", err
	}
	aiContext, cancel := context.WithTimeout(ctx, codexReportTimeout())
	defer cancel()
	answer, err := app.marketReportAI.Synthesize(aiContext, prompt)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(answer) == "" {
		return "", fmt.Errorf("Codex Agent 未返回回答")
	}
	return answer, nil
}
