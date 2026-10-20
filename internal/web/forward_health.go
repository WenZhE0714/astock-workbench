package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/notify"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type forwardAcceptanceView struct {
	Configured bool                             `json:"configured"`
	Bound      bool                             `json:"bound"`
	Current    bool                             `json:"current"`
	Stale      bool                             `json:"stale"`
	Error      string                           `json:"error,omitempty"`
	Report     *storage.ForwardAcceptanceReport `json:"report,omitempty"`
}

func WithForwardAcceptance(store *storage.ForwardAcceptanceStore) ServerOption {
	return func(s *Server) { s.forwardAcceptance = store }
}

func (s *Server) readForwardAcceptance(plan backtest.VolumeForwardPlan, count int, lastDate string, now time.Time) forwardAcceptanceView {
	if s.forwardAcceptance == nil {
		return forwardAcceptanceView{}
	}
	report, err := s.forwardAcceptance.Read(plan, s.volumeForwardStudy.Path(), now)
	return assessForwardAcceptance(report, err, count, lastDate, now)
}

func assessForwardAcceptance(report storage.ForwardAcceptanceReport, err error, count int, lastDate string, now time.Time) forwardAcceptanceView {
	view := forwardAcceptanceView{Configured: true}
	if err != nil {
		view.Error = err.Error()
		return view
	}
	view.Bound, view.Report = true, &report
	age := now.Sub(report.CheckedAt)
	view.Stale = age > 15*time.Minute || age < -time.Minute
	through := report.FirstDate
	if report.Execution != nil && report.Execution.VerifiedThrough != "" {
		through = report.Execution.VerifiedThrough
	}
	view.Current = !view.Stale && report.SealedDays == count && (count == 0 || through == lastDate)
	return view
}

func (s *Server) runForwardHealthLoop(ctx context.Context) {
	if s.volumeForwardStudy == nil || s.nativeNotifications == nil {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		for _, message := range s.forwardHealthMessages(s.currentTime()) {
			s.nativeNotifications.Offer(message)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) forwardHealthMessages(now time.Time) []notify.Message {
	rootHash := sha256.Sum256([]byte(s.volumeForwardStudy.Path()))
	key := hex.EncodeToString(rootHash[:])
	plan, err := s.volumeForwardStudy.ReadPlan()
	if err != nil {
		return []notify.Message{forwardFailureMessage(key, "archive", "前瞻归档异常", err.Error(), now, time.Time{})}
	}
	status, statusErr := s.volumeForwardStudy.ReadStatus()
	view := forwardAcceptanceView{}
	if s.forwardAcceptance != nil {
		report, readErr := s.forwardAcceptance.Read(plan, s.volumeForwardStudy.Path(), now)
		view = assessForwardAcceptance(report, readErr, status.SealedDays, "", now)
	}
	return forwardStatusFailureMessages(plan.Fingerprint, status, statusErr, view, now)
}

func forwardStatusFailureMessages(key string, status storage.VolumeForwardStatus, statusErr error, acceptance forwardAcceptanceView, now time.Time) []notify.Message {
	var messages []notify.Message
	collectorFailed := statusErr != nil || status.Status == "error"
	if statusErr != nil {
		messages = append(messages, forwardFailureMessage(key, "collection", "前瞻采集状态异常", statusErr.Error(), now, time.Time{}))
	} else if status.Status == "error" {
		messages = append(messages, forwardFailureMessage(key, "collection", "前瞻采集失败", status.Detail, now, status.CheckedAt))
	}
	if !acceptance.Configured {
		return messages
	}
	if acceptance.Error != "" {
		return append(messages, forwardFailureMessage(key, "acceptance", "前瞻验收报告异常", acceptance.Error, now, time.Time{}))
	}
	report := acceptance.Report
	if report == nil {
		return messages
	}
	if collectorFailed && report.Execution != nil && report.Execution.Status == "paused" && strings.HasPrefix(report.Detail, "采集任务报错") {
		return messages
	}
	if report.Status == "error" || report.Execution != nil && report.Execution.Status == "error" {
		detail := report.Detail
		if report.Execution != nil && report.Execution.Status == "error" {
			detail = report.Execution.Detail
		}
		messages = append(messages, forwardFailureMessage(key, "acceptance", "前瞻验收失败", detail, now, report.CheckedAt))
	} else if report.Status == "attention" {
		messages = append(messages, forwardFailureMessage(key, "acceptance-attention", "前瞻验收需关注", report.Detail, now, report.CheckedAt))
	}
	return messages
}

func forwardFailureMessage(key, kind, title, detail string, now, checked time.Time) notify.Message {
	text := []rune(detail)
	if len(text) > 500 {
		detail = string(text[:500]) + "…"
	}
	body := "当前状态：" + detail
	if !checked.IsZero() {
		body += "\n报告时间 " + checked.In(realtimeWebLocation).Format("01-02 15:04:05")
	}
	return notify.Message{ID: fmt.Sprintf("forward-health:%s:%s:%s", key, now.In(realtimeWebLocation).Format(time.DateOnly), kind), Kind: "forward-" + kind, Title: "ASTOCK · " + title, Subtitle: "P008 量比前瞻", Body: body, At: now}
}
