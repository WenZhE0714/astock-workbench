package backtest

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

type continuousEngineMock struct {
	calls []Request
}

type candidateFailureEngine struct{}

func (candidateFailureEngine) Run(_ context.Context, request Request) (Result, error) {
	if request.Technical.EffectiveEntryMode() == EntryModeBreakout {
		return Result{}, errors.New("candidate data unavailable")
	}
	return Result{Request: request, Metrics: Metrics{TotalReturn: 2, AnnualizedReturn: 2, MaxDrawdown: -2, Sharpe: 1, Trades: 10, FinalEquity: request.InitialCash}}, nil
}

func TestContinuousOptimizerIsolatesCandidateEvaluationFailure(t *testing.T) {
	request := continuousTestRequest()
	neighbor := request.Proposals[1]
	neighbor.ID = "reclaim-neighbor"
	neighbor.Parameters.FastMA = 21
	request.Proposals = append(request.Proposals, neighbor)
	result, err := NewContinuousOptimizer(candidateFailureEngine{}).Optimize(context.Background(), request, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 3 || result.Selected == nil || result.Selected.Proposal.ID != "reclaim" {
		t.Fatalf("one failed candidate aborted or corrupted experiment: %#v", result)
	}
	foundFailure := false
	for _, candidate := range result.Candidates {
		if candidate.Proposal.ID == "breakout" && candidate.Rejected && strings.Contains(strings.Join(candidate.Reasons, ""), "训练失败") {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Fatalf("candidate failure evidence missing: %#v", result.Candidates)
	}
}

func (mock *continuousEngineMock) Run(_ context.Context, request Request) (Result, error) {
	mock.calls = append(mock.calls, request)
	modeBonus := map[string]float64{EntryModeBreakout: 1, EntryModeReclaim: 3, EntryModePullback: -1}[request.Technical.EffectiveEntryMode()]
	metrics := Metrics{
		TotalReturn: modeBonus + 2, AnnualizedReturn: modeBonus + 2, MaxDrawdown: -4,
		Sharpe: .8, Trades: 8, Turnover: 30, FinalEquity: request.InitialCash * (1 + (modeBonus+2)/100),
	}
	trades := []Trade{{ID: "T1", NetProfit: 300}, {ID: "T2", NetProfit: 250}, {ID: "T3", NetProfit: -100}}
	return Result{Request: request, Metrics: metrics, Trades: trades}, nil
}

func continuousTestRequest() ContinuousOptimizationRequest {
	base := testRequest()
	base.Technical = DefaultTechnicalParameters()
	base.InitialCash = 100000
	folds := []WalkForwardFold{
		{ID: "F01", Train: Period{Start: time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2018, 12, 31, 0, 0, 0, 0, time.UTC)}, Validate: Period{Start: time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2019, 12, 31, 0, 0, 0, 0, time.UTC)}},
		{ID: "F02", Train: Period{Start: time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2019, 12, 31, 0, 0, 0, 0, time.UTC)}, Validate: Period{Start: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2020, 12, 31, 0, 0, 0, 0, time.UTC)}},
		{ID: "F03", Train: Period{Start: time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2020, 12, 31, 0, 0, 0, 0, time.UTC)}, Validate: Period{Start: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2021, 12, 31, 0, 0, 0, 0, time.UTC)}},
	}
	breakout := DefaultTechnicalParameters()
	reclaim := breakout
	reclaim.EntryMode = EntryModeReclaim
	return ContinuousOptimizationRequest{
		BaseRequest: base, Folds: folds,
		Holdout: Period{Start: time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2022, 12, 31, 0, 0, 0, 0, time.UTC)},
		Proposals: []StrategyProposal{
			{ID: "breakout", Agent: "baseline", Parameters: breakout},
			{ID: "reclaim", Agent: "trend", Parameters: reclaim},
		},
		MinimumValidationTrades: 20, MinimumPositiveFoldRatio: .67, MaximumValidationDrawdown: 15,
	}
}

func TestContinuousOptimizerLocksBeforeHoldoutAndRunsStressOnce(t *testing.T) {
	engine := &continuousEngineMock{}
	optimizer := NewContinuousOptimizer(engine)
	optimizer.now = func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }
	request := continuousTestRequest()
	neighbor := request.Proposals[1]
	neighbor.ID = "reclaim-neighbor"
	neighbor.Parameters.FastMA = 21
	request.Proposals = append(request.Proposals, neighbor)
	result, err := optimizer.Optimize(context.Background(), request, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected == nil || result.Selected.Proposal.ID != "reclaim" || result.Holdout == nil || result.Stress.DoubleCost == nil {
		t.Fatalf("continuous selection or stress result missing: %#v", result)
	}
	wantCalls := len(request.Proposals)*len(request.Folds)*2 + 2
	if len(engine.calls) != wantCalls {
		t.Fatalf("unexpected engine calls %d, want %d", len(engine.calls), wantCalls)
	}
	for index, call := range engine.calls[:len(engine.calls)-2] {
		if !call.End.Before(request.Holdout.Start) {
			t.Fatalf("call %d read final holdout before selection: %#v", index, call)
		}
	}
	if engine.calls[len(engine.calls)-2].Technical.EffectiveEntryMode() != EntryModeReclaim || engine.calls[len(engine.calls)-1].SlippageBPS != request.BaseRequest.SlippageBPS*2 {
		t.Fatalf("holdout/stress did not use locked parameters and doubled costs: %#v", engine.calls[len(engine.calls)-2:])
	}
	if result.Stress.BestTradeProfitShare <= 0 || result.Stress.BestTradeProfitShare > 1 {
		t.Fatalf("invalid auditable concentration metric: %#v", result.Stress)
	}
}

func TestExperimentManifestHashesAreDeterministicAndOrderIndependent(t *testing.T) {
	request := continuousTestRequest()
	request.BaseRequest.Tickers = []string{"sz000001", "sh600519"}
	request.BaseRequest.Names = map[string]string{"sh600519": "贵州茅台", "sz000001": "平安银行"}
	first := BuildExperimentManifest(request, "2022-12-31", "AUTO-old", "2021-12-31")

	reordered := request
	reordered.BaseRequest.Tickers = []string{"sh600519", "sz000001"}
	reordered.Proposals = []StrategyProposal{request.Proposals[1], request.Proposals[0]}
	second := BuildExperimentManifest(reordered, "2022-12-31", "AUTO-old", "2021-12-31")
	if first.CandidateSetHash == "" || first.ConfigurationHash == "" ||
		first.CandidateSetHash != second.CandidateSetHash || first.ConfigurationHash != second.ConfigurationHash {
		t.Fatalf("manifest hashes depend on caller ordering: %#v %#v", first, second)
	}
	if strings.Join(first.Tickers, ",") != "sh600519,sz000001" {
		t.Fatalf("manifest tickers were not canonicalized: %#v", first.Tickers)
	}
}

func TestExperimentManifestConfigurationChangesAlterHash(t *testing.T) {
	request := continuousTestRequest()
	first := BuildExperimentManifest(request, "2022-12-31", "", "")
	request.BaseRequest.SlippageBPS++
	second := BuildExperimentManifest(request, "2022-12-31", "", "")
	if first.ConfigurationHash == second.ConfigurationHash {
		t.Fatalf("execution configuration change did not alter manifest hash: %s", first.ConfigurationHash)
	}
}

func TestExperimentManifestNormalizesEquivalentEntryMode(t *testing.T) {
	request := continuousTestRequest()
	request.Proposals[0].Parameters.EntryMode = ""
	first := BuildExperimentManifest(request, "2022-12-31", "", "")
	request.Proposals[0].Parameters.EntryMode = EntryModeBreakout
	second := BuildExperimentManifest(request, "2022-12-31", "", "")
	if first.CandidateSetHash != second.CandidateSetHash {
		t.Fatalf("equivalent breakout modes changed manifest hash: %#v %#v", first, second)
	}
}

func TestValidateContinuousRequestRejectsDuplicateOrOverlappingFolds(t *testing.T) {
	request := continuousTestRequest()
	request.Folds[1].ID = request.Folds[0].ID
	if err := validateContinuousRequest(request); err == nil {
		t.Fatal("duplicate fold ID should be rejected")
	}
	request = continuousTestRequest()
	request.Folds[1].Validate.Start = request.Folds[0].Validate.End
	if err := validateContinuousRequest(request); err == nil {
		t.Fatal("overlapping validation folds should be rejected")
	}
	request = continuousTestRequest()
	request.MinimumPositiveFoldRatio = math.NaN()
	if err := validateContinuousRequest(request); err == nil {
		t.Fatal("non-finite continuous gate should be rejected")
	}
}

func TestContinuousQualityHardFailureBlocksShadowPromotion(t *testing.T) {
	result := ContinuousOptimizationResult{
		Request: ContinuousOptimizationRequest{BaseRequest: Request{
			Tickers: []string{"sh600519", "sz000001", "sz300750"}, Adjustment: AdjustmentNone,
			NoFutureData: false,
		}, Folds: make([]WalkForwardFold, 3)},
		Selected: &ContinuousCandidateResult{Folds: make([]WalkForwardFoldResult, 3), PositiveFoldRatio: 1, ValidationTrades: 30, NeighborhoodSize: 1},
		Holdout: &Result{Metrics: Metrics{TotalReturn: 3, MaxDrawdown: -5, Sharpe: 1, Trades: 12}, DataSources: map[string]string{
			"sh600519": "test", "sz000001": "test", "sz300750": "test",
		}},
		Stress: StressResult{DoubleCost: &Result{Metrics: Metrics{TotalReturn: 1}}, BestTradeProfitShare: .3},
	}
	result.Quality = evaluateContinuousDataQuality(&result)
	continuousGate(&result)
	if result.Quality.Passed || result.Stage == ContinuousStageShadow || !strings.Contains(strings.Join(result.GateReasons, "；"), "数据质量") {
		t.Fatalf("hard data-quality failure was allowed to promote: %#v", result)
	}
}

func TestContinuousQualityChecksEachTickerDataSource(t *testing.T) {
	result := ContinuousOptimizationResult{
		Request: ContinuousOptimizationRequest{BaseRequest: Request{
			Tickers: []string{"sh600519", "sz000001", "sz300750"}, Adjustment: AdjustmentNone, NoFutureData: true,
		}, Folds: make([]WalkForwardFold, 3)},
		Holdout: &Result{DataSources: map[string]string{
			"sh600519": "test", "sz000001": "test", "unrelated": "test",
		}},
	}
	quality := evaluateContinuousDataQuality(&result)
	if quality.Passed || !strings.Contains(mustQualityDetail(quality, "行情数据源"), "sz300750") {
		t.Fatalf("unrelated data source masked a missing ticker: %#v", quality)
	}
}

func mustQualityDetail(quality DataQualitySummary, name string) string {
	for _, check := range quality.Checks {
		if check.Name == name {
			return check.Detail
		}
	}
	return ""
}

func TestCandidateStabilityPopulatesConsensusAndNeighborhood(t *testing.T) {
	base := DefaultTechnicalParameters()
	neighbor := base
	neighbor.FastMA = 21
	candidates := []ContinuousCandidateResult{
		{Proposal: StrategyProposal{ID: "base", Parameters: base}, Score: 10},
		{Proposal: StrategyProposal{ID: "neighbor", Parameters: neighbor}, Score: 8},
	}
	agents := []AgentResearchRun{
		{Agent: "risk", Status: "ok", Proposals: []StrategyProposal{{Parameters: base}}},
		{Agent: "trend", Status: "ok", Proposals: []StrategyProposal{{Parameters: base}}},
	}
	enrichCandidateStability(candidates, agents)
	if candidates[0].ConsensusAgents != 2 || candidates[0].NeighborhoodSize != 1 || candidates[0].NeighborhoodScore != 8 {
		t.Fatalf("candidate stability evidence missing: %#v", candidates[0])
	}
}

func TestCandidateStabilityScoresDoNotDependOnIterationOrder(t *testing.T) {
	base := DefaultTechnicalParameters()
	neighbor := base
	neighbor.FastMA = 21
	left := []ContinuousCandidateResult{
		{Proposal: StrategyProposal{ID: "base", Parameters: base}, Score: 10},
		{Proposal: StrategyProposal{ID: "neighbor", Parameters: neighbor}, Score: 8},
	}
	right := []ContinuousCandidateResult{left[1], left[0]}
	agents := []AgentResearchRun{{Agent: "risk", Status: "ok", Proposals: []StrategyProposal{{Parameters: base}}}}
	enrichCandidateStability(left, agents)
	enrichCandidateStability(right, agents)
	scores := map[string]float64{}
	for _, candidate := range left {
		scores[candidate.Proposal.ID] = candidate.Score
	}
	for _, candidate := range right {
		if candidate.Score != scores[candidate.Proposal.ID] {
			t.Fatalf("stability score changed with candidate order: left=%#v right=%#v", left, right)
		}
	}
}

func TestContinuousGateRejectsIsolatedOptimum(t *testing.T) {
	result := ContinuousOptimizationResult{
		Selected: &ContinuousCandidateResult{Folds: make([]WalkForwardFoldResult, 3), PositiveFoldRatio: 1, ValidationTrades: 30},
		Holdout:  &Result{Metrics: Metrics{TotalReturn: 3, MaxDrawdown: -5, Sharpe: 1, Trades: 12}},
		Stress:   StressResult{DoubleCost: &Result{Metrics: Metrics{TotalReturn: 1}}, BestTradeProfitShare: .3},
		Quality:  DataQualitySummary{Grade: "A", Passed: true},
	}
	result.Request.MinimumValidationTrades = 20
	result.Request.MinimumPositiveFoldRatio = .67
	result.Request.MaximumValidationDrawdown = 15
	continuousGate(&result)
	if result.Stage == ContinuousStageShadow || !strings.Contains(strings.Join(result.GateReasons, "；"), "邻域稳定平台") {
		t.Fatalf("isolated optimum should remain research-only: %#v", result)
	}
}

func TestContinuousGateRejectsWeakParameterNeighborhood(t *testing.T) {
	result := ContinuousOptimizationResult{
		Request: ContinuousOptimizationRequest{MinimumValidationTrades: 20, MinimumPositiveFoldRatio: .67},
		Selected: &ContinuousCandidateResult{
			Folds: make([]WalkForwardFoldResult, 3), PositiveFoldRatio: 1, ValidationTrades: 30,
			Score: 20, NeighborhoodSize: 2, NeighborhoodScore: 10,
		},
		Holdout: &Result{Metrics: Metrics{TotalReturn: 3, MaxDrawdown: -5, Sharpe: 1, Trades: 12}},
		Stress:  StressResult{DoubleCost: &Result{Metrics: Metrics{TotalReturn: 1}}, BestTradeProfitShare: .3},
		Quality: DataQualitySummary{Grade: "A", Passed: true},
	}
	continuousGate(&result)
	if result.Stage == ContinuousStageShadow || !strings.Contains(strings.Join(result.GateReasons, "；"), "邻域表现显著弱于") {
		t.Fatalf("weak parameter neighborhood should block promotion: %#v", result)
	}
}

func TestContinuousGateRejectsNonFiniteHoldoutMetrics(t *testing.T) {
	result := ContinuousOptimizationResult{
		Request:  ContinuousOptimizationRequest{MinimumValidationTrades: 20, MinimumPositiveFoldRatio: .67},
		Selected: &ContinuousCandidateResult{Folds: make([]WalkForwardFoldResult, 3), PositiveFoldRatio: 1, ValidationTrades: 30, NeighborhoodSize: 1},
		Holdout:  &Result{Metrics: Metrics{TotalReturn: math.NaN(), MaxDrawdown: -5, Sharpe: 1, Trades: 12}},
		Stress:   StressResult{DoubleCost: &Result{Metrics: Metrics{TotalReturn: 1}}, BestTradeProfitShare: .3},
		Quality:  DataQualitySummary{Grade: "A", Passed: true},
	}
	continuousGate(&result)
	if result.Stage == ContinuousStageShadow || !strings.Contains(strings.Join(result.GateReasons, "；"), "非有限数值") {
		t.Fatalf("non-finite holdout metrics should block promotion: %#v", result)
	}
}

func TestContinuousOptimizerRejectsInsufficientRollingEvidence(t *testing.T) {
	engine := &continuousEngineMock{}
	request := continuousTestRequest()
	request.MinimumValidationTrades = 100
	result, err := NewContinuousOptimizer(engine).Optimize(context.Background(), request, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Selected != nil || result.Holdout != nil || len(result.GateReasons) == 0 {
		t.Fatalf("insufficient rolling evidence should remain research-only: %#v", result)
	}
	if !strings.Contains(strings.Join(result.Candidates[0].Reasons, "；"), "交易数") {
		t.Fatalf("rejection evidence missing: %#v", result.Candidates)
	}
}

func TestEntryModesHaveDistinctTriggers(t *testing.T) {
	base := DefaultTechnicalParameters()
	snapshot := SignalSnapshot{Close: 11, Low: 9.9, PreviousClose: 9.8, FastMA: 10, PreviousFastMA: 10, SlowMA: 9, PriorHigh: 12, VolumeRatio: 1.5}
	if _, ok := entrySignal(snapshot, base); ok {
		t.Fatal("breakout must not trigger below prior high")
	}
	reclaim := base
	reclaim.EntryMode = EntryModeReclaim
	if signal, ok := entrySignal(snapshot, reclaim); !ok || !strings.Contains(strings.Join(signal.Reasons, ""), "重新站上") {
		t.Fatalf("trend reclaim did not trigger: %#v", signal)
	}
	pullback := base
	pullback.EntryMode = EntryModePullback
	if signal, ok := entrySignal(snapshot, pullback); !ok || !strings.Contains(strings.Join(signal.Reasons, ""), "回踩") {
		t.Fatalf("MA pullback did not trigger: %#v", signal)
	}
}
