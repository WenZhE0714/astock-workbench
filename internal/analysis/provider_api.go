package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const (
	maxProviderResponseBytes = 2 << 20
	providerTestPrompt       = "只回复 OK，不要添加其他文字。"
)

type providerAPIResponse struct {
	StatusCode int
	Body       []byte
}

func providerToken(config domain.AIConfig, stored string) string {
	if value := strings.TrimSpace(stored); value != "" {
		return value
	}
	for _, key := range providerTokenEnvCandidates(config.Provider) {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

// getenv is a variable so unit tests can replace environment lookup without
// mutating process state while requests are running in parallel.
var getenv = func(key string) string {
	return os.Getenv(key)
}

func providerTokenEnvCandidates(provider string) []string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	result := make([]string, 0, 3)
	if key := domain.AIProviderTokenEnv(provider); key != "" {
		result = append(result, key)
	}
	switch provider {
	case "anthropic":
		result = append(result, "ANTHROPIC_AUTH_TOKEN")
	case "google":
		result = append(result, "GEMINI_API_KEY")
	}
	return result
}

func (runner *CodexRunner) synthesizeProviderAPI(ctx context.Context, config domain.AIConfig, storedToken, prompt string) (string, error) {
	config = config.Normalized()
	if err := config.Validate(); err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok && config.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(config.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	token := providerToken(config, storedToken)
	switch strings.ToLower(strings.TrimSpace(config.Provider)) {
	case "anthropic":
		return callAnthropic(ctx, config, token, prompt)
	case "google":
		return callGoogle(ctx, config, token, prompt)
	default:
		return callOpenAICompatible(ctx, config, token, prompt)
	}
}

func (runner *CodexRunner) synthesizeProviderJSON(ctx context.Context, config domain.AIConfig, storedToken string, prompt string, schema []byte, target any) error {
	if len(schema) == 0 || len(schema) > 128*1024 {
		return fmt.Errorf("AI 结构化 schema 大小无效")
	}
	jsonPrompt := strings.TrimSpace(prompt) + "\n\n只输出一个符合以下 JSON Schema 的 JSON 对象，不要 Markdown 代码围栏或解释：\n" + string(schema)
	text, err := runner.synthesizeProviderAPI(ctx, config, storedToken, jsonPrompt)
	if err != nil {
		return err
	}
	data, err := extractJSONObject([]byte(text))
	if err != nil {
		return fmt.Errorf("AI 结构化候选格式无效: %w", err)
	}
	if len(data) > 256*1024 {
		return fmt.Errorf("AI 结构化候选超过 256KB 限制")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("AI 结构化候选格式无效: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return fmt.Errorf("AI 结构化候选包含多余 JSON")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("AI 结构化候选尾部无效: %w", err)
	}
	return nil
}

var providerHTTPClientFactory = func(config domain.AIConfig) *http.Client {
	timeout := 60 * time.Second
	if config.TimeoutSeconds >= 30 && config.TimeoutSeconds <= 1800 {
		timeout = time.Duration(config.TimeoutSeconds) * time.Second
	}
	return &http.Client{Timeout: timeout}
}

func apiBaseURL(config domain.AIConfig) (string, error) {
	base := strings.TrimSpace(config.BaseURL)
	if base == "" {
		if definition, ok := domain.FindAIProviderDefinition(config.Provider); ok {
			base = definition.DefaultBaseURL
		}
	}
	base = strings.TrimRight(base, "/")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", fmt.Errorf("AI API Base URL 无效")
	}
	return base, nil
}

func callOpenAICompatible(ctx context.Context, config domain.AIConfig, token, prompt string) (string, error) {
	if token == "" && strings.ToLower(config.Provider) != "ollama" {
		return "", fmt.Errorf("未找到 %s 的 API Token（可在 AI 设置页填写，或配置对应环境变量）", config.Provider)
	}
	base, err := apiBaseURL(config)
	if err != nil {
		return "", err
	}
	endpoint := base
	if !strings.HasSuffix(strings.ToLower(endpoint), "/chat/completions") {
		endpoint += "/chat/completions"
	}
	model := configuredModel(config)
	body := map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
		"stream":   false,
	}
	if strings.TrimSpace(config.Reasoning) != "" {
		body["reasoning_effort"] = strings.TrimSpace(config.Reasoning)
	}
	requestBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if strings.EqualFold(config.Provider, "openrouter") {
		request.Header.Set("X-Title", "ASTOCK Workbench")
	}
	response, err := providerHTTPClientFactory(config).Do(request)
	if err != nil {
		return "", providerNetworkError(ctx, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxProviderResponseBytes {
		return "", fmt.Errorf("AI provider 返回内容超过 2MB 限制")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", providerHTTPError(response.StatusCode, data, token)
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("AI provider 响应格式无效: %w", err)
	}
	if len(payload.Choices) == 0 {
		return "", fmt.Errorf("AI provider 未返回 choices")
	}
	content := flexibleText(payload.Choices[0].Message.Content)
	if content == "" {
		content = strings.TrimSpace(payload.Choices[0].Text)
	}
	if content == "" {
		return "", fmt.Errorf("AI provider 未返回文本内容")
	}
	return content + "\n", nil
}

func callAnthropic(ctx context.Context, config domain.AIConfig, token, prompt string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("未找到 Anthropic API Token（可在 AI 设置页填写，或配置 ANTHROPIC_API_KEY）")
	}
	base, err := apiBaseURL(config)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(base), "/v1") {
		base += "/v1"
	}
	requestBody, err := json.Marshal(map[string]any{
		"model": configuredModel(config), "max_tokens": 4096,
		"messages": []map[string]string{{"role": "user", "content": prompt}},
	})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/messages", bytes.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-api-key", token)
	request.Header.Set("anthropic-version", "2023-06-01")
	response, err := providerHTTPClientFactory(config).Do(request)
	if err != nil {
		return "", providerNetworkError(ctx, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxProviderResponseBytes {
		return "", fmt.Errorf("AI provider 返回内容超过 2MB 限制")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", providerHTTPError(response.StatusCode, data, token)
	}
	var payload struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("Anthropic 响应格式无效: %w", err)
	}
	var parts []string
	for _, item := range payload.Content {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			parts = append(parts, strings.TrimSpace(item.Text))
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("Anthropic 未返回文本内容")
	}
	return strings.Join(parts, "\n") + "\n", nil
}

func callGoogle(ctx context.Context, config domain.AIConfig, token, prompt string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("未找到 Google Gemini API Token（可在 AI 设置页填写，或配置 GOOGLE_API_KEY）")
	}
	base, err := apiBaseURL(config)
	if err != nil {
		return "", err
	}
	model := configuredModel(config)
	endpoint := base
	if !strings.Contains(strings.ToLower(endpoint), "/models/") {
		if !strings.HasSuffix(endpoint, "/v1beta") && !strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/v1beta"
		}
		endpoint += "/models/" + url.PathEscape(model) + ":generateContent"
	}
	requestBody, err := json.Marshal(map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": []map[string]string{{"text": prompt}}}},
	})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-goog-api-key", token)
	response, err := providerHTTPClientFactory(config).Do(request)
	if err != nil {
		return "", providerNetworkError(ctx, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxProviderResponseBytes {
		return "", fmt.Errorf("AI provider 返回内容超过 2MB 限制")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", providerHTTPError(response.StatusCode, data, token)
	}
	var payload struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("Gemini 响应格式无效: %w", err)
	}
	var parts []string
	if len(payload.Candidates) > 0 {
		for _, item := range payload.Candidates[0].Content.Parts {
			if strings.TrimSpace(item.Text) != "" {
				parts = append(parts, strings.TrimSpace(item.Text))
			}
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("Gemini 未返回文本内容")
	}
	return strings.Join(parts, "\n") + "\n", nil
}

func configuredModel(config domain.AIConfig) string {
	if value := strings.TrimSpace(config.Model); value != "" {
		return value
	}
	if value := strings.TrimSpace(config.DeepModel); value != "" {
		return value
	}
	return strings.TrimSpace(config.QuickModel)
}

func flexibleText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		parts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			if block.Type == "text" || block.Type == "output_text" || block.Text != "" {
				if strings.TrimSpace(block.Text) != "" {
					parts = append(parts, strings.TrimSpace(block.Text))
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func extractJSONObject(data []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) >= 6 && bytes.HasPrefix(trimmed, []byte("```")) {
		lines := strings.Split(string(trimmed), "\n")
		if len(lines) >= 3 {
			lines = lines[1:]
			if strings.TrimSpace(lines[len(lines)-1]) == "```" {
				lines = lines[:len(lines)-1]
			}
			trimmed = bytes.TrimSpace([]byte(strings.Join(lines, "\n")))
		}
	}
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("响应为空")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if err := decoder.Decode(&value); err == nil {
		if object, ok := value.(map[string]any); ok {
			encoded, encodeErr := json.Marshal(object)
			if encodeErr == nil {
				return encoded, nil
			}
		}
	}
	start := bytes.IndexByte(trimmed, '{')
	end := bytes.LastIndexByte(trimmed, '}')
	if start >= 0 && end > start {
		candidate := trimmed[start : end+1]
		var object map[string]any
		if err := json.Unmarshal(candidate, &object); err == nil {
			return candidate, nil
		}
	}
	return nil, fmt.Errorf("未找到 JSON 对象")
}

func providerHTTPError(status int, data []byte, token string) error {
	detail := strings.TrimSpace(string(data))
	if token != "" {
		detail = strings.ReplaceAll(detail, token, "[TOKEN]")
	}
	if len([]rune(detail)) > 600 {
		detail = string([]rune(detail)[:600]) + "..."
	}
	if detail == "" {
		detail = http.StatusText(status)
	}
	return fmt.Errorf("AI provider HTTP %s: %s", strconv.Itoa(status), detail)
}

func providerNetworkError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("AI provider 请求超时")
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return fmt.Errorf("AI provider 请求已取消")
	}
	return fmt.Errorf("AI provider 请求失败: %w", err)
}
