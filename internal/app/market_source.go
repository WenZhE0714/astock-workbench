package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wenzhe/astock-workbench/internal/market"
)

const (
	marketSourceHTTP = "http"
	marketSourceTDX  = "tdx"
)

func normalizeMarketSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "http", "https", "default":
		return marketSourceHTTP
	case "tdx", "tongdaxin", "tcp":
		return marketSourceTDX
	default:
		return ""
	}
}

func requestedMarketSource(value string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return os.Getenv("ASTOCK_MARKET_SOURCE")
}

func defaultWatchMarketSource() string {
	if value := strings.TrimSpace(os.Getenv("ASTOCK_MARKET_SOURCE")); value != "" {
		return value
	}
	return marketSourceTDX
}

func (app *App) tdxPythonPath() string {
	if value := strings.TrimSpace(os.Getenv("ASTOCK_TDX_PYTHON")); value != "" {
		return value
	}
	path := filepath.Join(app.paths.DataDir, "tdx-venv", "bin", "python")
	if runtime.GOOS == "windows" {
		path = filepath.Join(app.paths.DataDir, "tdx-venv", "Scripts", "python.exe")
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

// configureMarketSource changes quote, daily-history and intraday adapters.
// Fund-flow, board, ranking and news adapters retain their existing sources.
func (app *App) configureMarketSource(value string) error {
	source := normalizeMarketSource(requestedMarketSource(value))
	if source == "" {
		return fmt.Errorf("未知行情源 %q；可选 http 或 tdx", value)
	}
	if source == marketSourceHTTP {
		if app.httpQuotes != nil {
			app.quotes = app.httpQuotes
		}
		if app.httpHistory != nil {
			app.history = app.httpHistory
		}
		if app.httpMinutes != nil {
			app.minutes = app.httpMinutes
		}
		app.marketSource = source
		return nil
	}
	if app.httpQuotes == nil || app.httpHistory == nil || app.httpMinutes == nil {
		return fmt.Errorf("HTTP 行情回退源未初始化")
	}
	if app.tdxMarket == nil {
		app.tdxMarket = market.NewTDXClientWithMinute(app.httpQuotes, app.httpHistory, app.httpMinutes, market.TDXOptions{
			Python: app.tdxPythonPath(),
			Server: os.Getenv("ASTOCK_TDX_SERVER"),
		})
	}
	app.quotes = app.tdxMarket
	app.history = app.tdxMarket
	app.minutes = app.tdxMarket
	app.marketSource = source
	return nil
}
