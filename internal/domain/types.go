package domain

// Candidate is a resolved A-share name/code candidate.
type Candidate struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
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

// FundFlow is an Eastmoney main-fund-flow snapshot. MainNet is denominated in
// yuan; a positive value is a net inflow and a negative value a net outflow.
type FundFlow struct {
	Symbol    string  `json:"symbol"`
	MainNet   float64 `json:"main_net_yuan"`
	MainRatio float64 `json:"main_ratio_percent"`
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
	LeaderName    string  `json:"leader_name"`
	LeaderCode    string  `json:"leader_code"`
	LeaderPercent float64 `json:"leader_percent"`
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
