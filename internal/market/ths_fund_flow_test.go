package market

import (
	"context"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const thsFundFlowFixture = `{"flash":[{"sr":"87596.49","name":"大单流出"},{"sr":"54109.80","name":"中单流出"},{"sr":"49019.27","name":"小单流出"},{"sr":"61938.34","name":"小单流入"},{"sr":"68481.71","name":"中单流入"},{"sr":"128018.27","name":"大单流入"}],"field":{"hyname":"文化传媒"},"title":{"zlc":190725.56,"zlr":258438.32,"je":"67712.76"}}`

func TestParseTHSFundFlowUsesLargeOrdersAndYuan(t *testing.T) {
	flow, err := ParseTHSFundFlowPayload(thsFundFlowFixture, "sz300364")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(flow.MainNet-404217800) > 0.01 || math.Abs(flow.MainRatio-40421.78/449163.88*100) > 1e-9 {
		t.Fatalf("large-order basis or units incorrect: %+v", flow)
	}
	if flow.Source != THSFundFlowSource || flow.Symbol != "sz300364" || flow.Industry != "文化传媒" {
		t.Fatalf("missing fund-flow provenance: %+v", flow)
	}
	if !math.IsNaN(flow.Price) || !math.IsNaN(flow.Percent) || !math.IsNaN(flow.Speed) || !flow.FetchedAt.IsZero() {
		t.Fatalf("parser invented unavailable quote/time data: %+v", flow)
	}
}

func TestParseTHSFundFlowUnavailableAndZero(t *testing.T) {
	for _, test := range []struct {
		name, body string
		wantError  bool
		wantNet    float64
	}{
		{name: "empty", body: `{}`, wantError: true},
		{name: "total-only", body: `{"title":{"je":"12345"}}`, wantError: true},
		{name: "missing-outflow", body: `{"flash":[{"name":"大单流入","sr":1}]}`, wantError: true},
		{name: "null", body: `{"flash":[{"name":"大单流入","sr":null},{"name":"大单流出","sr":1}]}`, wantError: true},
		{name: "invalid", body: `{"flash":[{"name":"大单流入","sr":"--"},{"name":"大单流出","sr":1}]}`, wantError: true},
		{name: "infinity", body: `{"flash":[{"name":"大单流入","sr":"Inf"},{"name":"大单流出","sr":1}]}`, wantError: true},
		{name: "overflow", body: `{"flash":[{"name":"大单流入","sr":"1e308"},{"name":"大单流出","sr":0}]}`, wantError: true},
		{name: "negative", body: `{"flash":[{"name":"大单流入","sr":-1},{"name":"大单流出","sr":1}]}`, wantError: true},
		{name: "duplicate", body: `{"flash":[{"name":"大单流入","sr":1},{"name":"大单流入","sr":2},{"name":"大单流出","sr":1}]}`, wantError: true},
		{name: "html", body: `<html>blocked</html>`, wantError: true},
		{name: "zero", body: `{"flash":[{"name":"大单流入","sr":0},{"name":"大单流出","sr":"0"}]}`},
		{name: "outflow", body: `{"flash":[{"name":"大单流入","sr":1},{"name":"大单流出","sr":3}]}`, wantNet: -20000},
	} {
		t.Run(test.name, func(t *testing.T) {
			flow, err := ParseTHSFundFlowPayload(test.body, "sz300364")
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v, expected error=%t", err, test.wantError)
			}
			if err == nil && (flow.MainNet != test.wantNet || !math.IsNaN(flow.MainRatio)) {
				t.Fatalf("incorrect net or invented missing ratio: %+v", flow)
			}
		})
	}
}

func TestTHSFundFlowFetchDoesNotConfuseStocksAndIndices(t *testing.T) {
	var mu sync.Mutex
	paths := make(map[string]int)
	client := THSFundFlowClient{BaseURL: "https://ths.test/{code}/funds", IndexURL: "https://ths.test/index/{code}", MinuteURL: "https://ths.test/minute/{code}", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		paths[r.URL.Path]++
		mu.Unlock()
		if r.Header.Get("Referer") == "" {
			t.Error("missing referer")
		}
		if strings.HasPrefix(r.URL.Path, "/index/") {
			symbol := "sh000001"
			if strings.HasSuffix(r.URL.Path, "399001") {
				symbol = "sz399001"
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(thsIndexFundFixture(t, symbol, nil)))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(thsFundFlowFixture))}, nil
	})}}
	flows, err := client.Fetch(context.Background(), []string{"sz000001", "sz300364", "sz300364", "sh000001", "sz399001", "sh510300", "sh113001", "bk0001", "bad"})
	if err == nil || len(flows) != 4 || len(paths) != 6 || paths["/000001/funds"] != 1 || paths["/index/1A0001"] != 1 || paths["/300364/funds"] != 1 {
		t.Fatalf("incorrect coverage/requests: flows=%v paths=%v err=%v", flows, paths, err)
	}
	if flows["sh000001"].MainNet == flows["sz000001"].MainNet || flows["sh000001"].DataTime == "" {
		t.Fatal("Shanghai index was confused with Ping An Bank")
	}
	for _, flow := range flows {
		if flow.FetchedAt.IsZero() {
			t.Fatal("missing retrieval time")
		}
	}
}

func TestTHSFundFlowFetchKeepsSuccessWhenAnotherStockFails(t *testing.T) {
	client := THSFundFlowClient{BaseURL: "https://ths.test/{code}", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/600519" {
			return &http.Response{StatusCode: http.StatusForbidden, Status: "403 Forbidden", Body: io.NopCloser(strings.NewReader("denied"))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(thsFundFlowFixture))}, nil
	})}}
	flows, err := client.Fetch(context.Background(), []string{"sz300364", "sh600519"})
	if err == nil || len(flows) != 1 || flows["sz300364"].Source != THSFundFlowSource {
		t.Fatalf("partial success lost: flows=%v err=%v", flows, err)
	}
}

func TestTHSFundFlowFetchHonorsCancellation(t *testing.T) {
	client := THSFundFlowClient{HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := client.Fetch(ctx, []string{"sz300364"})
	if err != context.DeadlineExceeded {
		t.Fatalf("expected context deadline, got %v", err)
	}
}

func TestTHSFundFlowFetchEnrichesSpeedWithoutLosingValidFunds(t *testing.T) {
	for _, available := range []bool{true, false} {
		client := THSFundFlowClient{
			BaseURL: "https://ths.test/funds/{code}", MinuteURL: "https://ths.test/minute/{code}",
			HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := thsFundFlowFixture
				if strings.HasPrefix(r.URL.Path, "/minute/") {
					body = thsMinuteSpeedFixture("20261012", "1459,10;1500,10.1")
					if !available {
						body = `{}`
					}
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			})},
		}
		flows, err := client.Fetch(context.Background(), []string{"sz300364"})
		flow := flows["sz300364"]
		if err != nil || math.Abs(flow.MainNet-404217800) > 0.01 {
			t.Fatalf("speed failure lost funds: %+v %v", flow, err)
		}
		if available {
			if math.Abs(flow.Speed-1) > 1e-9 || flow.SpeedSource != THSMinuteSpeedSource || flow.SpeedTime != "2026-10-12 15:00" {
				t.Fatalf("speed not enriched: %+v", flow)
			}
		} else if !math.IsNaN(flow.Speed) || flow.SpeedSource != "" {
			t.Fatalf("invented missing speed: %+v", flow)
		}
	}
}
