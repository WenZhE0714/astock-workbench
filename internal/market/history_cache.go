package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const dailyHistoryCacheMaxAge = 14 * 24 * time.Hour

type CachedDailyHistoryClient struct {
	primary DailyHistoryClient
	dir     string
	now     func() time.Time
}

type cachedDailyBar struct {
	Symbol   string   `json:"symbol"`
	Source   string   `json:"source"`
	Date     string   `json:"date"`
	Open     float64  `json:"open"`
	Close    float64  `json:"close"`
	High     float64  `json:"high"`
	Low      float64  `json:"low"`
	Volume   float64  `json:"volume"`
	Amount   float64  `json:"amount_yuan"`
	Turnover *float64 `json:"turnover_percent,omitempty"`
}

type dailyHistoryCachePayload struct {
	FetchedAt time.Time        `json:"fetched_at"`
	Bars      []cachedDailyBar `json:"bars"`
}

func NewCachedDailyHistoryClient(primary DailyHistoryClient, dir string) *CachedDailyHistoryClient {
	return &CachedDailyHistoryClient{primary: primary, dir: dir, now: time.Now}
}

func (client *CachedDailyHistoryClient) cachePath(symbol string) string {
	return filepath.Join(client.dir, symbol+".json")
}

func encodeCachedBars(bars []domain.DailyBar) []cachedDailyBar {
	result := make([]cachedDailyBar, 0, len(bars))
	for _, bar := range bars {
		item := cachedDailyBar{
			Symbol: bar.Symbol, Source: bar.Source, Date: bar.Date, Open: bar.Open, Close: bar.Close,
			High: bar.High, Low: bar.Low, Volume: bar.Volume, Amount: bar.Amount,
		}
		if !math.IsNaN(bar.Turnover) && !math.IsInf(bar.Turnover, 0) {
			turnover := bar.Turnover
			item.Turnover = &turnover
		}
		result = append(result, item)
	}
	return result
}

func decodeCachedBars(items []cachedDailyBar) []domain.DailyBar {
	result := make([]domain.DailyBar, 0, len(items))
	for _, item := range items {
		turnover := math.NaN()
		if item.Turnover != nil {
			turnover = *item.Turnover
		}
		source := strings.TrimSpace(item.Source)
		if source == "" {
			source = "未复权日K"
		}
		if !strings.Contains(source, "缓存") {
			source += "缓存"
		}
		result = append(result, domain.DailyBar{
			Symbol: item.Symbol, Source: source, Date: item.Date, Open: item.Open, Close: item.Close,
			High: item.High, Low: item.Low, Volume: item.Volume, Amount: item.Amount, Turnover: turnover,
		})
	}
	return result
}

func writeFileAtomically(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".daily-history-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func (client *CachedDailyHistoryClient) save(symbol string, bars []domain.DailyBar) error {
	payload := dailyHistoryCachePayload{FetchedAt: client.now(), Bars: encodeCachedBars(bars)}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeFileAtomically(client.cachePath(symbol), append(data, '\n'))
}

func (client *CachedDailyHistoryClient) load(symbol string) ([]domain.DailyBar, error) {
	data, err := os.ReadFile(client.cachePath(symbol))
	if err != nil {
		return nil, err
	}
	var payload dailyHistoryCachePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	age := client.now().Sub(payload.FetchedAt)
	if payload.FetchedAt.IsZero() || age < 0 || age > dailyHistoryCacheMaxAge {
		return nil, fmt.Errorf("日K缓存已过期")
	}
	bars := decodeCachedBars(payload.Bars)
	if len(bars) < 60 {
		return nil, fmt.Errorf("日K缓存仅有%d根", len(bars))
	}
	return bars, nil
}

func (client *CachedDailyHistoryClient) FetchDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	if client == nil || client.primary == nil {
		return nil, fmt.Errorf("日K数据源未初始化")
	}
	bars, primaryError := client.primary.FetchDailyBars(ctx, symbol)
	if primaryError == nil && len(bars) >= 60 {
		_ = client.save(symbol, bars)
		return bars, nil
	}
	cached, cacheError := client.load(symbol)
	if cacheError == nil {
		return cached, nil
	}
	if primaryError == nil {
		primaryError = fmt.Errorf("仅返回%d根有效日K", len(bars))
	}
	return nil, fmt.Errorf("在线日K: %v；本地缓存: %v", primaryError, cacheError)
}
