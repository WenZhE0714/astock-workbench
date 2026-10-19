package backtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const VolumeForwardVersion = "p008-volume-forward-v1"

var forwardLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type ForwardPendingOrder struct {
	Symbol string         `json:"symbol"`
	Side   string         `json:"side"`
	Signal SignalSnapshot `json:"signal"`
	Reason string         `json:"reason"`
}

type VolumeForwardPlan struct {
	Version          string                     `json:"version"`
	CreatedAt        time.Time                  `json:"created_at"`
	SourceID         string                     `json:"source_id"`
	CandidateID      string                     `json:"candidate_id"`
	SourceSHA256     string                     `json:"source_sha256"`
	ExecutableSHA256 string                     `json:"executable_sha256"`
	Baseline         Request                    `json:"baseline"`
	Candidate        Request                    `json:"candidate"`
	Policy           CandidateObservationPolicy `json:"policy"`
	PromotionAllowed bool                       `json:"promotion_allowed"`
	Fingerprint      string                     `json:"fingerprint"`
}

type VolumeForwardDay struct {
	Date             string                         `json:"date"`
	CapturedAt       time.Time                      `json:"captured_at"`
	PlanFingerprint  string                         `json:"plan_fingerprint"`
	PreviousHash     string                         `json:"previous_hash"`
	Bars             forwardBars                    `json:"bars"`
	Runs             []Result                       `json:"runs"`
	NormalImpact     VolumeStudyImpact              `json:"normal_impact"`
	DoubleCostImpact VolumeStudyImpact              `json:"double_cost_impact"`
	Assessment       CandidateObservationAssessment `json:"assessment"`
	SampleReady      bool                           `json:"sample_ready"`
	PromotionAllowed bool                           `json:"promotion_allowed"`
	Fingerprint      string                         `json:"fingerprint"`
}

func ForwardFingerprint(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func validForwardHash(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == sha256.Size
}

func NewVolumeForwardPlan(source VolumeStudyResult, sourceHash, executableHash string, now time.Time) (VolumeForwardPlan, error) {
	var plan VolumeForwardPlan
	if !source.Completed || !source.BaselineReproduced || len(source.BaselineDifferences) != 0 || source.PromotionAllowed || source.CandidateID != "P008" || source.SourceID == "" || len(source.Variants) != 2 || source.MaxPosition != .10 {
		return plan, fmt.Errorf("需要已完成且基线复现成功的P008量比研究")
	}
	a, b := source.Variants[0], source.Variants[1]
	if a.Minimum != 0 || b.Minimum != VolumeStudyMinimum || len(a.Windows) < 2 || len(a.Windows) != len(b.Windows) || !b.Normal.Passed || !b.DoubleCost.Passed {
		return plan, fmt.Errorf("量比候选未通过常规及双倍成本检查")
	}
	base, candidate := a.Windows[0].Normal.Request, b.Windows[0].Normal.Request
	copy := candidate
	copy.EntryVolumeFloor = 0
	if !reflect.DeepEqual(base, copy) || candidate.EntryVolumeFloor != VolumeStudyMinimum || base.EntryVolumeFloor != 0 || base.Technical != (TechnicalParameters{EntryMode: EntryModeReclaim, FastMA: 20, SlowMA: 60, BreakoutDays: 20, VolumeRatioMin: 1.2, StopLoss: .08, TakeProfit: .20, MaxHoldingDays: 40, MaxPosition: .10}) || base.InitialCash != 1000000 || base.EntryMarketFilter != "" || base.EntryConfirmation != "" || base.ExitProtection != "" || !base.NoFutureData || base.Benchmark != "sh000300" {
		return plan, fmt.Errorf("源研究不是固定的单变量P008对照")
	}
	now = now.In(forwardLocation)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, forwardLocation).AddDate(0, 0, 1)
	for _, request := range []*Request{&base, &candidate} {
		request.Start, request.End = start, start
		request.LiquidateAtEnd = false
		request.StrategyVersion = VolumeForwardVersion
	}
	plan = VolumeForwardPlan{Version: VolumeForwardVersion, CreatedAt: now, SourceID: source.SourceID, CandidateID: source.CandidateID, SourceSHA256: sourceHash, ExecutableSHA256: executableHash, Baseline: base, Candidate: candidate, Policy: DefaultCandidateObservationPolicy()}
	var err error
	plan.Fingerprint, err = ForwardFingerprint(plan)
	if err != nil {
		return plan, err
	}
	return plan, ValidateVolumeForwardPlan(plan)
}

func ValidateVolumeForwardPlan(plan VolumeForwardPlan) error {
	want := plan.Fingerprint
	plan.Fingerprint = ""
	hash, err := ForwardFingerprint(plan)
	if err != nil || want != hash || plan.Version != VolumeForwardVersion || plan.PromotionAllowed || plan.CreatedAt.IsZero() || !validForwardHash(plan.SourceSHA256) || !validForwardHash(plan.ExecutableSHA256) {
		return fmt.Errorf("前瞻配置指纹或版本无效")
	}
	base, candidate := plan.Baseline, plan.Candidate
	candidate.EntryVolumeFloor = 0
	if !reflect.DeepEqual(base, candidate) || plan.Candidate.EntryVolumeFloor != VolumeStudyMinimum || base.EntryVolumeFloor != 0 || base.LiquidateAtEnd || base.Start.Format("2006-01-02") <= plan.CreatedAt.In(forwardLocation).Format("2006-01-02") || !base.Start.Equal(base.End) || plan.Policy != DefaultCandidateObservationPolicy() {
		return fmt.Errorf("前瞻起点、规则或样本门槛发生变化")
	}
	return validateRequest(base)
}

// ForwardCollectionDue never backdates collection after a missed close.
func ForwardCollectionDue(plan VolumeForwardPlan, previous *VolumeForwardDay, now time.Time) (bool, string) {
	now = now.In(forwardLocation)
	date := now.Format("2006-01-02")
	if date < plan.Baseline.Start.Format("2006-01-02") {
		return false, "等待冻结起点之后的新交易日"
	}
	if previous != nil && date <= previous.Date {
		return false, "当天已封存"
	}
	if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday || now.Hour()*60+now.Minute() < 15*60+30 {
		return false, "等待交易日15:30后的完整日K"
	}
	if now.Hour() >= 19 {
		return false, "当日采集窗口已结束；如有漏采，后续将停止推进"
	}
	return true, ""
}

type VolumeForwardSourceCheck struct {
	Symbol string `json:"symbol"`
	Bars   int    `json:"bars"`
	Latest string `json:"latest"`
}

// ProbeVolumeForwardSource checks connectivity and warmup without advancing
// the experiment or producing performance evidence.
func ProbeVolumeForwardSource(ctx context.Context, plan VolumeForwardPlan, provider DailyBarProvider, now time.Time) ([]VolumeForwardSourceCheck, error) {
	if err := ValidateVolumeForwardPlan(plan); err != nil {
		return nil, err
	}
	now = now.In(forwardLocation)
	if now.Hour()*60+now.Minute() < 15*60+30 {
		return nil, fmt.Errorf("数据预检需要15:30后的完整日K")
	}
	start := plan.Baseline.Start.AddDate(0, 0, -180)
	checks := []VolumeForwardSourceCheck{}
	for _, symbol := range append([]string{plan.Baseline.Benchmark}, plan.Baseline.Tickers...) {
		bars, err := provider.FetchDailyBarsRange(ctx, symbol, start, now, AdjustmentNone)
		if err != nil {
			return checks, fmt.Errorf("%s: %w", symbol, err)
		}
		bars, err = normalizedForwardBars(bars, start, now.Format("2006-01-02"))
		if err != nil || len(bars) < plan.Baseline.Technical.SlowMA+2 {
			return checks, fmt.Errorf("%s 预热日K无效或不足: %v", symbol, err)
		}
		check := VolumeForwardSourceCheck{Symbol: symbol, Bars: len(bars), Latest: bars[len(bars)-1].Date}
		checks = append(checks, check)
		if check.Latest != now.Format("2006-01-02") {
			return checks, fmt.Errorf("%s 最新日K仅到%s", symbol, check.Latest)
		}
	}
	return checks, nil
}

type forwardBars map[string][]domain.DailyBar

func (bars forwardBars) FetchDailyBarsRange(_ context.Context, symbol string, start, end time.Time, adjustment PriceAdjustment) ([]domain.DailyBar, error) {
	if adjustment != AdjustmentNone {
		return nil, fmt.Errorf("前瞻仅支持不复权日K")
	}
	result := []domain.DailyBar{}
	for _, bar := range bars[symbol] {
		if dateInRange(bar.Date, start, end) {
			result = append(result, bar)
		}
	}
	return result, nil
}

func forwardBarEqual(a, b domain.DailyBar) bool {
	return a.Date == b.Date && a.Open == b.Open && a.High == b.High && a.Low == b.Low && a.Close == b.Close && a.Volume == b.Volume
}

func normalizedForwardBars(bars []domain.DailyBar, start time.Time, date string) ([]domain.DailyBar, error) {
	result := []domain.DailyBar{}
	for _, bar := range bars {
		parsed, err := time.Parse("2006-01-02", bar.Date)
		if err != nil || parsed.Format("2006-01-02") != bar.Date {
			return nil, fmt.Errorf("日K日期无效")
		}
		if bar.Date < start.Format("2006-01-02") || bar.Date > date {
			continue
		}
		if !finiteValues(bar.Open, bar.High, bar.Low, bar.Close, bar.Volume) || bar.Open <= 0 || bar.Close <= 0 || bar.Low <= 0 || bar.High < math.Max(bar.Open, bar.Close) || bar.Low > math.Min(bar.Open, bar.Close) || bar.Volume <= 0 || bar.Source == "" {
			return nil, fmt.Errorf("%s 日K价格、成交量或来源无效", bar.Date)
		}
		result = append(result, bar)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	for i := 1; i < len(result); i++ {
		if result[i-1].Date == result[i].Date {
			return nil, fmt.Errorf("日K日期重复")
		}
	}
	return result, nil
}

func BuildVolumeForwardDay(ctx context.Context, plan VolumeForwardPlan, previous *VolumeForwardDay, provider DailyBarProvider, now time.Time) (VolumeForwardDay, error) {
	var day VolumeForwardDay
	if err := ValidateVolumeForwardPlan(plan); err != nil {
		return day, err
	}
	if due, reason := ForwardCollectionDue(plan, previous, now); !due {
		return day, fmt.Errorf("%s", reason)
	}
	if previous != nil {
		if err := ValidateVolumeForwardDay(plan, *previous); err != nil {
			return day, err
		}
	}
	now = now.In(forwardLocation)
	date := now.Format("2006-01-02")
	end, _ := time.ParseInLocation("2006-01-02", date, forwardLocation)
	warmStart := plan.Baseline.Start.AddDate(0, 0, -max(plan.Baseline.Technical.SlowMA, plan.Baseline.Technical.BreakoutDays+1, 21)*3)
	day = VolumeForwardDay{Date: date, CapturedAt: now, PlanFingerprint: plan.Fingerprint, Bars: map[string][]domain.DailyBar{}}
	if previous != nil {
		day.PreviousHash = previous.Fingerprint
	}
	symbols := append([]string{plan.Baseline.Benchmark}, plan.Baseline.Tickers...)
	for _, symbol := range symbols {
		fetched, err := provider.FetchDailyBarsRange(ctx, symbol, warmStart, end, AdjustmentNone)
		if err != nil {
			return day, fmt.Errorf("%s: %w", symbol, err)
		}
		bars, err := normalizedForwardBars(fetched, warmStart, date)
		if err != nil {
			return day, fmt.Errorf("%s: %w", symbol, err)
		}
		if len(bars) < plan.Baseline.Technical.SlowMA+2 || bars[len(bars)-1].Date != date {
			return day, fmt.Errorf("%s 尚无%s完整日K或预热不足；不封存、不补造停牌", symbol, date)
		}
		if previous != nil {
			old := previous.Bars[symbol]
			if len(bars) != len(old)+1 {
				return day, fmt.Errorf("%s 存在漏采交易日或历史覆盖变化；停止推进", symbol)
			}
			for i := range old {
				if !forwardBarEqual(old[i], bars[i]) {
					return day, fmt.Errorf("%s %s 历史价格或成交量变化；保留封存证据", symbol, old[i].Date)
				}
			}
			bars = append(append([]domain.DailyBar(nil), old...), bars[len(bars)-1])
		}
		day.Bars[symbol] = bars
	}
	calendar := []string{}
	for _, bar := range day.Bars[plan.Baseline.Benchmark] {
		if bar.Date >= plan.Baseline.Start.Format("2006-01-02") {
			calendar = append(calendar, bar.Date)
		}
	}
	expected := 1
	if previous != nil {
		expected += len(previous.Runs[0].Equity)
	}
	if len(calendar) != expected {
		return day, fmt.Errorf("存在未在当日采集的交易日；禁止回填为前瞻证据")
	}
	for _, symbol := range plan.Baseline.Tickers {
		dates := []string{}
		for _, bar := range day.Bars[symbol] {
			if bar.Date >= plan.Baseline.Start.Format("2006-01-02") {
				dates = append(dates, bar.Date)
			}
		}
		if !reflect.DeepEqual(dates, calendar) {
			return day, fmt.Errorf("%s 与基准交易日不齐；暂停整个对照", symbol)
		}
	}
	engine := NewDailyEngine(forwardBars(day.Bars))
	engine.now = func() time.Time { return now }
	for _, stress := range []bool{false, true} {
		for _, request := range []Request{plan.Baseline, plan.Candidate} {
			request.End = end
			if stress {
				request.CommissionRate *= 2
				request.MinimumCommission *= 2
				request.StampDutyRate *= 2
				request.TransferFeeRate *= 2
				request.SlippageBPS *= 2
			}
			run, err := engine.RunForward(ctx, request)
			if err != nil {
				return day, err
			}
			if previous != nil {
				prior := previous.Runs[len(day.Runs)]
				if err := forwardPrefixMatches(prior, run); err != nil {
					return day, err
				}
			}
			day.Runs = append(day.Runs, run)
		}
	}
	day.NormalImpact = volumeStudyImpact(day.Runs[0], day.Runs[1], plan.Baseline.Technical.TakeProfit*100)
	day.DoubleCostImpact = volumeStudyImpact(day.Runs[2], day.Runs[3], plan.Baseline.Technical.TakeProfit*100)
	if math.Abs(day.NormalImpact.ReconciliationError) > .01 || math.Abs(day.DoubleCostImpact.ReconciliationError) > .01 {
		return day, fmt.Errorf("前瞻资金归因未对平")
	}
	day.Assessment = AssessCandidateObservation(plan.Policy, &day.Runs[1])
	stressPassed := day.Runs[3].Metrics.TotalReturn > 0
	day.Assessment.Checks = append(day.Assessment.Checks, CandidateObservationCheck{Key: "double-cost", Name: "双倍成本", Passed: stressPassed, Detail: fmt.Sprintf("双倍成本收益 %+.4f%%，要求大于0", day.Runs[3].Metrics.TotalReturn)})
	day.Assessment.Passed = day.Assessment.Passed && stressPassed
	day.Assessment.Verdict = "继续前瞻观察，不自动晋级"
	day.SampleReady = len(day.Runs[1].Equity) >= plan.Policy.MinimumTradingDays && day.Runs[1].Metrics.Trades >= plan.Policy.MinimumTrades
	if day.SampleReady {
		day.Assessment.Verdict = "样本达到复核数量，需综合收益、风险与错失赢家人工评估"
	}
	var err error
	day.Fingerprint, err = ForwardFingerprint(day)
	return day, err
}

func forwardPrefixMatches(prior, next Result) error {
	if len(next.Equity) != len(prior.Equity)+1 || len(next.Trades) < len(prior.Trades) {
		return fmt.Errorf("前瞻历史净值或成交长度变化")
	}
	for i, point := range prior.Equity {
		n := next.Equity[i]
		if point.Date != n.Date || math.Abs(point.Equity-n.Equity) > .000001 || math.Abs(point.Cash-n.Cash) > .000001 || math.Abs(point.Positions-n.Positions) > .000001 {
			return fmt.Errorf("已封存净值发生变化: %s", point.Date)
		}
	}
	for i := range prior.Trades {
		if !reflect.DeepEqual(prior.Trades[i], next.Trades[i]) {
			return fmt.Errorf("已封存成交发生变化")
		}
	}
	for _, position := range prior.OpenPositions {
		matched := false
		for _, trade := range next.Trades {
			if trade.Symbol == position.Symbol && reflect.DeepEqual(trade.Entry, position.Entry) && reflect.DeepEqual(trade.EntrySignal, position.EntrySignal) {
				matched = true
			}
		}
		for _, retained := range next.OpenPositions {
			if retained.Symbol == position.Symbol && reflect.DeepEqual(retained.Entry, position.Entry) && reflect.DeepEqual(retained.EntrySignal, position.EntrySignal) {
				matched = true
			}
		}
		if !matched {
			return fmt.Errorf("已封存持仓入场发生变化: %s", position.Symbol)
		}
	}
	return nil
}

func ValidateVolumeForwardDay(plan VolumeForwardPlan, day VolumeForwardDay) error {
	want := day.Fingerprint
	day.Fingerprint = ""
	hash, err := ForwardFingerprint(day)
	if err != nil || want != hash || day.PlanFingerprint != plan.Fingerprint || day.PromotionAllowed || len(day.Runs) != 4 || day.Date != day.CapturedAt.In(forwardLocation).Format("2006-01-02") || day.Date < plan.Baseline.Start.Format("2006-01-02") {
		return fmt.Errorf("前瞻日档指纹、日期或对照数量无效")
	}
	for _, run := range day.Runs {
		if len(run.Equity) == 0 || run.Equity[len(run.Equity)-1].Date != day.Date || run.Request.LiquidateAtEnd {
			return fmt.Errorf("前瞻净值日期或期末持仓规则无效")
		}
	}
	return nil
}
