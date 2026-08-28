package app

import (
	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/market"
)

// backtestProvider returns the application's configured historical data path.
// Tests and embedders may inject a range provider directly; normal startup
// points this at the cached multi-source history client created in New.
func (app *App) backtestProvider() backtest.DailyBarProvider {
	if app != nil && app.history != nil {
		if provider, ok := app.history.(backtest.DailyBarProvider); ok {
			return provider
		}
	}
	if app != nil && app.backtestHistory != nil {
		return app.backtestHistory
	}
	// Keep partially constructed App values usable in CLI tests and during
	// migration from older callers that did not populate a history field.
	return market.EastmoneyClient{}
}

func (app *App) newBacktestEngine() *backtest.DailyEngine {
	return backtest.NewDailyEngine(backtest.NewCachingDailyBarProvider(app.backtestProvider()))
}
