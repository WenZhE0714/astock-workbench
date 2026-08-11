package market

import (
	"math"
	"testing"
)

func TestParseDailyHistoryPayloadUsesUnadjustedFieldsAndSorts(t *testing.T) {
	raw := `{"data":{"klines":["2026-08-07,10.10,10.30,10.50,10.00,123400,1260000,4.90,2.10,0.21,3.20","bad,row","2026-08-06,9.90,10.09,10.20,9.80,100000,1005000,4.00,1.00,0.10,2.80"]}}`
	bars := ParseDailyHistoryPayload(raw, "sz000001")
	if len(bars) != 2 {
		t.Fatalf("unexpected bar count: %d", len(bars))
	}
	if bars[0].Date != "2026-08-06" || bars[1].Date != "2026-08-07" {
		t.Fatalf("bars are not chronological: %#v", bars)
	}
	latest := bars[1]
	if latest.Symbol != "sz000001" || latest.Open != 10.10 || latest.Close != 10.30 || latest.High != 10.50 ||
		latest.Low != 10.00 || latest.Volume != 123400 || latest.Amount != 1260000 || latest.Turnover != 3.20 {
		t.Fatalf("unexpected latest bar: %#v", latest)
	}
}

func TestParseTencentDailyHistoryPayloadUsesExplicitUnadjustedDaySeries(t *testing.T) {
	raw := `{"code":0,"data":{"sh600519":{"day":[["2026-08-07","1308.660","1309.220","1315.280","1301.000","24976.000"],["2026-08-10","1325.00","1348.86","1359.97","1318.08","62686",{"ignored":true}]]}}}`
	bars := ParseTencentDailyHistoryPayload(raw, "sh600519")
	if len(bars) != 2 {
		t.Fatalf("unexpected bar count: %d", len(bars))
	}
	latest := bars[1]
	if latest.Source != "腾讯" || latest.Date != "2026-08-10" || latest.Open != 1325 || latest.Close != 1348.86 ||
		latest.High != 1359.97 || latest.Low != 1318.08 || latest.Volume != 62686 || latest.Amount != 0 || !math.IsNaN(latest.Turnover) {
		t.Fatalf("unexpected Tencent bar: %#v", latest)
	}
}

func TestParseDailyHistoryPayloadKeepsBarWhenTurnoverUnavailable(t *testing.T) {
	raw := `{"data":{"klines":["2026-08-07,10,10.1,10.2,9.9,100,1000,0,0,0,-"]}}`
	bars := ParseDailyHistoryPayload(raw, "sh600519")
	if len(bars) != 1 || !math.IsNaN(bars[0].Turnover) {
		t.Fatalf("unexpected bars: %#v", bars)
	}
}
