package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/realtime"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type patternForwardArchive interface {
	Create(domain.PatternForwardTask) error
	Load(string) (domain.PatternForwardTask, error)
	Tasks() ([]domain.PatternForwardTask, error)
	Control(string) (domain.PatternForwardControl, error)
	SetControl(string, domain.PatternForwardControl) error
	Status(string) (domain.PatternForwardStatus, error)
	SaveStatus(string, domain.PatternForwardStatus) error
	Days(string) ([]domain.PatternForwardDay, error)
	Day(string, string) (domain.PatternForwardDay, error)
	SaveDay(domain.PatternForwardDay) error
	Benchmark(string, string) (domain.PatternForwardBenchmark, error)
	SaveBenchmark(domain.PatternForwardBenchmark) (domain.PatternForwardBenchmark, error)
	Snapshot(string, string, string) (domain.PatternForwardSnapshot, error)
	SaveSnapshot(domain.PatternForwardSnapshot) (domain.PatternForwardSnapshot, error)
	Tracking(string) ([]domain.PatternForwardTracking, error)
	SaveTracking(domain.PatternForwardTracking) (domain.PatternForwardTracking, error)
	Acquire() (func(), error)
}

type patternForwardView struct {
	Task       domain.PatternForwardTask    `json:"task"`
	Control    domain.PatternForwardControl `json:"control"`
	Status     domain.PatternForwardStatus  `json:"status"`
	Days       []domain.PatternForwardDay   `json:"days"`
	CanCollect bool                         `json:"can_collect"`
}

func WithPatternForwards(store patternForwardArchive) ServerOption {
	return func(s *Server) { s.patternForwards = store }
}
func (s *Server) wakePatternForwards() {
	select {
	case s.patternForwardWake <- struct{}{}:
	default:
	}
}

func (s *Server) forwardView(task domain.PatternForwardTask) (patternForwardView, error) {
	v := patternForwardView{Task: task}
	var err error
	if v.Control, err = s.patternForwards.Control(task.ID); err != nil {
		return v, err
	}
	if v.Status, err = s.patternForwards.Status(task.ID); err != nil {
		return v, err
	}
	if v.Days, err = s.patternForwards.Days(task.ID); err != nil {
		return v, err
	}
	s.patternForwardStateMu.Lock()
	if message := s.patternForwardErrors[task.ID]; message != "" {
		v.Status.Error = message
	}
	s.patternForwardStateMu.Unlock()
	v.CanCollect = v.Control.Enabled && strategy.PatternForwardWindow(task, s.currentTime()) && strategy.PatternForwardCompatible(task)
	if !v.Status.LastAttemptAt.IsZero() && s.currentTime().Sub(v.Status.LastAttemptAt) < time.Duration(task.Config.RetryMinutes)*time.Minute {
		v.CanCollect = false
	}
	for _, day := range v.Days {
		if day.Date == s.currentTime().In(realtimeWebLocation).Format(time.DateOnly) && !day.SealedAt.IsZero() {
			v.CanCollect = false
		}
	}
	return v, nil
}

func (s *Server) handlePatternForwards(w http.ResponseWriter, r *http.Request) {
	if s.patternForwards == nil || s.patternBatches == nil {
		writeJSON(w, 503, errorResponse{Error: "前向研究存储未初始化"})
		return
	}
	if r.Method == http.MethodGet {
		if id := r.URL.Query().Get("id"); id != "" {
			task, err := s.patternForwards.Load(id)
			if err != nil {
				writeJSON(w, 404, errorResponse{Error: err.Error()})
				return
			}
			if r.URL.Query().Get("tracking") == "1" {
				s.forwardTrackingResponse(w, r, task)
				return
			}
			if date := r.URL.Query().Get("date"); date != "" {
				s.forwardDayResponse(w, r, task, date)
				return
			}
			v, err := s.forwardView(task)
			if err != nil {
				writeJSON(w, 500, errorResponse{Error: err.Error()})
				return
			}
			writeJSON(w, 200, v)
			return
		}
		tasks, err := s.patternForwards.Tasks()
		if err != nil {
			writeJSON(w, 500, errorResponse{Error: err.Error()})
			return
		}
		views := []patternForwardView{}
		for i := len(tasks) - 1; i >= 0; i-- {
			v, err := s.forwardView(tasks[i])
			if err != nil {
				writeJSON(w, 500, errorResponse{Error: err.Error()})
				return
			}
			views = append(views, v)
		}
		writeJSON(w, 200, map[string]any{"items": views})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, errorResponse{Error: "只支持GET、POST"})
		return
	}
	var input struct {
		Action      string `json:"action"`
		ID          string `json:"id"`
		PoolVersion string `json:"pool_version"`
		Name        string `json:"name"`
		PriceBasis  string `json:"price_basis"`
	}
	if err := decodePatternBatch(w, r, &input); err != nil {
		writeJSON(w, 400, errorResponse{Error: err.Error()})
		return
	}
	if input.Action == "create" {
		pool, err := s.patternBatches.LoadPool(input.PoolVersion)
		if err != nil {
			writeJSON(w, 400, errorResponse{Error: err.Error()})
			return
		}
		task, err := strategy.NewPatternForwardTask(input.Name, pool, input.PriceBasis, s.currentTime())
		if err != nil {
			writeJSON(w, 400, errorResponse{Error: err.Error()})
			return
		}
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			writeJSON(w, 500, errorResponse{Error: err.Error()})
			return
		}
		task.ID = hex.EncodeToString(id[:])
		if err = s.patternForwards.Create(task); err != nil {
			writeJSON(w, 500, errorResponse{Error: err.Error()})
			return
		}
		s.wakePatternForwards()
		v, err := s.forwardView(task)
		if err != nil {
			writeJSON(w, 500, errorResponse{Error: err.Error()})
			return
		}
		writeJSON(w, 201, v)
		return
	}
	task, err := s.patternForwards.Load(input.ID)
	if err != nil {
		writeJSON(w, 404, errorResponse{Error: err.Error()})
		return
	}
	switch input.Action {
	case "pause", "enable":
		if err = s.patternForwards.SetControl(task.ID, domain.PatternForwardControl{Enabled: input.Action == "enable", UpdatedAt: s.currentTime()}); err != nil {
			writeJSON(w, 500, errorResponse{Error: err.Error()})
			return
		}
		if input.Action == "pause" {
			s.patternForwardStateMu.Lock()
			if cancel := s.patternForwardCancels[task.ID]; cancel != nil {
				cancel()
			}
			s.patternForwardStateMu.Unlock()
		}
	case "collect":
		v, err := s.forwardView(task)
		if err != nil || !v.CanCollect {
			writeJSON(w, 409, errorResponse{Error: "当前不在已启用任务的当日收盘采集窗口"})
			return
		}
	default:
		writeJSON(w, 400, errorResponse{Error: "无效前向任务操作"})
		return
	}
	s.wakePatternForwards()
	writeJSON(w, 202, map[string]string{"status": "scheduled"})
}

func (s *Server) forwardDayResponse(w http.ResponseWriter, r *http.Request, task domain.PatternForwardTask, date string) {
	day, err := s.patternForwards.Day(task.ID, date)
	if err != nil {
		writeJSON(w, 404, errorResponse{Error: err.Error()})
		return
	}
	stocks := []domain.PatternForwardSnapshot{}
	for _, item := range day.Items {
		if item.SnapshotHash == "" {
			continue
		}
		snapshot, err := s.patternForwards.Snapshot(task.ID, date, item.Symbol)
		if err != nil || snapshot.Hash != item.SnapshotHash {
			writeJSON(w, 500, errorResponse{Error: "采集快照损坏或指纹不一致"})
			return
		}
		if r.URL.Query().Get("download") != "1" {
			snapshot.Input.Bars = nil
		}
		stocks = append(stocks, snapshot)
	}
	var benchmark *domain.PatternForwardBenchmark
	if value, err := s.patternForwards.Benchmark(task.ID, date); err == nil {
		benchmark = &value
		if r.URL.Query().Get("download") != "1" {
			benchmark.Input.Bars = nil
		}
	} else if !os.IsNotExist(err) {
		writeJSON(w, 500, errorResponse{Error: err.Error()})
		return
	}
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"forward-%s-%s.json\"", task.ID, date))
	}
	writeJSON(w, 200, map[string]any{"task": task, "day": day, "benchmark": benchmark, "stocks": stocks})
}

func (s *Server) runPatternForwardLoop(ctx context.Context) {
	if s.patternForwards == nil {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.runPatternForwardCycle(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.patternForwardWake:
		}
	}
}

func (s *Server) recordForwardStatus(id string, status domain.PatternForwardStatus) {
	err := s.patternForwards.SaveStatus(id, status)
	s.patternForwardStateMu.Lock()
	defer s.patternForwardStateMu.Unlock()
	if s.patternForwardErrors == nil {
		s.patternForwardErrors = map[string]string{}
	}
	if err != nil {
		s.patternForwardErrors[id] = "运行状态写入失败: " + err.Error()
	} else {
		delete(s.patternForwardErrors, id)
	}
}

func (s *Server) runPatternForwardCycle(ctx context.Context) {
	if s.patternForwards == nil || !s.patternForwardMu.TryLock() {
		return
	}
	defer s.patternForwardMu.Unlock()
	unlock, err := s.patternForwards.Acquire()
	if err != nil {
		return
	}
	defer unlock()
	tasks, err := s.patternForwards.Tasks()
	if err != nil {
		return
	}
	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}
		now := s.currentTime()
		if now.In(realtimeWebLocation).Format(time.DateOnly) < task.Config.StartOn {
			continue
		}
		status, statusErr := s.patternForwards.Status(task.ID)
		if statusErr != nil {
			status.Error = statusErr.Error()
			s.recordForwardStatus(task.ID, status)
			continue
		}
		status.CheckedAt = now
		calendarCtx, stop := context.WithTimeout(ctx, 15*time.Second)
		var dates []string
		if s.tradingCalendarProvider != nil {
			dates, err = s.tradingCalendarProvider(calendarCtx, now)
			status.CalendarSource = "配置交易日历"
		} else {
			dates, err = s.tradingCalendar(calendarCtx, now)
			status.CalendarSource = "沪深300日K日期参照"
		}
		stop()
		dates = realtime.NormalizeTradingDates(dates)
		if err != nil || (len(dates) == 0 && s.tradingCalendarProvider != nil) {
			status.Error = "交易日历不可用，未推断工作日"
			if e := s.reconcileForwardDays(task, nil, now, status.CalendarSource); e != nil {
				status.Error = e.Error()
			}
			s.recordForwardStatus(task.ID, status)
			continue
		}
		if len(dates) > 0 {
			status.CalendarStart, status.CalendarEnd = dates[0], dates[len(dates)-1]
		}
		if err = s.reconcileForwardDays(task, dates, now, status.CalendarSource); err != nil {
			status.Error = err.Error()
			s.recordForwardStatus(task.ID, status)
			continue
		}
		control, err := s.patternForwards.Control(task.ID)
		if err != nil {
			status.Error = err.Error()
			s.recordForwardStatus(task.ID, status)
			continue
		}
		due := status.LastAttemptAt.IsZero() || now.Sub(status.LastAttemptAt) >= time.Duration(task.Config.RetryMinutes)*time.Minute
		if control.Enabled && strategy.PatternForwardWindow(task, now) && due {
			if !strategy.PatternForwardCompatible(task) {
				status.Error = "规则版本已变更，需新建前向任务"
			} else if s.patternValidationMu.TryLock() {
				status.LastAttemptAt, status.Error = now, ""
				err = s.patternForwards.SaveStatus(task.ID, status)
				if err == nil {
					err = s.collectPatternForward(ctx, task, dates, status.CalendarSource)
				}
				s.patternValidationMu.Unlock()
				if err != nil {
					status.Error = err.Error()
				}
			} else {
				status.Error = "形态验证正忙，等待后续收盘调度"
			}
		}
		if s.tradingCalendarProvider == nil {
			if benchmark, e := s.patternForwards.Benchmark(task.ID, now.In(realtimeWebLocation).Format(time.DateOnly)); e == nil {
				dates = realtime.TradingDatesFromBars(benchmark.Input.Bars)
				status.CalendarSource = "沪深300当日日K确认"
			}
		}
		if len(dates) == 0 {
			status.Error = "尚未取得交易日历，缺采覆盖等待核验"
		}
		if len(dates) > 0 {
			status.CalendarStart, status.CalendarEnd = dates[0], dates[len(dates)-1]
		}
		if control.Enabled {
			through, trackingErr := s.reconcileForwardTracking(ctx, task, dates)
			status.TrackingThrough, status.TrackingError = through, ""
			if trackingErr != nil {
				status.TrackingError = trackingErr.Error()
			}
		}
		s.recordForwardStatus(task.ID, status)
	}
}

func newForwardDay(task domain.PatternForwardTask, date, source string) domain.PatternForwardDay {
	d := domain.PatternForwardDay{TaskID: task.ID, Date: date, State: "open", CalendarSource: source, Items: []domain.PatternForwardItem{}}
	for _, m := range task.Pool.Members {
		d.Items = append(d.Items, domain.PatternForwardItem{Symbol: m.Symbol, State: "pending", SampleIDs: []string{}})
	}
	return d
}

func (s *Server) recoverForwardItems(task domain.PatternForwardTask, day *domain.PatternForwardDay) error {
	for i := range day.Items {
		item := &day.Items[i]
		if item.State == "captured" {
			continue
		}
		snapshot, err := s.patternForwards.Snapshot(task.ID, day.Date, item.Symbol)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if snapshot.ConfigHash != task.ConfigHash {
			return fmt.Errorf("观察快照配置不匹配")
		}
		item.State, item.SnapshotHash, item.CollectedAt, item.Error = "captured", snapshot.Hash, snapshot.CollectedAt, ""
		item.SampleIDs = []string{}
		for _, sample := range snapshot.Samples {
			item.SampleIDs = append(item.SampleIDs, sample.ID)
		}
	}
	return nil
}

func sealForwardDay(day *domain.PatternForwardDay, now time.Time, expired bool) {
	captured := 0
	for i := range day.Items {
		item := &day.Items[i]
		if item.State == "captured" {
			captured++
		} else if expired {
			item.State = "missing"
			if item.Error == "" {
				item.Error = "未在当日窗口内完成采集"
			}
		}
	}
	if captured == len(day.Items) {
		day.State, day.SealedAt = "captured", now
	} else if expired {
		day.State, day.SealedAt = "partial", now
		if captured == 0 {
			day.State = "missed"
		}
		day.Error = "缺采项未回填历史信号"
	}
}

func (s *Server) reconcileForwardDays(task domain.PatternForwardTask, dates []string, now time.Time, source string) error {
	today := now.In(realtimeWebLocation).Format(time.DateOnly)
	minutes := now.In(realtimeWebLocation).Hour()*60 + now.In(realtimeWebLocation).Minute()
	days, err := s.patternForwards.Days(task.ID)
	if err != nil {
		return err
	}
	known := map[string]domain.PatternForwardDay{}
	for _, day := range days {
		known[day.Date] = day
		dates = append(dates, day.Date)
	}
	dates = realtime.NormalizeTradingDates(dates)
	for _, date := range dates {
		if date < task.Config.StartOn || date > today || (date == today && minutes < task.Config.CloseMinute) {
			continue
		}
		day, exists := known[date]
		if exists && !day.SealedAt.IsZero() {
			continue
		}
		if !exists {
			day = newForwardDay(task, date, source)
		}
		if err = s.recoverForwardItems(task, &day); err != nil {
			return err
		}
		sealForwardDay(&day, now, true)
		if err = s.patternForwards.SaveDay(day); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) collectPatternForward(ctx context.Context, task domain.PatternForwardTask, dates []string, source string) error {
	now := s.currentTime()
	local := now.In(realtimeWebLocation)
	date := local.Format(time.DateOnly)
	if !strategy.PatternForwardWindow(task, now) {
		return nil
	}
	index := sort.SearchStrings(dates, date)
	if s.tradingCalendarProvider != nil && (index == len(dates) || dates[index] != date) {
		return fmt.Errorf("配置日历未确认当前交易日")
	}
	day, err := s.patternForwards.Day(task.ID, date)
	if os.IsNotExist(err) {
		day = newForwardDay(task, date, source)
	} else if err != nil {
		return err
	}
	if !day.SealedAt.IsZero() {
		return nil
	}
	if !day.LastAttemptAt.IsZero() && now.Sub(day.LastAttemptAt) < time.Duration(task.Config.RetryMinutes)*time.Minute {
		return nil
	}
	deadline := time.Date(local.Year(), local.Month(), local.Day(), task.Config.CloseMinute/60, task.Config.CloseMinute%60, 0, 0, realtimeWebLocation)
	collectCtx, cancel := context.WithTimeout(ctx, min(deadline.Sub(local), 30*time.Minute))
	defer cancel()
	s.patternForwardStateMu.Lock()
	if s.patternForwardCancels == nil {
		s.patternForwardCancels = map[string]context.CancelFunc{}
	}
	s.patternForwardCancels[task.ID] = cancel
	s.patternForwardStateMu.Unlock()
	defer func() {
		s.patternForwardStateMu.Lock()
		delete(s.patternForwardCancels, task.ID)
		s.patternForwardStateMu.Unlock()
	}()
	request := domain.PatternValidationRequest{Start: task.Config.StartOn, End: date, PriceBasis: task.Config.PriceBasis}
	benchmark, err := s.patternForwards.Benchmark(task.ID, date)
	if os.IsNotExist(err) {
		input := s.fetchPatternValidationInput(collectCtx, "sh000300", request)
		input, prepareErr := strategy.PreparePatternForwardBenchmark(input, date)
		if prepareErr != nil {
			return prepareErr
		}
		if !strategy.PatternForwardWindow(task, s.currentTime()) || s.currentTime().In(realtimeWebLocation).Format(time.DateOnly) != date {
			return fmt.Errorf("已越过当日采集窗口")
		}
		if s.tradingCalendarProvider == nil {
			dates = realtime.TradingDatesFromBars(input.Bars)
			source = "沪深300当日日K确认"
			day.CalendarSource = source
			if err = s.reconcileForwardDays(task, dates, s.currentTime(), source); err != nil {
				return err
			}
		}
		benchmark, err = s.patternForwards.SaveBenchmark(domain.PatternForwardBenchmark{TaskID: task.ID, Date: date, CollectedAt: s.currentTime(), Input: input})
	}
	if err != nil {
		return err
	}
	day.LastAttemptAt = now
	day.Error = ""
	if err = s.recoverForwardItems(task, &day); err != nil {
		return err
	}
	if err = s.patternForwards.SaveDay(day); err != nil {
		return err
	}
	days, err := s.patternForwards.Days(task.ID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, previous := range days {
		for _, item := range previous.Items {
			for _, id := range item.SampleIDs {
				seen[id] = true
			}
		}
	}
	for i := range day.Items {
		item := &day.Items[i]
		if item.State == "captured" {
			continue
		}
		control, err := s.patternForwards.Control(task.ID)
		if err != nil {
			return err
		}
		if !control.Enabled || collectCtx.Err() != nil {
			break
		}
		if s.currentTime().In(realtimeWebLocation).Format(time.DateOnly) != date || !strategy.PatternForwardWindow(task, s.currentTime()) {
			break
		}
		item.Attempts++
		input := s.fetchPatternValidationInput(collectCtx, item.Symbol, request)
		capturedAt := s.currentTime()
		snapshot, buildErr := strategy.BuildPatternForwardSnapshot(collectCtx, task, date, input, benchmark, capturedAt, seen)
		if buildErr != nil {
			item.Error = buildErr.Error()
		} else {
			control, err = s.patternForwards.Control(task.ID)
			if err != nil {
				return err
			}
			if !control.Enabled || collectCtx.Err() != nil || !strategy.PatternForwardWindow(task, s.currentTime()) || s.currentTime().In(realtimeWebLocation).Format(time.DateOnly) != date {
				break
			}
			snapshot, err = s.patternForwards.SaveSnapshot(snapshot)
			if err != nil {
				return err
			}
			item.State, item.Error, item.SnapshotHash, item.CollectedAt = "captured", "", snapshot.Hash, snapshot.CollectedAt
			item.SampleIDs = []string{}
			for _, sample := range snapshot.Samples {
				seen[sample.ID] = true
				item.SampleIDs = append(item.SampleIDs, sample.ID)
			}
		}
		if err = s.patternForwards.SaveDay(day); err != nil {
			return err
		}
	}
	finished := s.currentTime()
	expired := finished.In(realtimeWebLocation).Format(time.DateOnly) != date || !finished.Before(deadline)
	sealForwardDay(&day, finished, expired)
	return s.patternForwards.SaveDay(day)
}
