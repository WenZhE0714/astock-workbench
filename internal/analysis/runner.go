package analysis

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

//go:embed bridge.py
var bridgeSource string

type Options struct {
	Repo       string
	Python     string
	WorkDir    string
	Ticker     string
	TradeDate  string
	Provider   string
	DeepModel  string
	QuickModel string
	BackendURL string
	Checkpoint bool
}

type Runner struct {
	Progress io.Writer
}

func NewRunner(progress io.Writer) *Runner {
	return &Runner{Progress: progress}
}

func ResolveRepo(explicit string) (string, error) {
	candidates := make([]string, 0)
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	if value := os.Getenv("ASTOCK_TRADINGAGENTS_HOME"); value != "" {
		candidates = append(candidates, value)
	}
	if current, err := os.Getwd(); err == nil {
		candidates = append(candidates, current, filepath.Join(current, "tradingagents-astock"), filepath.Join(filepath.Dir(current), "tradingagents-astock"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "tradingagents-astock"))
	}
	seen := make(map[string]bool)
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil || seen[absolute] {
			continue
		}
		seen[absolute] = true
		if info, err := os.Stat(filepath.Join(absolute, "tradingagents", "graph", "trading_graph.py")); err == nil && !info.IsDir() {
			return absolute, nil
		}
	}
	return "", fmt.Errorf("未找到 tradingagents-astock；请用 --repo 指定，或设置 ASTOCK_TRADINGAGENTS_HOME")
}

func ResolvePython(repo, explicit string) (string, error) {
	candidates := make([]string, 0)
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	if value := os.Getenv("ASTOCK_TRADINGAGENTS_PYTHON"); value != "" {
		candidates = append(candidates, value)
	}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, filepath.Join(repo, ".venv", "Scripts", "python.exe"))
	} else {
		candidates = append(candidates, filepath.Join(repo, ".venv", "bin", "python"))
	}
	for _, name := range []string{"python3.12", "python3.11", "python3.10", "python3"} {
		if path, err := exec.LookPath(name); err == nil {
			candidates = append(candidates, path)
		}
	}
	for _, candidate := range candidates {
		if strings.ContainsRune(candidate, filepath.Separator) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				return candidate, nil
			}
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("未找到可用 Python；TradingAgents 需要 Python >= 3.10")
}

func (runner *Runner) invoke(ctx context.Context, options Options, check bool, target any) error {
	repo, err := ResolveRepo(options.Repo)
	if err != nil {
		return err
	}
	python, err := ResolvePython(repo, options.Python)
	if err != nil {
		return err
	}
	temporary, err := os.MkdirTemp("", "astock-analysis-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	scriptPath := filepath.Join(temporary, "bridge.py")
	outputPath := filepath.Join(temporary, "result.json")
	if err := os.WriteFile(scriptPath, []byte(bridgeSource), 0o600); err != nil {
		return err
	}
	arguments := []string{scriptPath, "--repo", repo, "--output", outputPath, "--work-dir", options.WorkDir}
	if check {
		arguments = append(arguments, "--check")
	} else {
		arguments = append(arguments, "--ticker", options.Ticker, "--date", options.TradeDate)
	}
	if options.Provider != "" {
		arguments = append(arguments, "--provider", options.Provider)
	}
	if options.DeepModel != "" {
		arguments = append(arguments, "--deep-model", options.DeepModel)
	}
	if options.QuickModel != "" {
		arguments = append(arguments, "--quick-model", options.QuickModel)
	}
	if options.BackendURL != "" {
		arguments = append(arguments, "--backend-url", options.BackendURL)
	}
	if options.Checkpoint {
		arguments = append(arguments, "--checkpoint")
	}
	command := exec.CommandContext(ctx, python, arguments...)
	var checkOutput bytes.Buffer
	if check {
		command.Stdout = &checkOutput
		command.Stderr = &checkOutput
	} else {
		command.Stdout = runner.Progress
		command.Stderr = runner.Progress
	}
	runError := command.Run()
	data, readError := os.ReadFile(outputPath)
	if readError != nil {
		if runError != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err()
			}
			return fmt.Errorf("分析桥接进程失败: %w", runError)
		}
		return readError
	}
	var envelope struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	_ = json.Unmarshal(data, &envelope)
	if runError != nil || envelope.Status == "error" {
		if check && checkOutput.Len() > 0 && runner.Progress != nil {
			_, _ = runner.Progress.Write(checkOutput.Bytes())
		}
		if envelope.Error != "" {
			return fmt.Errorf("TradingAgents 分析失败: %s", envelope.Error)
		}
		return fmt.Errorf("TradingAgents 分析失败: %w", runError)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("分析结果格式无效: %w", err)
	}
	return nil
}

func (runner *Runner) Run(ctx context.Context, options Options) (domain.AnalysisResult, error) {
	var result domain.AnalysisResult
	if err := runner.invoke(ctx, options, false, &result); err != nil {
		return domain.AnalysisResult{}, err
	}
	if result.SchemaVersion != 1 || result.Status != "ok" || result.Ticker == "" {
		return domain.AnalysisResult{}, fmt.Errorf("TradingAgents 返回了不兼容的结果")
	}
	return result, nil
}

func (runner *Runner) Check(ctx context.Context, options Options) (domain.AnalysisCheck, error) {
	var result domain.AnalysisCheck
	if err := runner.invoke(ctx, options, true, &result); err != nil {
		return domain.AnalysisCheck{}, err
	}
	return result, nil
}
