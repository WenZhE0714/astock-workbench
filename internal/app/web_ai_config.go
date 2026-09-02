package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/analysis"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/web"
)

type webAIConfigService struct {
	app *App
}

func (service webAIConfigService) Load(_ context.Context) (domain.AIConfigSnapshot, error) {
	record, err := service.loadRecord()
	if err != nil {
		return domain.AIConfigSnapshot{}, err
	}
	return service.snapshot(record), nil
}

func (service webAIConfigService) Save(_ context.Context, update domain.AIConfigUpdate) (domain.AIConfigSnapshot, error) {
	if service.app == nil || service.app.aiConfig == nil {
		return domain.AIConfigSnapshot{}, errors.New("AI 配置存储未初始化")
	}
	config := update.Config.Normalized()
	if err := config.Validate(); err != nil {
		return domain.AIConfigSnapshot{}, err
	}
	if err := service.app.aiConfig.Save(config, valueOrEmpty(update.Token), update.Token != nil, update.ClearToken); err != nil {
		return domain.AIConfigSnapshot{}, err
	}
	return service.Load(context.Background())
}

func (service webAIConfigService) Test(ctx context.Context, update domain.AIConfigUpdate) (domain.AIConfigTestResult, error) {
	if service.app == nil || service.app.aiConfig == nil {
		return domain.AIConfigTestResult{}, errors.New("AI 配置存储未初始化")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	config := update.Config.Normalized()
	if err := config.Validate(); err != nil {
		return domain.AIConfigTestResult{}, err
	}
	record, err := service.app.aiConfig.Load()
	if err != nil {
		return domain.AIConfigTestResult{}, err
	}
	token := record.Token
	if update.Token != nil {
		token = strings.TrimSpace(*update.Token)
	}
	if update.ClearToken {
		token = ""
	}
	// A connection test should return promptly even when a provider is
	// unreachable; normal research requests retain the configured timeout.
	testTimeout := 45 * time.Second
	if config.TimeoutSeconds > 0 && time.Duration(config.TimeoutSeconds)*time.Second < testTimeout {
		testTimeout = time.Duration(config.TimeoutSeconds) * time.Second
	}
	testContext, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	started := time.Now()
	runner := analysis.NewConfiguredCodexRunner("", func() (domain.AIConfig, string, error) {
		return config, token, nil
	})
	answer, testErr := runner.Synthesize(testContext, "只回复 OK，不要添加其他文字。")
	result := domain.AIConfigTestResult{
		Provider: config.Provider, Model: config.Model,
		LatencyMS: time.Since(started).Milliseconds(),
	}
	if testErr != nil {
		result.Message = testErr.Error()
		return result, nil
	}
	if strings.TrimSpace(answer) == "" {
		result.Message = "连接成功但没有返回文本"
		return result, nil
	}
	result.OK = true
	result.Message = fmt.Sprintf("连接成功，返回 %d 字符", len([]rune(strings.TrimSpace(answer))))
	return result, nil
}

func (service webAIConfigService) Reset(_ context.Context) (domain.AIConfigSnapshot, error) {
	if service.app == nil || service.app.aiConfig == nil {
		return domain.AIConfigSnapshot{}, errors.New("AI 配置存储未初始化")
	}
	if err := service.app.aiConfig.Reset(); err != nil {
		return domain.AIConfigSnapshot{}, err
	}
	return service.Load(context.Background())
}

func (service webAIConfigService) loadRecord() (storageAIConfigRecord, error) {
	if service.app == nil || service.app.aiConfig == nil {
		return storageAIConfigRecord{}, errors.New("AI 配置存储未初始化")
	}
	record, err := service.app.aiConfig.Load()
	if err != nil {
		return storageAIConfigRecord{}, err
	}
	config := record.Config.Normalized()
	if strings.TrimSpace(config.CodexHome) == "" {
		config.CodexHome = strings.TrimSpace(os.Getenv("CODEX_HOME"))
	}
	if strings.TrimSpace(config.CodexBin) == "" {
		config.CodexBin = strings.TrimSpace(os.Getenv("ASTOCK_CODEX_BIN"))
	}
	if strings.TrimSpace(config.CodexProfile) == "" {
		config.CodexProfile = strings.TrimSpace(os.Getenv("ASTOCK_CODEX_PROFILE"))
	}
	tokenSource := ""
	if strings.TrimSpace(record.Token) == "" {
		if token, source := aiCredentialForConfig(config); token != "" {
			record.Token = token
			record.TokenConfigured = true
			// Keep the source only for the redacted status response.
			tokenSource = source
		}
	} else {
		record.TokenConfigured = true
		tokenSource = "本地秘密文件"
	}
	record.Config = config
	return storageAIConfigRecord{Config: record.Config, Token: record.Token, TokenConfigured: record.TokenConfigured, TokenSource: tokenSource, UpdatedAt: record.UpdatedAt}, nil
}

// storageAIConfigRecord is an adapter type that keeps source metadata out of
// storage's persisted contract while allowing the Web status to explain where
// credentials came from.
type storageAIConfigRecord struct {
	Config          domain.AIConfig
	Token           string
	TokenConfigured bool
	TokenSource     string
	UpdatedAt       time.Time
}

func (service webAIConfigService) snapshot(record storageAIConfigRecord) domain.AIConfigSnapshot {
	return domain.AIConfigSnapshot{
		Config:          record.Config.Normalized(),
		TokenConfigured: record.TokenConfigured,
		TokenHint:       maskAIToken(record.Token),
		TokenSource:     record.TokenSource,
		Providers:       domain.AIProviderDefinitions(),
		Profiles:        discoverCodexProfiles(record.Config),
	}
}

func (service webAIConfigService) runnerSettings() (domain.AIConfig, string, error) {
	record, err := service.loadRecord()
	if err != nil {
		return domain.AIConfig{}, "", err
	}
	return record.Config, record.Token, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func aiCredentialForConfig(config domain.AIConfig) (string, string) {
	for _, envName := range credentialEnvCandidates(config.Provider) {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			return value, "环境变量 " + envName
		}
	}
	if config.ExecutionMode == domain.AIExecutionCodex {
		if token := codexProfileToken(config.CodexHome); token != "" {
			return token, "Codex Profile auth.json"
		}
	}
	return "", ""
}

func credentialEnvCandidates(provider string) []string {
	result := make([]string, 0, 3)
	if key := domain.AIProviderTokenEnv(provider); key != "" {
		result = append(result, key)
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic":
		result = append(result, "ANTHROPIC_AUTH_TOKEN")
	case "google":
		result = append(result, "GEMINI_API_KEY")
	}
	return result
}

func codexProfileToken(path string) string {
	path = expandAIHome(path)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("CODEX_HOME"))
	}
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(path, "auth.json"))
	if err != nil {
		return ""
	}
	var values map[string]any
	if json.Unmarshal(data, &values) != nil {
		return ""
	}
	for key, raw := range values {
		if strings.EqualFold(strings.TrimSpace(key), "OPENAI_API_KEY") {
			if value, ok := raw.(string); ok {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func discoverCodexProfiles(config domain.AIConfig) []domain.AIProfile {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	root := filepath.Join(home, ".codex-profiles")
	current := expandAIHome(config.CodexHome)
	if current == "" {
		current = expandAIHome(os.Getenv("CODEX_HOME"))
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if current == "" {
			return nil
		}
		return []domain.AIProfile{profileFromPath(current, filepath.Base(current), current == current)}
	}
	profiles := make([]domain.AIProfile, 0, len(entries)+1)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Name() == "_shared" || strings.HasPrefix(entry.Name(), "_backup") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if _, err := os.Stat(filepath.Join(path, "auth.json")); err != nil {
			if _, configErr := os.Stat(filepath.Join(path, "config.toml")); configErr != nil {
				continue
			}
		}
		profiles = append(profiles, profileFromPath(path, entry.Name(), samePath(path, current)))
	}
	if current != "" {
		found := false
		for _, profile := range profiles {
			if samePath(profile.Path, current) {
				found = true
				break
			}
		}
		if !found {
			profiles = append(profiles, profileFromPath(current, filepath.Base(current), true))
		}
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Current != profiles[j].Current {
			return profiles[i].Current
		}
		return profiles[i].Name < profiles[j].Name
	})
	return profiles
}

func profileFromPath(path, id string, current bool) domain.AIProfile {
	provider, model, baseURL := parseCodexProfileConfig(filepath.Join(path, "config.toml"))
	_, authErr := os.Stat(filepath.Join(path, "auth.json"))
	return domain.AIProfile{ID: id, Name: id, Path: path, Current: current, HasAuth: authErr == nil, Provider: provider, Model: model, BaseURL: safeProfileBaseURL(baseURL)}
}

func safeProfileBaseURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func parseCodexProfileConfig(path string) (string, string, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", ""
	}
	provider, model, baseURL := "", "", ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"")
		switch key {
		case "model_provider":
			provider = value
		case "model":
			model = value
		case "base_url":
			baseURL = value
		}
	}
	return provider, model, baseURL
}

func samePath(left, right string) bool {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(expandAIHome(left))
	rightAbs, rightErr := filepath.Abs(expandAIHome(right))
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func expandAIHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

func maskAIToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= 8 {
		return "••••"
	}
	return string(runes[:3]) + "••••" + string(runes[len(runes)-4:])
}

var _ web.AIConfigService = webAIConfigService{}
