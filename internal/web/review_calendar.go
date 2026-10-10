package web

import (
	"net/http"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/paper"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

func (s *Server) handleReviewCalendar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, errorResponse{Error: "复盘日历只支持 GET"})
		return
	}
	now := s.currentTime()
	value := r.URL.Query().Get("month")
	if value == "" {
		value = now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01")
	}
	month, err := strategy.ReviewCalendarMonth(value)
	if err != nil {
		writeJSON(w, 400, errorResponse{Error: err.Error()})
		return
	}
	source := r.URL.Query().Get("source")
	if source == "" {
		source = "manual"
	}
	if source != "manual" && source != "shadow" {
		writeJSON(w, 400, errorResponse{Error: "日历来源须为 manual 或 shadow"})
		return
	}
	profiles := []map[string]string{}
	for _, id := range s.orderedShadowProfileIDs() {
		profiles = append(profiles, map[string]string{"id": id, "name": s.shadowProfiles[id].Name})
	}
	var events []domain.ReviewCalendarEvent
	warnings, asOf := []string{}, ""
	profileID := ""
	if source == "manual" {
		plans, plansOK := s.tradePlans.(allTradePlanArchive)
		reviews, reviewsOK := s.planReviews.(allTradePlanReviewArchive)
		if !plansOK || !reviewsOK {
			writeJSON(w, 503, errorResponse{Error: "人工复盘存储未配置"})
			return
		}
		items, err := plans.All(0)
		if err != nil {
			writeJSON(w, 500, errorResponse{Error: "读取计划失败: " + err.Error()})
			return
		}
		reviewItems, err := reviews.All(0)
		if err != nil {
			writeJSON(w, 500, errorResponse{Error: "读取复盘失败: " + err.Error()})
			return
		}
		validated := strategy.BuildTradePlaybookReport(items, reviewItems, now)
		events, warnings = strategy.ManualCalendarEvents(validated), validated.Warnings
	} else {
		profileID = r.URL.Query().Get("profile")
		if profileID == "" {
			profileID = shadowProfileBalanced
		}
		profile, ok := s.shadowProfiles[profileID]
		if !ok || profile.Archive == nil {
			writeJSON(w, 400, errorResponse{Error: "未配置该影子账户"})
			return
		}
		archived, err := profile.Archive.Load()
		if err != nil {
			writeJSON(w, 500, errorResponse{Error: "读取影子归档失败: " + err.Error()})
			return
		}
		events, warnings = paper.ReviewCalendarEvents(archived)
		asOf = archived.AsOf
	}
	if s.nameCacheFile != "" {
		names, err := storage.LoadNameCache(s.nameCacheFile)
		if err == nil {
			for i := range events {
				if events[i].Name == "" {
					events[i].Name = names.LookupName(events[i].Symbol)
				}
			}
		}
	}
	report := strategy.BuildReviewCalendar(month, source, events, now)
	report.AsOf, report.Warnings = asOf, append(report.Warnings, warnings...)
	writeJSON(w, http.StatusOK, struct {
		Report   domain.ReviewCalendarReport `json:"report"`
		Profiles []map[string]string         `json:"profiles"`
		Profile  string                      `json:"profile"`
	}{report, profiles, profileID})
}
