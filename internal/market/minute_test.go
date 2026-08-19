package market

import (
	"math"
	"testing"
)

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
