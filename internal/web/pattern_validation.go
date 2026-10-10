package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type patternValidationArchive interface {
	Save(domain.PatternValidationReport) (domain.PatternValidationReport, error)
	Load(string) (domain.PatternValidationReport, error)
	List(int) ([]domain.PatternValidationRun, error)
}

func WithPatternValidation(store patternValidationArchive) ServerOption {
	return func(s *Server) { s.patternValidations = store }
}

func (s *Server) handlePatternValidation(w http.ResponseWriter, r *http.Request) {
	if s.patternValidations == nil {
		writeJSON(w, 503, errorResponse{Error: "形态验证归档未初始化"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			items, err := s.patternValidations.List(30)
			if err != nil {
				writeJSON(w, 500, errorResponse{Error: err.Error()})
				return
			}
			writeJSON(w, 200, struct {
				Items []domain.PatternValidationRun `json:"items"`
			}{items})
			return
		}
		report, err := s.patternValidations.Load(id)
		if err != nil {
			writeJSON(w, 404, errorResponse{Error: err.Error()})
			return
		}
		if r.URL.Query().Get("download") == "1" {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"pattern-validation-%s.json\"", report.RunID))
			writeJSON(w, 200, report)
			return
		}
		if report.Version != strategy.PatternValidationVersion {
			writeJSON(w, 409, errorResponse{Error: "归档算法版本不同，请导出查看或重新运行验证"})
			return
		}
		filter, err := patternValidationFilter(r)
		if err != nil {
			writeJSON(w, 400, errorResponse{Error: err.Error()})
			return
		}
		view, err := strategy.PatternValidationResult(report, filter)
		if err != nil {
			writeJSON(w, 400, errorResponse{Error: err.Error()})
			return
		}
		writeJSON(w, 200, view)
	case http.MethodPost:
		s.runPatternValidation(w, r)
	default:
		writeJSON(w, 405, errorResponse{Error: "形态验证只支持 GET、POST"})
	}
}

func patternValidationFilter(r *http.Request) (strategy.PatternValidationFilter, error) {
	q := r.URL.Query()
	filter := strategy.PatternValidationFilter{Pattern: q.Get("pattern"), Bias: q.Get("bias"), Regime: q.Get("regime"), State: q.Get("state")}
	for key, destination := range map[string]*int{"horizon": &filter.Horizon, "offset": &filter.Offset, "limit": &filter.Limit} {
		if value := q.Get(key); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return filter, fmt.Errorf("%s 参数无效", key)
			}
			*destination = parsed
		}
	}
	return filter, nil
}

func (s *Server) runPatternValidation(w http.ResponseWriter, r *http.Request) {
	if s.resolver == nil || s.history == nil {
		writeJSON(w, 503, errorResponse{Error: "日K数据服务未初始化"})
		return
	}
	var input domain.PatternValidationRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, 400, errorResponse{Error: "验证请求格式无效"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, 400, errorResponse{Error: "验证请求只能包含一个JSON对象"})
		return
	}
	now := s.currentTime()
	if _, err := strategy.ValidatePatternValidationRequest(input, now); err != nil {
		writeJSON(w, 400, errorResponse{Error: err.Error()})
		return
	}
	if !s.patternValidationMu.TryLock() {
		writeJSON(w, 409, errorResponse{Error: "已有形态验证正在运行，请稍后重试"})
		return
	}
	defer s.patternValidationMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	resolved := []string{}
	seen := map[string]bool{}
	for _, raw := range input.Symbols {
		symbol, err := s.resolver.Resolve(ctx, strings.TrimSpace(raw))
		if err != nil {
			writeJSON(w, 400, errorResponse{Error: err.Error()})
			return
		}
		if market.AssetKindOf(symbol) != domain.AssetKindStock || strategyIndexSymbol(symbol) {
			writeJSON(w, 400, errorResponse{Error: raw + " 不是A股股票"})
			return
		}
		if !seen[symbol] {
			resolved = append(resolved, symbol)
			seen[symbol] = true
		}
	}
	input.Symbols = resolved
	inputs := []domain.PatternValidationInput{}
	for _, symbol := range append(append([]string{}, resolved...), "sh000300") {
		if err := ctx.Err(); err != nil {
			writeJSON(w, 408, errorResponse{Error: "形态验证已取消或超时"})
			return
		}
		fetchCtx, stop := context.WithTimeout(ctx, 12*time.Second)
		bars, err := s.fetchDailyBars(fetchCtx, symbol)
		stop()
		series := domain.PatternValidationInput{Symbol: symbol, Bars: bars}
		if err != nil {
			series.Error = err.Error()
			series.Bars = nil
		}
		inputs = append(inputs, series)
	}
	report, err := strategy.BuildPatternValidation(ctx, input, inputs, now)
	if err != nil {
		writeJSON(w, 502, errorResponse{Error: err.Error()})
		return
	}
	if ctx.Err() != nil {
		writeJSON(w, 408, errorResponse{Error: "形态验证已取消或超时"})
		return
	}
	report, err = s.patternValidations.Save(report)
	if err != nil {
		writeJSON(w, 500, errorResponse{Error: "形态验证归档失败: " + err.Error()})
		return
	}
	view, err := strategy.PatternValidationResult(report, strategy.PatternValidationFilter{})
	if err != nil {
		writeJSON(w, 500, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, 200, view)
}
