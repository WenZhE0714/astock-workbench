package web

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type planExperimentArchive interface {
	Load() (paper.PlanExperiment, error)
	Update(func(paper.PlanExperiment) (paper.PlanExperiment, error)) (paper.PlanExperiment, error)
}

type planExperimentRuntime struct {
	Supported   bool      `json:"supported"`
	Running     bool      `json:"running"`
	Checking    bool      `json:"checking"`
	LastCheckAt time.Time `json:"last_check_at,omitzero"`
	Error       string    `json:"error,omitempty"`
}

func WithPlanExperiment(store planExperimentArchive) ServerOption {
	return func(server *Server) { server.planExperiment = store }
}

func (s *Server) experimentRuntime() planExperimentRuntime {
	s.planExperimentMu.Lock()
	result := s.planExperimentRuntime
	s.planExperimentMu.Unlock()
	result.Supported = s.planExperiment != nil && s.planMonitors != nil && s.tradePlans != nil
	result.Running = s.monitorRuntime().Running
	return result
}

func (s *Server) handlePlanExperiment(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		var state *paper.PlanExperiment
		if s.planExperiment != nil {
			loaded, err := s.planExperiment.Load()
			if err != nil {
				writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: err.Error()})
				return
			}
			if loaded.Version != "" {
				state = &loaded
			}
		}
		if request.URL.Query().Get("download") == "1" {
			writer.Header().Set("Content-Disposition", `attachment; filename="plan-experiment.json"`)
		} else if state != nil {
			if len(state.Equity) > 720 {
				state.Equity = state.Equity[len(state.Equity)-720:]
			}
			for index := range state.Arms {
				report := &state.Arms[index].Report
				if len(report.Orders) > 100 {
					report.Orders = report.Orders[len(report.Orders)-100:]
				}
				if len(report.Trades) > 100 {
					report.Trades = report.Trades[len(report.Trades)-100:]
				}
				if len(report.Rejections) > 100 {
					report.Rejections = report.Rejections[len(report.Rejections)-100:]
				}
			}
		}
		writeJSON(writer, http.StatusOK, struct {
			State   *paper.PlanExperiment `json:"state,omitempty"`
			Runtime planExperimentRuntime `json:"runtime"`
			Config  paper.Config          `json:"config"`
		}{state, s.experimentRuntime(), paper.PlanExperimentConfig()})
		return
	}
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "只支持 GET / POST"})
		return
	}
	if s.planExperiment == nil || s.tradePlans == nil || s.planMonitors == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "计划实验存储未配置"})
		return
	}
	var input struct {
		Action  string `json:"action"`
		Enabled *bool  `json:"enabled"`
		Symbol  string `json:"symbol"`
		PlanID  string `json:"plan_id"`
	}
	if !decodePlanMonitorMutation(writer, request, &input) {
		return
	}
	if input.Enabled == nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "缺少明确的实验开关"})
		return
	}
	var plan domain.TradePlan
	if input.Action == "select" {
		if !supportsStockPlan(input.Symbol) {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "实验仅支持A股个股计划"})
			return
		}
		var err error
		plan, err = s.tradePlans.Load(input.Symbol, input.PlanID)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		if *input.Enabled {
			monitor, err := s.planMonitors.Load(plan.ID)
			if err != nil || !monitor.Enabled || monitor.Fingerprint != plan.Analysis.Fingerprint {
				writeJSON(writer, http.StatusConflict, errorResponse{Error: "计划监控未启用或与快照不一致"})
				return
			}
		}
	} else if input.Action != "entries" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "不支持的实验操作"})
		return
	}
	state, err := s.planExperiment.Update(func(current paper.PlanExperiment) (paper.PlanExperiment, error) {
		if input.Action == "entries" {
			return paper.ConfigurePlanExperiment(current, *input.Enabled, s.currentTime())
		}
		return paper.SelectPlanExperiment(current, plan, *input.Enabled, s.currentTime())
	})
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	s.wakePlanMonitors()
	writeJSON(writer, http.StatusOK, state)
}

// Both experiment arms receive one immutable input batch. Only their entry
// predicate differs; no baseline shadow archive is read or written here.
func (s *Server) runPlanExperimentCycle(parent context.Context) {
	if s.planExperiment == nil || s.planMonitors == nil || !s.planExperimentCycleMu.TryLock() {
		return
	}
	defer s.planExperimentCycleMu.Unlock()
	s.planExperimentMu.Lock()
	s.planExperimentRuntime.Checking = true
	s.planExperimentMu.Unlock()
	var cycleErr error
	defer func() {
		s.planExperimentMu.Lock()
		defer s.planExperimentMu.Unlock()
		s.planExperimentRuntime.Checking = false
		s.planExperimentRuntime.LastCheckAt = s.currentTime()
		s.planExperimentRuntime.Error = ""
		if cycleErr != nil {
			s.planExperimentRuntime.Error = cycleErr.Error()
		}
	}()
	state, err := s.planExperiment.Load()
	if err != nil {
		cycleErr = err
		return
	}
	if state.Version == "" {
		return
	}
	now := s.currentTime()
	mode, slot := planMonitorWindow(now)
	if !paper.PlanExperimentNeedsQuotes(state) {
		return
	}
	if mode == "closed" {
		_, cycleErr = s.planExperiment.Update(func(current paper.PlanExperiment) (paper.PlanExperiment, error) {
			return paper.AdvancePlanExperiment(current, nil, s.currentTime(), "closed")
		})
		return
	}
	if mode == "closing" && !state.ValuedAt.IsZero() {
		_, previousSlot := planMonitorWindow(state.ValuedAt)
		if previousSlot == slot {
			return
		}
	}
	ctx, cancel := context.WithTimeout(parent, 35*time.Second)
	defer cancel()
	monitors, err := s.planMonitors.List("")
	if err != nil {
		cycleErr = err
		monitors = nil
	}
	byPlan := make(map[string]domain.PlanMonitor)
	for _, monitor := range monitors {
		byPlan[monitor.PlanID] = monitor
	}
	wanted := make(map[string]bool)
	needHistory := make(map[string]bool)
	for id, selection := range state.Selections {
		if state.EntriesEnabled && selection.Enabled && byPlan[id].Enabled {
			wanted[selection.Plan.Symbol] = true
			if byPlan[id].DataStatus == "healthy" {
				needHistory[selection.Plan.Symbol] = true
			}
		}
	}
	for _, arm := range state.Arms {
		for _, position := range arm.Report.Positions {
			wanted[position.Symbol] = true
		}
	}
	if len(wanted) == 0 {
		return
	}
	symbols := make([]string, 0, len(wanted))
	for symbol := range wanted {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	dates, calendarErr := s.tradingCalendar(ctx, now)
	session := realtime.MarketSessionAtWithCalendar(now, dates)
	if calendarErr == nil && session.CalendarKnown && !session.TradingDay {
		_, cycleErr = s.planExperiment.Update(func(current paper.PlanExperiment) (paper.PlanExperiment, error) {
			return paper.AdvancePlanExperiment(current, nil, s.currentTime(), "closed")
		})
		return
	}
	quoteCtx, cancelQuotes := context.WithTimeout(ctx, 10*time.Second)
	quotes, quoteErr := s.fetchQuoteBatch(quoteCtx, symbols)
	cancelQuotes()
	inputs := make([]paper.PlanExperimentInput, len(symbols))
	jobs := make(chan int, len(symbols))
	for index := range symbols {
		jobs <- index
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(4, len(symbols)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				symbol := symbols[index]
				quote := quotes[symbol]
				input := paper.PlanExperimentInput{Symbol: symbol, Monitors: byPlan, CalendarDates: dates}
				input.Qualified = quoteErr == nil && quote.Symbol == symbol && strategy.MonitorQuoteConfirmsSession(quote, s.currentTime(), mode)
				input.Quote = experimentPositionQuote(quote)
				if input.Qualified && (calendarErr != nil || !session.CalendarKnown) {
					input.CalendarDates = realtime.NormalizeTradingDates(append(append([]string{}, dates...), session.TradingDate))
				}
				if quoteErr != nil {
					input.Error = quoteErr.Error()
				}
				if input.Qualified && mode == "trading" && needHistory[symbol] && s.history != nil {
					historyCtx, stop := context.WithTimeout(ctx, 10*time.Second)
					bars, err := s.fetchDailyBars(historyCtx, symbol)
					stop()
					input.Bars = bars
					if err != nil {
						input.Error = err.Error()
					}
				}
				inputs[index] = input
			}
		}()
	}
	workers.Wait()
	// A control may change while history is loading. Re-read monitoring state
	// before committing entries; fixed position exits do not depend on it.
	latest, monitorErr := s.planMonitors.List("")
	latestByPlan := make(map[string]domain.PlanMonitor)
	if monitorErr == nil {
		for _, monitor := range latest {
			latestByPlan[monitor.PlanID] = monitor
		}
	}
	for index := range inputs {
		inputs[index].Monitors = latestByPlan
	}
	_, cycleErr = s.planExperiment.Update(func(current paper.PlanExperiment) (paper.PlanExperiment, error) {
		return paper.AdvancePlanExperiment(current, inputs, s.currentTime(), mode)
	})
	if cycleErr == nil && monitorErr != nil {
		cycleErr = monitorErr
	}
}

func experimentPositionQuote(quote domain.Quote) paper.PositionQuote {
	stamp := ""
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "20060102150405"} {
		if at, err := time.ParseInLocation(layout, strings.TrimSpace(quote.QuoteTime), realtimeWebLocation); err == nil {
			stamp = at.In(realtimeWebLocation).Format("2006-01-02 15:04:05")
			break
		}
	}
	return paper.PositionQuote{Symbol: quote.Symbol, Price: parseQuoteFloat(quote.Current), PreviousClose: parseQuoteFloat(quote.PreviousClose), Open: parseQuoteFloat(quote.Open), High: parseQuoteFloat(quote.High), Low: parseQuoteFloat(quote.Low), LimitUp: parseQuoteFloat(quote.LimitUp), LimitDown: parseQuoteFloat(quote.LimitDown), Volume: quote.Volume, Amount: quote.Amount, QuoteTime: stamp, Source: quote.Source}
}
