package strategy

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func trendBars(direction float64) []domain.DailyBar {
	bars := make([]domain.DailyBar, 90)
	for index := range bars {
		closePrice := 10 + direction*float64(index)*0.04
		if index%5 == 0 {
			closePrice -= direction * 0.20
		}
		bars[index] = domain.DailyBar{
			Symbol: "sz000001", Date: fmt.Sprintf("2026-%02d-%02d", 1+index/28, 1+index%28),
			Open: closePrice - direction*0.02, Close: closePrice,
			High: closePrice + 0.03, Low: closePrice - 0.03,
			Volume: 100, Amount: closePrice * 100,
		}
	}
	bars[len(bars)-1].Volume = 150
	return bars
}

func TestAnalyzeTechnicalFindsConfirmedBullishBreakout(t *testing.T) {
	signal, err := AnalyzeTechnical("sz000001", trendBars(1))
	if err != nil {
		t.Fatal(err)
	}
	if signal.Bias != "看涨" || signal.Action != "买入触发" || signal.OptionLike != "CALL-like" {
		t.Fatalf("unexpected bullish signal: %#v", signal)
	}
	if signal.VolumeRatio != 1.5 || signal.Price <= signal.High20 || signal.Score < 3 {
		t.Fatalf("bullish structure was not confirmed: %#v", signal)
	}
	if !strings.Contains(signal.BuyTrigger, "成交量") || !strings.Contains(signal.Invalidation, "看涨结构失效") {
		t.Fatalf("missing conditional plan: %#v", signal)
	}
}

func TestAnalyzeTechnicalFindsBearishBreakdown(t *testing.T) {
	signal, err := AnalyzeTechnical("sz000001", trendBars(-1))
	if err != nil {
		t.Fatal(err)
	}
	if signal.Bias != "看跌" || signal.Action != "卖出触发" || signal.OptionLike != "PUT-like" {
		t.Fatalf("unexpected bearish signal: %#v", signal)
	}
	if signal.Price >= signal.Low20 || signal.Score > -3 || !strings.Contains(signal.Invalidation, "看跌结构失效") {
		t.Fatalf("bearish structure was not confirmed: %#v", signal)
	}
}

func TestAnalyzeTechnicalUsesPriorTwentyBarsForStructure(t *testing.T) {
	bars := trendBars(0.1)
	latest := len(bars) - 1
	bars[latest].High = 99
	bars[latest].Low = 1
	signal, err := AnalyzeTechnical("sz000001", bars)
	if err != nil {
		t.Fatal(err)
	}
	if signal.High20 == 99 || signal.Low20 == 1 {
		t.Fatalf("current bar leaked into prior range: %#v", signal)
	}
}

func TestAnalyzeTechnicalRejectsInsufficientHistory(t *testing.T) {
	_, err := AnalyzeTechnical("sz000001", trendBars(1)[:60])
	if err == nil || !strings.Contains(err.Error(), "至少 65 根") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRSIFlatSeriesIsNeutral(t *testing.T) {
	values := make([]float64, 30)
	for index := range values {
		values[index] = 10
	}
	if got := rsi(values, 14); math.Abs(got-50) > 1e-9 {
		t.Fatalf("flat RSI = %v, want 50", got)
	}
}
