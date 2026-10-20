package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

type failedStockHistoryEngine struct{}

func (failedStockHistoryEngine) Run(_ context.Context, request backtest.Request) (backtest.Result, error) {
	return backtest.Result{}, fmt.Errorf("load failed: %w", &backtest.SymbolHistoryError{Symbol: request.Tickers[0], Err: fmt.Errorf("有效日K不足：55 根，当前策略至少需要 62 根")})
}

func TestStrategyBacktestReportsStockHistoryFailureWithoutSaving(t *testing.T) {
	archive := &strategyArchiveStub{}
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithStrategyResearch(failedStockHistoryEngine{}, archive))
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/strategy/backtests", strings.NewReader(`{"symbols":["sh688825"],"start":"2023-10-19","end":"2026-10-19"}`)))
	var failure strategyBacktestFailure
	if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil || w.Code != 502 {
		t.Fatalf("response: %d %s", w.Code, w.Body.String())
	}
	if failure.HistoryIssue == nil || failure.HistoryIssue.Symbol != "sh688825" || !strings.Contains(failure.HistoryIssue.Detail, "55 根") || !strings.Contains(failure.Error, "当前回测未完成") {
		t.Fatalf("missing recovery context: %+v", failure)
	}
	if archive.result.RunID != "" {
		t.Fatal("failed backtest was saved")
	}
}

func TestStrategyRequestStockPoolBoundaries(t *testing.T) {
	server := NewServer(resolverStub{}, nil, nil, nil, "")
	stocks := make([]string, 20)
	for index := range stocks {
		stocks[index] = fmt.Sprintf("sh600%03d", index)
	}
	for _, tc := range []struct {
		name      string
		symbols   []string
		want      int
		errorText string
	}{
		{"twenty-stocks", stocks, 20, ""},
		{"main-and-growth-boards", []string{"sh600519", "sh688825", "sz000001", "sz002837", "sz300250", "sz301217"}, 6, ""},
		{"empty", nil, 0, "股票池为空"},
		{"over-limit", append(append([]string{}, stocks...), "sh600999"), 0, "当前提交了 21 个标的"},
		{"shanghai-etf", []string{"sh516350"}, 0, "不是可用于"},
		{"shenzhen-etf", []string{"sz159915"}, 0, "不是可用于"},
		{"b-share", []string{"sh900901"}, 0, "不是可用于"},
		{"index", []string{"sz399006"}, 0, "不是可用于"},
		{"bond", []string{"sh113001"}, 0, "不是可用于"},
		{"sector", []string{"BK0423"}, 0, "不是可用于"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := server.strategyRequest(context.Background(), strategyBacktestInput{Symbols: tc.symbols, Start: "2023-01-01", End: "2025-12-31"})
			if tc.errorText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errorText) {
					t.Fatalf("expected %q, got %v", tc.errorText, err)
				}
				return
			}
			if err != nil || len(request.Tickers) != tc.want {
				t.Fatalf("tickers=%v error=%v", request.Tickers, err)
			}
		})
	}
}
