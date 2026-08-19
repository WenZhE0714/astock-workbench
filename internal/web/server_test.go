package web

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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
