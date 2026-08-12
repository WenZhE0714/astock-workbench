package analysis

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
