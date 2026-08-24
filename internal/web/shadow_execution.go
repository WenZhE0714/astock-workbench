package web

import (
	"context"
	"net/http"
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
		previous, loadErr := s.shadowArchive.Load()
		if loadErr != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取影子执行结果失败: " + loadErr.Error()})
			return
		}
		if !shadowRebuildRequested(request) {
			if shadowReportAsOfCurrentDay(previous, s.currentTime()) {
				if previous.EngineVersion != paper.ShadowEngineVersion {
					previous.EngineVersion = paper.ShadowEngineVersion
					if saveErr := s.shadowArchive.Save(previous); saveErr != nil {
						writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "升级影子账户版本失败: " + saveErr.Error()})
						return
					}
				}
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
		report, err := s.shadowEvaluator.Evaluate(ctx, signals, options)
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
	for _, quote := range quotes {
		price, parseErr := strconv.ParseFloat(strings.TrimSpace(quote.Current), 64)
		if parseErr != nil || price <= 0 {
			continue
		}
		items = append(items, paper.PositionQuote{
			Symbol: quote.Symbol, Price: price,
			QuoteTime: strings.TrimSpace(quote.QuoteTime), Source: strings.TrimSpace(quote.Source),
		})
	}
	return paper.RevaluePositions(report, items, s.currentTime())
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

func shadowReportAsOfCurrentDay(report paper.Report, now time.Time) bool {
	return report.AsOf != "" && report.AsOf == now.In(realtimeWebLocation).Format("2006-01-02")
}

type queryError struct{ message string }

func (err *queryError) Error() string { return err.message }
