package market

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestBacktestHistoryAddressFixesUnadjustedRange(t *testing.T) {
	address := backtestHistoryAddress("https://example.test/api", "1.600519", time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC))
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	for key, expected := range map[string]string{"secid": "1.600519", "klt": "101", "fqt": "0", "beg": "20200102", "end": "20241231"} {
		if query.Get(key) != expected {
			t.Fatalf("unexpected %s=%q, want %q", key, query.Get(key), expected)
		}
	}
	if strings.Contains(address, "fqt=1") || strings.Contains(address, "fqt=2") {
		t.Fatalf("backtest must not silently mix adjusted prices: %s", address)
	}
}

func TestTencentBacktestRangeTruncationIsDetected(t *testing.T) {
	bars := make([]domain.DailyBar, tencentBacktestHistoryLimit)
	bars[0].Date = "2020-01-02"
	start := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	if !tencentBacktestRangeTruncated(bars, start) {
		t.Fatal("a full Tencent response that starts late must trigger fallback")
	}
	bars[0].Date = "2019-01-01"
	if tencentBacktestRangeTruncated(bars, start) {
		t.Fatal("a full response covering the requested start is complete")
	}
}

func TestTencentBacktestHistoryAddressUsesExplicitNoneRange(t *testing.T) {
	address := tencentBacktestHistoryAddress("https://example.test/api", "sh600519", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := parsed.Query().Get("param"), "sh600519,day,2024-01-01,2025-12-31,1000,none"; got != want {
		t.Fatalf("unexpected Tencent range param %q, want %q", got, want)
	}
}
