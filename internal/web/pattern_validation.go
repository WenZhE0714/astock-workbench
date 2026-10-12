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

	"github.com/wenzhe/astock-workbench/internal/backtest"
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

func WithPatternValidationHistory(history market.DailyBarRangeClient) ServerOption {
	return func(s *Server) { s.patternHistory = history }
}

func WithPatternValidationActions(actions market.CorporateActionClient) ServerOption {
	return func(s *Server) { s.patternActions = actions }
}

func WithPatternValidationFactors(factors market.AdjustmentFactorClient) ServerOption {
	return func(s *Server) { s.patternFactors = factors }
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
		if report.Version != strategy.PatternValidationVersion && report.Version != "pattern-validation-v1" && report.Version != "pattern-validation-v2" && report.Version != "pattern-validation-v3" && report.Version != "pattern-validation-v4" {
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
	filter.Action = q.Get("action")
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
	if s.resolver == nil || (s.history == nil && s.patternHistory == nil) {
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
	cutoff, err := strategy.ValidatePatternValidationRequest(input, now)
	if err != nil {
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
		fetchCtx, stop := context.WithTimeout(ctx, 30*time.Second)
		series := s.fetchPatternValidationHistory(fetchCtx, symbol, input.Start, cutoff)
		stop()
		if symbol != "sh000300" && s.patternActions != nil {
			actionCtx, stop := context.WithTimeout(ctx, 10*time.Second)
			actions, actionErr := s.patternActions.FetchCorporateActions(actionCtx, symbol, series.FetchStart, cutoff)
			stop()
			if actionErr != nil {
				actions.Status, actions.Error = "unavailable", actionErr.Error()
			}
			series.Actions = &actions
		}
		if symbol != "sh000300" && input.PriceBasis == domain.PatternPriceHFQ && s.patternFactors != nil && series.Error == "" {
			factorCtx, stop := context.WithTimeout(ctx, 10*time.Second)
			factors, factorErr := s.patternFactors.FetchAdjustmentFactors(factorCtx, symbol, series.FetchStart, cutoff)
			stop()
			if factorErr != nil {
				factors.Status, factors.Error = "unavailable", factorErr.Error()
			}
			series.Adjustment = &factors
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

func (s *Server) fetchPatternValidationHistory(ctx context.Context, symbol, startDate, cutoff string) domain.PatternValidationInput {
	start, _ := time.Parse(time.DateOnly, startDate)
	start = start.AddDate(0, 0, -strategy.PatternValidationWarmupDays)
	end, _ := time.Parse(time.DateOnly, cutoff)
	input := domain.PatternValidationInput{Symbol: symbol, FetchStart: start.Format(time.DateOnly), FetchEnd: cutoff, FetchMode: "range"}
	provider := s.patternHistory
	if provider == nil {
		provider, _ = s.history.(market.DailyBarRangeClient)
	}
	var err error
	if provider != nil {
		input.Bars, err = provider.FetchDailyBarsRange(ctx, symbol, start, end, backtest.AdjustmentNone)
		if err != nil && ctx.Err() == nil && s.history != nil {
			input.FetchMode = "snapshot"
			input.FetchWarning = "区间取数失败（" + err.Error() + "），使用近期快照，较早历史可能缺失"
			input.Bars, err = s.fetchDailyBars(ctx, symbol)
		}
	} else {
		input.FetchMode = "snapshot"
		input.FetchWarning = "当前数据源不支持日期区间，使用近期快照，较早历史可能缺失"
		input.Bars, err = s.fetchDailyBars(ctx, symbol)
	}
	if err != nil {
		input.Error, input.Bars = err.Error(), nil
		return input
	}
	filtered := make([]domain.DailyBar, 0, len(input.Bars))
	for _, bar := range input.Bars {
		if bar.Date >= input.FetchStart && bar.Date <= cutoff {
			filtered = append(filtered, bar)
		}
	}
	input.Bars = filtered
	return input
}
