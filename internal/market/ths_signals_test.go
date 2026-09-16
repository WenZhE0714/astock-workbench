package market

import (
	"context"
	"net/http"
	"strings"
	"testing"
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
