package realtime

import (
	"encoding/json"
	"math"
)

type signalJSONAlias Signal

type signalJSON struct {
	signalJSONAlias
	Percent *float64 `json:"percent"`
	Speed   *float64 `json:"speed_percent"`
}

// Optional quote fields stay missing across HTTP responses and archive reloads.
func (signal Signal) MarshalJSON() ([]byte, error) {
	optional := func(value float64) *float64 {
		if !finite(value) {
			return nil
		}
		return &value
	}
	return json.Marshal(signalJSON{signalJSONAlias: signalJSONAlias(signal), Percent: optional(signal.Percent), Speed: optional(signal.Speed)})
}

func (signal *Signal) UnmarshalJSON(data []byte) error {
	var value signalJSON
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	result := Signal(value.signalJSONAlias)
	result.Percent, result.Speed = math.NaN(), math.NaN()
	if value.Percent != nil {
		result.Percent = *value.Percent
	}
	if value.Speed != nil {
		result.Speed = *value.Speed
	}
	*signal = result
	return nil
}
