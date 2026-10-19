package backtest

type EntryVolumeBlock struct {
	Symbol      string  `json:"symbol"`
	SignalDate  string  `json:"signal_date"`
	VolumeRatio float64 `json:"volume_ratio"`
	Minimum     float64 `json:"minimum_volume_ratio"`
}

func entryVolumeAllowed(signal SignalSnapshot, minimum float64) bool {
	return minimum == 0 || finiteValues(signal.VolumeRatio) && signal.VolumeRatio >= minimum
}
