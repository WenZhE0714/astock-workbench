package paper

import (
	"fmt"
	"math"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func calendarLedgerDate(date, timestamp string) (string, time.Time, bool) {
	parsed, err := time.Parse(time.DateOnly, date)
	if err != nil || parsed.Format(time.DateOnly) != date {
		return "", time.Time{}, false
	}
	if timestamp == "" {
		return date, time.Time{}, true
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	at, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		var ok bool
		at, ok = parseRealtimeTimestamp(timestamp)
		if ok {
			err = nil
		}
	}
	if err != nil || at.In(location).Format(time.DateOnly) != date {
		return "", time.Time{}, false
	}
	return date, at, true
}

// Calendar events use archived fills only. No positions are marked to market
// and no hypothetical future exits are included.
func ReviewCalendarEvents(report Report) ([]domain.ReviewCalendarEvent, []string) {
	events, warnings := []domain.ReviewCalendarEvent{}, []string{}
	if _, _, ok := calendarLedgerDate(report.AsOf, ""); !ok {
		return events, []string{"该影子账户尚无有效归档日期"}
	}
	trades, conflicts := map[string]ShadowTrade{}, map[string]bool{}
	for _, trade := range report.Trades {
		if prior, exists := trades[trade.ID]; exists && prior != trade {
			conflicts[trade.ID] = true
		}
		trades[trade.ID] = trade
	}
	invalid := 0
	withinCheckpoint := func(date string, at time.Time) bool {
		if date != report.AsOf || report.CheckpointPhase != CheckpointOpen {
			return true
		}
		_, cutoff, ok := calendarLedgerDate(report.AsOf, report.LastRealtimeAt)
		if !ok || cutoff.IsZero() {
			_, cutoff, _ = calendarLedgerDate(report.AsOf, report.AsOf+" 09:30:00")
		}
		return !at.IsZero() && !at.After(cutoff)
	}
	for _, trade := range trades {
		date, at, dateOK := calendarLedgerDate(trade.ExitDate, trade.ExitTime)
		_, _, entryOK := calendarLedgerDate(trade.EntryDate, "")
		if dateOK && date > report.AsOf {
			continue
		}
		if dateOK && !withinCheckpoint(date, at) {
			continue
		}
		expected := (trade.ExitPrice-trade.EntryPrice)*float64(trade.Quantity) - trade.TotalFee
		if conflicts[trade.ID] || trade.ID == "" || trade.Symbol == "" || !dateOK || !entryOK || trade.EntryDate > date || trade.Quantity <= 0 ||
			!finite(trade.NetProfit) || !finite(expected) || !finite(trade.TotalFee) || trade.TotalFee < 0 || trade.EntryPrice <= 0 || trade.ExitPrice <= 0 ||
			math.Abs(expected-trade.NetProfit) > math.Max(.02, math.Abs(expected)*1e-8) {
			invalid++
			continue
		}
		net, price := trade.NetProfit, trade.ExitPrice
		events = append(events, domain.ReviewCalendarEvent{ID: "trade:" + trade.ID, Date: date, At: at, Kind: "exit", Symbol: trade.Symbol, Name: trade.Name,
			PlanID: trade.PlanID, Label: "影子平仓", Note: trade.ExitReason, NetProfit: &net, Quantity: trade.Quantity, Price: &price})
	}
	orders, orderConflicts := map[string]ShadowOrder{}, map[string]bool{}
	for _, order := range report.Orders {
		if order.Side != "buy" || order.Status != OrderFilled {
			continue
		}
		if prior, exists := orders[order.ID]; exists && (prior.Symbol != order.Symbol || prior.Quantity != order.Quantity || prior.Price != order.Price || prior.AttemptDate != order.AttemptDate || prior.ExecutionTime != order.ExecutionTime) {
			orderConflicts[order.ID] = true
		}
		orders[order.ID] = order
	}
	for _, order := range orders {
		date, at, ok := calendarLedgerDate(order.AttemptDate, order.ExecutionTime)
		if ok && date > report.AsOf {
			continue
		}
		if ok && !withinCheckpoint(date, at) {
			continue
		}
		if !ok || order.ID == "" || order.Symbol == "" || orderConflicts[order.ID] || order.Quantity <= 0 || !finite(order.Price) || order.Price <= 0 {
			invalid++
			continue
		}
		price := order.Price
		events = append(events, domain.ReviewCalendarEvent{ID: "order:" + order.ID, Date: date, At: at, Kind: "entry", Symbol: order.Symbol, Name: order.Name,
			PlanID: order.PlanID, Label: "影子买入", Note: order.Reason, Quantity: order.Quantity, Price: &price})
	}
	if invalid > 0 {
		warnings = append(warnings, fmt.Sprintf("%d条归档记录日期、金额或标识不一致，未计入日历", invalid))
	}
	return events, warnings
}
