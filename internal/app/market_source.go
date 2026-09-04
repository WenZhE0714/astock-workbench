package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

const (
	marketSourceHTTP = "http"
	marketSourceTDX  = "tdx"
	marketSourceTHS  = "ths"
)

func normalizeMarketSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "http", "https", "default":
		return marketSourceHTTP
	case "tdx", "tongdaxin", "tcp":
		return marketSourceTDX
	case "ths", "10jqka", "ifind", "quantapi":
		return marketSourceTHS
	default:
		return ""
	}
}

func requestedMarketSource(value string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	if configured := strings.TrimSpace(os.Getenv("ASTOCK_MARKET_SOURCE")); configured != "" {
		return configured
	}
	if localTHSConfigEnabled() {
		return marketSourceTHS
	}
	return ""
}

func defaultWatchMarketSource() string {
	if value := strings.TrimSpace(os.Getenv("ASTOCK_MARKET_SOURCE")); value != "" {
		return value
	}
	if localTHSConfigEnabled() {
		return marketSourceTHS
	}
	return marketSourceTDX
}

func localTHSConfigEnabled() bool {
	paths, err := storage.ResolvePaths()
	if err != nil {
		return false
	}
	config, access, _, err := storage.LoadTHSQuantAPIConfig(paths.THSQuantAPIConfigFile, paths.THSQuantAPITokenFile, paths.THSQuantAPIRefreshTokenFile)
	return err == nil && config.Enabled && strings.TrimSpace(access) != ""
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
		return fmt.Errorf("未知行情源 %q；可选 http、tdx 或 ths", value)
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
	if source == marketSourceTHS {
		localConfig, fileAccessToken, fileRefreshToken, loadErr := storage.LoadTHSQuantAPIConfig(
			app.paths.THSQuantAPIConfigFile, app.paths.THSQuantAPITokenFile, app.paths.THSQuantAPIRefreshTokenFile,
		)
		if loadErr != nil {
			return fmt.Errorf("读取同花顺本地配置失败: %w", loadErr)
		}
		accessToken := firstNonEmptyEnv("ASTOCK_THS_ACCESS_TOKEN", fileAccessToken)
		if accessToken == "" {
			return fmt.Errorf("同花顺 Quant API 未配置 ASTOCK_THS_ACCESS_TOKEN")
		}
		refreshToken := firstNonEmptyEnv("ASTOCK_THS_REFRESH_TOKEN", fileRefreshToken)
		if app.httpQuotes == nil || app.httpHistory == nil || app.httpMinutes == nil {
			return fmt.Errorf("HTTP 行情回退源未初始化")
		}
		if app.thsMarket == nil || app.thsMarket.Configured() && app.thsMarket.AccessToken() != accessToken {
			gapMS := localConfig.MinRequestGapMS
			if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ASTOCK_THS_MIN_REQUEST_GAP_MS"))); err == nil && value >= 0 {
				gapMS = value
			}
			app.thsMarket = market.NewTHSQuantClient(market.THSQuantOptions{
				AccessToken: accessToken, RefreshToken: refreshToken,
				BaseURL: os.Getenv("ASTOCK_THS_BASE_URL"), MinRequestGap: time.Duration(gapMS) * time.Millisecond,
				QuoteIndicators: os.Getenv("ASTOCK_THS_QUOTE_INDICATORS"), HistoryIndicators: os.Getenv("ASTOCK_THS_HISTORY_INDICATORS"), MinuteIndicators: os.Getenv("ASTOCK_THS_MINUTE_INDICATORS"),
			})
		}
		app.quotes = market.NewFallbackQuoteClient(app.thsMarket, app.httpQuotes)
		app.history = market.NewFallbackDailyHistoryClient(app.thsMarket, app.httpHistory)
		// Use the HTTP minute source first for broad indices because it carries
		// Eastmoney's f58 leading series; stocks still prefer QuantAPI below.
		app.minutes = market.NewMarketMinuteClient(app.thsMarket, app.httpMinutes)
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

func firstNonEmptyEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}
