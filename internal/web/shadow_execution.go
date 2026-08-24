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
	Report *paper.Report `json:"report,omitempty"`
	Cached bool          `json:"cached,omitempty"`
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
	options := paper.Options{Config: paper.DefaultConfig(), Limit: 500}
	values := request.URL.Query()
	var err error
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		options.Limit, err = strconv.Atoi(raw)
		if err != nil || options.Limit < 1 || options.Limit > 5000 {
			return paper.Options{}, &queryError{"影子评估信号数量必须在 1 到 5000 之间"}
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

type queryError struct{ message string }

func (err *queryError) Error() string { return err.message }
