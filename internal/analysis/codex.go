package analysis

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type TextSynthesizer interface {
	Synthesize(context.Context, string) (string, error)
}

type CodexRunner struct {
	WorkDir string
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
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 800 {
			detail = detail[len(detail)-800:]
		}
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("Codex 后台综合失败: %s", detail)
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
