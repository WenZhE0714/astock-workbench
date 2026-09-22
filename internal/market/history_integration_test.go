package market_test

import (
	"context"
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
