package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type TextSynthesizer interface {
	Synthesize(context.Context, string) (string, error)
}

type StructuredSynthesizer interface {
	SynthesizeJSON(context.Context, string, []byte, any) error
}

type CodexRunner struct {
	WorkDir  string
	Settings func() (domain.AIConfig, string, error)
}

func limitedRuneSuffix(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	return string(runes[len(runes)-limit:])
}

func codexErrorDetail(stderr string) string {
	var detail string
	markers := []string{
		"error", "failed", "failure", "timed out", "timeout", "rate limit", "too many requests",
		"429", "401", "403", "connection", "stream disconnected", "unexpected status", "quota",
		"overloaded", "unavailable",
	}
	for _, line := range strings.Split(strings.ReplaceAll(stderr, "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		for _, marker := range markers {
			if strings.Contains(lower, marker) {
				detail = line
				break
			}
		}
	}
	return limitedRuneSuffix(detail, 800)
}

func codexCommandError(ctx context.Context, runError error, stderr string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("Codex 后台综合超时")
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return fmt.Errorf("Codex 后台综合已取消")
	}
	detail := codexErrorDetail(stderr)
	if detail == "" {
		detail = runError.Error()
	}
	return fmt.Errorf("Codex 后台综合失败: %s", detail)
}

func NewCodexRunner(workDir string) *CodexRunner {
	return &CodexRunner{WorkDir: workDir}
}

// NewConfiguredCodexRunner keeps the historical runner type while allowing
// the Web settings page to switch between Codex CLI and direct provider APIs.
func NewConfiguredCodexRunner(workDir string, settings func() (domain.AIConfig, string, error)) *CodexRunner {
	return &CodexRunner{WorkDir: workDir, Settings: settings}
}

// CloneForWorkDir creates an isolated runner for parallel specialist agents
// without losing the shared runtime configuration.
func (runner *CodexRunner) CloneForWorkDir(workDir string) *CodexRunner {
	if runner == nil {
		return NewCodexRunner(workDir)
	}
	return &CodexRunner{WorkDir: workDir, Settings: runner.Settings}
}

func (runner *CodexRunner) currentSettings() (domain.AIConfig, string, error) {
	if runner != nil && runner.Settings != nil {
		config, token, err := runner.Settings()
		if err != nil {
			return domain.AIConfig{}, "", err
		}
		return config.Normalized(), strings.TrimSpace(token), nil
	}
	// Preserve the original environment-driven behavior for embedders and
	// existing unit tests that construct NewCodexRunner directly.
	config := domain.AIConfig{
		Enabled:        true,
		ExecutionMode:  domain.AIExecutionCodex,
		Provider:       strings.TrimSpace(os.Getenv("ASTOCK_AI_PROVIDER")),
		Model:          strings.TrimSpace(os.Getenv("ASTOCK_CODEX_MODEL")),
		CodexBin:       strings.TrimSpace(os.Getenv("ASTOCK_CODEX_BIN")),
		CodexHome:      strings.TrimSpace(os.Getenv("CODEX_HOME")),
		CodexProfile:   strings.TrimSpace(os.Getenv("ASTOCK_CODEX_PROFILE")),
		TimeoutSeconds: 600,
	}
	if execution := strings.TrimSpace(os.Getenv("ASTOCK_AI_EXECUTION")); execution != "" {
		config.ExecutionMode = strings.ToLower(execution)
	}
	if config.Provider == "" {
		config.Provider = "openai"
	}
	return config.Normalized(), "", nil
}

func (runner *CodexRunner) Synthesize(ctx context.Context, prompt string) (string, error) {
	config, token, err := runner.currentSettings()
	if err != nil {
		return "", err
	}
	if !config.Enabled {
		return "", fmt.Errorf("AI 服务已停用，请在 Web 的 AI 设置中启用")
	}
	ctx, cancel := contextWithAIConfigTimeout(ctx, config)
	defer cancel()
	if config.ExecutionMode == domain.AIExecutionAPI {
		return runner.synthesizeProviderAPI(ctx, config, token, prompt)
	}
	binary := strings.TrimSpace(config.CodexBin)
	if binary == "" {
		binary = "codex"
	}
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("未找到 Codex CLI；请确认 codex 在 PATH 中或设置 ASTOCK_CODEX_BIN")
	}
	temporary, err := os.CreateTemp("", "astock-market-report-*.md")
	if err != nil {
		return "", err
	}
	outputPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		return "", err
	}
	defer os.Remove(outputPath)

	args := []string{"-a", "never"}
	if profile := strings.TrimSpace(config.CodexProfile); profile != "" {
		args = append(args, "-p", profile)
	}
	if model := strings.TrimSpace(config.Model); model != "" && runner.Settings != nil {
		args = append(args, "-m", model)
	} else if model := strings.TrimSpace(os.Getenv("ASTOCK_CODEX_MODEL")); model != "" {
		args = append(args, "-m", model)
	}
	args = append(args,
		"exec", "--ephemeral", "--skip-git-repo-check", "-s", "read-only", "--color", "never",
	)
	workDir := runner.WorkDir
	if workDir == "" {
		workDir = os.TempDir()
	}
	if absolute, absoluteError := filepath.Abs(workDir); absoluteError == nil {
		workDir = absolute
	}
	args = append(args, "-C", workDir, "-o", outputPath, "-")
	command := exec.CommandContext(ctx, resolved, args...)
	command.Stdin = strings.NewReader(prompt)
	command.Env = configuredCommandEnv(config, token)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", codexCommandError(ctx, err, stderr.String())
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		return "", err
	}
	result := strings.TrimSpace(string(content))
	if result == "" {
		result = strings.TrimSpace(stdout.String())
	}
	if result == "" {
		return "", fmt.Errorf("Codex 未返回报告内容")
	}
	return result + "\n", nil
}

func (runner *CodexRunner) SynthesizeJSON(ctx context.Context, prompt string, schema []byte, target any) error {
	config, token, err := runner.currentSettings()
	if err != nil {
		return err
	}
	if !config.Enabled {
		return fmt.Errorf("AI 服务已停用，请在 Web 的 AI 设置中启用")
	}
	ctx, cancel := contextWithAIConfigTimeout(ctx, config)
	defer cancel()
	if config.ExecutionMode == domain.AIExecutionAPI {
		return runner.synthesizeProviderJSON(ctx, config, token, prompt, schema, target)
	}
	binary := strings.TrimSpace(config.CodexBin)
	if binary == "" {
		binary = "codex"
	}
	resolved, err := exec.LookPath(binary)
	if err != nil {
		return fmt.Errorf("未找到 Codex CLI；请确认 codex 在 PATH 中或设置 ASTOCK_CODEX_BIN")
	}
	schemaFile, err := os.CreateTemp("", "astock-agent-schema-*.json")
	if err != nil {
		return err
	}
	schemaPath := schemaFile.Name()
	defer os.Remove(schemaPath)
	if err := schemaFile.Chmod(0o600); err != nil {
		schemaFile.Close()
		return err
	}
	if _, err := schemaFile.Write(schema); err != nil {
		schemaFile.Close()
		return err
	}
	if err := schemaFile.Close(); err != nil {
		return err
	}
	outFile, err := os.CreateTemp("", "astock-agent-output-*.json")
	if err != nil {
		return err
	}
	outPath := outFile.Name()
	defer os.Remove(outPath)
	if err := outFile.Close(); err != nil {
		return err
	}
	args := []string{"-a", "never"}
	if profile := strings.TrimSpace(config.CodexProfile); profile != "" {
		args = append(args, "-p", profile)
	}
	if model := strings.TrimSpace(config.Model); model != "" && runner.Settings != nil {
		args = append(args, "-m", model)
	} else if model := strings.TrimSpace(os.Getenv("ASTOCK_CODEX_MODEL")); model != "" {
		args = append(args, "-m", model)
	}
	workDir := runner.WorkDir
	if workDir == "" {
		workDir = os.TempDir()
	}
	if absolute, absoluteError := filepath.Abs(workDir); absoluteError == nil {
		workDir = absolute
	}
	args = append(args, "exec", "--ephemeral", "--skip-git-repo-check", "-s", "read-only", "--color", "never", "--output-schema", schemaPath, "-C", workDir, "-o", outPath, "-")
	command := exec.CommandContext(ctx, resolved, args...)
	command.Stdin = strings.NewReader(prompt)
	command.Env = configuredCommandEnv(config, token)
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return codexCommandError(ctx, err, stderr.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		return err
	}
	if len(data) > 256*1024 {
		return fmt.Errorf("Codex 结构化候选超过 256KB 限制")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("Codex 结构化候选格式无效: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return fmt.Errorf("Codex 结构化候选包含多余 JSON")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("Codex 结构化候选尾部无效: %w", err)
	}
	return nil
}

func contextWithAIConfigTimeout(ctx context.Context, config domain.AIConfig) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if config.TimeoutSeconds < 30 || config.TimeoutSeconds > 1800 {
		return ctx, func() {}
	}
	deadline := time.Now().Add(time.Duration(config.TimeoutSeconds) * time.Second)
	if parentDeadline, ok := ctx.Deadline(); ok && !deadline.Before(parentDeadline) {
		return ctx, func() {}
	}
	return context.WithDeadline(ctx, deadline)
}

func configuredCommandEnv(config domain.AIConfig, token string) []string {
	env := append([]string(nil), os.Environ()...)
	setEnv := func(key, value string) {
		prefix := key + "="
		for index, item := range env {
			if strings.HasPrefix(item, prefix) {
				env[index] = prefix + value
				return
			}
		}
		env = append(env, prefix+value)
	}
	setEnv("NO_COLOR", "1")
	if home := strings.TrimSpace(config.CodexHome); home != "" {
		setEnv("CODEX_HOME", expandHome(home))
	}
	if strings.TrimSpace(token) != "" {
		setEnv("OPENAI_API_KEY", strings.TrimSpace(token))
	}
	return env
}

func expandHome(value string) string {
	value = strings.TrimSpace(value)
	if value == "~" || strings.HasPrefix(value, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(value, "~/"))
		}
	}
	return value
}
