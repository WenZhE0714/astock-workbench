package storage

import (
	"os"
	"path/filepath"
)

type Paths struct {
	ConfigDir        string
	WatchlistFile    string
	CacheDir         string
	NameCacheFile    string
	PinyinCacheFile  string
	DataDir          string
	ViewHistoryFile  string
	ReportsDir       string
	TradingAgentsDir string
	PaperFile        string
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
		WatchlistFile:    filepath.Join(configRoot, "astock", "watchlist"),
		CacheDir:         filepath.Join(cacheRoot, "astock"),
		NameCacheFile:    filepath.Join(cacheRoot, "astock", "names.tsv"),
		PinyinCacheFile:  filepath.Join(cacheRoot, "astock", "pinyin.tsv"),
		DataDir:          dataDir,
		ViewHistoryFile:  filepath.Join(dataDir, "view-history.tsv"),
		ReportsDir:       filepath.Join(dataDir, "reports"),
		TradingAgentsDir: filepath.Join(dataDir, "tradingagents"),
		PaperFile:        filepath.Join(dataDir, "paper", "account.json"),
	}, nil
}
