package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTHSQuantAPIConfigReadsNonSecretAndSecretFiles(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "ths-quantapi.json")
	accessFile := filepath.Join(dir, "ths-quantapi-token")
	refreshFile := filepath.Join(dir, "ths-quantapi-refresh-token")
	if err := os.WriteFile(configFile, []byte(`{"enabled":true,"access_token":"json-access","refresh_token":"json-refresh","base_url":"https://example.test/api/v1","min_request_gap_ms":250,"quote_indicators":"latest"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(accessFile, []byte(" access-token \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refreshFile, []byte("refresh-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config, access, refresh, err := LoadTHSQuantAPIConfig(configFile, accessFile, refreshFile)
	if err != nil {
		t.Fatal(err)
	}
	if !config.Enabled || config.BaseURL != "https://example.test/api/v1" || config.MinRequestGapMS != 250 || config.QuoteIndicators != "latest" {
		t.Fatalf("unexpected config: %#v", config)
	}
	if access != "json-access" || refresh != "json-refresh" {
		t.Fatalf("unexpected secrets: %q / %q", access, refresh)
	}
}

func TestLoadTHSQuantAPIConfigMissingFilesReturnsDefaults(t *testing.T) {
	config, access, refresh, err := LoadTHSQuantAPIConfig(filepath.Join(t.TempDir(), "missing.json"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if config.MinRequestGapMS != 100 || access != "" || refresh != "" {
		t.Fatalf("unexpected defaults: %#v %q %q", config, access, refresh)
	}
}
