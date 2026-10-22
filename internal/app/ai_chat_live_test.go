package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/analysis"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

// Opt-in model evaluation uses synthetic market data and never saves a conversation.
func TestAIChatQualityLive(t *testing.T) {
	configPath := os.Getenv("ASTOCK_AI_QUALITY_CONFIG")
	if configPath == "" {
		t.Skip("live model evaluation is opt-in")
	}
	store := storage.NewAIConfigStore(configPath, os.Getenv("ASTOCK_AI_QUALITY_TOKEN_FILE"))
	runner := analysis.NewConfiguredCodexRunner(t.TempDir(), func() (domain.AIConfig, string, error) {
		record, err := store.Load()
		return record.Config, record.Token, err
	})
	app := &App{quotes: stockReportQuoteMock{}, flows: stockReportFlowMock{}, history: stockReportHistoryMock{},
		boards: stockReportBoardMock{}, dragonTiger: stockReportDragonMock{}, marketScan: stockReportScanMock{},
		news: stockReportNewsMock{}, marketReportAI: runner}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	result, err := app.answerAIChatQuestionDetailed(ctx, "sh600519", nil,
		[]domain.AIChatTurn{{Question: "未持有，计划观察一周", Answer: "此前价格777.77，旧结论不作为当前证据"}},
		"现在适合介入还是继续等？请说明最关键的依据、最强反证以及改变判断的条件。", nil)
	if directory := os.Getenv("ASTOCK_AI_QUALITY_OUTPUT"); directory != "" {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		data, marshalErr := json.MarshalIndent(result, "", "  ")
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if err := os.WriteFile(filepath.Join(directory, "result.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err != nil || result.Fallback {
		t.Fatalf("live model evaluation incomplete: %v; %s", err, researchFailureSummary(result.Agents))
	}
	t.Log(result.Answer)
}

func TestMarketChatQualityLive(t *testing.T) {
	if os.Getenv("ASTOCK_MARKET_CHAT_LIVE") != "1" {
		t.Skip("live market evaluation is opt-in")
	}
	app, err := New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	app.aiChats = storage.NewAIChatStore(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	result, err := app.answerMarketChat(ctx, nil, "现在大盘趋势怎么样，后续看涨还是看跌", func(value string) { t.Log(value) })
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Assessment == nil {
		t.Fatalf("market model did not complete: %s; %s", result.ModelError, researchFailureSummary(result.Agents))
	}
	record, err := app.aiChats.LoadMarketRecord(result.Assessment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("ASTOCK_AI_QUALITY_OUTPUT"); directory != "" {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		data, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "market-result.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log(result.Answer)
}
