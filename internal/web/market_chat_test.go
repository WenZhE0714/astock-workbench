package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestAssistantMarketRouting(t *testing.T) {
	for _, test := range []struct {
		symbol, question, scope string
		market                  bool
	}{
		{"sh600519", "现在大盘趋势怎么样，后续看涨还是看跌", "", true},
		{"sh000001", "现在怎么样", "", true},
		{"sz000001", "平安银行怎么样", "", false},
		{"sh600519", "这只股票能跑赢大盘吗", "", false},
		{"sh600519", "大盘影响这只股票吗", "", false},
		{"sh600519", "大盘走势", "security", false},
		{"sh600519", "接着说", "market", true},
		{"market", "那反证呢", "auto", true},
	} {
		got, err := assistantMarketTarget(test.symbol, test.question, test.scope)
		if err != nil || got != test.market {
			t.Fatalf("%+v: got %v %v", test, got, err)
		}
	}
	if _, err := assistantMarketTarget("sh600519", "问题", "invalid"); err == nil {
		t.Fatal("unknown scope accepted")
	}
}

func TestMarketChatRoutesBeforeChartValidationAndRetainsHistory(t *testing.T) {
	turns := make([]domain.AIChatTurn, 12)
	for i := range turns {
		turns[i] = domain.AIChatTurn{Question: "旧问题", Answer: "旧答案"}
	}
	service := &assistantServiceStub{turns: turns}
	s := NewServer(assistantResolverStub{}, nil, nil, nil, "600519", WithAIChatService(service))
	r := httptest.NewRecorder()
	s.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/assistant/chat", strings.NewReader(`{"symbol":"sh600519","question":"大盘后续看涨还是看跌","chart":{"fingerprint":"unrelated-stock-chart"}}`)))
	if r.Code != http.StatusAccepted {
		t.Fatalf("market query rejected by stock chart: %d %s", r.Code, r.Body.String())
	}
	var started assistantChatResponse
	if err := json.Unmarshal(r.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started.Symbol != domain.MarketChatSymbol {
		t.Fatal("question was routed to the current stock")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		service.mu.Lock()
		n := len(service.saved)
		service.mu.Unlock()
		if n > 0 {
			if n != 13 {
				t.Fatalf("history trimmed on save: %d", n)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("answer was not saved")
}
