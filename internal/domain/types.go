package domain

// Candidate is a resolved A-share name/code candidate.
type Candidate struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
}

type MarketRankingKind string

const (
	MarketRankingGainers   MarketRankingKind = "gainers"
	MarketRankingLosers    MarketRankingKind = "losers"
	MarketRankingRapidRise MarketRankingKind = "rapid_rise"
)

// MarketRankingItem is one Eastmoney Shanghai/Shenzhen A-share ranking row.
type MarketRankingItem struct {
	Symbol   string  `json:"symbol"`
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Percent  float64 `json:"percent"`
	Speed    float64 `json:"speed"`
	Industry string  `json:"industry"`
}

type DepthLevel struct {
	Level  int    `json:"level"`
	Price  string `json:"price"`
	Volume string `json:"volume"`
}

// Quote is a Tencent Level-1 quote. Display-oriented numeric fields stay as
// strings so an unavailable value can be represented as "--" without losing
// fidelity from the upstream payload.
type Quote struct {
	Symbol         string       `json:"symbol"`
	Name           string       `json:"name"`
	TaskName       string       `json:"task_name"`
	Code           string       `json:"code"`
	Current        string       `json:"current"`
	PreviousClose  string       `json:"previous_close"`
	Open           string       `json:"open"`
	QuoteTime      string       `json:"quote_time"`
	Delta          float64      `json:"delta"`
	Percent        float64      `json:"percent"`
	High           string       `json:"high"`
	Low            string       `json:"low"`
	Volume         float64      `json:"volume"`
	Amount         float64      `json:"amount"`
	Turnover       string       `json:"turnover"`
	Amplitude      string       `json:"amplitude"`
	PETTM          string       `json:"pe_ttm"`
	PEStatic       string       `json:"pe_static"`
	PB             string       `json:"pb"`
	MarketCap      float64      `json:"market_cap_yi"`
	FloatMarketCap float64      `json:"float_market_cap_yi"`
	LimitUp        string       `json:"limit_up"`
	LimitDown      string       `json:"limit_down"`
	VolumeRatio    string       `json:"volume_ratio"`
	AveragePrice   string       `json:"average_price"`
	Bids           []DepthLevel `json:"bids,omitempty"`
	Asks           []DepthLevel `json:"asks,omitempty"`
}

// MarketAmountSnapshot contains one trading day's exchange-wide A-share
// turnover. Amounts use ten-thousand yuan, matching Tencent quote field 37.
type MarketAmountSnapshot struct {
	TradeDate string  `json:"trade_date"`
	Shanghai  float64 `json:"shanghai_wan_yuan"`
	Shenzhen  float64 `json:"shenzhen_wan_yuan"`
	Beijing   float64 `json:"beijing_wan_yuan"`
	Source    string  `json:"source"`
}

// DailyBar is one unadjusted daily K-line. Price levels derived from these
// bars stay on the same scale as the Level-1 quote shown to the user.
type DailyBar struct {
	Symbol   string  `json:"symbol"`
	Source   string  `json:"source"`
	Date     string  `json:"date"`
	Open     float64 `json:"open"`
	Close    float64 `json:"close"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Volume   float64 `json:"volume"`
	Amount   float64 `json:"amount_yuan"`
	Turnover float64 `json:"turnover_percent"`
}

const (
	TechnicalStatusLoading     = "loading"
	TechnicalStatusReady       = "ready"
	TechnicalStatusUnavailable = "unavailable"
)

// TechnicalSignal is a deterministic price-volume observation. It describes
// scenarios and invalidations, not an executable order.
type TechnicalSignal struct {
	Status       string   `json:"status"`
	Symbol       string   `json:"symbol"`
	DataSource   string   `json:"data_source"`
	DataDate     string   `json:"data_date"`
	Bias         string   `json:"bias"`
	Action       string   `json:"action"`
	OptionLike   string   `json:"option_like,omitempty"`
	Strength     int      `json:"strength"`
	Score        int      `json:"score"`
	Price        float64  `json:"price"`
	MA5          float64  `json:"ma5"`
	MA20         float64  `json:"ma20"`
	MA60         float64  `json:"ma60"`
	MACD         float64  `json:"macd_histogram"`
	RSI14        float64  `json:"rsi14"`
	VolumeRatio  float64  `json:"volume_ratio_20d"`
	High20       float64  `json:"prior_20d_high"`
	Low20        float64  `json:"prior_20d_low"`
	Support      string   `json:"support"`
	Resistance   string   `json:"resistance"`
	BuyTrigger   string   `json:"buy_trigger"`
	SellTrigger  string   `json:"sell_trigger"`
	Invalidation string   `json:"invalidation"`
	PositionPlan string   `json:"position_plan"`
	Evidence     []string `json:"evidence"`
	Error        string   `json:"error,omitempty"`
}

// FundFlow is an Eastmoney main-fund-flow snapshot. MainNet is denominated in
// yuan; a positive value is a net inflow and a negative value a net outflow.
type FundFlow struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name,omitempty"`
	Industry  string  `json:"industry,omitempty"`
	Price     float64 `json:"price"`
	Percent   float64 `json:"percent"`
	MainNet   float64 `json:"main_net_yuan"`
	MainRatio float64 `json:"main_ratio_percent"`
}

// FundMovement is a short-line behavior observation derived from cumulative
// main-fund snapshots. It is evidence for monitoring, not a trading order.
type FundMovement struct {
	Symbol          string  `json:"symbol"`
	Name            string  `json:"name"`
	Industry        string  `json:"industry"`
	Price           float64 `json:"price"`
	Percent         float64 `json:"percent"`
	MainNet         float64 `json:"main_net_yuan"`
	MainRatio       float64 `json:"main_ratio_percent"`
	Delta1Minute    float64 `json:"delta_1m_yuan"`
	Delta3Minutes   float64 `json:"delta_3m_yuan"`
	Delta5Minutes   float64 `json:"delta_5m_yuan"`
	IndustryNet     float64 `json:"industry_main_net_yuan"`
	IndustryPercent float64 `json:"industry_percent"`
	State           string  `json:"state"`
}

const (
	BoardKindIndustry = "industry"
	BoardKindConcept  = "concept"
)

// BoardFlow is an Eastmoney industry/concept board snapshot associated with
// one stock. MainNet is denominated in yuan.
type BoardFlow struct {
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	Percent       float64 `json:"percent"`
	MainNet       float64 `json:"main_net_yuan"`
	MainRatio     float64 `json:"main_ratio_percent"`
	Turnover      float64 `json:"turnover_percent"`
	RiseCount     int     `json:"rise_count"`
	FallCount     int     `json:"fall_count"`
	FlatCount     int     `json:"flat_count"`
	ChangeRank    int     `json:"change_rank"`
	FlowRank      int     `json:"flow_rank"`
	TurnoverRank  int     `json:"turnover_rank"`
	UniverseSize  int     `json:"universe_size"`
	LeaderName    string  `json:"leader_name"`
	LeaderCode    string  `json:"leader_code"`
	LeaderPercent float64 `json:"leader_percent"`
}

// DragonTigerEntry is one Eastmoney daily-billboard record. A stock can have
// multiple entries on the same date when it triggers more than one rule.
type DragonTigerEntry struct {
	Symbol          string  `json:"symbol"`
	Name            string  `json:"name"`
	TradeDate       string  `json:"trade_date"`
	Reason          string  `json:"reason"`
	SeatSummary     string  `json:"seat_summary"`
	ClosePrice      float64 `json:"close_price"`
	ChangePercent   float64 `json:"change_percent"`
	NetAmount       float64 `json:"net_amount_yuan"`
	BuyAmount       float64 `json:"buy_amount_yuan"`
	SellAmount      float64 `json:"sell_amount_yuan"`
	DealAmount      float64 `json:"deal_amount_yuan"`
	MarketAmount    float64 `json:"market_amount_yuan"`
	NetRatio        float64 `json:"net_ratio_percent"`
	DealAmountRatio float64 `json:"deal_amount_ratio_percent"`
	Turnover        float64 `json:"turnover_percent"`
	Next1Percent    float64 `json:"next_1d_percent"`
	Next2Percent    float64 `json:"next_2d_percent"`
	Next5Percent    float64 `json:"next_5d_percent"`
	Next10Percent   float64 `json:"next_10d_percent"`
}

type DragonTigerSnapshot struct {
	Loaded     bool               `json:"loaded"`
	WindowDays int                `json:"window_days"`
	Entries    []DragonTigerEntry `json:"entries"`
}

type EngineInfo struct {
	Name     string `json:"name"`
	Version  string `json:"version,omitempty"`
	RepoPath string `json:"repo_path"`
}

// AnalysisResult is the stable boundary between the Go workbench and the
// Python TradingAgents engine. Strategy, storage and future execution modules
// consume this shape instead of importing the LLM framework directly.
type AnalysisResult struct {
	SchemaVersion   int               `json:"schema_version"`
	Status          string            `json:"status"`
	ID              string            `json:"id"`
	Ticker          string            `json:"ticker"`
	TradeDate       string            `json:"trade_date"`
	CreatedAt       string            `json:"created_at"`
	DurationSeconds float64           `json:"duration_seconds"`
	Signal          string            `json:"signal"`
	Provider        string            `json:"provider"`
	DeepModel       string            `json:"deep_model"`
	QuickModel      string            `json:"quick_model"`
	Engine          EngineInfo        `json:"engine"`
	DataVendors     map[string]string `json:"data_vendors"`
	Reports         map[string]string `json:"reports"`
	Disclaimer      string            `json:"disclaimer"`
	Error           string            `json:"error,omitempty"`
}

type AnalysisCheck struct {
	SchemaVersion int        `json:"schema_version"`
	Status        string     `json:"status"`
	PythonVersion string     `json:"python_version"`
	Engine        EngineInfo `json:"engine"`
	Provider      string     `json:"provider"`
	CredentialEnv string     `json:"credential_env,omitempty"`
	CredentialSet bool       `json:"credential_set"`
	Error         string     `json:"error,omitempty"`
}
