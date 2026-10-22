package storage

import (
	"encoding/json"
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

func TestSaveTHSAccessTokenPreservesConfigurationAndSecretLocation(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config.json")
	token := filepath.Join(root, "token")
	if err := os.WriteFile(cfg, []byte(`{"enabled":true,"access_token":"old","refresh_token":"refresh","extra":{"keep":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTHSAccessToken(cfg, token, "old", "new"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if string(doc["access_token"]) != `"new"` || string(doc["refresh_token"]) != `"refresh"` || len(doc["extra"]) == 0 {
		t.Fatal("unrelated configuration changed")
	}
	if _, err := os.Stat(token); !os.IsNotExist(err) {
		t.Fatal("inline secret was relocated")
	}
	info, _ := os.Stat(cfg)
	if info.Mode().Perm() != 0o600 {
		t.Fatal("credential file permissions changed")
	}
	if err := SaveTHSAccessToken(cfg, token, "old", "other"); err == nil {
		t.Fatal("newer credential overwritten")
	}
	if err := SaveTHSAccessToken(cfg, token, "old", "new"); err != nil {
		t.Fatal("same server token not idempotent")
	}
}

func TestSaveTHSAccessTokenSupportsSeparateFile(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config.json")
	token := filepath.Join(root, "token")
	if err := os.WriteFile(cfg, []byte(`{"enabled":true,"refresh_token":"refresh"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTHSAccessToken(cfg, token, "", "new"); err != nil {
		t.Fatal(err)
	}
	_, access, refresh, err := LoadTHSQuantAPIConfig(cfg, token, "")
	if err != nil || access != "new" || refresh != "refresh" {
		t.Fatal("separate token not readable")
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
