package domain

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	AIExecutionCodex = "codex"
	AIExecutionAPI   = "api"
)

// AIConfig contains non-secret runtime settings for the research models.
// Credentials are deliberately kept out of this value and are managed by the
// local AIConfigStore instead.
type AIConfig struct {
	SchemaVersion  int       `json:"schema_version,omitempty"`
	Enabled        bool      `json:"enabled"`
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

// AIProviderDefinition is safe to expose to the browser. It contains no
// credential material and mirrors the provider choices used by
// tradingagents-astock.
type AIProviderDefinition struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Protocol       string   `json:"protocol"`
	DefaultBaseURL string   `json:"default_base_url,omitempty"`
	TokenEnv       string   `json:"token_env,omitempty"`
	Models         []string `json:"models,omitempty"`
}

type AIProfile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Current  bool   `json:"current"`
	HasAuth  bool   `json:"has_auth"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
}

// AIConfigSnapshot is the redacted representation returned by the Web API.
// Token is intentionally absent; TokenConfigured and TokenHint are the only
// credential-related fields a browser can observe.
type AIConfigSnapshot struct {
	Config          AIConfig               `json:"config"`
	TokenConfigured bool                   `json:"token_configured"`
	TokenHint       string                 `json:"token_hint,omitempty"`
	TokenSource     string                 `json:"token_source,omitempty"`
	Providers       []AIProviderDefinition `json:"providers"`
	Profiles        []AIProfile            `json:"profiles,omitempty"`
}

type AIConfigUpdate struct {
	Config     AIConfig
	Token      *string
	ClearToken bool
}

type AIConfigTestResult struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

// DefaultAIConfig keeps the existing Codex-backed assistant as the safe
// default while exposing the same quick/deep model vocabulary as the reference
// TradingAgents project.
func DefaultAIConfig() AIConfig {
	return AIConfig{
		SchemaVersion:  1,
		Enabled:        true,
		ExecutionMode:  AIExecutionCodex,
		Provider:       "openai",
		Model:          "gpt-5.5",
		QuickModel:     "gpt-5.5",
		DeepModel:      "gpt-5.5",
		TimeoutSeconds: 600,
	}
}

func AIProviderDefinitions() []AIProviderDefinition {
	return []AIProviderDefinition{
		{ID: "minimax", Name: "MiniMax", Protocol: AIExecutionAPI, DefaultBaseURL: "https://api.minimax.chat/v1", TokenEnv: "MINIMAX_API_KEY", Models: []string{"MiniMax-M2.7", "MiniMax-M2.7-highspeed", "MiniMax-M2.5"}},
		{ID: "deepseek", Name: "DeepSeek", Protocol: AIExecutionAPI, DefaultBaseURL: "https://api.deepseek.com/v1", TokenEnv: "DEEPSEEK_API_KEY", Models: []string{"deepseek-chat", "deepseek-reasoner", "deepseek-v4-pro"}},
		{ID: "qwen", Name: "通义千问 Qwen", Protocol: AIExecutionAPI, DefaultBaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", TokenEnv: "DASHSCOPE_API_KEY", Models: []string{"qwen3.5-flash", "qwen-plus", "qwen3-max"}},
		{ID: "glm", Name: "智谱 GLM", Protocol: AIExecutionAPI, DefaultBaseURL: "https://api.z.ai/api/paas/v4", TokenEnv: "ZHIPU_API_KEY", Models: []string{"glm-5", "glm-4.7"}},
		{ID: "openai", Name: "OpenAI", Protocol: AIExecutionAPI, DefaultBaseURL: "https://api.openai.com/v1", TokenEnv: "OPENAI_API_KEY", Models: []string{"gpt-5.5", "gpt-5.4", "gpt-4.1"}},
		{ID: "anthropic", Name: "Anthropic", Protocol: AIExecutionAPI, DefaultBaseURL: "https://api.anthropic.com", TokenEnv: "ANTHROPIC_API_KEY", Models: []string{"claude-opus-4-6", "claude-sonnet-4-6", "claude-haiku-4-5"}},
		{ID: "google", Name: "Google Gemini", Protocol: AIExecutionAPI, DefaultBaseURL: "https://generativelanguage.googleapis.com/v1beta", TokenEnv: "GOOGLE_API_KEY", Models: []string{"gemini-3.1-pro-preview", "gemini-3-flash-preview", "gemini-2.5-pro"}},
		{ID: "xai", Name: "xAI Grok", Protocol: AIExecutionAPI, DefaultBaseURL: "https://api.x.ai/v1", TokenEnv: "XAI_API_KEY", Models: []string{"grok-4-0709", "grok-4-1-fast-reasoning"}},
		{ID: "openrouter", Name: "OpenRouter", Protocol: AIExecutionAPI, DefaultBaseURL: "https://openrouter.ai/api/v1", TokenEnv: "OPENROUTER_API_KEY", Models: []string{"openai/gpt-5.5", "anthropic/claude-sonnet-4.6"}},
		{ID: "ollama", Name: "Ollama（本地）", Protocol: AIExecutionAPI, DefaultBaseURL: "http://127.0.0.1:11434/v1", Models: []string{"qwen3:latest", "gpt-oss:latest", "glm-4.7-flash:latest"}},
	}
}

func FindAIProviderDefinition(provider string) (AIProviderDefinition, bool) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	for _, definition := range AIProviderDefinitions() {
		if definition.ID == provider {
			return definition, true
		}
	}
	return AIProviderDefinition{}, false
}

func AIProviderTokenEnv(provider string) string {
	definition, ok := FindAIProviderDefinition(provider)
	if !ok {
		return ""
	}
	return definition.TokenEnv
}

func (config AIConfig) Normalized() AIConfig {
	result := config
	defaults := DefaultAIConfig()
	if result.SchemaVersion == 0 {
		result.SchemaVersion = defaults.SchemaVersion
	}
	if strings.TrimSpace(result.ExecutionMode) == "" {
		result.ExecutionMode = defaults.ExecutionMode
	}
	result.ExecutionMode = strings.ToLower(strings.TrimSpace(result.ExecutionMode))
	if strings.TrimSpace(result.Provider) == "" {
		result.Provider = defaults.Provider
	}
	result.Provider = strings.ToLower(strings.TrimSpace(result.Provider))
	if strings.TrimSpace(result.Model) == "" {
		result.Model = strings.TrimSpace(result.DeepModel)
	}
	if strings.TrimSpace(result.Model) == "" {
		result.Model = strings.TrimSpace(result.QuickModel)
	}
	if strings.TrimSpace(result.Model) == "" {
		result.Model = defaults.Model
	}
	if strings.TrimSpace(result.QuickModel) == "" {
		result.QuickModel = result.Model
	}
	if strings.TrimSpace(result.DeepModel) == "" {
		result.DeepModel = result.Model
	}
	if result.TimeoutSeconds == 0 {
		result.TimeoutSeconds = defaults.TimeoutSeconds
	}
	return result
}

func (config AIConfig) Validate() error {
	config = config.Normalized()
	if config.ExecutionMode != AIExecutionCodex && config.ExecutionMode != AIExecutionAPI {
		return fmt.Errorf("不支持的 AI 运行方式 %q", config.ExecutionMode)
	}
	if config.Provider == "" {
		return fmt.Errorf("AI provider 不能为空")
	}
	if _, ok := FindAIProviderDefinition(config.Provider); !ok {
		return fmt.Errorf("不支持的 AI provider %q", config.Provider)
	}
	if strings.TrimSpace(config.Model) == "" {
		return fmt.Errorf("AI 模型不能为空")
	}
	if len([]rune(config.Model)) > 200 || len([]rune(config.QuickModel)) > 200 || len([]rune(config.DeepModel)) > 200 {
		return fmt.Errorf("AI 模型 ID 过长")
	}
	if config.TimeoutSeconds < 30 || config.TimeoutSeconds > 1800 {
		return fmt.Errorf("AI 超时必须在 30 到 1800 秒之间")
	}
	if len([]rune(config.BaseURL)) > 500 || len([]rune(config.CodexBin)) > 500 || len([]rune(config.CodexHome)) > 1000 || len([]rune(config.CodexProfile)) > 200 {
		return fmt.Errorf("AI 配置字段过长")
	}
	if strings.TrimSpace(config.BaseURL) != "" {
		parsed, err := url.Parse(strings.TrimSpace(config.BaseURL))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return fmt.Errorf("API Base URL 必须是 http(s) 地址且不能包含账号密码")
		}
	}
	return nil
}
