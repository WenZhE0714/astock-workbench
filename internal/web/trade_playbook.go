package web

import (
	"net/http"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type allTradePlanArchive interface {
	All(int) ([]domain.TradePlan, error)
}

type allTradePlanReviewArchive interface {
	All(int) ([]domain.TradePlanReview, error)
}

func (s *Server) handleTradePlaybook(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "交易剧本只支持 GET"})
		return
	}
	plans, plansOK := s.tradePlans.(allTradePlanArchive)
	reviews, reviewsOK := s.planReviews.(allTradePlanReviewArchive)
	if !plansOK || !reviewsOK {
		writeJSON(writer, http.StatusServiceUnavailable, errorResponse{Error: "交易剧本存储未配置"})
		return
	}
	planItems, err := plans.All(0)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取冻结计划失败: " + err.Error()})
		return
	}
	reviewItems, err := reviews.All(0)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "读取计划复盘失败: " + err.Error()})
		return
	}
	report := strategy.BuildTradePlaybookReport(planItems, reviewItems, s.currentTime())
	if s.nameCacheFile != "" {
		names, err := storage.LoadNameCache(s.nameCacheFile)
		if err != nil {
			report.Warnings = append(report.Warnings, "股票名称缓存不可用，暂以证券代码显示")
		} else {
			for index := range report.Recent {
				report.Recent[index].Name = names.LookupName(report.Recent[index].Symbol)
			}
		}
	}
	writeJSON(writer, http.StatusOK, struct {
		Report domain.TradePlaybookReport `json:"report"`
	}{Report: report})
}
