package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type PlanMonitorStore struct{ root string }

func NewPlanMonitorStore(root string) *PlanMonitorStore { return &PlanMonitorStore{root: root} }

func (store *PlanMonitorStore) path(id string) (string, error) {
	if store == nil || strings.TrimSpace(store.root) == "" || !validTradePlanID(id) {
		return "", fmt.Errorf("监控目录或计划标识无效")
	}
	return filepath.Join(store.root, id+".json"), nil
}

func validatePlanMonitor(state domain.PlanMonitor) error {
	if state.Version != 1 || !validTradePlanID(state.PlanID) || !tradePlanSymbol.MatchString(state.Symbol) || state.Fingerprint == "" || state.Rule.Version != "plan-monitor-v1" || state.Rule.VolumeDays != 20 || state.Rule.MinimumVolume != 1.2 || state.Rule.CooldownSecs != 300 || state.Sequence != uint64(len(state.Events)) {
		return fmt.Errorf("计划监控格式无效")
	}
	if _, err := time.Parse(time.DateOnly, state.ExpiresOn); err != nil {
		return fmt.Errorf("监控有效期无效")
	}
	if _, err := time.Parse(time.DateOnly, state.AnalysisDate); err != nil {
		return fmt.Errorf("监控分析日期无效")
	}
	if state.UpdatedAt.IsZero() || state.Events == nil || (state.Enabled && state.EnabledAt.IsZero()) {
		return fmt.Errorf("监控时间或事件历史缺失")
	}
	switch state.Phase {
	case "waiting", "confirmed", "in_zone", "invalidated", "expired":
	default:
		return fmt.Errorf("监控阶段无效")
	}
	for _, price := range []float64{state.Rule.Levels.EntryLow, state.Rule.Levels.EntryHigh, state.Rule.Levels.Invalidation} {
		if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
			return fmt.Errorf("监控价格无效")
		}
	}
	if state.Rule.Levels.EntryLow > state.Rule.Levels.EntryHigh || state.Rule.Levels.Invalidation >= state.Rule.Levels.EntryLow {
		return fmt.Errorf("监控价格顺序无效")
	}
	for index, event := range state.Events {
		if event.Sequence != uint64(index+1) || event.ID != fmt.Sprintf("plan:%s:%d", state.PlanID, index+1) || event.ObservedAt.IsZero() {
			return fmt.Errorf("计划监控事件序列无效")
		}
	}
	return nil
}

func (store *PlanMonitorStore) Load(id string) (domain.PlanMonitor, error) {
	path, err := store.path(id)
	if err != nil {
		return domain.PlanMonitor{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return domain.PlanMonitor{}, nil
	}
	if err != nil {
		return domain.PlanMonitor{}, err
	}
	var state domain.PlanMonitor
	if err := json.Unmarshal(data, &state); err != nil {
		return domain.PlanMonitor{}, err
	}
	if state.PlanID != id {
		return domain.PlanMonitor{}, fmt.Errorf("监控文件与计划标识不一致")
	}
	return state, validatePlanMonitor(state)
}

// Update serializes configuration and polling across Web processes. Callbacks
// run under the file lock and must only compute state, never fetch market data.
func (store *PlanMonitorStore) Update(id string, update func(domain.PlanMonitor) (domain.PlanMonitor, error)) (domain.PlanMonitor, error) {
	path, err := store.path(id)
	if err != nil {
		return domain.PlanMonitor{}, err
	}
	if err := os.MkdirAll(store.root, 0o700); err != nil {
		return domain.PlanMonitor{}, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return domain.PlanMonitor{}, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return domain.PlanMonitor{}, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	current, err := store.Load(id)
	if err != nil {
		return domain.PlanMonitor{}, err
	}
	candidate := current
	candidate.Events = append([]domain.PlanMonitorEvent{}, current.Events...)
	for index := range candidate.Events {
		if candidate.Events[index].Price != nil {
			value := *candidate.Events[index].Price
			candidate.Events[index].Price = &value
		}
	}
	next, err := update(candidate)
	if err != nil {
		return current, err
	}
	if next.PlanID != id {
		return current, fmt.Errorf("监控更新试图改变计划标识")
	}
	if err := validatePlanMonitor(next); err != nil {
		return current, err
	}
	if current.Version != 0 {
		if next.Fingerprint != current.Fingerprint || next.Symbol != current.Symbol || next.ExpiresOn != current.ExpiresOn || next.AnalysisDate != current.AnalysisDate || !reflect.DeepEqual(next.Rule, current.Rule) || next.UpdatedAt.Before(current.UpdatedAt) || len(next.Events) < len(current.Events) || !reflect.DeepEqual(next.Events[:len(current.Events)], current.Events) {
			return current, fmt.Errorf("监控更新不能回退状态或改写历史事件、冻结规则")
		}
		if reflect.DeepEqual(current, next) {
			return current, nil
		}
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return current, err
	}
	if err := atomicWrite(path, append(data, '\n'), 0o600); err != nil {
		return current, err
	}
	return next, nil
}

func (store *PlanMonitorStore) List(symbol string) ([]domain.PlanMonitor, error) {
	if store == nil || strings.TrimSpace(store.root) == "" || (symbol != "" && !tradePlanSymbol.MatchString(symbol)) {
		return nil, fmt.Errorf("监控目录或股票代码无效")
	}
	entries, err := os.ReadDir(store.root)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.PlanMonitor{}, nil
	}
	if err != nil {
		return nil, err
	}
	states := make([]domain.PlanMonitor, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		state, err := store.Load(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, fmt.Errorf("读取监控 %s 失败: %w", entry.Name(), err)
		}
		if symbol == "" || state.Symbol == symbol {
			states = append(states, state)
		}
	}
	sort.Slice(states, func(i, j int) bool {
		if states[i].UpdatedAt.Equal(states[j].UpdatedAt) {
			return states[i].PlanID < states[j].PlanID
		}
		return states[i].UpdatedAt.After(states[j].UpdatedAt)
	})
	return states, nil
}
