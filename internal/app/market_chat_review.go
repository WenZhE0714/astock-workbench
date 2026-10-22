package app

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func (service webAIChatService) MarketRecord(_ context.Context, id string) (domain.MarketChatRecord, error) {
	if service.app == nil || service.app.aiChats == nil {
		return domain.MarketChatRecord{}, fmt.Errorf("大盘记录存储未初始化")
	}
	return service.app.aiChats.LoadMarketRecord(id)
}

func (service webAIChatService) ReviewMarket(ctx context.Context, id string) (domain.MarketChatReview, error) {
	record, err := service.MarketRecord(ctx, id)
	if err != nil {
		return domain.MarketChatReview{}, err
	}
	if !record.Assessment.Evaluable {
		return evaluateMarketRecord(record, nil, time.Now())
	}
	if service.app.history == nil {
		return domain.MarketChatReview{}, fmt.Errorf("日K服务未初始化")
	}
	bars, err := service.app.history.FetchDailyBars(ctx, record.Assessment.AnchorSymbol)
	if err != nil {
		return domain.MarketChatReview{}, err
	}
	return evaluateMarketRecord(record, bars, time.Now())
}

func evaluateMarketRecord(record domain.MarketChatRecord, bars []domain.DailyBar, now time.Time) (domain.MarketChatReview, error) {
	a := record.Assessment
	result := domain.MarketChatReview{Assessment: a, CheckedAt: now}
	if a.AnchorSymbol != "sh000001" || a.BaseClose <= 0 || !finite(a.BaseClose) || a.NeutralBandPercent != 1 {
		return result, fmt.Errorf("判断基准无效")
	}
	var future []domain.DailyBar
	if a.Evaluable {
		found := false
		previous := ""
		today := now.In(shanghaiLocation).Format(time.DateOnly)
		for _, bar := range bars {
			if _, ok := parseFactDate(bar.Date); !ok || bar.Date <= previous || bar.Close <= 0 || !finite(bar.Close) {
				return result, fmt.Errorf("复核日K日期或价格无效")
			}
			if bar.Symbol != "" && bar.Symbol != a.AnchorSymbol {
				return result, fmt.Errorf("复核日K证券不匹配")
			}
			previous = bar.Date
			if bar.Date > today || bar.Date == today && marketSessionAt(now).Label != "已收盘" {
				continue
			}
			if bar.Date == a.BaseDate {
				found = true
				if math.Abs(bar.Close-a.BaseClose) > 0.005 {
					return result, fmt.Errorf("历史基准收盘价已变化，暂停方向复核")
				}
			}
			if bar.Date > a.BaseDate {
				future = append(future, bar)
			}
		}
		if !found {
			return result, fmt.Errorf("当前日K区间未覆盖归档基准，无法完整复核")
		}
	}
	for _, prediction := range a.Predictions {
		if (prediction.TradingDays != 5 && prediction.TradingDays != 20) || !validMarketDirection(prediction.Direction) {
			return result, fmt.Errorf("归档方向或期限无效")
		}
		outcome := domain.MarketChatOutcome{TradingDays: prediction.TradingDays, Direction: prediction.Direction, Status: "waiting", ObservedDays: min(len(future), prediction.TradingDays)}
		if !a.Evaluable {
			outcome.Status = "ineligible"
		} else if len(future) >= prediction.TradingDays {
			end := future[prediction.TradingDays-1]
			outcome.EndDate = end.Date
			outcome.ReturnPercent = (end.Close/a.BaseClose - 1) * 100
			outcome.RealizedDirection = "range"
			if end.Close > a.BaseClose*(1+a.NeutralBandPercent/100) {
				outcome.RealizedDirection = "bullish"
			}
			if end.Close < a.BaseClose*(1-a.NeutralBandPercent/100) {
				outcome.RealizedDirection = "bearish"
			}
			outcome.Status = "completed"
			if prediction.Direction != "unknown" {
				matched := prediction.Direction == outcome.RealizedDirection
				outcome.Matched = &matched
			}
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}
	return result, nil
}
