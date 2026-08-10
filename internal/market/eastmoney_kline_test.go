package market

import "testing"

func TestParsePreviousAmountPayloadUsesPriorTradingDayAndNormalizesUnits(t *testing.T) {
	raw := `{"data":{"klines":["2026-08-05,1328.36,1306.45,1333.80,1303.50,426890000,998800000000,0,0,0,0","2026-08-06,1310,1308.55,1314.40,1300.01,254630000,271234000000,0,0,0,0","2026-08-07,1308.66,1309.22,1315.28,1301,249760000,3266919421,0,0,0,0"]}}`
	amount, ok := ParsePreviousAmountPayload(raw, "2026-08-07")
	if !ok || amount != 27123400 {
		t.Fatalf("unexpected previous amount: %v, %v", amount, ok)
	}
}

func TestParsePreviousAmountPayloadReturnsUnavailableWhenNoPriorBar(t *testing.T) {
	amount, ok := ParsePreviousAmountPayload(`{"data":{"klines":["2026-08-07,1,1,1,1,1,100000,0,0,0,0"]}}`, "2026-08-07")
	if ok || !isNaN(amount) {
		t.Fatalf("expected unavailable amount, got %v, %v", amount, ok)
	}
}

func isNaN(value float64) bool {
	return value != value
}
