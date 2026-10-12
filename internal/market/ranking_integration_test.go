package market_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
)

func TestMarketRankingFallbackIntegration(t *testing.T) {
	if os.Getenv("ASTOCK_INTEGRATION") != "1" {
		t.Skip("set ASTOCK_INTEGRATION=1 to request public rankings")
	}
	client := market.NewIndustryEnrichedRankingClient(
		market.NewFallbackMarketRankingClient(market.EastmoneyClient{}, market.THSMarketRankingClient{}),
		market.THSStockIndustryClient{},
	)
	for _, kind := range []domain.MarketRankingKind{domain.MarketRankingGainers, domain.MarketRankingLosers, domain.MarketRankingRapidRise, domain.MarketRankingAmount, domain.MarketRankingTurnover} {
		t.Run(string(kind), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			items, err := client.FetchMarketRanking(ctx, kind, 20)
			if err != nil || len(items) != 20 {
				t.Fatalf("ranking unavailable: count=%d err=%v", len(items), err)
			}
			for _, item := range items {
				if item.Industry == "" || item.Industry == "--" {
					t.Errorf("missing industry for %s %s", item.Symbol, item.Name)
				}
			}
			t.Logf("kind=%s count=%d source=%s first=%s %s industry=%s industry_source=%s", kind, len(items), items[0].Source, items[0].Symbol, items[0].Name, items[0].Industry, items[0].IndustrySource)
		})
	}
}
