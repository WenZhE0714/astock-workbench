package market

import "testing"

func TestBoardDailyFlowFieldsAndInvalidRows(t *testing.T) {
	raw := `{"data":{"code":"BK0475","klines":["2026-10-15,200000000,1,2,3,4,12.5","2026-10-14,-100000000,1,2,3,4,-6.2"]}}`
	rows, err := ParseBoardDailyFlows(raw, "BK0475")
	if err != nil || len(rows) != 2 || rows[0].Date != "2026-10-14" || rows[1].MainNet != 2e8 || rows[1].MainRatio != 12.5 {
		t.Fatalf("wrong field mapping: %+v %v", rows, err)
	}
	for _, invalid := range []string{`{"data":null}`, `{"data":{"code":"BK0475","klines":["2026-10-15,NaN,1,2,3,4,1"]}}`, `{"data":{"code":"BK0475","klines":["2026-10-15,1,1,2,3,4,1","2026-10-15,2,1,2,3,4,1"]}}`} {
		if _, err := ParseBoardDailyFlows(invalid, "BK0475"); err == nil {
			t.Fatal("invalid day accepted")
		}
	}
	if _, err := ParseBoardDailyFlows(raw, "BK0437"); err == nil {
		t.Fatal("different board accepted")
	}
}
