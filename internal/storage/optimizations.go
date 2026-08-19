package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

type OptimizationStore struct {
	root string
}

type OptimizationIndexEntry struct {
	ID              string
	GeneratedAt     time.Time
	Candidates      int
	SelectedID      string
	ValidationScore float64
	OOSReturn       float64
	OOSAvailable    bool
	AIUsed          bool
	Directory       string
}

func NewOptimizationStore(root string) *OptimizationStore {
	return &OptimizationStore{root: root}
}

func validArchiveID(id string) bool {
	return strings.TrimSpace(id) != "" && !strings.Contains(id, string(filepath.Separator)) && !strings.Contains(id, "..")
}

func (store *OptimizationStore) Save(result backtest.OptimizationResult) (backtest.OptimizationResult, error) {
	if result.GeneratedAt.IsZero() {
		result.GeneratedAt = time.Now()
	}
	if strings.TrimSpace(result.ID) == "" {
		result.ID = result.GeneratedAt.Format("OPT-20060102T150405")
	}
	if !validArchiveID(result.ID) {
		return result, fmt.Errorf("无效优化实验 ID %q", result.ID)
	}
	directory := filepath.Join(store.root, result.ID)
	baseID := result.ID
	for index := 1; ; index++ {
		if _, err := os.Stat(directory); os.IsNotExist(err) {
			break
		} else if err != nil {
			return result, err
		}
		directory = filepath.Join(store.root, fmt.Sprintf("%s-%02d", baseID, index))
		result.ID = filepath.Base(directory)
	}

	if result.SelectedTrain != nil {
		if err := writeBacktestArtifacts(filepath.Join(directory, "train"), *result.SelectedTrain); err != nil {
			return result, err
		}
	}
	if result.SelectedValidation != nil {
		if err := writeBacktestArtifacts(filepath.Join(directory, "validation"), *result.SelectedValidation); err != nil {
			return result, err
		}
	}
	if result.OutOfSample != nil {
		if err := writeBacktestArtifacts(filepath.Join(directory, "out-of-sample"), *result.OutOfSample); err != nil {
			return result, err
		}
	}
	if err := writeCandidates(filepath.Join(directory, "candidates.jsonl"), result.Candidates); err != nil {
		return result, err
	}
	if result.Selected != nil {
		data, err := json.MarshalIndent(result.Selected, "", "  ")
		if err != nil {
			return result, err
		}
		if err := atomicWrite(filepath.Join(directory, "selected-strategy.json"), append(data, '\n'), 0o600); err != nil {
			return result, err
		}
	}
	if strings.TrimSpace(result.AIReview) != "" {
		if err := atomicWrite(filepath.Join(directory, "ai-review.md"), []byte(result.AIReview), 0o600); err != nil {
			return result, err
		}
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return result, err
	}
	if err := atomicWrite(filepath.Join(directory, "summary.json"), append(data, '\n'), 0o600); err != nil {
		return result, err
	}
	if err := atomicWrite(filepath.Join(directory, "report.md"), []byte(renderOptimizationMarkdown(result)), 0o600); err != nil {
		return result, err
	}
	result.Directory = directory
	result.ReportPath = filepath.Join(directory, "report.md")
	return result, nil
}

func writeCandidates(path string, candidates []backtest.CandidateResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".candidates-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	writer := bufio.NewWriter(file)
	for _, candidate := range candidates {
		data, marshalError := json.Marshal(candidate)
		if marshalError != nil {
			file.Close()
			return marshalError
		}
		if _, writeError := writer.Write(append(data, '\n')); writeError != nil {
			file.Close()
			return writeError
		}
	}
	if err := writer.Flush(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func loadBacktestArtifacts(directory string) (*backtest.Result, error) {
	data, err := os.ReadFile(filepath.Join(directory, "summary.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result backtest.Result
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	result.Directory = directory
	result.ReportPath = filepath.Join(directory, "report.md")
	return &result, nil
}

func (store *OptimizationStore) Load(id string) (backtest.OptimizationResult, error) {
	if !validArchiveID(id) {
		return backtest.OptimizationResult{}, fmt.Errorf("无效优化实验 ID %q", id)
	}
	directory := filepath.Join(store.root, id)
	data, err := os.ReadFile(filepath.Join(directory, "summary.json"))
	if os.IsNotExist(err) {
		return backtest.OptimizationResult{}, fmt.Errorf("未找到优化实验 %s", id)
	}
	if err != nil {
		return backtest.OptimizationResult{}, err
	}
	var result backtest.OptimizationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return backtest.OptimizationResult{}, err
	}
	if result.SelectedTrain, err = loadBacktestArtifacts(filepath.Join(directory, "train")); err != nil {
		return backtest.OptimizationResult{}, err
	}
	if result.SelectedValidation, err = loadBacktestArtifacts(filepath.Join(directory, "validation")); err != nil {
		return backtest.OptimizationResult{}, err
	}
	if result.OutOfSample, err = loadBacktestArtifacts(filepath.Join(directory, "out-of-sample")); err != nil {
		return backtest.OptimizationResult{}, err
	}
	result.Directory = directory
	result.ReportPath = filepath.Join(directory, "report.md")
	return result, nil
}

func (store *OptimizationStore) List(limit int) ([]OptimizationIndexEntry, error) {
	entries, err := os.ReadDir(store.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]OptimizationIndexEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		result, loadError := store.Load(entry.Name())
		if loadError != nil {
			continue
		}
		item := OptimizationIndexEntry{
			ID: result.ID, GeneratedAt: result.GeneratedAt, Candidates: len(result.Candidates),
			AIUsed: strings.TrimSpace(result.AIReview) != "", Directory: result.Directory,
		}
		if result.Selected != nil {
			item.SelectedID = result.Selected.ID
			item.ValidationScore = result.Selected.Score
		}
		if result.OutOfSample != nil {
			item.OOSAvailable = true
			item.OOSReturn = result.OutOfSample.Metrics.TotalReturn
		}
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool { return items[left].GeneratedAt.After(items[right].GeneratedAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func renderOptimizationMarkdown(result backtest.OptimizationResult) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# A股策略优化实验：%s\n\n", result.ID)
	fmt.Fprintf(&builder, "- 生成时间：%s\n", result.GeneratedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&builder, "- 股票池：%s\n", strings.Join(result.Request.BaseRequest.Tickers, ", "))
	fmt.Fprintf(&builder, "- 训练：%s 至 %s\n", result.Request.Train.Start.Format("2006-01-02"), result.Request.Train.End.Format("2006-01-02"))
	fmt.Fprintf(&builder, "- 验证：%s 至 %s\n", result.Request.Validate.Start.Format("2006-01-02"), result.Request.Validate.End.Format("2006-01-02"))
	fmt.Fprintf(&builder, "- 样本外：%s 至 %s\n", result.Request.OutOfSample.Start.Format("2006-01-02"), result.Request.OutOfSample.End.Format("2006-01-02"))
	fmt.Fprintf(&builder, "- 候选：%d 个；通过验证门禁：%d 个\n\n", len(result.Candidates), acceptedCandidates(result.Candidates))
	if result.Selected == nil {
		builder.WriteString("## 选择结果\n\n没有候选通过验证门禁，样本外回测未执行。\n\n")
	} else {
		candidate := result.Selected
		fmt.Fprintf(&builder, "## 入选策略\n\n候选 `%s`，验证评分 %.4f。参数：MA%d/MA%d、突破%d日、量比%.2f、止损%.0f%%、止盈%.0f%%、最长持有%d日、单股上限%.0f%%。\n\n",
			candidate.ID, candidate.Score, candidate.Parameters.FastMA, candidate.Parameters.SlowMA,
			candidate.Parameters.BreakoutDays, candidate.Parameters.VolumeRatioMin,
			candidate.Parameters.StopLoss*100, candidate.Parameters.TakeProfit*100,
			candidate.Parameters.MaxHoldingDays, candidate.Parameters.MaxPosition*100)
		fmt.Fprintf(&builder, "训练收益 %+.2f%%、回撤 %+.2f%%、交易 %d；验证收益 %+.2f%%、回撤 %+.2f%%、交易 %d。\n\n",
			candidate.Train.TotalReturn, candidate.Train.MaxDrawdown, candidate.Train.Trades,
			candidate.Validate.TotalReturn, candidate.Validate.MaxDrawdown, candidate.Validate.Trades)
		if result.OutOfSample != nil {
			metrics := result.OutOfSample.Metrics
			fmt.Fprintf(&builder, "## 一次性样本外检验\n\n收益 %+.2f%%，年化 %+.2f%%，最大回撤 %+.2f%%，夏普 %.2f，交易 %d，胜率 %.2f%%。\n\n",
				metrics.TotalReturn, metrics.AnnualizedReturn, metrics.MaxDrawdown, metrics.Sharpe, metrics.Trades, metrics.WinRate)
		}
	}
	builder.WriteString("## 候选排名\n\n| 候选 | 状态 | 验证评分 | 训练收益 | 验证收益 | 验证回撤 | 验证交易 | 参数 |\n|---|---|---:|---:|---:|---:|---:|---|\n")
	limit := min(20, len(result.Candidates))
	for _, candidate := range result.Candidates[:limit] {
		status := "通过"
		if candidate.Rejected {
			status = "拒绝：" + strings.Join(candidate.Reasons, "；")
		}
		p := candidate.Parameters
		fmt.Fprintf(&builder, "| %s | %s | %.4f | %+.2f%% | %+.2f%% | %+.2f%% | %d | MA%d/%d B%d V%.1f SL%.0f%% TP%.0f%% H%d |\n",
			candidate.ID, status, candidate.Score, candidate.Train.TotalReturn, candidate.Validate.TotalReturn,
			candidate.Validate.MaxDrawdown, candidate.Validate.Trades, p.FastMA, p.SlowMA,
			p.BreakoutDays, p.VolumeRatioMin, p.StopLoss*100, p.TakeProfit*100, p.MaxHoldingDays)
	}
	if result.AIReview != "" {
		builder.WriteString("\n## AI 只读复盘\n\n")
		builder.WriteString(strings.TrimSpace(result.AIReview))
		builder.WriteString("\n")
	} else if result.AIError != "" {
		fmt.Fprintf(&builder, "\n## AI 复盘状态\n\n生成失败：%s\n", result.AIError)
	}
	if len(result.Warnings) > 0 {
		builder.WriteString("\n## 研究限制\n\n")
		for _, warning := range result.Warnings {
			fmt.Fprintf(&builder, "- %s\n", warning)
		}
	}
	builder.WriteString("\n> 本实验是历史研究，不代表未来收益，不触发自动交易。\n")
	return builder.String()
}

func acceptedCandidates(candidates []backtest.CandidateResult) int {
	count := 0
	for _, candidate := range candidates {
		if !candidate.Rejected {
			count++
		}
	}
	return count
}
