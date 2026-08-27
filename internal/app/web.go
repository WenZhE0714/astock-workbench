package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/web"
)

const fallbackWebSymbol = "600519"

func (app *App) webInitialSymbol() (string, error) {
	groups, _, err := storage.LoadWatchlistGroups(app.paths.WatchlistFile)
	if err != nil {
		return "", err
	}
	symbols := storage.WatchlistSymbols(groups, storage.AllWatchlistGroup)
	if len(symbols) > 0 {
		return symbols[0], nil
	}
	return fallbackWebSymbol, nil
}

func (app *App) runWeb(ctx context.Context, arguments []string) error {
	set := flag.NewFlagSet("web", flag.ContinueOnError)
	set.SetOutput(app.errOut)
	listen := set.String("listen", "127.0.0.1:8765", "Web 监听地址")
	defaultSymbol := set.String("symbol", "", "首次打开的股票；默认使用自选第一只")
	source := set.String("source", "", "行情源：http 或 tdx")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	if set.NArg() > 1 {
		return fmt.Errorf("用法: astock web [--listen 地址] [--symbol 代码或名称] [--source http|tdx]")
	}
	if err := app.configureMarketSource(*source); err != nil {
		return err
	}
	if set.NArg() == 1 {
		*defaultSymbol = set.Arg(0)
	}
	if strings.TrimSpace(*defaultSymbol) == "" {
		initialSymbol, initialError := app.webInitialSymbol()
		if initialError != nil {
			return fmt.Errorf("读取默认自选失败: %w", initialError)
		}
		*defaultSymbol = initialSymbol
	}
	strategyEngine := backtest.NewDailyEngine(backtest.NewCachingDailyBarProvider(market.EastmoneyClient{}))
	serverOptions := []web.ServerOption{
		web.WithWatchlist(app.paths.WatchlistFile),
		web.WithNameCache(app.paths.NameCacheFile),
		web.WithMarketAmount(app.amounts),
		web.WithBoardDetails(market.EastmoneyClient{}),
		web.WithStrategyResearch(
			strategyEngine,
			storage.NewBacktestStore(app.paths.BacktestsDir),
		),
		web.WithStrategyCandidateLifecycle(strategyEngine, app.continuousOptimizationStore()),
		web.WithRealtimeStrategy(
			realtime.NewScanner(app.marketScan, app.quotes, app.scanHistory, app.minutes, storage.NewRealtimeSignalStore(app.paths.RealtimeSignalsDir)),
			storage.NewRealtimeSignalStore(app.paths.RealtimeSignalsDir),
		),
		web.WithRealtimeOutcomes(
			realtime.NewOutcomeEvaluator(app.scanHistory, app.scanHistory, storage.NewRealtimeOutcomeStore(app.paths.RealtimeSignalsDir)),
		),
		web.WithShadowExecutionProfiles(
			paper.NewEvaluator(app.history),
			storage.NewShadowStore(app.paths.ShadowReportFile),
			storage.NewShadowStore(app.paths.ShadowConservativeFile),
			storage.NewShadowStore(app.paths.ShadowAggressiveFile),
		),
		web.WithAutomaticStrategyResearch(func(ctx context.Context, symbols []string, end time.Time) (web.AutomaticResearchResult, error) {
			id, message, ran, err := app.runAutomaticContinuousOptimization(ctx, symbols, end)
			return web.AutomaticResearchResult{Ran: ran, ExperimentID: id, Message: message}, err
		}),
		web.WithAutomationState(storage.NewAutomationStore(app.paths.AutomationStateFile)),
	}
	if calendarPath := strings.TrimSpace(os.Getenv("ASTOCK_TRADING_CALENDAR_FILE")); calendarPath != "" {
		provider := market.NewFileTradingCalendar(calendarPath)
		serverOptions = append(serverOptions, web.WithTradingCalendar(provider.Dates))
	}
	server := web.NewServer(app.resolver, app.quotes, app.history, app.minutes, *defaultSymbol, serverOptions...)
	fmt.Fprintf(app.out, "ASTOCK Web 已启动: http://%s/\n", *listen)
	fmt.Fprintln(app.out, "按 Ctrl-C 停止；自选和量化回测归档与 CLI 共用")
	return server.Serve(ctx, *listen)
}
