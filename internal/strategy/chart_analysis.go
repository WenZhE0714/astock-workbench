package strategy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

var chartLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

// AnalyzeChart uses only bars on or before through. AsOf determines whether
// the rightmost daily candle can confirm a structure; it is not a quote date.
func AnalyzeChart(symbol string, input []domain.DailyBar, asOf time.Time, through string) (domain.ChartAnalysis, error) {
	if asOf.IsZero() {
		return domain.ChartAnalysis{}, fmt.Errorf("缺少图表分析时间")
	}
	now := asOf.In(chartLocation)
	today := now.Format(time.DateOnly)
	if through == "" {
		through = today
	}
	if _, err := time.Parse(time.DateOnly, through); err != nil || through > today {
		return domain.ChartAnalysis{}, fmt.Errorf("图表日期无效或晚于当前日期")
	}
	bars, discarded := chartBarsThrough(symbol, input, through)
	if len(bars) < 21 {
		return domain.ChartAnalysis{}, fmt.Errorf("结构分析需要至少21根有效日K，当前%d根", len(bars))
	}
	latest := bars[len(bars)-1]
	previous := bars[len(bars)-2]
	complete := latest.Date < today || now.Hour() >= 15
	values := closes(bars)
	ma20 := average(values[len(values)-20:])
	ma60 := math.NaN()
	if len(values) >= 60 {
		ma60 = average(values[len(values)-60:])
	}
	highs, lows := make([]float64, len(bars)), make([]float64, len(bars))
	for index, bar := range bars {
		highs[index], lows[index] = bar.High, bar.Low
	}
	atr14 := atr(values, highs, lows, 14)
	ema20 := ema(values, 20)[len(values)-1]
	high, low := chartRangeAnchors(bars[len(bars)-21 : len(bars)-1])
	volume := 0.0
	for _, bar := range bars[len(bars)-21 : len(bars)-1] {
		volume += bar.Volume
	}
	var ratio *float64
	if volume > 0 {
		ratio = chartNumber(latest.Volume / (volume / 20))
	}
	analysis := domain.ChartAnalysis{
		Version: "chart-v1", Symbol: symbol, Source: latest.Source, Timeframe: "1d", PriceBasis: "unadjusted",
		DataDate: latest.Date, Complete: complete, BarsUsed: len(bars), Price: latest.Close,
		RangeHigh: high.Price, RangeLow: low.Price, VolumeRatio: ratio,
		Weekly: chartWeeklyTrend(bars, complete),
		Levels: []domain.ChartLevel{
			{Key: "range20", Label: "前20日低 / 高", Value: chartNumber(low.Price), Upper: chartNumber(high.Price), Basis: "不含观察日的前20根日K极值"},
			{Key: "ma20", Label: "MA20", Value: chartNumber(ma20), Basis: "20根收盘价算术平均，不代表成交密集区"},
			{Key: "ma60", Label: "MA60", Value: chartNumber(ma60), Basis: "60根收盘价算术平均"},
			{Key: "pivot", Label: "前日枢轴", Value: chartNumber((previous.High + previous.Low + previous.Close) / 3), Date: previous.Date, Basis: "前一交易日(最高+最低+收盘)/3"},
			{Key: "keltner", Label: "Keltner 下 / 上", Value: chartNumber(ema20 - 2*atr14), Upper: chartNumber(ema20 + 2*atr14), Basis: "EMA20 +/- 2倍Wilder ATR14"},
			{Key: "atr14", Label: "ATR14", Value: chartNumber(atr14), Basis: "14周期Wilder真实波幅"},
		},
	}
	gap := chartNearestOpenGap(bars)
	analysis.Levels = append(analysis.Levels, gap)
	if !complete {
		analysis.Warnings = append(analysis.Warnings, "观察日日K未收盘，形态和成交量尚未确认")
	}
	if discarded > 0 {
		analysis.Warnings = append(analysis.Warnings, fmt.Sprintf("已排除%d根日期、证券或OHLC无效的数据", discarded))
	}
	analysis.Warnings = append(analysis.Warnings, "未复权价格；除权除息可能形成非交易性缺口，需核验公司行动")
	analysis.Structures = chartStructures(bars, complete, high, low, ma20, ma60, atr14, ratio)
	analysis.Structures = append(analysis.Structures, classicChartStructures(bars, complete)...)
	encoded, err := json.Marshal(analysis)
	if err != nil {
		return domain.ChartAnalysis{}, err
	}
	digest := sha256.Sum256(encoded)
	analysis.Fingerprint = hex.EncodeToString(digest[:])
	return analysis, nil
}

func chartNumber(value float64) *float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return &value
}

func chartBarsThrough(symbol string, input []domain.DailyBar, through string) ([]domain.DailyBar, int) {
	valid := make([]domain.DailyBar, 0, len(input))
	discarded := 0
	for _, bar := range input {
		_, dateErr := time.Parse(time.DateOnly, bar.Date)
		if dateErr == nil && bar.Date > through {
			continue
		}
		if dateErr != nil || !validBar(bar) || bar.High < math.Max(bar.Open, bar.Close) || bar.Low > math.Min(bar.Open, bar.Close) || (bar.Symbol != "" && bar.Symbol != symbol) {
			discarded++
			continue
		}
		valid = append(valid, bar)
	}
	return normalizedBars(valid), discarded
}

func chartRangeAnchors(bars []domain.DailyBar) (high, low domain.ChartAnchor) {
	high = domain.ChartAnchor{Date: bars[0].Date, Price: bars[0].High, Label: "区间高点"}
	low = domain.ChartAnchor{Date: bars[0].Date, Price: bars[0].Low, Label: "区间低点"}
	for _, bar := range bars[1:] {
		if bar.High >= high.Price {
			high.Date, high.Price = bar.Date, bar.High
		}
		if bar.Low <= low.Price {
			low.Date, low.Price = bar.Date, bar.Low
		}
	}
	return high, low
}

func chartNearestOpenGap(bars []domain.DailyBar) domain.ChartLevel {
	type gap struct {
		low, high float64
		up        bool
		date      string
	}
	gaps := make([]gap, 0)
	for index, bar := range bars {
		remaining := gaps[:0]
		for _, item := range gaps {
			if item.up {
				item.high = math.Min(item.high, bar.Low)
			} else {
				item.low = math.Max(item.low, bar.High)
			}
			if item.high > item.low {
				remaining = append(remaining, item)
			}
		}
		gaps = remaining
		if index == 0 {
			continue
		}
		previous := bars[index-1]
		if bar.Low > previous.High {
			gaps = append(gaps, gap{low: previous.High, high: bar.Low, up: true, date: bar.Date})
		} else if bar.High < previous.Low {
			gaps = append(gaps, gap{low: bar.High, high: previous.Low, date: bar.Date})
		}
	}
	result := domain.ChartLevel{Key: "gap", Label: "未回补缺口", Basis: "样本内未发现未回补缺口"}
	price, distance := bars[len(bars)-1].Close, math.Inf(1)
	for _, item := range gaps {
		current := math.Max(item.low-price, math.Max(price-item.high, 0))
		if current <= distance {
			distance = current
			result.Value, result.Upper, result.Date = chartNumber(item.low), chartNumber(item.high), item.date
			result.Basis = "距观察价最近的剩余缺口区间；已扣除后续回补部分"
		}
	}
	return result
}

func chartStructures(bars []domain.DailyBar, complete bool, high, low domain.ChartAnchor, ma20, ma60, atr14 float64, ratio *float64) []domain.ChartStructure {
	latest := bars[len(bars)-1]
	breakout := func(id, name string, upper, lower domain.ChartAnchor) domain.ChartStructure {
		state := "watching"
		if latest.Close < lower.Price {
			state = "invalidated"
		} else if latest.Close > upper.Price {
			state = "forming"
			if complete && ratio != nil && *ratio >= 1.2 {
				state = "confirmed"
			}
		}
		stop := math.Max(lower.Price, upper.Price-atr14)
		item := domain.ChartStructure{
			ID: id, Name: name, State: state, Anchors: []domain.ChartAnchor{upper, lower},
			Evidence: []string{fmt.Sprintf("观察价 %.2f；区间 %.2f - %.2f", latest.Close, lower.Price, upper.Price)},
			Plan:     chartPlanLevels(upper.Price, upper.Price+atr14*.25, stop, "完整日K收盘突破区间高点，日成交量达到此前20日均量的1.20倍；后续交易日仅在入场区间内观察"),
		}
		if ratio == nil {
			item.Evidence = append(item.Evidence, "前20日成交量不足，无法确认放量")
		} else if complete {
			item.Evidence = append(item.Evidence, fmt.Sprintf("完整日成交量 / 前20日均量 %.2f倍", *ratio))
		} else {
			item.Evidence = append(item.Evidence, "盘中累计成交量不能作为完整日放量确认")
		}
		return item
	}
	result := []domain.ChartStructure{breakout("range-breakout", "区间突破", high, low)}
	if !math.IsNaN(ma60) && atr14 > 0 {
		stop := math.Min(ma20-atr14, low.Price)
		state := "watching"
		if latest.Close < stop || ma20 <= ma60 {
			state = "invalidated"
		} else if latest.Low <= ma20+atr14*.25 && latest.Close >= ma20 {
			state = "forming"
			if complete {
				state = "confirmed"
			}
		}
		result = append(result, domain.ChartStructure{
			ID: "ma-pullback", Name: "趋势回踩", State: state,
			Anchors:  []domain.ChartAnchor{{Date: latest.Date, Price: ma20, Label: "MA20"}, low},
			Evidence: []string{fmt.Sprintf("MA20 %.2f / MA60 %.2f；观察日最低 %.2f", ma20, ma60, latest.Low)},
			Plan:     chartPlanLevels(ma20, ma20+atr14*.25, stop, "MA20高于MA60；完整日K回踩MA20附近后收于MA20上方，后续交易日仅在入场区间内观察"),
		})
	}
	shortHigh, shortLow := chartRangeAnchors(bars[len(bars)-11 : len(bars)-1])
	if high.Price > low.Price && (shortHigh.Price-shortLow.Price)/(high.Price-low.Price) <= .6 {
		item := breakout("compression-breakout", "收缩突破", shortHigh, shortLow)
		item.Evidence = append(item.Evidence, fmt.Sprintf("前10日/前20日区间宽度 %.0f%%", (shortHigh.Price-shortLow.Price)/(high.Price-low.Price)*100))
		result = append(result, item)
	}
	return result
}

func chartPlanLevels(entryLow, entryHigh, stop float64, confirmation string) *domain.ChartPlanLevels {
	entryLow = math.Ceil(entryLow*100-1e-8) / 100
	entryHigh = math.Ceil(entryHigh*100-1e-8) / 100
	stop = math.Floor(stop*100+1e-8) / 100
	risk := math.Round((entryHigh-stop)*100) / 100
	if stop <= 0 || stop >= entryLow || entryLow > entryHigh || risk <= 0 || math.IsNaN(risk) || math.IsInf(risk, 0) {
		return nil
	}
	return &domain.ChartPlanLevels{
		EntryLow: entryLow, EntryHigh: entryHigh, Invalidation: stop,
		Target1: math.Round((entryHigh+risk)*100) / 100, Target2: math.Round((entryHigh+2*risk)*100) / 100,
		RiskPerShare: risk, RewardRisk: 2, Confirmation: confirmation,
		RiskBasis: "按入场区间上沿计算1R；目标为1R/2R情景测算，未扣费用，不是预测；T+1、跳空和跌停可能扩大损失",
	}
}

func chartWeeklyTrend(bars []domain.DailyBar, complete bool) domain.ChartWeeklyTrend {
	latest := bars[len(bars)-1]
	cutoff, _ := time.Parse(time.DateOnly, latest.Date)
	if !complete {
		cutoff = cutoff.AddDate(0, 0, -1)
	}
	type week struct {
		friday time.Time
		last   domain.DailyBar
	}
	weeks := make([]week, 0)
	for _, bar := range bars {
		date, _ := time.Parse(time.DateOnly, bar.Date)
		weekday := int(date.Weekday())
		if weekday == 0 || weekday == 6 {
			continue
		}
		friday := date.AddDate(0, 0, 5-weekday)
		if friday.After(cutoff) {
			continue
		}
		if len(weeks) > 0 && weeks[len(weeks)-1].friday.Equal(friday) {
			weeks[len(weeks)-1].last = bar
		} else {
			weeks = append(weeks, week{friday: friday, last: bar})
		}
	}
	// The first fetched week may begin midweek, so it is not used for MA warmup.
	if len(weeks) > 0 {
		weeks = weeks[1:]
	}
	result := domain.ChartWeeklyTrend{State: "insufficient", Weeks: len(weeks)}
	if len(weeks) == 0 {
		return result
	}
	result.DataDate = weeks[len(weeks)-1].last.Date
	if len(weeks) < 10 {
		return result
	}
	closes := make([]float64, len(weeks))
	for index, item := range weeks {
		closes[index] = item.last.Close
	}
	ma5, ma10 := average(closes[len(closes)-5:]), average(closes[len(closes)-10:])
	result.MA5, result.MA10, result.State = chartNumber(ma5), chartNumber(ma10), "sideways"
	if ma5 > ma10 && closes[len(closes)-1] > ma5 {
		result.State = "bullish"
	} else if ma5 < ma10 && closes[len(closes)-1] < ma5 {
		result.State = "bearish"
	}
	return result
}

func BuildTradePlan(analysis domain.ChartAnalysis, structureID, expiresOn string, createdAt time.Time) (domain.TradePlan, error) {
	date, err := time.Parse(time.DateOnly, expiresOn)
	now := createdAt.In(chartLocation)
	if err != nil || createdAt.IsZero() || expiresOn < now.Format(time.DateOnly) || date.Format(time.DateOnly) > now.AddDate(0, 0, 90).Format(time.DateOnly) {
		return domain.TradePlan{}, fmt.Errorf("有效期须在今天至未来90个自然日内")
	}
	if analysis.Fingerprint == "" || analysis.Version != "chart-v1" {
		return domain.TradePlan{}, fmt.Errorf("缺少有效分析快照")
	}
	for _, structure := range analysis.Structures {
		if structure.ID != structureID {
			continue
		}
		if structure.Plan == nil || structure.State == "invalidated" {
			return domain.TradePlan{}, fmt.Errorf("当前结构已失效或没有有效风险区间")
		}
		digest := sha256.Sum256([]byte(analysis.Fingerprint + ":" + structure.ID + ":" + expiresOn))
		plan := domain.TradePlan{
			ID: hex.EncodeToString(digest[:]), Version: 1, Symbol: analysis.Symbol,
			CreatedAt: createdAt, ExpiresOn: expiresOn, Analysis: analysis, Structure: structure,
		}
		if structure.Pattern != nil {
			if structure.Pattern.Version != "classic-v1" || structure.Pattern.Bias != "bullish" {
				return domain.TradePlan{}, fmt.Errorf("该形态仅用于风险观察，不能生成做多计划")
			}
			plan.MonitorRule = &domain.PlanMonitorRule{
				Version: "plan-monitor-v1", StructureID: structure.ID, Kind: "breakout",
				Description: structure.Plan.Confirmation, Levels: *structure.Plan,
				BreakoutPrice: structure.Pattern.TriggerPrice, VolumeDays: 20, MinimumVolume: 1.2,
				CooldownSecs: 300, PatternReadyOn: structure.Pattern.ReadyOn,
			}
		}
		return plan, nil
	}
	return domain.TradePlan{}, fmt.Errorf("结构不存在，请刷新分析")
}

func BuildAssistantRuleDraft(analysis domain.ChartAnalysis, structure domain.ChartStructure, question, expiresOn string, proposal domain.AssistantRuleProposal, createdAt time.Time) (domain.AssistantRuleDraft, error) {
	if createdAt.IsZero() || analysis.Version != "chart-v1" || analysis.Fingerprint == "" || analysis.Symbol == "" || structure.ID == "" {
		return domain.AssistantRuleDraft{}, fmt.Errorf("缺少可验证的图表上下文")
	}
	question = strings.TrimSpace(question)
	if question == "" || len([]rune(question)) > 500 {
		return domain.AssistantRuleDraft{}, fmt.Errorf("规则描述须为1至500个字")
	}
	if err := validatePlanExpiry(expiresOn, createdAt); err != nil {
		return domain.AssistantRuleDraft{}, err
	}
	normalized, warnings, err := normalizeAssistantRuleProposal(analysis, structure, proposal)
	if err != nil {
		return domain.AssistantRuleDraft{}, err
	}
	draft := domain.AssistantRuleDraft{
		Version: "assistant-rule-v1", Symbol: analysis.Symbol, CreatedAt: createdAt, ExpiresOn: expiresOn,
		SourceQuestion: question, AnalysisDate: analysis.DataDate, AnalysisFingerprint: analysis.Fingerprint,
		StructureID: structure.ID, Proposal: normalized, Warnings: warnings,
	}
	draft.ID = assistantRuleDraftID(draft)
	return draft, nil
}

func BuildAssistantTradePlan(analysis domain.ChartAnalysis, draft domain.AssistantRuleDraft, createdAt time.Time) (domain.TradePlan, error) {
	if createdAt.IsZero() || draft.Version != "assistant-rule-v1" || draft.Symbol != analysis.Symbol || draft.AnalysisDate != analysis.DataDate || draft.AnalysisFingerprint != analysis.Fingerprint || draft.ID == "" || draft.ID != assistantRuleDraftID(draft) {
		return domain.TradePlan{}, fmt.Errorf("规则草案与当前图表快照不一致")
	}
	if err := validatePlanExpiry(draft.ExpiresOn, createdAt); err != nil {
		return domain.TradePlan{}, err
	}
	var selected domain.ChartStructure
	for _, item := range analysis.Structures {
		if item.ID == draft.StructureID {
			selected = item
			break
		}
	}
	if selected.ID == "" {
		return domain.TradePlan{}, fmt.Errorf("规则草案引用的结构已不存在")
	}
	proposal, _, err := normalizeAssistantRuleProposal(analysis, selected, draft.Proposal)
	if err != nil {
		return domain.TradePlan{}, err
	}
	if encoded, _ := json.Marshal(proposal); string(encoded) != mustJSON(draft.Proposal) {
		return domain.TradePlan{}, fmt.Errorf("规则草案未经过规范化校验")
	}
	confirmation := fmt.Sprintf("完整日K收盘高于 %.2f，成交量达到此前%d日均量的%.2f倍；后续交易日仅在入场区间内观察", proposal.ConfirmationPrice, proposal.VolumeDays, proposal.MinimumVolumeRatio)
	if proposal.Kind == "pullback" {
		trend := ""
		if proposal.RequireTrend {
			trend = "MA20高于MA60；"
		}
		confirmation = fmt.Sprintf("%s完整日K最低进入 %.2f - %.2f 且收盘不低于 %.2f；后续交易日才观察入场", trend, proposal.EntryLow, proposal.EntryHigh, proposal.EntryLow)
	}
	levels := chartPlanLevels(proposal.EntryLow, proposal.EntryHigh, proposal.Invalidation, confirmation)
	if levels == nil {
		return domain.TradePlan{}, fmt.Errorf("规则草案无法形成有效风险区间")
	}
	structureID := "assistant-" + proposal.Kind
	structure := domain.ChartStructure{
		ID: structureID, Name: proposal.Name, State: "watching",
		Anchors:  append([]domain.ChartAnchor(nil), selected.Anchors...),
		Evidence: []string{proposal.Description, "由研究助手提出，经程序校验并由用户确认；原始意图：" + draft.SourceQuestion},
		Plan:     levels,
	}
	rule := &domain.PlanMonitorRule{
		Version: "plan-monitor-v1", StructureID: structureID, Kind: proposal.Kind,
		Description: proposal.Description, Levels: *levels, BreakoutPrice: proposal.ConfirmationPrice,
		VolumeDays: proposal.VolumeDays, MinimumVolume: proposal.MinimumVolumeRatio,
		RequireTrend: proposal.RequireTrend, CooldownSecs: 300,
	}
	digest := sha256.Sum256([]byte(analysis.Fingerprint + ":assistant:" + draft.ID + ":" + draft.ExpiresOn))
	return domain.TradePlan{
		ID: hex.EncodeToString(digest[:]), Version: 1, Symbol: analysis.Symbol, CreatedAt: createdAt,
		ExpiresOn: draft.ExpiresOn, Analysis: analysis, Structure: structure, MonitorRule: rule,
	}, nil
}

func validatePlanExpiry(expiresOn string, at time.Time) error {
	date, err := time.Parse(time.DateOnly, expiresOn)
	now := at.In(chartLocation)
	if err != nil || expiresOn < now.Format(time.DateOnly) || date.Format(time.DateOnly) > now.AddDate(0, 0, 90).Format(time.DateOnly) {
		return fmt.Errorf("有效期须在今天至未来90个自然日内")
	}
	return nil
}

func normalizeAssistantRuleProposal(analysis domain.ChartAnalysis, structure domain.ChartStructure, proposal domain.AssistantRuleProposal) (domain.AssistantRuleProposal, []string, error) {
	proposal.Kind = strings.ToLower(strings.TrimSpace(proposal.Kind))
	proposal.Name = strings.TrimSpace(proposal.Name)
	proposal.Description = strings.TrimSpace(proposal.Description)
	if proposal.Kind != "breakout" && proposal.Kind != "pullback" {
		return proposal, nil, fmt.Errorf("仅支持突破或回踩两类可审计规则")
	}
	if proposal.Name == "" || len([]rune(proposal.Name)) > 24 || proposal.Description == "" || len([]rune(proposal.Description)) > 240 {
		return proposal, nil, fmt.Errorf("规则名称或说明长度无效")
	}
	round := func(value float64) float64 { return math.Round(value*100) / 100 }
	proposal.EntryLow, proposal.EntryHigh = round(proposal.EntryLow), round(proposal.EntryHigh)
	proposal.Invalidation, proposal.ConfirmationPrice = round(proposal.Invalidation), round(proposal.ConfirmationPrice)
	proposal.MinimumVolumeRatio = math.Round(proposal.MinimumVolumeRatio*100) / 100
	if proposal.VolumeDays == 0 {
		proposal.VolumeDays = 20
	}
	if proposal.MinimumVolumeRatio == 0 {
		proposal.MinimumVolumeRatio = 1.2
	}
	prices := []float64{proposal.EntryLow, proposal.EntryHigh, proposal.Invalidation, proposal.ConfirmationPrice}
	for _, value := range prices {
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) || value < analysis.Price*.5 || value > analysis.Price*1.5 {
			return proposal, nil, fmt.Errorf("规则价位超出当前价格的可校验范围")
		}
	}
	if proposal.EntryLow > proposal.EntryHigh || proposal.Invalidation >= proposal.EntryLow || proposal.EntryHigh-proposal.EntryLow > analysis.Price*.15 {
		return proposal, nil, fmt.Errorf("入场区间、失效位或区间宽度无效")
	}
	if proposal.Kind == "breakout" && proposal.ConfirmationPrice < proposal.EntryLow {
		return proposal, nil, fmt.Errorf("突破确认价不能低于入场区间下沿")
	}
	if proposal.VolumeDays < 5 || proposal.VolumeDays > 60 || proposal.MinimumVolumeRatio < .5 || proposal.MinimumVolumeRatio > 5 {
		return proposal, nil, fmt.Errorf("量能窗口或最低量比超出允许范围")
	}
	warnings := []string{"AI只提出受限规则；价格、有效期和风险顺序已由程序校验，仍不代表收益预测"}
	if structure.State == "invalidated" {
		return proposal, nil, fmt.Errorf("所选图表结构已经失效")
	}
	if !analysis.Complete {
		warnings = append(warnings, "观察日日K尚未收盘，草案只能等待后续完整日K确认")
	}
	return proposal, warnings, nil
}

func assistantRuleDraftID(draft domain.AssistantRuleDraft) string {
	payload := struct {
		Version, Symbol, ExpiresOn, SourceQuestion, AnalysisDate, AnalysisFingerprint, StructureID string
		Proposal                                                                                   domain.AssistantRuleProposal
	}{
		Version: draft.Version, Symbol: draft.Symbol, ExpiresOn: draft.ExpiresOn,
		SourceQuestion: draft.SourceQuestion, AnalysisDate: draft.AnalysisDate,
		AnalysisFingerprint: draft.AnalysisFingerprint, StructureID: draft.StructureID, Proposal: draft.Proposal,
	}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func mustJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
