package app

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/notify"
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
	source := set.String("source", "", "行情源：http、tdx 或 ths")
	macNotifications := set.Bool("mac-notifications", false, "通过 macOS 通知中心提醒候选触发和模拟买卖")
	testMacNotification := set.Bool("test-mac-notification", false, "发送一条 Mac 测试通知后退出")
	volumeForwardDir := set.String("volume-forward-dir", os.Getenv("ASTOCK_VOLUME_FORWARD_DIR"), "只读展示已冻结量比前瞻实验的目录")
	replayArchiveDir := set.String("replay-archive-dir", os.Getenv("ASTOCK_REPLAY_ARCHIVE_DIR"), "只读展示历史演练归档，不计入真实前瞻")
	acceptanceDir := set.String("volume-forward-acceptance-dir", os.Getenv("ASTOCK_VOLUME_FORWARD_ACCEPTANCE_DIR"), "只读展示独立前瞻验收报告的目录")
	if err := set.Parse(arguments); err != nil {
		return err
	}
	if set.NArg() > 1 {
		return fmt.Errorf("用法: astock web [--listen 地址] [--symbol 代码或名称] [--source http|tdx|ths]")
	}
	if (*macNotifications || *testMacNotification) && runtime.GOOS != "darwin" {
		return fmt.Errorf("Mac 通知仅支持 macOS")
	}
	if *testMacNotification {
		testCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := (notify.MacSender{}).Send(testCtx, notify.Message{Title: "ASTOCK · 通知测试", Subtitle: "Mac 原生通知", Body: "候选触发与模拟买卖提醒将显示在这里。这是一条测试消息。"}); err != nil {
			return err
		}
		fmt.Fprintln(app.out, "测试消息已提交给 macOS；若未见横幅，请在系统设置 → 通知 → 脚本编辑器中允许通知，并检查专注模式。")
		return nil
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
	strategyEngine := app.newBacktestEngine()
	realtimeScanner := realtime.NewScanner(
		app.marketScan,
		app.quotes,
		app.scanHistory,
		app.minutes,
		storage.NewRealtimeSignalStore(app.paths.RealtimeSignalsDir),
	)
	realtimeOutcomes := realtime.NewOutcomeEvaluator(
		app.scanHistory,
		app.scanHistory,
		storage.NewRealtimeOutcomeStore(app.paths.RealtimeSignalsDir),
	)
	realtimeCalibration := storage.NewRealtimeCalibrationStore(app.paths.RealtimeCalibrationFile)
	// Restore the last fully gated challenger before the first scan after a
	// process restart. This keeps the adaptive shadow account continuous while
	// leaving the champion score and archived signals immutable.
	calibrationState, calibrationStateErr := realtimeCalibration.Load()
	if calibrationStateErr == nil && calibrationState.Active.ID != "" && calibrationState.Status != "challenger-rolled-back" {
		realtimeScanner.SetCalibration(calibrationState.Active)
	} else if calibrationStateErr == nil && calibrationState.Status == "challenger-rolled-back" {
		// A rejected Challenger stays in the audit history but must not be
		// resurrected on restart until a genuinely newer calibration is produced.
		realtimeScanner.SetCalibration(realtime.ScoreCalibration{})
	} else if report, reportErr := realtimeOutcomes.Report(0, time.Now()); reportErr == nil {
		if calibration, ready := realtime.CalibrationFromOutcome(report); ready {
			realtimeScanner.SetCalibration(calibration)
			_ = realtimeCalibration.Save(storage.RealtimeCalibrationState{
				Active: calibration, AppliedAt: time.Now(), Source: "启动时恢复历史验证结果", Status: "challenger-active",
			})
		}
	}
	serverOptions := []web.ServerOption{
		web.WithWatchlist(app.paths.WatchlistFile),
		web.WithNameCache(app.paths.NameCacheFile),
		web.WithAIChatService(webAIChatService{app: app}),
		web.WithAIConfigService(webAIConfigService{app: app}),
		web.WithTradePlans(storage.NewTradePlanStore(app.paths.TradePlansDir)),
		web.WithPatternValidation(storage.NewPatternValidationStore(app.paths.PatternValidationsDir)),
		web.WithPatternBatches(storage.NewPatternBatchStore(app.paths.PatternBatchesDir)),
		web.WithPatternForwards(storage.NewPatternForwardStore(app.paths.PatternForwardsDir)),
		web.WithPatternValidationHistory(app.backtestProvider()),
		web.WithPatternValidationActions(market.EastmoneyClient{}),
		web.WithPatternValidationFactors(market.SinaAdjustmentClient{}),
		web.WithTradePlanReviews(storage.NewTradePlanReviewStore(app.paths.PlanReviewsDir)),
		web.WithPlanMonitors(storage.NewPlanMonitorStore(app.paths.PlanMonitorsDir)),
		web.WithPlanExperiment(storage.NewPlanExperimentStore(app.paths.PlanExperimentFile)),
		web.WithMarketAmount(app.amounts),
		web.WithIndustryFlows(app.industryFlows),
		web.WithBoardWorkbench(storage.NewBoardSnapshotStore(filepath.Join(app.paths.DataDir, "board-snapshots")), market.EastmoneyClient{}),
		web.WithSentimentSignals(market.NewTHSSignalClientFromEnv()),
		web.WithLimitStats(market.EastmoneyClient{}),
		web.WithMarketRankings(app.rankings),
		web.WithSentimentHistoryStore(storage.NewSentimentHistoryStore(app.paths.SentimentHistoryFile)),
		web.WithGlobalMarkets(app.globalMarkets),
		web.WithGlobalCharts(market.NewFallbackGlobalChartClient(
			market.YahooGlobalChartClient{},
			market.EastmoneyGlobalChartClient{},
		)),
		web.WithBoardDetails(market.EastmoneyClient{}),
		web.WithRelatedData(app.boards, market.EastmoneyClient{}),
		web.WithStrategyResearch(
			strategyEngine,
			storage.NewBacktestStore(app.paths.BacktestsDir),
		),
		web.WithStrategyCandidateLifecycle(strategyEngine, app.continuousOptimizationStore()),
		web.WithRealtimeStrategy(
			realtimeScanner,
			storage.NewRealtimeSignalStore(app.paths.RealtimeSignalsDir),
		),
		web.WithRealtimeOutcomes(realtimeOutcomes),
		web.WithRealtimeCalibrationStore(realtimeCalibration),
		web.WithShadowExecutionProfiles(
			paper.NewEvaluator(app.history),
			storage.NewShadowStore(app.paths.ShadowReportFile),
			storage.NewShadowStore(app.paths.ShadowConservativeFile),
			storage.NewShadowStore(app.paths.ShadowAggressiveFile),
		),
		web.WithAdaptiveShadowExecution(storage.NewShadowStore(app.paths.ShadowAdaptiveFile)),
		web.WithMonsterShadowExecution(storage.NewShadowStore(app.paths.ShadowMonsterFile)),
		web.WithShadowDailyReviews(storage.NewShadowDailyReviewStore(filepath.Join(filepath.Dir(app.paths.ShadowReportFile), "daily-reviews")), storage.NewRealtimeSignalStore(app.paths.RealtimeSignalsDir)),
		web.WithShadowComparisonCalendarFile(filepath.Join(app.paths.CacheDir, "daily-history-scan", "sh000300.json")),
		web.WithAutomaticStrategyResearch(func(ctx context.Context, symbols []string, end time.Time) (web.AutomaticResearchResult, error) {
			id, message, ran, err := app.runAutomaticContinuousOptimization(ctx, symbols, end)
			return web.AutomaticResearchResult{Ran: ran, ExperimentID: id, Message: message}, err
		}),
		web.WithAutomationState(storage.NewAutomationStore(app.paths.AutomationStateFile)),
	}
	trialRoot := filepath.Join(filepath.Dir(app.paths.ShadowReportFile), "trials")
	if directory := strings.TrimSpace(*volumeForwardDir); directory != "" {
		absolute, err := filepath.Abs(directory)
		if err != nil {
			return err
		}
		serverOptions = append(serverOptions, web.WithVolumeForwardStudy(storage.NewVolumeForwardStore(absolute)))
		serverOptions = append(serverOptions, web.WithFrozenReplay("forward", "P008 前瞻", "forward", absolute))
		if directory := strings.TrimSpace(*acceptanceDir); directory != "" {
			absolute, err := filepath.Abs(directory)
			if err != nil {
				return err
			}
			serverOptions = append(serverOptions, web.WithForwardAcceptance(storage.NewForwardAcceptanceStore(absolute)))
		}
	}
	if directory := strings.TrimSpace(*replayArchiveDir); directory != "" {
		absolute, err := filepath.Abs(directory)
		if err != nil {
			return err
		}
		serverOptions = append(serverOptions, web.WithFrozenReplay("historical", "P008 历史演练", "historical", absolute))
	}
	notificationRecords := storage.NewNativeNotificationStore(filepath.Join(app.paths.DataDir, "notifications", "mac.json"))
	notificationReviews := storage.NewNotificationReviewStore(filepath.Join(app.paths.DataDir, "notifications", "reviews.json"))
	serverOptions = append(serverOptions, web.WithNotificationCenter(notificationRecords, notificationReviews))
	if *macNotifications {
		host, port, err := net.SplitHostPort(*listen)
		if err != nil {
			return err
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		sender := notify.MacSender{BaseURL: "http://" + net.JoinHostPort(host, port), Allowed: func(message notify.Message) (bool, error) {
			state, err := notificationReviews.Load()
			if err != nil {
				return false, err
			}
			return !state.Symbols[message.Symbol].MutedUntil.After(time.Now()), nil
		}}
		dispatcher, err := notify.NewDispatcher(sender, notificationRecords, log.New(app.errOut, "", log.LstdFlags))
		if err != nil {
			return err
		}
		serverOptions = append(serverOptions, web.WithNativeNotifications(dispatcher))
		fmt.Fprintln(app.out, "Mac 通知已开启：候选触发、模拟买入和模拟卖出；无需浏览器保持打开。")
	}
	trials, err := storage.LoadShadowTrials(trialRoot)
	if err != nil {
		return fmt.Errorf("读取影子对照配置失败: %w", err)
	}
	if len(trials.Trials) > 0 {
		serverOptions = append(serverOptions, web.WithShadowTrials(trials, trialRoot))
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
