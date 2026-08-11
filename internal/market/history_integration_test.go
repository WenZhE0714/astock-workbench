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
