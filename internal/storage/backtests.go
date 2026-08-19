package storage

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

type BacktestStore struct {
	root string
}

type BacktestIndexEntry struct {
	RunID       string
	GeneratedAt time.Time
	Strategy    string
	Trades      int
	TotalReturn float64
	MaxDrawdown float64
	Directory   string
}

func NewBacktestStore(root string) *BacktestStore { return &BacktestStore{root: root} }

func (store *BacktestStore) Save(result backtest.Result) (backtest.Result, error) {
	if result.GeneratedAt.IsZero() {
		result.GeneratedAt = time.Now()
	}
	if strings.TrimSpace(result.RunID) == "" {
		result.RunID = result.GeneratedAt.Format("20060102T150405")
	}
	runID := result.RunID
	directory := filepath.Join(store.root, runID)
	for index := 1; ; index++ {
		if _, err := os.Stat(directory); os.IsNotExist(err) {
			break
		} else if err != nil {
			return result, err
		}
		directory = filepath.Join(store.root, fmt.Sprintf("%s-%02d", runID, index))
		result.RunID = filepath.Base(directory)
	}
	if err := writeBacktestArtifacts(directory, result); err != nil {
		return result, err
	}
	result.Directory = directory
	result.ReportPath = filepath.Join(directory, "report.md")
	return result, nil
}

func writeBacktestArtifacts(directory string, result backtest.Result) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(directory, "summary.json"), append(data, '\n'), 0o600); err != nil {
		return err
	}
	strategyData, err := json.MarshalIndent(result.Request, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(directory, "strategy.json"), append(strategyData, '\n'), 0o600); err != nil {
		return err
	}
	report := renderBacktestMarkdown(result)
	if err := atomicWrite(filepath.Join(directory, "report.md"), []byte(report), 0o600); err != nil {
		return err
	}
	if err := writeTrades(filepath.Join(directory, "trades.jsonl"), result.Trades); err != nil {
		return err
	}
	if err := writeEquity(filepath.Join(directory, "equity.csv"), result.Equity); err != nil {
		return err
	}
	return nil
}

func (store *BacktestStore) Load(runID string) (backtest.Result, error) {
	if strings.TrimSpace(runID) == "" || strings.Contains(runID, string(filepath.Separator)) || strings.Contains(runID, "..") {
		return backtest.Result{}, fmt.Errorf("无效回测运行 ID %q", runID)
	}
	path := filepath.Join(store.root, runID, "summary.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return backtest.Result{}, fmt.Errorf("未找到回测运行 %s", runID)
	}
	if err != nil {
		return backtest.Result{}, err
	}
	var result backtest.Result
	if err := json.Unmarshal(data, &result); err != nil {
		return backtest.Result{}, err
	}
	result.Directory = filepath.Dir(path)
	result.ReportPath = filepath.Join(result.Directory, "report.md")
	return result, nil
}

func (store *BacktestStore) List(limit int) ([]BacktestIndexEntry, error) {
	entries, err := os.ReadDir(store.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]BacktestIndexEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		loaded, loadError := store.Load(entry.Name())
		if loadError != nil {
			continue
		}
		result = append(result, BacktestIndexEntry{
			RunID: loaded.RunID, GeneratedAt: loaded.GeneratedAt, Strategy: loaded.Request.Strategy,
			Trades: loaded.Metrics.Trades, TotalReturn: loaded.Metrics.TotalReturn,
			MaxDrawdown: loaded.Metrics.MaxDrawdown, Directory: loaded.Directory,
		})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].GeneratedAt.After(result[right].GeneratedAt) })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func writeTrades(path string, trades []backtest.Trade) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".trades-*")
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
	for _, trade := range trades {
		data, marshalError := json.Marshal(trade)
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

func writeEquity(path string, points []backtest.EquityPoint) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".equity-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"date", "cash", "positions", "equity", "drawdown_percent"}); err != nil {
		file.Close()
		return err
	}
	for _, point := range points {
		if err := writer.Write([]string{point.Date, strconv.FormatFloat(point.Cash, 'f', 2, 64), strconv.FormatFloat(point.Positions, 'f', 2, 64), strconv.FormatFloat(point.Equity, 'f', 2, 64), strconv.FormatFloat(point.Drawdown, 'f', 4, 64)}); err != nil {
			file.Close()
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func renderBacktestMarkdown(result backtest.Result) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# A股回测报告：%s\n\n", result.Request.Strategy)
	fmt.Fprintf(&builder, "- 运行 ID：`%s`\n- 区间：%s 至 %s\n- 股票：%s\n- 数据口径：%s\n- 基准：%s\n\n", result.RunID, result.Request.Start.Format("2006-01-02"), result.Request.End.Format("2006-01-02"), strings.Join(result.Request.Tickers, ", "), result.Request.Adjustment, result.Request.Benchmark)
	builder.WriteString("## 绩效摘要\n\n")
	fmt.Fprintf(&builder, "收益率：**%+.2f%%**  年化：%+.2f%%  最大回撤：%+.2f%%  夏普：%.2f\n\n", result.Metrics.TotalReturn, result.Metrics.AnnualizedReturn, result.Metrics.MaxDrawdown, result.Metrics.Sharpe)
	if result.Metrics.BenchmarkAvailable {
		fmt.Fprintf(&builder, "基准收益：%+.2f%%  超额收益：%+.2f%%\n\n", result.Metrics.BenchmarkReturn, result.Metrics.ExcessReturn)
	}
	fmt.Fprintf(&builder, "交易：%d 笔，胜率 %.2f%%，盈亏比 %.2f，平均持有 %.1f 天，换手 %.2f%%，费用 %.2f 元\n\n", result.Metrics.Trades, result.Metrics.WinRate, result.Metrics.ProfitFactor, result.Metrics.AverageHoldingDays, result.Metrics.Turnover, result.Metrics.TotalFees)
	builder.WriteString("## 交易记录\n\n| ID | 股票 | 买入 | 卖出 | 净收益 | 收益率 | 持有天数 | 退出原因 |\n|---|---|---|---|---:|---:|---:|---|\n")
	for _, trade := range result.Trades {
		fmt.Fprintf(&builder, "| %s | %s %s | %s %.2f | %s %.2f | %+.2f | %+.2f%% | %d | %s |\n", trade.ID, displayStockCode(trade.Symbol), trade.Name, trade.Entry.Date, trade.Entry.Price, trade.Exit.Date, trade.Exit.Price, trade.NetProfit, trade.ReturnPercent, trade.HoldingDays, trade.ExitReason)
	}
	if len(result.OpenPositions) > 0 {
		builder.WriteString("\n## 未平仓\n\n")
		for _, position := range result.OpenPositions {
			fmt.Fprintf(&builder, "- %s %s：%d股，最后价 %.2f，浮动收益 %+.2f（%+.2f%%）\n", displayStockCode(position.Symbol), position.Name, position.Quantity, position.LastPrice, position.UnrealizedProfit, position.ReturnPercent)
		}
	}
	if len(result.Warnings) > 0 {
		builder.WriteString("\n## 回测限制\n\n")
		for _, warning := range result.Warnings {
			fmt.Fprintf(&builder, "- %s\n", warning)
		}
	}
	builder.WriteString("\n> 回测是历史模拟，不代表未来收益，不触发自动交易。\n")
	return builder.String()
}

func displayStockCode(symbol string) string {
	if len(symbol) > 2 && (strings.HasPrefix(symbol, "sh") || strings.HasPrefix(symbol, "sz") || strings.HasPrefix(symbol, "bj")) {
		return symbol[2:]
	}
	return symbol
}
