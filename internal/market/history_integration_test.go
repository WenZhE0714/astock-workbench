package market_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

func TestDailyHistoryAndTechnicalSignalIntegration(t *testing.T) {
	if os.Getenv("ASTOCK_INTEGRATION") != "1" {
		t.Skip("set ASTOCK_INTEGRATION=1 to call public market-data endpoints")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	bars, err := (market.EastmoneyClient{}).FetchDailyBars(ctx, "sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if len(bars) < 60 || bars[len(bars)-1].Source == "" {
		t.Fatalf("unexpected history: count=%d latest=%#v", len(bars), bars[len(bars)-1])
	}
	signal, err := strategy.AnalyzeTechnical("sh600519", bars)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("source=%s bars=%d date=%s close=%.2f bias=%s action=%s strength=%d",
		signal.DataSource, len(bars), signal.DataDate, signal.Price, signal.Bias, signal.Action, signal.Strength)
}

func TestTHSStockBoardFallbackIntegration(t *testing.T) {
	if os.Getenv("ASTOCK_INTEGRATION") != "1" {
		t.Skip("set ASTOCK_INTEGRATION=1 to call public market-data endpoints")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	items, err := (market.THSStockBoardClient{}).FetchBoards(ctx, "sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Code == "" || items[0].Name == "" || items[0].Source == "" {
		t.Fatalf("unexpected Tonghuashun board fallback: %+v", items)
	}
	t.Logf("source=%s board=%s(%s) percent=%.2f main_net=%.0f", items[0].Source, items[0].Name, items[0].Code, items[0].Percent, items[0].MainNet)
}

func TestOutcomeBenchmarkHistoryIntegration(t *testing.T) {
	if os.Getenv("ASTOCK_INTEGRATION") != "1" {
		t.Skip("set ASTOCK_INTEGRATION=1 to call public market-data endpoints")
	}
	request, _ := http.NewRequest(http.MethodGet, "https://web.ifzq.gtimg.cn", nil)
	proxy, proxyErr := http.ProxyFromEnvironment(request)
	t.Logf("proxy_configured=%t proxy_error=%v", proxy != nil, proxyErr)
	for _, test := range []struct {
		name   string
		client market.DailyHistoryClient
	}{{"tencent", market.TencentClient{}}, {"eastmoney", market.EastmoneyClient{}}} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			bars, err := test.client.FetchDailyBars(ctx, "sh000300")
			if err != nil {
				t.Fatal(err)
			}
			if len(bars) < 60 {
				t.Fatalf("only %d daily bars", len(bars))
			}
			t.Logf("bars=%d latest=%s close=%.2f source=%s", len(bars), bars[len(bars)-1].Date, bars[len(bars)-1].Close, bars[len(bars)-1].Source)
		})
	}
}
