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

func applyAdjustedPatternActionChecks(sample *domain.PatternValidationSample, actions domain.CorporateActionSnapshot, factors []domain.AdjustmentFactor) {
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
		cashDates := map[string]bool{}
		unsupported, unmatched := false, false
		for _, event := range check.Events {
			for _, kind := range event.Kinds {
				unsupported = unsupported || kind != "cash-dividend"
			}
			cashDates[event.ExDate] = true
			index := sort.Search(len(factors), func(i int) bool { return factors[i].Date >= event.ExDate })
			if index == 0 || index == len(factors) || factors[index].Date != event.ExDate || factors[index].Value == factors[index-1].Value {
				unmatched = true
			}
		}
		for i := 1; i < len(factors); i++ {
			factor := factors[i]
			if factor.Date > check.Start && factor.Date <= check.End && factor.Value != factors[i-1].Value && !cashDates[factor.Date] {
				unmatched = true
			}
		}
		switch {
		case unsupported:
			check.State, check.Detail = "affected", "价格已后复权；送股或转增的原始成交量尚不可比，继续隔离"
		case unmatched:
			check.State, check.Detail = "unverified", "分红日期与复权因子变动未完全对齐，后复权结果不纳入统计"
		case len(check.Events) > 0:
			check.State, check.Detail = "adjusted", "现金分红日期与因子变动已对齐，纳入后复权价格统计；供应商数值未逐条核验官方披露"
		}
	}
}
