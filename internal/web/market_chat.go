package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/market"
)

type marketChatReviewService interface {
	MarketRecord(context.Context, string) (domain.MarketChatRecord, error)
	ReviewMarket(context.Context, string) (domain.MarketChatReview, error)
}

func assistantMarketTarget(symbol, question, scope string) (bool, error) {
	if scope != "" && scope != "auto" && scope != "security" && scope != "market" {
		return false, fmt.Errorf("无效分析对象")
	}
	if scope == "market" || symbol == domain.MarketChatSymbol || market.IsBroadMarketSymbol(symbol) {
		return true, nil
	}
	if scope == "security" {
		return false, nil
	}
	// Explicit scope remains available when a sentence mentions both a stock and the market.
	for _, word := range []string{"这只", "个股", "跑赢", "相对", "相比", "这支", "我的持仓"} {
		if strings.Contains(question, word) {
			return false, nil
		}
	}
	for _, word := range []string{"大盘", "整体市场", "市场整体", "A股整体", "沪深两市", "上证指数", "深证成指", "创业板指数"} {
		if strings.Contains(question, word) {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) handleMarketChatReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{Error: "判断复核只支持GET"})
		return
	}
	service, ok := s.aiChatService.(marketChatReviewService)
	if !ok {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "大盘复核服务未初始化"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	id := r.URL.Query().Get("id")
	if r.URL.Query().Get("evidence") == "1" {
		record, err := service.MarketRecord(ctx, id)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="market-evidence.json"`)
		writeJSON(w, http.StatusOK, record)
		return
	}
	result, err := service.ReviewMarket(ctx, id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}
