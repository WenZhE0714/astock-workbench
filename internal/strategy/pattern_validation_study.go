package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const patternStudyMethod = "fixed-time-holdout-v1"

func freezePatternStudy(request domain.PatternValidationRequest, cutoff string, now time.Time) domain.PatternValidationStudy {
	boundary, _ := time.Parse(time.DateOnly, request.ValidationStart)
	symbols := append([]string{}, request.Symbols...)
	sort.Strings(symbols)
	basis := request.PriceBasis
	if basis == "" {
		basis = domain.PatternPriceRaw
	}
	config := domain.PatternStudyConfig{
		AlgorithmVersion: PatternValidationVersion, PatternVersion: "classic-v1",
		Patterns: classicPatternIDs(),
		Symbols:  symbols, PriceBasis: basis, Start: request.Start, ResearchEnd: boundary.AddDate(0, 0, -1).Format(time.DateOnly), ValidationStart: request.ValidationStart, End: request.End, DataCutoff: cutoff,
		Horizons: []int{5, 10, 20}, WarmupDays: PatternValidationWarmupDays, LookbackBars: classicLookback, PivotRadius: classicPivotRadius, ConfirmationBars: classicMaximumAge,
		PriceMethod: "unadjusted", VolumeMethod: "raw", QualityMethod: domain.CorporateActionScope, BenchmarkMethod: patternBenchmarkMethod,
	}
	if basis == domain.PatternPriceHFQ {
		config.PriceMethod, config.VolumeMethod, config.QualityMethod = domain.PatternAdjustmentMethod, domain.PatternVolumeMethod, domain.PatternAdjustmentQualityMethod
	}
	encoded, _ := json.Marshal(struct {
		Method string
		Config domain.PatternStudyConfig
	}{patternStudyMethod, config})
	hash := sha256.Sum256(encoded)
	return domain.PatternValidationStudy{Method: patternStudyMethod, Kind: "retrospective", FrozenAt: now, Config: config, ConfigHash: hex.EncodeToString(hash[:])}
}

func buildPatternStudy(ctx context.Context, request domain.PatternValidationRequest, inputs []domain.PatternValidationInput, now time.Time, cutoff string) (domain.PatternValidationReport, error) {
	study := freezePatternStudy(request, cutoff, now)
	researchRequest := request
	researchRequest.ValidationStart = ""
	researchRequest.End = study.Config.ResearchEnd
	// Replay against the earlier cutoff: later confirmation, regime and failure
	// states must never enter the research period's evidence or filters.
	research, err := BuildPatternValidation(ctx, researchRequest, inputs, now)
	if err != nil {
		return research, fmt.Errorf("研究期重放失败: %w", err)
	}
	fullRequest := request
	fullRequest.ValidationStart = ""
	report, err := BuildPatternValidation(ctx, fullRequest, inputs, now)
	if err != nil {
		return report, err
	}
	study.Research = domain.PatternResearchEvidence{Cutoff: research.Cutoff, InputHash: research.InputHash, Coverage: research.Coverage, ActionCoverage: research.ActionCoverage, Adjustments: research.Adjustments}
	samples := make([]domain.PatternValidationSample, 0, len(report.Samples))
	for _, sample := range research.Samples {
		sample.Period = "research"
		for i := range sample.Outcomes {
			outcome := &sample.Outcomes[i]
			check := &domain.PatternPeriodCheck{State: "within", Cutoff: research.Cutoff, Detail: "按研究截止日重放"}
			if outcome.State == "pending" || (sample.ConfirmedOn == "" && sample.InvalidatedOn == "" && sample.ExpiredOn == "") {
				check.State, check.Detail = "excluded", "分界日前尚未确认或观察窗口未完成，不纳入研究期效果，也不迁入验证期"
			}
			outcome.PeriodCheck = check
		}
		samples = append(samples, sample)
	}
	for _, sample := range report.Samples {
		if sample.ObservedOn < request.ValidationStart {
			continue
		}
		sample.Period = "validation"
		for i := range sample.Outcomes {
			sample.Outcomes[i].PeriodCheck = &domain.PatternPeriodCheck{State: "within", Cutoff: cutoff, Detail: "验证期内首次识别，指标沿用此前历史预热"}
		}
		samples = append(samples, sample)
	}
	sort.Slice(samples, func(i, j int) bool {
		if samples[i].ObservedOn != samples[j].ObservedOn {
			return samples[i].ObservedOn > samples[j].ObservedOn
		}
		return samples[i].ID < samples[j].ID
	})
	report.Request, report.Study, report.Samples = request, &study, samples
	report.Warnings = append(report.Warnings,
		"本次分期为历史回溯评估：配置冻结时间为本次运行时间，不代表在历史分界日前已确定规则，也不构成独立前向验证。",
		"研究期单独截断并重放至分界日前一天，未确认或窗口未完成的样本单列排除；验证期仅统计分界日及之后首次识别的样本，允许使用此前日K预热。",
		"两期使用同一股票池、形态规则和价格口径，不自动挑选最优规则；多次更改分界或依据验证期结果选规则仍可能造成选择偏差。")
	return report, nil
}
