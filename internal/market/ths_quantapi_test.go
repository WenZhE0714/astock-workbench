package market

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestTHSQuantClientUsesAccessTokenAndParsesTables(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/real_time_quotation" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if request.Header.Get("access_token") != "access-secret" {
			t.Fatalf("access token header missing")
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["codes"] != "600519.SH" {
			t.Fatalf("unexpected codes: %#v", payload["codes"])
		}
		return &http.Response{StatusCode: http.StatusOK, Body: ioNopCloser{strings.NewReader(`{"errorcode":0,"errmsg":"success","data":{"tables":[{"thscode":"600519.SH","table":[{"latest":1500.5,"preClose":1490,"changeRatio":0.7,"open":1495,"high":1502,"low":1491,"volume":1000,"amount":120000000,"time":"2026-09-03 10:00:00"}]}]}}`)}}, nil
	})

	client := NewTHSQuantClient(THSQuantOptions{AccessToken: "access-secret", BaseURL: "http://ths.test", HTTPClient: &http.Client{Transport: transport}, MinRequestGap: 0})
	quotes, err := client.Fetch(context.Background(), []string{"sh600519"})
	if err != nil {
		t.Fatalf("fetch quote: %v", err)
	}
	if len(quotes) != 1 || quotes[0].Symbol != "sh600519" || quotes[0].Current != "1500.5" || quotes[0].Percent != 0.7 {
		t.Fatalf("unexpected quote: %#v", quotes)
	}
}

func TestParseTHSQuotesVectorTable(t *testing.T) {
	raw := []byte(`{"errorcode":0,"errmsg":"Success!","tables":[{"thscode":"600519.SH","time":["2026-09-03 16:01:17"],"table":{"latest":[1298.88],"changeRatio":[0.1063],"open":[1297.5],"high":[1305],"low":[1293.02],"preClose":[1297.5],"volume":[17747],"amount":[2305193100],"turnoverRatio":[0.1419]}}]}`)
	quotes := parseTHSQuotes(raw, []string{"sh600519"})
	if len(quotes) != 1 || quotes[0].Symbol != "sh600519" || quotes[0].Current != "1298.88" || quotes[0].QuoteTime != "2026-09-03 16:01:17" || quotes[0].Source != "同花顺QuantAPI" || quotes[0].Amount != 230519.31 || math.Abs(quotes[0].Delta-1.38) > 0.001 {
		t.Fatalf("vector table was not parsed: %#v", quotes)
	}
}

func TestTHSQuantClientRejectsMissingToken(t *testing.T) {
	client := NewTHSQuantClient(THSQuantOptions{})
	if _, err := client.Fetch(context.Background(), []string{"sh600519"}); err == nil || !strings.Contains(err.Error(), "access token") {
		t.Fatalf("expected missing token error, got %v", err)
	}
}

func TestTHSQuantDailyHistoryUsesUnadjustedHistoryQuotation(t *testing.T) {
	t.Setenv("ASTOCK_THS_HISTORY_INDICATORS", "")
	for _, configured := range []string{"", "open,close,high,low,volume,amount"} {
		t.Run(firstNonEmpty(configured, "default"), func(t *testing.T) {
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodPost || request.URL.Path != "/api/v1/cmd_history_quotation" {
					t.Fatalf("unexpected history endpoint: %s %s", request.Method, request.URL.Path)
				}
				if request.Header.Get("access_token") != "test-token" {
					t.Fatal("missing access token header")
				}
				var payload struct {
					Codes        string            `json:"codes"`
					Indicators   string            `json:"indicators"`
					StartDate    string            `json:"startdate"`
					EndDate      string            `json:"enddate"`
					FunctionPara map[string]string `json:"functionpara"`
				}
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				wantIndicators := firstNonEmpty(configured, "open,close,high,low,volume,amount,turnoverRatio")
				if payload.Codes != "300364.SZ" || payload.Indicators != wantIndicators {
					t.Fatalf("unexpected history selection: %+v", payload)
				}
				if len(payload.FunctionPara) != 3 || payload.FunctionPara["Interval"] != "D" || payload.FunctionPara["CPS"] != "1" || payload.FunctionPara["Fill"] != "Original" {
					t.Fatalf("daily history must use original, unadjusted daily bars: %v", payload.FunctionPara)
				}
				start, startErr := time.Parse("2006-01-02", payload.StartDate)
				end, endErr := time.Parse("2006-01-02", payload.EndDate)
				if startErr != nil || endErr != nil || !start.AddDate(0, 0, 500).Equal(end) {
					t.Fatalf("unexpected history date range: %q to %q", payload.StartDate, payload.EndDate)
				}
				return thsDailyHistoryResponse(t, 65), nil
			})
			client := NewTHSQuantClient(THSQuantOptions{
				AccessToken: "test-token", BaseURL: "https://ths.test/api/v1",
				HistoryIndicators: configured, HTTPClient: &http.Client{Transport: transport},
			})
			bars, err := client.FetchDailyBars(context.Background(), "sz300364")
			if err != nil {
				t.Fatal(err)
			}
			if len(bars) != 65 || bars[0].Date != "2026-01-01" || bars[64].Date != "2026-03-06" {
				t.Fatalf("unexpected history dates/count: %+v", bars)
			}
			latest := bars[len(bars)-1]
			if latest.Symbol != "sz300364" || latest.Source != "同花顺QuantAPI" || latest.Open != 24.91 || latest.Close != 25.41 || latest.High != 26.66 || latest.Low != 24.24 || latest.Volume != 175605875 || latest.Amount != 4491594376.49 || latest.Turnover != 26.56 {
				t.Fatalf("unexpected history values: %+v", latest)
			}
		})
	}
}

func TestTHSQuantDailyHistoryRejectsShortHistoryAndAPIErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		bars int
		body string
		want string
	}{
		{name: "short", bars: 59, want: "仅返回59根"},
		{name: "empty", want: "仅返回0根"},
		{name: "api-error", body: `{"errorcode":-4210,"errmsg":"invalid parameters"}`, want: "错误 -4210"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewTHSQuantClient(THSQuantOptions{
				AccessToken: "test-token", BaseURL: "https://ths.test",
				HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					if test.body != "" {
						return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.body))}, nil
					}
					return thsDailyHistoryResponse(t, test.bars), nil
				})},
			})
			bars, err := client.FetchDailyBars(context.Background(), "sz300364")
			if err == nil || !strings.Contains(err.Error(), test.want) || len(bars) != 0 {
				t.Fatalf("expected unusable history to fail: bars=%d err=%v", len(bars), err)
			}
		})
	}
}

func thsDailyHistoryResponse(t *testing.T, count int) *http.Response {
	t.Helper()
	dates := make([]string, 0, count)
	table := make(map[string][]float64)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := count - 1; i >= 0; i-- {
		dates = append(dates, start.AddDate(0, 0, i).Format("2006-01-02"))
		for key, value := range map[string]float64{"open": 24.91, "close": 25.41, "high": 26.66, "low": 24.24, "volume": 175605875, "amount": 4491594376.49, "turnoverRatio": 26.56} {
			table[key] = append(table[key], value)
		}
	}
	body, err := json.Marshal(map[string]any{
		"errorcode": 0, "errmsg": "", "tables": []any{map[string]any{"thscode": "300364.SZ", "time": dates, "table": table}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body)))}
}

func TestTHSQuantClientRequestGapIsHonored(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: ioNopCloser{strings.NewReader(`{"errorcode":0,"errmsg":"success","data":{"tables":[{"thscode":"600519.SH","table":[{"latest":1500,"preClose":1490,"open":1495,"high":1502,"low":1491}]}]}}`)}}, nil
	})
	client := NewTHSQuantClient(THSQuantOptions{AccessToken: "x", BaseURL: "http://ths.test", HTTPClient: &http.Client{Transport: transport}, MinRequestGap: 20 * time.Millisecond})
	if _, err := client.Fetch(context.Background(), []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := client.Fetch(context.Background(), []string{"sh600519"}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Fatalf("request gap was not enforced: %s", elapsed)
	}
}

func TestFallbackQuoteClientRejectsZeroPriceResponse(t *testing.T) {
	client := NewFallbackQuoteClient(
		quoteStub{quotes: []domain.Quote{{Symbol: "sh600519", Current: "0"}}},
		quoteStub{quotes: []domain.Quote{{Symbol: "sh600519", Current: "1500"}}},
	)
	quotes, err := client.Fetch(context.Background(), []string{"sh600519"})
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 1 || quotes[0].Current != "1500" {
		t.Fatalf("invalid preferred quote was not rejected: %#v", quotes)
	}
}

func TestFallbackQuoteClientEnrichesPreferredMetadata(t *testing.T) {
	client := NewFallbackQuoteClient(
		quoteStub{quotes: []domain.Quote{{Symbol: "sh600519", Source: "同花顺QuantAPI", Current: "1298.88", LimitUp: ""}}},
		quoteStub{quotes: []domain.Quote{{Symbol: "sh600519", Source: "腾讯HTTP", Name: "贵州茅台", Current: "1298.88", LimitUp: "1427.25", MarketCap: 16200}}},
	)
	quotes, err := client.Fetch(context.Background(), []string{"sh600519"})
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 1 || quotes[0].Source != "同花顺QuantAPI" || quotes[0].Name != "贵州茅台" || quotes[0].LimitUp != "1427.25" || quotes[0].MarketCap != 16200 {
		t.Fatalf("preferred quote was not enriched: %#v", quotes)
	}
}

type quoteStub struct{ quotes []domain.Quote }

func (stub quoteStub) Fetch(context.Context, []string) ([]domain.Quote, error) {
	return stub.quotes, nil
}

type ioNopCloser struct{ *strings.Reader }

func (ioNopCloser) Close() error { return nil }
