package storage

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type TradePlanStore struct {
	root string
}

var tradePlanSymbol = regexp.MustCompile(`^(sh|sz|bj)[0-9]{6}$`)

func NewTradePlanStore(root string) *TradePlanStore {
	return &TradePlanStore{root: root}
}

func (store *TradePlanStore) directory(symbol string) (string, error) {
	if strings.TrimSpace(store.root) == "" || !tradePlanSymbol.MatchString(symbol) {
		return "", fmt.Errorf("交易计划目录或股票代码无效")
	}
	return filepath.Join(store.root, symbol), nil
}

func validateStoredTradePlan(plan domain.TradePlan) error {
	id, err := hex.DecodeString(plan.ID)
	if err != nil || len(id) != 32 || plan.ID != strings.ToLower(plan.ID) || plan.Version != 1 || !tradePlanSymbol.MatchString(plan.Symbol) || plan.Analysis.Symbol != plan.Symbol || plan.Analysis.Fingerprint == "" || plan.Structure.Plan == nil || plan.CreatedAt.IsZero() {
		return fmt.Errorf("交易计划快照格式无效")
	}
	if pattern := plan.Structure.Pattern; pattern != nil {
		if pattern.Version != "classic-v1" || pattern.Bias != "bullish" || pattern.ReadyOn == "" || plan.MonitorRule == nil || plan.MonitorRule.PatternReadyOn != pattern.ReadyOn {
			return fmt.Errorf("经典形态计划缺少冻结的机器条件")
		}
	}
	if plan.MonitorRule != nil {
		rule := plan.MonitorRule
		if rule.Version != "plan-monitor-v1" || rule.StructureID != plan.Structure.ID || rule.Levels != *plan.Structure.Plan ||
			(rule.Kind != "breakout" && rule.Kind != "pullback") || strings.TrimSpace(rule.Description) == "" ||
			rule.VolumeDays < 5 || rule.VolumeDays > 60 || rule.MinimumVolume < .5 || rule.MinimumVolume > 5 ||
			rule.CooldownSecs < 60 || rule.CooldownSecs > 3600 || math.IsNaN(rule.BreakoutPrice) || math.IsInf(rule.BreakoutPrice, 0) ||
			(rule.Kind == "breakout" && rule.BreakoutPrice <= 0) {
			return fmt.Errorf("交易计划自定义监控规则无效")
		}
		if rule.PatternReadyOn != "" {
			pattern := plan.Structure.Pattern
			if _, err := time.Parse(time.DateOnly, rule.PatternReadyOn); err != nil || rule.PatternReadyOn > plan.Analysis.DataDate || rule.Kind != "breakout" || pattern == nil || pattern.Version != "classic-v1" || pattern.Bias != "bullish" || rule.PatternReadyOn != pattern.ReadyOn || rule.BreakoutPrice != pattern.TriggerPrice || rule.Levels.Invalidation != pattern.InvalidationPrice {
				return fmt.Errorf("经典形态计划与冻结规则不一致")
			}
		}
	}
	return nil
}

func validTradePlanID(id string) bool {
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) == 32 && id == strings.ToLower(id)
}

func (store *TradePlanStore) Load(symbol, id string) (domain.TradePlan, error) {
	if !validTradePlanID(id) {
		return domain.TradePlan{}, fmt.Errorf("交易计划标识无效")
	}
	directory, err := store.directory(symbol)
	if err != nil {
		return domain.TradePlan{}, err
	}
	plan, err := loadTradePlan(filepath.Join(directory, id+".json"))
	if err == nil && (plan.Symbol != symbol || plan.ID != id) {
		err = fmt.Errorf("交易计划文件与快照不一致")
	}
	return plan, err
}

func (store *TradePlanStore) Save(plan domain.TradePlan) (domain.TradePlan, bool, error) {
	if err := validateStoredTradePlan(plan); err != nil {
		return domain.TradePlan{}, false, err
	}
	directory, err := store.directory(plan.Symbol)
	if err != nil {
		return domain.TradePlan{}, false, err
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return domain.TradePlan{}, false, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return domain.TradePlan{}, false, err
	}
	temporary, err := os.CreateTemp(directory, ".plan-*.tmp")
	if err != nil {
		return domain.TradePlan{}, false, err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	if err := temporary.Chmod(0o600); err != nil {
		return domain.TradePlan{}, false, err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		return domain.TradePlan{}, false, err
	}
	if err := temporary.Sync(); err != nil {
		return domain.TradePlan{}, false, err
	}
	if err := temporary.Close(); err != nil {
		return domain.TradePlan{}, false, err
	}
	path := filepath.Join(directory, plan.ID+".json")
	// Publish a complete file without replacing an existing snapshot, including
	// when two Web processes save the same plan concurrently.
	if err := os.Link(temporary.Name(), path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return domain.TradePlan{}, false, err
		}
		existing, loadErr := loadTradePlan(path)
		if loadErr == nil && (existing.ID != plan.ID || existing.Symbol != plan.Symbol) {
			loadErr = fmt.Errorf("交易计划快照标识不一致")
		}
		return existing, false, loadErr
	}
	return plan, true, nil
}

func loadTradePlan(path string) (domain.TradePlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.TradePlan{}, err
	}
	var plan domain.TradePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return domain.TradePlan{}, err
	}
	return plan, validateStoredTradePlan(plan)
}

func (store *TradePlanStore) List(symbol string, limit int) ([]domain.TradePlan, error) {
	directory, err := store.directory(symbol)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.TradePlan{}, nil
	}
	if err != nil {
		return nil, err
	}
	plans := make([]domain.TradePlan, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		plan, err := loadTradePlan(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("读取交易计划 %s 失败: %w", entry.Name(), err)
		}
		if plan.Symbol != symbol || entry.Name() != plan.ID+".json" {
			return nil, fmt.Errorf("交易计划文件与快照不一致")
		}
		plans = append(plans, plan)
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].CreatedAt.Equal(plans[j].CreatedAt) {
			return plans[i].ID < plans[j].ID
		}
		return plans[i].CreatedAt.After(plans[j].CreatedAt)
	})
	if limit > 0 && len(plans) > limit {
		plans = plans[:limit]
	}
	return plans, nil
}

func (store *TradePlanStore) All(limit int) ([]domain.TradePlan, error) {
	if store == nil || strings.TrimSpace(store.root) == "" {
		return nil, fmt.Errorf("交易计划目录未初始化")
	}
	entries, err := os.ReadDir(store.root)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.TradePlan{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]domain.TradePlan, 0)
	for _, entry := range entries {
		if !entry.IsDir() || !tradePlanSymbol.MatchString(entry.Name()) {
			continue
		}
		plans, listErr := store.List(entry.Name(), 0)
		if listErr != nil {
			return nil, listErr
		}
		result = append(result, plans...)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
