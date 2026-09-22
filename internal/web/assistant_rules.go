package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type assistantChartInput struct {
	Timeframe   string `json:"timeframe"`
	Through     string `json:"through"`
	Fingerprint string `json:"fingerprint"`
	StructureID string `json:"structure_id"`
	VisibleFrom string `json:"visible_from,omitempty"`
	VisibleTo   string `json:"visible_to,omitempty"`
}

type assistantRuleRequest struct {
	Action    string                     `json:"action"`
	Symbol    string                     `json:"symbol"`
	Question  string                     `json:"question,omitempty"`
	ExpiresOn string                     `json:"expires_on,omitempty"`
	Chart     *assistantChartInput       `json:"chart,omitempty"`
	Draft     *domain.AssistantRuleDraft `json:"draft,omitempty"`
}

func (s *Server) resolveAssistantChartContext(ctx context.Context, symbol string, input *assistantChartInput) (*domain.AssistantChartContext, error) {
	if input == nil {
		return nil, nil
	}
	if input.Timeframe != "1d" || input.Through == "" || input.Fingerprint == "" || input.StructureID == "" {
		return nil, fmt.Errorf("图表上下文缺少日线周期、日期、指纹或所选结构")
	}
	if _, err := time.Parse(time.DateOnly, input.Through); err != nil {
		return nil, fmt.Errorf("图表上下文日期无效")
	}
	analysis, err := s.chartAnalysis(ctx, symbol, input.Through)
	if err != nil {
		return nil, fmt.Errorf("重新计算图表上下文失败: %w", err)
	}
	if analysis.Symbol != symbol || analysis.Fingerprint != input.Fingerprint {
		return nil, fmt.Errorf("图表快照已变化，请刷新结构分析后重试")
	}
	selected := domain.ChartStructure{}
	for _, item := range analysis.Structures {
		if item.ID == input.StructureID {
			selected = item
			break
		}
	}
	if selected.ID == "" {
		return nil, fmt.Errorf("所选图表结构已变化，请重新选择")
	}
	visibleFrom, visibleTo := strings.TrimSpace(input.VisibleFrom), strings.TrimSpace(input.VisibleTo)
	if visibleFrom != "" {
		if _, err := time.Parse(time.DateOnly, visibleFrom); err != nil {
			return nil, fmt.Errorf("可视区间起点无效")
		}
	}
	if visibleTo != "" {
		if _, err := time.Parse(time.DateOnly, visibleTo); err != nil {
			return nil, fmt.Errorf("可视区间终点无效")
		}
	}
	if (visibleFrom != "" && visibleTo != "" && visibleFrom > visibleTo) || visibleTo > analysis.DataDate {
		return nil, fmt.Errorf("可视区间与分析日期不一致")
	}
	return &domain.AssistantChartContext{
		Version: "assistant-chart-v1", Symbol: symbol, Timeframe: "1d",
		VisibleFrom: visibleFrom, VisibleTo: visibleTo, SelectedStructure: selected, Analysis: analysis,
	}, nil
}

func cloneAssistantChartContext(input *domain.AssistantChartContext) *domain.AssistantChartContext {
	if input == nil {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	var result domain.AssistantChartContext
	if json.Unmarshal(data, &result) != nil {
		return nil
	}
	return &result
}

func (s *Server) handleAssistantRuleDraft(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "规则草案只支持 POST"})
		return
	}
	contentType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if contentType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, errorResponse{Error: "规则草案请求须使用 application/json"})
		return
	}
	if origin := request.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host != request.Host || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			writeJSON(writer, http.StatusForbidden, errorResponse{Error: "拒绝跨站修改规则草案"})
			return
		}
	}
	if request.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSON(writer, http.StatusForbidden, errorResponse{Error: "拒绝跨站修改规则草案"})
		return
	}
	var input assistantRuleRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "规则草案请求格式无效"})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "规则草案请求包含多余内容"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), assistantChatTimeout())
	defer cancel()
	switch input.Action {
	case "draft":
		s.draftAssistantRule(ctx, writer, input)
	case "confirm":
		s.confirmAssistantRule(ctx, writer, input)
	default:
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "规则草案操作无效"})
	}
}

func (s *Server) draftAssistantRule(ctx context.Context, writer http.ResponseWriter, input assistantRuleRequest) {
	service, ok := s.aiChatService.(AITradeRuleService)
	if !ok {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "当前AI通道不支持结构化规则草案"})
		return
	}
	symbol, err := s.resolveAssistantSymbol(ctx, input.Symbol)
	if err != nil || !supportsStockPlan(symbol) {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "规则草案只支持A股个股"})
		return
	}
	question := strings.TrimSpace(input.Question)
	if question == "" || len([]rune(question)) > 500 || input.ExpiresOn == "" {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "请输入500字以内的监控意图和有效期"})
		return
	}
	chart, err := s.resolveAssistantChartContext(ctx, symbol, input.Chart)
	if err != nil || chart == nil {
		if err == nil {
			err = fmt.Errorf("生成规则草案必须携带当前日线图表")
		}
		writeJSON(writer, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	}
	draft, err := service.DraftRule(ctx, symbol, question, input.ExpiresOn, *chart)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, errorResponse{Error: "生成规则草案失败: " + err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, struct {
		Draft domain.AssistantRuleDraft `json:"draft"`
	}{Draft: draft})
}

func (s *Server) confirmAssistantRule(ctx context.Context, writer http.ResponseWriter, input assistantRuleRequest) {
	if s.tradePlans == nil || input.Draft == nil || input.Draft.Symbol == "" {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "规则草案或交易计划存储未配置"})
		return
	}
	symbol, err := s.resolveAssistantSymbol(ctx, input.Draft.Symbol)
	if err != nil || symbol != input.Draft.Symbol || !supportsStockPlan(symbol) {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "规则草案股票无效"})
		return
	}
	analysis, err := s.chartAnalysis(ctx, symbol, input.Draft.AnalysisDate)
	if err != nil {
		writeJSON(writer, http.StatusBadGateway, errorResponse{Error: "重新计算规则快照失败: " + err.Error()})
		return
	}
	plan, err := strategy.BuildAssistantTradePlan(analysis, *input.Draft, s.currentTime())
	if err != nil {
		writeJSON(writer, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	}
	plan, created, err := s.tradePlans.Save(plan)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "保存助手计划失败: " + err.Error()})
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
