package ui

import (
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestDisplayPriceUsesInstrumentPrecision(t *testing.T) {
	for _, test := range []struct{ symbol, value, want string }{
		{"sh516350", "1.237", "1.237"},
		{"sh516350", "1.23", "1.230"},
		{"sh510300", "4", "4.000"},
		{"sz159915", "2.001", "2.001"},
		{"sz161725", "0.999", "0.999"},
		{"sh113000", "110.123", "110.123"},
		{"sh600519", "1500.0", "1500.00"},
		{"sz000001", "12.340", "12.34"},
		{"sh000001", "3200.123", "3200.12"},
		{"sh516350", "--", "--"},
	} {
		if got := displayPrice(test.value, test.symbol); got != test.want {
			t.Errorf("displayPrice(%q, %q) = %q; want %q", test.value, test.symbol, got, test.want)
		}
	}
}

func TestETFPrecisionInWatchListAndDetail(t *testing.T) {
	quote := domain.Quote{
		Symbol: "sh516350", Code: "516350", Name: "测试ETF", TaskName: "ETF",
		Current: "1.237", PreviousClose: "1.23", Open: "1.231", High: "1.239", Low: "1.229",
		Delta: .007, Percent: .57, AveragePrice: "1.23", LimitUp: "1.353", LimitDown: "1.107",
		Bids: []domain.DepthLevel{{Level: 1, Price: "1.236", Volume: "100"}, {Level: 2, Price: "1.235", Volume: "200"}},
		Asks: []domain.DepthLevel{{Level: 1, Price: "1.238", Volume: "100"}, {Level: 2, Price: "1.239", Volume: "200"}},
	}
	for _, width := range []int{60, 79, 120} {
		for _, moyu := range []bool{false, true} {
			frame := BuildLiveFrame(LiveData{Quotes: []domain.Quote{quote}}, ViewOptions{Moyu: moyu}, width, 24)
			if !strings.Contains(frame, "1.237") || !strings.Contains(frame, "+0.57%") {
				t.Fatalf("ETF list lost precision at width %d:\n%s", width, frame)
			}
		}
		frame := BuildLiveFrame(LiveData{Quotes: []domain.Quote{quote}, Detail: true}, ViewOptions{Depth: true}, width, 40)
		for _, expected := range []string{"1.237", "+0.007", "+0.57%", "1.230", "1.231", "1.229", "1.239", "1.236", "1.238", "1.235", "1.353", "1.107"} {
			if !strings.Contains(frame, expected) {
				t.Fatalf("ETF detail missing %s at width %d:\n%s", expected, width, frame)
			}
		}
		for _, line := range strings.Split(frame, "\n") {
			if displayWidth(line) > width {
				t.Fatalf("ETF detail exceeds width %d: %s", width, line)
			}
		}
	}
}
