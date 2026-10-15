package paper

import "github.com/wenzhe/astock-workbench/internal/realtime"

func signalsSinceTradingStart(signals []realtime.Signal, cfg Config) []realtime.Signal {
	if cfg.TradingStartAt.IsZero() {
		return signals
	}
	filtered := make([]realtime.Signal, 0, len(signals))
	for _, signal := range signals {
		if !signal.AsOf.IsZero() && !signal.AsOf.Before(cfg.TradingStartAt) {
			filtered = append(filtered, signal)
		}
	}
	return filtered
}
