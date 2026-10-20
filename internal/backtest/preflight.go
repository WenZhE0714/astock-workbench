package backtest

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/marketregime"
)

type HistoryInspection struct {
	Symbol        string `json:"symbol"`
	Name          string `json:"name"`
	Ready         bool   `json:"ready"`
	Bars          int    `json:"bars"`
	MinimumBars   int    `json:"minimum_bars"`
	RequiredStart string `json:"required_start"`
	FirstDate     string `json:"first_date,omitempty"`
	LastDate      string `json:"last_date,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

type HistoryPreflight struct {
	CheckedAt   time.Time           `json:"checked_at"`
	Start       string              `json:"start"`
	End         string              `json:"end"`
	Ready       bool                `json:"ready"`
	Available   int                 `json:"available"`
	Unavailable int                 `json:"unavailable"`
	Items       []HistoryInspection `json:"items"`
	Benchmark   *HistoryInspection  `json:"benchmark,omitempty"`
}

type HistoryInspector interface {
	InspectHistory(context.Context, Request) (HistoryPreflight, error)
}

func stockHistoryStart(request Request) time.Time {
	// Three calendar days per bar allows for weekends and exchange holidays.
	warmup := max(request.Technical.SlowMA, request.Technical.BreakoutDays+1, 21)
	return request.Start.AddDate(0, 0, -warmup*3)
}

func (engine *DailyEngine) stockHistory(ctx context.Context, request Request, symbol string) ([]domain.DailyBar, error) {
	bars, err := engine.provider.FetchDailyBarsRange(ctx, symbol, stockHistoryStart(request), request.End, request.Adjustment)
	if err != nil {
		return nil, &SymbolHistoryError{Symbol: symbol, Err: err}
	}
	bars = append([]domain.DailyBar(nil), bars...)
	sort.SliceStable(bars, func(left, right int) bool { return bars[left].Date < bars[right].Date })
	if len(bars) < request.Technical.SlowMA+2 {
		return bars, &SymbolHistoryError{Symbol: symbol, Err: fmt.Errorf("有效日K不足：%d 根，当前策略至少需要 %d 根", len(bars), request.Technical.SlowMA+2)}
	}
	return bars, nil
}

// InspectHistory checks every input independently; it never runs signals,
// removes a ticker, or turns an unavailable benchmark into a stock failure.
func (engine *DailyEngine) InspectHistory(ctx context.Context, request Request) (HistoryPreflight, error) {
	var report HistoryPreflight
	if engine == nil || engine.provider == nil {
		return report, fmt.Errorf("日K数据源未初始化")
	}
	if err := validateRequest(request); err != nil {
		return report, err
	}
	request = canonicalRequest(request)
	report.Start, report.End = request.Start.Format(time.DateOnly), request.End.Format(time.DateOnly)
	report.Items = make([]HistoryInspection, len(request.Tickers))
	for i, symbol := range request.Tickers {
		name := request.Names[symbol]
		if name == "" {
			name = symbol
		}
		report.Items[i] = HistoryInspection{Symbol: symbol, Name: name, MinimumBars: request.Technical.SlowMA + 2, RequiredStart: stockHistoryStart(request).Format(time.DateOnly)}
	}
	count := len(report.Items)
	if request.Benchmark != "" {
		report.Benchmark = &HistoryInspection{Symbol: request.Benchmark, Name: request.Benchmark, MinimumBars: 2, RequiredStart: request.Start.AddDate(0, 0, -marketregime.MinimumBars*3).Format(time.DateOnly)}
		count++
	}
	jobs := make(chan int, count)
	for i := 0; i < count; i++ {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(3, count) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				itemCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
				var bars []domain.DailyBar
				var err error
				var item *HistoryInspection
				if i < len(report.Items) {
					item = &report.Items[i]
					bars, err = engine.stockHistory(itemCtx, request, item.Symbol)
				} else {
					item = report.Benchmark
					bars, err = engine.provider.FetchDailyBarsRange(itemCtx, request.Benchmark, request.Start.AddDate(0, 0, -marketregime.MinimumBars*3), request.End, request.Adjustment)
					bars = normalizedRegimeBars(bars)
					if err == nil && len(barsWithin(bars, request.Start, request.End)) < 2 {
						err = fmt.Errorf("回测区间内基准日K不足，收益对照暂不可用")
					}
				}
				cancel()
				item.Bars = len(bars)
				if len(bars) > 0 {
					item.FirstDate, item.LastDate = bars[0].Date, bars[len(bars)-1].Date
				}
				item.Ready = err == nil
				if err != nil {
					item.Detail = err.Error()
				}
			}
		}()
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return HistoryPreflight{}, err
	}
	for _, item := range report.Items {
		if item.Ready {
			report.Available++
		} else {
			report.Unavailable++
		}
	}
	report.Ready = report.Unavailable == 0
	report.CheckedAt = engine.now()
	return report, nil
}
