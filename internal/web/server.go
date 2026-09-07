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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

//go:embed dist
var assets embed.FS

const (
	quoteCacheTTL           = 5 * time.Second
	minuteCacheTTL          = 5 * time.Second
	boardCacheTTL           = 5 * time.Second
	historyCacheTTL         = time.Minute
	amountCacheTTL          = 10 * time.Minute
	globalMarketCacheTTL    = 30 * time.Second
	globalHistoryCacheTTL   = 5 * time.Minute
	globalMinuteCacheTTL    = 30 * time.Second
	sentimentExtrasCacheTTL = 30 * time.Second
	limitStatsCacheTTL      = 30 * time.Second
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

type globalMarketDefinition struct {
	Symbol string
	Region string
	Name   string
}

// Keep the Web surface focused on the markets most useful for an A-share
// operator. The underlying Sina adapter may fetch more indices for the CLI,
// but the browser should remain a compact market context strip.
var globalMarketDefinitions = []globalMarketDefinition{
	{Symbol: "rt_hkHSI", Region: "港股", Name: "恒生指数"},
	{Symbol: "rt_hkHSTECH", Region: "港股", Name: "恒生科技"},
	{Symbol: "b_NKY", Region: "日本", Name: "日经225"},
	{Symbol: "b_KOSPI", Region: "韩国", Name: "KOSPI"},
	{Symbol: "b_KOSDAQ", Region: "韩国", Name: "KOSDAQ"},
	{Symbol: "gb_ixic", Region: "美国", Name: "纳斯达克"},
	{Symbol: "gb_inx", Region: "美国", Name: "标普500"},
	{Symbol: "gb_dji", Region: "美国", Name: "道琼斯"},
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

type GlobalIndexClient interface {
	FetchGlobalIndices(context.Context) ([]domain.GlobalIndex, error)
}

// GlobalChartClient supplies the historical and intraday series used by the
// dedicated overseas-market view. It is kept separate from the A-share
// history interfaces because global exchanges use different sessions and
// timezones.
type GlobalChartClient interface {
	FetchGlobalDailyBars(context.Context, string) ([]domain.DailyBar, error)
	FetchGlobalMinutePoints(context.Context, string) ([]domain.MinutePoint, error)
}

type SymbolResolver interface {
	Resolve(context.Context, string) (string, error)
}

type backtestArchive interface {
	Save(backtest.Result) (backtest.Result, error)
	Load(string) (backtest.Result, error)
	List(int) ([]storage.BacktestIndexEntry, error)
}

type continuousOptimizationArchive interface {
	All() ([]backtest.ContinuousOptimizationResult, error)
	Load(string) (backtest.ContinuousOptimizationResult, error)
	LoadLifecycle(string) (backtest.CandidateLifecycle, error)
	SaveLifecycle(backtest.CandidateLifecycle) error
}

type realtimeScanner interface {
	Scan(context.Context, []string, bool) (realtime.ScanResult, error)
}

type realtimeCalibratableScanner interface {
	SetCalibration(realtime.ScoreCalibration)
}

type realtimeSectorEnricher interface {
	EnrichSectors(context.Context, realtime.ScanResult) (realtime.ScanResult, error)
}

type realtimeSignalArchive interface {
	List(int) ([]realtime.Signal, error)
	Latest() (realtime.ScanResult, error)
}

type realtimeOutcomeAnalyzer interface {
	Evaluate(context.Context, []realtime.Signal, realtime.OutcomeOptions) (realtime.OutcomeReport, error)
	Report(int, time.Time) (realtime.OutcomeReport, error)
}

type shadowAnalyzer interface {
	Evaluate(context.Context, []realtime.Signal, paper.Options) (paper.Report, error)
}

type shadowAdvancer interface {
	Advance(context.Context, paper.Report, []realtime.Signal, paper.Options) (paper.Report, error)
}

type shadowRealtimeAdvancer interface {
	AdvanceRealtime(context.Context, paper.Report, []realtime.Signal, paper.Options) (paper.Report, error)
}

type shadowArchive interface {
	Save(paper.Report) error
	Load() (paper.Report, error)
}

type shadowExecutionProfile struct {
	ID          string
	Name        string
	Strategy    string
	Description string
	Config      paper.Config
	Archive     shadowArchive
}

type AutomaticResearchResult struct {
	Ran          bool   `json:"ran"`
	ExperimentID string `json:"experiment_id,omitempty"`
	Message      string `json:"message,omitempty"`
}

type automaticResearchRunner func(context.Context, []string, time.Time) (AutomaticResearchResult, error)

type automationStateStore interface {
	Load() (storage.AutomationState, error)
	Save(storage.AutomationState) error
}

type realtimeCalibrationStateStore interface {
	Load() (storage.RealtimeCalibrationState, error)
	Save(storage.RealtimeCalibrationState) error
}

type sentimentHistoryStore interface {
	Load() ([]domain.MarketSentimentPoint, error)
	Save([]domain.MarketSentimentPoint) error
}

type Server struct {
	resolver                  SymbolResolver
	quotes                    QuoteClient
	history                   DailyHistoryClient
	minutes                   MinuteClient
	boardDetails              BoardDetailClient
	relatedBoards             market.BoardFlowClient
	stockNews                 market.StockNewsClient
	marketAmounts             MarketAmountClient
	industryFlows             marketIndustryFlowClient
	sentimentSignals          marketSentimentSignalClient
	limitStats                market.LimitStatsClient
	rankings                  market.MarketRankingClient
	sentimentExtrasMu         sync.Mutex
	sentimentExtrasCache      sentimentExtrasCacheEntry
	limitStatsMu              sync.Mutex
	limitStatsCache           limitStatsCacheEntry
	sentimentMu               sync.Mutex
	sentimentHistory          []domain.MarketSentimentPoint
	sentimentHistoryStore     sentimentHistoryStore
	globalMarkets             GlobalIndexClient
	globalCharts              GlobalChartClient
	strategyEngine            backtest.Engine
	strategyArchive           backtestArchive
	candidateEngine           backtest.Engine
	candidateArchive          continuousOptimizationArchive
	realtimeScanner           realtimeScanner
	realtimeArchive           realtimeSignalArchive
	realtimeOutcomes          realtimeOutcomeAnalyzer
	shadowEvaluator           shadowAnalyzer
	shadowArchive             shadowArchive
	shadowProfiles            map[string]shadowExecutionProfile
	aiChatService             AIChatService
	aiConfigService           AIConfigService
	assistantContextMu        sync.Mutex
	assistantContexts         map[string]assistantContextCacheEntry
	assistantJobsMu           sync.Mutex
	assistantJobs             map[string]*assistantChatJob
	assistantJobSequence      uint64
	defaultSymbol             string
	watchlistFile             string
	nameCacheFile             string
	handler                   http.Handler
	quoteMu                   sync.Mutex
	quoteCache                map[string]quoteCacheEntry
	minuteMu                  sync.Mutex
	minuteCache               map[string]minuteCacheEntry
	historyMu                 sync.Mutex
	historyCache              map[string]historyCacheEntry
	boardMu                   sync.Mutex
	boardCache                map[string]boardCacheEntry
	amountMu                  sync.Mutex
	amountCache               marketAmountCacheEntry
	globalMu                  sync.Mutex
	globalCache               globalMarketCacheEntry
	globalChartMu             sync.Mutex
	globalHistoryCache        map[string]historyCacheEntry
	globalMinuteCache         map[string]minuteCacheEntry
	watchlistMu               sync.Mutex
	strategyMu                sync.Mutex
	strategyRunning           bool
	candidateMu               sync.Mutex
	realtimeMu                sync.Mutex
	realtimeRunning           bool
	realtimeCache             realtime.ScanResult
	realtimeSectorEnriching   bool
	realtimeSectorEnrichedAt  time.Time
	shadowMu                  sync.Mutex
	shadowCalendarMu          sync.Mutex
	shadowCalendarDates       []string
	shadowCalendarFetchedAt   time.Time
	automationMu              sync.Mutex
	automationCtx             context.Context
	automationRunning         bool
	automationLastRun         time.Time
	automationLastSuccess     time.Time
	automationLastError       string
	automationNextRun         time.Time
	automationLastOutcome     time.Time
	automationLastShadow      time.Time
	automationResearch        automaticResearchRunner
	tradingCalendarProvider   realtime.TradingCalendarProvider
	automationStateStore      automationStateStore
	realtimeCalibrationStore  realtimeCalibrationStateStore
	calibrationMu             sync.Mutex
	automationResearchRunning bool
	automationResearchAttempt time.Time
	automationResearchSuccess time.Time
	automationResearchID      string
	automationResearchMessage string
	automationResearchError   string
	automationTasks           map[string]storage.AutomationTaskState
	now                       func() time.Time
}

type AutomationStatus struct {
	Enabled              bool                                   `json:"enabled"`
	Running              bool                                   `json:"running"`
	LastRunAt            time.Time                              `json:"last_run_at,omitempty"`
	LastSuccessAt        time.Time                              `json:"last_success_at,omitempty"`
	LastError            string                                 `json:"last_error,omitempty"`
	NextRunAt            time.Time                              `json:"next_run_at,omitempty"`
	LastOutcomeAt        time.Time                              `json:"last_outcome_at,omitempty"`
	LastShadowAt         time.Time                              `json:"last_shadow_at,omitempty"`
	Tasks                []string                               `json:"tasks"`
	TaskOrder            []string                               `json:"task_order"`
	ResearchRunning      bool                                   `json:"research_running"`
	ResearchAttemptAt    time.Time                              `json:"research_attempt_at,omitempty"`
	ResearchSuccessAt    time.Time                              `json:"research_success_at,omitempty"`
	ResearchExperimentID string                                 `json:"research_experiment_id,omitempty"`
	ResearchMessage      string                                 `json:"research_message,omitempty"`
	ResearchError        string                                 `json:"research_error,omitempty"`
	TaskStates           map[string]storage.AutomationTaskState `json:"task_states,omitempty"`
}

const (
	automationTaskScan      = "scan"
	automationTaskOutcomes  = "outcomes"
	automationTaskShadow    = "shadow"
	automationTaskResearch  = "research"
	automationCycleInterval = 30 * time.Second
	outcomePrimaryHour      = 15
	outcomePrimaryMinute    = 30
	outcomeRetryHour        = 16
	outcomeRetryMinute      = 5
	automaticResearchRetry  = 10 * time.Minute
)

const shadowCalendarCacheTTL = 2 * time.Minute

type automationTaskDefinition struct {
	Key   string
	Label string
}

// Keep task order explicit at the API/UI boundary. JSON object iteration is
// intentionally unordered, while operators need the same scan -> outcomes ->
// shadow -> research sequence on every refresh.
var automationTaskDefinitions = []automationTaskDefinition{
	{Key: automationTaskScan, Label: "扫描"},
	{Key: automationTaskOutcomes, Label: "验证"},
	{Key: automationTaskShadow, Label: "影子"},
	{Key: automationTaskResearch, Label: "研究"},
}

type automaticOutcomePlan struct {
	Run          bool
	RetryAttempt bool
	NextRunAt    time.Time
	Detail       string
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

type globalMarketCacheEntry struct {
	items     []domain.GlobalIndex
	fetchedAt time.Time
	valid     bool
}

type sentimentExtrasCacheEntry struct {
	northbound domain.NorthboundFlowSnapshot
	hot        domain.HotStockSnapshot
	northErr   error
	hotErr     error
	fetchedAt  time.Time
}

type limitStatsCacheEntry struct {
	tradeDate string
	snapshot  domain.LimitStatsSnapshot
	err       error
	fetchedAt time.Time
}

type boardCacheEntry struct {
	flow      domain.BoardFlow
	leaders   []domain.MarketStockSnapshot
	fetchedAt time.Time
}

type stockResponse struct {
	Symbol        string                  `json:"symbol"`
	Kind          domain.AssetKind        `json:"kind"`
	Name          string                  `json:"name,omitempty"`
	Quote         *quoteResponse          `json:"quote,omitempty"`
	Bars          []chartBar              `json:"bars,omitempty"`
	Minutes       []minutePointResponse   `json:"minutes,omitempty"`
	Technical     *domain.TechnicalSignal `json:"technical,omitempty"`
	FetchedAt     string                  `json:"fetched_at"`
	QuoteError    string                  `json:"quote_error,omitempty"`
	HistoryError  string                  `json:"history_error,omitempty"`
	MinuteError   string                  `json:"minute_error,omitempty"`
	BoardError    string                  `json:"board_error,omitempty"`
	Board         *boardResponse          `json:"board,omitempty"`
	RelatedBoards []boardResponse         `json:"related_boards,omitempty"`
	News          []domain.StockNewsItem  `json:"news,omitempty"`
	NewsError     string                  `json:"news_error,omitempty"`
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
	Leading       *float64 `json:"leading,omitempty"`
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

type globalMarketResponse struct {
	Symbol        string                  `json:"symbol"`
	Region        string                  `json:"region"`
	Name          string                  `json:"name"`
	Current       string                  `json:"current"`
	PreviousClose string                  `json:"previous_close"`
	Open          string                  `json:"open"`
	High          string                  `json:"high"`
	Low           string                  `json:"low"`
	Delta         *float64                `json:"delta"`
	Percent       *float64                `json:"percent"`
	QuoteTime     string                  `json:"quote_time"`
	Source        string                  `json:"source"`
	Extended      *globalExtendedResponse `json:"extended,omitempty"`
}

type globalExtendedResponse struct {
	Session   string   `json:"session"`
	Symbol    string   `json:"symbol"`
	Name      string   `json:"name"`
	Price     string   `json:"price"`
	Delta     *float64 `json:"delta"`
	Percent   *float64 `json:"percent"`
	Volume    *float64 `json:"volume"`
	QuoteTime string   `json:"quote_time"`
	Source    string   `json:"source"`
}

type marketIndicesResponse struct {
	Items           []marketIndexResponse  `json:"items"`
	MarketAmount    *marketAmountResponse  `json:"market_amount,omitempty"`
	FetchedAt       string                 `json:"fetched_at"`
	Warning         string                 `json:"warning,omitempty"`
	AmountWarning   string                 `json:"amount_warning,omitempty"`
	GlobalMarkets   []globalMarketResponse `json:"global_markets,omitempty"`
	GlobalFetchedAt string                 `json:"global_fetched_at,omitempty"`
	GlobalWarning   string                 `json:"global_warning,omitempty"`
}

type globalMarketsResponse struct {
	Items     []globalMarketResponse `json:"items"`
	FetchedAt string                 `json:"fetched_at"`
	Warning   string                 `json:"warning,omitempty"`
}

// globalChartResponse intentionally carries both series when mode=all. This
// lets the browser switch between 分时 and 日 K without a second round trip.
type globalChartResponse struct {
	Symbol      string                `json:"symbol"`
	Region      string                `json:"region"`
	Name        string                `json:"name"`
	Timezone    string                `json:"timezone"`
	Interval    string                `json:"interval,omitempty"`
	Proxy       string                `json:"proxy,omitempty"`
	Approximate bool                  `json:"approximate,omitempty"`
	Market      *globalMarketResponse `json:"market,omitempty"`
	Bars        []chartBar            `json:"bars,omitempty"`
	Minutes     []minutePointResponse `json:"minutes,omitempty"`
	FetchedAt   string                `json:"fetched_at"`
	QuoteError  string                `json:"quote_error,omitempty"`
	DailyError  string                `json:"daily_error,omitempty"`
	MinuteError string                `json:"minute_error,omitempty"`
	Warning     string                `json:"warning,omitempty"`
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
	Leading   float64 `json:"leading,omitempty"`
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

// WithAIChatService connects the read-only Codex-backed research assistant.
// The Web package owns HTTP/job lifecycle while the embedding application owns
// fact collection, model invocation and conversation persistence.
func WithAIChatService(service AIChatService) ServerOption {
	return func(server *Server) {
		server.aiChatService = service
	}
}

// WithAIConfigService connects the local, redacted AI settings surface. The
// embedding application owns persistence, credential lookup and connection
// tests; Web never receives a token except in the inbound request body.
func WithAIConfigService(service AIConfigService) ServerOption {
	return func(server *Server) {
		server.aiConfigService = service
	}
}

func WithMarketAmount(client MarketAmountClient) ServerOption {
	return func(server *Server) {
		server.marketAmounts = client
	}
}

// WithIndustryFlows enables the market sentiment cockpit. The client is
// intentionally independent from the stock quote client so a premium quote
// source can be combined with the existing broad industry feed.
func WithIndustryFlows(client marketIndustryFlowClient) ServerOption {
	return func(server *Server) { server.industryFlows = client }
}

// WithSentimentSignals connects optional northbound-flow and hot-theme data.
func WithSentimentSignals(client marketSentimentSignalClient) ServerOption {
	return func(server *Server) { server.sentimentSignals = client }
}

// WithLimitStats connects market-wide limit-up/limit-down structure data.
func WithLimitStats(client market.LimitStatsClient) ServerOption {
	return func(server *Server) { server.limitStats = client }
}

// WithMarketRankings connects the exchange-wide gainers/losers/amount/turnover
// feeds used by the dashboard ranking matrix.
func WithMarketRankings(client market.MarketRankingClient) ServerOption {
	return func(server *Server) { server.rankings = client }
}

// WithSentimentHistoryStore persists the rolling cockpit chart history.
func WithSentimentHistoryStore(store sentimentHistoryStore) ServerOption {
	return func(server *Server) { server.sentimentHistoryStore = store }
}

// WithGlobalMarkets connects the compact Web market strip to the existing
// read-only overseas-index adapter. It is optional so lightweight embedders
// can keep serving domestic quotes without configuring an external source.
func WithGlobalMarkets(client GlobalIndexClient) ServerOption {
	return func(server *Server) {
		server.globalMarkets = client
	}
}

// WithGlobalCharts connects historical and intraday series for the dedicated
// overseas-market tab. It is optional so existing lightweight embedders keep
// the domestic quote surface unchanged.
func WithGlobalCharts(client GlobalChartClient) ServerOption {
	return func(server *Server) {
		server.globalCharts = client
	}
}

func WithBoardDetails(client BoardDetailClient) ServerOption {
	return func(server *Server) {
		server.boardDetails = client
	}
}

// WithRelatedData enables the stock-page context panel without coupling the
// quote adapter to news or board-directory APIs.
func WithRelatedData(boards market.BoardFlowClient, news market.StockNewsClient) ServerOption {
	return func(server *Server) {
		server.relatedBoards = boards
		server.stockNews = news
	}
}

func WithStrategyResearch(engine backtest.Engine, archive backtestArchive) ServerOption {
	return func(server *Server) {
		server.strategyEngine = engine
		server.strategyArchive = archive
	}
}

func WithStrategyCandidateLifecycle(engine backtest.Engine, archive continuousOptimizationArchive) ServerOption {
	return func(server *Server) {
		server.candidateEngine = engine
		server.candidateArchive = archive
	}
}

func WithRealtimeStrategy(scanner realtimeScanner, archive realtimeSignalArchive) ServerOption {
	return func(server *Server) {
		server.realtimeScanner = scanner
		server.realtimeArchive = archive
		if server.tradingCalendarProvider != nil {
			if configurable, ok := scanner.(interface {
				SetTradingCalendarProvider(realtime.TradingCalendarProvider)
			}); ok {
				configurable.SetTradingCalendarProvider(server.tradingCalendarProvider)
			}
		}
	}
}

func WithRealtimeOutcomes(analyzer realtimeOutcomeAnalyzer) ServerOption {
	return func(server *Server) {
		server.realtimeOutcomes = analyzer
	}
}

func WithShadowExecution(evaluator shadowAnalyzer, archive shadowArchive) ServerOption {
	return func(server *Server) {
		server.shadowEvaluator = evaluator
		server.shadowArchive = archive
		server.shadowProfiles = singleShadowExecutionProfile(archive)
	}
}

// WithShadowExecutionProfiles keeps the existing balanced ledger and adds two
// independent paper accounts. Each profile owns a separate archive and config,
// so switching the web view never rewrites another account's fills or T+1 lots.
func WithShadowExecutionProfiles(evaluator shadowAnalyzer, balanced, conservative, aggressive shadowArchive) ServerOption {
	return func(server *Server) {
		server.shadowEvaluator = evaluator
		server.shadowArchive = balanced
		server.shadowProfiles = defaultShadowExecutionProfiles(balanced, conservative, aggressive)
	}
}

// WithAdaptiveShadowExecution adds an isolated Challenger ledger. It is
// optional so embedders and existing tests can keep the original three
// accounts while the full web application enables gated calibration.
func WithAdaptiveShadowExecution(archive shadowArchive) ServerOption {
	return func(server *Server) {
		if archive == nil {
			return
		}
		if server.shadowProfiles == nil {
			server.shadowProfiles = make(map[string]shadowExecutionProfile)
		}
		server.shadowProfiles[shadowProfileAdaptive] = adaptiveShadowExecutionProfile(archive)
	}
}

// WithMonsterShadowExecution adds an isolated radar experiment ledger. The
// profile is optional so embedders can keep the original account set while the
// full Web application can collect forward evidence for the Monster model.
func WithMonsterShadowExecution(archive shadowArchive) ServerOption {
	return func(server *Server) {
		if archive == nil {
			return
		}
		if server.shadowProfiles == nil {
			server.shadowProfiles = make(map[string]shadowExecutionProfile)
		}
		server.shadowProfiles[shadowProfileMonster] = monsterShadowExecutionProfile(archive)
	}
}

func WithAutomaticStrategyResearch(runner func(context.Context, []string, time.Time) (AutomaticResearchResult, error)) ServerOption {
	return func(server *Server) {
		server.automationResearch = runner
	}
}

func WithAutomationState(store automationStateStore) ServerOption {
	return func(server *Server) {
		server.automationStateStore = store
	}
}

// WithRealtimeCalibrationStore persists the currently applied gated
// Challenger and its short audit history. It is optional for embedders that
// only need the scanner surface.
func WithRealtimeCalibrationStore(store realtimeCalibrationStateStore) ServerOption {
	return func(server *Server) {
		server.realtimeCalibrationStore = store
	}
}

// WithTradingCalendar allows an embedding application to provide an official
// point-in-time exchange calendar. The benchmark K-line calendar remains the
// default fallback when this option is not supplied.
func WithTradingCalendar(provider func(context.Context, time.Time) ([]string, error)) ServerOption {
	return func(server *Server) {
		server.tradingCalendarProvider = realtime.TradingCalendarProvider(provider)
		if configurable, ok := server.realtimeScanner.(interface {
			SetTradingCalendarProvider(realtime.TradingCalendarProvider)
		}); ok {
			configurable.SetTradingCalendarProvider(server.tradingCalendarProvider)
		}
	}
}

// NewServer uses options so embedders that only need the quote surface do not
// have to configure the shared CLI watchlist and name cache.
func NewServer(resolver SymbolResolver, quotes QuoteClient, history DailyHistoryClient, minutes MinuteClient, defaultSymbol string, options ...ServerOption) *Server {
	server := &Server{
		resolver:           resolver,
		quotes:             quotes,
		history:            history,
		minutes:            minutes,
		defaultSymbol:      strings.TrimSpace(defaultSymbol),
		quoteCache:         make(map[string]quoteCacheEntry),
		minuteCache:        make(map[string]minuteCacheEntry),
		historyCache:       make(map[string]historyCacheEntry),
		globalHistoryCache: make(map[string]historyCacheEntry),
		globalMinuteCache:  make(map[string]minuteCacheEntry),
		boardCache:         make(map[string]boardCacheEntry),
		assistantContexts:  make(map[string]assistantContextCacheEntry),
		assistantJobs:      make(map[string]*assistantChatJob),
		automationTasks:    make(map[string]storage.AutomationTaskState),
		now:                time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(server)
		}
	}
	if server.sentimentHistoryStore != nil {
		if points, err := server.sentimentHistoryStore.Load(); err == nil {
			server.sentimentHistory = normalizeSentimentHistory(points, server.now())
		}
	}
	server.restoreRealtimeCalibration()
	server.restoreAutomationState()
	server.initializeAutomationTaskStates()
	server.handler = server.routes()
	return server
}

func (s *Server) restoreRealtimeCalibration() {
	if s == nil || s.realtimeCalibrationStore == nil || s.realtimeScanner == nil {
		return
	}
	calibratable, ok := s.realtimeScanner.(realtimeCalibratableScanner)
	if !ok {
		return
	}
	state, err := s.realtimeCalibrationStore.Load()
	if err != nil || state.Active.ID == "" || state.Status == "challenger-rolled-back" {
		return
	}
	calibratable.SetCalibration(state.Active)
}

func (s *Server) restoreAutomationState() {
	if s == nil || s.automationStateStore == nil {
		return
	}
	state, err := s.automationStateStore.Load()
	if err != nil {
		return
	}
	s.automationMu.Lock()
	s.automationLastRun = state.LastRunAt
	s.automationLastSuccess = state.LastSuccessAt
	s.automationLastError = state.LastError
	s.automationNextRun = state.NextRunAt
	s.automationLastOutcome = state.LastOutcomeAt
	s.automationLastShadow = state.LastShadowAt
	s.automationResearchAttempt = state.ResearchAttemptAt
	s.automationResearchSuccess = state.ResearchSuccessAt
	s.automationResearchID = state.ResearchExperimentID
	s.automationResearchMessage = state.ResearchMessage
	s.automationResearchError = state.ResearchError
	s.automationTasks = cloneAutomationTasks(state.Tasks)
	// A process cannot still be executing work from a previous process. Do not
	// restore transient `running`/`busy` states as if they were live locks.
	for key, task := range s.automationTasks {
		if task.Status == "running" || task.Status == "busy" {
			task.Status = "waiting"
			task.Detail = "服务重启后等待下一次调度"
			task.LastError = ""
			s.automationTasks[key] = task
		}
	}
	s.automationMu.Unlock()
}

func (s *Server) persistAutomationState() {
	if s == nil || s.automationStateStore == nil {
		return
	}
	s.automationMu.Lock()
	state := storage.AutomationState{
		LastRunAt: s.automationLastRun, LastSuccessAt: s.automationLastSuccess,
		LastError: s.automationLastError, NextRunAt: s.automationNextRun,
		LastOutcomeAt: s.automationLastOutcome, LastShadowAt: s.automationLastShadow,
		ResearchAttemptAt: s.automationResearchAttempt, ResearchSuccessAt: s.automationResearchSuccess,
		ResearchExperimentID: s.automationResearchID, ResearchMessage: s.automationResearchMessage,
		ResearchError: s.automationResearchError,
		Tasks:         cloneAutomationTasks(s.automationTasks),
	}
	s.automationMu.Unlock()
	// A failed metadata write must not stop market scanning; the next cycle will
	// retry and the in-memory status remains authoritative for this process.
	_ = s.automationStateStore.Save(state)
}

func cloneAutomationTasks(input map[string]storage.AutomationTaskState) map[string]storage.AutomationTaskState {
	if len(input) == 0 {
		return make(map[string]storage.AutomationTaskState)
	}
	result := make(map[string]storage.AutomationTaskState, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func (s *Server) initializeAutomationTaskStates() {
	if s == nil {
		return
	}
	s.automationMu.Lock()
	defer s.automationMu.Unlock()
	if s.automationTasks == nil {
		s.automationTasks = make(map[string]storage.AutomationTaskState)
	}
	defaults := map[string]storage.AutomationTaskState{
		automationTaskScan:     {Status: "waiting", Detail: "等待服务端调度"},
		automationTaskOutcomes: {Status: "waiting", Detail: "等待收盘后信号验证"},
		automationTaskShadow:   {Status: "waiting", Detail: "等待当前交易日快照"},
		automationTaskResearch: {Status: "waiting", Detail: "等待收盘后的非重叠窗口"},
	}
	if strings.TrimSpace(s.watchlistFile) == "" || s.realtimeScanner == nil || s.realtimeArchive == nil {
		defaults[automationTaskScan] = storage.AutomationTaskState{Status: "paused", Detail: "实时扫描服务未配置"}
	}
	if s.realtimeOutcomes == nil {
		defaults[automationTaskOutcomes] = storage.AutomationTaskState{Status: "paused", Detail: "信号验证服务未配置"}
	}
	if s.shadowEvaluator == nil || len(s.shadowProfiles) == 0 {
		defaults[automationTaskShadow] = storage.AutomationTaskState{Status: "paused", Detail: "影子账户服务未配置"}
	}
	if s.automationResearch == nil {
		defaults[automationTaskResearch] = storage.AutomationTaskState{Status: "paused", Detail: "自动研究服务未配置"}
	}
	for _, definition := range automationTaskDefinitions {
		if _, exists := s.automationTasks[definition.Key]; !exists {
			s.automationTasks[definition.Key] = defaults[definition.Key]
		}
	}
}

func (s *Server) setAutomationTask(key, status, detail string, attemptedAt time.Time, err error) {
	if s == nil || strings.TrimSpace(key) == "" {
		return
	}
	s.automationMu.Lock()
	if s.automationTasks == nil {
		s.automationTasks = make(map[string]storage.AutomationTaskState)
	}
	task := s.automationTasks[key]
	task.Status = status
	task.Detail = strings.TrimSpace(detail)
	if !attemptedAt.IsZero() {
		task.LastAttemptAt = attemptedAt
	}
	if err != nil {
		task.LastError = err.Error()
	} else if status == "success" {
		task.LastError = ""
		task.LastSuccessAt = s.currentTime()
	} else if status == "waiting" || status == "paused" || status == "busy" || status == "running" {
		// A current non-error state supersedes an older failure while preserving
		// the last successful timestamp for operators.
		task.LastError = ""
	}
	s.automationTasks[key] = task
	s.automationMu.Unlock()
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/indices", s.handleIndices)
	mux.HandleFunc("/api/sentiment", s.handleSentiment)
	mux.HandleFunc("/api/boards", s.handleBoards)
	mux.HandleFunc("/api/rankings", s.handleRankings)
	mux.HandleFunc("/api/global/markets", s.handleGlobalMarkets)
	mux.HandleFunc("/api/global/chart", s.handleGlobalChart)
	mux.HandleFunc("/api/stock", s.handleStock)
	mux.HandleFunc("/api/watchlist", s.handleWatchlist)
	mux.HandleFunc("/api/strategy/backtests", s.handleStrategyBacktests)
	mux.HandleFunc("/api/strategy/candidates", s.handleStrategyCandidates)
	mux.HandleFunc("/api/strategy/realtime", s.handleRealtimeStrategy)
	mux.HandleFunc("/api/strategy/shadow", s.handleShadowExecution)
	mux.HandleFunc("/api/strategy/automation", s.handleAutomationStatus)
	mux.HandleFunc("/api/assistant/context", s.handleAssistantContext)
	mux.HandleFunc("/api/assistant/alerts", s.handleAssistantAlerts)
	mux.HandleFunc("/api/assistant/chat", s.handleAssistantChat)
	mux.HandleFunc("/api/ai/config", s.handleAIConfig)
	mux.HandleFunc("/api/ai/config/test", s.handleAIConfigTest)
	mux.HandleFunc("/api/ai/config/reset", s.handleAIConfigReset)
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
	if ctx == nil {
		ctx = context.Background()
	}
	defer s.cancelAssistantJobs()
	server := &http.Server{Addr: address, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	automationCtx, cancelAutomation := context.WithCancel(ctx)
	defer cancelAutomation()
	s.automationMu.Lock()
	s.automationCtx = automationCtx
	s.automationMu.Unlock()
	defer func() {
		s.automationMu.Lock()
		if s.automationCtx == automationCtx {
			s.automationCtx = nil
		}
		s.automationMu.Unlock()
	}()
	go s.runAutomationLoop(automationCtx)
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
		cancelAutomation()
		return err
	case <-ctx.Done():
		cancelAutomation()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	}
}

func (s *Server) automationRunContext() context.Context {
	if s == nil {
		return context.Background()
	}
	s.automationMu.Lock()
	ctx := s.automationCtx
	s.automationMu.Unlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (s *Server) automationStatus() AutomationStatus {
	s.automationMu.Lock()
	defer s.automationMu.Unlock()
	shadowTask := "影子账户推进"
	if count := len(s.shadowProfiles); count > 0 {
		shadowTask = fmt.Sprintf("%d个影子账户推进", count)
	}
	return AutomationStatus{
		Enabled: strings.TrimSpace(s.watchlistFile) != "" && s.realtimeScanner != nil && s.realtimeArchive != nil,
		Running: s.automationRunning, LastRunAt: s.automationLastRun,
		LastSuccessAt: s.automationLastSuccess, LastError: s.automationLastError,
		NextRunAt: s.automationNextRun, LastOutcomeAt: s.automationLastOutcome, LastShadowAt: s.automationLastShadow,
		Tasks:           []string{"交易时段实时扫描", "收盘后信号验证", shadowTask, "非重叠窗口滚动研究"},
		TaskOrder:       []string{automationTaskScan, automationTaskOutcomes, automationTaskShadow, automationTaskResearch},
		ResearchRunning: s.automationResearchRunning, ResearchAttemptAt: s.automationResearchAttempt,
		ResearchSuccessAt: s.automationResearchSuccess, ResearchExperimentID: s.automationResearchID,
		ResearchMessage: s.automationResearchMessage, ResearchError: s.automationResearchError,
		TaskStates: cloneAutomationTasks(s.automationTasks),
	}
}

func (s *Server) handleAutomationStatus(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		writeJSON(writer, http.StatusOK, s.automationStatus())
	case http.MethodPost:
		action := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("action")))
		if action != "" && action != "run" {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "不支持的自动化操作"})
			return
		}
		if !s.automationStatus().Enabled {
			writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "自动化任务未初始化"})
			return
		}
		if s.beginAutomationCycle() {
			go s.runAutomationCycleStarted(s.automationRunContext())
		}
		writeJSON(writer, http.StatusAccepted, s.automationStatus())
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "自动化状态只支持 GET、POST"})
	}
}

func (s *Server) runAutomationLoop(ctx context.Context) {
	if s == nil || s.realtimeScanner == nil || s.realtimeArchive == nil || strings.TrimSpace(s.watchlistFile) == "" {
		return
	}
	ticker := time.NewTicker(automationCycleInterval)
	defer ticker.Stop()
	next := s.currentTime().Add(automationCycleInterval)
	s.automationMu.Lock()
	s.automationNextRun = next
	s.automationMu.Unlock()
	s.setAutomationTaskNextRun(automationTaskScan, next)
	s.persistAutomationState()
	s.runAutomationCycle(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.automationMu.Lock()
			s.automationNextRun = now.Add(automationCycleInterval)
			s.automationMu.Unlock()
			s.setAutomationTaskNextRun(automationTaskScan, now.Add(automationCycleInterval))
			s.persistAutomationState()
			s.runAutomationCycle(ctx)
		}
	}
}

func (s *Server) setAutomationTaskNextRun(key string, next time.Time) {
	if s == nil {
		return
	}
	s.automationMu.Lock()
	if s.automationTasks == nil {
		s.automationTasks = make(map[string]storage.AutomationTaskState)
	}
	task := s.automationTasks[key]
	task.NextRunAt = next
	s.automationTasks[key] = task
	s.automationMu.Unlock()
}

func (s *Server) beginAutomationCycle() bool {
	if s == nil {
		return false
	}
	s.automationMu.Lock()
	defer s.automationMu.Unlock()
	if s.automationRunning {
		return false
	}
	s.automationRunning = true
	s.automationLastRun = s.currentTime()
	return true
}

func (s *Server) runAutomationCycle(ctx context.Context) {
	if !s.beginAutomationCycle() {
		return
	}
	s.runAutomationCycleStarted(ctx)
}

func automaticOutcomeValidationPlan(now time.Time, session realtime.MarketSession, lastSuccess time.Time, task storage.AutomationTaskState) automaticOutcomePlan {
	localNow := now.In(realtimeWebLocation)
	day := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, realtimeWebLocation)
	primary := day.Add(outcomePrimaryHour*time.Hour + outcomePrimaryMinute*time.Minute)
	retry := day.Add(outcomeRetryHour*time.Hour + outcomeRetryMinute*time.Minute)
	nextPrimary := nextAutomaticOutcomeValidation(session, localNow)
	if !session.TradingDay {
		return automaticOutcomePlan{NextRunAt: nextPrimary, Detail: "等待下个交易日收盘验证"}
	}
	if !lastSuccess.IsZero() && !lastSuccess.In(realtimeWebLocation).Before(primary) {
		return automaticOutcomePlan{NextRunAt: nextPrimary, Detail: "本交易日信号验证已完成"}
	}
	if localNow.Before(primary) {
		return automaticOutcomePlan{NextRunAt: primary, Detail: "等待收盘后 15:30 验证"}
	}
	lastAttempt := task.LastAttemptAt.In(realtimeWebLocation)
	if localNow.Before(retry) {
		if task.LastAttemptAt.IsZero() || lastAttempt.Before(primary) {
			return automaticOutcomePlan{Run: true, NextRunAt: retry}
		}
		return automaticOutcomePlan{NextRunAt: retry, Detail: "等待 16:05 日K就绪重试"}
	}
	if task.LastAttemptAt.IsZero() || lastAttempt.Before(retry) {
		return automaticOutcomePlan{Run: true, RetryAttempt: true, NextRunAt: nextPrimary}
	}
	return automaticOutcomePlan{NextRunAt: nextPrimary, Detail: "今日验证窗口已结束，等待下个交易日"}
}

func nextAutomaticOutcomeValidation(session realtime.MarketSession, now time.Time) time.Time {
	if session.TradingDay {
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, realtimeWebLocation)
		primary := day.Add(outcomePrimaryHour*time.Hour + outcomePrimaryMinute*time.Minute)
		if now.Before(primary) {
			return primary
		}
	}
	next := session.NextScanAt.In(realtimeWebLocation)
	if next.IsZero() {
		next = now.AddDate(0, 0, 1)
		for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
			next = next.AddDate(0, 0, 1)
		}
	}
	return time.Date(next.Year(), next.Month(), next.Day(), outcomePrimaryHour, outcomePrimaryMinute, 0, 0, realtimeWebLocation)
}

func outcomeReportCoversTradingDate(report realtime.OutcomeReport, tradingDate string) bool {
	return len(tradingDate) == len("2006-01-02") && report.DataThrough >= tradingDate
}

func (s *Server) runAutomationCycleStarted(ctx context.Context) {
	defer func() {
		s.automationMu.Lock()
		s.automationRunning = false
		s.automationMu.Unlock()
		s.persistAutomationState()
	}()

	cycleCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	now := s.currentTime()
	session := s.marketSession(cycleCtx, now)
	cycleErrors := make([]error, 0, 4)
	appendCycleError := func(label string, err error) {
		if err == nil {
			return
		}
		cycleErrors = append(cycleErrors, fmt.Errorf("%s: %w", label, err))
	}
	scanBusy := false
	scanFailed := false
	scanSucceeded := false
	shadowFailed := false
	shadowPreserved := false
	shadowPreserveReason := ""
	if !session.ScanAllowed && !session.ShouldFinalize(time.Time{}) {
		s.setAutomationTask(automationTaskScan, "paused", "当前不在可扫描交易时段", now, nil)
	} else {
		s.setAutomationTask(automationTaskScan, "running", "正在获取实时行情与因子", now, nil)
	}
	latestSnapshot, latestSnapshotError := s.latestRealtimeSnapshot()
	if session.ScanAllowed || session.ShouldFinalize(latestSnapshot.GeneratedAt) {
		groups, _, err := storage.LoadWatchlistGroups(s.watchlistFile)
		if err != nil {
			scanFailed = true
			appendCycleError("读取自选失败", err)
			s.setAutomationTask(automationTaskScan, "error", "读取自选失败", now, err)
		} else {
			symbols := storage.WatchlistSymbols(groups, storage.AllWatchlistGroup)
			s.realtimeMu.Lock()
			shouldScan := !s.realtimeRunning
			scanBusy = !shouldScan
			if shouldScan {
				s.realtimeRunning = true
			}
			s.realtimeMu.Unlock()
			if shouldScan {
				result, err := s.realtimeScanner.Scan(cycleCtx, symbols, true)
				s.realtimeMu.Lock()
				s.realtimeRunning = false
				if err != nil {
					s.realtimeMu.Unlock()
					scanFailed = true
					appendCycleError("实时扫描失败", err)
					s.setAutomationTask(automationTaskScan, "error", "实时扫描失败", now, err)
				} else {
					scanSucceeded = true
					s.realtimeCache = result
					latestSnapshot = result
					s.realtimeMu.Unlock()
					s.setAutomationTask(automationTaskScan, "success", fmt.Sprintf("已生成%d个信号", len(result.Signals)), now, nil)
				}
			}
		}
	}
	if latestSnapshotError != nil && !scanSucceeded && !scanBusy {
		appendCycleError("读取最近实时快照失败", latestSnapshotError)
	}
	if scanBusy {
		// A manual scan owns the scanner lock, but it should not block the
		// independent validation and shadow jobs. Signal validation follows its
		// own after-close schedule, while shadow advancement can continue from a
		// current cached snapshot. Keep the scanner visibly busy so operators know
		// the fresh result is still pending.
		s.setAutomationTask(automationTaskScan, "busy", "已有手动扫描正在运行，使用最近快照继续影子同步", now, nil)
	}

	s.automationMu.Lock()
	lastOutcome := s.automationLastOutcome
	outcomeTask := s.automationTasks[automationTaskOutcomes]
	s.automationMu.Unlock()
	outcomePlan := automaticOutcomeValidationPlan(now, session, lastOutcome, outcomeTask)
	s.setAutomationTaskNextRun(automationTaskOutcomes, outcomePlan.NextRunAt)
	if s.realtimeOutcomes == nil {
		s.setAutomationTask(automationTaskOutcomes, "paused", "信号验证服务未配置", now, nil)
	} else if !outcomePlan.Run {
		s.setAutomationTask(automationTaskOutcomes, "waiting", outcomePlan.Detail, now, nil)
	} else {
		s.setAutomationTask(automationTaskOutcomes, "running", "正在更新信号验证结果", now, nil)
		// Read the complete append-only signal archive. Passing a negative limit
		// explicitly requests full-archive mode; truncating this to the newest
		// 2000 rows leaves older 5/10-day horizons pending forever when the
		// scanner emits a large universe every 30 seconds.
		if signals, err := s.realtimeArchive.List(-1); err != nil {
			appendCycleError("读取信号验证失败", err)
			s.setAutomationTask(automationTaskOutcomes, "error", "读取信号验证失败", now, err)
		} else if len(signals) == 0 {
			s.automationMu.Lock()
			s.automationLastOutcome = now
			s.automationMu.Unlock()
			next := nextAutomaticOutcomeValidation(session, now.In(realtimeWebLocation))
			s.setAutomationTaskNextRun(automationTaskOutcomes, next)
			s.setAutomationTask(automationTaskOutcomes, "success", "当前没有待验证信号", now, nil)
		} else if report, err := s.realtimeOutcomes.Evaluate(cycleCtx, signals, realtime.OutcomeOptions{SignalLimit: -1}); err != nil {
			appendCycleError("更新信号验证失败", err)
			s.setAutomationTask(automationTaskOutcomes, "error", "更新信号验证失败", now, err)
		} else if outcomeReportCoversTradingDate(report, session.TradingDate) {
			s.applyRealtimeCalibration(report)
			s.automationMu.Lock()
			s.automationLastOutcome = now
			s.automationMu.Unlock()
			next := nextAutomaticOutcomeValidation(session, now.In(realtimeWebLocation))
			s.setAutomationTaskNextRun(automationTaskOutcomes, next)
			s.setAutomationTask(automationTaskOutcomes, "success", fmt.Sprintf("已验证%d个信号，数据截至%s", len(signals), report.DataThrough), now, nil)
		} else {
			dataThrough := report.DataThrough
			if dataThrough == "" {
				dataThrough = "尚未返回有效交易日"
			}
			if outcomePlan.RetryAttempt {
				s.setAutomationTask(automationTaskOutcomes, "waiting", fmt.Sprintf("日K仅更新至%s，本日验证窗口已结束", dataThrough), now, nil)
			} else {
				s.setAutomationTask(automationTaskOutcomes, "waiting", fmt.Sprintf("日K仅更新至%s，16:05重试", dataThrough), now, nil)
			}
		}
	}

	currentShadowSnapshot := s.hasCurrentSessionSnapshot(latestSnapshot, session)
	if !currentShadowSnapshot {
		s.setAutomationTaskNextRun(automationTaskShadow, now.Add(automationCycleInterval))
		s.setAutomationTask(automationTaskShadow, "paused", "当前快照不是本交易日有效快照", now, nil)
	} else if len(s.shadowProfiles) > 0 && s.shadowEvaluator != nil {
		s.shadowMu.Lock()
		defer s.shadowMu.Unlock()
		var signals []realtime.Signal
		loaded := false
		loadSignals := func(limit int) ([]realtime.Signal, error) {
			if loaded {
				return signals, nil
			}
			items, err := s.realtimeArchive.List(limit)
			if err != nil {
				return nil, err
			}
			signals, loaded = items, true
			return signals, nil
		}
		allCached := true
		for _, id := range s.orderedShadowProfileIDs() {
			profile := s.shadowProfiles[id]
			options := paper.Options{Config: profile.Config, Limit: 0, Realtime: session.State == realtime.MarketStateTrading || session.State == realtime.MarketStateAuction, RealtimeAt: now}
			if dates, calendarErr := s.tradingCalendar(cycleCtx, now); calendarErr == nil {
				options.CalendarDates = dates
			}
			if options.Realtime {
				options.RealtimeQuotes = s.shadowRealtimeQuotes(cycleCtx, latestSnapshot)
			}
			cached, preserved, preserveReason, err := s.syncShadowProfileFromSnapshot(cycleCtx, nil, profile, options, loadSignals, latestSnapshot.Signals)
			if err != nil {
				shadowFailed = true
				appendCycleError(profile.Name+"影子账户同步失败", err)
				s.setAutomationTask(automationTaskShadow, "error", profile.Name+"影子账户同步失败", now, err)
				break
			}
			allCached = allCached && cached
			if preserved {
				shadowPreserved = true
				if shadowPreserveReason == "" {
					shadowPreserveReason = profile.Name + "：" + preserveReason
				}
			}
		}
		if !shadowFailed {
			s.automationMu.Lock()
			if currentShadowSnapshot && !latestSnapshot.GeneratedAt.IsZero() {
				s.automationLastShadow = latestSnapshot.GeneratedAt
			} else {
				s.automationLastShadow = now
			}
			s.automationMu.Unlock()
			s.setAutomationTaskNextRun(automationTaskShadow, now.Add(automationCycleInterval))
			if shadowPreserved {
				s.setAutomationTask(automationTaskShadow, "warning", shadowPreserveReason, now, nil)
			} else if allCached {
				s.setAutomationTask(automationTaskShadow, "waiting", "已检查最新快照，暂无新增报价水位", now, nil)
			} else {
				shadowCount := len(s.shadowProfiles)
				s.setAutomationTask(automationTaskShadow, "success", fmt.Sprintf("%d个影子账户已按最新快照推进", shadowCount), now, nil)
			}
		}
	} else {
		s.setAutomationTaskNextRun(automationTaskShadow, now.Add(automationCycleInterval))
		s.setAutomationTask(automationTaskShadow, "paused", "影子账户服务未配置", now, nil)
	}

	s.automationMu.Lock()
	if len(cycleErrors) > 0 {
		messages := make([]string, 0, len(cycleErrors))
		for _, err := range cycleErrors {
			messages = append(messages, err.Error())
		}
		s.automationLastError = strings.Join(messages, "; ")
	} else {
		s.automationLastSuccess = s.currentTime()
		s.automationLastError = ""
	}
	s.automationMu.Unlock()
	s.persistAutomationState()
	// A cached snapshot is sufficient for shadow work, while signal validation
	// follows its own after-close schedule. Automatic
	// research requires a clean scan cycle so a transient quote failure cannot
	// accidentally open a new research window.
	if len(cycleErrors) == 0 && !scanFailed && !scanBusy {
		s.maybeStartAutomaticResearch(ctx, now, session, latestSnapshot)
	}
}

func (s *Server) hasCurrentSessionSnapshot(result realtime.ScanResult, session realtime.MarketSession) bool {
	if !session.TradingDay {
		return false
	}
	if result.GeneratedAt.IsZero() {
		return false
	}
	local := result.GeneratedAt.In(realtimeWebLocation)
	current := session.TradingDate
	if current == "" || local.Format("2006-01-02") != current {
		return false
	}
	quoteDates := make(map[string]bool)
	for _, signal := range result.Signals {
		if len(signal.QuoteTime) >= len("2006-01-02") {
			quoteDates[strings.TrimSpace(signal.QuoteTime[:len("2006-01-02")])] = true
		}
	}
	if len(quoteDates) > 0 && !quoteDates[current] {
		return false
	}
	// Requiring a same-day completed snapshot prevents weekend/holiday cycles
	// from repeatedly revaluing accounts or re-running outcome analysis.
	return true
}

func (s *Server) maybeStartAutomaticResearch(ctx context.Context, now time.Time, session realtime.MarketSession, latestSnapshot realtime.ScanResult) {
	localNow := now.In(realtimeWebLocation)
	if s.automationResearch == nil || session.State != realtime.MarketStateClosed || localNow.Hour() < 16 || !s.hasCurrentSessionSnapshot(latestSnapshot, session) {
		if s.automationResearch != nil {
			s.setAutomationTask(automationTaskResearch, "waiting", "等待收盘后的有效交易日快照", now, nil)
		}
		return
	}
	s.automationMu.Lock()
	sameDayAttempt := !s.automationResearchAttempt.IsZero() && s.automationResearchAttempt.In(realtimeWebLocation).Format("2006-01-02") == localNow.Format("2006-01-02")
	retryAfterFailure := sameDayAttempt && strings.TrimSpace(s.automationResearchError) != ""
	if s.automationResearchRunning || (sameDayAttempt && !retryAfterFailure) {
		s.automationMu.Unlock()
		s.setAutomationTask(automationTaskResearch, "waiting", "本交易日已尝试或已有研究任务运行", now, nil)
		return
	}
	if retryAfterFailure && localNow.Sub(s.automationResearchAttempt.In(realtimeWebLocation)) < automaticResearchRetry {
		next := s.automationResearchAttempt.Add(automaticResearchRetry)
		s.automationMu.Unlock()
		s.setAutomationTaskNextRun(automationTaskResearch, next)
		s.setAutomationTask(automationTaskResearch, "waiting", "上一轮研究失败，等待 10 分钟后自动重试", now, nil)
		return
	}
	s.automationResearchRunning = true
	s.automationResearchAttempt = now
	s.automationResearchError = ""
	s.automationMu.Unlock()
	s.setAutomationTask(automationTaskResearch, "running", "准备非重叠窗口滚动研究", now, nil)

	groups, _, err := storage.LoadWatchlistGroups(s.watchlistFile)
	if err != nil {
		s.finishAutomaticResearch(AutomaticResearchResult{}, err, now)
		return
	}
	all := storage.WatchlistSymbols(groups, storage.AllWatchlistGroup)
	symbols := make([]string, 0, min(20, len(all)))
	for _, symbol := range all {
		if market.AssetKindOf(symbol) != domain.AssetKindStock {
			continue
		}
		symbols = append(symbols, symbol)
		if len(symbols) >= 20 {
			break
		}
	}
	if len(symbols) == 0 {
		s.finishAutomaticResearch(AutomaticResearchResult{}, fmt.Errorf("自动滚动研究股票池为空"), now)
		return
	}
	end := automaticResearchCutoff(latestSnapshot, now)
	// Keep the launch timestamp as the completion metadata for the asynchronous
	// callback. Besides making the audit record deterministic, this avoids
	// reading an embedding application's mutable clock function from a worker
	// goroutine after the request that started the work has returned.
	attemptedAt := now
	go func() {
		researchCtx, cancel := context.WithTimeout(ctx, 45*time.Minute)
		defer cancel()
		result, runErr := s.automationResearch(researchCtx, symbols, end)
		s.finishAutomaticResearch(result, runErr, attemptedAt)
	}()
}

func automaticResearchCutoff(snapshot realtime.ScanResult, now time.Time) time.Time {
	localNow := now.In(realtimeWebLocation)
	latest := time.Time{}
	for _, signal := range snapshot.Signals {
		value := strings.TrimSpace(signal.DataDate)
		if len(value) != len("2006-01-02") {
			continue
		}
		date, err := time.ParseInLocation("2006-01-02", value, realtimeWebLocation)
		if err != nil || date.After(localNow) {
			continue
		}
		if latest.IsZero() || date.After(latest) {
			latest = date
		}
	}
	if !latest.IsZero() {
		return latest
	}
	day := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, realtimeWebLocation).AddDate(0, 0, -1)
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

func (s *Server) finishAutomaticResearch(result AutomaticResearchResult, err error, completedAt time.Time) {
	if completedAt.IsZero() {
		completedAt = time.Now()
	}
	s.automationMu.Lock()
	s.automationResearchRunning = false
	if err != nil {
		s.automationResearchError = err.Error()
		s.automationMu.Unlock()
		s.setAutomationTask(automationTaskResearch, "error", "自动滚动研究失败", completedAt, err)
		s.setAutomationTaskNextRun(automationTaskResearch, completedAt.Add(automaticResearchRetry))
		s.persistAutomationState()
		return
	}
	s.automationResearchError = ""
	s.automationResearchMessage = result.Message
	if result.Ran {
		s.automationResearchSuccess = completedAt
		s.automationResearchID = result.ExperimentID
	}
	s.automationMu.Unlock()
	if result.Ran {
		s.setAutomationTask(automationTaskResearch, "success", result.Message, completedAt, nil)
	} else {
		s.setAutomationTask(automationTaskResearch, "waiting", result.Message, completedAt, nil)
	}
	s.persistAutomationState()
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
	if s.quotes == nil && s.globalMarkets == nil {
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
	quotes := make(map[string]domain.Quote)
	var fetchError error
	if s.quotes == nil {
		fetchError = fmt.Errorf("A股指数行情服务未初始化")
	} else {
		quotes, fetchError = s.fetchQuoteBatch(ctx, allSymbols)
	}
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
	globalItems, globalFetchedAt, globalError := s.fetchGlobalIndices(ctx)
	globalAvailable := 0
	if s.globalMarkets != nil || len(globalItems) > 0 {
		response.GlobalMarkets = make([]globalMarketResponse, 0, len(globalMarketDefinitions))
		for _, definition := range globalMarketDefinitions {
			item := domain.GlobalIndex{Symbol: definition.Symbol, Region: definition.Region, Name: definition.Name, Current: math.NaN(), Delta: math.NaN(), Percent: math.NaN()}
			for _, candidate := range globalItems {
				if candidate.Symbol == definition.Symbol {
					item = candidate
					break
				}
			}
			if !math.IsNaN(item.Current) && !math.IsInf(item.Current, 0) {
				globalAvailable++
			}
			response.GlobalMarkets = append(response.GlobalMarkets, newGlobalMarketResponse(item, definition))
		}
		if !globalFetchedAt.IsZero() {
			response.GlobalFetchedAt = globalFetchedAt.Format(time.RFC3339)
		}
		if globalError != nil {
			response.GlobalWarning = globalError.Error()
		} else if globalAvailable < len(globalMarketDefinitions) {
			response.GlobalWarning = fmt.Sprintf("仅返回 %d/%d 个外盘市场行情", globalAvailable, len(globalMarketDefinitions))
		}
	}
	if available == 0 && globalAvailable == 0 {
		if response.Warning == "" {
			response.Warning = "未返回有效指数行情"
		}
		writeJSON(writer, http.StatusBadGateway, response)
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

func globalMarketDefinitionFor(symbol string) (globalMarketDefinition, bool) {
	normalized := strings.ToLower(strings.TrimSpace(symbol))
	for _, definition := range globalMarketDefinitions {
		if strings.ToLower(definition.Symbol) == normalized {
			return definition, true
		}
	}
	return globalMarketDefinition{}, false
}

func (s *Server) handleGlobalMarkets(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "外盘市场只支持 GET"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
	defer cancel()
	items, fetchedAt, fetchError := s.fetchGlobalIndices(ctx)
	received := make(map[string]domain.GlobalIndex, len(items))
	for _, item := range items {
		received[strings.ToLower(item.Symbol)] = item
	}
	response := globalMarketsResponse{Items: make([]globalMarketResponse, 0, len(globalMarketDefinitions))}
	for _, definition := range globalMarketDefinitions {
		item := domain.GlobalIndex{Symbol: definition.Symbol, Region: definition.Region, Name: definition.Name, Current: math.NaN(), Delta: math.NaN(), Percent: math.NaN(), Open: math.NaN(), PreviousClose: math.NaN(), High: math.NaN(), Low: math.NaN()}
		if candidate, ok := received[strings.ToLower(definition.Symbol)]; ok {
			item = candidate
		}
		response.Items = append(response.Items, newGlobalMarketResponse(item, definition))
	}
	if !fetchedAt.IsZero() {
		response.FetchedAt = fetchedAt.Format(time.RFC3339)
	} else {
		response.FetchedAt = time.Now().Format(time.RFC3339)
	}
	if fetchError != nil {
		response.Warning = fetchError.Error()
	}
	available := 0
	for _, item := range response.Items {
		if item.Current != "--" {
			available++
		}
	}
	if available == 0 {
		if response.Warning == "" {
			response.Warning = "未返回有效外盘市场行情"
		}
		writeJSON(writer, http.StatusBadGateway, response)
		return
	}
	writeJSON(writer, http.StatusOK, response)
}

type globalChartFetchResult struct {
	bars   []domain.DailyBar
	points []domain.MinutePoint
	err    error
}

func (s *Server) handleGlobalChart(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "外盘图表只支持 GET"})
		return
	}
	symbol := strings.TrimSpace(request.URL.Query().Get("symbol"))
	definition, ok := globalMarketDefinitionFor(symbol)
	if !ok {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "不支持的外盘市场代码"})
		return
	}
	mode := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("mode")))
	if mode == "" {
		mode = "all"
	}
	if mode == "kline" {
		mode = "daily"
	}
	if mode != "all" && mode != "daily" && mode != "intraday" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "外盘图表 mode 只支持 all、daily 或 intraday"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 18*time.Second)
	defer cancel()
	response := globalChartResponse{
		Symbol: definition.Symbol, Region: definition.Region, Name: definition.Name,
		Timezone: timezoneLabel(definition.Symbol), FetchedAt: time.Now().Format(time.RFC3339),
		Interval: "5分钟",
	}

	marketChannel := make(chan struct {
		items []domain.GlobalIndex
		at    time.Time
		err   error
	}, 1)
	go func() {
		items, at, err := s.fetchGlobalIndices(ctx)
		marketChannel <- struct {
			items []domain.GlobalIndex
			at    time.Time
			err   error
		}{items: items, at: at, err: err}
	}()

	var dailyChannel, minuteChannel chan globalChartFetchResult
	if mode == "all" || mode == "daily" {
		dailyChannel = make(chan globalChartFetchResult, 1)
		go func() {
			bars, err := s.fetchGlobalDailyBars(ctx, symbol)
			dailyChannel <- globalChartFetchResult{bars: limitBars(bars, request.URL.Query().Get("limit")), err: err}
		}()
	}
	if mode == "all" || mode == "intraday" {
		minuteChannel = make(chan globalChartFetchResult, 1)
		go func() {
			points, err := s.fetchGlobalMinutePoints(ctx, symbol)
			minuteChannel <- globalChartFetchResult{points: points, err: err}
		}()
	}

	marketResult := <-marketChannel
	for _, item := range marketResult.items {
		if strings.EqualFold(item.Symbol, symbol) {
			converted := newGlobalMarketResponse(item, definition)
			response.Market = &converted
			break
		}
	}
	if marketResult.err != nil {
		response.QuoteError = marketResult.err.Error()
	}
	if !marketResult.at.IsZero() {
		response.FetchedAt = marketResult.at.Format(time.RFC3339)
	}
	if dailyChannel != nil {
		result := <-dailyChannel
		if len(result.bars) > 0 {
			response.Bars = newChartBars(result.bars)
		}
		if result.err != nil {
			response.DailyError = result.err.Error()
		}
	}
	if minuteChannel != nil {
		result := <-minuteChannel
		if len(result.points) > 0 {
			response.Minutes = newMinutePoints(result.points)
		}
		if result.err != nil {
			response.MinuteError = result.err.Error()
		}
	}
	normalizeGlobalProxySeries(&response)
	if response.QuoteError != "" || response.DailyError != "" || response.MinuteError != "" {
		warnings := make([]string, 0, 3)
		if response.QuoteError != "" {
			warnings = append(warnings, response.QuoteError)
		}
		if response.DailyError != "" {
			warnings = append(warnings, response.DailyError)
		}
		if response.MinuteError != "" {
			warnings = append(warnings, response.MinuteError)
		}
		response.Warning = strings.Join(warnings, "；")
	}
	status := http.StatusOK
	if response.Market == nil && len(response.Bars) == 0 && len(response.Minutes) == 0 {
		status = http.StatusBadGateway
	}
	writeJSON(writer, status, response)
}

func normalizeGlobalProxySeries(response *globalChartResponse) {
	if response == nil {
		return
	}
	proxy := ""
	for _, bar := range response.Bars {
		if strings.Contains(bar.Source, "代理") {
			proxy = bar.Source
			break
		}
	}
	if proxy == "" {
		for _, point := range response.Minutes {
			if strings.Contains(point.Source, "代理") {
				proxy = point.Source
				break
			}
		}
	}
	if proxy == "" {
		return
	}
	response.Proxy = proxy
	response.Approximate = true
	if response.Market == nil {
		return
	}
	target, err := strconv.ParseFloat(response.Market.Current, 64)
	if err != nil || !finiteNumber(target) || target <= 0 {
		return
	}
	if len(response.Bars) > 0 {
		latest := response.Bars[len(response.Bars)-1].Close
		if finiteNumber(latest) && latest > 0 {
			scale := target / latest
			for index := range response.Bars {
				response.Bars[index].Open *= scale
				response.Bars[index].Close *= scale
				response.Bars[index].High *= scale
				response.Bars[index].Low *= scale
			}
		}
	}
	if len(response.Minutes) > 0 {
		latest := response.Minutes[len(response.Minutes)-1].Price
		if finiteNumber(latest) && latest > 0 {
			scale := target / latest
			for index := range response.Minutes {
				response.Minutes[index].Price *= scale
				response.Minutes[index].Average *= scale
				response.Minutes[index].Amount *= scale
			}
		}
	}
}

func finiteNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func timezoneLabel(symbol string) string {
	// Keep the Web package independent from provider internals while exposing
	// the exchange-local timezone in the chart response.
	switch strings.ToLower(strings.TrimSpace(symbol)) {
	case "rt_hkhsi", "rt_hkhstech":
		return "Asia/Hong_Kong"
	case "b_nky":
		return "Asia/Tokyo"
	case "b_kospi", "b_kosdaq":
		return "Asia/Seoul"
	default:
		return "America/New_York"
	}
}

func (s *Server) fetchGlobalIndices(ctx context.Context) ([]domain.GlobalIndex, time.Time, error) {
	if s == nil || s.globalMarkets == nil {
		return nil, time.Time{}, fmt.Errorf("外盘指数服务未初始化")
	}
	now := time.Now()
	s.globalMu.Lock()
	cached := s.globalCache
	if cached.valid && now.Sub(cached.fetchedAt) >= 0 && now.Sub(cached.fetchedAt) < globalMarketCacheTTL {
		items := append([]domain.GlobalIndex(nil), cached.items...)
		s.globalMu.Unlock()
		return items, cached.fetchedAt, nil
	}
	s.globalMu.Unlock()

	items, err := s.globalMarkets.FetchGlobalIndices(ctx)
	if err != nil {
		if cached.valid && len(cached.items) > 0 {
			return append([]domain.GlobalIndex(nil), cached.items...), cached.fetchedAt, fmt.Errorf("外盘刷新失败，已使用上次数据：%w", err)
		}
		return nil, time.Time{}, err
	}
	s.globalMu.Lock()
	s.globalCache = globalMarketCacheEntry{items: append([]domain.GlobalIndex(nil), items...), fetchedAt: now, valid: true}
	s.globalMu.Unlock()
	return append([]domain.GlobalIndex(nil), items...), now, nil
}

func newGlobalMarketResponse(item domain.GlobalIndex, definition globalMarketDefinition) globalMarketResponse {
	if strings.TrimSpace(item.Symbol) == "" {
		item.Symbol = definition.Symbol
	}
	if strings.TrimSpace(item.Region) == "" {
		item.Region = definition.Region
	}
	if strings.TrimSpace(item.Name) == "" {
		item.Name = definition.Name
	}
	result := globalMarketResponse{
		Symbol: item.Symbol, Region: item.Region, Name: item.Name,
		Current: globalNumberString(item.Current), PreviousClose: globalNumberString(item.PreviousClose),
		Open: globalNumberString(item.Open), High: globalNumberString(item.High), Low: globalNumberString(item.Low),
		Delta: finitePointer(item.Delta), Percent: finitePointer(item.Percent),
		QuoteTime: item.QuoteTime, Source: item.Source,
	}
	if item.Extended != nil {
		result.Extended = &globalExtendedResponse{
			Session: item.Extended.Session, Symbol: item.Extended.Symbol, Name: item.Extended.Name,
			Price: globalNumberString(item.Extended.Price), Delta: finitePointer(item.Extended.Delta), Percent: finitePointer(item.Extended.Percent),
			Volume: finitePointer(item.Extended.Volume), QuoteTime: item.Extended.QuoteTime, Source: item.Extended.Source,
		}
	}
	return result
}

func globalNumberString(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "--"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
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
	// The leading yellow line is carried by minute points rather than the
	// quote snapshot. Promote the latest valid value to the quote summary for
	// broad-market indices so chart, tooltip and side metrics stay consistent.
	if quoteResult.err == nil && minuteResult.err == nil && market.IsBroadMarketSymbol(symbol) {
		for index := len(minuteResult.points) - 1; index >= 0; index-- {
			leading := minuteResult.points[index].Leading
			if leading > 0 && !math.IsNaN(leading) && !math.IsInf(leading, 0) {
				quoteResult.quote.Leading = leading
				break
			}
		}
	}
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
		if technical, technicalErr := strategy.AnalyzeTechnical(symbol, historyResult.bars); technicalErr == nil {
			response.Technical = &technical
		}
	}
	if minuteResult.err != nil {
		response.MinuteError = minuteResult.err.Error()
	} else {
		response.Minutes = newMinutePoints(minuteResult.points)
	}
	if s.relatedBoards != nil || s.stockNews != nil {
		relatedCtx, cancelRelated := context.WithTimeout(ctx, 5*time.Second)
		defer cancelRelated()
		boardsCh := make(chan []domain.BoardFlow, 1)
		newsCh := make(chan []domain.StockNewsItem, 1)
		if s.relatedBoards != nil {
			go func() { boards, _ := s.relatedBoards.FetchBoards(relatedCtx, symbol); boardsCh <- boards }()
		} else {
			boardsCh <- nil
		}
		if s.stockNews != nil {
			go func() {
				news, err := s.stockNews.FetchStockNews(relatedCtx, symbol, 8)
				if err != nil {
					response.NewsError = err.Error()
				}
				newsCh <- news
			}()
		} else {
			newsCh <- nil
		}
		for _, flow := range <-boardsCh {
			var leaders []domain.MarketStockSnapshot
			if s.boardDetails != nil && flow.Code != "" {
				_, leaders, _ = s.fetchBoard(relatedCtx, flow.Code)
				if len(leaders) > 3 {
					leaders = leaders[:3]
				}
			}
			response.RelatedBoards = append(response.RelatedBoards, *newBoardResponse(flow, leaders))
		}
		response.News = sortNewsByTime(<-newsCh)
	}
	status := http.StatusOK
	if response.Quote == nil && len(response.Bars) == 0 && len(response.Minutes) == 0 {
		status = http.StatusBadGateway
	}
	writeJSON(writer, status, response)
}

func sortNewsByTime(items []domain.StockNewsItem) []domain.StockNewsItem {
	result := append([]domain.StockNewsItem(nil), items...)
	parse := func(value string) time.Time {
		value = strings.TrimSpace(value)
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
			if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
				return parsed
			}
		}
		return time.Time{}
	}
	sort.SliceStable(result, func(left, right int) bool {
		lt, rt := parse(result[left].Date), parse(result[right].Date)
		if lt.IsZero() && rt.IsZero() {
			return false
		}
		if lt.IsZero() {
			return false
		}
		if rt.IsZero() {
			return true
		}
		return lt.After(rt)
	})
	return result
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
		Leading: finitePointer(quote.Leading),
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
			Price: point.Price, Average: point.Average, Leading: point.Leading, Volume: point.Volume, Amount: point.Amount,
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

func (s *Server) fetchGlobalDailyBars(ctx context.Context, symbol string) ([]domain.DailyBar, error) {
	if s == nil || s.globalCharts == nil {
		return nil, fmt.Errorf("外盘日 K 服务未初始化")
	}
	now := time.Now()
	s.globalChartMu.Lock()
	cached, found := s.globalHistoryCache[symbol]
	if found && now.Sub(cached.fetchedAt) >= 0 && now.Sub(cached.fetchedAt) < globalHistoryCacheTTL {
		bars := append([]domain.DailyBar(nil), cached.bars...)
		s.globalChartMu.Unlock()
		return bars, nil
	}
	s.globalChartMu.Unlock()

	bars, err := s.globalCharts.FetchGlobalDailyBars(ctx, symbol)
	if err != nil {
		if found && len(cached.bars) > 0 {
			return append([]domain.DailyBar(nil), cached.bars...), fmt.Errorf("外盘日 K 刷新失败，已使用上次数据：%w", err)
		}
		return nil, err
	}
	s.globalChartMu.Lock()
	s.globalHistoryCache[symbol] = historyCacheEntry{bars: append([]domain.DailyBar(nil), bars...), fetchedAt: now}
	s.globalChartMu.Unlock()
	return append([]domain.DailyBar(nil), bars...), nil
}

func (s *Server) fetchGlobalMinutePoints(ctx context.Context, symbol string) ([]domain.MinutePoint, error) {
	if s == nil || s.globalCharts == nil {
		return nil, fmt.Errorf("外盘分时服务未初始化")
	}
	now := time.Now()
	s.globalChartMu.Lock()
	cached, found := s.globalMinuteCache[symbol]
	if found && now.Sub(cached.fetchedAt) >= 0 && now.Sub(cached.fetchedAt) < globalMinuteCacheTTL {
		points := append([]domain.MinutePoint(nil), cached.points...)
		s.globalChartMu.Unlock()
		return points, nil
	}
	s.globalChartMu.Unlock()

	points, err := s.globalCharts.FetchGlobalMinutePoints(ctx, symbol)
	if err != nil {
		if found && len(cached.points) > 0 {
			return append([]domain.MinutePoint(nil), cached.points...), fmt.Errorf("外盘分时刷新失败，已使用上次数据：%w", err)
		}
		return nil, err
	}
	s.globalChartMu.Lock()
	s.globalMinuteCache[symbol] = minuteCacheEntry{points: append([]domain.MinutePoint(nil), points...), fetchedAt: now}
	s.globalChartMu.Unlock()
	return append([]domain.MinutePoint(nil), points...), nil
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
