package strategy

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func normalizePatternActions(input *domain.CorporateActionSnapshot, symbol, cutoff string) domain.CorporateActionSnapshot {
	result := domain.CorporateActionSnapshot{Symbol: symbol, Status: "unavailable", Scope: domain.CorporateActionScope, Events: []domain.CorporateAction{}, Error: "未取得公司行动数据"}
	if input == nil {
		return result
	}
	result = *input
	result.Events = []domain.CorporateAction{}
	first, firstErr := time.Parse(time.DateOnly, result.Start)
	last, lastErr := time.Parse(time.DateOnly, result.End)
	if result.Symbol != symbol || result.Scope != domain.CorporateActionScope || firstErr != nil || lastErr != nil || first.After(last) || result.Source == "" || result.FetchedAt.IsZero() {
		result.Symbol, result.Status, result.Error = symbol, "unavailable", "公司行动来源或覆盖区间无效"
	}
	result.End = min(result.End, cutoff)
	if result.Status != "checked" || result.Error != "" {
		result.Status = "unavailable"
	}
	seen := map[string]string{}
	for _, event := range input.Events {
		_, dateErr := time.Parse(time.DateOnly, event.ExDate)
		if dateErr == nil && event.ExDate > cutoff {
			continue
		}
		valid := dateErr == nil && event.Symbol == symbol && event.ID != "" && event.Source != "" && len(event.Kinds) > 0
		for _, kind := range event.Kinds {
			valid = valid && (kind == "cash-dividend" || kind == "bonus-share" || kind == "capitalization")
		}
		if !valid {
			result.Status, result.Error = "unavailable", "公司行动记录缺少有效日期、类型或来源"
			continue
		}
		encoded, _ := json.Marshal(event)
		if previous, exists := seen[event.ID]; exists {
			if previous != string(encoded) {
				result.Status, result.Error = "unavailable", "公司行动记录冲突"
			}
			continue
		}
		seen[event.ID] = string(encoded)
		result.Events = append(result.Events, event)
	}
	sort.Slice(result.Events, func(i, j int) bool {
		return result.Events[i].ExDate+result.Events[i].ID < result.Events[j].ExDate+result.Events[j].ID
	})
	return result
}

func checkPatternActions(snapshot domain.CorporateActionSnapshot, start, end string) *domain.PatternActionCheck {
	check := &domain.PatternActionCheck{State: "clear", Start: start, End: end, Detail: "此区间未发现已实施的现金分红、送股及转增记录；未核验配股、拆合股等其他行动"}
	for _, event := range snapshot.Events {
		if event.ExDate >= start && event.ExDate <= end {
			check.Events = append(check.Events, event)
		}
	}
	if len(check.Events) > 0 {
		check.State, check.Detail = "affected", "识别依赖历史或观察窗口跨除权除息，原始结果保留并隔离统计；供应商记录未逐条核验官方披露"
	} else if snapshot.Status != "checked" || snapshot.Start > start || snapshot.End < end {
		check.State, check.Detail = "unverified", "公司行动未核验或覆盖不足，暂不纳入常规统计"
	}
	return check
}

func applyPatternActionChecks(sample *domain.PatternValidationSample, snapshot domain.CorporateActionSnapshot, bars, benchmark []domain.DailyBar, cutoff string) {
	// ATR is recursively seeded from the first input bar. Keep that entire
	// dependency history in the audit interval without changing the detector.
	start, end := bars[0].Date, cutoff
	if sample.ConfirmedOn != "" {
		end = sample.ConfirmedOn
	} else if sample.InvalidatedOn != "" {
		end = sample.InvalidatedOn
	} else if sample.ExpiredOn != "" {
		end = sample.ExpiredOn
	}
	sample.ActionCheck = checkPatternActions(snapshot, start, end)
	for i := range sample.Outcomes {
		outcome := &sample.Outcomes[i]
		through := end
		if sample.ConfirmedOn != "" {
			through = cutoff
			index := sort.Search(len(benchmark), func(i int) bool { return benchmark[i].Date >= sample.ConfirmedOn })
			if index < len(benchmark) && benchmark[index].Date == sample.ConfirmedOn && index+outcome.Horizon < len(benchmark) {
				through = benchmark[index+outcome.Horizon].Date
			}
		}
		outcome.ActionCheck = checkPatternActions(snapshot, start, through)
	}
}

func patternActionState(sample domain.PatternValidationSample, horizon int) string {
	for _, outcome := range sample.Outcomes {
		if outcome.Horizon == horizon && outcome.ActionCheck != nil {
			return outcome.ActionCheck.State
		}
	}
	return "legacy"
}
