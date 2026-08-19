package domain

import "time"

// StockNewsItem is a third-party market-news clue. It is not an official
// disclosure and must not be used as the sole basis for a trading conclusion.
type StockNewsItem struct {
	Symbol    string `json:"symbol,omitempty"`
	Name      string `json:"name,omitempty"`
	Date      string `json:"date"`
	Title     string `json:"title"`
	Source    string `json:"source"`
	URL       string `json:"url,omitempty"`
	Relevance string `json:"relevance,omitempty"` // direct_mention or market_context
}

type StockQuoteSnapshot struct {
	Source         string  `json:"source"`
	Symbol         string  `json:"symbol"`
	Name           string  `json:"name"`
	Price          float64 `json:"price"`
	Percent        float64 `json:"percent"`
	Open           float64 `json:"open"`
	High           float64 `json:"high"`
	Low            float64 `json:"low"`
	PreviousClose  float64 `json:"previous_close"`
	AveragePrice   float64 `json:"average_price"`
	VolumeRatio    float64 `json:"volume_ratio"`
	Turnover       float64 `json:"turnover_percent"`
	Amount         float64 `json:"amount_wan_yuan"`
	PETTM          float64 `json:"pe_ttm"`
	PB             float64 `json:"pb"`
	MarketCap      float64 `json:"market_cap_yi"`
	FloatMarketCap float64 `json:"float_market_cap_yi"`
	LimitUp        float64 `json:"limit_up"`
	LimitDown      float64 `json:"limit_down"`
	QuoteTime      string  `json:"quote_time"`
}

// StockPriceBoundary is the exchange price interval reported by the live
// quote source for the current trading day. Technical levels outside this
// interval remain useful only as cross-session structure levels.
type StockPriceBoundary struct {
	TradeDate string  `json:"trade_date"`
	LimitUp   float64 `json:"limit_up"`
	LimitDown float64 `json:"limit_down"`
	Available bool    `json:"available"`
	Source    string  `json:"source"`
}

type StockIndexContext struct {
	Symbol  string  `json:"symbol"`
	Name    string  `json:"name"`
	Price   float64 `json:"price"`
	Percent float64 `json:"percent"`
	MainNet float64 `json:"main_net_yuan"`
}

type StockReportFacts struct {
	SchemaVersion int                  `json:"schema_version"`
	SnapshotHash  string               `json:"snapshot_hash,omitempty"`
	GeneratedAt   time.Time            `json:"generated_at"`
	Quote         StockQuoteSnapshot   `json:"quote"`
	PriceBoundary StockPriceBoundary   `json:"price_boundary"`
	Market        []StockIndexContext  `json:"market"`
	Technical     TechnicalSignal      `json:"technical"`
	Fund          FundFlow             `json:"fund"`
	FundMovement  *FundMovement        `json:"fund_movement,omitempty"`
	Boards        []BoardFlow          `json:"boards"`
	DragonTiger   DragonTigerSnapshot  `json:"dragon_tiger"`
	Announcements []MarketAnnouncement `json:"announcements,omitempty"`
	News          []StockNewsItem      `json:"news_clues,omitempty"`
	Evidence      EvidenceSnapshot     `json:"evidence"`
	Warnings      []string             `json:"warnings,omitempty"`
}

type GeneratedStockReport struct {
	GeneratedAt  time.Time          `json:"generated_at"`
	Symbol       string             `json:"symbol"`
	Name         string             `json:"name"`
	AIUsed       bool               `json:"ai_used"`
	AIError      string             `json:"ai_error,omitempty"`
	Markdown     string             `json:"markdown"`
	Facts        StockReportFacts   `json:"facts"`
	Agents       []AgentResearchRun `json:"agents,omitempty"`
	Directory    string             `json:"-"`
	MarkdownPath string             `json:"-"`
	FactsPath    string             `json:"-"`
	AgentsPath   string             `json:"-"`
	EvidencePath string             `json:"-"`
}
