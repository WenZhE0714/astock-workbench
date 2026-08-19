package analysis

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCodexRunnerUsesEphemeralReadOnlyNonInteractiveMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	directory := t.TempDir()
	binary := filepath.Join(directory, "fake-codex")
	argumentsPath := filepath.Join(directory, "arguments.txt")
	promptPath := filepath.Join(directory, "prompt.txt")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$ARGS_FILE"
out=''
previous=''
for argument in "$@"; do
  if [ "$previous" = '-o' ]; then out="$argument"; fi
  previous="$argument"
done
cat > "$PROMPT_FILE"
printf '# generated report\n' > "$out"
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASTOCK_CODEX_BIN", binary)
	t.Setenv("ARGS_FILE", argumentsPath)
	t.Setenv("PROMPT_FILE", promptPath)
	runner := NewCodexRunner(directory)
	result, err := runner.Synthesize(context.Background(), "structured facts")
	if err != nil {
		t.Fatal(err)
	}
	if result != "# generated report\n" {
		t.Fatalf("unexpected result: %q", result)
	}
	arguments, err := os.ReadFile(argumentsPath)
	if err != nil {
		t.Fatal(err)
	}
	joined := string(arguments)
	for _, expected := range []string{"-a\nnever\n", "exec\n", "--ephemeral\n", "-s\nread-only\n", "--skip-git-repo-check\n"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("Codex arguments missing %q:\n%s", expected, joined)
		}
	}
	prompt, err := os.ReadFile(promptPath)
	if err != nil || string(prompt) != "structured facts" {
		t.Fatalf("unexpected prompt: %q err=%v", prompt, err)
	}
}

func TestCodexRunnerStructuredOutputUsesSchemaAndStrictJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	directory := t.TempDir()
	binary := filepath.Join(directory, "fake-codex")
	argumentsPath := filepath.Join(directory, "arguments.txt")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$ARGS_FILE"
out=''
previous=''
for argument in "$@"; do
  if [ "$previous" = '-o' ]; then out="$argument"; fi
  previous="$argument"
done
cat >/dev/null
printf '{"schema_version":1,"candidates":[]}' > "$out"
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASTOCK_CODEX_BIN", binary)
	t.Setenv("ARGS_FILE", argumentsPath)
	var result struct {
		SchemaVersion int   `json:"schema_version"`
		Candidates    []any `json:"candidates"`
	}
	runner := NewCodexRunner(directory)
	if err := runner.SynthesizeJSON(context.Background(), "prompt", []byte(`{"type":"object"}`), &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 1 || result.Candidates == nil {
		t.Fatalf("unexpected structured result: %#v", result)
	}
	arguments, err := os.ReadFile(argumentsPath)
	if err != nil {
		t.Fatal(err)
	}
	joined := string(arguments)
	for _, expected := range []string{"--output-schema\n", "--ephemeral\n", "read-only\n"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("structured arguments missing %q:\n%s", expected, joined)
		}
	}
}

func TestCodexCommandErrorReportsTimeoutWithoutPartialOutput(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(context.DeadlineExceeded)
	errorValue := codexCommandError(ctx, errors.New("signal: killed"), "# 半截报告正文\ntokens used\n14,155")
	if errorValue.Error() != "Codex 后台综合已取消" {
		t.Fatalf("unexpected cancellation error: %q", errorValue)
	}

	deadlineContext, deadlineCancel := context.WithTimeout(context.Background(), 0)
	defer deadlineCancel()
	<-deadlineContext.Done()
	errorValue = codexCommandError(deadlineContext, errors.New("signal: killed"), "# 半截报告正文")
	if errorValue.Error() != "Codex 后台综合超时" || strings.Contains(errorValue.Error(), "报告正文") {
		t.Fatalf("unexpected timeout error: %q", errorValue)
	}
}

func TestCodexErrorDetailKeepsUTF8AndIgnoresGeneratedProse(t *testing.T) {
	stderr := strings.Repeat("这是生成中的中文报告。", 200) + "\nERROR stream disconnected before completion\n" +
		strings.Repeat("跨交易日结构位。", 200)
	detail := codexErrorDetail(stderr)
	if detail != "ERROR stream disconnected before completion" {
		t.Fatalf("unexpected extracted error detail: %q", detail)
	}
	if !utf8.ValidString(detail) || strings.Contains(detail, "�") {
		t.Fatalf("error detail is not valid UTF-8: %q", detail)
	}

	truncated := limitedRuneSuffix(strings.Repeat("中文", 500), 800)
	if !utf8.ValidString(truncated) || len([]rune(truncated)) != 800 {
		t.Fatalf("rune truncation is invalid: runes=%d valid=%v", len([]rune(truncated)), utf8.ValidString(truncated))
	}
}
