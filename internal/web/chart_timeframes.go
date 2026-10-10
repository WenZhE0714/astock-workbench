package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

func (s *Server) handleChartTimeframes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, errorResponse{Error: "周期对照只支持 GET"})
		return
	}
	input := strings.TrimSpace(r.URL.Query().Get("symbol"))
	if input == "" {
		writeJSON(w, 400, errorResponse{Error: "缺少证券代码"})
		return
	}
	if s.resolver == nil || s.history == nil {
		writeJSON(w, 503, errorResponse{Error: "日K服务未初始化"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	symbol, err := s.resolver.Resolve(ctx, input)
	if err != nil {
		writeJSON(w, 400, errorResponse{Error: err.Error()})
		return
	}
	if market.AssetKindOf(symbol) == domain.AssetKindSector {
		writeJSON(w, 400, errorResponse{Error: "板块暂不支持周日对照"})
		return
	}
	bars, err := s.fetchDailyBars(ctx, symbol)
	if err != nil {
		writeJSON(w, 502, errorResponse{Error: err.Error()})
		return
	}
	result, err := strategy.AnalyzeChartTimeframes(symbol, bars, s.currentTime(), r.URL.Query().Get("through"))
	if err != nil {
		writeJSON(w, 400, errorResponse{Error: err.Error()})
		return
	}
	if fingerprint := r.URL.Query().Get("fingerprint"); fingerprint != "" && fingerprint != result.BaseFingerprint {
		writeJSON(w, 409, errorResponse{Error: "图表快照已变化，请刷新结构分析后查看周期对照"})
		return
	}
	writeJSON(w, 200, result)
}
