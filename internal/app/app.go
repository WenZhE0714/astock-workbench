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
	version     = "0.21.0"
	maxStocks   = 50
)

type App struct {
	out             io.Writer
	errOut          io.Writer
	paths           storage.Paths
	names           *storage.NameCache
	resolver        *market.Resolver
	quotes          market.QuoteClient
	httpQuotes      market.QuoteClient
	flows           market.FundFlowClient
	industryFlows   market.IndustryFlowClient
	boards          market.BoardFlowClient
	dragonTiger     market.DragonTigerClient
	amounts         market.MarketAmountClient
	globalMarkets   market.GlobalIndexClient
	history         market.DailyHistoryClient
	httpHistory     market.DailyHistoryClient
	minutes         market.MinuteClient
	httpMinutes     market.MinuteClient
	rankings        market.MarketRankingClient
	marketScan      market.MarketScanClient
	industryLeaders market.IndustryLeaderClient
	news            market.StockNewsClient
	research        market.BrokerResearchClient
	scanHistory     market.DailyHistoryClient
	analyzer        *analysis.Runner
	marketReportAI  analysis.TextSynthesizer
	reports         *storage.ReportStore
	marketReports   *storage.MarketReportStore
	stockReports    *storage.StockReportStore
	aiChats         *storage.AIChatStore
	marketSource    string
	tdxMarket       *market.TDXClient
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
	quoteClient := market.TencentClient{}
	historyClient := market.NewCachedDailyHistoryClient(primaryHistory, filepath.Join(paths.CacheDir, "daily-history"))
	return &App{
		out:             output,
		errOut:          errorOutput,
		paths:           paths,
		names:           names,
		resolver:        market.NewResolver(names),
		quotes:          quoteClient,
		httpQuotes:      quoteClient,
		flows:           market.EastmoneyClient{},
		industryFlows:   market.EastmoneyClient{},
		boards:          market.EastmoneyClient{},
		dragonTiger:     market.EastmoneyClient{},
		amounts:         market.SinaAmountClient{},
		globalMarkets:   market.SinaGlobalIndexClient{},
		history:         historyClient,
		httpHistory:     historyClient,
		minutes:         market.TencentClient{},
		httpMinutes:     market.TencentClient{},
		rankings:        market.EastmoneyClient{},
		marketScan:      market.EastmoneyClient{},
		industryLeaders: market.EastmoneyClient{},
		news:            market.EastmoneyClient{},
		research:        market.EastmoneyClient{},
		scanHistory:     market.NewCachedDailyHistoryClient(primaryScanHistory, filepath.Join(paths.CacheDir, "daily-history")),
		analyzer:        analysis.NewRunner(errorOutput),
		marketReportAI:  analysis.NewCodexRunner(""),
		reports:         storage.NewReportStore(paths.ReportsDir),
		marketReports:   storage.NewMarketReportStore(paths.MarketReportsDir),
		stockReports:    storage.NewStockReportStore(paths.StockReportsDir),
		aiChats:         storage.NewAIChatStore(paths.AIChatsDir),
		marketSource:    "http",
	}, nil
}

// Close releases optional long-lived data-source processes such as the TDX
// TCP bridge. HTTP-only clients do not need explicit shutdown.
func (app *App) Close() {
	seen := make(map[io.Closer]bool)
	clients := []any{app.quotes, app.history, app.minutes}
	if app.tdxMarket != nil {
		clients = append(clients, app.tdxMarket)
	}
	for _, client := range clients {
		closer, ok := client.(io.Closer)
		if !ok || seen[closer] {
			continue
		}
		seen[closer] = true
		_ = closer.Close()
	}
}

func (app *App) PrintError(err error) {
	fmt.Fprintf(app.errOut, "%s: %s\n", programName, err)
	var ambiguous *market.AmbiguousNameError
	if errors.As(err, &ambiguous) {
		for _, item := range ambiguous.Candidates {
			fmt.Fprintf(app.errOut, "  %s  %s  %s\n", candidateDisplayCode(item.Symbol), item.Name, market.MarketText(item.Symbol))
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
		return app.runBacktest(ctx, rest)
	case "web":
		return app.runWeb(ctx, rest)
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
	astock web [--listen 地址] [--symbol 代码或名称]
	astock backtest run [选项] 股票代码或名称 ...
  astock backtest optimize [选项] 股票代码或名称 ...
  astock backtest continuous [选项] 股票代码或名称 ...
  astock backtest [list | show | trades | trade | optimize-list | optimize-show | continuous-list | continuous-show]
  astock doctor [选项]

实时行情:
  -1, --once              只获取一次，不持续刷新
	-i, --interval 秒数     刷新间隔，默认 1 秒
	    --source http|tdx    行情源；CLI 默认 tdx，失败时自动回退 HTTP
  -d, --depth             显示买卖五档盘口
  -m, --moyu              无色带框摸鱼表格
  -p, --pinyin            股票、板块和外盘指数显示无声调拼音，自动开启 --moyu
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
  y                       查看行业板块主力流入/流出 Top 5 及成交额龙头
  w                       查看港股、日本、韩国和美国主要指数
  t                       打开统一策略研究中心（回测、优化、历史与交易记录）
  x                       查看该股历史咨询或继续咨询AI，自动带入当前多维数据
  s                       后台生成大盘、板块与智能选股报告
  r                       查看最近生成的智能市场报告
  c                       后台生成所选股票的多维 AI 研判
  o                       查看所选股票最近的多维研判
	                          在市场报告或个股研判详情中按 h 查看按日期/时间归档的历史
  e                       排序：选择股票后 Enter 锁定，方向键移动，Enter 保存
  f                       分组列表：Enter切换、n新建、d删除
  m                       为所选股票多选分组：空格勾选，Enter 保存
  [/] 或 PgUp/PgDn 或 b/空格 列表跳选/详情翻页（分组分配时空格用于勾选）
  g/G                     选择第一只/最后一只
	q 或 Ctrl-C             退出并恢复原终端

	本地 Web:
	astock web              启动行情 Web（默认打开自选第一只，监听 127.0.0.1:8765）
	--listen 地址           修改 Web 监听地址
	--symbol 代码或名称     设置页面首次打开的股票
	--source http|tdx       选择 HTTP 或通达信 TCP 行情源

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

日线回测:
      --start/--end 日期  回测区间，默认最近 3 年至昨天
      --cash 金额         初始资金，默认 100 万元
      --fast-ma/--slow-ma 快慢均线周期，默认 20/60
      --breakout-days N   突破回看周期，默认 20 日
      --volume-ratio N    放量门槛，默认前 20 日均量的 1.2 倍
      --stop-loss 比例    止损比例，默认 0.08
      --take-profit 比例  止盈比例，默认 0.20
      --max-position 比例 单股最大仓位，默认 0.20
      --slippage-bps N    单边滑点，默认 5 基点

策略优化:
      --train-start/end       训练区间，必须指定
      --validate-start/end    验证区间，必须晚于训练且不重叠
      --oos-start/end         样本外区间，必须晚于验证且不重叠
      --max-candidates N      受控候选数，默认 30，最多 200
      --min-validation-trades N 验证集最低交易数，默认 3
      --max-validation-drawdown N 验证集最大允许回撤，默认 30%
      --no-ai                 跳过 Codex 只读复盘

多Agent持续优化:
      --folds N               滚动验证折数，默认 4，范围 2-12
      --train-years N         每折训练年数，默认 4
      --validate-years N      每折验证年数，默认 1
      --holdout-months N      最终留出月数，默认 3
      --min-validation-trades N 全部滚动验证最低交易数，默认 20
      --min-positive-fold-ratio N 正收益验证窗口比例，默认 0.67
      --max-validation-drawdown N 验证窗口最大回撤，默认 15%
      --no-ai                 只运行确定性候选，不调用子Agent和主Agent复盘

示例:
  astock watch --moyu 贵州茅台 平安银行
  astock watch --pinyin 600519 000001
  astock add 贵州茅台 宁德时代
  astock remove 宁德时代
  astock analyze 600519
  astock analyze --provider deepseek --deep-model deepseek-chat --quick-model deepseek-chat 600519
  astock reports show 600519
	astock stock-report --full 600519
	astock web --listen 127.0.0.1:8765 600519
  astock backtest run --start 2022-01-01 --end 2025-12-31 600519 000001
  astock backtest trades <run-id>
  astock backtest optimize --train-start 2018-01-01 --train-end 2021-12-31 \
    --validate-start 2022-01-01 --validate-end 2023-12-31 \
    --oos-start 2024-01-01 --oos-end 2025-12-31 600519 000001
  astock backtest continuous 600519 000001 300750

基础看盘无需 Python 或模型。analyze 会调用独立的 TradingAgents Python 环境；scan 默认调用
本机 Codex CLI，只读综合失败时自动保存确定性量价回退报告；stock-report 和看盘内 x 咨询
使用同一只读综合器生成个股研判或条件式回答，均不会触发自动交易。`
