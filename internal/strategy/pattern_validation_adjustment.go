package strategy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func preparePatternAdjustment(input *domain.AdjustmentSnapshot, symbol, cutoff string, bars []domain.DailyBar) ([]domain.DailyBar, domain.AdjustmentSnapshot, domain.PatternAdjustment) {
	snapshot := domain.AdjustmentSnapshot{Symbol: symbol, Status: "unavailable", Factors: []domain.AdjustmentFactor{}}
	info := domain.PatternAdjustment{Symbol: symbol, Status: "unavailable", Method: domain.PatternAdjustmentMethod, End: cutoff}
	fail := func(reason string) ([]domain.DailyBar, domain.AdjustmentSnapshot, domain.PatternAdjustment) {
		snapshot.Status, snapshot.Error = "unavailable", reason
		info.Error = reason
		return nil, snapshot, info
	}
	if input == nil {
		return fail("未取得后复权因子，未使用原始价格替代")
	}
	snapshot = *input
	snapshot.Factors = []domain.AdjustmentFactor{}
	info.Source, info.SourceURL, info.FetchedAt = input.Source, input.SourceURL, input.FetchedAt
	if input.Status != "checked" || input.Error != "" {
		return fail("复权因子未核验：" + input.Error)
	}
	if len(bars) == 0 {
		return fail("没有可复权的原始日K")
	}
	info.Start, info.AnchorDate = bars[0].Date, bars[0].Date
	_, startErr := time.Parse(time.DateOnly, input.Start)
	_, endErr := time.Parse(time.DateOnly, input.End)
	if input.Symbol != symbol || input.Source == "" || input.SourceURL == "" || input.FetchedAt.IsZero() || startErr != nil || endErr != nil || input.Start > bars[0].Date || input.End < cutoff {
		return fail("复权因子证券、来源或覆盖区间无效")
	}
	factors := []domain.AdjustmentFactor{}
	for _, factor := range input.Factors {
		_, err := time.Parse(time.DateOnly, factor.Date)
		if err != nil {
			return fail("复权因子日期无效")
		}
		if factor.Date > cutoff {
			continue
		}
		if factor.Value <= 0 || math.IsNaN(factor.Value) || math.IsInf(factor.Value, 0) {
			return fail("复权因子须为有限正数")
		}
		factors = append(factors, factor)
	}
	sort.Slice(factors, func(i, j int) bool { return factors[i].Date < factors[j].Date })
	for i := 1; i < len(factors); i++ {
		if factors[i].Date == factors[i-1].Date {
			return fail("复权因子日期重复或冲突")
		}
	}
	begin := sort.Search(len(factors), func(i int) bool { return factors[i].Date > bars[0].Date }) - 1
	if begin < 0 {
		return fail("复权因子缺少起始日的有效基值")
	}
	snapshot.Factors = append(snapshot.Factors, factors[begin:]...)
	snapshot.Start, snapshot.End = bars[0].Date, cutoff
	info.AnchorFactor, info.FactorCount = snapshot.Factors[0].Value, len(snapshot.Factors)
	encoded, _ := json.Marshal(struct {
		Method  string
		Factors []domain.AdjustmentFactor
	}{info.Method, snapshot.Factors})
	hash := sha256.Sum256(encoded)
	info.FactorVersion = hex.EncodeToString(hash[:])
	adjusted := append([]domain.DailyBar{}, bars...)
	index := 0
	for i := range adjusted {
		bar := &adjusted[i]
		for index+1 < len(snapshot.Factors) && snapshot.Factors[index+1].Date <= bar.Date {
			index++
		}
		// Forward normalization preserves earlier prices when future factors arrive.
		ratio := snapshot.Factors[index].Value / info.AnchorFactor
		bar.Open *= ratio
		bar.High *= ratio
		bar.Low *= ratio
		bar.Close *= ratio
		bar.Source += " / 新浪因子后复权研究"
		if !validBar(*bar) {
			return fail(fmt.Sprintf("%s 复权价格超出有效范围", bar.Date))
		}
	}
	info.Status = "applied"
	return adjusted, snapshot, info
}

func applyAdjustedPatternActionChecks(sample *domain.PatternValidationSample, actions domain.CorporateActionSnapshot, quality []domain.AdjustmentQualityCheck) {
	checks := []*domain.PatternActionCheck{sample.ActionCheck}
	for i := range sample.Outcomes {
		checks = append(checks, sample.Outcomes[i].ActionCheck)
	}
	for _, check := range checks {
		if check == nil {
			continue
		}
		if actions.Status != "checked" || actions.Start > check.Start || actions.End < check.End {
			check.State, check.Detail = "unverified", "公司行动覆盖未核验，后复权结果保留但不纳入统计"
			continue
		}
		verified := map[string]bool{}
		failure := ""
		for _, result := range quality {
			if result.Date < check.Start || result.Date > check.End {
				continue
			}
			if result.State != "passed" {
				failure = result.Detail
				break
			}
			for _, id := range result.EventIDs {
				verified[id] = true
			}
		}
		for _, event := range check.Events {
			if !verified[event.ID] && failure == "" {
				failure = "缺少事件数值校验结果"
			}
		}
		if failure != "" {
			check.State, check.Detail = "unverified", failure+"，继续隔离统计"
		} else if len(check.Events) > 0 {
			check.State, check.Detail = "adjusted", "分配数值、因子变动及成交量口径校验通过，纳入复权统计；供应商记录未逐条核验官方披露"
		}
	}
}
