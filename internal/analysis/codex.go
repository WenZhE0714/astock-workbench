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
)

type TextSynthesizer interface {
	Synthesize(context.Context, string) (string, error)
}

type StructuredSynthesizer interface {
	SynthesizeJSON(context.Context, string, []byte, any) error
}

type CodexRunner struct {
	WorkDir string
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

func (runner *CodexRunner) Synthesize(ctx context.Context, prompt string) (string, error) {
	binary := strings.TrimSpace(os.Getenv("ASTOCK_CODEX_BIN"))
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
	if profile := strings.TrimSpace(os.Getenv("ASTOCK_CODEX_PROFILE")); profile != "" {
		args = append(args, "-p", profile)
	}
	if model := strings.TrimSpace(os.Getenv("ASTOCK_CODEX_MODEL")); model != "" {
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
	command.Env = append(os.Environ(), "NO_COLOR=1")
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
	binary := strings.TrimSpace(os.Getenv("ASTOCK_CODEX_BIN"))
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
	if profile := strings.TrimSpace(os.Getenv("ASTOCK_CODEX_PROFILE")); profile != "" {
		args = append(args, "-p", profile)
	}
	if model := strings.TrimSpace(os.Getenv("ASTOCK_CODEX_MODEL")); model != "" {
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
	command.Env = append(os.Environ(), "NO_COLOR=1")
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
