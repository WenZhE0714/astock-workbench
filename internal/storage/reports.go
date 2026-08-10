package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

var safeIDPattern = regexp.MustCompile(`[^0-9A-Za-z_-]+`)

type SavedReport struct {
	Directory    string
	JSONPath     string
	MarkdownPath string
}

type ReportStore struct {
	root string
}

func NewReportStore(root string) *ReportStore {
	return &ReportStore{root: root}
}

func safeReportID(result domain.AnalysisResult) string {
	value := safeIDPattern.ReplaceAllString(result.ID, "-")
	value = strings.Trim(value, "-")
	if value != "" {
		return value
	}
	return time.Now().Format("20060102T150405")
}

func (store *ReportStore) Save(result domain.AnalysisResult) (SavedReport, error) {
	if len(result.Ticker) != 6 {
		return SavedReport{}, fmt.Errorf("无效分析代码 %q", result.Ticker)
	}
	directory := filepath.Join(store.root, result.Ticker, safeReportID(result))
	jsonPath := filepath.Join(directory, "analysis.json")
	markdownPath := filepath.Join(directory, "report.md")
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return SavedReport{}, err
	}
	if err := atomicWrite(jsonPath, append(data, '\n'), 0o600); err != nil {
		return SavedReport{}, err
	}
	if err := atomicWrite(markdownPath, []byte(RenderAnalysisMarkdown(result)), 0o600); err != nil {
		return SavedReport{}, err
	}
	return SavedReport{Directory: directory, JSONPath: jsonPath, MarkdownPath: markdownPath}, nil
}

var reportSections = []struct {
	Key   string
	Title string
}{
	{"market_report", "市场与技术面"},
	{"sentiment_report", "市场情绪"},
	{"news_report", "新闻与事件"},
	{"fundamentals_report", "基本面"},
	{"policy_report", "政策环境"},
	{"hot_money_report", "资金与游资"},
	{"lockup_report", "解禁与股东行为"},
	{"data_quality_summary", "数据质量"},
	{"investment_plan", "研究团队投资计划"},
	{"research_manager", "研究经理结论"},
	{"trader_investment_plan", "交易员计划"},
	{"portfolio_manager", "组合经理结论"},
	{"final_trade_decision", "最终组合决策"},
}

func RenderAnalysisMarkdown(result domain.AnalysisResult) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# A 股策略分析：%s\n\n", result.Ticker)
	fmt.Fprintf(&builder, "- 分析日期：%s\n", result.TradeDate)
	fmt.Fprintf(&builder, "- 生成时间：%s\n", result.CreatedAt)
	fmt.Fprintf(&builder, "- 组合评级：**%s**\n", result.Signal)
	fmt.Fprintf(&builder, "- 模型：%s / %s / %s\n", result.Provider, result.DeepModel, result.QuickModel)
	fmt.Fprintf(&builder, "- 分析引擎：%s %s\n\n", result.Engine.Name, result.Engine.Version)
	if result.Disclaimer != "" {
		fmt.Fprintf(&builder, "> %s\n\n", result.Disclaimer)
	}
	seen := make(map[string]bool)
	for _, section := range reportSections {
		content := strings.TrimSpace(result.Reports[section.Key])
		if content == "" {
			continue
		}
		seen[section.Key] = true
		fmt.Fprintf(&builder, "## %s\n\n%s\n\n", section.Title, content)
	}
	keys := make([]string, 0)
	for key, content := range result.Reports {
		if !seen[key] && strings.TrimSpace(content) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&builder, "## %s\n\n%s\n\n", key, strings.TrimSpace(result.Reports[key]))
	}
	return builder.String()
}

func (store *ReportStore) LoadLatest(ticker string) (domain.AnalysisResult, string, error) {
	directory := filepath.Join(store.root, ticker)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return domain.AnalysisResult{}, "", err
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return domain.AnalysisResult{}, "", fmt.Errorf("没有 %s 的已保存分析", ticker)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	path := filepath.Join(directory, names[0], "analysis.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.AnalysisResult{}, "", err
	}
	var result domain.AnalysisResult
	if err := json.Unmarshal(data, &result); err != nil {
		return domain.AnalysisResult{}, "", err
	}
	return result, filepath.Dir(path), nil
}

type ReportIndexEntry struct {
	Ticker    string
	ID        string
	TradeDate string
	Signal    string
	CreatedAt string
}

func (store *ReportStore) List(limit int) ([]ReportIndexEntry, error) {
	tickers, err := os.ReadDir(store.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]ReportIndexEntry, 0)
	for _, ticker := range tickers {
		if !ticker.IsDir() {
			continue
		}
		runs, err := os.ReadDir(filepath.Join(store.root, ticker.Name()))
		if err != nil {
			continue
		}
		for _, run := range runs {
			if !run.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(store.root, ticker.Name(), run.Name(), "analysis.json"))
			if err != nil {
				continue
			}
			var item domain.AnalysisResult
			if json.Unmarshal(data, &item) != nil {
				continue
			}
			result = append(result, ReportIndexEntry{Ticker: item.Ticker, ID: item.ID, TradeDate: item.TradeDate, Signal: item.Signal, CreatedAt: item.CreatedAt})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
