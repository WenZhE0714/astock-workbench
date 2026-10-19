package backtest

const EntryConfirmReclaimTwoClose = "reclaim-two-close"

type EntryConfirmationEvent struct {
	Symbol           string  `json:"symbol"`
	SignalDate       string  `json:"signal_date"`
	ConfirmationDate string  `json:"confirmation_date,omitempty"`
	Status           string  `json:"status"`
	Close            float64 `json:"close,omitempty"`
	FastMA           float64 `json:"fast_ma,omitempty"`
	SlowMA           float64 `json:"slow_ma,omitempty"`
	Reason           string  `json:"reason"`
}

func confirmReclaimEntry(symbol string, signal, snapshot SignalSnapshot, ready bool) EntryConfirmationEvent {
	event := EntryConfirmationEvent{Symbol: symbol, SignalDate: signal.Date, ConfirmationDate: snapshot.Date, Status: "rejected", Reason: "确认日价格或均线趋势失守"}
	if !ready {
		event.Reason = "确认日指标不足"
		return event
	}
	event.Close, event.FastMA, event.SlowMA = snapshot.Close, snapshot.FastMA, snapshot.SlowMA
	if snapshot.Close > snapshot.FastMA && snapshot.FastMA > snapshot.SlowMA {
		event.Status, event.Reason = "confirmed", "下一交易日收盘仍高于快线，且快线高于慢线"
	}
	return event
}
