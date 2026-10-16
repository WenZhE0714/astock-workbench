package backtest

import (
	"sort"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/marketregime"
)

const EntryMarketNonBear = "non-bear"

type EntryMarketBlock struct {
	Symbol        string              `json:"symbol"`
	SignalDate    string              `json:"signal_date"`
	AttemptDate   string              `json:"attempt_date"`
	BenchmarkDate string              `json:"benchmark_date,omitempty"`
	Regime        marketregime.Regime `json:"regime"`
	Reason        string              `json:"reason"`
}

func entryMarketDecision(bars []domain.DailyBar, date, previousSession string) (bool, marketregime.Regime, string) {
	end := sort.Search(len(bars), func(i int) bool { return bars[i].Date >= date }) - 1
	if end < 0 {
		return false, marketregime.Insufficient, ""
	}
	benchmarkDate := bars[end].Date
	if previousSession != "" && benchmarkDate < previousSession {
		return false, marketregime.Insufficient, benchmarkDate
	}
	regime := marketregime.ClassifyBefore(bars, date)
	return regime != marketregime.Bear && regime != marketregime.Insufficient, regime, benchmarkDate
}
