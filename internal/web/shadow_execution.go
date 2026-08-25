package web

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/paper"
)

const shadowExecutionTimeout = 2 * time.Minute

type shadowResponse struct {
	Report    *paper.Report `json:"report,omitempty"`
	Cached    bool          `json:"cached,omitempty"`
	Preserved bool          `json:"preserved,omitempty"`
}

func (s *Server) handleShadowExecution(writer http.ResponseWriter, request *http.Request) {
	if s.shadowEvaluator == nil || s.shadowArchive == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "影子执行服务未初始化"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		report, err := s.shadowArchive.Load()
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取影子执行结果失败: " + err.Error()})
			return
		}
		report = s.markShadowPositions(request.Context(), report)
		writeJSON(writer, http.StatusOK, shadowResponse{Report: &report, Cached: true})
	case http.MethodPost:
		if s.realtimeArchive == nil {
			writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "实时信号归档未初始化"})
			return
		}
		options, err := shadowOptions(request)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		s.shadowMu.Lock()
		defer s.shadowMu.Unlock()
		checkpoint, checkpointErr := s.shadowCheckpoint(request.Context())
		if checkpointErr != nil {
			writeJSON(writer, http.StatusBadGateway, errorResponse{Error: "读取影子账户交易日历失败: " + checkpointErr.Error()})
			return
		}
		previous, loadErr := s.shadowArchive.Load()
		if loadErr != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取影子执行结果失败: " + loadErr.Error()})
			return
		}
		if !shadowRebuildRequested(request) {
			if shadowReportMatches(previous, options, checkpoint) {
				previous = s.markShadowPositions(request.Context(), previous)
				writeJSON(writer, http.StatusOK, shadowResponse{Report: &previous, Cached: true})
				return
			}
		}
		signals, err := s.realtimeArchive.List(options.Limit)
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取实时信号失败: " + err.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), shadowExecutionTimeout)
		defer cancel()
		var report paper.Report
		var evaluateErr error
		if !shadowRebuildRequested(request) && shadowCanAdvance(previous, options) {
			if advancer, ok := s.shadowEvaluator.(shadowAdvancer); ok {
				report, evaluateErr = advancer.Advance(ctx, previous, signals, options)
			} else {
				report, evaluateErr = s.shadowEvaluator.Evaluate(ctx, signals, options)
			}
		} else {
			report, evaluateErr = s.shadowEvaluator.Evaluate(ctx, signals, options)
		}
		if evaluateErr != nil {
			err = evaluateErr
		}
		if err != nil {
			writeJSON(writer, http.StatusBadGateway, errorResponse{Error: err.Error()})
			return
		}
		if err := paper.ValidateTransition(previous, report); err != nil {
			previous.Warnings = append(previous.Warnings, "账户连续性保护：本次同步未覆盖持仓，"+err.Error())
			previous = s.markShadowPositions(request.Context(), previous)
			writeJSON(writer, http.StatusOK, shadowResponse{Report: &previous, Cached: true, Preserved: true})
			return
		}
		if err := s.shadowArchive.Save(report); err != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "保存影子执行结果失败: " + err.Error()})
			return
		}
		report = s.markShadowPositions(request.Context(), report)
		writeJSON(writer, http.StatusOK, shadowResponse{Report: &report})
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "影子执行只支持 GET、POST"})
	}
}

func (s *Server) markShadowPositions(ctx context.Context, report paper.Report) paper.Report {
	if s == nil || s.quotes == nil || len(report.Positions) == 0 {
		return report
	}
	symbols := make([]string, 0, len(report.Positions))
	seen := make(map[string]struct{}, len(report.Positions))
	for _, position := range report.Positions {
		if position.Symbol == "" {
			continue
		}
		if _, ok := seen[position.Symbol]; ok {
			continue
		}
		seen[position.Symbol] = struct{}{}
		symbols = append(symbols, position.Symbol)
	}
	if len(symbols) == 0 {
		return report
	}
	quoteContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	quotes, err := s.quotes.Fetch(quoteContext, symbols)
	if err != nil {
		return report
	}
	items := make([]paper.PositionQuote, 0, len(quotes))
	stale := 0
	for _, quote := range quotes {
		price, parseErr := strconv.ParseFloat(strings.TrimSpace(quote.Current), 64)
		if parseErr != nil || price <= 0 {
			continue
		}
		if !shadowQuoteFresh(quote.QuoteTime, s.currentTime()) {
			stale++
			continue
		}
		items = append(items, paper.PositionQuote{
			Symbol: quote.Symbol, Price: price,
			QuoteTime: strings.TrimSpace(quote.QuoteTime), Source: strings.TrimSpace(quote.Source),
		})
	}
	if stale > 0 {
		report.Warnings = append(report.Warnings, fmt.Sprintf("%d 只持仓行情超过 30 分钟，保留日线估值", stale))
	}
	return paper.RevaluePositions(report, items, s.currentTime())
}

func (s *Server) shadowCheckpoint(ctx context.Context) (paper.Checkpoint, error) {
	if s == nil || s.history == nil {
		return paper.TradingCheckpointAt(s.currentTime(), nil), nil
	}
	calendarContext, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	bars, err := s.history.FetchDailyBars(calendarContext, "sh000300")
	if err != nil {
		return paper.Checkpoint{}, err
	}
	dates := make([]string, 0, len(bars))
	for _, bar := range bars {
		if bar.Date != "" {
			dates = append(dates, bar.Date)
		}
	}
	sort.Strings(dates)
	return paper.TradingCheckpointAt(s.currentTime(), dates), nil
}

func shadowOptions(request *http.Request) (paper.Options, error) {
	// A zero limit means all archived signals. The shadow account must not let
	// frequent intraday scans crowd older T+1 signals out of the replay set.
	options := paper.Options{Config: paper.DefaultConfig(), Limit: 0}
	values := request.URL.Query()
	var err error
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		options.Limit, err = strconv.Atoi(raw)
		if err != nil || options.Limit < 0 || options.Limit > 20000 {
			return paper.Options{}, &queryError{"影子评估信号数量必须在 0 到 20000 之间，0 表示全部归档"}
		}
	}
	if raw := strings.TrimSpace(values.Get("minimum_score")); raw != "" {
		options.Config.MinimumScore, err = strconv.ParseFloat(raw, 64)
		if err != nil || options.Config.MinimumScore < 0 || options.Config.MinimumScore > 100 {
			return paper.Options{}, &queryError{"最低综合分必须在 0 到 100 之间"}
		}
	}
	if raw := strings.TrimSpace(values.Get("holding_days")); raw != "" {
		options.Config.HoldingDays, err = strconv.Atoi(raw)
		if err != nil || options.Config.HoldingDays < 1 || options.Config.HoldingDays > 60 {
			return paper.Options{}, &queryError{"影子持有窗口必须在 1 到 60 个交易日之间"}
		}
	}
	return options, nil
}

func shadowRebuildRequested(request *http.Request) bool {
	value := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("rebuild")))
	return value == "1" || value == "true" || value == "yes"
}

func shadowReportMatches(report paper.Report, options paper.Options, checkpoint paper.Checkpoint) bool {
	return report.EngineVersion == paper.ShadowEngineVersion && report.ConfigFingerprint == paper.OptionsFingerprint(options.Config, options.Limit) && report.AsOf == checkpoint.Date && report.CheckpointPhase == checkpoint.Phase
}

func shadowCanAdvance(report paper.Report, options paper.Options) bool {
	hasLedger := len(report.Orders) > 0 || len(report.Positions) > 0 || len(report.Trades) > 0
	if report.EngineVersion != paper.ShadowEngineVersion && hasLedger {
		return paper.ConfigFingerprint(report.Config) == paper.ConfigFingerprint(options.Config)
	}
	if report.ConfigFingerprint != "" && report.ConfigFingerprint != paper.OptionsFingerprint(options.Config, options.Limit) {
		return false
	}
	if report.ConfigFingerprint == "" && paper.ConfigFingerprint(report.Config) != paper.ConfigFingerprint(options.Config) {
		return false
	}
	return report.EngineVersion == paper.ShadowEngineVersion || hasLedger
}

func shadowQuoteFresh(raw string, now time.Time) bool {
	value := strings.TrimSpace(raw)
	if value == "" {
		return false
	}
	quoteTime, err := time.ParseInLocation("2006-01-02 15:04:05", value, realtimeWebLocation)
	if err != nil {
		quoteTime, err = time.ParseInLocation("2006-01-02 15:04", value, realtimeWebLocation)
	}
	if err != nil {
		return false
	}
	delta := now.In(realtimeWebLocation).Sub(quoteTime)
	return delta >= -2*time.Minute && delta <= 30*time.Minute
}

type queryError struct{ message string }

func (err *queryError) Error() string { return err.message }
