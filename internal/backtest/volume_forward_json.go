package backtest

import (
	"encoding/json"
	"math"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

// Match research archives: unavailable turnover is null on disk and NaN at
// runtime. Required OHLCV fields retain normal JSON validation.
type forwardBarJSON struct {
	domain.DailyBar
	Turnover *float64 `json:"turnover_percent"`
}

func (bars forwardBars) MarshalJSON() ([]byte, error) {
	if bars == nil {
		return []byte("null"), nil
	}
	value := make(map[string][]forwardBarJSON, len(bars))
	for symbol, items := range bars {
		var encoded []forwardBarJSON
		if items != nil {
			encoded = make([]forwardBarJSON, 0, len(items))
		}
		for _, bar := range items {
			var turnover *float64
			if finiteValues(bar.Turnover) {
				number := bar.Turnover
				turnover = &number
			}
			encoded = append(encoded, forwardBarJSON{DailyBar: bar, Turnover: turnover})
		}
		value[symbol] = encoded
	}
	return json.Marshal(value)
}

func (bars *forwardBars) UnmarshalJSON(data []byte) error {
	var value map[string][]forwardBarJSON
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var result forwardBars
	if value != nil {
		result = make(forwardBars, len(value))
	}
	for symbol, items := range value {
		var decoded []domain.DailyBar
		if items != nil {
			decoded = make([]domain.DailyBar, 0, len(items))
		}
		for _, bar := range items {
			bar.DailyBar.Turnover = math.NaN()
			if bar.Turnover != nil {
				bar.DailyBar.Turnover = *bar.Turnover
			}
			decoded = append(decoded, bar.DailyBar)
		}
		result[symbol] = decoded
	}
	*bars = result
	return nil
}
