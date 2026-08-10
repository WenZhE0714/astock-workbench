package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/wenzhe/astock-workbench/internal/analysis"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

const (
	programName = "astock"
	version     = "0.5.0"
	maxStocks   = 50
)

type App struct {
	out         io.Writer
	errOut      io.Writer
	paths       storage.Paths
	names       *storage.NameCache
	resolver    *market.Resolver
	quotes      market.QuoteClient
	flows       market.FundFlowClient
	boards      market.BoardFlowClient
	dragonTiger market.DragonTigerClient
	amounts     market.PreviousAmountClient
	analyzer    *analysis.Runner
	reports     *storage.ReportStore
}

func New(output, errorOutput io.Writer) (*App, error) {
	paths, err := storage.ResolvePaths()
	if err != nil {
		return nil, err
	}
	names, err := storage.LoadNameCache(paths.NameCacheFile)
	if err != nil {
		return nil, err
	}
	return &App{
		out:         output,
		errOut:      errorOutput,
		paths:       paths,
		names:       names,
		resolver:    market.NewResolver(names),
		quotes:      market.TencentClient{},
		flows:       market.EastmoneyClient{},
		boards:      market.EastmoneyClient{},
		dragonTiger: market.EastmoneyClient{},
		amounts:     market.EastmoneyClient{},
		analyzer:    analysis.NewRunner(errorOutput),
		reports:     storage.NewReportStore(paths.ReportsDir),
	}, nil
}

func (app *App) PrintError(err error) {
	fmt.Fprintf(app.errOut, "%s: %s\n", programName, err)
	var ambiguous *market.AmbiguousNameError
	if errors.As(err, &ambiguous) {
		for _, item := range ambiguous.Candidates {
			fmt.Fprintf(app.errOut, "  %s  %s  %s\n", item.Symbol[2:], item.Name, market.MarketText(item.Symbol))
		}
	}
}

func (app *App) Run(ctx context.Context, arguments []string) error {
	if len(arguments) == 0 {
		return app.runWatch(ctx, nil)
	}
	command := arguments[0]
	rest := arguments[1:]
	switch command {
	case "watch":
		return app.runWatch(ctx, rest)
	case "add":
		return app.runWatchlistMutation(ctx, "add", rest)
	case "remove", "rm":
		return app.runWatchlistMutation(ctx, "remove", rest)
	case "list", "ls":
		return app.runWatchlistList(rest)
	case "analyze", "analyse":
		return app.runAnalyze(ctx, rest)
	case "reports", "report":
		return app.runReports(ctx, rest)
	case "doctor":
		return app.runDoctor(ctx, rest)
	case "paper":
		return app.runPaper(rest)
	case "backtest":
		return app.runBacktest(rest)
	case "version", "-V", "--version":
		fmt.Fprintf(app.out, "%s %s\n", programName, version)
		return nil
	case "help", "-h", "--help":
		fmt.Fprintln(app.out, usageText)
		return nil
	case "-a", "--add":
		return app.runWatchlistMutation(ctx, "add", rest)
	case "-r", "--remove":
		return app.runWatchlistMutation(ctx, "remove", rest)
	case "-l", "--list":
		return app.runWatchlistList(rest)
	default:
		// Backwards-compatible shorthand: `astock 600519 --once` and
		// `astock --moyu 贵州茅台` both mean `astock watch ...`.
		return app.runWatch(ctx, arguments)
	}
}

func executableName() string {
	return filepath.Base(os.Args[0])
}

const usageText = `A 股实时行情与策略研究工作台

用法:
  astock watch [选项] [股票代码或名称 ...]
  astock add 股票代码或名称 ...
  astock remove 股票代码或名称 ...
  astock list
  astock analyze [选项] 股票代码或名称
  astock reports [list | show 股票代码]
  astock doctor [选项]

实时行情:
  -1, --once              只获取一次，不持续刷新
  -i, --interval 秒数     刷新间隔，默认 1 秒
  -d, --depth             显示买卖五档盘口
  -m, --moyu              无色带框摸鱼表格
  -p, --pinyin            股票名称显示无声调拼音，自动开启 --moyu
      --no-color          禁用红涨绿跌颜色

终端导航（持续模式）:
  ↑/↓ 或 j/k              选择上一只/下一只股票
	Enter                   打开所选股票完整详情
	Esc                     从详情返回股票列表
	a                       输入代码或名称添加自选
	d                       删除当前选中的自选（需确认）
	i                       输入代码或名称查看股票详情
	PgUp/PgDn 或 b/空格     列表跳选/详情翻页
  g/G                     选择第一只/最后一只
  q 或 Ctrl-C             退出并恢复原终端

策略分析:
      --date YYYY-MM-DD   分析日期，默认今天
      --repo 路径         tradingagents-astock 项目路径
      --python 路径       其 Python >= 3.10 解释器
      --provider 名称     覆盖 LLM provider
      --deep-model 名称   覆盖深度模型
      --quick-model 名称  覆盖快速模型
      --backend-url URL   覆盖模型 API 地址
      --checkpoint        启用 TradingAgents 断点续跑
      --full              完成后在终端打印完整报告

示例:
  astock watch --moyu 贵州茅台 平安银行
  astock watch --pinyin 600519 000001
  astock add 贵州茅台 宁德时代
  astock remove 宁德时代
  astock analyze 600519
  astock analyze --provider deepseek --deep-model deepseek-chat --quick-model deepseek-chat 600519
  astock reports show 600519

行情无需 Python 或模型。analyze 才会调用独立的 TradingAgents Python 环境。`
