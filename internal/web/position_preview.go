package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/wenzhe/astock-workbench/internal/strategy"
)

func (s *Server) handlePositionPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "仓位试算只支持 POST"})
		return
	}
	var input struct {
		Symbol      string                  `json:"symbol"`
		Through     string                  `json:"through"`
		Fingerprint string                  `json:"fingerprint"`
		StructureID string                  `json:"structure_id"`
		Budget      strategy.PositionBudget `json:"budget"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Symbol == "" || input.Through == "" || input.Fingerprint == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "仓位试算参数无效"})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "试算请求包含多余内容"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	analysis, err := s.chartAnalysis(ctx, input.Symbol, input.Through)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResponse{Error: err.Error()})
		return
	}
	if !supportsStockPlan(analysis.Symbol) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "仅支持 A 股个股试算"})
		return
	}
	if input.Fingerprint != analysis.Fingerprint {
		writeJSON(w, http.StatusConflict, errorResponse{Error: "图表快照已变化，请刷新结构分析后重新试算"})
		return
	}
	for _, structure := range analysis.Structures {
		if structure.ID != input.StructureID {
			continue
		}
		if structure.Plan == nil || structure.State == "invalidated" {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: "当前形态已失效或没有做多计划"})
			return
		}
		preview, err := strategy.PreviewPosition(analysis.Symbol, *structure.Plan, input.Budget)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, preview)
		return
	}
	writeJSON(w, http.StatusBadRequest, errorResponse{Error: "所选形态不存在"})
}
