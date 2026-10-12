package market

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func thsIndexFundFixture(t *testing.T, symbol string, changes map[string]any) string {
	t.Helper()
	code := thsFundIndexCode(symbol)
	marketID := "32"
	if symbol == "sh000001" {
		marketID = "16"
	}
	items := map[string]any{
		"5": code, "marketid": marketID, "name": "测试指数", "updateTime": "2026-10-12 15:00",
		"223": "20", "224": "5", "225": "10", "226": "15", "19": "100",
		"10": "3797.24", "199112": "-0.43",
	}
	for key, value := range changes {
		items[key] = value
	}
	data, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}
	return "quotebridge_v6_realhead_hs_" + code + "_last(" + string(data) + ")"
}

func TestTHSIndexFundFlowUsesCorrectMarketCodeUnitsAndTime(t *testing.T) {
	for _, symbol := range []string{"sh000001", "sz399001", "sz399006"} {
		flow, err := ParseTHSIndexFundFlowPayload(thsIndexFundFixture(t, symbol, nil), symbol)
		if err != nil {
			t.Fatal(err)
		}
		if flow.Symbol != symbol || flow.MainNet != 10 || flow.MainRatio != 10 || flow.Source != THSFundFlowSource || flow.DataTime != "2026-10-12 15:00" || flow.Price != 3797.24 || flow.Percent != -0.43 || !math.IsNaN(flow.Speed) {
			t.Fatalf("incorrect index snapshot: %+v", flow)
		}
	}
}

func TestTHSIndexFundFlowRejectsInvalidAndMismatchedData(t *testing.T) {
	for _, changes := range []map[string]any{
		{"5": "000001"}, {"marketid": "33"}, {"updateTime": ""}, {"updateTime": "bad"},
		{"223": nil}, {"224": "--"}, {"225": "Inf"}, {"226": -1},
	} {
		if _, err := ParseTHSIndexFundFlowPayload(thsIndexFundFixture(t, "sh000001", changes), "sh000001"); err == nil {
			t.Fatalf("accepted invalid index data: %v", changes)
		}
	}
	raw := thsIndexFundFixture(t, "sh000001", nil)
	if _, err := ParseTHSIndexFundFlowPayload(strings.ReplaceAll(raw, "hs_1A0001", "hs_000001"), "sh000001"); err == nil {
		t.Fatal("accepted wrong callback")
	}
	if _, err := ParseTHSIndexFundFlowPayload(raw, "sz000001"); err == nil {
		t.Fatal("accepted stock as index")
	}
	flow, err := ParseTHSIndexFundFlowPayload(thsIndexFundFixture(t, "sh000001", map[string]any{"19": nil}), "sh000001")
	if err != nil || flow.MainNet != 10 || !math.IsNaN(flow.MainRatio) {
		t.Fatalf("missing ratio should not erase net: %+v %v", flow, err)
	}
}

func thsMinuteSpeedFixture(date, records string) string {
	return `quotebridge_v6_time_hs_300364_last({"hs_300364":{"date":"` + date + `","data":"` + records + `"}})`
}

func TestTHSMinuteSpeedComputesConsecutiveMinuteReturn(t *testing.T) {
	for _, test := range []struct {
		name, records string
		want          float64
		clock         string
	}{
		{"rising", "0930,10;0931,10.2", 2, "09:31"},
		{"falling", "0930,10;0931,9.9", -1, "09:31"},
		{"flat", "0930,10;0931,10", 0, "09:31"},
		{"unsorted", "0932,10.3;0930,9;0931,10", 3, "09:32"},
		{"ignore-after-hours", "1459,10;1500,10.1;1529,20;1530,20", 1, "15:00"},
	} {
		t.Run(test.name, func(t *testing.T) {
			speed, sampledAt, err := ParseTHSMinuteSpeed(thsMinuteSpeedFixture("20261012", test.records), "sz300364")
			if err != nil || math.Abs(speed-test.want) > 1e-9 || sampledAt != "2026-10-12 "+test.clock {
				t.Fatalf("incorrect minute speed: speed=%f time=%s err=%v", speed, sampledAt, err)
			}
		})
	}
}

func TestTHSMinuteSpeedRejectsGapsLunchAndInvalidLatestPrice(t *testing.T) {
	for _, records := range []string{"", "0930,10", "0930,10;0932,11", "1130,10;1300,11", "0929,10;0930,11", "1458,10;1459,11;1500,--", "1459,0;1500,10"} {
		if _, _, err := ParseTHSMinuteSpeed(thsMinuteSpeedFixture("20261012", records), "sz300364"); err == nil {
			t.Fatalf("accepted invalid minute history: %s", records)
		}
	}
	if _, _, err := ParseTHSMinuteSpeed(thsMinuteSpeedFixture("invalid", "0930,10;0931,11"), "sz300364"); err == nil {
		t.Fatal("accepted invalid date")
	}
}
