package market

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestTHSSignalClientParsesNorthboundAndHotStocks(t *testing.T) {
	client := THSSignalClient{HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{"time":["09:30","10:00"],"hgt":[1.2,2.5],"sgt":[-0.4,0.3]}`
		if strings.Contains(request.URL.Path, "getharden") {
			body = `{"errocode":0,"data":[{"code":"600519","name":"贵州茅台","zhangfu":"5.2","huanshou":"3.1","chengjiaoe":"123456789","ddejingliang":"1.4","reason":"白酒+消费"}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Body: ioNopCloser{strings.NewReader(body)}}, nil
	})}, NorthboundURL: "http://test/dayChart", HotStocksURL: "http://test/getharden/date/%s"}
	northbound, err := client.FetchNorthbound(context.Background())
	if err != nil || !northbound.Available || northbound.Total != 2.8 {
		t.Fatalf("unexpected northbound: %+v %v", northbound, err)
	}
	hot, err := client.FetchHotStocks(context.Background(), "2026-09-03")
	if err != nil || !hot.Available || len(hot.Stocks) != 1 || len(hot.Themes) != 2 {
		t.Fatalf("unexpected hot snapshot: %+v %v", hot, err)
	}
	if hot.Stocks[0].Symbol != "sh600519" || hot.Stocks[0].Percent != 5.2 {
		t.Fatalf("unexpected hot stock: %+v", hot.Stocks[0])
	}
}
