package strategy

import (
	"fmt"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

func PatternForwardHealth(task domain.PatternForwardTask, control domain.PatternForwardControl, status domain.PatternForwardStatus, days []domain.PatternForwardDay, now time.Time) domain.PatternForwardHealth {
	local := now.In(chartLocation)
	today := local.Format(time.DateOnly)
	h := domain.PatternForwardHealth{Date: today, Phase: "waiting", Detail: "等待首个收盘采集窗口", Expected: len(task.Pool.Members), Issues: []domain.PatternForwardIssue{}, Days: []domain.PatternForwardDailyHealth{}}
	add := func(code, date, message, severity string, symbols []string) {
		h.Issues = append(h.Issues, domain.PatternForwardIssue{Key: code + ":" + date, Code: code, Date: date, Message: message, Severity: severity, Symbols: symbols})
	}
	sealedToday := false
	for _, day := range days {
		d := domain.PatternForwardDailyHealth{Date: day.Date, State: day.State, Expected: len(day.Items), Missing: []string{}, TrackingDone: !day.SealedAt.IsZero() && status.TrackingThrough >= day.Date}
		for _, item := range day.Items {
			if item.State == "captured" && item.SnapshotHash != "" {
				d.Captured++
			} else {
				d.Missing = append(d.Missing, item.Symbol)
			}
		}
		if day.Date == today {
			h.Captured = d.Captured
			sealedToday = !day.SealedAt.IsZero()
		}
		if !day.SealedAt.IsZero() {
			if d.Captured == d.Expected && day.SealedAt.After(h.LastSuccessAt) {
				h.LastSuccessAt = day.SealedAt
			}
			if len(d.Missing) > 0 {
				h.MissingDays++
				add("missing-capture", day.Date, fmt.Sprintf("%s 已封存，%d/%d只缺采", day.Date, len(d.Missing), d.Expected), "warning", d.Missing)
			}
			if !d.TrackingDone {
				h.TrackingLag++
			}
		} else if day.Date == today && len(d.Missing) > 0 && !day.LastAttemptAt.IsZero() {
			add("capture-pending", today, fmt.Sprintf("%d/%d只尚未留存，等待重试", len(d.Missing), d.Expected), "warning", d.Missing)
		}
		h.Days = append(h.Days, d)
	}
	sort.Slice(h.Days, func(i, j int) bool { return h.Days[i].Date > h.Days[j].Date })
	if !PatternForwardCompatible(task) {
		add("incompatible", "", "冻结规则与当前引擎不兼容，需新建任务", "error", nil)
	}
	if status.CalendarError != "" {
		add("calendar", today, status.CalendarError, "error", nil)
	}
	if status.CollectionError != "" && status.LastAttemptAt.In(chartLocation).Format(time.DateOnly) == today && !sealedToday {
		add("collection", today, status.CollectionError, "error", nil)
	}
	if status.TrackingError != "" {
		add("tracking", "", status.TrackingError, "error", nil)
	}
	if h.TrackingLag > 0 && status.TrackingError == "" {
		add("tracking-lag", "", fmt.Sprintf("%d个封存日等待追加结果日志", h.TrackingLag), "warning", nil)
	}
	if !control.Enabled {
		h.Phase, h.Detail = "paused", "任务已暂停，历史缺采记录保留"
		return h
	}
	if !PatternForwardCompatible(task) {
		h.Phase, h.Detail = "blocked", "冻结规则不兼容"
		return h
	}
	dates := realtime.NormalizeTradingDates(status.CalendarDates)
	openAt := func(date string) time.Time {
		at, _ := time.ParseInLocation(time.DateOnly, date, chartLocation)
		return at.Add(time.Duration(task.Config.OpenMinute) * time.Minute)
	}
	for _, date := range dates {
		if status.CalendarError != "" {
			break
		}
		if date < today || date < task.Config.StartOn || (date == today && sealedToday) {
			continue
		}
		candidate := openAt(date)
		deadline := candidate.Add(time.Duration(task.Config.CloseMinute-task.Config.OpenMinute) * time.Minute)
		if date == today {
			if !now.Before(deadline) {
				continue
			}
			if retry := status.LastAttemptAt.Add(time.Duration(task.Config.RetryMinutes) * time.Minute); retry.After(candidate) {
				candidate = retry
			}
			if candidate.Before(now) {
				candidate = now.Add(time.Minute)
			}
			if !candidate.Before(deadline) {
				continue
			}
		}
		h.NextAttemptAt = candidate
		break
	}
	if sealedToday {
		h.Phase, h.Detail = "complete", "当日采集已完成"
		if h.Captured < h.Expected {
			h.Phase, h.Detail = "incomplete", "当日窗口已结束，缺采不会回填"
		} else if h.TrackingLag > 0 {
			h.Phase, h.Detail = "tracking", "快照已封存，等待结果日志"
		}
		return h
	}
	if today < task.Config.StartOn || local.Hour()*60+local.Minute() < task.Config.OpenMinute {
		return h
	}
	index := sort.SearchStrings(dates, today)
	known := index < len(dates) && dates[index] == today
	if status.CalendarError != "" || !known {
		h.Phase, h.Detail = "calendar-pending", "当前交易日尚未确认，等待日历或当日基准日K"
		if status.CalendarError == "" && len(dates) > 0 && today >= dates[0] && today <= dates[len(dates)-1] {
			h.Phase, h.Detail = "closed", "日历显示今天为非交易日"
		}
		return h
	}
	if local.Hour()*60+local.Minute() >= task.Config.CloseMinute {
		h.Phase, h.Detail = "incomplete", "已过采集窗口，等待核对缺采记录"
		return h
	}
	h.Phase, h.Detail = "due", "等待收盘调度"
	if status.LastAttemptAt.In(chartLocation).Format(time.DateOnly) == today {
		h.Phase, h.Detail = "retrying", "等待后续重试"
	}
	return h
}

func AdvancePatternForwardOperations(current domain.PatternForwardOperations, issues []domain.PatternForwardIssue, now time.Time) domain.PatternForwardOperations {
	next := current
	next.Version, next.UpdatedAt = 1, now
	next.Events = append([]domain.PatternForwardAlert{}, current.Events...)
	next.Active = append([]domain.PatternForwardIssue{}, issues...)
	previous, active := map[string]domain.PatternForwardIssue{}, map[string]bool{}
	for _, issue := range current.Active {
		previous[issue.Key] = issue
	}
	appendEvent := func(kind string, issue domain.PatternForwardIssue) {
		next.Sequence++
		next.Events = append(next.Events, domain.PatternForwardAlert{Sequence: next.Sequence, Kind: kind, At: now, Issue: issue})
	}
	for _, issue := range issues {
		active[issue.Key] = true
		if _, existed := previous[issue.Key]; !existed {
			appendEvent("opened", issue)
		}
	}
	for _, issue := range current.Active {
		if !active[issue.Key] {
			appendEvent("resolved", issue)
		}
	}
	if len(next.Events) > 300 {
		next.Events = append([]domain.PatternForwardAlert{}, next.Events[len(next.Events)-300:]...)
	}
	return next
}
