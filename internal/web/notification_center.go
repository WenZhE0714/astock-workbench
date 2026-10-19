package web

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/notify"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func WithNotificationCenter(records *storage.NativeNotificationStore, reviews *storage.NotificationReviewStore) ServerOption {
	return func(s *Server) { s.notificationRecords = records; s.notificationReviews = reviews }
}

func (s *Server) handleNativeNotifications(w http.ResponseWriter, r *http.Request) {
	if s.notificationRecords == nil || s.notificationReviews == nil {
		writeJSON(w, 503, errorResponse{Error: "Mac 通知记录未配置"})
		return
	}
	state, err := s.notificationRecords.Load()
	if err != nil {
		writeJSON(w, 500, errorResponse{Error: err.Error()})
		return
	}
	if r.Method == http.MethodGet {
		reviews, err := s.notificationReviews.Load()
		if err != nil {
			writeJSON(w, 500, errorResponse{Error: err.Error()})
			return
		}
		items := make([]notify.Record, 0, len(state.Records))
		for _, record := range state.Records {
			items = append(items, record)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].At.After(items[j].At) })
		if len(items) > 500 {
			items = items[:500]
		}
		writeJSON(w, 200, struct {
			Items   []notify.Record             `json:"items"`
			Reviews storage.NotificationReviews `json:"reviews"`
			Enabled bool                        `json:"enabled"`
		}{items, reviews, s.nativeNotifications != nil})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, errorResponse{Error: "只支持 GET、POST"})
		return
	}
	var input struct {
		Action string `json:"action"`
		ID     string `json:"id"`
		Symbol string `json:"symbol"`
		Note   string `json:"note"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, 400, errorResponse{Error: "请求格式无效"})
		return
	}
	switch input.Action {
	case "read", "handle", "note":
		if _, ok := state.Records[input.ID]; !ok {
			writeJSON(w, 404, errorResponse{Error: "提醒记录不存在"})
			return
		}
		if len([]rune(input.Note)) > 500 {
			writeJSON(w, 400, errorResponse{Error: "备注最多500字"})
			return
		}
	case "follow", "unfollow", "mute", "unmute":
		if !regexp.MustCompile(`^(sh|sz|bj)[0-9]{6}$`).MatchString(input.Symbol) {
			writeJSON(w, 400, errorResponse{Error: "股票代码无效"})
			return
		}
	default:
		writeJSON(w, 400, errorResponse{Error: "未知通知操作"})
		return
	}
	now := s.currentTime()
	err = s.notificationReviews.Update(func(reviews *storage.NotificationReviews) {
		item := reviews.Items[input.ID]
		symbol := reviews.Symbols[input.Symbol]
		switch input.Action {
		case "read":
			item.ReadAt = now
		case "handle":
			item.ReadAt = now
			item.HandledAt = now
		case "note":
			item.Note = input.Note
		case "follow":
			symbol.Followed = true
		case "unfollow":
			symbol.Followed = false
		case "mute":
			local := now.In(realtimeWebLocation)
			symbol.MutedUntil = time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, realtimeWebLocation)
		case "unmute":
			symbol.MutedUntil = time.Time{}
		}
		if input.ID != "" {
			reviews.Items[input.ID] = item
		}
		if input.Symbol != "" {
			reviews.Symbols[input.Symbol] = symbol
		}
	})
	if err != nil {
		writeJSON(w, 500, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
