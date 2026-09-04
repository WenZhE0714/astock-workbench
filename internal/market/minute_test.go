package market

import (
	"context"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type minuteRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn minuteRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestEastmoneyMinuteRequestIncludesLeadingFields(t *testing.T) {
	originalClient := httpClient
	t.Cleanup(func() { httpClient = originalClient })
	httpClient = &http.Client{Transport: minuteRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		fields := request.URL.Query().Get("fields2")
		if !strings.Contains(fields, "f56") || !strings.Contains(fields, "f57") || !strings.Contains(fields, "f58") {
			t.Fatalf("minute request fields2=%q", fields)
		}
		body := `{"data":{"prePrice":10,"trends":["2026-08-17 09:30,10,10.10,10.20,9.90,100,1000,10.00"]}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	points, err := (EastmoneyMinuteClient{}).FetchMinutePoints(context.Background(), "sh000001")
	if err != nil || len(points) != 1 || points[0].Leading != 10 {
		t.Fatalf("unexpected Eastmoney minute result: points=%+v err=%v", points, err)
	}
}

type minuteClientStub struct {
	called int
	points []domain.MinutePoint
}

func (stub *minuteClientStub) FetchMinutePoints(context.Context, string) ([]domain.MinutePoint, error) {
	stub.called++
	return stub.points, nil
}

func TestMarketMinuteClientPrefersYellowLineSourceForIndices(t *testing.T) {
	primary := &minuteClientStub{points: []domain.MinutePoint{{Price: 1}}}
	fallback := &minuteClientStub{points: []domain.MinutePoint{{Price: 1, Leading: 2}}}
	client := NewMarketMinuteClient(primary, fallback)
	points, err := client.FetchMinutePoints(context.Background(), "sh000001")
	if err != nil || len(points) != 1 || points[0].Leading != 2 || fallback.called != 1 || primary.called != 0 {
		t.Fatalf("index minute source order is wrong: points=%+v err=%v primary=%d fallback=%d", points, err, primary.called, fallback.called)
	}
}

func TestMarketMinuteClientKeepsFallbackOrderForIndices(t *testing.T) {
	primary := &minuteClientStub{points: []domain.MinutePoint{{Price: 1}}}
	fallback := &minuteClientStub{points: []domain.MinutePoint{{Price: 1}}}
	client := NewMarketMinuteClient(primary, fallback)
	points, err := client.FetchMinutePoints(context.Background(), "sh000001")
	if err != nil || len(points) != 1 || fallback.called != 1 || primary.called != 1 {
		t.Fatalf("index source order is wrong: points=%+v err=%v primary=%d fallback=%d", points, err, primary.called, fallback.called)
	}
}

func TestParseTencentMinutePayloadDerivesMinuteVolumeAndAverage(t *testing.T) {
	raw := `{"code":0,"data":{"sh600519":{"data":{"date":"20260817","data":["0930 10.00 100 100000.00","0931 10.20 130 130600.00","1130 10.30 150 151200.00","1300 10.30 150 151200.00","1301 10.40 170 172000.00","1500 10.50 200 203500.00","1506 10.50 210 214000.00","bad row"]}}}}`
	points := ParseTencentMinutePayload(raw, "sh600519")
	if len(points) != 6 {
		t.Fatalf("unexpected point count %d: %+v", len(points), points)
	}
	if points[0].TradeDate != "2026-08-17" || points[0].Time != "09:30" || points[0].Volume != 100 {
		t.Fatalf("unexpected first point: %+v", points[0])
	}
	if points[1].Volume != 30 || points[1].Amount != 30600 {
		t.Fatalf("cumulative fields were not converted to minute increments: %+v", points[1])
	}
	if math.Abs(points[1].Average-10.046153846) > 0.000001 {
		t.Fatalf("unexpected average price %.9f", points[1].Average)
	}
	if points[3].Time != "13:00" || points[3].Volume != 0 {
		t.Fatalf("lunch boundary should preserve zero increment: %+v", points[3])
	}
	if points[len(points)-1].Time != "15:00" {
		t.Fatalf("post-close point was not filtered: %+v", points[len(points)-1])
	}
}

func TestParseTencentMinutePayloadRejectsInvalidRows(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"code":1}`, `{"code":0,"data":{"sh600519":{"data":{"date":"20260817","data":["0930 -- 1 1"]}}}}`} {
		if points := ParseTencentMinutePayload(raw, "sh600519"); len(points) != 0 {
			t.Fatalf("unexpected parsed points for %q: %+v", raw, points)
		}
	}
}

func TestParseEastmoneyMinutePayloadUsesProviderAverageAsYellowLine(t *testing.T) {
	raw := `{"data":{"prePrice":10,"trends":["2026-08-17 09:30,10,10.10,10.20,9.90,100,1000,10.00","2026-08-17 09:31,10.10,10.20,10.30,10.00,150,1525,10.05"]}}`
	points := ParseEastmoneyMinutePayload(raw, "sh600519")
	if len(points) != 2 {
		t.Fatalf("unexpected point count %d: %+v", len(points), points)
	}
	if points[0].Time != "09:30" || points[0].Price != 10.10 || points[0].Average != 10 || points[0].Leading != 10 {
		t.Fatalf("unexpected first point: %+v", points[0])
	}
	if points[1].Volume != 50 || points[1].Amount != 525 || points[1].Average != 10.05 {
		t.Fatalf("cumulative values or average not parsed: %+v", points[1])
	}
}

func TestParseEastmoneyMinutePayloadAcceptsClockOnlyRows(t *testing.T) {
	raw := `{"data":{"trends":["09:30,10,10.10,10.20,9.90,100,1000,10.00"]}}`
	points := ParseEastmoneyMinutePayload(raw, "sh000001")
	if len(points) != 1 || points[0].Time != "09:30" || points[0].Leading != 10 {
		t.Fatalf("clock-only Eastmoney row was dropped: %+v", points)
	}
}
