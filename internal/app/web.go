package app

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/market"
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
	server := web.NewServer(
		app.resolver, app.quotes, app.history, app.minutes, *defaultSymbol,
		web.WithWatchlist(app.paths.WatchlistFile),
		web.WithNameCache(app.paths.NameCacheFile),
		web.WithMarketAmount(app.amounts),
		web.WithBoardDetails(market.EastmoneyClient{}),
	)
	fmt.Fprintf(app.out, "ASTOCK Web 已启动: http://%s/\n", *listen)
	fmt.Fprintln(app.out, "按 Ctrl-C 停止；行情只读，自选改动与 CLI 共用")
	return server.Serve(ctx, *listen)
}
