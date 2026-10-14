package market_test

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/market"
)

func TestStockScanFallbackIntegration(t *testing.T) {
	if os.Getenv("ASTOCK_INTEGRATION") != "1" {
		t.Skip("set ASTOCK_INTEGRATION=1 for public snapshots")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	symbols := []string{"sh600584", "sz001317", "sz002080", "sz002463", "sh688825", "sz300250", "sz002837", "sz002413", "sh600176", "sh601138", "sh600206", "sh600110", "sz301217", "sz002156", "sh600667", "sh600066", "sz300059", "sh603588", "sh600362", "sh688433", "sz300207", "sz000100"}
	client := market.NewFallbackMarketScanClient(market.EastmoneyClient{}, market.THSStockScanClient{Industries: market.THSStockIndustryClient{}}, market.THSMarketRankingClient{})
	items, err := client.FetchStocks(ctx, symbols)
	if err != nil || len(items) != len(symbols) {
		t.Fatalf("incomplete fallback: count=%d err=%v", len(items), err)
	}
	for _, item := range items {
		if item.Price <= 0 || math.IsNaN(item.Price) || item.Source == "" {
			t.Fatalf("invalid snapshot: %+v", item)
		}
		t.Logf("%s %s source=%s quote_time=%s price=%.2f industry=%s", item.Symbol, item.Name, item.Source, item.QuoteTime, item.Price, item.Industry)
	}
}
