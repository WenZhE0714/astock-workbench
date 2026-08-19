package market

import (
	"errors"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

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
