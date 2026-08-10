package market

import "testing"

func TestParseTusharePreviousAmountPayload(t *testing.T) {
	raw := `{"code":0,"data":{"fields":["ts_code","trade_date","amount"],"items":[["000001.SH","20260807",120954357],["000001.SH","20260806",110000000],["000001.SH","20260805",99000000]]}}`
	amount, ok := ParseTusharePreviousAmountPayload(raw, "000001.SH", "2026-08-07")
	if !ok || amount != 11000000 {
		t.Fatalf("unexpected Tushare amount: %v, %v", amount, ok)
	}
}
