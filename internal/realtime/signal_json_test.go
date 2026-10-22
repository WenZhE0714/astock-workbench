package realtime

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestSignalJSONPreservesOptionalQuoteValues(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 0, -2.5, 3.2} {
		signal := Signal{ID: "signal", Symbol: "sh600000", Score: 60, Price: 18, Percent: value, Speed: value,
			Components: []Component{{Key: "trend-breakout", Score: 14, Maximum: 20}}, Monster: MonsterRadar{Score: 30}}
		data, err := json.Marshal(signal)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"percent", "speed_percent"} {
			got, exists := fields[key]
			if !exists || finite(value) && got != value || !finite(value) && got != nil {
				t.Fatalf("incorrect optional value %s=%v for %v", key, got, value)
			}
		}
		var restored Signal
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatal(err)
		}
		if finite(value) {
			if !reflect.DeepEqual(restored, signal) {
				t.Fatalf("finite signal changed: %+v", restored)
			}
		} else if !math.IsNaN(restored.Percent) || !math.IsNaN(restored.Speed) || restored.ID != signal.ID || restored.Price != signal.Price || !reflect.DeepEqual(restored.Components, signal.Components) || !reflect.DeepEqual(restored.Monster, signal.Monster) {
			t.Fatalf("missing quote fields or other signal fields changed: %+v", restored)
		}
	}
}

func TestSignalJSONReadsLegacyNumbersAndMissingFields(t *testing.T) {
	for _, data := range []string{`{"percent":0,"speed_percent":-0.5}`, `{"id":"legacy"}`, `{"percent":null,"speed_percent":null}`} {
		var signal Signal
		if err := json.Unmarshal([]byte(data), &signal); err != nil {
			t.Fatal(err)
		}
		if data == `{"percent":0,"speed_percent":-0.5}` {
			if signal.Percent != 0 || signal.Speed != -0.5 {
				t.Fatalf("legacy numeric values changed: %+v", signal)
			}
		} else if !math.IsNaN(signal.Percent) || !math.IsNaN(signal.Speed) {
			t.Fatalf("missing values became zero: %+v", signal)
		}
	}
	if _, err := json.Marshal(Signal{Price: math.NaN()}); err == nil {
		t.Fatal("invalid required price must still be rejected")
	}
}
