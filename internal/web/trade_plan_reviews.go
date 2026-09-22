package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type tradePlanReviewArchive interface {
	Load(string, string) (domain.TradePlanReview, error)
	List(string) ([]domain.TradePlanReview, error)
	Update(domain.TradePlan, domain.TradePlanReviewRevision, time.Time) (domain.TradePlanReview, error)
}

func WithTradePlanReviews(store tradePlanReviewArchive) ServerOption {
	return func(server *Server) { server.planReviews = store }
}

type tradePlanReviewRequest struct {
	Symbol          string   `json:"symbol"`
	PlanID          string   `json:"plan_id"`
	Note            string   `json:"note,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	ExecutionStatus string   `json:"execution_status"`
	ActualEntry     *float64 `json:"actual_entry,omitempty"`
	ActualExit      *float64 `json:"actual_exit,omitempty"`
	EntryAt         string   `json:"entry_at,omitempty"`
	ExitAt          string   `json:"exit_at,omitempty"`
	ExitReason      string   `json:"exit_reason,omitempty"`
	Discipline      string   `json:"discipline,omitempty"`
}

func parseReviewTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04", "2006-01-02 15:04:05"} {
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid review time")
}

func (s *Server) handleTradePlanReviews(writer http.ResponseWriter, request *http.Request) {
	if s.planReviews == nil || s.tradePlans == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "计划复盘存储未配置"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Second)
	defer cancel()
	if request.Method == http.MethodGet {
		symbol, err := s.resolveAssistantSymbol(ctx, request.URL.Query().Get("symbol"))
		if err != nil || !supportsStockPlan(symbol) {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "计划复盘股票无效"})
			return
		}
		items, err := s.planReviews.List(symbol)
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, struct {
			Items []domain.TradePlanReview `json:"items"`
		}{Items: items})
		return
	}
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "计划复盘只支持 GET / POST"})
		return
	}
	var input tradePlanReviewRequest
	if !decodePlanMonitorMutation(writer, request, &input) {
		return
	}
	if !supportsStockPlan(input.Symbol) || input.PlanID == "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "计划复盘缺少有效股票或计划标识"})
		return
	}
	plan, err := s.tradePlans.Load(input.Symbol, input.PlanID)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "读取原始计划失败: " + err.Error()})
		return
	}
	entryAt, entryErr := parseReviewTime(input.EntryAt)
	exitAt, exitErr := parseReviewTime(input.ExitAt)
	if entryErr != nil || exitErr != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "复盘成交时间格式无效"})
		return
	}
	review, err := s.planReviews.Update(plan, domain.TradePlanReviewRevision{
		Note: input.Note, Tags: input.Tags, ExecutionStatus: input.ExecutionStatus,
		ActualEntry: input.ActualEntry, ActualExit: input.ActualExit, EntryAt: entryAt, ExitAt: exitAt,
		ExitReason: input.ExitReason, Discipline: input.Discipline,
	}, s.currentTime())
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, review)
}
