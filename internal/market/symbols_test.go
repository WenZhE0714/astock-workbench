package market

import "testing"

func TestInspectSymbol(t *testing.T) {
	cases := map[string]string{
		"600519":   "sh600519",
		"000001":   "sz000001",
		"sh600519": "sh600519",
	}
	for input, expected := range cases {
		actual, status := InspectSymbol(input)
		if status != "ok" || actual != expected {
			t.Fatalf("%s: got %s/%s", input, actual, status)
		}
	}
}

func TestCandidateParsers(t *testing.T) {
	sina := ParseSinaCandidates(`var suggestvalue="贵州茅台,11,600519,sh600519,x,x,x,1,1;贵州茅台债,23,123,sh123,x,x,x,1,1";`)
	if len(sina) != 1 || sina[0].Symbol != "sh600519" {
		t.Fatalf("unexpected Sina result: %#v", sina)
	}
	tencent := ParseTencentCandidates(`v_hint="sh~600519~\u8d35\u5dde\u8305\u53f0~gzmt~GP-A^";`)
	if len(tencent) != 1 || tencent[0].Name != "贵州茅台" {
		t.Fatalf("unexpected Tencent result: %#v", tencent)
	}
}
