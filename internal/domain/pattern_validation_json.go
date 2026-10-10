package domain

import (
	"encoding/json"
	"math"
)

// Quote providers use NaN for unavailable turnover. Preserve it as null in
// research archives without changing DailyBar's shared runtime contract.
type patternValidationBarJSON struct {
	DailyBar
	Turnover *float64 `json:"turnover_percent"`
}

type patternValidationInputJSON struct {
	Symbol string                     `json:"symbol"`
	Bars   []patternValidationBarJSON `json:"bars"`
	Error  string                     `json:"error,omitempty"`
}

func (input PatternValidationInput) MarshalJSON() ([]byte, error) {
	value := patternValidationInputJSON{Symbol: input.Symbol, Error: input.Error}
	for _, bar := range input.Bars {
		var turnover *float64
		if !math.IsNaN(bar.Turnover) && !math.IsInf(bar.Turnover, 0) {
			number := bar.Turnover
			turnover = &number
		}
		value.Bars = append(value.Bars, patternValidationBarJSON{DailyBar: bar, Turnover: turnover})
	}
	return json.Marshal(value)
}

func (input *PatternValidationInput) UnmarshalJSON(data []byte) error {
	var value patternValidationInputJSON
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	result := PatternValidationInput{Symbol: value.Symbol, Error: value.Error}
	for _, bar := range value.Bars {
		bar.DailyBar.Turnover = math.NaN()
		if bar.Turnover != nil {
			bar.DailyBar.Turnover = *bar.Turnover
		}
		result.Bars = append(result.Bars, bar.DailyBar)
	}
	*input = result
	return nil
}
