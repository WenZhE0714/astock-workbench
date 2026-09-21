package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type tradePlanArchive interface {
	Save(domain.TradePlan) (domain.TradePlan, bool, error)
	List(string, int) ([]domain.TradePlan, error)
	Load(string, string) (domain.TradePlan, error)
}

var planStockSymbol = regexp.MustCompile(`^(sh6[0-9]{5}|sz(00|30)[0-9]{4}|bj([48][0-9]{5}|92[0-9]{4}))$`)

func supportsStockPlan(symbol string) bool {
	return planStockSymbol.MatchString(symbol) && !market.IsBroadMarketSymbol(symbol)
}

func WithTradePlans(store tradePlanArchive) ServerOption {
	return func(server *Server) { server.tradePlans = store }
}

func (s *Server) chartAnalysis(ctx context.Context, input, through string) (domain.ChartAnalysis, error) {
	if s.resolver == nil || s.history == nil {
		return domain.ChartAnalysis{}, fmt.Errorf("日K服务未初始化")
	}
	symbol, err := s.resolver.Resolve(ctx, input)
	if err != nil {
		return domain.ChartAnalysis{}, err
	}
	if market.AssetKindOf(symbol) == domain.AssetKindSector {
		return domain.ChartAnalysis{}, fmt.Errorf("板块暂不支持个股结构分析")
	}
	bars, err := s.fetchDailyBars(ctx, symbol)
	if err != nil {
		return domain.ChartAnalysis{}, err
	}
	return strategy.AnalyzeChart(symbol, bars, s.now(), through)
}

func (s *Server) handleChartAnalysis(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "只支持 GET"})
		return
	}
	input := strings.TrimSpace(request.URL.Query().Get("symbol"))
	if input == "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "缺少股票代码"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 12*time.Second)
	defer cancel()
	analysis, err := s.chartAnalysis(ctx, input, request.URL.Query().Get("through"))
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, analysis)
}

func (s *Server) handleTradePlans(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "只支持 GET / POST"})
		return
	}
	if s.tradePlans == nil || s.resolver == nil {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "交易计划存储未配置"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 12*time.Second)
	defer cancel()
	if request.Method == http.MethodGet {
		input := strings.TrimSpace(request.URL.Query().Get("symbol"))
		if input == "" {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "缺少股票代码"})
			return
		}
		symbol, err := s.resolver.Resolve(ctx, input)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		plans, err := s.tradePlans.List(symbol, 100)
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, struct {
			Items []domain.TradePlan `json:"items"`
			Today string             `json:"today"`
		}{Items: plans, Today: s.now().In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format(time.DateOnly)})
		return
	}
	contentType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if contentType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, errorResponse{Error: "计划请求须使用 application/json"})
		return
	}
	if origin := request.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host != request.Host || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			writeJSON(writer, http.StatusForbidden, errorResponse{Error: "拒绝跨站保存计划"})
			return
		}
	}
	if request.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSON(writer, http.StatusForbidden, errorResponse{Error: "拒绝跨站保存计划"})
		return
	}
	var input struct {
		Symbol      string `json:"symbol"`
		Through     string `json:"through"`
		Fingerprint string `json:"fingerprint"`
		StructureID string `json:"structure_id"`
		ExpiresOn   string `json:"expires_on"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.Symbol) == "" || input.Through == "" || input.Fingerprint == "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "计划请求格式无效"})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "计划请求包含多余内容"})
		return
	}
	analysis, err := s.chartAnalysis(ctx, input.Symbol, input.Through)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, errorResponse{Error: err.Error()})
		return
	}
	if !supportsStockPlan(analysis.Symbol) {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "当前仅支持A股个股观察计划"})
		return
	}
	if analysis.Fingerprint != input.Fingerprint {
		writeJSON(writer, http.StatusConflict, errorResponse{Error: "行情快照已变化，请刷新结构分析后重新保存"})
		return
	}
	plan, err := strategy.BuildTradePlan(analysis, input.StructureID, input.ExpiresOn, s.now())
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	plan, created, err := s.tradePlans.Save(plan)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "保存交易计划失败: " + err.Error()})
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(writer, status, struct {
		Plan    domain.TradePlan `json:"plan"`
		Created bool             `json:"created"`
	}{Plan: plan, Created: created})
}
