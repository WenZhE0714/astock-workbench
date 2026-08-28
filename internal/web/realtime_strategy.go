package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

const realtimeScanTimeout = 90 * time.Second
const realtimeOutcomeTimeout = 90 * time.Second
const realtimeSectorEnrichTimeout = 25 * time.Second

const (
	realtimeCalibrationActiveStatus   = "challenger-active"
	realtimeCalibrationRollbackStatus = "challenger-rolled-back"
)

var realtimeWebLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type realtimeStrategyResponse struct {
	Result        *realtime.ScanResult    `json:"result,omitempty"`
	History       []realtime.Signal       `json:"history,omitempty"`
	Report        *realtime.OutcomeReport `json:"report,omitempty"`
	MarketState   string                  `json:"market_state,omitempty"`
	TradingDay    *bool                   `json:"trading_day,omitempty"`
	CalendarKnown *bool                   `json:"calendar_known,omitempty"`
	ScanAllowed   *bool                   `json:"scan_allowed,omitempty"`
	Frozen        *bool                   `json:"frozen,omitempty"`
	NextScanAt    string                  `json:"next_scan_at,omitempty"`
	Cached        bool                    `json:"cached,omitempty"`
	Automation    AutomationStatus        `json:"automation"`
}

func (s *Server) handleRealtimeStrategy(writer http.ResponseWriter, request *http.Request) {
	view := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("view")))
	if view == "outcomes" || view == "performance" {
		s.writeRealtimeOutcomes(writer, request)
		return
	}
	if s.realtimeScanner == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "实时量化扫描服务未初始化"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		s.writeRealtimeStrategy(writer, request)
	case http.MethodPost:
		s.runRealtimeStrategy(writer, request)
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "实时量化扫描只支持 GET、POST"})
	}
}

func (s *Server) writeRealtimeOutcomes(writer http.ResponseWriter, request *http.Request) {
	if s.realtimeOutcomes == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "实时信号结果评估服务未初始化"})
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "实时结果评估只支持 GET、POST"})
		return
	}
	limit := 500
	fullArchive := outcomeFullArchiveRequested(request)
	if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || (value < 1 && value != -1) || value > 5000 {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "评估信号数量必须在 1 到 5000 之间"})
			return
		}
		limit = value
	}
	if fullArchive {
		// A negative limit is the explicit full-archive sentinel understood by
		// both the signal and outcome stores. It prevents older 5/10-day rows
		// from being crowded out as the intraday archive grows.
		limit = -1
	}
	if request.Method == http.MethodGet {
		report, err := s.realtimeOutcomes.Report(limit, s.currentTime())
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取实时评估结果失败: " + err.Error()})
			return
		}
		// A bounded/read-only report is useful for the UI, but it is not a valid
		// calibration dataset. Only an explicit full-archive read may install a
		// Challenger; otherwise a truncated recent slice could bias the gate.
		if fullArchive {
			s.applyRealtimeCalibration(report)
		}
		writeJSON(writer, http.StatusOK, realtimeStrategyResponse{Report: &report, Cached: true})
		return
	}
	if s.realtimeArchive == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "实时信号归档未初始化"})
		return
	}
	signals, err := s.realtimeArchive.List(limit)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取实时信号失败: " + err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), realtimeOutcomeTimeout)
	defer cancel()
	report, err := s.realtimeOutcomes.Evaluate(ctx, signals, realtimeOutcomeOptions(request))
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, errorResponse{Error: err.Error()})
		return
	}
	if fullArchive {
		s.applyRealtimeCalibration(report)
	}
	writeJSON(writer, http.StatusOK, realtimeStrategyResponse{Report: &report})
}

func (s *Server) applyRealtimeCalibration(report realtime.OutcomeReport) {
	if s == nil || s.realtimeScanner == nil {
		return
	}
	calibratable, ok := s.realtimeScanner.(realtimeCalibratableScanner)
	if !ok {
		return
	}
	calibration, ready := realtime.CalibrationFromOutcome(report)
	s.calibrationMu.Lock()
	defer s.calibrationMu.Unlock()
	state := storage.RealtimeCalibrationState{}
	if s.realtimeCalibrationStore != nil {
		loaded, err := s.realtimeCalibrationStore.Load()
		if err == nil {
			state = loaded
		}
	}
	activeID := strings.TrimSpace(state.Active.ID)
	reportID := strings.TrimSpace(report.Calibration.ID)
	// A challenger is observed in parallel with the immutable Champion. Once
	// the same-sample forward comparison says it is materially worse, clear it
	// immediately and remember the rejected version so a later GET cannot load
	// it back from the same archived outcomes.
	if activeID != "" && reportID == activeID && report.Calibration.Status == "候选落后" {
		calibratable.SetCalibration(realtime.ScoreCalibration{})
		if s.realtimeCalibrationStore != nil {
			state.RejectedIDs = appendUniqueCalibrationID(state.RejectedIDs, activeID)
			state.Status = realtimeCalibrationRollbackStatus
			state.LastDecisionAt = s.currentTime()
			state.History = append(state.History, storage.RealtimeCalibrationRevision{
				ID: activeID, AppliedAt: state.LastDecisionAt, Action: "rollback",
				ReadySamples: report.Calibration.ReadySamples,
				Note:         report.Calibration.Recommendation,
			})
			_ = s.realtimeCalibrationStore.Save(state)
		}
		return
	}
	if reportID != "" && calibrationIDRejected(state.RejectedIDs, reportID) {
		calibratable.SetCalibration(realtime.ScoreCalibration{})
		return
	}
	if !ready {
		// Keep a currently active Challenger alive while more forward samples are
		// collected. A report limited to a partial window must never silently
		// downgrade it to the Champion.
		if activeID != "" && state.Status != realtimeCalibrationRollbackStatus {
			calibratable.SetCalibration(state.Active)
		} else {
			calibratable.SetCalibration(realtime.ScoreCalibration{})
		}
		return
	}
	if report.Calibration.Status == "候选落后" {
		// This can happen when a fresh process sees a rejected candidate before its
		// durable state has been restored. Treat it as a rejection rather than
		// activating a known-bad version.
		calibratable.SetCalibration(realtime.ScoreCalibration{})
		if s.realtimeCalibrationStore != nil && reportID != "" {
			state.RejectedIDs = appendUniqueCalibrationID(state.RejectedIDs, reportID)
			state.Status = realtimeCalibrationRollbackStatus
			state.LastDecisionAt = s.currentTime()
			_ = s.realtimeCalibrationStore.Save(state)
		}
		return
	}
	if activeID != "" && !newerRealtimeCalibration(calibration, state.Active) {
		// A stale report must never roll the scanner back to an older Challenger.
		if state.Status == realtimeCalibrationRollbackStatus {
			calibratable.SetCalibration(realtime.ScoreCalibration{})
		} else {
			calibratable.SetCalibration(state.Active)
		}
		return
	}
	calibratable.SetCalibration(calibration)
	if s.realtimeCalibrationStore == nil {
		return
	}
	now := s.currentTime()
	state.Active = calibration
	state.AppliedAt = now
	state.Source = "收盘后滚动验证"
	state.Status = realtimeCalibrationActiveStatus
	state.LastDecisionAt = now
	state.History = append(state.History, storage.RealtimeCalibrationRevision{
		ID: calibration.ID, AppliedAt: now, Action: "apply",
		ReadySamples: calibration.ReadySamples, MinimumScore: calibration.MinimumScore,
		ComponentWeights: cloneCalibrationWeights(calibration.ComponentWeights),
		Note:             "通过时间顺序留出、覆盖、相关性和组合门禁",
	})
	_ = s.realtimeCalibrationStore.Save(state)
}

func appendUniqueCalibrationID(input []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return input
	}
	for _, item := range input {
		if strings.TrimSpace(item) == value {
			return input
		}
	}
	return append(input, value)
}

func calibrationIDRejected(ids []string, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, item := range ids {
		if strings.TrimSpace(item) == value {
			return true
		}
	}
	return false
}

func newerRealtimeCalibration(next, current realtime.ScoreCalibration) bool {
	if strings.TrimSpace(current.ID) == "" {
		return true
	}
	if strings.TrimSpace(next.ID) == strings.TrimSpace(current.ID) {
		return false
	}
	if strings.TrimSpace(next.DataThrough) != strings.TrimSpace(current.DataThrough) {
		return strings.TrimSpace(next.DataThrough) > strings.TrimSpace(current.DataThrough)
	}
	return next.ReadySamples > current.ReadySamples
}

func cloneCalibrationWeights(input map[string]float64) map[string]float64 {
	if len(input) == 0 {
		return nil
	}
	result := make(map[string]float64, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func realtimeOutcomeOptions(request *http.Request) realtime.OutcomeOptions {
	options := realtime.OutcomeOptions{}
	if raw := strings.TrimSpace(request.URL.Query().Get("horizons")); raw != "" {
		for _, item := range strings.Split(raw, ",") {
			if value, err := strconv.Atoi(strings.TrimSpace(item)); err == nil {
				options.Horizons = append(options.Horizons, value)
			}
		}
	}
	if raw := strings.TrimSpace(request.URL.Query().Get("target_percent")); raw != "" {
		if value, err := strconv.ParseFloat(raw, 64); err == nil {
			options.TargetReturn = value
		}
	}
	if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			options.SignalLimit = value
		}
	}
	if outcomeFullArchiveRequested(request) {
		options.SignalLimit = -1
	}
	return options
}

func outcomeFullArchiveRequested(request *http.Request) bool {
	if request == nil {
		return false
	}
	raw := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("full")))
	return raw == "1" || raw == "true" || raw == "yes" || raw == "all"
}

func (s *Server) writeRealtimeStrategy(writer http.ResponseWriter, request *http.Request) {
	if strings.EqualFold(strings.TrimSpace(request.URL.Query().Get("view")), "history") {
		if s.realtimeArchive == nil {
			writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "实时信号历史未初始化"})
			return
		}
		limit := 100
		if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > 1000 {
				writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "历史数量必须在 1 到 1000 之间"})
				return
			}
			limit = value
		}
		items, err := s.realtimeArchive.List(limit)
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, realtimeStrategyResponse{History: items})
		return
	}
	result, err := s.latestRealtimeSnapshot()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取最近实时扫描失败: " + err.Error()})
		return
	}
	session := s.marketSession(request.Context(), s.currentTime())
	result = s.enrichRealtimeSectors(request.Context(), result, session)
	writeJSON(writer, http.StatusOK, s.realtimeSessionPayload(result, session, !result.GeneratedAt.IsZero()))
}

func (s *Server) runRealtimeStrategy(writer http.ResponseWriter, request *http.Request) {
	result, err := s.latestRealtimeSnapshot()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取最近实时扫描失败: " + err.Error()})
		return
	}
	session := s.marketSession(request.Context(), s.currentTime())
	if !session.ScanAllowed && !session.ShouldFinalize(result.GeneratedAt) {
		result = s.enrichRealtimeSectors(request.Context(), result, session)
		writeJSON(writer, http.StatusOK, s.realtimeSessionPayload(result, session, !result.GeneratedAt.IsZero()))
		return
	}

	s.realtimeMu.Lock()
	result = s.realtimeCache
	if !session.ScanAllowed && !session.ShouldFinalize(result.GeneratedAt) {
		s.realtimeMu.Unlock()
		writeJSON(writer, http.StatusOK, s.realtimeSessionPayload(result, session, !result.GeneratedAt.IsZero()))
		return
	}
	if s.realtimeRunning {
		s.realtimeMu.Unlock()
		writeJSON(writer, http.StatusConflict, errorResponse{Error: "实时策略扫描正在运行"})
		return
	}
	s.realtimeRunning = true
	s.realtimeMu.Unlock()
	defer func() {
		s.realtimeMu.Lock()
		s.realtimeRunning = false
		s.realtimeMu.Unlock()
	}()

	groups, _, err := storage.LoadWatchlistGroups(s.watchlistFile)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取自选失败: " + err.Error()})
		return
	}
	symbols := storage.WatchlistSymbols(groups, storage.AllWatchlistGroup)
	includeLeaders := !strings.EqualFold(strings.TrimSpace(request.URL.Query().Get("scope")), "watchlist")
	ctx, cancel := context.WithTimeout(request.Context(), realtimeScanTimeout)
	defer cancel()
	result, err = s.realtimeScanner.Scan(ctx, symbols, includeLeaders)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, errorResponse{Error: err.Error()})
		return
	}
	s.realtimeMu.Lock()
	s.realtimeCache = result
	s.realtimeMu.Unlock()
	writeJSON(writer, http.StatusOK, s.realtimeSessionPayload(result, session, false))
}

func (s *Server) latestRealtimeSnapshot() (realtime.ScanResult, error) {
	s.realtimeMu.Lock()
	result := s.realtimeCache
	s.realtimeMu.Unlock()
	if !result.GeneratedAt.IsZero() || s.realtimeArchive == nil {
		return result, nil
	}

	latest, err := s.realtimeArchive.Latest()
	if err != nil {
		return realtime.ScanResult{}, err
	}
	if latest.GeneratedAt.IsZero() {
		return realtime.ScanResult{}, nil
	}
	s.realtimeMu.Lock()
	if s.realtimeCache.GeneratedAt.IsZero() || latest.GeneratedAt.After(s.realtimeCache.GeneratedAt) {
		s.realtimeCache = latest
	}
	result = s.realtimeCache
	s.realtimeMu.Unlock()
	return result, nil
}

func (s *Server) enrichRealtimeSectors(ctx context.Context, result realtime.ScanResult, session realtime.MarketSession) realtime.ScanResult {
	if result.GeneratedAt.IsZero() || session.State != realtime.MarketStateClosed || session.FinalizationAllowed || !hasMissingRealtimeSector(result) {
		return result
	}
	now := s.currentTime().In(realtimeWebLocation)
	generatedAt := result.GeneratedAt.In(realtimeWebLocation)
	if now.Format("2006-01-02") != generatedAt.Format("2006-01-02") || now.Before(session.CloseAt) {
		return result
	}
	enricher, ok := s.realtimeScanner.(realtimeSectorEnricher)
	if !ok {
		return result
	}

	s.realtimeMu.Lock()
	if s.realtimeSectorEnrichedAt.Equal(result.GeneratedAt) {
		result = s.realtimeCache
		s.realtimeMu.Unlock()
		return result
	}
	if s.realtimeSectorEnriching {
		result = s.realtimeCache
		s.realtimeMu.Unlock()
		return result
	}
	s.realtimeSectorEnriching = true
	s.realtimeMu.Unlock()

	enrichContext, cancel := context.WithTimeout(ctx, realtimeSectorEnrichTimeout)
	updated, err := enricher.EnrichSectors(enrichContext, result)
	cancel()

	s.realtimeMu.Lock()
	s.realtimeSectorEnriching = false
	if err == nil {
		if s.realtimeCache.GeneratedAt.Equal(result.GeneratedAt) {
			s.realtimeCache = updated
		}
		s.realtimeSectorEnrichedAt = result.GeneratedAt
	}
	result = s.realtimeCache
	s.realtimeMu.Unlock()
	return result
}

func hasMissingRealtimeSector(result realtime.ScanResult) bool {
	for _, signal := range result.Signals {
		for _, component := range signal.Components {
			if component.Key == "sector-rotation" && component.State == "数据不足" {
				return true
			}
		}
	}
	return false
}

func (s *Server) currentTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Server) realtimeSessionPayload(result realtime.ScanResult, session realtime.MarketSession, cached bool) realtimeStrategyResponse {
	scanAllowed := session.ScanAllowed || session.ShouldFinalize(result.GeneratedAt)
	frozen := !scanAllowed
	tradingDay := session.TradingDay
	calendarKnown := session.CalendarKnown
	response := realtimeStrategyResponse{
		MarketState:   session.State,
		TradingDay:    &tradingDay,
		CalendarKnown: &calendarKnown,
		ScanAllowed:   &scanAllowed,
		Frozen:        &frozen,
		Cached:        cached,
		Automation:    s.automationStatus(),
	}
	if !session.NextScanAt.IsZero() {
		response.NextScanAt = session.NextScanAt.Format(time.RFC3339)
	}
	if !result.GeneratedAt.IsZero() {
		copied := result
		response.Result = &copied
	}
	return response
}
