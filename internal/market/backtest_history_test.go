package market

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
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

func TestBacktestHistoryAddressSupportsSecurityIDProxyTemplate(t *testing.T) {
	address := backtestHistoryAddress("https://example.test/{secid}/bars", "0.000001", time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(address, "/0.000001/bars?") || strings.Contains(address, "{secid}") {
		t.Fatalf("security ID template was not expanded: %s", address)
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

func TestTencentBacktestHistoryAddressSupportsSymbolProxyTemplate(t *testing.T) {
	address := tencentBacktestHistoryAddress("https://example.test/{symbol}/bars", "sz000001", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(address, "/sz000001/bars?") || strings.Contains(address, "{symbol}") {
		t.Fatalf("symbol template was not expanded: %s", address)
	}
}

func TestTencentBacktestRangeSplitsLongWindowAndMergesBars(t *testing.T) {
	var requests atomic.Int32
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		parts := strings.Split(request.URL.Query().Get("param"), ",")
		if len(parts) != 6 {
			t.Fatalf("unexpected Tencent param: %q", request.URL.Query().Get("param"))
		}
		if parts[4] != strconv.Itoa(tencentBacktestHistoryLimit) || parts[5] != "none" {
			t.Fatalf("unexpected limit/adjustment: %q", parts)
		}
		payload := map[string]any{
			"code": 0,
			"data": map[string]any{
				parts[0]: map[string]any{"day": [][]string{
					{parts[2], "10", "10.5", "11", "9.5", "100"},
					{parts[3], "10.5", "11", "11.5", "10", "120"},
				}},
			},
		}
		if err := json.NewEncoder(writer).Encode(payload); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local test listener unavailable: %v", err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	t.Setenv("ASTOCK_BACKTEST_HISTORY_TENCENT_API_URL", "http://"+listener.Addr().String())
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)
	bars, err := fetchTencentDailyBarsRange(t.Context(), "sh600519", start, end)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() < 2 {
		t.Fatalf("long range should be split into multiple requests, got %d", requests.Load())
	}
	if len(bars) < 2 || bars[0].Date >= bars[len(bars)-1].Date {
		t.Fatalf("merged bars are not sorted: %+v", bars)
	}
	for _, bar := range bars {
		if bar.Source != "腾讯不复权" {
			t.Fatalf("unexpected source: %+v", bar)
		}
	}
}

func TestTencentBacktestRangeReturnsChunkContextOnFailure(t *testing.T) {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, fmt.Sprintf("%s", "upstream down"), http.StatusBadGateway)
	})
	listener, listenErr := net.Listen("tcp4", "127.0.0.1:0")
	if listenErr != nil {
		t.Skipf("local test listener unavailable: %v", listenErr)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	t.Setenv("ASTOCK_BACKTEST_HISTORY_TENCENT_API_URL", "http://"+listener.Addr().String())
	_, err := fetchTencentDailyBarsRange(t.Context(), "sh600519", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "腾讯回测日K分段") {
		t.Fatalf("expected contextual segment error, got %v", err)
	}
}
