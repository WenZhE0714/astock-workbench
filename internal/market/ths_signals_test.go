package market

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestTHSSignalClientFetchesOnlyHotStocks(t *testing.T) {
	requests := 0
	client := THSSignalClient{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Path != "/getharden/date/2026-09-03" {
			t.Fatalf("unexpected signal request: %s", request.URL.Path)
		}
		body := `{"errocode":0,"data":[{"code":"600519","name":"贵州茅台","zhangfu":"5.2","huanshou":"3.1","chengjiaoe":"123456789","ddejingliang":"1.4","reason":"白酒+消费"}]}`
		return &http.Response{StatusCode: http.StatusOK, Body: ioNopCloser{strings.NewReader(body)}}, nil
	})}, HotStocksURL: "http://test/getharden/date/%s"}
	hot, err := client.FetchHotStocks(context.Background(), "2026-09-03")
	if err != nil || !hot.Available || len(hot.Stocks) != 1 || len(hot.Themes) != 2 {
		t.Fatalf("unexpected hot snapshot: %+v %v", hot, err)
	}
	if hot.Stocks[0].Symbol != "sh600519" || hot.Stocks[0].Percent != 5.2 {
		t.Fatalf("unexpected hot stock: %+v", hot.Stocks[0])
	}
	if requests != 1 {
		t.Fatalf("unexpected signal request count: %d", requests)
	}
}

func TestHotStocksUsesHTTPSAndDecodesGBK(t *testing.T) {
	body := `{"errocode":0,"data":[{"code":"601519","name":"大智慧","reason":"互联网金融+人工智能"}]}`
	encoded, err := simplifiedchinese.GB18030.NewEncoder().String(body)
	if err != nil {
		t.Fatal(err)
	}
	client := THSSignalClient{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme != "https" || request.Header.Get("Referer") == "" {
			t.Fatalf("unexpected hotspot request: %s", request.URL)
		}
		return &http.Response{StatusCode: 200, Body: ioNopCloser{strings.NewReader(encoded)}}, nil
	})}}
	hot, err := client.FetchHotStocks(context.Background(), "2026-10-09")
	if err != nil || len(hot.Stocks) != 1 || hot.Stocks[0].Name != "大智慧" || len(hot.Themes) != 2 {
		t.Fatalf("GBK hotspot response was not decoded: %+v %v", hot, err)
	}
}

func TestHotStocksRejectsProviderErrorWithData(t *testing.T) {
	client := THSSignalClient{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: ioNopCloser{strings.NewReader(`{"errocode":1,"data":[{"code":"600519","name":"sample"}]}`)}}, nil
	})}}
	if hot, err := client.FetchHotStocks(context.Background(), "2026-10-09"); err == nil || hot.Available {
		t.Fatalf("provider error accepted: %+v %v", hot, err)
	}
}
