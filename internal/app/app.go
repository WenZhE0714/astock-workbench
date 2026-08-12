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
	version     = "0.12.2"
	maxStocks   = 50
)

type App struct {
	out            io.Writer
	errOut         io.Writer
	paths          storage.Paths
	names          *storage.NameCache
	resolver       *market.Resolver
	quotes         market.QuoteClient
	flows          market.FundFlowClient
	industryFlows  market.IndustryFlowClient
	boards         market.BoardFlowClient
	dragonTiger    market.DragonTigerClient
	amounts        market.MarketAmountClient
	history        market.DailyHistoryClient
	rankings       market.MarketRankingClient
	marketScan     market.MarketScanClient
	news           market.StockNewsClient
	scanHistory    market.DailyHistoryClient
	analyzer       *analysis.Runner
	marketReportAI analysis.TextSynthesizer
	reports        *storage.ReportStore
	marketReports  *storage.MarketReportStore
	stockReports   *storage.StockReportStore
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
	primaryHistory := market.EastmoneyClient{}
	primaryScanHistory := market.TencentClient{}
	return &App{
		out:            output,
		errOut:         errorOutput,
		paths:          paths,
		names:          names,
		resolver:       market.NewResolver(names),
		quotes:         market.TencentClient{},
		flows:          market.EastmoneyClient{},
		industryFlows:  market.EastmoneyClient{},
		boards:         market.EastmoneyClient{},
		dragonTiger:    market.EastmoneyClient{},
		amounts:        market.SinaAmountClient{},
		history:        market.NewCachedDailyHistoryClient(primaryHistory, filepath.Join(paths.CacheDir, "daily-history")),
		rankings:       market.EastmoneyClient{},
		marketScan:     market.EastmoneyClient{},
		news:           market.EastmoneyClient{},
		scanHistory:    market.NewCachedDailyHistoryClient(primaryScanHistory, filepath.Join(paths.CacheDir, "daily-history")),
		analyzer:       analysis.NewRunner(errorOutput),
		marketReportAI: analysis.NewCodexRunner(""),
		reports:        storage.NewReportStore(paths.ReportsDir),
		marketReports:  storage.NewMarketReportStore(paths.MarketReportsDir),
		stockReports:   storage.NewStockReportStore(paths.StockReportsDir),
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
	case "scan", "market-report":
		return app.runMarketReport(ctx, rest)
	case "stock-report", "insight":
		return app.runStockReport(ctx, rest)
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
  astock scan [--full] [--no-ai]
  astock stock-report [--full] [--no-ai] 股票代码或名称
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
  d                       从当前分组移出所选自选（“全部”中为全局删除，需确认）
  i                       输入代码或名称查看股票详情
  h                       打开最近查看历史，选择后恢复股票详情
  1/2/3                   涨幅榜/跌幅榜/快速涨幅榜前 20（含行业板块）
  v                       监视当前自选组或榜单的主力资金动向
  s                       后台生成大盘、板块与智能选股报告
  r                       查看最近生成的智能市场报告
  c                       后台生成所选股票的多维 AI 研判
  o                       查看所选股票最近的多维研判
  e                       排序：选择股票后 Enter 锁定，方向键移动，Enter 保存
  f                       分组列表：Enter切换、n新建、d删除
  m                       为所选股票多选分组：空格勾选，Enter 保存
  [/] 或 PgUp/PgDn 或 b/空格 列表跳选/详情翻页（分组分配时空格用于勾选）
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
  astock stock-report --full 600519

基础看盘无需 Python 或模型。analyze 会调用独立的 TradingAgents Python 环境；scan 默认调用
本机 Codex CLI，只读综合失败时自动保存确定性量价回退报告；stock-report 使用同一只读综合器
生成个股多维研判。`
