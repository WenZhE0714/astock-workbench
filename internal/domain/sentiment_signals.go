package domain

type HotTheme struct {
	Name        string  `json:"name"`
	Count       int     `json:"count"`
	Leader      string  `json:"leader,omitempty"`
	AverageRise float64 `json:"average_rise_percent,omitempty"`
	Amount      float64 `json:"amount_yuan,omitempty"`
}

type HotStockSignal struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	Percent   float64 `json:"percent"`
	Turnover  float64 `json:"turnover_percent"`
	Amount    float64 `json:"amount_yuan"`
	MainRatio float64 `json:"main_ratio"`
	Reason    string  `json:"reason,omitempty"`
}

type HotStockSnapshot struct {
	TradeDate string           `json:"trade_date"`
	Stocks    []HotStockSignal `json:"stocks"`
	Themes    []HotTheme       `json:"themes"`
	Available bool             `json:"available"`
	Source    string           `json:"source,omitempty"`
}
