package market

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestTDXBridgePreservesETFPricePrecision(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required to exercise the optional TDX bridge")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, python, "-c", `
import json, sys
scope = {"__name__": "tdx_bridge_test"}
exec(compile(sys.stdin.read(), "tdx_bridge.py", "exec"), scope)
print(json.dumps(scope["quote_item"]({
    "market": 1, "code": "516350", "price": 1.237, "last_close": 1.230,
    "open": 1.231, "high": 1.239, "low": 1.229,
    "bid1": 1.236, "ask1": 1.238, "bid5": 1.232, "ask5": 1.242,
})))
`)
	command.Stdin = strings.NewReader(tdxBridgeSource)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("bridge failed: %v: %s", err, output)
	}
	var raw tdxQuote
	if err := json.Unmarshal(output, &raw); err != nil {
		t.Fatal(err)
	}
	quote := mergeTDXQuote(raw, domain.Quote{Symbol: "sh516350", Name: "ETF"})
	if quote.Current != "1.237" || quote.PreviousClose != "1.23" || quote.Open != "1.231" || quote.High != "1.239" || quote.Low != "1.229" {
		t.Fatalf("bridge rounded ETF prices: %+v", quote)
	}
	if len(quote.Bids) != 5 || len(quote.Asks) != 5 || quote.Bids[0].Price != "1.236" || quote.Asks[0].Price != "1.238" || quote.Bids[4].Price != "1.232" || quote.Asks[4].Price != "1.242" {
		t.Fatalf("bridge rounded ETF depth: bids=%+v asks=%+v", quote.Bids, quote.Asks)
	}
	if math.Abs(quote.Delta-0.007) > 1e-10 {
		t.Fatalf("bridge changed ETF price delta: %v", quote.Delta)
	}
}

func TestMergeTDXQuoteUsesTCPPricesAndHTTPMetadata(t *testing.T) {
	metadata := domain.Quote{
		Symbol: "sz002080", Source: "腾讯HTTP", Name: "中材科技", TaskName: "中材科技",
		LimitUp: "68.20", LimitDown: "55.80", Turnover: "2.62", VolumeRatio: "1.27",
	}
	raw := tdxQuote{
		Symbol: "sz002080", Code: "002080", Current: "59.05", PreviousClose: "62.00",
		Open: "61.00", High: "63.70", Low: "58.91", QuoteTime: "2026-08-18 13:32:33",
		Delta: -2.95, Percent: -4.758, Amount: 268500, Volume: 440400,
	}
	quote := mergeTDXQuote(raw, metadata)
	if quote.Source != "通达信TCP" || quote.Current != "59.05" || quote.Name != "中材科技" {
		t.Fatalf("unexpected merged quote: %#v", quote)
	}
	if quote.LimitUp != "68.20" || quote.Turnover != "2.62" || quote.VolumeRatio != "1.27" {
		t.Fatalf("HTTP metadata was not preserved: %#v", quote)
	}
}

func TestMergeTDXQuoteRowsFallsBackForZeroAuctionPlaceholder(t *testing.T) {
	metadata := domain.Quote{
		Symbol: "sz002080", Source: "腾讯HTTP", Name: "中材科技", Current: "58.80",
		LimitUp: "59.50", LimitDown: "48.68",
	}
	rows := map[string]tdxQuote{
		"sz002080": {Symbol: "sz002080", Code: "002080", Current: "0.00", PreviousClose: "54.09"},
	}
	fallback := map[string]domain.Quote{
		"sz002080": {Symbol: "sz002080", Source: "腾讯HTTP", Name: "中材科技", Current: "55.32", PreviousClose: "54.09", Percent: 2.27},
	}
	if validTDXQuote(rows["sz002080"]) {
		t.Fatal("zero-priced TDX auction placeholder was considered valid")
	}
	if invalid := tdxFallbackSymbols([]string{"sz002080"}, rows); len(invalid) != 1 || invalid[0] != "sz002080" {
		t.Fatalf("invalid TDX quote did not request HTTP fallback: %#v", invalid)
	}
	merged := mergeTDXQuoteRows([]string{"sz002080"}, rows, map[string]domain.Quote{"sz002080": metadata}, fallback)
	if len(merged) != 1 || merged[0].Current != "55.32" || merged[0].Source != "腾讯HTTP" {
		t.Fatalf("auction fallback did not replace zero TDX quote: %#v", merged)
	}
}

func TestMergeTDXQuoteRowsKeepsValidTCPQuote(t *testing.T) {
	metadata := domain.Quote{Symbol: "sh600519", Source: "腾讯HTTP", Name: "贵州茅台", LimitUp: "1600.00"}
	rows := map[string]tdxQuote{
		"sh600519": {Symbol: "sh600519", Code: "600519", Current: "1498.50", PreviousClose: "1480.00", Percent: 1.25},
	}
	fallback := map[string]domain.Quote{
		"sh600519": {Symbol: "sh600519", Source: "腾讯HTTP", Current: "1499.00"},
	}
	merged := mergeTDXQuoteRows([]string{"sh600519"}, rows, map[string]domain.Quote{"sh600519": metadata}, fallback)
	if len(merged) != 1 || merged[0].Current != "1498.50" || merged[0].Source != "通达信TCP" || merged[0].LimitUp != "1600.00" {
		t.Fatalf("valid TCP quote should retain TCP price and HTTP metadata: %#v", merged)
	}
}

func TestValidTDXMinutePoint(t *testing.T) {
	valid := domain.MinutePoint{
		Symbol: "sz002080", Source: "通达信TCP", TradeDate: "2026-08-18",
		Time: "09:31", Price: 62.79, Average: 62.79, Volume: 26688,
	}
	if !validTDXMinutePoint(valid) {
		t.Fatalf("valid minute point rejected: %#v", valid)
	}
	valid.Time = "12:00"
	if validTDXMinutePoint(valid) {
		t.Fatalf("non-trading minute accepted: %#v", valid)
	}
}

func TestTDXFailuresBackOffIndependently(t *testing.T) {
	client := NewTDXClient(nil, nil, TDXOptions{})
	client.recordTDXError(tdxOperationMinute, errors.New("minute unavailable"))
	if client.tdxAllowed(tdxOperationMinute) {
		t.Fatal("minute operation should be in backoff")
	}
	if !client.tdxAllowed(tdxOperationQuote) || !client.tdxAllowed(tdxOperationDaily) {
		t.Fatal("minute failure must not disable quote or daily operations")
	}
}

func TestValidTDXBarRejectsBrokenPrices(t *testing.T) {
	valid := domain.DailyBar{Date: "2026-08-18", Open: 61, Close: 59.05, High: 63.7, Low: 58.91, Volume: 440400}
	if !validTDXBar(valid) {
		t.Fatalf("valid bar rejected: %#v", valid)
	}
	valid.High = 50
	if validTDXBar(valid) {
		t.Fatalf("bar with high below low accepted: %#v", valid)
	}
}
