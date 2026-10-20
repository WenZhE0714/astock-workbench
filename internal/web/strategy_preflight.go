package web

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/wenzhe/astock-workbench/internal/backtest"
)

func (s *Server) handleStrategyPreflight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "数据预检只支持 POST"})
		return
	}
	inspector, ok := s.strategyEngine.(backtest.HistoryInspector)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "当前回测引擎不支持数据预检"})
		return
	}
	s.strategyMu.Lock()
	if s.strategyRunning {
		s.strategyMu.Unlock()
		writeJSON(w, http.StatusConflict, errorResponse{Error: "已有回测或数据预检正在运行，请稍后重试"})
		return
	}
	s.strategyRunning = true
	s.strategyMu.Unlock()
	defer func() { s.strategyMu.Lock(); s.strategyRunning = false; s.strategyMu.Unlock() }()
	var input strategyBacktestInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "预检请求格式无效: " + err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), strategyRunTimeout)
	defer cancel()
	request, err := s.strategyRequest(ctx, input)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	report, err := inspector.InspectHistory(ctx, request)
	if err != nil {
		writeJSON(w, http.StatusRequestTimeout, errorResponse{Error: "数据预检未完成: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}
