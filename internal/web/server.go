// Package web exposes a small read-only browser surface over the existing
// market adapters. It deliberately does not contain a second quote or
// backtest implementation.
package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

//go:embed dist
var assets embed.FS

const (
	quoteCacheTTL   = 5 * time.Second
	minuteCacheTTL  = 5 * time.Second
	boardCacheTTL   = 5 * time.Second
	historyCacheTTL = time.Minute
	amountCacheTTL  = 10 * time.Minute
)

type marketIndexDefinition struct {
	Symbol string
	Name   string
}

var marketIndexDefinitions = []marketIndexDefinition{
	{Symbol: "sh000001", Name: "上证指数"},
	{Symbol: "sz399001", Name: "深证成指"},
	{Symbol: "sz399006", Name: "创业板指"},
}

var marketAmountSymbols = []string{"sh000001", "sz399106", "bj899050"}

type QuoteClient interface {
	Fetch(context.Context, []string) ([]domain.Quote, error)
}

type DailyHistoryClient interface {
	FetchDailyBars(context.Context, string) ([]domain.DailyBar, error)
}

type MinuteClient interface {
	FetchMinutePoints(context.Context, string) ([]domain.MinutePoint, error)
}

type BoardDetailClient interface {
	FetchBoard(context.Context, string) (domain.BoardFlow, []domain.MarketStockSnapshot, error)
}

type MarketAmountClient interface {
	FetchPreviousMarketAmount(context.Context) (domain.MarketAmountSnapshot, error)
}

type SymbolResolver interface {
	Resolve(context.Context, string) (string, error)
}

type Server struct {
	resolver      SymbolResolver
	quotes        QuoteClient
	history       DailyHistoryClient
	minutes       MinuteClient
	boardDetails  BoardDetailClient
	marketAmounts MarketAmountClient
	defaultSymbol string
	watchlistFile string
	nameCacheFile string
	handler       http.Handler
	quoteMu       sync.Mutex
	quoteCache    map[string]quoteCacheEntry
	minuteMu      sync.Mutex
	minuteCache   map[string]minuteCacheEntry
	historyMu     sync.Mutex
	historyCache  map[string]historyCacheEntry
	boardMu       sync.Mutex
	boardCache    map[string]boardCacheEntry
	amountMu      sync.Mutex
	amountCache   marketAmountCacheEntry
	watchlistMu   sync.Mutex
}

type quoteCacheEntry struct {
	quote     domain.Quote
	fetchedAt time.Time
}

type historyCacheEntry struct {
	bars      []domain.DailyBar
	fetchedAt time.Time
}

type minuteCacheEntry struct {
	points    []domain.MinutePoint
	fetchedAt time.Time
}

type marketAmountCacheEntry struct {
	snapshot  domain.MarketAmountSnapshot
	fetchedAt time.Time
	valid     bool
}

type boardCacheEntry struct {
	flow      domain.BoardFlow
	leaders   []domain.MarketStockSnapshot
	fetchedAt time.Time
}

type stockResponse struct {
	Symbol       string                `json:"symbol"`
	Kind         domain.AssetKind      `json:"kind"`
	Name         string                `json:"name,omitempty"`
	Quote        *quoteResponse        `json:"quote,omitempty"`
	Bars         []chartBar            `json:"bars,omitempty"`
	Minutes      []minutePointResponse `json:"minutes,omitempty"`
	FetchedAt    string                `json:"fetched_at"`
	QuoteError   string                `json:"quote_error,omitempty"`
	HistoryError string                `json:"history_error,omitempty"`
	MinuteError  string                `json:"minute_error,omitempty"`
	BoardError   string                `json:"board_error,omitempty"`
	Board        *boardResponse        `json:"board,omitempty"`
}

type boardResponse struct {
	Code          string                `json:"code"`
	Name          string                `json:"name"`
	Kind          string                `json:"kind"`
	Quote         *boardQuoteResponse   `json:"quote,omitempty"`
	Percent       *float64              `json:"percent"`
	MainNet       *float64              `json:"main_net_yuan"`
	MainRatio     *float64              `json:"main_ratio_percent"`
	Turnover      *float64              `json:"turnover_percent"`
	RiseCount     int                   `json:"rise_count"`
	FallCount     int                   `json:"fall_count"`
	FlatCount     int                   `json:"flat_count"`
	LeaderName    string                `json:"leader_name,omitempty"`
	LeaderCode    string                `json:"leader_code,omitempty"`
	LeaderPercent *float64              `json:"leader_percent"`
	ChangeRank    int                   `json:"change_rank"`
	UniverseSize  int                   `json:"universe_size"`
	Leaders       []boardLeaderResponse `json:"leaders,omitempty"`
}

type boardQuoteResponse struct {
	Price         *float64 `json:"price"`
	Delta         *float64 `json:"delta"`
	Open          *float64 `json:"open"`
	PreviousClose *float64 `json:"previous_close"`
	High          *float64 `json:"high"`
	Low           *float64 `json:"low"`
	Volume        *float64 `json:"volume_wan_lots"`
	Amount        *float64 `json:"amount_yuan"`
}

type boardLeaderResponse struct {
	Symbol      string   `json:"symbol"`
	Name        string   `json:"name"`
	Price       *float64 `json:"price"`
	Percent     *float64 `json:"percent"`
	Speed       *float64 `json:"speed_percent"`
	Turnover    *float64 `json:"turnover_percent"`
	VolumeRatio *float64 `json:"volume_ratio"`
	Amount      *float64 `json:"amount_yuan"`
	MainNet     *float64 `json:"main_net_yuan"`
	Industry    string   `json:"industry,omitempty"`
}

type quoteResponse struct {
	Symbol        string   `json:"symbol"`
	Source        string   `json:"source"`
	Name          string   `json:"name"`
	Code          string   `json:"code"`
	Current       string   `json:"current"`
	PreviousClose string   `json:"previous_close"`
	Open          string   `json:"open"`
	QuoteTime     string   `json:"quote_time"`
	Delta         *float64 `json:"delta"`
	Percent       *float64 `json:"percent"`
	High          string   `json:"high"`
	Low           string   `json:"low"`
	Amount        *float64 `json:"amount"`
	Turnover      string   `json:"turnover"`
	LimitUp       string   `json:"limit_up"`
	LimitDown     string   `json:"limit_down"`
	VolumeRatio   string   `json:"volume_ratio"`
}

type marketIndexResponse struct {
	Symbol    string   `json:"symbol"`
	Name      string   `json:"name"`
	Current   string   `json:"current"`
	Delta     *float64 `json:"delta"`
	Percent   *float64 `json:"percent"`
	QuoteTime string   `json:"quote_time"`
	Source    string   `json:"source"`
}

type marketIndicesResponse struct {
	Items         []marketIndexResponse `json:"items"`
	MarketAmount  *marketAmountResponse `json:"market_amount,omitempty"`
	FetchedAt     string                `json:"fetched_at"`
	Warning       string                `json:"warning,omitempty"`
	AmountWarning string                `json:"amount_warning,omitempty"`
}

type marketAmountResponse struct {
	TradeDate string  `json:"trade_date"`
	Current   float64 `json:"current_wan_yuan"`
	Previous  float64 `json:"previous_wan_yuan"`
	Delta     float64 `json:"delta_wan_yuan"`
	Percent   float64 `json:"percent"`
	Source    string  `json:"source"`
}

type chartBar struct {
	Symbol string  `json:"symbol"`
	Source string  `json:"source"`
	Date   string  `json:"date"`
	Open   float64 `json:"open"`
	Close  float64 `json:"close"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Volume float64 `json:"volume"`
}

type minutePointResponse struct {
	Source    string  `json:"source"`
	TradeDate string  `json:"trade_date"`
	Time      string  `json:"time"`
	Price     float64 `json:"price"`
	Average   float64 `json:"average"`
	Volume    float64 `json:"volume"`
	Amount    float64 `json:"amount_yuan"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type watchlistGroupResponse struct {
	Name    string                  `json:"name"`
	Symbols []string                `json:"symbols"`
	Items   []watchlistItemResponse `json:"items"`
}

type watchlistItemResponse struct {
	Symbol    string           `json:"symbol"`
	Kind      domain.AssetKind `json:"kind"`
	Name      string           `json:"name,omitempty"`
	Price     string           `json:"price,omitempty"`
	Percent   *float64         `json:"percent,omitempty"`
	QuoteTime string           `json:"quote_time,omitempty"`
}

type watchlistResponse struct {
	Groups   []watchlistGroupResponse `json:"groups"`
	Warnings []string                 `json:"warnings,omitempty"`
}

type watchlistMutationRequest struct {
	Symbol string `json:"symbol"`
	Group  string `json:"group"`
}

type watchlistMutationResponse struct {
	Symbol  string `json:"symbol"`
	Group   string `json:"group"`
	Added   bool   `json:"added,omitempty"`
	Removed bool   `json:"removed,omitempty"`
}

type ServerOption func(*Server)

func WithWatchlist(file string) ServerOption {
	return func(server *Server) {
		server.watchlistFile = strings.TrimSpace(file)
	}
}

func WithNameCache(file string) ServerOption {
	return func(server *Server) {
		server.nameCacheFile = strings.TrimSpace(file)
	}
}

func WithMarketAmount(client MarketAmountClient) ServerOption {
	return func(server *Server) {
		server.marketAmounts = client
	}
}

func WithBoardDetails(client BoardDetailClient) ServerOption {
	return func(server *Server) {
		server.boardDetails = client
	}
}

// NewServer uses options so embedders that only need the quote surface do not
// have to configure the shared CLI watchlist and name cache.
func NewServer(resolver SymbolResolver, quotes QuoteClient, history DailyHistoryClient, minutes MinuteClient, defaultSymbol string, options ...ServerOption) *Server {
	server := &Server{
		resolver:      resolver,
		quotes:        quotes,
		history:       history,
		minutes:       minutes,
		defaultSymbol: strings.TrimSpace(defaultSymbol),
		quoteCache:    make(map[string]quoteCacheEntry),
		minuteCache:   make(map[string]minuteCacheEntry),
		historyCache:  make(map[string]historyCacheEntry),
		boardCache:    make(map[string]boardCacheEntry),
	}
	for _, option := range options {
		if option != nil {
			option(server)
		}
	}
	server.handler = server.routes()
	return server
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/indices", s.handleIndices)
	mux.HandleFunc("/api/stock", s.handleStock)
	mux.HandleFunc("/api/watchlist", s.handleWatchlist)
	staticAssets, err := fs.Sub(assets, "dist")
	if err == nil {
		mux.Handle("/assets/", http.FileServer(http.FS(staticAssets)))
	}
	mux.HandleFunc("/", s.handleIndex)
	return withHeaders(mux)
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) Serve(ctx context.Context, address string) error {
	if strings.TrimSpace(address) == "" {
		return fmt.Errorf("web 监听地址不能为空")
	}
	server := &http.Server{Addr: address, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	serveError := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveError <- err
			return
		}
		serveError <- nil
	}()
	select {
	case err := <-serveError:
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	}
}

func withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		if request.URL.Path == "/api/stock" || strings.HasPrefix(request.URL.Path, "/api/") {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
		next.ServeHTTP(writer, request)
	})
}

func (s *Server) handleIndex(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(writer, request)
		return
	}
	page, err := template.ParseFS(assets, "dist/index.html")
	if err != nil {
		http.Error(writer, "页面资源不可用", http.StatusInternalServerError)
		return
	}
	data := struct{ DefaultSymbol string }{DefaultSymbol: s.defaultSymbol}
	if err := page.Execute(writer, data); err != nil {
		return
	}
}

func (s *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok", "service": "astock-web"})
}

func (s *Server) handleIndices(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "指数行情只支持 GET"})
		return
	}
	if s.quotes == nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "实时行情服务未初始化"})
		return
	}
	symbols := make([]string, 0, len(marketIndexDefinitions))
	for _, definition := range marketIndexDefinitions {
		symbols = append(symbols, definition.Symbol)
	}
	allSymbols := append([]string(nil), symbols...)
	allSymbols = append(allSymbols, marketAmountSymbols[1:]...)
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	quotes, fetchError := s.fetchQuoteBatch(ctx, allSymbols)
	response := marketIndicesResponse{
		Items:     make([]marketIndexResponse, 0, len(marketIndexDefinitions)),
		FetchedAt: time.Now().Format(time.RFC3339),
	}
	available := 0
	for _, definition := range marketIndexDefinitions {
		item := marketIndexResponse{Symbol: definition.Symbol, Name: definition.Name}
		if quote, found := quotes[definition.Symbol]; found {
			available++
			item.Current = quote.Current
			item.Delta = finitePointer(quote.Delta)
			item.Percent = finitePointer(quote.Percent)
			item.QuoteTime = quote.QuoteTime
			item.Source = quote.Source
		}
		response.Items = append(response.Items, item)
	}
	if fetchError != nil {
		response.Warning = fetchError.Error()
	} else if available < len(marketIndexDefinitions) {
		response.Warning = fmt.Sprintf("仅返回 %d/%d 个指数行情", available, len(marketIndexDefinitions))
	}
	amount, amountError := s.marketAmount(ctx, quotes)
	if amount != nil {
		response.MarketAmount = amount
	}
	if amountError != nil {
		response.AmountWarning = amountError.Error()
	}
	if available == 0 {
		if response.Warning == "" {
			response.Warning = "未返回有效指数行情"
		}
		writeJSON(writer, http.StatusBadGateway, response)
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

func (s *Server) marketAmount(ctx context.Context, quotes map[string]domain.Quote) (*marketAmountResponse, error) {
	values := make(map[string]float64, len(quotes))
	for symbol, quote := range quotes {
		if math.IsNaN(quote.Amount) || math.IsInf(quote.Amount, 0) || quote.Amount <= 0 {
			continue
		}
		values[symbol] = quote.Amount
	}
	shanghai := values["sh000001"]
	shenzhen := values["sz399106"]
	if shenzhen <= 0 {
		shenzhen = values["sz399001"]
	}
	beijing := values["bj899050"]
	current := shanghai + shenzhen + math.Max(beijing, 0)
	if current <= 0 {
		return nil, fmt.Errorf("今日成交额暂不可用")
	}
	if s.marketAmounts == nil {
		return nil, fmt.Errorf("上一交易日成交额服务未初始化")
	}
	previous, err := s.fetchPreviousMarketAmount(ctx)
	if err != nil {
		previous, err = s.fetchPreviousMarketAmountFromHistory(ctx, quotes)
		if err != nil {
			return nil, err
		}
	}
	previousTotal := previous.Shanghai + previous.Shenzhen + math.Max(previous.Beijing, 0)
	if previousTotal <= 0 {
		return nil, fmt.Errorf("上一交易日成交额不完整")
	}
	return &marketAmountResponse{
		TradeDate: previous.TradeDate,
		Current:   current,
		Previous:  previousTotal,
		Delta:     current - previousTotal,
		Percent:   (current/previousTotal - 1) * 100,
		Source:    previous.Source,
	}, nil
}

func (s *Server) fetchPreviousMarketAmount(ctx context.Context) (domain.MarketAmountSnapshot, error) {
	now := time.Now()
	s.amountMu.Lock()
	if s.amountCache.valid && now.Sub(s.amountCache.fetchedAt) >= 0 && now.Sub(s.amountCache.fetchedAt) < amountCacheTTL {
		snapshot := s.amountCache.snapshot
		s.amountMu.Unlock()
		return snapshot, nil
	}
	s.amountMu.Unlock()
	snapshot, err := s.marketAmounts.FetchPreviousMarketAmount(ctx)
	if err != nil {
		return domain.MarketAmountSnapshot{}, err
	}
	s.amountMu.Lock()
	s.amountCache = marketAmountCacheEntry{snapshot: snapshot, fetchedAt: now, valid: true}
	s.amountMu.Unlock()
	return snapshot, nil
}

func previousMarketAmountBar(bars []domain.DailyBar, currentWanYuan float64) (domain.DailyBar, bool) {
	valid := make([]domain.DailyBar, 0, len(bars))
	for _, bar := range bars {
		if bar.Date != "" && bar.Amount > 0 && !math.IsNaN(bar.Amount) && !math.IsInf(bar.Amount, 0) {
			valid = append(valid, bar)
		}
	}
	if len(valid) == 0 {
		return domain.DailyBar{}, false
	}
	latest := valid[len(valid)-1]
	latestWanYuan := latest.Amount / 1e4
	if len(valid) >= 2 && currentWanYuan > 0 {
		difference := math.Abs(latestWanYuan-currentWanYuan) / math.Max(currentWanYuan, latestWanYuan)
		if difference <= 0.05 {
			return valid[len(valid)-2], true
		}
	}
	return latest, true
}

type marketAmountHistoryResult struct {
	symbol string
	bar    domain.DailyBar
	err    error
}

func (s *Server) fetchPreviousMarketAmountFromHistory(ctx context.Context, quotes map[string]domain.Quote) (domain.MarketAmountSnapshot, error) {
	if s.history == nil {
		return domain.MarketAmountSnapshot{}, fmt.Errorf("日 K 服务未初始化")
	}
	results := make(chan marketAmountHistoryResult, len(marketAmountSymbols))
	for _, symbol := range marketAmountSymbols {
		symbol := symbol
		go func() {
			bars, err := s.fetchDailyBars(ctx, symbol)
			if err != nil {
				results <- marketAmountHistoryResult{symbol: symbol, err: err}
				return
			}
			bar, ok := previousMarketAmountBar(bars, quotes[symbol].Amount)
			if !ok {
				results <- marketAmountHistoryResult{symbol: symbol, err: fmt.Errorf("%s 日 K 缺少成交额", symbol)}
				return
			}
			results <- marketAmountHistoryResult{symbol: symbol, bar: bar}
		}()
	}
	snapshot := domain.MarketAmountSnapshot{Source: "指数日K成交额回退"}
	for range marketAmountSymbols {
		select {
		case result := <-results:
			if result.err != nil {
				return domain.MarketAmountSnapshot{}, result.err
			}
			if snapshot.TradeDate == "" {
				snapshot.TradeDate = result.bar.Date
			} else if snapshot.TradeDate != result.bar.Date {
				return domain.MarketAmountSnapshot{}, fmt.Errorf("指数日 K 成交额交易日不一致: %s / %s", snapshot.TradeDate, result.bar.Date)
			}
			amount := result.bar.Amount / 1e4
			switch result.symbol {
			case "sh000001":
				snapshot.Shanghai = amount
			case "sz399106":
				snapshot.Shenzhen = amount
			case "bj899050":
				snapshot.Beijing = amount
			}
		case <-ctx.Done():
			return domain.MarketAmountSnapshot{}, ctx.Err()
		}
	}
	if snapshot.Shanghai <= 0 || snapshot.Shenzhen <= 0 || snapshot.Beijing <= 0 {
		return domain.MarketAmountSnapshot{}, fmt.Errorf("指数日 K 成交额不完整")
	}
	s.amountMu.Lock()
	s.amountCache = marketAmountCacheEntry{snapshot: snapshot, fetchedAt: time.Now(), valid: true}
	s.amountMu.Unlock()
	return snapshot, nil
}

func (s *Server) handleWatchlist(writer http.ResponseWriter, request *http.Request) {
	if s.watchlistFile == "" {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "自选服务未初始化"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		s.writeWatchlist(writer, request)
	case http.MethodPost:
		s.addWatchlist(writer, request)
	case http.MethodDelete:
		s.removeWatchlist(writer, request)
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "自选仅支持 GET、POST、DELETE"})
	}
}

func (s *Server) writeWatchlist(writer http.ResponseWriter, request *http.Request) {
	s.watchlistMu.Lock()
	groups, warnings, err := storage.LoadWatchlistGroups(s.watchlistFile)
	var names *storage.NameCache
	if err == nil && s.nameCacheFile != "" {
		names, err = storage.LoadNameCache(s.nameCacheFile)
	}
	s.watchlistMu.Unlock()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 8*time.Second)
	defer cancel()
	allSymbols := storage.WatchlistSymbols(groups, storage.AllWatchlistGroup)
	quoteSymbols := make([]string, 0, len(allSymbols))
	for _, symbol := range allSymbols {
		if market.AssetKindOf(symbol) == domain.AssetKindSector {
			continue
		}
		quoteSymbols = append(quoteSymbols, symbol)
	}
	quotes, quoteError := s.fetchQuoteBatch(ctx, quoteSymbols)
	if quoteError != nil {
		warnings = append(warnings, "自选行情暂不可用: "+quoteError.Error())
	}
	response := watchlistResponse{Groups: make([]watchlistGroupResponse, 0, len(groups)), Warnings: warnings}
	for _, group := range groups {
		groupResponse := watchlistGroupResponse{
			Name: group.Name, Symbols: append([]string(nil), group.Symbols...),
			Items: make([]watchlistItemResponse, 0, len(group.Symbols)),
		}
		for _, symbol := range group.Symbols {
			name := ""
			if names != nil {
				name = names.LookupName(symbol)
			}
			item := watchlistItemResponse{Symbol: symbol, Kind: market.AssetKindOf(symbol), Name: name}
			if quote, found := quotes[symbol]; found {
				if item.Name == "" {
					item.Name = quote.Name
				}
				item.Price = quote.Current
				item.Percent = finitePointer(quote.Percent)
				item.QuoteTime = quote.QuoteTime
			}
			if item.Kind == domain.AssetKindSector && s.boardDetails != nil {
				flow, _, boardErr := s.fetchBoard(ctx, symbol)
				if boardErr == nil {
					if item.Name == "" {
						item.Name = flow.Name
					}
					item.Percent = finitePointer(flow.Percent)
				} else {
					response.Warnings = append(response.Warnings, fmt.Sprintf("板块 %s 行情暂不可用: %v", symbol, boardErr))
				}
			}
			groupResponse.Items = append(groupResponse.Items, item)
		}
		response.Groups = append(response.Groups, groupResponse)
	}
	writeJSON(writer, http.StatusOK, response)
}

func (s *Server) addWatchlist(writer http.ResponseWriter, request *http.Request) {
	var input watchlistMutationRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 8<<10)).Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "自选请求格式无效"})
		return
	}
	if strings.TrimSpace(input.Symbol) == "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "缺少证券、转债或板块代码/名称"})
		return
	}
	group := strings.TrimSpace(input.Group)
	if group == "" || group == storage.AllWatchlistGroup {
		group = storage.DefaultWatchlistGroup
	}
	ctx, cancel := context.WithTimeout(request.Context(), 8*time.Second)
	defer cancel()
	if s.resolver == nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "证券解析服务未初始化"})
		return
	}
	symbol, err := s.resolver.Resolve(ctx, strings.TrimSpace(input.Symbol))
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	s.watchlistMu.Lock()
	added, err := storage.AddWatchlistToGroup(s.watchlistFile, group, []string{symbol})
	s.watchlistMu.Unlock()
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, watchlistMutationResponse{Symbol: symbol, Group: group, Added: len(added) == 1 && added[0]})
}

func (s *Server) removeWatchlist(writer http.ResponseWriter, request *http.Request) {
	rawSymbol := strings.TrimSpace(request.URL.Query().Get("symbol"))
	if rawSymbol == "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "缺少证券、转债或板块代码/名称"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 8*time.Second)
	defer cancel()
	if s.resolver == nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "证券解析服务未初始化"})
		return
	}
	symbol, err := s.resolver.Resolve(ctx, rawSymbol)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	group := strings.TrimSpace(request.URL.Query().Get("group"))
	s.watchlistMu.Lock()
	var removed []bool
	if group == "" || group == storage.AllWatchlistGroup {
		removed, err = storage.RemoveWatchlist(s.watchlistFile, []string{symbol})
	} else {
		removed, err = storage.RemoveWatchlistFromGroup(s.watchlistFile, group, []string{symbol})
	}
	s.watchlistMu.Unlock()
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, watchlistMutationResponse{Symbol: symbol, Group: group, Removed: len(removed) == 1 && removed[0]})
}

func (s *Server) handleStock(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "只支持 GET"})
		return
	}
	input := strings.TrimSpace(request.URL.Query().Get("symbol"))
	if input == "" {
		input = s.defaultSymbol
	}
	if input == "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "缺少股票代码或名称"})
		return
	}
	if s.resolver == nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "证券解析服务未初始化"})
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	symbol, err := s.resolver.Resolve(ctx, input)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	if market.AssetKindOf(symbol) == domain.AssetKindSector {
		s.handleBoard(writer, ctx, symbol)
		return
	}
	if s.quotes == nil || s.history == nil || s.minutes == nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "行情服务未初始化"})
		return
	}

	// Quote, minute data and history are independent upstream requests. Fetching them in
	// parallel keeps the chart responsive even when one optional source slows.
	quoteChannel := make(chan struct {
		quote domain.Quote
		err   error
	}, 1)
	historyChannel := make(chan struct {
		bars []domain.DailyBar
		err  error
	}, 1)
	minuteChannel := make(chan struct {
		points []domain.MinutePoint
		err    error
	}, 1)
	go func() {
		quote, fetchError := s.fetchQuote(ctx, symbol)
		if fetchError != nil {
			quoteChannel <- struct {
				quote domain.Quote
				err   error
			}{err: fetchError}
			return
		}
		quoteChannel <- struct {
			quote domain.Quote
			err   error
		}{quote: quote}
	}()
	go func() {
		bars, fetchError := s.fetchDailyBars(ctx, symbol)
		historyChannel <- struct {
			bars []domain.DailyBar
			err  error
		}{bars: limitBars(bars, request.URL.Query().Get("limit")), err: fetchError}
	}()
	go func() {
		points, fetchError := s.fetchMinutePoints(ctx, symbol)
		minuteChannel <- struct {
			points []domain.MinutePoint
			err    error
		}{points: points, err: fetchError}
	}()

	quoteResult := <-quoteChannel
	historyResult := <-historyChannel
	minuteResult := <-minuteChannel
	response := stockResponse{Symbol: symbol, Kind: market.AssetKindOf(symbol), FetchedAt: time.Now().Format(time.RFC3339)}
	if quoteResult.err != nil {
		response.QuoteError = quoteResult.err.Error()
	} else {
		quote := newQuoteResponse(quoteResult.quote)
		response.Quote = &quote
		response.Name = quoteResult.quote.Name
	}
	if historyResult.err != nil {
		response.HistoryError = historyResult.err.Error()
	} else {
		response.Bars = newChartBars(historyResult.bars)
	}
	if minuteResult.err != nil {
		response.MinuteError = minuteResult.err.Error()
	} else {
		response.Minutes = newMinutePoints(minuteResult.points)
	}
	status := http.StatusOK
	if response.Quote == nil && len(response.Bars) == 0 && len(response.Minutes) == 0 {
		status = http.StatusBadGateway
	}
	writeJSON(writer, status, response)
}

func (s *Server) handleBoard(writer http.ResponseWriter, ctx context.Context, symbol string) {
	response := stockResponse{Symbol: symbol, Kind: domain.AssetKindSector, FetchedAt: time.Now().Format(time.RFC3339)}
	if s.boardDetails == nil {
		response.BoardError = "板块行情服务未初始化"
		writeJSON(writer, http.StatusInternalServerError, response)
		return
	}
	flow, leaders, err := s.fetchBoard(ctx, symbol)
	if err != nil {
		response.BoardError = err.Error()
		writeJSON(writer, http.StatusBadGateway, response)
		return
	}
	response.Name = flow.Name
	response.Board = newBoardResponse(flow, leaders)
	writeJSON(writer, http.StatusOK, response)
}

func finitePointer(value float64) *float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	result := value
	return &result
}

func newBoardResponse(flow domain.BoardFlow, leaders []domain.MarketStockSnapshot) *boardResponse {
	result := &boardResponse{
		Code: flow.Code, Name: flow.Name, Kind: flow.Kind,
		Percent: finitePointer(flow.Percent), MainNet: finitePointer(flow.MainNet),
		MainRatio: finitePointer(flow.MainRatio), Turnover: finitePointer(flow.Turnover),
		RiseCount: flow.RiseCount, FallCount: flow.FallCount, FlatCount: flow.FlatCount,
		ChangeRank: flow.ChangeRank, UniverseSize: flow.UniverseSize,
		LeaderName: flow.LeaderName, LeaderCode: flow.LeaderCode, LeaderPercent: finitePointer(flow.LeaderPercent),
		Leaders: make([]boardLeaderResponse, 0, len(leaders)),
	}
	if flow.Quote != nil {
		result.Quote = &boardQuoteResponse{
			Price: finitePointer(flow.Quote.Price), Delta: finitePointer(flow.Quote.Delta),
			Open: finitePointer(flow.Quote.Open), PreviousClose: finitePointer(flow.Quote.PreviousClose),
			High: finitePointer(flow.Quote.High), Low: finitePointer(flow.Quote.Low),
			Volume: finitePointer(flow.Quote.Volume), Amount: finitePointer(flow.Quote.Amount),
		}
	}
	for _, leader := range leaders {
		result.Leaders = append(result.Leaders, boardLeaderResponse{
			Symbol: leader.Symbol, Name: leader.Name, Price: finitePointer(leader.Price), Percent: finitePointer(leader.Percent),
			Speed: finitePointer(leader.Speed), Turnover: finitePointer(leader.Turnover), VolumeRatio: finitePointer(leader.VolumeRatio),
			Amount: finitePointer(leader.Amount), MainNet: finitePointer(leader.MainNet), Industry: leader.Industry,
		})
	}
	return result
}

func newQuoteResponse(quote domain.Quote) quoteResponse {
	return quoteResponse{
		Symbol: quote.Symbol, Source: quote.Source, Name: quote.Name, Code: quote.Code, Current: quote.Current,
		PreviousClose: quote.PreviousClose, Open: quote.Open, QuoteTime: quote.QuoteTime,
		Delta: finitePointer(quote.Delta), Percent: finitePointer(quote.Percent), High: quote.High,
		Low: quote.Low, Amount: finitePointer(quote.Amount), Turnover: quote.Turnover,
		LimitUp: quote.LimitUp, LimitDown: quote.LimitDown, VolumeRatio: quote.VolumeRatio,
	}
}

func newChartBars(bars []domain.DailyBar) []chartBar {
	result := make([]chartBar, 0, len(bars))
	for _, bar := range bars {
		result = append(result, chartBar{
			Symbol: bar.Symbol, Source: bar.Source, Date: bar.Date, Open: bar.Open,
			Close: bar.Close, High: bar.High, Low: bar.Low, Volume: bar.Volume,
		})
	}
	return result
}

func newMinutePoints(points []domain.MinutePoint) []minutePointResponse {
	result := make([]minutePointResponse, 0, len(points))
	for _, point := range points {
		result = append(result, minutePointResponse{
			Source: point.Source, TradeDate: point.TradeDate, Time: point.Time,
			Price: point.Price, Average: point.Average, Volume: point.Volume, Amount: point.Amount,
		})
	}
	return result
}

func (s *Server) fetchBoard(ctx context.Context, symbol string) (domain.BoardFlow, []domain.MarketStockSnapshot, error) {
	now := time.Now()
	s.boardMu.Lock()
	cached, found := s.boardCache[symbol]
	if found && now.Sub(cached.fetchedAt) >= 0 && now.Sub(cached.fetchedAt) < boardCacheTTL {
		leaders := append([]domain.MarketStockSnapshot(nil), cached.leaders...)
		s.boardMu.Unlock()
		return cached.flow, leaders, nil
	}
	s.boardMu.Unlock()
	if s.boardDetails == nil {
		return domain.BoardFlow{}, nil, fmt.Errorf("板块行情服务未初始化")
	}
	flow, leaders, err := s.boardDetails.FetchBoard(ctx, symbol)
	if err != nil {
		return domain.BoardFlow{}, nil, err
	}
	s.boardMu.Lock()
	s.boardCache[symbol] = boardCacheEntry{flow: flow, leaders: append([]domain.MarketStockSnapshot(nil), leaders...), fetchedAt: now}
	s.boardMu.Unlock()
	return flow, leaders, nil
}

func (s *Server) fetchQuote(ctx context.Context, symbol string) (domain.Quote, error) {
	now := time.Now()
	s.quoteMu.Lock()
	cached, found := s.quoteCache[symbol]
	if found && now.Sub(cached.fetchedAt) >= 0 && now.Sub(cached.fetchedAt) < quoteCacheTTL {
		s.quoteMu.Unlock()
		return cached.quote, nil
	}
	s.quoteMu.Unlock()

	quotes, err := s.quotes.Fetch(ctx, []string{symbol})
	if err != nil {
		return domain.Quote{}, err
	}
	for _, quote := range quotes {
		if quote.Symbol != symbol {
			continue
		}
		s.quoteMu.Lock()
		s.quoteCache[symbol] = quoteCacheEntry{quote: quote, fetchedAt: now}
		s.quoteMu.Unlock()
		return quote, nil
	}
	return domain.Quote{}, fmt.Errorf("未返回 %s 的实时行情", symbol)
}

func (s *Server) fetchQuoteBatch(ctx context.Context, symbols []string) (map[string]domain.Quote, error) {
	result := make(map[string]domain.Quote, len(symbols))
	missing := make([]string, 0, len(symbols))
	now := time.Now()
	s.quoteMu.Lock()
	for _, symbol := range symbols {
		cached, found := s.quoteCache[symbol]
		if found {
			result[symbol] = cached.quote
		}
		if !found || now.Sub(cached.fetchedAt) < 0 || now.Sub(cached.fetchedAt) >= quoteCacheTTL {
			missing = append(missing, symbol)
		}
	}
	s.quoteMu.Unlock()
	if len(missing) == 0 {
		return result, nil
	}
	if s.quotes == nil {
		return result, fmt.Errorf("实时行情服务未初始化")
	}
	quotes, err := s.quotes.Fetch(ctx, missing)
	if err != nil {
		return result, err
	}
	wanted := make(map[string]bool, len(missing))
	for _, symbol := range missing {
		wanted[symbol] = true
	}
	s.quoteMu.Lock()
	for _, quote := range quotes {
		if !wanted[quote.Symbol] {
			continue
		}
		result[quote.Symbol] = quote
		s.quoteCache[quote.Symbol] = quoteCacheEntry{quote: quote, fetchedAt: now}
	}
	s.quoteMu.Unlock()
	return result, nil
}

func (s *Server) fetchDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	now := time.Now()
	s.historyMu.Lock()
	cached, found := s.historyCache[symbol]
	if found && now.Sub(cached.fetchedAt) >= 0 && now.Sub(cached.fetchedAt) < historyCacheTTL {
		bars := append([]domain.DailyBar(nil), cached.bars...)
		s.historyMu.Unlock()
		return bars, nil
	}
	s.historyMu.Unlock()

	bars, err := s.history.FetchDailyBars(ctx, symbol)
	if err != nil {
		return nil, err
	}
	s.historyMu.Lock()
	s.historyCache[symbol] = historyCacheEntry{bars: append([]domain.DailyBar(nil), bars...), fetchedAt: now}
	s.historyMu.Unlock()
	return bars, nil
}

func (s *Server) fetchMinutePoints(ctx context.Context, symbol string) ([]domain.MinutePoint, error) {
	now := time.Now()
	s.minuteMu.Lock()
	cached, found := s.minuteCache[symbol]
	if found && now.Sub(cached.fetchedAt) >= 0 && now.Sub(cached.fetchedAt) < minuteCacheTTL {
		points := append([]domain.MinutePoint(nil), cached.points...)
		s.minuteMu.Unlock()
		return points, nil
	}
	s.minuteMu.Unlock()

	points, err := s.minutes.FetchMinutePoints(ctx, symbol)
	if err != nil {
		return nil, err
	}
	s.minuteMu.Lock()
	s.minuteCache[symbol] = minuteCacheEntry{points: append([]domain.MinutePoint(nil), points...), fetchedAt: now}
	s.minuteMu.Unlock()
	return points, nil
}

func limitBars(bars []domain.DailyBar, rawLimit string) []domain.DailyBar {
	limit := 180
	if parsed, err := strconv.Atoi(rawLimit); err == nil {
		limit = parsed
	}
	if limit < 30 {
		limit = 30
	}
	if limit > 300 {
		limit = 300
	}
	if len(bars) <= limit {
		return bars
	}
	return bars[len(bars)-limit:]
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	var buffer bytes.Buffer
	if err := json.NewEncoder(&buffer).Encode(value); err != nil {
		status = http.StatusInternalServerError
		buffer.Reset()
		buffer.WriteString("{\"error\":\"响应编码失败\"}\n")
	}
	writer.WriteHeader(status)
	_, _ = writer.Write(buffer.Bytes())
}
