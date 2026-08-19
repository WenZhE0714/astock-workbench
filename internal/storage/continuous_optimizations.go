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

type ContinuousOptimizationStore struct {
	root string
}

type ContinuousOptimizationIndexEntry struct {
	ID            string
	GeneratedAt   time.Time
	Stage         string
	SelectedID    string
	Candidates    int
	HoldoutReturn float64
	Directory     string
}

func NewContinuousOptimizationStore(root string) *ContinuousOptimizationStore {
	return &ContinuousOptimizationStore{root: root}
}

func (store *ContinuousOptimizationStore) Save(result backtest.ContinuousOptimizationResult) (backtest.ContinuousOptimizationResult, error) {
	if result.GeneratedAt.IsZero() {
		result.GeneratedAt = time.Now()
	}
	if result.ID == "" {
		result.ID = "AUTO-" + result.GeneratedAt.Format("20060102T150405")
	}
	if !validArchiveID(result.ID) {
		return result, fmt.Errorf("无效持续优化实验 ID %q", result.ID)
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
	if result.Holdout != nil {
		if err := writeBacktestArtifacts(filepath.Join(directory, "holdout"), *result.Holdout); err != nil {
			return result, err
		}
	}
	if result.Stress.DoubleCost != nil {
		if err := writeBacktestArtifacts(filepath.Join(directory, "stress-double-cost"), *result.Stress.DoubleCost); err != nil {
			return result, err
		}
	}
	if err := writeJSONLines(filepath.Join(directory, "agents.jsonl"), result.Agents); err != nil {
		return result, err
	}
	if err := writeJSONLines(filepath.Join(directory, "candidates.jsonl"), result.Candidates); err != nil {
		return result, err
	}
	if result.SupervisorReview != "" {
		if err := atomicWrite(filepath.Join(directory, "supervisor-review.md"), []byte(strings.TrimSpace(result.SupervisorReview)+"\n"), 0o600); err != nil {
			return result, err
		}
	}
	manifest, err := json.MarshalIndent(result.Manifest, "", "  ")
	if err != nil {
		return result, err
	}
	if err := atomicWrite(filepath.Join(directory, "manifest.json"), append(manifest, '\n'), 0o600); err != nil {
		return result, err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return result, err
	}
	if err := atomicWrite(filepath.Join(directory, "summary.json"), append(data, '\n'), 0o600); err != nil {
		return result, err
	}
	if err := atomicWrite(filepath.Join(directory, "report.md"), []byte(renderContinuousOptimizationMarkdown(result)), 0o600); err != nil {
		return result, err
	}
	result.Directory = directory
	result.ReportPath = filepath.Join(directory, "report.md")
	return result, nil
}

func writeJSONLines[T any](path string, items []T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".jsonl-*")
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
	encoder := json.NewEncoder(writer)
	for _, item := range items {
		if err := encoder.Encode(item); err != nil {
			file.Close()
			return err
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

func (store *ContinuousOptimizationStore) Load(id string) (backtest.ContinuousOptimizationResult, error) {
	if !validArchiveID(id) {
		return backtest.ContinuousOptimizationResult{}, fmt.Errorf("无效持续优化实验 ID %q", id)
	}
	directory := filepath.Join(store.root, id)
	data, err := os.ReadFile(filepath.Join(directory, "summary.json"))
	if os.IsNotExist(err) {
		return backtest.ContinuousOptimizationResult{}, fmt.Errorf("未找到持续优化实验 %s", id)
	}
	if err != nil {
		return backtest.ContinuousOptimizationResult{}, err
	}
	var result backtest.ContinuousOptimizationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	if result.Holdout, err = loadBacktestArtifacts(filepath.Join(directory, "holdout")); err != nil {
		return result, err
	}
	if result.Stress.DoubleCost, err = loadBacktestArtifacts(filepath.Join(directory, "stress-double-cost")); err != nil {
		return result, err
	}
	result.Directory = directory
	result.ReportPath = filepath.Join(directory, "report.md")
	return result, nil
}

func (store *ContinuousOptimizationStore) List(limit int) ([]ContinuousOptimizationIndexEntry, error) {
	results, err := store.All()
	if err != nil {
		return nil, err
	}
	items := make([]ContinuousOptimizationIndexEntry, 0, len(results))
	for _, result := range results {
		item := ContinuousOptimizationIndexEntry{
			ID: result.ID, GeneratedAt: result.GeneratedAt, Stage: result.Stage,
			Candidates: len(result.Candidates), Directory: result.Directory,
		}
		if result.Selected != nil {
			item.SelectedID = result.Selected.Proposal.ID
		}
		if result.Holdout != nil {
			item.HoldoutReturn = result.Holdout.Metrics.TotalReturn
		}
		items = append(items, item)
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (store *ContinuousOptimizationStore) All() ([]backtest.ContinuousOptimizationResult, error) {
	entries, err := os.ReadDir(store.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	results := make([]backtest.ContinuousOptimizationResult, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		result, loadError := store.Load(entry.Name())
		if loadError != nil {
			return nil, fmt.Errorf("读取持续优化归档 %s: %w", entry.Name(), loadError)
		}
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].GeneratedAt.After(results[j].GeneratedAt) })
	return results, nil
}

func continuousStageText(stage string) string {
	if stage == backtest.ContinuousStageShadow {
		return "模拟观察候选"
	}
	return "研究候选"
}

func renderContinuousOptimizationMarkdown(result backtest.ContinuousOptimizationResult) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# 多Agent持续策略优化：%s\n\n", result.ID)
	fmt.Fprintf(&builder, "- 生成时间：%s\n- 阶段：%s\n- 股票池：%s\n- 子Agent：%d 个\n- 候选：%d 个\n- 滚动窗口：%d 折\n\n",
		result.GeneratedAt.Format("2006-01-02 15:04:05"), continuousStageText(result.Stage),
		strings.Join(result.Request.BaseRequest.Tickers, ", "), len(result.Agents), len(result.Candidates), len(result.Request.Folds))
	fmt.Fprintf(&builder, "- 数据截止：%s\n- 最终留出：%s 至 %s\n", result.DataCutoff, result.Manifest.Holdout.Start.Format("2006-01-02"), result.Manifest.Holdout.End.Format("2006-01-02"))
	if result.Manifest.PreviousExperiment != "" {
		fmt.Fprintf(&builder, "- 上一轮实验：%s；上一轮留出结束：%s\n", result.Manifest.PreviousExperiment, result.Manifest.PreviousHoldoutEnd)
	}
	fmt.Fprintf(&builder, "- 候选集指纹：`%s`\n- 配置指纹：`%s`\n\n", result.Manifest.CandidateSetHash, result.Manifest.ConfigurationHash)
	if result.Selected == nil {
		builder.WriteString("## 选择结果\n\n没有候选通过滚动验证门禁，最终留出与压力测试未运行。\n")
	} else {
		candidate := result.Selected
		p := candidate.Proposal.Parameters
		fmt.Fprintf(&builder, "## 锁定候选\n\n`%s`（%s）：%s。参数模式 `%s`，MA%d/%d、突破%d日、量比%.2f、止损%.0f%%、止盈%.0f%%、最长持有%d日。\n\n",
			candidate.Proposal.ID, candidate.Proposal.Agent, candidate.Proposal.Thesis,
			p.EffectiveEntryMode(), p.FastMA, p.SlowMA, p.BreakoutDays, p.VolumeRatioMin, p.StopLoss*100, p.TakeProfit*100, p.MaxHoldingDays)
		fmt.Fprintf(&builder, "滚动验证：正收益窗口 %.0f%%，交易 %d，平均收益 %+.2f%%，最差回撤 %+.2f%%，稳定性评分 %.2f。\n\n",
			candidate.PositiveFoldRatio*100, candidate.ValidationTrades, candidate.AverageValidation, candidate.WorstDrawdown, candidate.Score)
		fmt.Fprintf(&builder, "Agent共识 %d 个，邻域候选 %d 个，邻域平均评分 %.2f。\n\n", candidate.ConsensusAgents, candidate.NeighborhoodSize, candidate.NeighborhoodScore)
		builder.WriteString("## 滚动折证据\n\n")
		for _, fold := range candidate.Folds {
			fmt.Fprintf(&builder, "- %s：训练 %s 至 %s，收益 %+.2f%%、回撤 %+.2f%%、夏普 %.2f、交易 %d；验证 %s 至 %s，收益 %+.2f%%、回撤 %+.2f%%、夏普 %.2f、交易 %d\n",
				fold.Fold.ID, fold.Fold.Train.Start.Format("2006-01-02"), fold.Fold.Train.End.Format("2006-01-02"), fold.Train.TotalReturn, fold.Train.MaxDrawdown, fold.Train.Sharpe, fold.Train.Trades,
				fold.Fold.Validate.Start.Format("2006-01-02"), fold.Fold.Validate.End.Format("2006-01-02"), fold.Validate.TotalReturn, fold.Validate.MaxDrawdown, fold.Validate.Sharpe, fold.Validate.Trades)
			for _, ticker := range result.Request.BaseRequest.Tickers {
				train := fold.TrainCoverage[ticker]
				validate := fold.ValidateCoverage[ticker]
				fmt.Fprintf(&builder, "  - %s 覆盖：训练 %.0f%%/%d 根，验证 %.0f%%/%d 根；来源 %s / %s\n", ticker, train.CoverageRatio*100, train.Bars, validate.CoverageRatio*100, validate.Bars, fold.TrainSources[ticker], fold.ValidateSources[ticker])
			}
		}
		builder.WriteString("\n")
		if result.Holdout != nil {
			m := result.Holdout.Metrics
			fmt.Fprintf(&builder, "## 最终留出检验\n\n收益 %+.2f%%，年化 %+.2f%%，最大回撤 %+.2f%%，夏普 %.2f，交易 %d。\n\n", m.TotalReturn, m.AnnualizedReturn, m.MaxDrawdown, m.Sharpe, m.Trades)
		}
		if result.Stress.DoubleCost != nil {
			m := result.Stress.DoubleCost.Metrics
			fmt.Fprintf(&builder, "## 压力测试\n\n双倍费用与滑点收益 %+.2f%%，回撤 %+.2f%%；最佳单笔占全部正收益 %.0f%%。\n\n", m.TotalReturn, m.MaxDrawdown, result.Stress.BestTradeProfitShare*100)
		}
	}
	if result.PriorLessons != "" {
		builder.WriteString("## 历史反思上下文\n\n" + strings.TrimSpace(result.PriorLessons) + "\n\n")
	}
	if len(result.GateReasons) > 0 {
		builder.WriteString("## 未晋级原因\n\n")
		for _, reason := range result.GateReasons {
			fmt.Fprintf(&builder, "- %s\n", reason)
		}
	}
	builder.WriteString("\n## 数据质量门\n\n")
	fmt.Fprintf(&builder, "整体评级：%s；硬门：%v\n\n", result.Quality.Grade, result.Quality.Passed)
	for _, check := range result.Quality.Checks {
		status := "通过"
		if !check.Passed {
			status = "警告"
			if !check.Warning {
				status = "失败"
			}
		}
		fmt.Fprintf(&builder, "- %s：%s，%s\n", check.Name, status, check.Detail)
	}
	if result.SupervisorReview != "" {
		builder.WriteString("\n## 主Agent监督复盘\n\n" + strings.TrimSpace(result.SupervisorReview) + "\n")
	} else if result.SupervisorError != "" {
		builder.WriteString("\n## 主Agent状态\n\n复盘失败：" + result.SupervisorError + "\n")
	}
	builder.WriteString("\n## 候选排名\n\n")
	for index, candidate := range result.Candidates[:min(20, len(result.Candidates))] {
		status := "通过"
		if candidate.Rejected {
			status = "拒绝：" + strings.Join(candidate.Reasons, "；")
		}
		fmt.Fprintf(&builder, "%d. `%s` %s  评分 %.2f  正收益窗口 %.0f%%  交易%d  平均收益 %+.2f%%  最差回撤 %+.2f%%\n",
			index+1, candidate.Proposal.ID, status, candidate.Score, candidate.PositiveFoldRatio*100,
			candidate.ValidationTrades, candidate.AverageValidation, candidate.WorstDrawdown)
	}
	if len(result.Warnings) > 0 {
		builder.WriteString("\n## 使用边界\n\n")
		for _, warning := range result.Warnings {
			fmt.Fprintf(&builder, "- %s\n", warning)
		}
	}
	builder.WriteString("\n> 本实验不会自动交易；模拟观察候选也必须经过真实时间的影子运行后，才能成为操作参考。\n")
	return builder.String()
}
