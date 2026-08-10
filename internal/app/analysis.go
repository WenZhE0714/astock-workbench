package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/analysis"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func (app *App) runAnalyze(ctx context.Context, arguments []string) error {
	set := flag.NewFlagSet("analyze", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	tradeDate := set.String("date", time.Now().Format("2006-01-02"), "分析日期")
	repo := set.String("repo", "", "tradingagents-astock 路径")
	python := set.String("python", "", "Python 路径")
	provider := set.String("provider", "", "LLM provider")
	deepModel := set.String("deep-model", "", "深度模型")
	quickModel := set.String("quick-model", "", "快速模型")
	backendURL := set.String("backend-url", "", "模型 API 地址")
	checkpoint := set.Bool("checkpoint", false, "启用断点续跑")
	full := set.Bool("full", false, "打印完整报告")
	quoteSnapshot := set.Bool("quote", true, "分析前获取实时快照")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	if set.NArg() != 1 {
		return fmt.Errorf("用法: astock analyze [选项] 股票代码或名称（选项放在股票前）")
	}
	if _, err := time.Parse("2006-01-02", *tradeDate); err != nil {
		return fmt.Errorf("--date 必须为 YYYY-MM-DD")
	}
	symbol, err := app.resolver.Resolve(ctx, set.Arg(0))
	if err != nil {
		return err
	}
	name := app.names.LookupName(symbol)
	if *quoteSnapshot {
		quotes, quoteError := app.quotes.Fetch(ctx, []string{symbol})
		if quoteError != nil {
			fmt.Fprintf(app.errOut, "行情快照暂不可用（不影响历史策略分析）: %s\n", quoteError)
		} else if len(quotes) > 0 {
			_ = app.names.Remember(quoteCandidates(quotes))
			name = quotes[0].Name
			fmt.Fprintf(app.out, "实时快照  %s %s  %s  %+.2f%%  %s\n", quotes[0].Code, quotes[0].Name, quotes[0].Current, quotes[0].Percent, quotes[0].QuoteTime)
		}
	}
	fmt.Fprintf(app.errOut, "正在分析 %s %s（这通常会产生多次模型调用）...\n", symbol[2:], name)
	result, err := app.analyzer.Run(ctx, analysis.Options{
		Repo: *repo, Python: *python, WorkDir: app.paths.TradingAgentsDir,
		Ticker: symbol[2:], TradeDate: *tradeDate, Provider: *provider,
		DeepModel: *deepModel, QuickModel: *quickModel, BackendURL: *backendURL,
		Checkpoint: *checkpoint,
	})
	if err != nil {
		return err
	}
	saved, err := app.reports.Save(result)
	if err != nil {
		return err
	}
	if name == "" {
		name = symbol[2:]
	}
	fmt.Fprintf(app.out, "\n分析完成  %s %s\n", result.Ticker, name)
	fmt.Fprintf(app.out, "组合评级  %s（研究信号，不直接触发交易）\n", result.Signal)
	fmt.Fprintf(app.out, "分析日期  %s\n", result.TradeDate)
	fmt.Fprintf(app.out, "模型配置  %s / %s / %s\n", result.Provider, result.DeepModel, result.QuickModel)
	fmt.Fprintf(app.out, "JSON      %s\n", saved.JSONPath)
	fmt.Fprintf(app.out, "Markdown  %s\n", saved.MarkdownPath)
	if *full {
		fmt.Fprintln(app.out)
		fmt.Fprint(app.out, storage.RenderAnalysisMarkdown(result))
	}
	return nil
}

func (app *App) runReports(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 || arguments[0] == "list" {
		if len(arguments) > 1 {
			return fmt.Errorf("用法: astock reports list")
		}
		items, err := app.reports.List(20)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Fprintln(app.out, "尚无策略分析报告。运行: astock analyze 600519")
			return nil
		}
		fmt.Fprintln(app.out, "TICKER  DATE        SIGNAL        CREATED")
		for _, item := range items {
			fmt.Fprintf(app.out, "%-7s %-11s %-13s %s\n", item.Ticker, item.TradeDate, item.Signal, item.CreatedAt)
		}
		return nil
	}
	if arguments[0] != "show" || len(arguments) != 2 {
		return fmt.Errorf("用法: astock reports [list | show 股票代码]")
	}
	symbol, err := app.resolver.Resolve(ctx, arguments[1])
	if err != nil {
		return err
	}
	result, directory, err := app.reports.LoadLatest(symbol[2:])
	if err != nil {
		return err
	}
	fmt.Fprintf(app.out, "报告目录: %s\n\n", directory)
	fmt.Fprint(app.out, storage.RenderAnalysisMarkdown(result))
	return nil
}

func (app *App) runDoctor(ctx context.Context, arguments []string) error {
	set := flag.NewFlagSet("doctor", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	repo := set.String("repo", "", "tradingagents-astock 路径")
	python := set.String("python", "", "Python 路径")
	online := set.Bool("online", false, "同时测试腾讯行情")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return fmt.Errorf("doctor 不接受位置参数")
	}
	fmt.Fprintf(app.out, "astock-workbench %s\n", version)
	fmt.Fprintf(app.out, "Go runtime       %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(app.out, "Watchlist        %s\n", app.paths.WatchlistFile)
	fmt.Fprintf(app.out, "Reports          %s\n", app.paths.ReportsDir)
	check, err := app.analyzer.Check(ctx, analysis.Options{Repo: *repo, Python: *python, WorkDir: app.paths.TradingAgentsDir})
	if err != nil {
		fmt.Fprintf(app.out, "TradingAgents    unavailable\n")
		return err
	}
	fmt.Fprintf(app.out, "TradingAgents    %s %s\n", check.Engine.Version, check.Engine.RepoPath)
	fmt.Fprintf(app.out, "Python           %s\n", check.PythonVersion)
	fmt.Fprintf(app.out, "LLM provider     %s\n", check.Provider)
	if check.CredentialEnv != "" {
		status := "未检测到"
		if check.CredentialSet {
			status = "已配置"
		}
		fmt.Fprintf(app.out, "Credential       %s (%s)\n", status, check.CredentialEnv)
	}
	if *online {
		quotes, quoteError := app.quotes.Fetch(ctx, []string{"sh600519"})
		if quoteError != nil {
			return fmt.Errorf("腾讯行情测试失败: %w", quoteError)
		}
		fmt.Fprintf(app.out, "Tencent quote    ok (%s %s)\n", quotes[0].Code, quotes[0].Current)
	}
	if check.CredentialEnv != "" && !check.CredentialSet {
		fmt.Fprintf(app.out, "\n提示: 实时看盘可正常使用；analyze 前需在 %s/.env 或环境变量配置模型凭证。\n", strings.TrimSuffix(check.Engine.RepoPath, string(os.PathSeparator)))
	}
	return nil
}
