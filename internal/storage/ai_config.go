package storage

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

// AIConfigRecord is the in-process form of the AI settings. Token is never
// serialized into the non-secret configuration file.
type AIConfigRecord struct {
	Config          domain.AIConfig
	Token           string
	TokenConfigured bool
	UpdatedAt       time.Time
}

type AIConfigStore struct {
	configFile string
	tokenFile  string
	mu         sync.Mutex
}

type aiConfigDocument struct {
	SchemaVersion  int       `json:"schema_version"`
	Enabled        *bool     `json:"enabled"`
	ExecutionMode  string    `json:"execution_mode"`
	Provider       string    `json:"provider"`
	Model          string    `json:"model"`
	QuickModel     string    `json:"quick_model"`
	DeepModel      string    `json:"deep_model"`
	BaseURL        string    `json:"base_url,omitempty"`
	CodexBin       string    `json:"codex_bin,omitempty"`
	CodexHome      string    `json:"codex_home,omitempty"`
	CodexProfile   string    `json:"codex_profile,omitempty"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	Reasoning      string    `json:"reasoning_effort,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

func NewAIConfigStore(configFile, tokenFile string) *AIConfigStore {
	return &AIConfigStore{configFile: strings.TrimSpace(configFile), tokenFile: strings.TrimSpace(tokenFile)}
}

func (store *AIConfigStore) Load() (AIConfigRecord, error) {
	if store == nil {
		return AIConfigRecord{Config: domain.DefaultAIConfig()}, nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	config := domain.DefaultAIConfig()
	updatedAt := time.Time{}
	if strings.TrimSpace(store.configFile) != "" {
		data, err := os.ReadFile(store.configFile)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return AIConfigRecord{}, err
		}
		if len(data) > 0 {
			var document aiConfigDocument
			if err := json.Unmarshal(data, &document); err != nil {
				return AIConfigRecord{}, err
			}
			enabled := config.Enabled
			if document.Enabled != nil {
				enabled = *document.Enabled
			}
			config = domain.AIConfig{
				SchemaVersion: document.SchemaVersion, Enabled: enabled,
				ExecutionMode: document.ExecutionMode, Provider: document.Provider,
				Model: document.Model, QuickModel: document.QuickModel, DeepModel: document.DeepModel,
				BaseURL: document.BaseURL, CodexBin: document.CodexBin, CodexHome: document.CodexHome,
				CodexProfile: document.CodexProfile, TimeoutSeconds: document.TimeoutSeconds,
				Reasoning: document.Reasoning, UpdatedAt: document.UpdatedAt,
			}
			updatedAt = document.UpdatedAt
		}
	}
	config = config.Normalized()
	if config.UpdatedAt.IsZero() {
		config.UpdatedAt = updatedAt
	}
	token := ""
	if strings.TrimSpace(store.tokenFile) != "" {
		data, err := os.ReadFile(store.tokenFile)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return AIConfigRecord{}, err
		}
		token = strings.TrimSpace(string(data))
	}
	return AIConfigRecord{Config: config, Token: token, TokenConfigured: token != "", UpdatedAt: config.UpdatedAt}, nil
}

// Save updates non-secret settings and optionally updates the separate token.
// tokenProvided distinguishes an omitted token (keep the old value) from an
// explicitly blank/cleared token.
func (store *AIConfigStore) Save(config domain.AIConfig, token string, tokenProvided, clearToken bool) error {
	if store == nil {
		return errors.New("AI 配置存储未初始化")
	}
	config = config.Normalized()
	if err := config.Validate(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	config.SchemaVersion = 1
	config.UpdatedAt = time.Now()
	document := aiConfigDocument{
		SchemaVersion: config.SchemaVersion, Enabled: &config.Enabled, ExecutionMode: config.ExecutionMode,
		Provider: config.Provider, Model: config.Model, QuickModel: config.QuickModel, DeepModel: config.DeepModel,
		BaseURL: config.BaseURL, CodexBin: config.CodexBin, CodexHome: config.CodexHome, CodexProfile: config.CodexProfile,
		TimeoutSeconds: config.TimeoutSeconds, Reasoning: config.Reasoning, UpdatedAt: config.UpdatedAt,
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(store.configFile) != "" {
		if err := atomicWrite(store.configFile, append(data, '\n'), 0o600); err != nil {
			return err
		}
	}
	if clearToken {
		if strings.TrimSpace(store.tokenFile) != "" {
			if err := os.Remove(store.tokenFile); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	} else if tokenProvided {
		value := strings.TrimSpace(token)
		if value == "" {
			if strings.TrimSpace(store.tokenFile) != "" {
				if err := os.Remove(store.tokenFile); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		} else if strings.TrimSpace(store.tokenFile) != "" {
			if err := atomicWrite(store.tokenFile, []byte(value+"\n"), 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

func (store *AIConfigStore) Reset() error {
	if store == nil {
		return errors.New("AI 配置存储未初始化")
	}
	return store.Save(domain.DefaultAIConfig(), "", true, true)
}
