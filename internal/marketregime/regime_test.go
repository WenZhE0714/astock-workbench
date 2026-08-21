package marketregime

import (
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func regimeBars(start time.Time, closes []float64) []domain.DailyBar {
	bars := make([]domain.DailyBar, len(closes))
	for index, close := range closes {
		bars[index] = domain.DailyBar{
			Date: start.AddDate(0, 0, index).Format("2006-01-02"),
			Open: close, Close: close, High: close * 1.01, Low: close * .99,
		}
	}
	return bars
}

func TestClassifyBeforeDoesNotUseFutureBars(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	closes := make([]float64, 71)
	for index := range closes {
		closes[index] = 100 + float64(index)*.5
	}
	bars := regimeBars(start, closes)
	bars[70].Close = 1
	bars[70].Open = 1
	date := start.AddDate(0, 0, 70).Format("2006-01-02")
	if got := ClassifyBefore(bars, date); got != Bull {
		t.Fatalf("future bar changed point-in-time label: %s", got)
	}
	if got := ClassifyBefore(bars, start.Format("2006-01-02")); got != Insufficient {
		t.Fatalf("expected insufficient warmup label, got %s", got)
	}
}

func TestClassifyBeforeReturnsCanonicalStates(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		closes func(int) float64
		want   Regime
	}{
		{name: "bull", closes: func(index int) float64 { return 100 + float64(index)*.5 }, want: Bull},
		{name: "bear", closes: func(index int) float64 { return 140 - float64(index)*.5 }, want: Bear},
		{name: "range", closes: func(_ int) float64 { return 100 }, want: Range},
		{name: "high volatility", closes: func(index int) float64 {
			if index%2 == 0 {
				return 100
			}
			return 104
		}, want: HighVol},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			closes := make([]float64, 70)
			for index := range closes {
				closes[index] = test.closes(index)
			}
			bars := regimeBars(start, closes)
			date := start.AddDate(0, 0, 70).Format("2006-01-02")
			if got := ClassifyBefore(bars, date); got != test.want {
				t.Fatalf("expected %s, got %s", test.want, got)
			}
		})
	}
}

func TestOrderedUsesStableResearchDisplayOrder(t *testing.T) {
	ordered := Ordered()
	if len(ordered) != 6 || ordered[0] != Bull || ordered[1] != Bear || ordered[2] != Range || ordered[3] != HighVol {
		t.Fatalf("unexpected regime order: %#v", ordered)
	}
}
