package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
)

const dailyHistoryCacheMaxAge = 14 * 24 * time.Hour

const dailyHistoryRangeBoundaryToleranceDays = 14

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

func sortedDailyBars(bars []domain.DailyBar) []domain.DailyBar {
	result := append([]domain.DailyBar(nil), bars...)
	sort.SliceStable(result, func(left, right int) bool {
		return result[left].Date < result[right].Date
	})
	return result
}

func dailyBarsInRange(bars []domain.DailyBar, start, end time.Time) []domain.DailyBar {
	startDate := start.Format("2006-01-02")
	endDate := end.Format("2006-01-02")
	result := make([]domain.DailyBar, 0, len(bars))
	for _, bar := range bars {
		if bar.Date >= startDate && bar.Date <= endDate {
			result = append(result, bar)
		}
	}
	return result
}

// dailyBarsCoverRange accepts exchange holidays and weekends at either
// boundary, but rejects a recent-only series for an older research window.
// The engine performs its own stricter coverage check after this function;
// this gate only decides whether it is safe to use the cached source at all.
func dailyBarsCoverRange(bars []domain.DailyBar, start, end time.Time) ([]domain.DailyBar, bool) {
	if start.IsZero() || end.IsZero() || start.After(end) {
		return nil, false
	}
	sorted := sortedDailyBars(bars)
	if len(sorted) < 60 {
		return nil, false
	}
	firstAllowed := start.AddDate(0, 0, dailyHistoryRangeBoundaryToleranceDays).Format("2006-01-02")
	lastAllowed := end.AddDate(0, 0, -dailyHistoryRangeBoundaryToleranceDays).Format("2006-01-02")
	if sorted[0].Date > firstAllowed || sorted[len(sorted)-1].Date < lastAllowed {
		return nil, false
	}
	filtered := dailyBarsInRange(sorted, start, end)
	if len(filtered) < 60 {
		return nil, false
	}
	return filtered, true
}

// FetchDailyBarsRange lets historical research reuse the same disk cache as
// the normal snapshot path. A complete, fresh cache is preferred so a
// transient upstream 501 cannot invalidate an otherwise auditable run; when
// it does not cover the requested interval, the configured range-capable
// provider is tried next.
func (client *CachedDailyHistoryClient) FetchDailyBarsRange(
	ctx context.Context,
	symbol string,
	start, end time.Time,
	adjustment backtest.PriceAdjustment,
) ([]domain.DailyBar, error) {
	if client == nil {
		return nil, fmt.Errorf("日K数据源未初始化")
	}
	if adjustment != backtest.AdjustmentNone {
		return nil, fmt.Errorf("当前回测仅支持固定的不复权口径")
	}
	if start.IsZero() || end.IsZero() || start.After(end) {
		return nil, fmt.Errorf("无效回测日期区间")
	}

	cached, cacheError := client.load(symbol)
	if cacheError == nil {
		if bars, ok := dailyBarsCoverRange(cached, start, end); ok {
			return bars, nil
		}
		cacheError = fmt.Errorf("本地缓存未覆盖 %s 至 %s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	}

	var primaryError error
	if client.primary != nil {
		if rangeSource, ok := client.primary.(DailyBarRangeClient); ok {
			bars, err := rangeSource.FetchDailyBarsRange(ctx, symbol, start, end, adjustment)
			if err == nil {
				if filtered, covered := dailyBarsCoverRange(bars, start, end); covered {
					return filtered, nil
				}
				primaryError = fmt.Errorf("在线区间未覆盖足够日K")
			} else {
				primaryError = err
			}
		} else {
			// Custom clients used by integrations may only implement the
			// ordinary full-history method. It is still useful when its data
			// happens to cover the requested range.
			bars, err := client.primary.FetchDailyBars(ctx, symbol)
			if err == nil {
				if filtered, covered := dailyBarsCoverRange(bars, start, end); covered {
					return filtered, nil
				}
				primaryError = fmt.Errorf("在线日K未覆盖足够区间")
			} else {
				primaryError = err
			}
		}
	} else {
		primaryError = fmt.Errorf("在线日K数据源未初始化")
	}
	return nil, fmt.Errorf("在线区间日K: %v；本地缓存: %v", primaryError, cacheError)
}
