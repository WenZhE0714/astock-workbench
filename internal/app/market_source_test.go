package app

import "testing"

func TestNormalizeMarketSourceAliases(t *testing.T) {
	for input, expected := range map[string]string{
		"": marketSourceHTTP, "https": marketSourceHTTP, "tdx": marketSourceTDX, "TCP": marketSourceTDX,
	} {
		if actual := normalizeMarketSource(input); actual != expected {
			t.Fatalf("normalizeMarketSource(%q) = %q, want %q", input, actual, expected)
		}
	}
	if actual := normalizeMarketSource("unknown"); actual != "" {
		t.Fatalf("unknown source normalized to %q", actual)
	}
}
