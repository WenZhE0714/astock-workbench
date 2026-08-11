package market

import (
	"context"
	"math"
	"os"
	"testing"
	"time"
)

func TestParseSinaPreviousAmountPayloadSumsLatestPriorTradingDay(t *testing.T) {
	raw := `{"result":{"status":{"code":0},"data":[` +
		`{"day":"2026-08-06 15:00:00","amount":"90000000.0000"},` +
		`{"day":"2026-08-07 09:35:00","amount":"100000000.0000"},` +
		`{"day":"2026-08-07 09:40:00","amount":"200000000.0000"},` +
		`{"day":"2026-08-10 09:35:00","amount":"400000000.0000"}]}}`
	tradeDate, amount, ok := ParseSinaPreviousAmountPayload(raw, "2026-08-10")
	if !ok || tradeDate != "2026-08-07" || amount != 30000 {
		t.Fatalf("unexpected previous amount: %s %v %v", tradeDate, amount, ok)
	}
}

func TestParseSinaPreviousAmountPayloadReturnsUnavailableWithoutPriorDay(t *testing.T) {
	tradeDate, amount, ok := ParseSinaPreviousAmountPayload(
		`{"result":{"status":{"code":0},"data":[{"day":"2026-08-10 09:35:00","amount":"100000000"}]}}`,
		"2026-08-10",
	)
	if ok || tradeDate != "" || !math.IsNaN(amount) {
		t.Fatalf("expected unavailable amount, got %s %v %v", tradeDate, amount, ok)
	}
}

func TestSinaPreviousMarketAmountIntegration(t *testing.T) {
	if os.Getenv("ASTOCK_INTEGRATION") != "1" {
		t.Skip("set ASTOCK_INTEGRATION=1 to call Sina's public market-data endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	snapshot, err := (SinaAmountClient{}).FetchPreviousMarketAmount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TradeDate == "" || snapshot.Shanghai <= 0 || snapshot.Shenzhen <= 0 || snapshot.Beijing <= 0 {
		t.Fatalf("incomplete market amount snapshot: %#v", snapshot)
	}
	t.Logf("%s 沪 %.2f亿 深 %.2f亿 京 %.2f亿 合计 %.2f亿", snapshot.TradeDate,
		snapshot.Shanghai/1e4, snapshot.Shenzhen/1e4, snapshot.Beijing/1e4,
		(snapshot.Shanghai+snapshot.Shenzhen+snapshot.Beijing)/1e4)
}
