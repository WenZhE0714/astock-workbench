package web

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type resolverStub struct{}

func (resolverStub) Resolve(_ context.Context, input string) (string, error) {
	if strings.HasPrefix(strings.ToLower(input), "th") {
		return strings.ToLower(input), nil
	}
	if len(input) == 6 && strings.HasPrefix(input, "88") {
		return "th" + input, nil
	}
	if strings.HasPrefix(strings.ToUpper(input), "BK") {
		return strings.ToUpper(input), nil
	}
	if strings.HasPrefix(input, "sh") || strings.HasPrefix(input, "sz") {
		return input, nil
	}
	if strings.Contains(input, "贵州") {
		return "sh600519", nil
	}
	return "sh" + input, nil
}

type boardDetailStub struct{}

func (boardDetailStub) FetchBoard(_ context.Context, code string) (domain.BoardFlow, []domain.MarketStockSnapshot, error) {
	return domain.BoardFlow{
			Code: code, Name: "半导体", Kind: domain.BoardKindIndustry, Percent: 2.35,
			Quote:   &domain.BoardQuoteSnapshot{Price: 1234.5, Delta: 12.3, Open: 1220, PreviousClose: 1222.2, High: 1240, Low: 1218, Volume: 345.6, Amount: 8.9e9},
			MainNet: 1.2e9, MainRatio: 6.8, Turnover: 3.2, RiseCount: 48, FallCount: 7,
			ChangeRank: 3, UniverseSize: 90, LeaderName: "测试龙头", LeaderCode: "600001", LeaderPercent: 9.98,
		}, []domain.MarketStockSnapshot{{
			Symbol: "sh600001", Name: "测试龙头", Price: 12.3, Percent: 9.98, Speed: .23, Turnover: 4.5, VolumeRatio: 1.8, Amount: 8e8, MainNet: 1.1e8,
		}}, nil
}

func TestIndicesEndpointReturnsThreeOrderedMarketQuotes(t *testing.T) {
	server := NewServer(
		resolverStub{}, marketQuoteStub{}, historyStub{}, minuteStub{}, "600519",
		WithMarketAmount(marketAmountStub{}),
	)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/indices", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response marketIndicesResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 3 {
		t.Fatalf("unexpected index count: %+v", response.Items)
	}
	wantSymbols := []string{"sh000001", "sz399001", "sz399006"}
	wantNames := []string{"上证指数", "深证成指", "创业板指"}
	for index := range wantSymbols {
		item := response.Items[index]
		if item.Symbol != wantSymbols[index] || item.Name != wantNames[index] || item.Current != "12.34" || item.Percent == nil || *item.Percent != 1.2 {
			t.Fatalf("unexpected index item %d: %+v", index, item)
		}
	}
	if response.MarketAmount == nil {
		t.Fatal("market amount is missing")
	}
	amount := response.MarketAmount
	if amount.Current != 267e6 || amount.Previous != 251e6 || amount.Delta != 16e6 || math.Abs(amount.Percent-6.374501992031879) > 1e-9 {
		t.Fatalf("unexpected market amount: %+v", amount)
	}
}

func TestPreviousMarketAmountBarSkipsCurrentDayWhenAmountMatches(t *testing.T) {
	bars := []domain.DailyBar{
		{Date: "2026-08-15", Amount: 900e9},
		{Date: "2026-08-16", Amount: 1_100e9},
		{Date: "2026-08-17", Amount: 1_200e9},
	}
	bar, ok := previousMarketAmountBar(bars, 120e6)
	if !ok || bar.Date != "2026-08-16" {
		t.Fatalf("expected previous bar when latest matches current amount: %+v, %v", bar, ok)
	}
}

func TestPreviousMarketAmountBarUsesLatestWhenHistoryLagsQuote(t *testing.T) {
	bars := []domain.DailyBar{
		{Date: "2026-08-15", Amount: 900e9},
		{Date: "2026-08-16", Amount: 1_100e9},
	}
	bar, ok := previousMarketAmountBar(bars, 20e6)
	if !ok || bar.Date != "2026-08-16" {
		t.Fatalf("expected latest completed bar when history lags quote: %+v, %v", bar, ok)
	}
}

type quoteStub struct{}

func (quoteStub) Fetch(_ context.Context, symbols []string) ([]domain.Quote, error) {
	quotes := make([]domain.Quote, 0, len(symbols))
	for _, symbol := range symbols {
		quotes = append(quotes, domain.Quote{Symbol: symbol, Name: "测试股票", Current: "12.34", Percent: 1.2, Delta: .15, QuoteTime: "2026-08-17 10:00:00", LimitUp: "13.57", LimitDown: "11.11"})
	}
	return quotes, nil
}

type marketQuoteStub struct{}

func (marketQuoteStub) Fetch(_ context.Context, symbols []string) ([]domain.Quote, error) {
	amounts := map[string]float64{
		"sh000001": 120e6,
		"sz399106": 145e6,
		"bj899050": 2e6,
	}
	quotes := make([]domain.Quote, 0, len(symbols))
	for _, symbol := range symbols {
		quotes = append(quotes, domain.Quote{
			Symbol: symbol, Source: "测试", Name: "测试指数", Current: "12.34",
			Percent: 1.2, Delta: .15, Amount: amounts[symbol], QuoteTime: "2026-08-17 10:00:00",
		})
	}
	return quotes, nil
}

type marketAmountStub struct{}

func (marketAmountStub) FetchPreviousMarketAmount(context.Context) (domain.MarketAmountSnapshot, error) {
	return domain.MarketAmountSnapshot{
		TradeDate: "2026-08-16", Shanghai: 110e6, Shenzhen: 140e6, Beijing: 1e6, Source: "测试历史",
	}, nil
}

type unavailableNumberQuoteStub struct{}

func (unavailableNumberQuoteStub) Fetch(_ context.Context, symbols []string) ([]domain.Quote, error) {
	return []domain.Quote{{Symbol: symbols[0], Name: "测试股票", Current: "12.34", Delta: math.NaN(), Percent: math.NaN(), Amount: math.NaN()}}, nil
}

type historyStub struct{}

func (historyStub) FetchDailyBars(_ context.Context, symbol string) ([]domain.DailyBar, error) {
	return []domain.DailyBar{{Symbol: symbol, Source: "测试", Date: "2026-08-15", Open: 12, Close: 12.3, High: 12.5, Low: 11.8, Volume: 100}}, nil
}

type minuteStub struct{}

func (minuteStub) FetchMinutePoints(_ context.Context, symbol string) ([]domain.MinutePoint, error) {
	return []domain.MinutePoint{{Symbol: symbol, Source: "测试", TradeDate: "2026-08-17", Time: "09:30", Price: 12.3, Average: 12.2, Volume: 100}}, nil
}

type strategyEngineStub struct {
	request backtest.Request
}

func (stub *strategyEngineStub) Run(_ context.Context, request backtest.Request) (backtest.Result, error) {
	stub.request = request
	equity := make([]backtest.EquityPoint, 252)
	for index := range equity {
		equity[index] = backtest.EquityPoint{
			Date:   time.Date(2023, 1, 3, 0, 0, 0, 0, time.Local).AddDate(0, 0, index).Format("2006-01-02"),
			Equity: 1_000_000 + float64(index)*500,
		}
	}
	return backtest.Result{
		RunID: "WEB-RUN", GeneratedAt: time.Date(2026, 8, 19, 12, 0, 0, 0, time.Local), Request: request,
		Metrics: backtest.Metrics{
			TotalReturn: 12.3, AnnualizedReturn: 5.2, AnnualizedVolatility: 11.2,
			MaxDrawdown: -8.1, Sharpe: 1.1, Sortino: 1.5, Calmar: .64,
			BenchmarkAvailable: true, BenchmarkReturn: 8, ExcessReturn: 4.3,
			Trades: 31, Wins: 18, Losses: 13, WinRate: 58.06, ProfitFactor: 1.4, FinalEquity: 1_123_000,
		},
		Equity:          equity,
		BenchmarkEquity: []backtest.BenchmarkPoint{{Date: "2023-01-03", Close: 4000}, {Date: "2025-12-31", Close: 4320, Return: 8}},
		DataSources:     map[string]string{"sh600519": "测试不复权"},
		DataCoverage: map[string]backtest.DataCoverage{"sh600519": {
			RequestedStart: "2023-01-01", RequestedEnd: "2025-12-31", FirstDate: "2023-01-03", LastDate: "2025-12-31", Bars: 730, CoverageRatio: .99,
		}},
	}, nil
}

type strategyArchiveStub struct {
	result backtest.Result
}

func (stub *strategyArchiveStub) Save(result backtest.Result) (backtest.Result, error) {
	stub.result = result
	return result, nil
}

func (stub *strategyArchiveStub) Load(id string) (backtest.Result, error) {
	if stub.result.RunID != id {
		return backtest.Result{}, fmt.Errorf("未找到回测运行 %s", id)
	}
	return stub.result, nil
}

func (stub *strategyArchiveStub) List(_ int) ([]storage.BacktestIndexEntry, error) {
	if stub.result.RunID == "" {
		return nil, nil
	}
	return []storage.BacktestIndexEntry{{
		RunID: stub.result.RunID, GeneratedAt: stub.result.GeneratedAt, Strategy: stub.result.Request.Strategy,
		Trades: stub.result.Metrics.Trades, TotalReturn: stub.result.Metrics.TotalReturn, MaxDrawdown: stub.result.Metrics.MaxDrawdown,
	}}, nil
}

type countingHistoryStub struct {
	calls int
}

func (stub *countingHistoryStub) FetchDailyBars(_ context.Context, symbol string) ([]domain.DailyBar, error) {
	stub.calls++
	return []domain.DailyBar{{Symbol: symbol, Source: "测试", Date: "2026-08-15", Open: 12, Close: 12.3, High: 12.5, Low: 11.8, Volume: 100}}, nil
}

func TestIndexContainsDefaultSymbol(t *testing.T) {
	server := NewServer(resolverStub{}, quoteStub{}, historyStub{}, minuteStub{}, "600519")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "600519") {
		t.Fatalf("unexpected index response %d", recorder.Code)
	}
}

func TestStockEndpointReturnsQuoteHistoryAndMinutes(t *testing.T) {
	server := NewServer(resolverStub{}, quoteStub{}, historyStub{}, minuteStub{}, "600519")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/stock?symbol=贵州巨石&limit=60", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response stockResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Symbol != "sh600519" || response.Quote == nil || len(response.Bars) != 1 || len(response.Minutes) != 1 {
		t.Fatalf("unexpected stock response: %+v", response)
	}
	if response.Minutes[0].Time != "09:30" || response.Minutes[0].Average != 12.2 {
		t.Fatalf("unexpected minute response: %+v", response.Minutes[0])
	}
}

func TestStockEndpointReturnsBoardDetailWithoutStockAdapters(t *testing.T) {
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithBoardDetails(boardDetailStub{}))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/stock?symbol=BK0423", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response stockResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Kind != domain.AssetKindSector || response.Board == nil || response.Board.Name != "半导体" || len(response.Board.Leaders) != 1 {
		t.Fatalf("unexpected board response: %+v", response)
	}
	if response.Board.MainNet == nil || *response.Board.MainNet != 1.2e9 {
		t.Fatalf("unexpected board flow: %+v", response.Board)
	}
	if response.Board.Quote == nil || response.Board.Quote.Price == nil || *response.Board.Quote.Price != 1234.5 || response.Board.ChangeRank != 3 || response.Board.UniverseSize != 90 {
		t.Fatalf("unexpected board quote: %+v", response.Board)
	}
	if response.Board.Leaders[0].Speed == nil || *response.Board.Leaders[0].Speed != .23 || response.Board.Leaders[0].Turnover == nil || *response.Board.Leaders[0].Turnover != 4.5 {
		t.Fatalf("unexpected board leader dimensions: %+v", response.Board.Leaders[0])
	}
}

func TestStockEndpointRoutesTHSIndustryCodeToBoardDetail(t *testing.T) {
	server := NewServer(resolverStub{}, nil, nil, nil, "", WithBoardDetails(boardDetailStub{}))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/stock?symbol=881155", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response stockResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Symbol != "th881155" || response.Kind != domain.AssetKindSector || response.Board == nil || response.Board.Code != "th881155" {
		t.Fatalf("unexpected THS industry response: %+v", response)
	}
}

func TestStockEndpointRequiresSymbolWhenNoDefault(t *testing.T) {
	server := NewServer(resolverStub{}, quoteStub{}, historyStub{}, minuteStub{}, "")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/stock", nil))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "缺少") {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestStockEndpointCachesDailyHistoryBetweenQuoteRefreshes(t *testing.T) {
	history := &countingHistoryStub{}
	server := NewServer(resolverStub{}, quoteStub{}, history, minuteStub{}, "600519")
	for range 2 {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/stock?symbol=600519", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
		}
	}
	if history.calls != 1 {
		t.Fatalf("daily history fetched %d times, want one fetch within cache TTL", history.calls)
	}
}

func TestStockEndpointSerializesUnavailableNumbersAsNull(t *testing.T) {
	server := NewServer(resolverStub{}, unavailableNumberQuoteStub{}, historyStub{}, minuteStub{}, "600519")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/stock", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
	}
	for _, expected := range []string{`"delta":null`, `"percent":null`, `"amount":null`} {
		if !strings.Contains(recorder.Body.String(), expected) {
			t.Fatalf("response missing %s: %s", expected, recorder.Body.String())
		}
	}
}

func TestWatchlistEndpointSharesGroupsAndMutations(t *testing.T) {
	file := filepath.Join(t.TempDir(), "watchlist")
	nameCacheFile := filepath.Join(t.TempDir(), "names.tsv")
	names, err := storage.LoadNameCache(nameCacheFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := names.Remember([]domain.Candidate{{Symbol: "sz000001", Name: "平安银行"}}); err != nil {
		t.Fatal(err)
	}
	if err := storage.SaveWatchlistGroups(file, []storage.WatchlistGroup{
		{Name: storage.DefaultWatchlistGroup, Symbols: []string{"sh600519"}},
		{Name: "科技", Symbols: []string{"sz000001"}},
	}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(
		resolverStub{}, quoteStub{}, historyStub{}, minuteStub{}, "600519",
		WithWatchlist(file), WithNameCache(nameCacheFile),
	)

	get := httptest.NewRecorder()
	server.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/watchlist", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("unexpected watchlist GET status %d: %s", get.Code, get.Body.String())
	}
	var response watchlistResponse
	if err := json.Unmarshal(get.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Groups) != 2 || response.Groups[1].Name != "科技" || len(response.Groups[1].Symbols) != 1 {
		t.Fatalf("unexpected watchlist groups: %+v", response)
	}
	if len(response.Groups[1].Items) != 1 || response.Groups[1].Items[0].Name != "平安银行" {
		t.Fatalf("unexpected watchlist names: %+v", response.Groups[1].Items)
	}
	if response.Groups[1].Items[0].Price != "12.34" || response.Groups[1].Items[0].Percent == nil || *response.Groups[1].Items[0].Percent != 1.2 {
		t.Fatalf("unexpected watchlist quote: %+v", response.Groups[1].Items[0])
	}

	add := httptest.NewRecorder()
	addRequest := httptest.NewRequest(http.MethodPost, "/api/watchlist", strings.NewReader(`{"symbol":"600176","group":"科技"}`))
	server.Handler().ServeHTTP(add, addRequest)
	if add.Code != http.StatusOK || !strings.Contains(add.Body.String(), `"added":true`) {
		t.Fatalf("unexpected watchlist POST response %d: %s", add.Code, add.Body.String())
	}

	remove := httptest.NewRecorder()
	server.Handler().ServeHTTP(remove, httptest.NewRequest(http.MethodDelete, "/api/watchlist?symbol=600176&group=%E7%A7%91%E6%8A%80", nil))
	if remove.Code != http.StatusOK || !strings.Contains(remove.Body.String(), `"removed":true`) {
		t.Fatalf("unexpected watchlist DELETE response %d: %s", remove.Code, remove.Body.String())
	}
	groups, _, err := storage.LoadWatchlistGroups(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := storage.WatchlistSymbols(groups, "科技"); len(got) != 1 || got[0] != "sz000001" {
		t.Fatalf("unexpected final technology group: %v", got)
	}
}

func TestStrategyBacktestEndpointRunsSharedEnginePersistsAndLists(t *testing.T) {
	engine := &strategyEngineStub{}
	archive := &strategyArchiveStub{}
	server := NewServer(
		resolverStub{}, quoteStub{}, historyStub{}, minuteStub{}, "600519",
		WithStrategyResearch(engine, archive),
	)
	body := `{
		"symbols":["600519"],"start":"2023-01-01","end":"2025-12-31","entry_mode":"trend-reclaim",
		"fast_ma":20,"slow_ma":60,"breakout_days":20,"volume_ratio_min":1.2,
		"stop_loss_percent":8,"take_profit_percent":20,"max_holding_days":40,"max_position_percent":20,
		"initial_cash":1000000,"commission_bps":3,"stamp_duty_bps":5,"slippage_bps":5,"benchmark":"sh000300"
	}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/strategy/backtests", strings.NewReader(body))
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected strategy POST status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response strategyBacktestResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if engine.request.Technical.EntryMode != backtest.EntryModeReclaim || engine.request.Benchmark != "sh000300" ||
		engine.request.Adjustment != backtest.AdjustmentNone || !engine.request.NoFutureData || engine.request.PointInTimePool {
		t.Fatalf("shared engine request lost research constraints: %+v", engine.request)
	}
	if response.Result.RunID != "WEB-RUN" || response.Result.Metrics.Sortino != 1.5 || len(response.Result.MarketRegimes) == 0 || !response.Assessment.Passed {
		t.Fatalf("unexpected strategy response: %+v", response)
	}

	list := httptest.NewRecorder()
	server.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/strategy/backtests?limit=10", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"run_id":"WEB-RUN"`) {
		t.Fatalf("unexpected strategy list: %d %s", list.Code, list.Body.String())
	}
	detail := httptest.NewRecorder()
	server.Handler().ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/strategy/backtests?id=WEB-RUN", nil))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"benchmark_equity"`) || !strings.Contains(detail.Body.String(), `"market_regimes"`) {
		t.Fatalf("unexpected strategy detail: %d %s", detail.Code, detail.Body.String())
	}
}

func TestStrategyBacktestRejectsBoardUniverse(t *testing.T) {
	server := NewServer(
		resolverStub{}, quoteStub{}, historyStub{}, minuteStub{}, "",
		WithStrategyResearch(&strategyEngineStub{}, &strategyArchiveStub{}),
	)
	body := `{
		"symbols":["BK0423"],"start":"2023-01-01","end":"2025-12-31","entry_mode":"breakout",
		"fast_ma":20,"slow_ma":60,"breakout_days":20,"volume_ratio_min":1.2,
		"stop_loss_percent":8,"take_profit_percent":20,"max_holding_days":40,"max_position_percent":20,"initial_cash":1000000
	}`
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/strategy/backtests", strings.NewReader(body)))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "不是可用于") {
		t.Fatalf("board should not enter A-share strategy pool: %d %s", recorder.Code, recorder.Body.String())
	}
}
