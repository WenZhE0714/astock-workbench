package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type marketChatQuotesMock struct{}

func (marketChatQuotesMock) Fetch(context.Context, []string) ([]domain.Quote, error) {
	var quotes []domain.Quote
	for _, symbol := range []string{"sh000001", "sz399001", "sz399006", "sz399106", "bj899050"} {
		quotes = append(quotes, domain.Quote{Symbol: symbol, Name: symbol, Current: "144.50", QuoteTime: time.Now().AddDate(0, 0, -1).Format(time.DateOnly) + " 15:00:00", Amount: 1e7})
	}
	return quotes, nil
}

type marketChatHistoryMock struct{}

func (marketChatHistoryMock) FetchDailyBars(_ context.Context, symbol string) ([]domain.DailyBar, error) {
	bars := stockReportBars(symbol)
	for i := range bars {
		bars[i].Turnover = math.NaN()
		bars[i].Amount = math.NaN()
	}
	return bars, nil
}

type marketChatIndustriesMock struct{}

func (marketChatIndustriesMock) FetchIndustryFlows(context.Context) (map[string]domain.BoardFlow, error) {
	return map[string]domain.BoardFlow{"科技": {Code: "BK1", Name: "科技", Percent: 2, MainNet: math.NaN()}, "银行": {Code: "BK2", Name: "银行", Percent: -1, MainNet: -1e8}}, nil
}

type forbiddenStockContext struct{}

func (forbiddenStockContext) FetchBoards(context.Context, string) ([]domain.BoardFlow, error) {
	panic("market chat fetched stock boards")
}
func (forbiddenStockContext) FetchDragonTiger(context.Context, string) (domain.DragonTigerSnapshot, error) {
	panic("market chat fetched dragon tiger")
}
func (forbiddenStockContext) FetchStockNews(context.Context, string, int) ([]domain.StockNewsItem, error) {
	panic("market chat fetched stock news")
}
func (forbiddenStockContext) FetchBrokerResearch(context.Context, string, time.Time, time.Time, int) ([]domain.BrokerResearchItem, error) {
	panic("market chat fetched company research")
}

type marketAnswerMock struct {
	prompts    []string
	structured int
}

func (m *marketAnswerMock) Synthesize(_ context.Context, prompt string) (string, error) {
	m.prompts = append(m.prompts, prompt)
	return "已有指数证据支持谨慎判断", nil
}
func (m *marketAnswerMock) SynthesizeJSON(_ context.Context, prompt string, _ []byte, out any) error {
	m.prompts = append(m.prompts, prompt)
	m.structured++
	return json.Unmarshal([]byte(`{"short_direction":"range","short_reason":"短线分化","medium_direction":"bearish","medium_reason":"中期结构承压","answer":"未来5个交易日震荡，20个交易日偏空。"}`), out)
}
func TestMarketChatUsesMarketEvidenceAndFreezesDirectionalRecord(t *testing.T) {
	model := &marketAnswerMock{}
	store := storage.NewAIChatStore(t.TempDir())
	app := &App{quotes: marketChatQuotesMock{}, history: marketChatHistoryMock{}, industryFlows: marketChatIndustriesMock{},
		boards: forbiddenStockContext{}, dragonTiger: forbiddenStockContext{}, news: forbiddenStockContext{}, research: forbiddenStockContext{}, marketReportAI: model, aiChats: store}
	result, err := app.answerAIChatQuestionDetailed(context.Background(), "market", nil, nil, "现在大盘趋势怎么样，后续看涨还是看跌", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Fallback || result.Assessment == nil || model.structured != 1 || len(model.prompts) != 4 {
		t.Fatalf("unexpected result: %+v", result)
	}
	for _, p := range model.prompts {
		for _, required := range []string{"sh000001", "sz399001", "sz399006", "现在大盘趋势怎么样", "industry_breadth", "未来5个交易日", "未来20个交易日"} {
			if !strings.Contains(p, required) {
				t.Fatalf("missing market context %s", required)
			}
		}
		if strings.Contains(p, `"announcements"`) || strings.Contains(p, `"dragon_tiger"`) {
			t.Fatal("company evidence leaked")
		}
	}
	record, err := store.LoadMarketRecord(result.Assessment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Facts.SnapshotHash != result.FactsHash || len(record.Facts.Indices) != 3 || record.Assessment.Predictions[1].TradingDays != 20 {
		t.Fatal("snapshot or fixed horizons were lost")
	}
	for _, run := range result.Agents {
		if run.FactsHash != result.FactsHash {
			t.Fatal("agent hash mismatch")
		}
	}
	if record.Facts.Breadth.Strong[0].FlowAvailable {
		t.Fatal("missing flow counted as zero")
	}
}

func TestMarketBreadthExcludesStaleCacheAndPreservesCoverage(t *testing.T) {
	boards := map[string]domain.BoardFlow{"a": {Code: "1", Percent: 2}, "duplicate": {Code: "1", Percent: 2}, "b": {Code: "2", Percent: -1}, "missing": {Code: "3", Percent: math.NaN()}}
	got := marketChatBreadth(boards, nil)
	if got.Industries != 3 || got.Rising != 1 || got.Falling != 1 || got.Missing != 1 {
		t.Fatalf("bad coverage: %+v", got)
	}
	if marketChatBreadth(boards, fmt.Errorf("cached old snapshot")).Available {
		t.Fatal("stale cache counted as current breadth")
	}
}

func TestMarketTurnoverUsesMatchedShanghaiShenzhenScope(t *testing.T) {
	quotes := map[string]domain.Quote{"sh000001": {Amount: 60, QuoteTime: "2026-10-21 15:00:00"}, "sz399106": {Amount: 60, QuoteTime: "2026-10-21 15:00:00"}, "bj899050": {Amount: 9000}}
	previous := domain.MarketAmountSnapshot{TradeDate: "2026-10-20", Shanghai: 50, Shenzhen: 50, Beijing: 8000}
	got := marketChatTurnover(quotes, previous, "已收盘")
	if !got.Comparable || got.Current != 120 || got.Previous != 100 || math.Abs(got.ChangePercent-20) > 1e-9 {
		t.Fatalf("mismatched scope: %+v", got)
	}
	if marketChatTurnover(quotes, previous, "交易中").Comparable {
		t.Fatal("intraday vs full-day compared")
	}
	q := quotes["sz399106"]
	q.QuoteTime = "2026-10-20 15:00:00"
	quotes["sz399106"] = q
	if marketChatTurnover(quotes, previous, "已收盘").Comparable {
		t.Fatal("mixed quote days compared")
	}
}

func marketReviewFixture() (domain.MarketChatRecord, []domain.DailyBar, time.Time) {
	a := domain.MarketChatAssessment{AnchorSymbol: "sh000001", BaseDate: "2026-09-01", BaseClose: 100, NeutralBandPercent: 1, Evaluable: true,
		Predictions: []domain.MarketChatPrediction{{TradingDays: 5, Direction: "bullish"}, {TradingDays: 20, Direction: "unknown"}}}
	date := time.Date(2026, 9, 1, 0, 0, 0, 0, shanghaiLocation)
	var bars []domain.DailyBar
	for len(bars) < 22 {
		if date.Weekday() != time.Saturday && date.Weekday() != time.Sunday {
			bars = append(bars, domain.DailyBar{Symbol: "sh000001", Date: date.Format(time.DateOnly), Close: 100 + float64(len(bars))})
		}
		date = date.AddDate(0, 0, 1)
	}
	return domain.MarketChatRecord{Assessment: a}, bars, time.Date(2026, 10, 21, 20, 0, 0, 0, shanghaiLocation)
}
func TestMarketReviewUsesTradingBarsAndDoesNotScoreUnknown(t *testing.T) {
	record, bars, now := marketReviewFixture()
	got, err := evaluateMarketRecord(record, bars, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcomes[0].EndDate != bars[5].Date || got.Outcomes[0].Matched == nil || !*got.Outcomes[0].Matched {
		t.Fatalf("wrong endpoint: %+v", got)
	}
	if got.Outcomes[1].Matched != nil || got.Outcomes[1].EndDate != bars[20].Date {
		t.Fatal("unknown direction was scored")
	}
	waiting, err := evaluateMarketRecord(record, bars[:4], now)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Outcomes[0].Status != "waiting" || waiting.Outcomes[0].ObservedDays != 3 || waiting.Outcomes[0].Matched != nil {
		t.Fatal("premature scoring")
	}
	bars[0].Close = 99
	if _, err := evaluateMarketRecord(record, bars, now); err == nil {
		t.Fatal("changed baseline accepted")
	}
}
func TestMarketReviewExcludesUnfinishedBarsAndIneligibleForecasts(t *testing.T) {
	record, bars, _ := marketReviewFixture()
	now, _ := time.ParseInLocation("2006-01-02 15:04", bars[5].Date+" 10:00", shanghaiLocation)
	got, err := evaluateMarketRecord(record, bars, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcomes[0].ObservedDays != 4 || got.Outcomes[0].Status != "waiting" {
		t.Fatal("unfinished or future bars used")
	}
	record.Assessment.Evaluable = false
	got, err = evaluateMarketRecord(record, nil, now)
	if err != nil || got.Outcomes[0].Status != "ineligible" {
		t.Fatal("ineligible record scored")
	}
}
