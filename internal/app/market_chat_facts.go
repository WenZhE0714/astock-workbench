package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

func (app *App) collectMarketChatFacts(ctx context.Context, progress stockReportProgress) (domain.MarketChatFacts, error) {
	if app.quotes == nil || app.history == nil {
		return domain.MarketChatFacts{}, fmt.Errorf("大盘行情服务未初始化")
	}
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	reportStockProgress(progress, "采集三大指数、沪深成交额与行业扩散")
	type fetchResult struct {
		kind   string
		quotes []domain.Quote
		bars   []domain.DailyBar
		flows  map[string]domain.FundFlow
		boards map[string]domain.BoardFlow
		amount domain.MarketAmountSnapshot
		err    error
	}
	results := make(chan fetchResult, 7)
	go func() {
		q, e := app.quotes.Fetch(ctx, market.QuoteMarketSymbols)
		results <- fetchResult{kind: "quotes", quotes: q, err: e}
	}()
	go func() {
		r := fetchResult{kind: "flows"}
		if app.flows != nil {
			r.flows, r.err = app.flows.Fetch(ctx, market.BroadMarketSymbols)
		}
		results <- r
	}()
	go func() {
		r := fetchResult{kind: "industries"}
		if app.industryFlows != nil {
			r.boards, r.err = app.industryFlows.FetchIndustryFlows(ctx)
		}
		results <- r
	}()
	go func() {
		r := fetchResult{kind: "amount"}
		if app.amounts != nil {
			r.amount, r.err = app.amounts.FetchPreviousMarketAmount(ctx)
		}
		results <- r
	}()
	for _, symbol := range market.BroadMarketSymbols {
		go func(symbol string) {
			bars, err := app.history.FetchDailyBars(ctx, symbol)
			results <- fetchResult{kind: symbol, bars: bars, err: err}
		}(symbol)
	}
	fetched := make(map[string]fetchResult)
	warnings := []string{"行业扩散按已覆盖行业计数，不是全市场个股涨跌家数；本轮未采集全市场涨跌停统计", "未采集分钟资金增量和已核验宏观事件，不能据此判断持续承接或政策催化"}
	for range 7 {
		select {
		case <-ctx.Done():
			return domain.MarketChatFacts{}, ctx.Err()
		case r := <-results:
			fetched[r.kind] = r
			if r.err != nil {
				warnings = append(warnings, r.kind+": "+r.err.Error())
			}
		}
	}
	facts := domain.MarketChatFacts{GeneratedAt: time.Now(), Warnings: warnings}
	facts.TradingDates = realtime.TradingDatesFromBars(fetched["sh000001"].bars)
	facts.MarketStatus = marketSessionAt(facts.GeneratedAt).Label
	quotes := map[string]domain.Quote{}
	for _, quote := range fetched["quotes"].quotes {
		quotes[quote.Symbol] = quote
	}
	for _, symbol := range market.BroadMarketSymbols {
		bars, warning := completedDailyBars(fetched[symbol].bars, facts.GeneratedAt)
		if warning != "" {
			facts.Warnings = append(facts.Warnings, symbol+": "+warning)
		}
		// Amount and turnover are optional for index price/volume indicators.
		// Keep their missing values in the evidence, and normalize only the calculation copy.
		calculationBars := append([]domain.DailyBar(nil), bars...)
		for i := range calculationBars {
			if !finite(calculationBars[i].Amount) {
				calculationBars[i].Amount = 0
			}
		}
		technical, err := strategy.AnalyzeTechnical(symbol, calculationBars)
		if err != nil {
			facts.Warnings = append(facts.Warnings, symbol+"日K不可用: "+err.Error())
			continue
		}
		quote, ok := quotes[symbol]
		if !ok || parseStockReportNumber(quote.Current) <= 0 {
			facts.Warnings = append(facts.Warnings, symbol+"报价缺失，仅保留完整日K")
			quote = domain.Quote{Symbol: symbol, Current: fmt.Sprintf("%.2f", technical.Price), Source: technical.DataSource}
		}
		if warning := quoteFreshnessWarning(quote.QuoteTime, facts.GeneratedAt); warning != "" {
			facts.Warnings = append(facts.Warnings, symbol+": "+warning)
		}
		if warning := dailyFreshnessWarning(technical.DataDate, technical.DataSource, facts.GeneratedAt); warning != "" {
			facts.Warnings = append(facts.Warnings, symbol+": "+warning)
		}
		clean := domain.StockReportFacts{Quote: stockQuoteSnapshot(quote), Technical: technical}
		sanitizeStockReportFacts(&clean)
		clean.Technical.Action, clean.Technical.OptionLike, clean.Technical.PositionPlan = "", "", ""
		clean.Technical.Chart = nil
		index := domain.MarketChatIndex{Quote: clean.Quote, Technical: clean.Technical}
		for _, bar := range bars[max(0, len(bars)-60):] {
			point := domain.MarketChatBar{Date: bar.Date, Close: bar.Close}
			if finite(bar.Volume) && bar.Volume >= 0 {
				value := bar.Volume
				point.Volume = &value
			}
			if finite(bar.Amount) && bar.Amount >= 0 {
				value := bar.Amount
				point.Amount = &value
			}
			index.RecentBars = append(index.RecentBars, point)
		}
		if fund, ok := fetched["flows"].flows[symbol]; ok && fund.Symbol == symbol && finite(fund.MainNet) {
			clean.Fund = fund
			sanitizeStockReportFacts(&clean)
			index.Fund = &clean.Fund
			if fund.DataTime == "" {
				facts.Warnings = append(facts.Warnings, symbol+"累计资金未提供成交时间，获取时间不能代替成交时间")
			}
		} else {
			facts.Warnings = append(facts.Warnings, symbol+"累计资金缺失，不作为零流入")
		}
		facts.Indices = append(facts.Indices, index)
	}
	if len(facts.Indices) == 0 {
		return facts, fmt.Errorf("三大指数日K均不可用")
	}
	// A cache error may carry an old list; it must not describe current breadth.
	facts.Breadth = marketChatBreadth(fetched["industries"].boards, fetched["industries"].err)
	if !facts.Breadth.Available {
		facts.Warnings = append(facts.Warnings, "行业扩散数据不可用，不能推断市场普涨或普跌")
	}
	facts.Amount = marketChatTurnover(quotes, fetched["amount"].amount, facts.MarketStatus)
	for _, index := range facts.Indices {
		if index.Quote.Symbol == "sh000001" {
			n := len(index.RecentBars)
			if n < 2 || facts.Amount.PreviousDate != index.RecentBars[n-2].Date || !strings.HasPrefix(index.Quote.QuoteTime, index.RecentBars[n-1].Date) {
				facts.Amount.Comparable = false
				facts.Amount.ChangePercent = 0
			}
		}
	}
	if !facts.Amount.Comparable {
		facts.Warnings = append(facts.Warnings, "沪深成交额缺少可比完整交易日口径，不能据此判定放量或缩量")
	}
	facts.Warnings = sortedWarnings(facts.Warnings)
	return finalizeMarketChatFacts(facts)
}

func marketChatBreadth(boards map[string]domain.BoardFlow, fetchErr error) domain.MarketChatBreadth {
	result := domain.MarketChatBreadth{Scope: "已覆盖行业，不等同全市场个股家数"}
	if fetchErr != nil {
		return result
	}
	seen := map[string]bool{}
	for _, board := range boards {
		key := board.Code
		if key == "" {
			key = board.Name
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result.Industries++
		if !finite(board.Percent) {
			result.Missing++
			continue
		}
		result.Available = true
		switch {
		case board.Percent > 0:
			result.Rising++
		case board.Percent < 0:
			result.Falling++
		default:
			result.Flat++
		}
		available := finite(board.MainNet)
		clean := domain.StockReportFacts{Boards: []domain.BoardFlow{board}}
		clean.Boards[0].Quote = nil
		sanitizeStockReportFacts(&clean)
		item := domain.MarketBoardAssessment{BoardFlow: clean.Boards[0], FlowAvailable: available}
		if board.Percent > 0 {
			result.Strong = append(result.Strong, item)
		}
		if board.Percent < 0 {
			result.Weak = append(result.Weak, item)
		}
	}
	sort.Slice(result.Strong, func(i, j int) bool {
		if result.Strong[i].Percent == result.Strong[j].Percent {
			return result.Strong[i].Code < result.Strong[j].Code
		}
		return result.Strong[i].Percent > result.Strong[j].Percent
	})
	sort.Slice(result.Weak, func(i, j int) bool {
		if result.Weak[i].Percent == result.Weak[j].Percent {
			return result.Weak[i].Code < result.Weak[j].Code
		}
		return result.Weak[i].Percent < result.Weak[j].Percent
	})
	result.Strong = result.Strong[:min(5, len(result.Strong))]
	result.Weak = result.Weak[:min(5, len(result.Weak))]
	return result
}

func marketChatTurnover(quotes map[string]domain.Quote, previous domain.MarketAmountSnapshot, status string) domain.MarketChatAmount {
	result := domain.MarketChatAmount{Basis: "shanghai-shenzhen", Scope: "沪深两市（上证+深证综指），单位万元", PreviousDate: previous.TradeDate}
	sh, sz := quotes["sh000001"], quotes["sz399106"]
	if finite(sh.Amount) && finite(sz.Amount) && sh.Amount > 0 && sz.Amount > 0 {
		result.Available = true
		result.Current = sh.Amount + sz.Amount
	}
	if finite(previous.Shanghai) && finite(previous.Shenzhen) && previous.Shanghai > 0 && previous.Shenzhen > 0 {
		result.Previous = previous.Shanghai + previous.Shenzhen
	}
	date := ""
	if len(sh.QuoteTime) >= 10 && len(sz.QuoteTime) >= 10 && sh.QuoteTime[:10] == sz.QuoteTime[:10] {
		date = sh.QuoteTime[:10]
	}
	_, validDate := parseFactDate(previous.TradeDate)
	result.QuoteDate = date
	result.Comparable = result.Available && result.Previous > 0 && validDate && date > previous.TradeDate && status == "已收盘"
	if result.Comparable {
		result.ChangePercent = (result.Current/result.Previous - 1) * 100
	}
	return result
}

func marketChatFreshness(facts domain.MarketChatFacts) string {
	parts := make([]string, 0, len(facts.Indices))
	for _, index := range facts.Indices {
		parts = append(parts, fmt.Sprintf("%s行情%s/日K%s", index.Quote.Symbol, index.Quote.QuoteTime, index.Technical.DataDate))
	}
	return fmt.Sprintf("采集%s；%s；市场%s；警告%d项", facts.GeneratedAt.Format("2006-01-02 15:04:05"), strings.Join(parts, "；"), facts.MarketStatus, len(facts.Warnings))
}
