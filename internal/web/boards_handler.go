package web

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/market"
)

type boardRankingResponse struct {
	Items     []*boardResponse `json:"items"`
	FetchedAt string           `json:"fetched_at"`
	Sort      string           `json:"sort"`
	Warning   string           `json:"warning,omitempty"`
}

func (s *Server) handleBoards(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "板块行情只支持 GET"})
		return
	}
	if s == nil || s.industryFlows == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "行业板块行情服务未初始化"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 8*time.Second)
	defer cancel()
	flows, err := s.industryFlows.FetchIndustryFlows(ctx)
	if err != nil && len(flows) == 0 {
		writeJSON(writer, http.StatusBadGateway, boardRankingResponse{Items: []*boardResponse{}, FetchedAt: time.Now().Format(time.RFC3339), Warning: err.Error()})
		return
	}
	items := make([]*boardResponse, 0, len(flows))
	for _, flow := range flows {
		items = append(items, newBoardResponse(flow, nil))
	}
	sortKey := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("sort")))
	if sortKey != "weak" && sortKey != "flow" {
		sortKey = "hot"
	}
	sort.SliceStable(items, func(left, right int) bool {
		lv, rv := items[left], items[right]
		switch sortKey {
		case "weak":
			return boardResponseNumber(lv.Percent) < boardResponseNumber(rv.Percent)
		case "flow":
			return boardResponseNumber(lv.MainNet) > boardResponseNumber(rv.MainNet)
		default:
			return boardResponseNumber(lv.Percent) > boardResponseNumber(rv.Percent)
		}
	})
	response := boardRankingResponse{Items: items, FetchedAt: time.Now().Format(time.RFC3339), Sort: sortKey}
	if err != nil {
		response.Warning = err.Error()
		var cached *market.IndustryFlowCacheError
		if errors.As(err, &cached) {
			response.FetchedAt = cached.FetchedAt.Format(time.RFC3339)
		}
	}
	writeJSON(writer, http.StatusOK, response)
}

func boardResponseNumber(value *float64) float64 {
	if value == nil {
		return -1e300
	}
	return *value
}
