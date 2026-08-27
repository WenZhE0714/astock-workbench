package backtest

import (
	"fmt"
	"math"
	"strings"
	"time"
)

func DefaultCandidateObservationPolicy() CandidateObservationPolicy {
	return CandidateObservationPolicy{
		MinimumTradingDays:   60,
		MinimumTrades:        10,
		MinimumTotalReturn:   0,
		MinimumExcessReturn:  0,
		MaximumDrawdown:      10,
		MinimumCoverageRatio: .90,
	}
}

func NewAwaitingCandidateLifecycle(result ContinuousOptimizationResult) (CandidateLifecycle, error) {
	if result.Stage != ContinuousStageShadow || result.Selected == nil {
		return CandidateLifecycle{}, fmt.Errorf("实验 %s 尚未通过历史晋级门禁", result.ID)
	}
	return CandidateLifecycle{
		SchemaVersion:     1,
		ExperimentID:      result.ID,
		CandidateID:       result.Selected.Proposal.ID,
		CandidateSetHash:  result.Manifest.CandidateSetHash,
		ConfigurationHash: result.Manifest.ConfigurationHash,
		Status:            CandidateLifecycleAwaitingObservation,
		Policy:            DefaultCandidateObservationPolicy(),
		Assessment:        AssessCandidateObservation(DefaultCandidateObservationPolicy(), nil),
	}, nil
}

func NewCandidateLifecycle(result ContinuousOptimizationResult, now time.Time, observationStart string) (CandidateLifecycle, error) {
	lifecycle, err := NewAwaitingCandidateLifecycle(result)
	if err != nil {
		return CandidateLifecycle{}, err
	}
	if strings.TrimSpace(observationStart) == "" {
		return CandidateLifecycle{}, fmt.Errorf("前向观察开始日期不能为空")
	}
	lifecycle.Status = CandidateLifecycleObserving
	lifecycle.ObservationStarted = now
	lifecycle.ObservationStart = observationStart
	lifecycle.Events = append(lifecycle.Events, CandidateLifecycleEvent{
		At: now, Action: "start-observation", Actor: "local-user",
		Note: "候选参数已冻结；仅使用纳入观察后的新增交易日",
	})
	return lifecycle, nil
}

func CandidateLifecycleMatches(result ContinuousOptimizationResult, lifecycle CandidateLifecycle) bool {
	return result.Selected != nil &&
		lifecycle.ExperimentID == result.ID &&
		lifecycle.CandidateID == result.Selected.Proposal.ID &&
		lifecycle.CandidateSetHash == result.Manifest.CandidateSetHash &&
		lifecycle.ConfigurationHash == result.Manifest.ConfigurationHash
}

func AssessCandidateObservation(policy CandidateObservationPolicy, observation *Result) CandidateObservationAssessment {
	if policy.MinimumTradingDays <= 0 {
		policy = DefaultCandidateObservationPolicy()
	}
	tradingDays := 0
	metrics := Metrics{}
	coveragePassed := false
	coverageDetail := "尚未形成前向观察结果"
	if observation != nil {
		tradingDays = len(observation.Equity)
		metrics = observation.Metrics
		coveragePassed = len(observation.Request.Tickers) > 0
		minimumCoverage := 1.0
		for _, symbol := range observation.Request.Tickers {
			coverage, ok := observation.DataCoverage[symbol]
			if !ok || coverage.CoverageRatio < policy.MinimumCoverageRatio || strings.TrimSpace(observation.DataSources[symbol]) == "" {
				coveragePassed = false
			}
			if ok && coverage.CoverageRatio < minimumCoverage {
				minimumCoverage = coverage.CoverageRatio
			}
		}
		coverageDetail = fmt.Sprintf("最低行情覆盖 %.0f%%，要求不低于 %.0f%%", minimumCoverage*100, policy.MinimumCoverageRatio*100)
	}
	finite := observation != nil && finiteMetrics(metrics)
	checks := []CandidateObservationCheck{
		{Key: "trading-days", Name: "真实时间观察", Passed: tradingDays >= policy.MinimumTradingDays, Detail: fmt.Sprintf("已观察 %d 个交易日，要求至少 %d 日", tradingDays, policy.MinimumTradingDays)},
		{Key: "trades", Name: "成交样本", Passed: finite && metrics.Trades >= policy.MinimumTrades, Detail: fmt.Sprintf("已完成 %d 笔交易，要求至少 %d 笔", metrics.Trades, policy.MinimumTrades)},
		{Key: "return", Name: "观察收益", Passed: finite && metrics.TotalReturn > policy.MinimumTotalReturn, Detail: fmt.Sprintf("收益 %+.2f%%，要求高于 %+.2f%%", metrics.TotalReturn, policy.MinimumTotalReturn)},
		{Key: "benchmark", Name: "相对基准", Passed: finite && metrics.BenchmarkAvailable && metrics.ExcessReturn > policy.MinimumExcessReturn, Detail: fmt.Sprintf("超额 %+.2f%%，要求基准可用且高于 %+.2f%%", metrics.ExcessReturn, policy.MinimumExcessReturn)},
		{Key: "drawdown", Name: "最大回撤", Passed: finite && math.Abs(metrics.MaxDrawdown) <= policy.MaximumDrawdown, Detail: fmt.Sprintf("回撤 %+.2f%%，上限 %.2f%%", metrics.MaxDrawdown, policy.MaximumDrawdown)},
		{Key: "coverage", Name: "数据完整性", Passed: coveragePassed, Detail: coverageDetail},
	}
	passed := true
	for _, check := range checks {
		if !check.Passed {
			passed = false
			break
		}
	}
	verdict := "继续前向观察"
	if observation == nil {
		verdict = "等待新增交易日"
	} else if passed {
		verdict = "已达到人工批准门槛"
	}
	return CandidateObservationAssessment{Passed: passed, Verdict: verdict, Checks: checks}
}

func UpdateCandidateObservation(lifecycle *CandidateLifecycle, observation Result, evaluationEnd string, now time.Time) {
	if lifecycle == nil {
		return
	}
	lifecycle.Observation = &observation
	lifecycle.EvaluationEnd = evaluationEnd
	lifecycle.TradingDays = len(observation.Equity)
	if lifecycle.TradingDays > 0 {
		lifecycle.ObservedThrough = observation.Equity[lifecycle.TradingDays-1].Date
	}
	lifecycle.Assessment = AssessCandidateObservation(lifecycle.Policy, &observation)
	if lifecycle.Assessment.Passed {
		lifecycle.Status = CandidateLifecycleApprovalReady
	} else {
		lifecycle.Status = CandidateLifecycleObserving
	}
	lifecycle.Events = append(lifecycle.Events, CandidateLifecycleEvent{
		At: now, Action: "refresh-observation", Actor: "system",
		Note: fmt.Sprintf("观察更新至 %s；%d 个交易日，%d 笔完成交易", lifecycle.ObservedThrough, lifecycle.TradingDays, observation.Metrics.Trades),
	})
}

func ApproveCandidateLifecycle(lifecycle *CandidateLifecycle, now time.Time, note string) error {
	if lifecycle == nil || lifecycle.Status != CandidateLifecycleApprovalReady || !lifecycle.Assessment.Passed {
		return fmt.Errorf("候选尚未达到人工批准门槛")
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return fmt.Errorf("批准为研究基线必须填写审批依据")
	}
	lifecycle.Status = CandidateLifecycleApproved
	lifecycle.ApprovedAt = now
	lifecycle.DecisionNote = note
	lifecycle.Events = append(lifecycle.Events, CandidateLifecycleEvent{At: now, Action: "approve-baseline", Actor: "local-user", Note: lifecycle.DecisionNote})
	return nil
}

func RejectCandidateLifecycle(lifecycle *CandidateLifecycle, now time.Time, note string) error {
	note = strings.TrimSpace(note)
	if lifecycle == nil || lifecycle.Status == CandidateLifecycleApproved {
		return fmt.Errorf("当前候选状态不能拒绝")
	}
	if note == "" {
		return fmt.Errorf("拒绝候选必须填写原因")
	}
	lifecycle.Status = CandidateLifecycleRejected
	lifecycle.RejectedAt = now
	lifecycle.DecisionNote = note
	lifecycle.Events = append(lifecycle.Events, CandidateLifecycleEvent{At: now, Action: "reject-candidate", Actor: "local-user", Note: note})
	return nil
}

func RevokeCandidateLifecycle(lifecycle *CandidateLifecycle, now time.Time, note string) error {
	note = strings.TrimSpace(note)
	if lifecycle == nil || lifecycle.Status != CandidateLifecycleApproved {
		return fmt.Errorf("只有已批准候选可以撤销")
	}
	if note == "" {
		return fmt.Errorf("撤销批准必须填写原因")
	}
	lifecycle.Status = CandidateLifecycleRevoked
	lifecycle.RevokedAt = now
	lifecycle.DecisionNote = note
	lifecycle.Events = append(lifecycle.Events, CandidateLifecycleEvent{At: now, Action: "revoke-approval", Actor: "local-user", Note: note})
	return nil
}
