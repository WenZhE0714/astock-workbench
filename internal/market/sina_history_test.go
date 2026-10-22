package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"
)

func sinaHistoryFixture(count int) string {
	var data []map[string]any
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := count - 1; i >= 0; i-- {
		data = append(data, map[string]any{"day": start.AddDate(0, 0, i).Format(time.DateOnly), "open": "20.00", "close": "21.00", "high": "22.00", "low": "19.00", "volume": "10000"})
	}
	body, _ := json.Marshal(map[string]any{"result": map[string]any{"status": map[string]int{"code": 0}, "data": data}})
	return string(body)
}

func TestSinaDailyHistoryParsesOriginalPricesAndMissingFields(t *testing.T) {
	bars, err := ParseSinaDailyHistoryPayload(sinaHistoryFixture(65), "sz300033")
	if err != nil || len(bars) != 65 {
		t.Fatalf("invalid bars: count=%d err=%v", len(bars), err)
	}
	if bars[0].Date != "2026-01-01" || bars[64].Date != "2026-03-06" || bars[64].Close != 21 || bars[64].Volume != 10000 || bars[64].Source != "新浪未复权" || bars[64].Symbol != "sz300033" {
		t.Fatal("prices, dates or source changed")
	}
	if bars[0].Amount != 0 || !math.IsNaN(bars[0].Turnover) {
		t.Fatal("missing amount or turnover was fabricated")
	}
	bad := `{"result":{"status":{"code":0},"data":[{"day":"bad","open":20,"close":21,"high":22,"low":19,"volume":10},{"day":"2026-01-01","open":20,"close":21,"high":10,"low":19,"volume":10}]}}`
	if b, err := ParseSinaDailyHistoryPayload(bad, "sz300033"); err != nil || len(b) != 0 {
		t.Fatal("malformed rows accepted")
	}
	if _, err := ParseSinaDailyHistoryPayload(`{"result":{"status":{"code":500}}}`, "sz300033"); err == nil {
		t.Fatal("provider error accepted")
	}
}

func TestSinaDailyFallbackRestoresAndCachesHistory(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	t.Setenv("ASTOCK_DAILY_HISTORY_SINA_API_URL", "https://sina.test/daily?keep=yes")
	requests := 0
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		q := r.URL.Query()
		if q.Get("symbol") != "sz300033" || q.Get("scale") != "240" || q.Get("datalen") != "300" || q.Get("ma") != "no" || q.Get("keep") != "yes" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		return thsAuthResponse(200, sinaHistoryFixture(65)), nil
	})}
	primary := &switchableHistoryMock{err: fmt.Errorf("primary blocked")}
	cached := NewCachedDailyHistoryClient(NewFallbackDailyHistoryClient(primary, SinaDailyHistoryClient{}), t.TempDir())
	bars, err := cached.FetchDailyBars(context.Background(), "sz300033")
	if err != nil || len(bars) != 65 || requests != 1 {
		t.Fatalf("fallback failed: %v", err)
	}
	httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("offline") })}
	bars, err = cached.FetchDailyBars(context.Background(), "sz300033")
	if err != nil || len(bars) != 65 {
		t.Fatalf("fresh fallback cache unavailable: %v", err)
	}
	if _, err := (SinaDailyHistoryClient{}).FetchDailyBars(context.Background(), "../../unsafe"); err == nil {
		t.Fatal("invalid symbol accepted")
	}
}

func TestTencentRejectedDailyRequestDoesNotRepeat(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	requests := 0
	httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		r := thsAuthResponse(501, "blocked")
		r.Status = "501 Not Implemented"
		return r, nil
	})}
	_, err := (TencentClient{}).FetchDailyBars(context.Background(), "sz300033")
	if err == nil || requests != 1 {
		t.Fatalf("blocked request repeated: %d %v", requests, err)
	}
}
