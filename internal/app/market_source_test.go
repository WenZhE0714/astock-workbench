package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeMarketSourceAliases(t *testing.T) {
	for input, expected := range map[string]string{
		"": marketSourceHTTP, "https": marketSourceHTTP, "tdx": marketSourceTDX, "TCP": marketSourceTDX,
		"ths": marketSourceTHS, "ifind": marketSourceTHS,
	} {
		if actual := normalizeMarketSource(input); actual != expected {
			t.Fatalf("normalizeMarketSource(%q) = %q, want %q", input, actual, expected)
		}
	}
	if actual := normalizeMarketSource("unknown"); actual != "" {
		t.Fatalf("unknown source normalized to %q", actual)
	}
}

func TestLocalTHSConfigEnablesDefaultSource(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	configDir := filepath.Join(root, "astock-workbench")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ths-quantapi.json"), []byte(`{"enabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ths-quantapi-token"), []byte("access"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := defaultWatchMarketSource(); got != marketSourceTHS {
		t.Fatalf("default source = %q, want %q", got, marketSourceTHS)
	}
}
