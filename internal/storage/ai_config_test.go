package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestAIConfigStoreSeparatesTokenAndUsesRestrictivePermissions(t *testing.T) {
	root := t.TempDir()
	configFile := filepath.Join(root, "config", "ai-config.json")
	tokenFile := filepath.Join(root, "config", "ai-token")
	store := NewAIConfigStore(configFile, tokenFile)
	config := domain.DefaultAIConfig()
	config.ExecutionMode = domain.AIExecutionAPI
	config.Provider = "deepseek"
	config.Model = "deepseek-chat"
	config.BaseURL = "http://127.0.0.1:9999/v1"
	if err := store.Save(config, "sk-test-secret", true, false); err != nil {
		t.Fatal(err)
	}
	record, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if record.Config.Provider != "deepseek" || record.Token != "sk-test-secret" || !record.TokenConfigured {
		t.Fatalf("unexpected record: %+v", record)
	}
	configData, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(configData) == "" || string(configData) == "sk-test-secret" {
		t.Fatal("configuration file unexpectedly contains the raw token")
	}
	for _, path := range []string{configFile, tokenFile} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, info.Mode().Perm())
		}
	}
	if err := store.Save(config, "", false, true); err != nil {
		t.Fatal(err)
	}
	record, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if record.TokenConfigured || record.Token != "" {
		t.Fatalf("token was not cleared: %+v", record)
	}
}

func TestAIConfigStoreRejectsInvalidBaseURL(t *testing.T) {
	store := NewAIConfigStore(filepath.Join(t.TempDir(), "config.json"), filepath.Join(t.TempDir(), "token"))
	config := domain.DefaultAIConfig()
	config.ExecutionMode = domain.AIExecutionAPI
	config.BaseURL = "file:///tmp/provider"
	if err := store.Save(config, "", false, false); err == nil {
		t.Fatal("expected invalid base URL error")
	}
}
