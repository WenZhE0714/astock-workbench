package strategy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type PlanMonitorObservation struct {
	Now                 time.Time
	Session             string
	TradingDate         string
	PreviousTradingDate string
	CalendarKnown       bool
	CalendarBasis       string
	ClosingSlot         string
	Quote               domain.Quote
	Bars                []domain.DailyBar
	Error               string
}

func PlanMonitorEnded(state domain.PlanMonitor) bool {
	return state.Phase == "invalidated" || state.Phase == "expired"
}

func ConfigurePlanMonitor(plan domain.TradePlan, current domain.PlanMonitor, enabled bool, now time.Time) (domain.PlanMonitor, error) {
	if now.IsZero() || plan.ID == "" || plan.Structure.Plan == nil || plan.Analysis.Version != "chart-v1" {
		return current, fmt.Errorf("计划快照不能用于条件监控")
	}
	if plan.CreatedAt.After(now.Add(5 * time.Second)) {
		return current, fmt.Errorf("计划创建时间晚于当前时间，请核验系统时钟")
	}
	if _, err := time.Parse(time.DateOnly, plan.ExpiresOn); err != nil {
		return current, fmt.Errorf("计划有效期无效")
	}
	state := current
	state.Events = append([]domain.PlanMonitorEvent{}, current.Events...)
	if state.Version == 0 {
		rule := domain.PlanMonitorRule{Version: "plan-monitor-v1", StructureID: plan.Structure.ID, Levels: *plan.Structure.Plan, VolumeDays: 20, MinimumVolume: 1.2, CooldownSecs: 300}
		if plan.MonitorRule != nil {
			rule = *plan.MonitorRule
			rule.Levels = *plan.Structure.Plan
		}
		switch monitorRuleKind(rule) {
		case "breakout":
			if len(plan.Structure.Anchors) == 0 || plan.Structure.Anchors[0].Price <= 0 {
				if rule.BreakoutPrice <= 0 {
					return current, fmt.Errorf("计划缺少冻结的突破锚点")
				}
			}
			if rule.BreakoutPrice <= 0 {
				rule.BreakoutPrice = plan.Structure.Anchors[0].Price
			}
			if strings.TrimSpace(rule.Description) == "" {
				rule.Description = fmt.Sprintf("完整日K收盘高于冻结锚点 %.2f，日成交量至少为此前%d日均量的%.2f倍", rule.BreakoutPrice, rule.VolumeDays, rule.MinimumVolume)
			}
		case "pullback":
			if strings.TrimSpace(rule.Description) == "" {
				prefix := ""
				if rule.RequireTrend || rule.StructureID == "ma-pullback" {
					prefix = "当期MA20高于MA60；"
					rule.RequireTrend = true
				}
				rule.Description = fmt.Sprintf("%s完整日K最低不高于冻结区间上沿 %.2f，收盘不低于冻结区间下沿 %.2f", prefix, rule.Levels.EntryHigh, rule.Levels.EntryLow)
			}
		default:
			return current, fmt.Errorf("暂不支持该计划的机器条件，请保存新版本计划")
		}
		levels := rule.Levels
		if rule.PatternReadyOn != "" {
			if _, err := time.Parse(time.DateOnly, rule.PatternReadyOn); err != nil || rule.PatternReadyOn > plan.Analysis.DataDate || rule.Kind != "breakout" {
				return current, fmt.Errorf("经典形态监控起点无效")
			}
		}
		rule.Description += fmt.Sprintf("；确认后从后续交易日起观察 %.2f - %.2f，触及 %.2f 失效", levels.EntryLow, levels.EntryHigh, levels.Invalidation)
		if !positiveMonitorPrice(levels.EntryLow) || !positiveMonitorPrice(levels.EntryHigh) || !positiveMonitorPrice(levels.Invalidation) || levels.EntryLow > levels.EntryHigh || levels.Invalidation >= levels.EntryLow {
			return current, fmt.Errorf("计划价位不满足监控条件")
		}
		state = domain.PlanMonitor{
			Version: 1, PlanID: plan.ID, Symbol: plan.Symbol, Name: plan.Structure.Name,
			AnalysisDate: plan.Analysis.DataDate, Fingerprint: plan.Analysis.Fingerprint,
			ExpiresOn: plan.ExpiresOn, Rule: rule, Phase: "waiting", DataStatus: "paused", DataMessage: "监控未启用",
			UpdatedAt: now, Events: []domain.PlanMonitorEvent{},
		}
	}
	if state.PlanID != plan.ID || state.Symbol != plan.Symbol || state.Fingerprint != plan.Analysis.Fingerprint || state.Rule.Version != "plan-monitor-v1" {
		return current, fmt.Errorf("监控与计划快照不一致")
	}
	if enabled && (PlanMonitorEnded(state) || now.In(chartLocation).Format(time.DateOnly) > plan.ExpiresOn) {
		return current, fmt.Errorf("计划已结束或过期，请保存新的观察计划")
	}
	if state.Enabled == enabled {
		return state, nil
	}
	state.Enabled = enabled
	state.UpdatedAt = now
	if enabled {
		kind := "enabled"
		if !state.EnabledAt.IsZero() {
			kind = "resumed"
		}
		state.EnabledAt = now
		state.LastClosingSlot = ""
		state.DataStatus, state.DataMessage = "waiting", "等待后台检查交易日历与行情"
		// Resuming starts a new live observation window, not a replay of ticks
		// received while paused. A previously confirmed daily setup is retained.
		if state.Phase == "in_zone" {
			state.Phase = "confirmed"
		}
		message := "已启用条件监控；仅记录观察事件，不下单"
		if kind == "resumed" {
			message = "恢复监控并复核最近完整日K，不重放暂停期间的盘中报价"
		}
		monitorEvent(&state, kind, message, now, time.Time{}, "", nil, false)
	} else {
		state.DataStatus, state.DataMessage = "paused", "已暂停，历史事件保留"
		monitorEvent(&state, "paused", "已暂停条件监控", now, time.Time{}, "", nil, false)
	}
	return state, nil
}

func AdvancePlanMonitor(current domain.PlanMonitor, observation PlanMonitorObservation) domain.PlanMonitor {
	state := current
	state.Events = append([]domain.PlanMonitorEvent{}, current.Events...)
	now := observation.Now
	if state.Version != 1 || state.Rule.Version != "plan-monitor-v1" || now.IsZero() || now.Before(state.UpdatedAt) || PlanMonitorEnded(state) {
		return state
	}
	if now.In(chartLocation).Format(time.DateOnly) > state.ExpiresOn {
		notify := state.Enabled
		state.Enabled, state.Phase, state.DataStatus, state.DataMessage = false, "expired", "ended", "计划已过有效期"
		state.UpdatedAt = now
		monitorEvent(&state, "expired", "计划已过有效期，监控结束", now, time.Time{}, "", nil, notify)
		return state
	}
	if !state.Enabled {
		return state
	}
	if (observation.Session != "trading" && observation.Session != "closing") || (observation.Session == "trading" && !monitorContinuousTime(now)) {
		if state.DataStatus == "healthy" || state.DataStatus == "waiting" || state.DataStatus == "closed" {
			monitorHealth(&state, "closed", "非监控时段，等待开盘或收盘复核", now)
		}
		return state
	}
	state.LastCheckedAt, state.UpdatedAt = now, now
	if observation.Session == "closing" {
		state.LastClosingSlot = observation.ClosingSlot
	}
	if !observation.CalendarKnown || observation.TradingDate != now.In(chartLocation).Format(time.DateOnly) || observation.PreviousTradingDate == "" || observation.PreviousTradingDate >= observation.TradingDate {
		monitorHealth(&state, "calendar_unavailable", "交易日未得到可靠确认（日历覆盖或当日报价不足），暂停条件判断", now)
		return state
	}
	state.CalendarBasis = observation.CalendarBasis
	if observation.Error != "" {
		monitorHealth(&state, "unavailable", observation.Error, now)
		return state
	}
	var quoteAt time.Time
	var price float64
	if observation.Session == "trading" {
		quoteAt = monitorQuoteTime(observation.Quote.QuoteTime)
		price, _ = strconv.ParseFloat(strings.TrimSpace(observation.Quote.Current), 64)
		if observation.Quote.Symbol != state.Symbol || !positiveMonitorPrice(price) || quoteAt.IsZero() {
			monitorHealth(&state, "unavailable", "缺少有效报价、证券标识或行情时间", now)
			return state
		}
		if quoteAt.Before(state.LastQuoteAt) {
			monitorHealth(&state, "stale_quote", "报价早于已处理水位，保留最近有效观察", now)
			return state
		}
		if !quoteAt.Equal(state.LastQuoteAt) {
			state.QuoteAt = quoteAt
			state.Price = chartNumber(price)
		}
		if quoteAt.In(chartLocation).Format(time.DateOnly) != observation.TradingDate || now.Sub(quoteAt) > 2*time.Minute || quoteAt.After(now.Add(5*time.Second)) || !monitorContinuousTime(quoteAt) {
			monitorHealth(&state, "stale_quote", "报价日期或时间不满足连续竞价新鲜度要求（最多2分钟）", now)
			return state
		}
		if quoteAt.Before(state.EnabledAt) {
			monitorHealth(&state, "waiting", "等待启用或恢复之后的新报价", now)
			return state
		}
	}
	expected := observation.PreviousTradingDate
	if observation.Session == "closing" {
		expected = observation.TradingDate
	}
	bars := monitorCompletedBars(state.Symbol, observation.Bars, expected)
	state.HistoryDate = ""
	if len(bars) > 0 {
		state.HistoryDate = bars[len(bars)-1].Date
	}
	requiredBars := state.Rule.VolumeDays + 1
	if monitorRuleKind(state.Rule) == "pullback" {
		requiredBars = 1
		if monitorRuleRequiresTrend(state.Rule) {
			requiredBars = 60
		}
	}
	if len(bars) < requiredBars || bars[len(bars)-1].Date != expected {
		monitorHealth(&state, "stale_history", fmt.Sprintf("完整日K需截至 %s 且至少%d根，当前截至 %s；暂停条件判断", expected, requiredBars, state.HistoryDate), now)
		return state
	}
	if observation.Session == "trading" {
		previousClose, _ := strconv.ParseFloat(strings.TrimSpace(observation.Quote.PreviousClose), 64)
		if !positiveMonitorPrice(previousClose) || math.Abs(previousClose-bars[len(bars)-1].Close) > .011 {
			monitorHealth(&state, "price_basis_mismatch", "报价昨收缺失或与完整日K收盘不一致，需核验日K更新或除权口径", now)
			return state
		}
	}
	monitorHealth(&state, "healthy", "行情与完整日K已通过新鲜度校验", now)
	latest := bars[len(bars)-1]
	data, err := json.Marshal(bars)
	if err != nil {
		monitorHealth(&state, "unavailable", "日K包含无效数值，暂停条件判断", now)
		return state
	}
	digest := sha256.Sum256(data)
	fingerprint := hex.EncodeToString(digest[:])
	if latest.Date > state.LastBarDate || (latest.Date == state.LastBarDate && fingerprint != state.LastBarFingerprint) {
		state.LastBarDate, state.LastBarFingerprint = latest.Date, fingerprint
		if state.Rule.PatternReadyOn != "" {
			if !advanceClassicMonitorDaily(&state, bars, now) {
				return state
			}
		} else if state.ConfirmedOn == "" && latest.Date >= state.AnalysisDate && monitorDailyConfirmation(state.Rule, bars) {
			state.Phase, state.ConfirmedOn = "confirmed", latest.Date
			monitorEvent(&state, "confirmed", "复核完整日K后确认计划条件；后续交易日才观察入场区间", now, time.Time{}, latest.Date, chartNumber(latest.Close), true)
		} else if state.ConfirmedOn != "" && latest.Date > state.ConfirmedOn && latest.Low <= state.Rule.Levels.Invalidation {
			invalidateMonitor(&state, now, time.Time{}, latest.Date, latest.Low, "确认后的完整日K最低价触及失效位；具体盘中时刻未知")
			return state
		}
	}
	if observation.Session != "trading" || !quoteAt.After(state.LastQuoteAt) {
		return state
	}
	state.LastQuoteAt = quoteAt
	if state.Rule.PatternReadyOn != "" && price <= state.Rule.Levels.Invalidation {
		invalidateMonitor(&state, now, quoteAt, observation.TradingDate, price, "盘中新报价触及经典形态冻结失效位")
		return state
	}
	if state.ConfirmedOn == "" || observation.TradingDate <= state.ConfirmedOn {
		return state
	}
	if price <= state.Rule.Levels.Invalidation {
		invalidateMonitor(&state, now, quoteAt, observation.TradingDate, price, "确认后的新报价触及计划失效位")
		return state
	}
	inZone := price >= state.Rule.Levels.EntryLow && price <= state.Rule.Levels.EntryHigh
	wasInZone := state.Phase == "in_zone"
	if inZone {
		state.Phase = "in_zone"
		if !wasInZone && (state.LastZoneAlertAt.IsZero() || now.Sub(state.LastZoneAlertAt) >= time.Duration(state.Rule.CooldownSecs)*time.Second) {
			state.LastZoneAlertAt = now
			monitorEvent(&state, "zone_entered", "新报价进入冻结的入场观察区间；这不是成交记录", now, quoteAt, observation.TradingDate, chartNumber(price), true)
		}
	} else {
		state.Phase = "confirmed"
	}
	return state
}

func advanceClassicMonitorDaily(state *domain.PlanMonitor, bars []domain.DailyBar, now time.Time) bool {
	startDate := state.Rule.PatternReadyOn
	if state.ConfirmedOn != "" {
		startDate = state.ConfirmedOn
	}
	start := -1
	for index, bar := range bars {
		if bar.Date == startDate {
			start = index
			break
		}
	}
	if start < 0 || (state.ConfirmedOn == "" && start < state.Rule.VolumeDays) {
		monitorHealth(state, "stale_history", "完整日K未覆盖形态起点和量比预热区间，暂停条件判断", now)
		// Re-evaluate the same latest date if a fuller history arrives later.
		state.LastBarFingerprint = ""
		return false
	}
	confirmed := state.ConfirmedOn
	if confirmed != "" {
		start++
	}
	for index := start; index < len(bars); index++ {
		bar := bars[index]
		if bar.Low <= state.Rule.Levels.Invalidation {
			invalidateMonitor(state, now, time.Time{}, bar.Date, bar.Low, "形态可识别后的完整日K触及冻结失效位；停止确认该计划")
			return false
		}
		if confirmed == "" && monitorDailyConfirmation(state.Rule, bars[:index+1]) {
			confirmed = bar.Date
		}
	}
	if state.ConfirmedOn == "" && confirmed != "" {
		state.Phase, state.ConfirmedOn = "confirmed", confirmed
		monitorEvent(state, "confirmed", "复核形态起点后的完整日K，确认冻结突破条件；不补记历史成交", now, time.Time{}, confirmed, chartNumber(state.Rule.BreakoutPrice), true)
	}
	return true
}

func monitorCompletedBars(symbol string, input []domain.DailyBar, through string) []domain.DailyBar {
	bars := make([]domain.DailyBar, 0, len(input))
	for _, bar := range input {
		if _, err := time.Parse(time.DateOnly, bar.Date); err != nil {
			continue
		}
		if bar.Date <= through && (bar.Symbol == "" || bar.Symbol == symbol) && validBar(bar) && bar.High >= math.Max(bar.Open, bar.Close) && bar.Low <= math.Min(bar.Open, bar.Close) {
			bars = append(bars, bar)
		}
	}
	return normalizedBars(bars)
}

func monitorDailyConfirmation(rule domain.PlanMonitorRule, bars []domain.DailyBar) bool {
	latest := bars[len(bars)-1]
	switch monitorRuleKind(rule) {
	case "breakout":
		if rule.PatternReadyOn != "" && latest.Date < rule.PatternReadyOn {
			return false
		}
		if latest.Close <= rule.BreakoutPrice || len(bars) <= rule.VolumeDays {
			return false
		}
		volume := 0.0
		for _, bar := range bars[len(bars)-rule.VolumeDays-1 : len(bars)-1] {
			if rule.PatternReadyOn != "" && !positiveMonitorPrice(bar.Volume) {
				return false
			}
			volume += bar.Volume
		}
		return volume > 0 && latest.Volume/(volume/float64(rule.VolumeDays)) >= rule.MinimumVolume
	case "pullback":
		if monitorRuleRequiresTrend(rule) && len(bars) < 60 {
			return false
		}
		trendOK := true
		if monitorRuleRequiresTrend(rule) {
			values := closes(bars)
			trendOK = average(values[len(values)-20:]) > average(values[len(values)-60:])
		}
		return trendOK && latest.Low <= rule.Levels.EntryHigh && latest.Close >= rule.Levels.EntryLow
	}
	return false
}

func monitorRuleKind(rule domain.PlanMonitorRule) string {
	if kind := strings.ToLower(strings.TrimSpace(rule.Kind)); kind != "" {
		return kind
	}
	switch rule.StructureID {
	case "range-breakout", "compression-breakout", "assistant-breakout":
		return "breakout"
	case "ma-pullback", "assistant-pullback":
		return "pullback"
	default:
		return ""
	}
}

func monitorRuleRequiresTrend(rule domain.PlanMonitorRule) bool {
	return rule.RequireTrend || (strings.TrimSpace(rule.Kind) == "" && rule.StructureID == "ma-pullback")
}

func monitorQuoteTime(value string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "20060102150405"} {
		if parsed, err := time.ParseInLocation(layout, strings.TrimSpace(value), chartLocation); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func monitorContinuousTime(at time.Time) bool {
	local := at.In(chartLocation)
	minutes := local.Hour()*60 + local.Minute()
	return (minutes >= 570 && minutes < 690) || (minutes >= 780 && minutes < 900)
}

// MonitorQuoteConfirmsSession attests an uncovered current trading day from
// an actual quote timestamp, never from the computer clock or weekday alone.
func MonitorQuoteConfirmsSession(quote domain.Quote, now time.Time, session string) bool {
	at := monitorQuoteTime(quote.QuoteTime)
	price, err := strconv.ParseFloat(strings.TrimSpace(quote.Current), 64)
	if err != nil || !positiveMonitorPrice(price) || at.IsZero() || at.In(chartLocation).Format(time.DateOnly) != now.In(chartLocation).Format(time.DateOnly) || at.After(now.Add(5*time.Second)) {
		return false
	}
	if session == "trading" {
		return monitorContinuousTime(now) && monitorContinuousTime(at) && now.Sub(at) <= 2*time.Minute
	}
	if session == "closing" {
		return now.In(chartLocation).Hour() >= 15 && at.In(chartLocation).Hour() >= 15
	}
	return false
}

func positiveMonitorPrice(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func invalidateMonitor(state *domain.PlanMonitor, now, quoteAt time.Time, date string, price float64, message string) {
	state.Enabled, state.Phase, state.DataStatus, state.DataMessage = false, "invalidated", "ended", "计划结构失效，监控结束"
	monitorEvent(state, "invalidated", message, now, quoteAt, date, chartNumber(price), true)
}

func monitorHealth(state *domain.PlanMonitor, status, message string, now time.Time) {
	if state.DataStatus == status && state.DataMessage == message {
		return
	}
	previous := state.DataStatus
	state.DataStatus, state.DataMessage, state.UpdatedAt = status, message, now
	if status != "healthy" && status != "waiting" && status != "closed" {
		if previous != status {
			monitorEvent(state, "data_wait", message, now, time.Time{}, state.HistoryDate, nil, false)
		}
	} else if status == "healthy" && previous != "healthy" && previous != "closed" && previous != "waiting" {
		monitorEvent(state, "data_recovered", "行情恢复有效，继续条件监控", now, time.Time{}, state.HistoryDate, nil, false)
	}
}

func monitorEvent(state *domain.PlanMonitor, kind, message string, now, quoteAt time.Time, date string, price *float64, notify bool) {
	state.Sequence++
	state.Events = append(state.Events, domain.PlanMonitorEvent{
		ID: fmt.Sprintf("plan:%s:%d", state.PlanID, state.Sequence), Sequence: state.Sequence,
		Kind: kind, Message: message, ObservedAt: now, QuoteAt: quoteAt, DataDate: date, Price: price, Notify: notify,
	})
}
