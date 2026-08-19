package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type partialResearchSynthesizer struct{}

func (partialResearchSynthesizer) Synthesize(_ context.Context, prompt string) (string, error) {
	if strings.Contains(prompt, "资金与板块") {
		return "", fmt.Errorf("资金角色暂不可用")
	}
	return "事实快照审计完成", nil
}

type failedResearchSynthesizer struct{}

func (failedResearchSynthesizer) Synthesize(context.Context, string) (string, error) {
	return "", fmt.Errorf("agent offline")
}

func TestRunResearchAgentsIsolatesFailureAndUsesOneSnapshot(t *testing.T) {
	facts := domain.StockReportFacts{SchemaVersion: 1, GeneratedAt: time.Now()}
	setSnapshotHash(&facts)
	app := &App{marketReportAI: partialResearchSynthesizer{}}
	runs := app.runResearchAgents(context.Background(), "测试", "当前", facts, aiChatAgentRoles, nil)
	if len(runs) != 3 || successfulAgentCount(runs) != 2 || runs[1].Status != "failed" {
		t.Fatalf("unexpected runs: %#v", runs)
	}
	for _, run := range runs {
		if run.FactsHash != facts.SnapshotHash {
			t.Fatalf("agents did not share frozen snapshot: %#v", runs)
		}
	}
}

func TestCompletedDailyBarsDropsUnfinishedIntradayBar(t *testing.T) {
	bars := []domain.DailyBar{{Date: "2026-08-12"}, {Date: "2026-08-13"}}
	at := time.Date(2026, 8, 13, 10, 0, 0, 0, shanghaiLocation)
	completed, warning := completedDailyBars(bars, at)
	if len(completed) != 1 || warning == "" {
		t.Fatalf("unfinished bar was not excluded: %#v %q", completed, warning)
	}
}

func TestAIChatAllAgentFailuresReturnAuditableFallback(t *testing.T) {
	app := &App{
		quotes: stockReportQuoteMock{}, flows: stockReportFlowMock{}, history: stockReportHistoryMock{},
		boards: stockReportBoardMock{}, dragonTiger: stockReportDragonMock{}, marketScan: stockReportScanMock{},
		news: stockReportNewsMock{}, marketReportAI: failedResearchSynthesizer{},
	}
	result, err := app.answerAIChatQuestionDetailed(context.Background(), "sh600519", nil, nil, "现在能买吗", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Agents) != len(aiChatAgentRoles) || successfulAgentCount(result.Agents) != 0 || result.FactsAt.IsZero() || result.FactsHash == "" {
		t.Fatalf("failed research audit was lost: %#v", result)
	}
	if !strings.Contains(result.Answer, "不据此给出买卖判断") || !strings.Contains(result.Answer, "多角色Agent成功0/3") {
		t.Fatalf("missing deterministic degraded answer: %s", result.Answer)
	}
}
