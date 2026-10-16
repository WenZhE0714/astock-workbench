package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
)

type offlineStudyProvider struct {
	cache, recent *market.CachedDailyHistoryClient
}

func (p offlineStudyProvider) FetchDailyBarsRange(ctx context.Context, symbol string, start, end time.Time, adjustment backtest.PriceAdjustment) ([]domain.DailyBar, error) {
	bars, err := p.cache.ReadCachedDailyBarsRange(ctx, symbol, start, end, adjustment)
	if err == nil || ctx.Err() != nil || p.recent == nil {
		return bars, err
	}
	return p.recent.ReadCachedDailyBarsRange(ctx, symbol, start, end, adjustment)
}

func (app *App) runBacktestPositionStudy(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("backtest position-study", flag.ContinueOnError)
	flags.SetOutput(app.errOut)
	output := flags.String("output", "", "新建研究输出目录")
	capsFlag := flags.String("caps", "0.20,0.15,0.10", "固定单股仓位上限")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return fmt.Errorf("用法: astock backtest position-study [--output 新目录] 实验ID 候选ID")
	}
	source, err := app.continuousOptimizationStore().Load(flags.Arg(0))
	if err != nil {
		return err
	}
	caps := []float64{}
	for _, raw := range strings.Split(*capsFlag, ",") {
		cap, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return fmt.Errorf("仓位列表无效")
		}
		caps = append(caps, cap)
	}
	dir := *output
	if dir == "" {
		dir = filepath.Join(app.paths.BacktestsDir, "position-studies", "POS-"+time.Now().Format("20060102T150405"))
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("输出目录已存在，使用新目录以保留研究证据")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tickers := append([]string(nil), source.Request.BaseRequest.Tickers...)
	if source.Request.BaseRequest.Benchmark != "" {
		tickers = append(tickers, source.Request.BaseRequest.Benchmark)
	}
	frozen := filepath.Join(dir, "cache")
	files := map[string]string{}
	for _, sourceName := range []string{"daily-history-execution", "daily-history-scan"} {
		copied, err := freezePositionStudyCache(filepath.Join(app.paths.CacheDir, sourceName), filepath.Join(frozen, sourceName), tickers)
		if err != nil {
			return err
		}
		for file, hash := range copied {
			files[filepath.Join(sourceName, file)] = hash
		}
	}
	plan := struct {
		Source    backtest.ContinuousOptimizationResult `json:"source"`
		Candidate string                                `json:"candidate"`
		Caps      []float64                             `json:"caps"`
		Files     map[string]string                     `json:"cache_file_sha256"`
		CreatedAt time.Time                             `json:"created_at"`
	}{source, flags.Arg(1), caps, files, time.Now()}
	if err := writeStudyJSON(filepath.Join(dir, "plan.json"), plan); err != nil {
		return err
	}
	provider := offlineStudyProvider{cache: market.NewCachedDailyHistoryClient(nil, filepath.Join(frozen, "daily-history-execution")), recent: market.NewCachedDailyHistoryClient(nil, filepath.Join(frozen, "daily-history-scan"))}
	engine := backtest.NewDailyEngine(backtest.NewCachingDailyBarProvider(provider))
	result, err := backtest.RunPositionStudy(ctx, engine, source, flags.Arg(1), caps, func(message string) { fmt.Fprintln(app.errOut, message) })
	if saveErr := writeStudyJSON(filepath.Join(dir, "result.json"), result); saveErr != nil {
		return saveErr
	}
	if err != nil {
		if saveErr := writeStudyJSON(filepath.Join(dir, "error.json"), map[string]string{"error": err.Error()}); saveErr != nil {
			return fmt.Errorf("%v；保存错误记录: %w", err, saveErr)
		}
		return fmt.Errorf("%w；已保留输入于%s", err, dir)
	}
	var report strings.Builder
	fmt.Fprintf(&report, "# 仓位对照研究\n\n源实验：%s；候选：%s。\n\n原基线复现：%v。仅离线研究，不自动晋级；近期区间为已见数据诊断。\n\n| 单股上限 | 平均验证收益 | 最差回撤 | 正收益窗口 | 交易 | 常规门禁 | 双倍成本平均收益 | 双倍成本门禁 |\n|---|---:|---:|---:|---:|---|---:|---|\n", result.SourceID, result.CandidateID, result.SourceBaselineReproduced)
	for _, variant := range result.Variants {
		n, s := variant.Normal, variant.DoubleCost
		fmt.Fprintf(&report, "| %.0f%% | %+.2f%% | %.2f%% | %.0f%% | %d | %v | %+.2f%% | %v |\n", variant.MaxPosition*100, n.AverageReturn, n.WorstDrawdown, n.PositiveRatio*100, n.Trades, n.Passed, s.AverageReturn, s.Passed)
	}
	report.WriteString("\n## 窗口明细\n\n| 单股上限 | 窗口 | 收益 | 回撤 | 交易 | 双倍成本收益 |\n|---|---|---:|---:|---:|---:|\n")
	for _, variant := range result.Variants {
		for _, window := range append(append([]backtest.PositionStudyWindow{}, variant.Windows...), variant.Diagnostic) {
			fmt.Fprintf(&report, "| %.0f%% | %s | %+.2f%% | %.2f%% | %d | %+.2f%% |\n", variant.MaxPosition*100, window.ID, window.Normal.Metrics.TotalReturn, window.Normal.Metrics.MaxDrawdown, window.Normal.Metrics.Trades, window.DoubleCost.Metrics.TotalReturn)
		}
	}
	report.WriteString("\n## 限制与差异\n\n")
	for _, warning := range append(result.Warnings, result.BaselineDifferences...) {
		fmt.Fprintf(&report, "- %s\n", warning)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.md"), []byte(report.String()), 0o600); err != nil {
		return err
	}
	fmt.Fprintln(app.out, report.String())
	fmt.Fprintf(app.out, "报告：%s\n", filepath.Join(dir, "report.md"))
	return nil
}

func freezePositionStudyCache(source, target string, tickers []string) (map[string]string, error) {
	files := map[string]string{}
	for _, symbol := range tickers {
		if len(symbol) != 8 || strings.ContainsAny(symbol, `/\\.`) {
			return nil, fmt.Errorf("无效研究证券代码")
		}
		paths := []string{symbol + ".json"}
		rangeDir := filepath.Join("ranges", "none", symbol)
		entries, err := os.ReadDir(filepath.Join(source, rangeDir))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
				paths = append(paths, filepath.Join(rangeDir, entry.Name()))
			}
		}
		for _, relative := range paths {
			data, err := os.ReadFile(filepath.Join(source, relative))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(target, relative)), 0o700); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(target, relative), data, 0o600); err != nil {
				return nil, err
			}
			hash := sha256.Sum256(data)
			files[relative] = hex.EncodeToString(hash[:])
		}
	}
	return files, nil
}

func writeStudyJSON(file string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(data, '\n'), 0o600)
}
