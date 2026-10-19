package backtest

import "fmt"

const ExitProtectHalfProfit = "close-profit-half-giveback"

type ProfitProtectionEvidence struct {
	Rule             string  `json:"rule"`
	ActivationReturn float64 `json:"activation_return_percent"`
	PeakNetReturn    float64 `json:"peak_net_return_percent"`
	PeakDate         string  `json:"peak_date"`
	TriggerNetReturn float64 `json:"trigger_net_return_percent"`
	CurrentNetReturn float64 `json:"current_net_return_percent"`
}

func estimatedNetCloseReturn(request Request, position *positionState, close float64, date string) float64 {
	fill := makeFill(request, position.symbol, "sell", date, close, position.quantity, false)
	return (fill.Amount - fill.TotalFee - position.entryCost) / position.entryCost * 100
}

func updateNetClosePeak(request Request, position *positionState, close float64, date string) {
	value := estimatedNetCloseReturn(request, position, close, date)
	if value > position.peakNetCloseReturn {
		position.peakNetCloseReturn, position.peakNetCloseDate = value, date
	}
}

func profitProtectionSignal(snapshot SignalSnapshot, position *positionState, request Request) (SignalSnapshot, string, bool) {
	activation := request.Technical.StopLoss * 100
	value := estimatedNetCloseReturn(request, position, snapshot.Close, snapshot.Date)
	threshold := position.peakNetCloseReturn / 2
	if request.ExitProtection != ExitProtectHalfProfit || position.peakNetCloseReturn < activation || value > threshold {
		return snapshot, "", false
	}
	evidence := &ProfitProtectionEvidence{Rule: request.ExitProtection, ActivationReturn: activation, PeakNetReturn: position.peakNetCloseReturn, PeakDate: position.peakNetCloseDate, TriggerNetReturn: threshold, CurrentNetReturn: value}
	reason := fmt.Sprintf("收盘盈利保护：净浮盈曾达%.2f%%，现为%.2f%%，回吐至峰值一半以下", evidence.PeakNetReturn, value)
	snapshot.Action, snapshot.EntryMode = "卖出", request.Technical.EffectiveEntryMode()
	snapshot.Reasons, snapshot.ProfitProtection = []string{reason}, evidence
	return snapshot, reason, true
}
