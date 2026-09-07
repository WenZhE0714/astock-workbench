package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type marketRankingResponse struct {
	Kind      domain.MarketRankingKind   `json:"kind"`
	Items     []domain.MarketRankingItem `json:"items"`
	FetchedAt string                     `json:"fetched_at"`
	Warning   string                     `json:"warning,omitempty"`
}

func (s *Server) handleRankings(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "个股榜单只支持 GET"})
		return
	}
	if s == nil || s.rankings == nil {
		writeJSON(writer, http.StatusServiceUnavailable, marketRankingResponse{Items: []domain.MarketRankingItem{}, FetchedAt: time.Now().Format(time.RFC3339), Warning: "个股榜单服务未初始化"})
		return
	}
	kind := domain.MarketRankingKind(strings.ToLower(strings.TrimSpace(request.URL.Query().Get("kind"))))
	if kind != domain.MarketRankingLosers && kind != domain.MarketRankingRapidRise && kind != domain.MarketRankingAmount && kind != domain.MarketRankingTurnover {
		kind = domain.MarketRankingGainers
	}
	limit := 8
	if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit < 1 {
		limit = 8
	}
	if limit > 20 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(request.Context(), 8*time.Second)
	defer cancel()
	items, err := s.rankings.FetchMarketRanking(ctx, kind, limit)
	response := marketRankingResponse{Kind: kind, Items: items, FetchedAt: time.Now().Format(time.RFC3339)}
	if err != nil {
		response.Warning = err.Error()
		writeJSON(writer, http.StatusBadGateway, response)
		return
	}
	writeJSON(writer, http.StatusOK, response)
}
