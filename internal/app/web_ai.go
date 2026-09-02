package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
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

func (service webAIChatService) Save(_ context.Context, symbol, name string, turns []domain.AIChatTurn) error {
	if service.app == nil || service.app.aiChats == nil {
		return fmt.Errorf("AI会话存储未初始化")
	}
	return service.app.aiChats.Save(strings.TrimSpace(symbol), strings.TrimSpace(name), turns)
}
