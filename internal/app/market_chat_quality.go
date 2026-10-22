package app

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

const minimumMarketChatIndustries = 20

func finalizeMarketChatFacts(facts domain.MarketChatFacts) (domain.MarketChatFacts, error) {
	quality := assessMarketChatQuality(facts)
	facts.Quality = &quality
	facts.SnapshotHash = ""
	data, err := json.Marshal(facts)
	if err != nil {
		return facts, fmt.Errorf("大盘快照含无效数据: %w", err)
	}
	facts.SnapshotHash = snapshotHash(data)
	return facts, nil
}

// Calendar coverage is used only inside observed dates. Outside it, weekday
// fallback is conservative: holiday gaps restrict forecasts until dates can be verified.
func marketChatExpectedDates(facts domain.MarketChatFacts) (string, string, bool) {
	local := facts.GeneratedAt.In(shanghaiLocation)
	today := local.Format(time.DateOnly)
	dates := realtime.NormalizeTradingDates(facts.TradingDates)
	trading := local.Weekday() != time.Saturday && local.Weekday() != time.Sunday
	previous := previousTradingDate(facts.GeneratedAt).Format(time.DateOnly)
	if len(dates) > 0 && dates[0] <= today && dates[len(dates)-1] >= today {
		index := sort.SearchStrings(dates, today)
		trading = index < len(dates) && dates[index] == today
		if index > 0 {
			previous = dates[index-1]
		}
	}
	minutes := local.Hour()*60 + local.Minute()
	quoteDate, dailyDate := previous, previous
	if trading && minutes >= 9*60+15 {
		quoteDate = today
	}
	closed := trading && minutes >= 15*60
	if closed {
		dailyDate = today
	}
	return quoteDate, dailyDate, closed
}

func assessMarketChatQuality(facts domain.MarketChatFacts) domain.MarketChatQuality {
	q := domain.MarketChatQuality{Version: 1, Status: "limited", ReadyIndices: []string{}, UsableIndices: []string{}, Reasons: []string{}}
	q.QuoteDate, q.DailyDate, _ = marketChatExpectedDates(facts)
	if facts.GeneratedAt.IsZero() {
		q.Reasons = append(q.Reasons, "采集时间缺失")
		return q
	}
	bySymbol := map[string][]domain.MarketChatIndex{}
	for _, index := range facts.Indices {
		bySymbol[index.Quote.Symbol] = append(bySymbol[index.Quote.Symbol], index)
	}
	for _, symbol := range market.BroadMarketSymbols {
		items := bySymbol[symbol]
		if len(items) != 1 {
			q.Reasons = append(q.Reasons, symbol+"指数数据缺失或重复")
			continue
		}
		index := items[0]
		if !marketChatDailyUsable(index, facts.GeneratedAt) {
			q.Reasons = append(q.Reasons, symbol+"完整日K或指标无法核验")
			continue
		}
		q.UsableIndices = append(q.UsableIndices, symbol)
		if index.Technical.DataDate != q.DailyDate {
			q.Reasons = append(q.Reasons, fmt.Sprintf("%s日K截至%s，所需日期%s（长假需有交易日历证据）", symbol, index.Technical.DataDate, q.DailyDate))
			continue
		}
		at, err := time.ParseInLocation("2006-01-02 15:04:05", index.Quote.QuoteTime, shanghaiLocation)
		if err != nil || at.Format(time.DateOnly) != q.QuoteDate || at.After(facts.GeneratedAt.Add(time.Minute)) || !finite(index.Quote.Price) || index.Quote.Price <= 0 {
			q.Reasons = append(q.Reasons, symbol+"行情时间或价格无效")
			continue
		}
		_, _, closed := marketChatExpectedDates(facts)
		if closed && at.Hour() < 15 {
			q.Reasons = append(q.Reasons, symbol+"尚未取得收盘报价")
			continue
		}
		if marketSessionAt(facts.GeneratedAt).Poll && q.QuoteDate == facts.GeneratedAt.In(shanghaiLocation).Format(time.DateOnly) && facts.GeneratedAt.Sub(at) > 3*time.Minute {
			q.Reasons = append(q.Reasons, symbol+"交易时段行情滞后超过3分钟")
			continue
		}
		if q.QuoteDate == q.DailyDate && math.Abs(index.Quote.Price-index.Technical.Price) > 0.02 {
			q.Reasons = append(q.Reasons, symbol+"报价与完整日K收盘不一致")
			continue
		}
		q.ReadyIndices = append(q.ReadyIndices, symbol)
	}
	amount := facts.Amount
	q.TurnoverReady = amount.Basis == "shanghai-shenzhen" && amount.Available && amount.Comparable && amount.QuoteDate == q.QuoteDate && q.QuoteDate == q.DailyDate &&
		finite(amount.Current) && amount.Current > 0 && finite(amount.Previous) && amount.Previous > 0 && finite(amount.ChangePercent)
	if q.TurnoverReady {
		difference := math.Abs((amount.Current/amount.Previous-1)*100 - amount.ChangePercent)
		q.TurnoverReady = difference < 0.01
		for _, index := range bySymbol["sh000001"] {
			n := len(index.RecentBars)
			if n < 2 || amount.PreviousDate != index.RecentBars[n-2].Date {
				q.TurnoverReady = false
			}
		}
	}
	if !q.TurnoverReady {
		q.Reasons = append(q.Reasons, "沪深成交额缺少同日、同口径的完整交易日比较；盘中不与昨日全天比较")
	}
	b := facts.Breadth
	valid := b.Industries - b.Missing
	q.BreadthReady = b.Available && b.Industries >= minimumMarketChatIndustries && b.Missing >= 0 && b.Rising >= 0 && b.Falling >= 0 && b.Flat >= 0 &&
		b.Rising+b.Falling+b.Flat == valid && valid*100 >= b.Industries*80
	if !q.BreadthReady {
		q.Reasons = append(q.Reasons, "行业扩散覆盖不足：至少20个行业且有效涨跌字段覆盖80%")
	}
	if len(q.ReadyIndices) == len(market.BroadMarketSymbols) && q.TurnoverReady && q.BreadthReady {
		q.Status = "ready"
	}
	return q
}

func marketChatDailyUsable(index domain.MarketChatIndex, now time.Time) bool {
	t := index.Technical
	if t.Status != domain.TechnicalStatusReady || t.Symbol != index.Quote.Symbol || len(index.RecentBars) < 20 {
		return false
	}
	for _, value := range []float64{t.Price, t.MA5, t.MA20, t.MA60} {
		if !finite(value) || value <= 0 {
			return false
		}
	}
	previous := ""
	today := now.In(shanghaiLocation).Format(time.DateOnly)
	if t.DataDate == today && marketSessionAt(now).Label != "已收盘" {
		return false
	}
	for _, bar := range index.RecentBars {
		if _, ok := parseFactDate(bar.Date); !ok || bar.Date <= previous || bar.Date > today || !finite(bar.Close) || bar.Close <= 0 {
			return false
		}
		previous = bar.Date
	}
	last := index.RecentBars[len(index.RecentBars)-1]
	return last.Date == t.DataDate && math.Abs(last.Close-t.Price) < 0.005
}
