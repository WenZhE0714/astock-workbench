package market_test

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/market"
)

func TestFundFlowFallbackIntegration(t *testing.T) {
	if os.Getenv("ASTOCK_INTEGRATION") != "1" {
		t.Skip("set ASTOCK_INTEGRATION=1 to call public fund-flow endpoints")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
	defer cancel()
	client := market.NewFallbackFundFlowClient(market.EastmoneyClient{}, market.THSFundFlowClient{})
	flows, err := client.Fetch(ctx, []string{"sz300364", "sh600519", "sh000001", "sz399001", "sz399006"})
	if err != nil {
		t.Fatalf("incomplete fund-flow coverage: %v", err)
	}
	for _, symbol := range []string{"sz300364", "sh600519", "sh000001", "sz399001", "sz399006"} {
		flow, ok := flows[symbol]
		if !ok || flow.Source == "" || flow.FetchedAt.IsZero() || math.IsNaN(flow.MainNet) || math.IsInf(flow.MainNet, 0) {
			t.Fatalf("missing valid stock flow for %s: %+v (error=%v)", symbol, flow, err)
		}
		if !market.IsBroadMarketSymbol(symbol) && (math.IsNaN(flow.Speed) || math.IsInf(flow.Speed, 0)) {
			t.Fatalf("missing stock speed for %s", symbol)
		}
		t.Logf("symbol=%s source=%s net_yuan=%.2f data_time=%s speed=%.4f speed_source=%s speed_time=%s", symbol, flow.Source, flow.MainNet, flow.DataTime, flow.Speed, flow.SpeedSource, flow.SpeedTime)
	}
}
