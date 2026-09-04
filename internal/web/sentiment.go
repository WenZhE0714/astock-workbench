package web

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type marketIndustryFlowClient interface {
	FetchIndustryFlows(context.Context) (map[string]domain.BoardFlow, error)
}

type marketSentimentSignalClient interface {
	FetchNorthbound(context.Context) (domain.NorthboundFlowSnapshot, error)
	FetchHotStocks(context.Context, string) (domain.HotStockSnapshot, error)
}

type marketSentimentResponse struct {
	Snapshot domain.MarketSentimentSnapshot `json:"snapshot"`
	History  []domain.MarketSentimentPoint  `json:"history"`
}

// MarshalJSON keeps unavailable numeric components as JSON null. encoding/json
// rejects NaN/Inf, while the UI needs to distinguish missing data from zero.
func (response marketSentimentResponse) MarshalJSON() ([]byte, error) {
	snapshot := response.Snapshot
	limitUp, limitDown, broken, brokenRate, highest, ladder := any(nil), any(nil), any(nil), any(nil), any(nil), any(nil)
	if snapshot.LimitStatsAvailable {
		limitUp, limitDown, broken = snapshot.LimitUpCount, snapshot.LimitDownCount, snapshot.BrokenCount
		brokenRate, highest, ladder = sentimentJSONNumber(snapshot.BrokenRate), snapshot.HighestStreak, snapshot.StreakLadder
	}
	snapshotJSON := map[string]any{
		"generated_at": snapshot.GeneratedAt, "trade_date": snapshot.TradeDate, "source": snapshot.Source,
		"score": sentimentJSONNumber(snapshot.Score), "phase": snapshot.Phase, "coverage_percent": snapshot.CoveragePercent,
		"index_signal": sentimentJSONNumber(snapshot.IndexSignal), "turnover_signal": sentimentJSONNumber(snapshot.TurnoverSignal),
		"industry_breadth": sentimentJSONNumber(snapshot.IndustryBreadth), "positive_industry_rate": sentimentJSONNumber(snapshot.PositiveIndustryRate),
		"industry_flow_signal": sentimentJSONNumber(snapshot.IndustryFlowSignal), "rise_count": snapshot.RiseCount, "fall_count": snapshot.FallCount,
		"flat_count": snapshot.FlatCount, "industry_count": snapshot.IndustryCount, "limit_up_count": limitUp,
		"limit_down_count": limitDown, "limit_counts_available": snapshot.LimitCountsAvailable,
		"broken_count": broken, "broken_rate_percent": brokenRate,
		"highest_streak": highest, "streak_ladder": ladder,
		"limit_stats_available": snapshot.LimitStatsAvailable,
		"limit_stats_source":    snapshot.LimitStatsSource,
		"warnings":              snapshot.Warnings, "strong_industries": snapshot.StrongIndustries, "weak_industries": snapshot.WeakIndustries,
		"northbound_net_hundred_million_yuan": sentimentJSONNumber(snapshot.NorthboundNet), "northbound_available": snapshot.NorthboundAvailable,
		"northbound_signal": sentimentJSONNumber(snapshot.NorthboundSignal),
		"northbound_at":     snapshot.NorthboundAt,
		"hot_stock_count":   snapshot.HotStockCount, "hot_theme_count": snapshot.HotThemeCount, "hot_signal_available": snapshot.HotSignalAvailable,
		"hot_themes": snapshot.HotThemes,
	}
	history := make([]map[string]any, 0, len(response.History))
	for _, point := range response.History {
		history = append(history, map[string]any{
			"at": point.At, "score": sentimentJSONNumber(point.Score), "phase": point.Phase,
			"index_signal": sentimentJSONNumber(point.IndexSignal), "turnover_signal": sentimentJSONNumber(point.TurnoverSignal),
			"industry_breadth": sentimentJSONNumber(point.IndustryBreadth), "positive_industry_rate": sentimentJSONNumber(point.PositiveIndustryRate),
			"industry_flow_signal": sentimentJSONNumber(point.IndustryFlowSignal), "northbound_signal": sentimentJSONNumber(point.NorthboundSignal),
		})
	}
	return json.Marshal(map[string]any{"snapshot": snapshotJSON, "history": history})
}

func sentimentJSONNumber(value float64) any {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return value
}

func calculateMarketSentiment(now time.Time, indices []domain.Quote, flows map[string]domain.BoardFlow, previousAmount domain.MarketAmountSnapshot) domain.MarketSentimentSnapshot {
	snapshot := domain.MarketSentimentSnapshot{GeneratedAt: now, Phase: "数据不足", Score: math.NaN(), CoveragePercent: 0, IndexSignal: math.NaN(), TurnoverSignal: math.NaN(), IndustryBreadth: math.NaN(), PositiveIndustryRate: math.NaN(), IndustryFlowSignal: math.NaN(), Warnings: make([]string, 0, 4)}
	indexValues := make([]float64, 0, len(indices))
	var amount float64
	for _, quote := range indices {
		if len(quote.QuoteTime) >= 10 && snapshot.TradeDate == "" {
			snapshot.TradeDate = quote.QuoteTime[:10]
		}
		if finiteSentiment(quote.Percent) {
			indexValues = append(indexValues, quote.Percent)
		}
		if finiteSentiment(quote.Amount) && quote.Amount > 0 {
			amount += quote.Amount
		}
		if quote.Source != "" {
			snapshot.Source = quote.Source
		}
	}
	if len(indexValues) > 0 {
		mean := 0.0
		for _, value := range indexValues {
			mean += value
		}
		mean /= float64(len(indexValues))
		snapshot.IndexSignal = clampSentiment(50+mean*8, 0, 100)
		snapshot.CoveragePercent += 25
	}
	if amount > 0 {
		// Quote.Amount is in ten-thousand yuan. Compare with the previous
		// exchange-wide snapshot, which uses the same unit.
		previous := previousAmount.Shanghai + previousAmount.Shenzhen + previousAmount.Beijing
		if previous > 0 {
			snapshot.TurnoverSignal = clampSentiment(50+(amount/previous-1)*100, 0, 100)
		} else {
			snapshot.TurnoverSignal = 50
		}
		snapshot.CoveragePercent += 15
	}
	if len(flows) > 0 {
		rise, fall, flat, positive, flowPositive, flowSamples := 0, 0, 0, 0, 0.0, 0
		industries := make([]domain.MarketSentimentIndustry, 0, len(flows))
		for _, board := range flows {
			switch {
			case board.Percent > 0:
				rise++
				positive++
			case board.Percent < 0:
				fall++
			default:
				flat++
			}
			if finiteSentiment(board.MainNet) {
				flowSamples++
				flowPositive += math.Copysign(1, board.MainNet)
			}
			boardTotal := board.RiseCount + board.FallCount + board.FlatCount
			breadth := 50.0
			if boardTotal > 0 {
				breadth = clampSentiment(50+float64(board.RiseCount-board.FallCount)/float64(boardTotal)*50, 0, 100)
			}
			priceSignal := clampSentiment(50+board.Percent*10, 0, 100)
			flowSignal := 50.0
			if finiteSentiment(board.MainNet) {
				switch {
				case board.MainNet > 0:
					flowSignal = 100
				case board.MainNet < 0:
					flowSignal = 0
				}
			}
			industries = append(industries, domain.MarketSentimentIndustry{
				Code: board.Code, Name: board.Name, Percent: board.Percent, MainNet: board.MainNet,
				RiseCount: board.RiseCount, FallCount: board.FallCount, FlatCount: board.FlatCount,
				Breadth: breadth, Score: math.Round((breadth*.5+priceSignal*.3+flowSignal*.2)*10) / 10,
			})
		}
		total := rise + fall + flat
		snapshot.RiseCount, snapshot.FallCount, snapshot.FlatCount, snapshot.IndustryCount = rise, fall, flat, len(flows)
		if total > 0 {
			snapshot.IndustryBreadth = clampSentiment(50+float64(rise-fall)/float64(total)*50, 0, 100)
			snapshot.CoveragePercent += 35
		}
		snapshot.PositiveIndustryRate = float64(positive) / float64(len(flows)) * 100
		snapshot.CoveragePercent += 15
		if flowSamples > 0 {
			snapshot.IndustryFlowSignal = clampSentiment(50+flowPositive/float64(flowSamples)*50, 0, 100)
			snapshot.CoveragePercent += 10
		}
		sort.SliceStable(industries, func(left, right int) bool {
			if industries[left].Score == industries[right].Score {
				return industries[left].Name < industries[right].Name
			}
			return industries[left].Score > industries[right].Score
		})
		snapshot.StrongIndustries = topSentimentIndustries(industries, 5)
		sort.SliceStable(industries, func(left, right int) bool {
			if industries[left].Score == industries[right].Score {
				return industries[left].Name < industries[right].Name
			}
			return industries[left].Score < industries[right].Score
		})
		snapshot.WeakIndustries = topSentimentIndustries(industries, 5)
	}
	components, weights := []float64{}, []float64{}
	for _, item := range []struct{ value, weight float64 }{{snapshot.IndexSignal, .25}, {snapshot.TurnoverSignal, .15}, {snapshot.IndustryBreadth, .35}, {snapshot.PositiveIndustryRate, .15}, {snapshot.IndustryFlowSignal, .10}} {
		if finiteSentiment(item.value) {
			components = append(components, item.value)
			weights = append(weights, item.weight)
		}
	}
	if len(components) == 0 {
		snapshot.Warnings = append(snapshot.Warnings, "指数、成交额和行业扩散均不可用")
		return snapshot
	}
	totalWeight, weighted := 0.0, 0.0
	for index, value := range components {
		totalWeight += weights[index]
		weighted += value * weights[index]
	}
	snapshot.Score = math.Round(weighted/totalWeight*10) / 10
	snapshot.Phase = sentimentPhase(snapshot.Score)
	if !snapshot.LimitCountsAvailable {
		snapshot.Warnings = append(snapshot.Warnings, "涨停/跌停、炸板率和连板梯队尚未接入")
	}
	if snapshot.CoveragePercent < 70 {
		snapshot.Warnings = append(snapshot.Warnings, "当前为行业扩散代理情绪，不能等同全市场涨跌家数")
	}
	return snapshot
}

func sentimentPhase(score float64) string {
	switch {
	case !finiteSentiment(score):
		return "数据不足"
	case score < 20:
		return "冰点"
	case score < 40:
		return "弱势"
	case score < 60:
		return "修复"
	case score < 80:
		return "强势"
	default:
		return "高潮/过热"
	}
}

func finiteSentiment(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func clampSentiment(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func appendSentimentHistory(history []domain.MarketSentimentPoint, snapshot domain.MarketSentimentSnapshot) []domain.MarketSentimentPoint {
	if !finiteSentiment(snapshot.Score) {
		return history
	}
	history = append(history, domain.MarketSentimentPoint{
		At: snapshot.GeneratedAt, Score: snapshot.Score, Phase: snapshot.Phase,
		IndexSignal: snapshot.IndexSignal, TurnoverSignal: snapshot.TurnoverSignal,
		IndustryBreadth: snapshot.IndustryBreadth, PositiveIndustryRate: snapshot.PositiveIndustryRate,
		IndustryFlowSignal: snapshot.IndustryFlowSignal, NorthboundSignal: snapshot.NorthboundSignal,
	})
	cutoff := snapshot.GeneratedAt.Add(-24 * time.Hour)
	start := sort.Search(len(history), func(index int) bool { return !history[index].At.Before(cutoff) })
	if start > 0 {
		history = append([]domain.MarketSentimentPoint(nil), history[start:]...)
	}
	if len(history) > 360 {
		history = append([]domain.MarketSentimentPoint(nil), history[len(history)-360:]...)
	}
	return history
}

func normalizeSentimentHistory(history []domain.MarketSentimentPoint, now time.Time) []domain.MarketSentimentPoint {
	if len(history) == 0 {
		return nil
	}
	sort.SliceStable(history, func(left, right int) bool { return history[left].At.Before(history[right].At) })
	cutoff := now.Add(-24 * time.Hour)
	start := sort.Search(len(history), func(index int) bool { return !history[index].At.Before(cutoff) })
	if start > 0 {
		history = history[start:]
	}
	if len(history) > 360 {
		history = history[len(history)-360:]
	}
	return append([]domain.MarketSentimentPoint(nil), history...)
}

func topSentimentIndustries(items []domain.MarketSentimentIndustry, limit int) []domain.MarketSentimentIndustry {
	if limit <= 0 || len(items) == 0 {
		return nil
	}
	if len(items) < limit {
		limit = len(items)
	}
	result := make([]domain.MarketSentimentIndustry, limit)
	copy(result, items[:limit])
	return result
}

func sentimentWarningText(snapshot domain.MarketSentimentSnapshot) string {
	return strings.Join(snapshot.Warnings, "；")
}
