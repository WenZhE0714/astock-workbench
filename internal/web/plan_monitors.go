package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type planMonitorArchive interface {
	List(string) ([]domain.PlanMonitor, error)
	Load(string) (domain.PlanMonitor, error)
	Update(string, func(domain.PlanMonitor) (domain.PlanMonitor, error)) (domain.PlanMonitor, error)
}

type planMonitorRuntime struct {
	Supported       bool      `json:"supported"`
	Running         bool      `json:"running"`
	Checking        bool      `json:"checking"`
	Session         string    `json:"session"`
	IntervalSeconds int       `json:"interval_seconds"`
	Today           string    `json:"today"`
	LastCycleAt     time.Time `json:"last_cycle_at,omitzero"`
	LastError       string    `json:"last_error,omitempty"`
}

func WithPlanMonitors(store planMonitorArchive) ServerOption {
	return func(server *Server) { server.planMonitors = store }
}

func (s *Server) monitorRuntime() planMonitorRuntime {
	s.planMonitorMu.Lock()
	defer s.planMonitorMu.Unlock()
	result := s.planMonitorRuntime
	result.Supported = s.planMonitors != nil && s.tradePlans != nil
	result.IntervalSeconds = 30
	result.Today = s.currentTime().In(realtimeWebLocation).Format(time.DateOnly)
	result.Session, _ = planMonitorWindow(s.currentTime())
	return result
}

func planMonitorWindow(now time.Time) (string, string) {
	local := now.In(realtimeWebLocation)
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
		return "closed", ""
	}
	minute := local.Hour()*60 + local.Minute()
	if (minute >= 570 && minute < 690) || (minute >= 780 && minute < 900) {
		return "trading", ""
	}
	date := local.Format(time.DateOnly)
	if minute >= 910 && minute < 965 {
		return "closing", date + ":primary"
	}
	if minute >= 965 && minute < 1080 {
		return "closing", date + ":retry"
	}
	return "closed", ""
}

func adjacentCalendarDate(dates []string, now time.Time) string {
	if len(dates) == 0 {
		return ""
	}
	previous := now.In(realtimeWebLocation).AddDate(0, 0, -1)
	for previous.Weekday() == time.Saturday || previous.Weekday() == time.Sunday {
		previous = previous.AddDate(0, 0, -1)
	}
	date := previous.Format(time.DateOnly)
	if dates[len(dates)-1] != date {
		return ""
	}
	return date
}

func (s *Server) runPlanMonitorLoop(ctx context.Context) {
	if s.planMonitors == nil || s.tradePlans == nil {
		return
	}
	s.planMonitorMu.Lock()
	if s.planMonitorRuntime.Running {
		s.planMonitorMu.Unlock()
		return
	}
	s.planMonitorRuntime.Running = true
	s.planMonitorMu.Unlock()
	defer func() {
		s.planMonitorMu.Lock()
		s.planMonitorRuntime.Running = false
		s.planMonitorMu.Unlock()
	}()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	s.runPlanWorkCycle(ctx, false)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runPlanWorkCycle(ctx, false)
		case <-s.planMonitorWake:
			s.runPlanWorkCycle(ctx, true)
		}
	}
}

func (s *Server) runPlanWorkCycle(ctx context.Context, force bool) {
	s.runPlanMonitorCycle(ctx, force)
	s.runPlanExperimentCycle(ctx)
}

func (s *Server) wakePlanMonitors() {
	select {
	case s.planMonitorWake <- struct{}{}:
	default:
	}
}

func (s *Server) runPlanMonitorCycle(parent context.Context, force bool) {
	if s.planMonitors == nil || s.tradePlans == nil || !s.planMonitorCycleMu.TryLock() {
		return
	}
	defer s.planMonitorCycleMu.Unlock()
	s.planMonitorMu.Lock()
	s.planMonitorRuntime.Checking = true
	s.planMonitorMu.Unlock()
	var cycleErr error
	defer func() {
		s.planMonitorMu.Lock()
		s.planMonitorRuntime.Checking = false
		s.planMonitorRuntime.LastCycleAt = s.currentTime()
		s.planMonitorRuntime.LastError = ""
		if cycleErr != nil {
			s.planMonitorRuntime.LastError = cycleErr.Error()
		}
		s.planMonitorMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(parent, 40*time.Second)
	defer cancel()
	states, err := s.planMonitors.List("")
	if err != nil {
		cycleErr = err
		return
	}
	now := s.currentTime()
	session, slot := planMonitorWindow(now)
	groups := make(map[string][]domain.PlanMonitor)
	for _, state := range states {
		if strategy.PlanMonitorEnded(state) {
			continue
		}
		if now.In(realtimeWebLocation).Format(time.DateOnly) > state.ExpiresOn || !state.Enabled || session == "closed" {
			_, err := s.planMonitors.Update(state.PlanID, func(current domain.PlanMonitor) (domain.PlanMonitor, error) {
				return strategy.AdvancePlanMonitor(current, strategy.PlanMonitorObservation{Now: now, Session: "closed"}), nil
			})
			if err != nil {
				cycleErr = err
			}
			continue
		}
		if session == "closing" && state.LastClosingSlot == slot && !force {
			continue
		}
		groups[state.Symbol] = append(groups[state.Symbol], state)
	}
	if len(groups) == 0 {
		return
	}
	dates, calendarErr := s.tradingCalendar(ctx, now)
	marketSession := realtime.MarketSessionAtWithCalendar(now, dates)
	base := strategy.PlanMonitorObservation{Now: now, Session: session, ClosingSlot: slot, TradingDate: marketSession.TradingDate, CalendarKnown: calendarErr == nil && marketSession.CalendarKnown, CalendarBasis: "交易日历"}
	if base.CalendarKnown && !marketSession.TradingDay {
		base.Session = "closed"
	}
	index := sort.SearchStrings(dates, marketSession.TradingDate)
	if index > 0 && index < len(dates) && dates[index] == marketSession.TradingDate {
		base.PreviousTradingDate = dates[index-1]
		base.CalendarBasis = fmt.Sprintf("交易日历 %s；前序交易日 %s", base.TradingDate, base.PreviousTradingDate)
	}
	needsQuoteCalendar := !marketSession.CalendarKnown && calendarErr == nil && adjacentCalendarDate(dates, now) != ""
	if needsQuoteCalendar {
		base.PreviousTradingDate = adjacentCalendarDate(dates, now)
		base.CalendarBasis = fmt.Sprintf("%s 有效报价确认；前序交易日 %s", base.TradingDate, base.PreviousTradingDate)
	}
	if (!base.CalendarKnown && !needsQuoteCalendar) || base.PreviousTradingDate == "" || base.Session == "closed" {
		for _, group := range groups {
			for _, state := range group {
				_, err := s.planMonitors.Update(state.PlanID, func(current domain.PlanMonitor) (domain.PlanMonitor, error) {
					return strategy.AdvancePlanMonitor(current, base), nil
				})
				if err != nil {
					cycleErr = err
				}
			}
		}
		return
	}
	symbols := make([]string, 0, len(groups))
	for symbol := range groups {
		symbols = append(symbols, symbol)
	}
	// Service symbols that have waited longest first when providers are slow.
	sort.Slice(symbols, func(i, j int) bool {
		return groups[symbols[i]][0].LastCheckedAt.Before(groups[symbols[j]][0].LastCheckedAt)
	})
	quotes := make(map[string]domain.Quote)
	var quoteErr error
	if session == "trading" || needsQuoteCalendar {
		quoteCtx, cancelQuote := context.WithTimeout(ctx, 10*time.Second)
		quotes, quoteErr = s.fetchQuoteBatch(quoteCtx, symbols)
		cancelQuote()
	}
	jobs := make(chan string, len(symbols))
	for _, symbol := range symbols {
		jobs <- symbol
	}
	close(jobs)
	errors := make(chan error, len(states))
	var workers sync.WaitGroup
	for range min(4, len(symbols)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for symbol := range jobs {
				if ctx.Err() != nil {
					return
				}
				observation := base
				observation.Quote = quotes[symbol]
				if needsQuoteCalendar {
					observation.CalendarKnown = quoteErr == nil && strategy.MonitorQuoteConfirmsSession(observation.Quote, s.currentTime(), session)
				}
				if quoteErr != nil {
					observation.Error = "报价暂不可用: " + quoteErr.Error()
				} else if !observation.CalendarKnown {
					observation.Error = "当日报价未能确认交易日"
				} else if s.history == nil {
					observation.Error = "日K服务未初始化"
				} else {
					historyCtx, cancelHistory := context.WithTimeout(ctx, 10*time.Second)
					bars, historyErr := s.fetchDailyBars(historyCtx, symbol)
					cancelHistory()
					observation.Bars = bars
					if historyErr != nil {
						observation.Error = "日K暂不可用: " + historyErr.Error()
					}
				}
				observation.Now = s.currentTime()
				if needsQuoteCalendar {
					observation.CalendarKnown = quoteErr == nil && strategy.MonitorQuoteConfirmsSession(observation.Quote, observation.Now, session)
				}
				for _, state := range groups[symbol] {
					plan, err := s.tradePlans.Load(symbol, state.PlanID)
					if err != nil {
						errors <- err
						continue
					}
					_, err = s.planMonitors.Update(state.PlanID, func(current domain.PlanMonitor) (domain.PlanMonitor, error) {
						if current.Fingerprint != plan.Analysis.Fingerprint {
							return current, fmt.Errorf("监控与原始计划不一致")
						}
						return strategy.AdvancePlanMonitor(current, observation), nil
					})
					if err != nil {
						errors <- err
					}
				}
			}
		}()
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		cycleErr = err
	}
}

func (s *Server) handlePlanMonitors(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		states := []domain.PlanMonitor{}
		planID := strings.TrimSpace(request.URL.Query().Get("plan_id"))
		download := request.URL.Query().Get("download") == "1" && planID != ""
		if s.planMonitors != nil {
			symbol := strings.TrimSpace(request.URL.Query().Get("symbol"))
			if symbol != "" && s.resolver != nil {
				ctx, cancel := context.WithTimeout(request.Context(), 8*time.Second)
				resolved, err := s.resolver.Resolve(ctx, symbol)
				cancel()
				if err != nil || !supportsStockPlan(resolved) {
					writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "监控股票代码无效"})
					return
				}
				symbol = resolved
			}
			var err error
			if planID == "" {
				states, err = s.planMonitors.List(symbol)
			} else {
				state, loadErr := s.planMonitors.Load(planID)
				if loadErr != nil || state.Version == 0 || (symbol != "" && state.Symbol != symbol) {
					writeJSON(writer, http.StatusNotFound, errorResponse{Error: "未找到计划监控"})
					return
				}
				states = append(states, state)
			}
			if err != nil {
				writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: err.Error()})
				return
			}
		}
		for index := range states {
			if !download && len(states[index].Events) > 30 {
				states[index].Events = states[index].Events[len(states[index].Events)-30:]
			}
		}
		if download && len(states) == 1 {
			writer.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="plan-monitor-%s.json"`, states[0].PlanID[:8]))
		}
		writeJSON(writer, http.StatusOK, struct {
			Items   []domain.PlanMonitor `json:"items"`
			Runtime planMonitorRuntime   `json:"runtime"`
		}{states, s.monitorRuntime()})
		return
	}
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "只支持 GET / POST"})
		return
	}
	if s.planMonitors == nil || s.tradePlans == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "计划监控存储未配置"})
		return
	}
	var input struct {
		Symbol  string `json:"symbol"`
		PlanID  string `json:"plan_id"`
		Enabled *bool  `json:"enabled"`
	}
	if !decodePlanMonitorMutation(writer, request, &input) {
		return
	}
	if action := request.URL.Query().Get("action"); action != "" && action != "check" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "不支持的监控操作"})
		return
	}
	if request.URL.Query().Get("action") == "check" {
		if !s.monitorRuntime().Running {
			writeJSON(writer, http.StatusConflict, errorResponse{Error: "后台监控调度未运行"})
			return
		}
		s.wakePlanMonitors()
		writeJSON(writer, http.StatusAccepted, s.monitorRuntime())
		return
	}
	if input.Enabled == nil || !supportsStockPlan(input.Symbol) || input.PlanID == "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "缺少股票、计划标识或监控开关"})
		return
	}
	plan, err := s.tradePlans.Load(input.Symbol, input.PlanID)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "读取原始计划失败: " + err.Error()})
		return
	}
	state, err := s.planMonitors.Update(plan.ID, func(current domain.PlanMonitor) (domain.PlanMonitor, error) {
		return strategy.ConfigurePlanMonitor(plan, current, *input.Enabled, s.currentTime())
	})
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	s.wakePlanMonitors()
	writeJSON(writer, http.StatusOK, state)
}

func decodePlanMonitorMutation(writer http.ResponseWriter, request *http.Request, input any) bool {
	contentType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if contentType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, errorResponse{Error: "监控请求须使用 application/json"})
		return false
	}
	if origin := request.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host != request.Host || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			writeJSON(writer, http.StatusForbidden, errorResponse{Error: "拒绝跨站修改监控"})
			return false
		}
	}
	if request.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSON(writer, http.StatusForbidden, errorResponse{Error: "拒绝跨站修改监控"})
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "监控请求格式无效"})
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "监控请求包含多余内容"})
		return false
	}
	return true
}

func (s *Server) planMonitorAlerts(symbol string) []assistantAlert {
	if s.planMonitors == nil {
		return nil
	}
	states, err := s.planMonitors.List(symbol)
	if err != nil {
		return nil
	}
	alerts := make([]assistantAlert, 0)
	now := s.currentTime()
	for _, state := range states {
		for index := len(state.Events) - 1; index >= 0; index-- {
			event := state.Events[index]
			if !event.Notify || now.Sub(event.ObservedAt) > 24*time.Hour || event.ObservedAt.After(now) {
				continue
			}
			severity := "medium"
			if event.Kind == "invalidated" {
				severity = "high"
			}
			price := 0.0
			if event.Price != nil {
				price = *event.Price
			}
			alerts = append(alerts, assistantAlert{ID: event.ID, Kind: "plan-monitor", Severity: severity, Symbol: state.Symbol, Title: state.Name + " · 计划监控", Detail: event.Message, Price: price, AsOf: event.ObservedAt, DataDate: event.DataDate})
		}
	}
	return sortAssistantAlerts(alerts, 24)
}
