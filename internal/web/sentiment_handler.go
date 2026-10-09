package web

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func (s *Server) handleSentiment(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "市场情绪只支持 GET"})
		return
	}
	if s == nil || s.quotes == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "市场情绪行情服务未初始化"})
		return
	}
	// Each optional provider gets its own deadline. A slow limit-pool or
	// industry request must not cancel independent hot-theme data.
	ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
	defer cancel()
	symbols := []string{"sh000001", "sz399001", "sz399006", "sz399106", "bj899050"}
	quotesCtx, cancelQuotes := context.WithTimeout(ctx, 8*time.Second)
	quotes, quoteErr := s.fetchQuoteBatch(quotesCtx, symbols)
	cancelQuotes()
	indexQuotes := make([]domain.Quote, 0, 3)
	for _, symbol := range []string{"sh000001", "sz399001", "sz399006"} {
		if quote, ok := quotes[symbol]; ok {
			indexQuotes = append(indexQuotes, quote)
		}
	}
	flows := map[string]domain.BoardFlow(nil)
	flowErr := error(nil)
	previous := domain.MarketAmountSnapshot{}
	previousErr := error(nil)
	var stats domain.LimitStatsSnapshot
	var statsErr error
	var hot domain.HotStockSnapshot
	var hotErr error
	var waitGroup sync.WaitGroup
	providerRoot := ctx
	if s.industryFlows != nil {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			flowCtx, flowCancel := context.WithTimeout(providerRoot, 8*time.Second)
			defer flowCancel()
			flows, flowErr = s.industryFlows.FetchIndustryFlows(flowCtx)
		}()
	}
	if s.marketAmounts != nil {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			amountCtx, amountCancel := context.WithTimeout(providerRoot, 8*time.Second)
			defer amountCancel()
			previous, previousErr = s.fetchPreviousMarketAmount(amountCtx)
		}()
	}
	if s.limitStats != nil {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			statsCtx, statsCancel := context.WithTimeout(providerRoot, 10*time.Second)
			defer statsCancel()
			stats, statsErr = s.fetchLimitStats(statsCtx, "")
		}()
	}
	if s.sentimentSignals != nil {
		tradeDate := ""
		for _, quote := range indexQuotes {
			if len(quote.QuoteTime) >= 10 {
				tradeDate = quote.QuoteTime[:10]
				break
			}
		}
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			extrasCtx, extrasCancel := context.WithTimeout(providerRoot, 8*time.Second)
			defer extrasCancel()
			hot, hotErr = s.fetchSentimentExtras(extrasCtx, tradeDate)
		}()
	}
	waitGroup.Wait()
	// Use the quote-derived trade date for the limit pool. The first fetch is
	// intentionally parallel; when it lacks a date, retry the cached/provider
	// call with today's date only if necessary.
	if s.limitStats != nil && statsErr == nil && stats.TradeDate == "" {
		statsCtx, statsCancel := context.WithTimeout(providerRoot, 10*time.Second)
		stats, statsErr = s.fetchLimitStats(statsCtx, "")
		statsCancel()
	}
	scoringFlows := flows
	if flowErr != nil {
		scoringFlows = nil
	}
	snapshot := calculateMarketSentiment(time.Now(), indexQuotes, scoringFlows, previous)
	if flowErr != nil && len(flows) > 0 {
		cached := calculateMarketSentiment(time.Now(), nil, flows, domain.MarketAmountSnapshot{})
		snapshot.StrongIndustries, snapshot.WeakIndustries = cached.StrongIndustries, cached.WeakIndustries
	}
	// calculateMarketSentiment is also used independently by callers that need
	// a coverage warning; the HTTP response has a more precise source status.
	snapshot.Warnings = removeSentimentWarning(snapshot.Warnings, "涨停/跌停、炸板率和连板梯队尚未接入")
	if s.limitStats != nil {
		// The parallel request above uses today's date when the quote provider
		// has no date. Refresh with the actual quote date when it is available.
		if snapshot.TradeDate != "" && stats.TradeDate != snapshot.TradeDate {
			statsCtx, statsCancel := context.WithTimeout(providerRoot, 10*time.Second)
			stats, statsErr = s.fetchLimitStats(statsCtx, snapshot.TradeDate)
			statsCancel()
		}
		if statsErr == nil && stats.Available {
			snapshot.LimitUpCount = stats.LimitUpCount
			snapshot.LimitDownCount = stats.LimitDownCount
			snapshot.BrokenCount = stats.BrokenCount
			snapshot.BrokenRate = stats.BrokenRate
			snapshot.HighestStreak = stats.HighestStreak
			snapshot.StreakLadder = stats.StreakLadder
			snapshot.LimitCountsAvailable = true
			snapshot.LimitStatsAvailable = true
			snapshot.LimitStatsSource = stats.Source
			snapshot.Warnings = removeSentimentWarning(snapshot.Warnings, "涨停/跌停、炸板率和连板梯队尚未接入")
			if snapshot.Source == "" {
				snapshot.Source = stats.Source
			}
			snapshot.CoveragePercent = math.Min(100, snapshot.CoveragePercent+10)
		} else {
			if statsErr != nil {
				snapshot.Warnings = append(snapshot.Warnings, "涨跌停结构暂不可用: "+statsErr.Error())
			} else {
				snapshot.Warnings = append(snapshot.Warnings, "涨跌停结构暂不可用")
			}
		}
	} else {
		snapshot.Warnings = append(snapshot.Warnings, "涨跌停结构暂未配置数据源")
	}
	if quoteErr != nil {
		snapshot.Warnings = append(snapshot.Warnings, "指数/成交额行情请求失败: "+quoteErr.Error())
	}
	if flowErr != nil {
		if len(flows) > 0 {
			snapshot.Warnings = append(snapshot.Warnings, flowErr.Error()+"；缓存不参与本轮情绪评分")
		} else {
			snapshot.Warnings = append(snapshot.Warnings, "行业扩散数据请求失败: "+flowErr.Error())
		}
	}
	if previousErr != nil {
		snapshot.Warnings = append(snapshot.Warnings, "上一交易日成交额不可用: "+previousErr.Error())
	}
	if s.sentimentSignals != nil {
		if hot.Available {
			snapshot.HotStockCount = len(hot.Stocks)
			snapshot.HotThemeCount = len(hot.Themes)
			snapshot.HotThemes = hot.Themes
			snapshot.HotSignalAvailable = true
		}
		if hotErr != nil {
			if hot.Available {
				snapshot.Warnings = append(snapshot.Warnings, hotErr.Error())
			} else {
				snapshot.Warnings = append(snapshot.Warnings, "同花顺热点暂不可用: "+hotErr.Error())
			}
		}
	}
	snapshot.Warnings = uniqueSentimentWarnings(snapshot.Warnings)
	s.sentimentMu.Lock()
	s.sentimentHistory = appendSentimentHistory(s.sentimentHistory, snapshot)
	history := append([]domain.MarketSentimentPoint(nil), s.sentimentHistory...)
	s.sentimentMu.Unlock()
	if s.sentimentHistoryStore != nil {
		_ = s.sentimentHistoryStore.Save(history)
	}
	sort.SliceStable(history, func(left, right int) bool { return history[left].At.Before(history[right].At) })
	writeJSON(writer, http.StatusOK, marketSentimentResponse{Snapshot: snapshot, History: history})
}

func (s *Server) fetchLimitStats(ctx context.Context, tradeDate string) (domain.LimitStatsSnapshot, error) {
	now := time.Now()
	s.limitStatsMu.Lock()
	cached := s.limitStatsCache
	if cached.tradeDate == tradeDate && !cached.fetchedAt.IsZero() && now.Sub(cached.fetchedAt) >= 0 && now.Sub(cached.fetchedAt) < limitStatsCacheTTL {
		s.limitStatsMu.Unlock()
		return cached.snapshot, cached.err
	}
	s.limitStatsMu.Unlock()
	snapshot, err := s.limitStats.FetchLimitStats(ctx, tradeDate)
	s.limitStatsMu.Lock()
	s.limitStatsCache = limitStatsCacheEntry{tradeDate: tradeDate, snapshot: snapshot, err: err, fetchedAt: now}
	s.limitStatsMu.Unlock()
	return snapshot, err
}

func removeSentimentWarning(values []string, unwanted string) []string {
	filtered := values[:0]
	for _, value := range values {
		if value != unwanted {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func (s *Server) fetchSentimentExtras(ctx context.Context, tradeDate string) (domain.HotStockSnapshot, error) {
	now := s.now()
	if tradeDate == "" {
		tradeDate = now.In(realtimeWebLocation).Format("2006-01-02")
	}
	for {
		if err := ctx.Err(); err != nil {
			return domain.HotStockSnapshot{}, err
		}
		now = s.now()
		s.sentimentExtrasMu.Lock()
		cached := s.sentimentExtrasCache
		if cached.tradeDate == tradeDate && !cached.fetchedAt.IsZero() && now.Sub(cached.fetchedAt) >= 0 && now.Sub(cached.fetchedAt) < sentimentExtrasCacheTTL {
			s.sentimentExtrasMu.Unlock()
			return cachedHotResult(cached, now)
		}
		if inflight := s.sentimentExtrasInflight; inflight != nil {
			s.sentimentExtrasMu.Unlock()
			select {
			case <-ctx.Done():
				return domain.HotStockSnapshot{}, ctx.Err()
			case <-inflight:
				continue
			}
		}
		s.sentimentExtrasInflight = make(chan struct{})
		s.sentimentExtrasMu.Unlock()

		hot, hotErr := s.sentimentSignals.FetchHotStocks(ctx, tradeDate)
		now = s.now()
		if hotErr == nil && (!hot.Available || hot.TradeDate != tradeDate) {
			hotErr = fmt.Errorf("未返回 %s 的有效热点数据", tradeDate)
		}
		entry := sentimentExtrasCacheEntry{hot: hot, hotErr: hotErr, tradeDate: tradeDate, fetchedAt: now}
		if hotErr == nil {
			entry.succeededAt = now
		} else {
			entry.hot = domain.HotStockSnapshot{}
			if cached.tradeDate == tradeDate && cached.hot.Available && now.Sub(cached.succeededAt) >= 0 && now.Sub(cached.succeededAt) < 5*time.Minute {
				entry.hot, entry.succeededAt = cached.hot, cached.succeededAt
				entry.hotErr = fmt.Errorf("同花顺热点刷新失败，暂用 %s 的缓存", cached.succeededAt.In(realtimeWebLocation).Format("15:04:05"))
			}
		}
		s.sentimentExtrasMu.Lock()
		if ctx.Err() == nil {
			s.sentimentExtrasCache = entry
		}
		close(s.sentimentExtrasInflight)
		s.sentimentExtrasInflight = nil
		s.sentimentExtrasMu.Unlock()
		return cachedHotResult(entry, now)
	}
}

func cachedHotResult(entry sentimentExtrasCacheEntry, now time.Time) (domain.HotStockSnapshot, error) {
	if entry.hot.Available && (entry.succeededAt.IsZero() || now.Sub(entry.succeededAt) < 0 || now.Sub(entry.succeededAt) >= 5*time.Minute) {
		return domain.HotStockSnapshot{}, fmt.Errorf("同花顺热点缓存已过期，等待在线数据恢复")
	}
	hot := entry.hot
	hot.Stocks = append([]domain.HotStockSignal(nil), hot.Stocks...)
	hot.Themes = append([]domain.HotTheme(nil), hot.Themes...)
	return hot, entry.hotErr
}

func uniqueSentimentWarnings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
