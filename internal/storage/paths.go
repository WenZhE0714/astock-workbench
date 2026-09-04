package storage

import (
	"os"
	"path/filepath"
)

type Paths struct {
	ConfigDir                   string
	WatchlistFile               string
	CacheDir                    string
	NameCacheFile               string
	PinyinCacheFile             string
	DataDir                     string
	ViewHistoryFile             string
	ReportsDir                  string
	MarketReportsDir            string
	StockReportsDir             string
	AIChatsDir                  string
	BacktestsDir                string
	OptimizationsDir            string
	ContinuousOptimizationsDir  string
	RealtimeSignalsDir          string
	TradingAgentsDir            string
	PaperFile                   string
	ShadowReportFile            string
	ShadowConservativeFile      string
	ShadowAggressiveFile        string
	ShadowAdaptiveFile          string
	ShadowMonsterFile           string
	RealtimeCalibrationFile     string
	AutomationStateFile         string
	SentimentHistoryFile        string
	AIConfigFile                string
	AITokenFile                 string
	THSQuantAPIConfigFile       string
	THSQuantAPITokenFile        string
	THSQuantAPIRefreshTokenFile string
}

func ResolvePaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	configRoot := os.Getenv("XDG_CONFIG_HOME")
	if configRoot == "" {
		configRoot = filepath.Join(home, ".config")
	}
	cacheRoot := os.Getenv("XDG_CACHE_HOME")
	if cacheRoot == "" {
		cacheRoot = filepath.Join(home, ".cache")
	}
	dataRoot := os.Getenv("XDG_DATA_HOME")
	if dataRoot == "" {
		dataRoot = filepath.Join(home, ".local", "share")
	}
	dataDir := filepath.Join(dataRoot, "astock-workbench")
	return Paths{
		ConfigDir: filepath.Join(configRoot, "astock-workbench"),
		// Keep watchlist/cache compatibility with astock-go and the Ink version.
		WatchlistFile:               filepath.Join(configRoot, "astock", "watchlist"),
		CacheDir:                    filepath.Join(cacheRoot, "astock"),
		NameCacheFile:               filepath.Join(cacheRoot, "astock", "names.tsv"),
		PinyinCacheFile:             filepath.Join(cacheRoot, "astock", "pinyin.tsv"),
		DataDir:                     dataDir,
		ViewHistoryFile:             filepath.Join(dataDir, "view-history.tsv"),
		ReportsDir:                  filepath.Join(dataDir, "reports"),
		MarketReportsDir:            filepath.Join(dataDir, "market-reports"),
		StockReportsDir:             filepath.Join(dataDir, "stock-reports"),
		AIChatsDir:                  filepath.Join(dataDir, "ai-chats"),
		BacktestsDir:                filepath.Join(dataDir, "backtests"),
		OptimizationsDir:            filepath.Join(dataDir, "backtests", "optimizations"),
		ContinuousOptimizationsDir:  filepath.Join(dataDir, "backtests", "continuous"),
		RealtimeSignalsDir:          filepath.Join(dataDir, "realtime-signals"),
		TradingAgentsDir:            filepath.Join(dataDir, "tradingagents"),
		PaperFile:                   filepath.Join(dataDir, "paper", "account.json"),
		ShadowReportFile:            filepath.Join(dataDir, "paper", "shadow-report.json"),
		ShadowConservativeFile:      filepath.Join(dataDir, "paper", "shadow-report-conservative.json"),
		ShadowAggressiveFile:        filepath.Join(dataDir, "paper", "shadow-report-aggressive.json"),
		ShadowAdaptiveFile:          filepath.Join(dataDir, "paper", "shadow-report-adaptive.json"),
		ShadowMonsterFile:           filepath.Join(dataDir, "paper", "shadow-report-monster.json"),
		RealtimeCalibrationFile:     filepath.Join(dataDir, "realtime-signals", "calibration-state.json"),
		AutomationStateFile:         filepath.Join(dataDir, "automation", "state.json"),
		SentimentHistoryFile:        filepath.Join(dataDir, "sentiment", "history.json"),
		AIConfigFile:                filepath.Join(configRoot, "astock-workbench", "ai-config.json"),
		AITokenFile:                 filepath.Join(configRoot, "astock-workbench", "ai-token"),
		THSQuantAPIConfigFile:       filepath.Join(configRoot, "astock-workbench", "ths-quantapi.json"),
		THSQuantAPITokenFile:        filepath.Join(configRoot, "astock-workbench", "ths-quantapi-token"),
		THSQuantAPIRefreshTokenFile: filepath.Join(configRoot, "astock-workbench", "ths-quantapi-refresh-token"),
	}, nil
}
