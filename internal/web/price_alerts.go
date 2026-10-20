package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/backtest"
	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/notify"
	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type priceAlertRuntime struct {
	Running         bool      `json:"running"`
	IntervalSeconds int       `json:"interval_seconds"`
	LastCycleAt     time.Time `json:"last_cycle_at,omitzero"`
	Session         string    `json:"session"`
	Error           string    `json:"error,omitempty"`
}

func WithPriceAlerts(store *storage.PriceAlertStore) ServerOption {
	return func(s *Server) { s.priceAlerts = store; s.priceAlertWake = make(chan struct{}, 1) }
}

func (s *Server) priceAlertIntervalValue() time.Duration {
	if s.priceAlertInterval > 0 {
		return s.priceAlertInterval
	}
	return 30 * time.Second
}
func (s *Server) wakePriceAlerts() {
	select {
	case s.priceAlertWake <- struct{}{}:
	default:
	}
}

func (s *Server) runPriceAlertLoop(ctx context.Context) {
	if s.priceAlerts == nil {
		return
	}
	ticker := time.NewTicker(s.priceAlertIntervalValue())
	defer ticker.Stop()
	var unlock func()
	defer func() {
		if unlock != nil {
			unlock()
		}
		s.priceAlertMu.Lock()
		s.priceAlertRuntime.Running = false
		s.priceAlertMu.Unlock()
	}()
	for {
		if ctx.Err() != nil {
			return
		}
		if unlock == nil {
			var err error
			unlock, err = s.priceAlerts.AcquireWorker()
			s.priceAlertMu.Lock()
			s.priceAlertRuntime.Running = err == nil
			if err != nil {
				s.priceAlertRuntime.Error = "另一进程正在检查，或检查锁不可用"
			} else {
				s.priceAlertRuntime.Error = ""
			}
			s.priceAlertMu.Unlock()
		}
		if unlock != nil {
			s.runPriceAlertCycle(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.priceAlertWake:
		}
	}
}

func (s *Server) runPriceAlertCycle(ctx context.Context) {
	if s.priceAlerts == nil || !s.priceAlertCycleMu.TryLock() {
		return
	}
	defer s.priceAlertCycleMu.Unlock()
	now := s.currentTime()
	var cycleErr error
	defer func() {
		s.priceAlertMu.Lock()
		defer s.priceAlertMu.Unlock()
		s.priceAlertRuntime.LastCycleAt = now
		s.priceAlertRuntime.Session = "closed"
		if strategy.PriceAlertSession(now) {
			s.priceAlertRuntime.Session = "trading"
		}
		s.priceAlertRuntime.Error = ""
		if cycleErr != nil {
			s.priceAlertRuntime.Error = cycleErr.Error()
		}
	}()
	state, err := s.priceAlerts.Load()
	if err != nil {
		cycleErr = err
		return
	}
	symbols := []string{}
	for _, alert := range state.Alerts {
		if strategy.PriceAlertPhase(alert, now) == "active" && !slices.Contains(symbols, alert.Rule.Symbol) {
			symbols = append(symbols, alert.Rule.Symbol)
		}
	}
	quotes := map[string]domain.Quote{}
	fetchError := ""
	if len(symbols) > 0 && strategy.PriceAlertSession(now) {
		fetchCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		quotes, err = s.fetchQuoteBatch(fetchCtx, symbols)
		cancel()
		if err != nil {
			fetchError = "价位提醒行情暂不可用"
			cycleErr = err
		}
	}
	if ctx.Err() != nil {
		return
	}
	now = s.currentTime()
	next, err := s.priceAlerts.Update(func(current *domain.PriceAlertState) error {
		for id := range state.Alerts {
			alert, exists := current.Alerts[id]
			if !exists {
				continue
			}
			message := fetchError
			quote, ok := quotes[alert.Rule.Symbol]
			if !ok && message == "" && strategy.PriceAlertSession(now) {
				message = "未取得该股票报价"
			}
			current.Alerts[id] = strategy.AdvancePriceAlert(alert, now, quote, message)
		}
		return nil
	})
	if err != nil {
		cycleErr = err
		return
	}
	// A persisted trigger remains an outbox item after a crash or full queue.
	// The shared dispatcher deduplicates its stable ID and handles delivery retries.
	if s.nativeNotifications != nil {
		for _, alert := range next.Alerts {
			if alert.Event != nil && alert.DeletedAt.IsZero() && notify.Fresh(alert.Event.ObservedAt, now) {
				s.nativeNotifications.Offer(priceAlertMessage(alert))
			}
		}
	}
}

func priceAlertMessage(alert domain.PriceAlert) notify.Message {
	operator := "达到或高于"
	if alert.Rule.Direction == "below" {
		operator = "达到或低于"
	}
	return notify.Message{ID: alert.Event.ID, Kind: "price-alert", Symbol: alert.Rule.Symbol, At: alert.Event.ObservedAt, Title: "ASTOCK · 价位提醒", Subtitle: alert.Rule.Name + " " + alert.Rule.Symbol, Body: fmt.Sprintf("现价 %.3f · %s %.3f\n报价 %s", alert.Event.Price, operator, alert.Rule.Price, alert.Event.QuoteAt.In(realtimeWebLocation).Format("2006-01-02 15:04:05"))}
}

func (s *Server) verifyPriceAlertSource(rule domain.PriceAlertRule) error {
	if rule.Source == nil {
		return nil
	}
	for _, source := range s.frozenReplaySources {
		if source.ID != rule.Source.ID {
			continue
		}
		plan, _, _, err := storage.NewVolumeForwardStore(source.Directory).Load()
		if err != nil {
			return fmt.Errorf("来源归档不可用")
		}
		if !slices.Contains(plan.Baseline.Tickers, rule.Symbol) {
			return fmt.Errorf("股票不在来源股票池内")
		}
		data, err := os.ReadFile(filepath.Join(source.Directory, "days", rule.Source.Date+".json"))
		if err != nil {
			return fmt.Errorf("来源日档不可用")
		}
		var day backtest.VolumeForwardDay
		if json.Unmarshal(data, &day) != nil || backtest.ValidateVolumeForwardDay(plan, day) != nil || day.Fingerprint != rule.Source.Fingerprint || day.Date != rule.Source.Date || day.CapturedAt.After(s.currentTime()) {
			return fmt.Errorf("来源日档指纹或时间不符")
		}
		return nil
	}
	return fmt.Errorf("来源归档未配置")
}

func (s *Server) handlePriceAlerts(w http.ResponseWriter, r *http.Request) {
	if s.priceAlerts == nil {
		writeJSON(w, 503, errorResponse{Error: "后台价位提醒未配置"})
		return
	}
	if r.Method == http.MethodGet {
		s.writePriceAlerts(w)
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, errorResponse{Error: "价位提醒仅支持GET、POST"})
		return
	}
	var input struct {
		Action    string                `json:"action"`
		ClientID  string                `json:"client_id"`
		Confirmed bool                  `json:"confirmed"`
		Rule      domain.PriceAlertRule `json:"rule"`
		ID        string                `json:"id"`
		Enabled   *bool                 `json:"enabled"`
		Revision  uint64                `json:"revision"`
	}
	if !decodePlanMonitorMutation(w, r, &input) {
		return
	}
	now := s.currentTime()
	id := input.ID
	if input.Action == "create" {
		if !input.Confirmed || !regexp.MustCompile(`^[a-zA-Z0-9-]{8,80}$`).MatchString(input.ClientID) {
			writeJSON(w, 400, errorResponse{Error: "请明确确认规则，并提供请求标识"})
			return
		}
		if s.resolver == nil {
			writeJSON(w, 503, errorResponse{Error: "股票解析服务未配置"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		symbol, err := s.resolver.Resolve(ctx, strings.TrimSpace(input.Rule.Symbol))
		if err != nil {
			writeJSON(w, 400, errorResponse{Error: "股票无法识别"})
			return
		}
		input.Rule.Symbol = symbol
		input.Rule.Name = strings.TrimSpace(input.Rule.Name)
		if input.Rule.Name == "" {
			input.Rule.Name = symbol
		}
		if err := strategy.ValidatePriceAlertRule(input.Rule); err != nil {
			writeJSON(w, 400, errorResponse{Error: err.Error()})
			return
		}
		today := now.In(realtimeWebLocation).Format(time.DateOnly)
		if input.Rule.ExpiresOn < today || input.Rule.ExpiresOn > now.In(realtimeWebLocation).AddDate(1, 0, 0).Format(time.DateOnly) {
			writeJSON(w, 400, errorResponse{Error: "有效期须在今天至一年内"})
			return
		}
		if err := s.verifyPriceAlertSource(input.Rule); err != nil {
			writeJSON(w, 409, errorResponse{Error: err.Error()})
			return
		}
		id = "price-" + input.ClientID
	} else if input.Action != "enable" && input.Action != "delete" {
		writeJSON(w, 400, errorResponse{Error: "未知提醒操作"})
		return
	}
	state, err := s.priceAlerts.Update(func(state *domain.PriceAlertState) error {
		alert, exists := state.Alerts[id]
		if input.Action == "create" {
			if exists {
				if !alert.DeletedAt.IsZero() {
					return fmt.Errorf("该请求对应的提醒已删除，请重新新建草案")
				}
				if !reflect.DeepEqual(alert.Rule, input.Rule) {
					return fmt.Errorf("相同请求标识不能创建不同规则")
				}
				return nil
			}
			active := 0
			for _, item := range state.Alerts {
				phase := strategy.PriceAlertPhase(item, now)
				if phase == "active" || phase == "paused" {
					active++
				}
			}
			if active >= 100 {
				return fmt.Errorf("最多保留100条未结束提醒")
			}
			state.Alerts[id] = domain.PriceAlert{ID: id, Rule: input.Rule, Revision: 1, CreatedAt: now, UpdatedAt: now, EnabledAt: now, Enabled: true, CheckState: "waiting", CheckDetail: "等待启用后的新报价"}
			return nil
		}
		if !exists || !alert.DeletedAt.IsZero() {
			return fmt.Errorf("提醒不存在或已删除")
		}
		if input.Revision != alert.Revision {
			return fmt.Errorf("提醒状态已更新，请刷新后重试")
		}
		if input.Action == "delete" {
			alert.Enabled = false
			alert.DeletedAt = now
		} else {
			if input.Enabled == nil {
				return fmt.Errorf("必须明确指定启用或暂停")
			}
			if alert.Event != nil || strategy.PriceAlertPhase(alert, now) == "expired" {
				return fmt.Errorf("提醒已结束，不能更改启停状态；需要继续监控请复制为新规则")
			}
			alert.Enabled = *input.Enabled
			if alert.Enabled {
				alert.EnabledAt = now
				alert.CheckState = "waiting"
				alert.CheckDetail = "等待启用后的新报价"
			} else {
				alert.CheckState = "paused"
				alert.CheckDetail = "已暂停"
			}
		}
		alert.Revision++
		alert.UpdatedAt = now
		state.Alerts[id] = alert
		return nil
	})
	if err != nil {
		writeJSON(w, 409, errorResponse{Error: err.Error()})
		return
	}
	s.wakePriceAlerts()
	writeJSON(w, 200, struct {
		Alert domain.PriceAlert `json:"alert"`
	}{state.Alerts[id]})
}

func (s *Server) writePriceAlerts(w http.ResponseWriter) {
	state, err := s.priceAlerts.Load()
	if err != nil {
		writeJSON(w, 500, errorResponse{Error: "价位提醒记录不可用: " + err.Error()})
		return
	}
	type item struct {
		domain.PriceAlert
		Phase        string         `json:"phase"`
		Notification *notify.Record `json:"notification,omitempty"`
	}
	items := []item{}
	var records notify.State
	warning := ""
	if s.notificationRecords != nil {
		records, err = s.notificationRecords.Load()
		if err != nil {
			warning = "系统通知记录暂不可用"
		}
	}
	for _, alert := range state.Alerts {
		phase := strategy.PriceAlertPhase(alert, s.currentTime())
		if phase == "deleted" {
			continue
		}
		row := item{PriceAlert: alert, Phase: phase}
		if alert.Event != nil {
			if record, ok := records.Records[alert.Event.ID]; ok {
				copy := record
				row.Notification = &copy
			}
		}
		items = append(items, row)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	s.priceAlertMu.Lock()
	runtime := s.priceAlertRuntime
	s.priceAlertMu.Unlock()
	runtime.IntervalSeconds = int(s.priceAlertIntervalValue() / time.Second)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, struct {
		Items         []item            `json:"items"`
		Runtime       priceAlertRuntime `json:"runtime"`
		NativeEnabled bool              `json:"native_enabled"`
		Warning       string            `json:"warning,omitempty"`
	}{items, runtime, s.nativeNotifications != nil, warning})
}
