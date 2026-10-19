package web

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/notify"
	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

func WithNativeNotifications(dispatcher *notify.Dispatcher) ServerOption {
	return func(server *Server) { server.nativeNotifications = dispatcher }
}

func (s *Server) notifyRealtimeCandidates(result realtime.ScanResult) {
	if s.nativeNotifications == nil {
		return
	}
	for _, message := range candidateNotifications(result, s.currentTime()) {
		s.nativeNotifications.Offer(message)
	}
}

func candidateNotifications(result realtime.ScanResult, now time.Time) []notify.Message {
	if result.MarketState != realtime.MarketStateTrading || !notify.Fresh(result.GeneratedAt, now) {
		return nil
	}
	var messages []notify.Message
	for _, signal := range result.Signals {
		if signal.State != realtime.StateTriggered || !signal.PortfolioEligible || signal.Symbol == "" || !notify.Fresh(signal.AsOf, now) || !validNotificationPrice(signal.Price) {
			continue
		}
		quoteAt := notificationTime(signal.QuoteTime)
		if !notify.Fresh(quoteAt, now) {
			continue
		}
		date := signal.AsOf.In(realtimeWebLocation).Format(time.DateOnly)
		body := fmt.Sprintf("现价 %.3f · 综合分 %.1f · %s", signal.Price, signal.Score, quoteAt.In(realtimeWebLocation).Format("15:04:05"))
		if len(signal.Reasons) > 0 {
			body += "\n" + signal.Reasons[0]
		}
		messages = append(messages, notify.Message{
			Symbol: signal.Symbol, Kind: "candidate",
			ID: "candidate:" + date + ":" + signal.Symbol, Title: "ASTOCK · 候选触发",
			Subtitle: signal.Name + " " + signal.Symbol, Body: body, At: signal.AsOf,
		})
	}
	return messages
}

func (s *Server) notifyShadowOrders(profile shadowExecutionProfile, previous, report paper.Report) {
	if s.nativeNotifications == nil {
		return
	}
	for _, message := range shadowOrderNotifications(profile.ID, profile.Name, previous, report, s.currentTime()) {
		s.nativeNotifications.Offer(message)
	}
}

func shadowOrderNotifications(profileID, name string, previous, report paper.Report, now time.Time) []notify.Message {
	seen := make(map[string]bool, len(previous.Orders))
	for _, order := range previous.Orders {
		seen[order.ID] = true
	}
	var messages []notify.Message
	for _, order := range report.Orders {
		if order.ID == "" || seen[order.ID] || order.Status != paper.OrderFilled || !strings.HasPrefix(order.EventSource, "realtime-") || order.Quantity <= 0 || !validNotificationPrice(order.Price) {
			continue
		}
		at := notificationTime(order.ExecutionTime)
		if !notify.Fresh(at, now) {
			continue
		}
		side := "买入"
		if order.Side == "sell" {
			side = "卖出"
		} else if order.Side != "buy" {
			continue
		}
		action := map[string]string{"open": "开仓", "add": "加仓", "reduce": "减仓", "exit": "退出", "risk_exit": "风险退出", "rotate_in": "调入", "rotate_out": "调出", "t_reduce": "做T卖出", "t_rebuy": "做T回补"}[order.PositionAction]
		body := fmt.Sprintf("%s · %.3f 元 × %d 股 · %s", action, order.Price, order.Quantity, at.In(realtimeWebLocation).Format("15:04:05"))
		if order.Reason != "" {
			body += "\n" + order.Reason
		} else if len(order.SignalReasons) > 0 {
			body += "\n" + order.SignalReasons[0]
		}
		messages = append(messages, notify.Message{
			Symbol: order.Symbol, Profile: profileID, Kind: order.Side,
			ID: "order:" + profileID + ":" + order.ID, Title: "ASTOCK · 模拟" + side,
			Subtitle: name + " · " + order.Name + " " + order.Symbol, Body: body, At: at,
		})
	}
	return messages
}

func notificationTime(value string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if at, err := time.ParseInLocation(layout, strings.TrimSpace(value), realtimeWebLocation); err == nil {
			return at
		}
	}
	return time.Time{}
}

func validNotificationPrice(price float64) bool {
	return price > 0 && !math.IsInf(price, 0) && !math.IsNaN(price)
}
