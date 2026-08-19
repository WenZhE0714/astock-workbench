package domain

import "time"

type MarketScanMetric string

const (
	MarketScanByPercent MarketScanMetric = "percent"
	MarketScanByAmount  MarketScanMetric = "amount"
	MarketScanByMainNet MarketScanMetric = "main_net"
)

// MarketStockSnapshot is a cross-sectional Shanghai/Shenzhen A-share quote
// used by the market scanner. Amount and MainNet are denominated in yuan.
type MarketStockSnapshot struct {
	Symbol        string  `json:"symbol"`
	Name          string  `json:"name"`
	Industry      string  `json:"industry"`
	Price         float64 `json:"price"`
	Percent       float64 `json:"percent"`
	Amount        float64 `json:"amount_yuan"`
	Turnover      float64 `json:"turnover_percent"`
	VolumeRatio   float64 `json:"volume_ratio"`
	Speed         float64 `json:"speed_percent"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	Open          float64 `json:"open"`
	PreviousClose float64 `json:"previous_close"`
	MarketCap     float64 `json:"market_cap_yuan"`
	MainNet       float64 `json:"main_net_yuan"`
	MainRatio     float64 `json:"main_ratio_percent"`
}

type MarketAnnouncement struct {
	Symbol  string `json:"symbol"`
	Name    string `json:"name"`
	Date    string `json:"date"`
	Title   string `json:"title"`
	ArtCode string `json:"art_code"`
}

type MarketTechnicalSnapshot struct {
	DataSource    string  `json:"data_source,omitempty"`
	DataDate      string  `json:"data_date"`
	Close         float64 `json:"close"`
	Return5       float64 `json:"return_5d_percent"`
	Return20      float64 `json:"return_20d_percent"`
	Return60      float64 `json:"return_60d_percent"`
	MA5           float64 `json:"ma5"`
	MA20          float64 `json:"ma20"`
	MA60          float64 `json:"ma60"`
	VolumeRatio20 float64 `json:"volume_ratio_20d"`
	ClosePosition float64 `json:"close_position_percent"`
	Prior20High   float64 `json:"prior_20d_high"`
	Prior20Low    float64 `json:"prior_20d_low"`
	Trend         string  `json:"trend"`
}

type MarketIndexAssessment struct {
	Symbol    string                  `json:"symbol"`
	Name      string                  `json:"name"`
	Percent   float64                 `json:"percent"`
	MainNet   float64                 `json:"main_net_yuan"`
	Technical MarketTechnicalSnapshot `json:"technical"`
}

type MarketBoardAssessment struct {
	BoardFlow
	Score         float64 `json:"score"`
	FlowAvailable bool    `json:"flow_available"`
}

type MarketCandidateAssessment struct {
	Stock         MarketStockSnapshot     `json:"stock"`
	Technical     MarketTechnicalSnapshot `json:"technical"`
	MatchedBoard  string                  `json:"matched_board,omitempty"`
	BoardLeader   bool                    `json:"board_leader"`
	FlowAvailable bool                    `json:"flow_available"`
	Score         float64                 `json:"score"`
	Grade         string                  `json:"grade"`
	Category      string                  `json:"category,omitempty"`
	Reasons       []string                `json:"reasons"`
	Risks         []string                `json:"risks"`
	Announcements []MarketAnnouncement    `json:"announcements,omitempty"`
	EvidenceIDs   []string                `json:"evidence_ids,omitempty"`
}

type MarketScanFacts struct {
	SchemaVersion      int                         `json:"schema_version"`
	SnapshotHash       string                      `json:"snapshot_hash,omitempty"`
	GeneratedAt        time.Time                   `json:"generated_at"`
	MarketStatus       string                      `json:"market_status"`
	QuoteSource        string                      `json:"quote_source,omitempty"`
	QuoteTime          string                      `json:"quote_time,omitempty"`
	CurrentAmount      float64                     `json:"current_market_amount_wan_yuan"`
	PreviousAmount     float64                     `json:"previous_market_amount_wan_yuan"`
	AmountChange       float64                     `json:"amount_change_percent"`
	Indices            []MarketIndexAssessment     `json:"indices"`
	HotBoards          []MarketBoardAssessment     `json:"hot_boards"`
	WeakBoards         []MarketBoardAssessment     `json:"weak_boards"`
	Candidates         []MarketCandidateAssessment `json:"candidates"`
	TopAmountAdvancers int                         `json:"top_amount_advancers"`
	TopAmountDecliners int                         `json:"top_amount_decliners"`
	TopAmountMainNet   float64                     `json:"top_amount_main_net_yuan"`
	Evidence           EvidenceSnapshot            `json:"evidence"`
	Warnings           []string                    `json:"warnings,omitempty"`
}

type GeneratedMarketReport struct {
	GeneratedAt  time.Time          `json:"generated_at"`
	AIUsed       bool               `json:"ai_used"`
	AIError      string             `json:"ai_error,omitempty"`
	Markdown     string             `json:"markdown"`
	Facts        MarketScanFacts    `json:"facts"`
	Agents       []AgentResearchRun `json:"agents,omitempty"`
	Directory    string             `json:"-"`
	MarkdownPath string             `json:"-"`
	FactsPath    string             `json:"-"`
	AgentsPath   string             `json:"-"`
	EvidencePath string             `json:"-"`
}
