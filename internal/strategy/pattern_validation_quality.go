package strategy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const adjustmentPriceTolerance = 0.01

func validatePatternAdjustmentQuality(actions domain.CorporateActionSnapshot, factors []domain.AdjustmentFactor, bars, benchmark []domain.DailyBar, cutoff string) []domain.AdjustmentQualityCheck {
	checks := []domain.AdjustmentQualityCheck{}
	if len(bars) == 0 {
		return checks
	}
	events := map[string][]domain.CorporateAction{}
	for _, event := range actions.Events {
		if event.ExDate >= bars[0].Date && event.ExDate <= cutoff {
			events[event.ExDate] = append(events[event.ExDate], event)
		}
	}
	// Include unexplained factor changes even when the event feed is empty.
	for i := 1; i < len(factors); i++ {
		factor := factors[i]
		if factor.Date > bars[0].Date && factor.Date <= cutoff && factor.Value != factors[i-1].Value {
			if _, exists := events[factor.Date]; !exists {
				events[factor.Date] = nil
			}
		}
	}
	dates := make([]string, 0, len(events))
	for date := range events {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	for _, date := range dates {
		checks = append(checks, checkPatternAdjustmentDate(date, events[date], actions, factors, bars, benchmark))
	}
	return checks
}

func checkPatternAdjustmentDate(date string, events []domain.CorporateAction, actions domain.CorporateActionSnapshot, factors []domain.AdjustmentFactor, bars, benchmark []domain.DailyBar) domain.AdjustmentQualityCheck {
	check := domain.AdjustmentQualityCheck{Date: date, State: "unverified", Tolerance: adjustmentPriceTolerance}
	for _, event := range events {
		check.EventIDs = append(check.EventIDs, event.ID)
	}
	fail := func(code, detail string) domain.AdjustmentQualityCheck {
		check.Code, check.Detail = code, detail
		return check
	}
	if actions.Status != "checked" || actions.Error != "" || actions.Start > date || actions.End < date {
		return fail("event-coverage", "公司行动来源或覆盖未核验")
	}
	if len(events) == 0 {
		return fail("unmatched-factor", "因子发生变化，但未找到对应的分红送转记录")
	}
	if len(events) > 1 {
		return fail("conflicting-events", "同一除权日存在多条方案，分配基数未确认，未合并计算")
	}
	event := events[0]
	terms := event.Terms
	if terms == nil {
		return fail("missing-terms", "缺少每10股分红、送股或转增数值")
	}
	amounts := []*float64{terms.CashPer10, terms.BonusPer10, terms.CapitalizationPer10}
	kinds := []string{"cash-dividend", "bonus-share", "capitalization"}
	for i, number := range amounts {
		if number == nil || math.IsNaN(*number) || math.IsInf(*number, 0) || *number < 0 {
			return fail("invalid-terms", "分配数值缺失或无效")
		}
		present := false
		for _, kind := range event.Kinds {
			if kind == kinds[i] {
				present = true
			}
		}
		if present != (*number > 0) {
			return fail("inconsistent-terms", "分配数值与事件类型不一致")
		}
	}
	shareRatio := 1 + *terms.BonusPer10/10 + *terms.CapitalizationPer10/10
	check.ShareRatio = chartNumber(shareRatio)
	if check.ShareRatio == nil {
		return fail("invalid-terms", "送转股比例超出有效范围")
	}
	index := sort.Search(len(bars), func(i int) bool { return bars[i].Date >= date })
	if index == len(bars) || bars[index].Date != date {
		return fail("missing-event-bar", "事件日缺少股票日K，停牌或数据缺失原因待核验")
	}
	if index == 0 {
		return fail("missing-previous-bar", "缺少除权前收盘日K")
	}
	previous := bars[index-1]
	check.PreviousDate, check.PreviousClose = previous.Date, chartNumber(previous.Close)
	if bars[index].Volume <= 0 || previous.Volume <= 0 {
		return fail("unverified-trading", "除权日或前一日成交量为零，交易状态未核验")
	}
	day := sort.Search(len(benchmark), func(i int) bool { return benchmark[i].Date >= date })
	if day == len(benchmark) || benchmark[day].Date != date || day == 0 {
		return fail("missing-calendar", "基准日K未覆盖事件及前一交易日")
	}
	if previous.Date != benchmark[day-1].Date {
		return fail("previous-bar-gap", "除权前交易日日K有缺口，停牌或数据缺失原因待核验")
	}
	if event.RecordDate != "" && event.RecordDate != previous.Date {
		return fail("record-date-mismatch", "股权登记日与除权前交易日不一致")
	}
	expected := (previous.Close - *terms.CashPer10/10) / shareRatio
	check.ExpectedExPrice = chartNumber(expected)
	if check.ExpectedExPrice == nil || expected <= 0 {
		return fail("invalid-ex-price", "理论除权价非正或超出有效范围")
	}
	check.ExpectedRatio = chartNumber(previous.Close / expected)
	step := sort.Search(len(factors), func(i int) bool { return factors[i].Date >= date })
	if step == 0 || step == len(factors) || factors[step].Date != date || factors[step].Value == factors[step-1].Value {
		return fail("missing-factor-step", "事件日缺少对应因子变动或前值")
	}
	ratio := factors[step].Value / factors[step-1].Value
	check.ObservedRatio = chartNumber(ratio)
	if check.ObservedRatio == nil || ratio <= 0 {
		return fail("invalid-factor-ratio", "因子变动比值无效")
	}
	implied := previous.Close / ratio
	check.ImpliedExPrice, check.PriceError = chartNumber(implied), chartNumber(math.Abs(implied-expected))
	if check.ImpliedExPrice == nil || check.PriceError == nil {
		return fail("invalid-ex-price", "因子隐含除权价超出有效范围")
	}
	if *check.PriceError > adjustmentPriceTolerance+1e-9 {
		check.State = "mismatch"
		return fail("factor-price-mismatch", "理论除权价与因子隐含除权价偏差超过0.01元")
	}
	check.State, check.Code, check.Detail = "passed", "price-volume-verified", "分配数值与因子隐含除权价一致；成交量按送转股比例归一，现金分红不改变成交量"
	return check
}

func applyPatternShareVolumes(bars []domain.DailyBar, info *domain.PatternAdjustment, actions domain.CorporateActionSnapshot) {
	info.VolumeMethod, info.QualityMethod = domain.PatternVolumeMethod, domain.PatternAdjustmentQualityMethod
	info.QualityStatus = "passed"
	if actions.Status != "checked" || actions.Error != "" || actions.Start > info.Start || actions.End < info.End {
		info.QualityStatus = "unverified"
	}
	index, shares := 0, 1.0
	for i := range bars {
		for index < len(info.Quality) && info.Quality[index].Date <= bars[i].Date {
			check := &info.Quality[index]
			if check.State == "passed" && check.Date > info.AnchorDate {
				if check.ShareRatio == nil || math.IsInf(shares**check.ShareRatio, 0) {
					check.State, check.Code, check.Detail = "unverified", "invalid-share-scale", "累计股数比例超出有效范围"
				} else {
					shares *= *check.ShareRatio
				}
			}
			index++
		}
		// Amount and turnover retain their original units. Cash factors never
		// enter the cumulative share count used to normalize volume.
		bars[i].Volume /= shares
	}
	for _, check := range info.Quality {
		if check.State != "passed" {
			info.QualityStatus = "unverified"
		}
	}
	encoded, _ := json.Marshal(struct {
		Method, QualityMethod string
		Checks                []domain.AdjustmentQualityCheck
	}{info.VolumeMethod, info.QualityMethod, info.Quality})
	hash := sha256.Sum256(encoded)
	info.VolumeVersion = hex.EncodeToString(hash[:])
}
