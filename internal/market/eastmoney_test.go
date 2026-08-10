package market

import (
	"math"
	"testing"
)

func TestParseFundFlowPayload(t *testing.T) {
	raw := `{"rc":0,"data":{"diff":[{"f12":"600519","f13":1,"f62":125000000.5,"f184":3.25},{"f12":"000001","f13":0,"f62":"-6300000","f184":"-0.81"},{"f12":"000001","f13":1,"f62":22349627392,"f184":1.85},{"f12":"399006","f13":0,"f62":4331544576,"f184":0.59}]}}`
	flows := ParseFundFlowPayload(raw)
	if len(flows) != 4 {
		t.Fatalf("expected four fund-flow rows, got %d", len(flows))
	}
	if flows["sh600519"].MainNet != 125000000.5 || flows["sh600519"].MainRatio != 3.25 {
		t.Fatalf("unexpected Shanghai flow: %#v", flows["sh600519"])
	}
	if flows["sz000001"].MainNet != -6300000 || flows["sz000001"].MainRatio != -0.81 {
		t.Fatalf("unexpected Shenzhen flow: %#v", flows["sz000001"])
	}
	if flows["sh000001"].MainNet != 22349627392 || flows["sh000001"].MainRatio != 1.85 {
		t.Fatalf("unexpected Shanghai index flow: %#v", flows["sh000001"])
	}
	if flows["sz399006"].MainNet != 4331544576 || flows["sz399006"].MainRatio != 0.59 {
		t.Fatalf("unexpected ChiNext flow: %#v", flows["sz399006"])
	}
}

func TestParseFundFlowPayloadKeepsUnavailableNumber(t *testing.T) {
	flows := ParseFundFlowPayload(`{"data":{"diff":[{"f12":"600519","f13":1,"f62":null,"f184":"-"}]}}`)
	if !math.IsNaN(flows["sh600519"].MainNet) || !math.IsNaN(flows["sh600519"].MainRatio) {
		t.Fatalf("unavailable values should be NaN: %#v", flows["sh600519"])
	}
}
