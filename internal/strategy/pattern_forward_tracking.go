package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

const PatternForwardTrackingVersion = "pattern-forward-tracking-v1"

type forwardPrepared struct {
	snapshot domain.PatternForwardSnapshot
	bars     []domain.DailyBar
	info     domain.PatternAdjustment
	check    *domain.PatternActionCheck
	err      error
}

func prepareForwardTracking(snapshot domain.PatternForwardSnapshot, benchmark []domain.DailyBar, basis string) forwardPrepared {
	p := forwardPrepared{snapshot: snapshot}
	var coverage domain.PatternValidationCoverage
	p.bars, coverage = patternValidationBars(snapshot.Input, snapshot.Date)
	if coverage.Error != "" || len(p.bars) == 0 || coverage.LastDate != snapshot.Date {
		p.err = fmt.Errorf("留存日K无效: %s", coverage.Error)
		return p
	}
	actions := normalizePatternActions(snapshot.Input.Actions, snapshot.Symbol, snapshot.Date)
	p.check = checkPatternActions(actions, p.bars[0].Date, snapshot.Date)
	if basis == domain.PatternPriceHFQ {
		raw := p.bars
		var factors domain.AdjustmentSnapshot
		p.bars, factors, p.info = preparePatternAdjustment(snapshot.Input.Adjustment, snapshot.Symbol, snapshot.Date, raw)
		if p.info.Status != "applied" {
			p.err = fmt.Errorf("后复权留存输入无效: %s", p.info.Error)
			return p
		}
		p.info.Quality = validatePatternAdjustmentQuality(actions, factors.Factors, raw, benchmark, snapshot.Date)
		applyPatternShareVolumes(p.bars, &p.info, actions)
		sample := domain.PatternValidationSample{ActionCheck: p.check}
		applyAdjustedPatternActionChecks(&sample, actions, p.info.Quality)
	}
	return p
}

func forwardScale(p forwardPrepared, date string) (float64, float64) {
	price, shares := 1.0, 1.0
	if p.snapshot.Input.Adjustment != nil && p.info.AnchorFactor > 0 {
		for _, factor := range p.snapshot.Input.Adjustment.Factors {
			if factor.Date <= date {
				price = factor.Value / p.info.AnchorFactor
			}
		}
	}
	for _, check := range p.info.Quality {
		if check.Date <= date && check.Date > p.info.AnchorDate && check.State == "passed" && check.ShareRatio != nil {
			shares *= *check.ShareRatio
		}
	}
	return price, shares
}

func forwardScalesContinuous(previous, current forwardPrepared) bool {
	if previous.info.AnchorDate != current.info.AnchorDate {
		return false
	}
	for _, bar := range previous.bars {
		a, b := forwardScale(previous, bar.Date)
		c, d := forwardScale(current, bar.Date)
		if math.Abs(a-c) > 1e-10*math.Max(1, math.Abs(a)) || math.Abs(b-d) > 1e-10*math.Max(1, math.Abs(b)) {
			return false
		}
	}
	return true
}

func mergeForwardActionCheck(previous, current *domain.PatternActionCheck, start, end string) *domain.PatternActionCheck {
	result := &domain.PatternActionCheck{State: "unverified", Start: start, End: end, Detail: "公司行动核验缺失"}
	if previous == nil || current == nil {
		return result
	}
	rank := map[string]int{"clear": 0, "adjusted": 1, "affected": 2, "unverified": 3}
	result.State, result.Detail = previous.State, previous.Detail
	if rank[current.State] > rank[previous.State] {
		result.State, result.Detail = current.State, current.Detail
	}
	seen := map[string]bool{}
	for _, check := range []*domain.PatternActionCheck{previous, current} {
		for _, event := range check.Events {
			if !seen[event.ID] {
				seen[event.ID] = true
				result.Events = append(result.Events, event)
			}
		}
	}
	return result
}

// BuildPatternForwardTracking consumes a sealed archive prefix, never a fresh
// historical download. Only each later snapshot's own daily bar is appended.
func BuildPatternForwardTracking(ctx context.Context, task domain.PatternForwardTask, evidence []domain.PatternForwardEvidence, previous []domain.PatternForwardTracking, dates []string, now time.Time) (domain.PatternForwardTracking, error) {
	result := domain.PatternForwardTracking{Version: PatternForwardTrackingVersion, TaskID: task.ID, ConfigHash: task.ConfigHash, EvaluatedAt: now, Records: []domain.PatternForwardProgress{}}
	if !PatternForwardCompatible(task) || len(evidence) == 0 || now.IsZero() {
		return result, fmt.Errorf("前向跟踪需要兼容的冻结任务及已封存证据")
	}
	result.Date = evidence[len(evidence)-1].Day.Date
	if result.Date > now.In(chartLocation).Format(time.DateOnly) {
		return result, fmt.Errorf("不能跟踪未来采集日")
	}
	for i, entry := range evidence {
		if entry.Day.TaskID != task.ID || entry.Day.SealedAt.IsZero() || (i > 0 && entry.Day.Date <= evidence[i-1].Day.Date) {
			return result, fmt.Errorf("前向跟踪证据须按日期封存")
		}
		dates = append(dates, entry.Day.Date)
	}
	for _, date := range realtime.NormalizeTradingDates(dates) {
		if date >= task.Config.StartOn && date <= result.Date {
			result.TradingDates = append(result.TradingDates, date)
		}
	}
	latest := map[string]domain.PatternForwardProgress{}
	if len(previous) > 0 {
		last := previous[len(previous)-1]
		if last.Date >= result.Date || last.Version != result.Version || last.ConfigHash != task.ConfigHash {
			return result, fmt.Errorf("跟踪版本或日志顺序不匹配")
		}
		result.PreviousHash = last.Hash
		priorDates := []string{}
		for _, date := range result.TradingDates {
			if date <= last.Date {
				priorDates = append(priorDates, date)
			}
		}
		if fmt.Sprint(priorDates) != fmt.Sprint(last.TradingDates) {
			return result, fmt.Errorf("交易日历修订涉及已封存跟踪区间，未重写原结果")
		}
	}
	for _, ledger := range previous {
		for _, record := range ledger.Records {
			latest[record.Sample.ID] = record
		}
	}
	byDate := map[string]domain.PatternForwardEvidence{}
	stocks := map[string]map[string]domain.PatternForwardSnapshot{}
	for _, entry := range evidence {
		byDate[entry.Day.Date] = entry
		stocks[entry.Day.Date] = map[string]domain.PatternForwardSnapshot{}
		for _, stock := range entry.Stocks {
			if stock.TaskID != task.ID || stock.Date != entry.Day.Date || stock.ConfigHash != task.ConfigHash || entry.Benchmark == nil || stock.BenchmarkHash != entry.Benchmark.Hash {
				return result, fmt.Errorf("跟踪输入与原始快照不匹配")
			}
			stocks[entry.Day.Date][stock.Symbol] = stock
		}
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return result, err
	}
	hash := sha256.Sum256(encoded)
	result.EvidenceHash = hex.EncodeToString(hash[:])
	prepared := map[string]forwardPrepared{}
	prepare := func(date, symbol string) forwardPrepared {
		key := date + symbol
		if p, ok := prepared[key]; ok {
			return p
		}
		snapshot, ok := stocks[date][symbol]
		if !ok {
			return forwardPrepared{err: fmt.Errorf("当日股票快照缺采")}
		}
		var benchmark []domain.DailyBar
		if ref := byDate[date].Benchmark; ref != nil {
			benchmark = ref.Input.Bars
		}
		p := prepareForwardTracking(snapshot, benchmark, task.Config.PriceBasis)
		prepared[key] = p
		return p
	}
	for _, entry := range evidence {
		for _, stock := range entry.Stocks {
			for _, original := range stock.Samples {
				if err := ctx.Err(); err != nil {
					return result, err
				}
				old := latest[original.ID]
				if old.Finished {
					continue
				}
				if original.ObservedOn != stock.Date || original.Snapshot.Pattern == nil || len(original.Snapshot.Anchors) == 0 {
					return result, fmt.Errorf("原始观察缺少冻结形态")
				}
				origin := prepare(stock.Date, stock.Symbol)
				if origin.err != nil {
					return result, origin.err
				}
				record := buildForwardProgress(task, original, origin, old, result.Date, result.TradingDates, byDate, prepare)
				result.Records = append(result.Records, record)
			}
		}
	}
	sort.Slice(result.Records, func(i, j int) bool { return result.Records[i].Sample.ID < result.Records[j].Sample.ID })
	return result, nil
}

func buildForwardProgress(task domain.PatternForwardTask, original domain.PatternValidationSample, origin forwardPrepared, old domain.PatternForwardProgress, cutoff string, dates []string, evidence map[string]domain.PatternForwardEvidence, prepare func(string, string) forwardPrepared) domain.PatternForwardProgress {
	record := domain.PatternForwardProgress{ObservationHash: origin.snapshot.Hash, AsOf: cutoff, MissingDates: []string{}}
	bars := append([]domain.DailyBar{}, origin.bars...)
	ready, contiguous := len(bars)-1, len(bars)
	benchmark := []domain.DailyBar{}
	if ref := evidence[original.ObservedOn].Benchmark; ref != nil {
		for _, bar := range ref.Input.Bars {
			if bar.Date <= original.ObservedOn {
				benchmark = append(benchmark, bar)
			}
		}
	}
	checks := map[string]*domain.PatternActionCheck{original.ObservedOn: original.ActionCheck}
	check, previous, gap := original.ActionCheck, origin, false
	for _, date := range dates {
		if date <= original.ObservedOn {
			continue
		}
		entry := evidence[date]
		if ref := entry.Benchmark; ref != nil {
			for _, bar := range ref.Input.Bars {
				if bar.Date == date {
					benchmark = append(benchmark, bar)
				}
			}
		}
		point := prepare(date, original.Symbol)
		reason := ""
		if point.err != nil {
			reason = point.err.Error()
		} else if task.Config.PriceBasis == domain.PatternPriceHFQ && !forwardScalesContinuous(previous, point) {
			reason = "历史复权或股数归一口径变动，不能拼接冻结点位"
		}
		if reason != "" {
			gap = true
			record.MissingDates = append(record.MissingDates, date)
			if record.Detail == "" {
				record.Detail = date + ": " + reason
			}
		} else {
			bars = append(bars, point.bars[len(point.bars)-1])
			previous = point
			if !gap {
				contiguous = len(bars)
			}
			check = mergeForwardActionCheck(check, point.check, bars[0].Date, date)
		}
		checks[date] = check
	}
	sample := buildPatternSample(original.ID, original.Symbol, original.Snapshot, original.SnapshotFingerprint, bars[:contiguous], ready, benchmark)
	sample.ActionCheck = checks[cutoff]
	if gap && sample.State != "invalidated" && sample.State != "expired" {
		sample.State = "unavailable"
	}
	confirmed := -1
	if sample.ConfirmedOn != "" {
		confirmed = sort.Search(len(bars), func(i int) bool { return bars[i].Date >= sample.ConfirmedOn })
		sample.ActionCheck = checks[sample.ConfirmedOn]
	}
	// The lifecycle uses frozen stock bars; the outcome clock uses explicit
	// session dates. These date-only rows never supply benchmark prices.
	calendar := make([]domain.DailyBar, len(dates))
	for i, date := range dates {
		calendar[i].Date = date
	}
	sample.Outcomes = nil
	terminal := 0
	for _, horizon := range []int{5, 10, 20} {
		outcome := patternOutcome(sample, bars, confirmed, calendar, horizon)
		if confirmed < 0 && gap && sample.State == "unavailable" {
			outcome.State, outcome.Detail = "unavailable", "确认前存在缺采，无法判定首次确认或失效"
		}
		end := cutoff
		if outcome.Through != "" {
			end = outcome.Through
		}
		outcome.ActionCheck = checks[end]
		outcome.Benchmark = comparePatternBenchmark(sample, outcome, benchmark)
		for _, frozen := range old.Sample.Outcomes {
			if frozen.Horizon == horizon && (frozen.State == "mature" || frozen.State == "unavailable") {
				outcome = frozen
			}
		}
		if outcome.State != "pending" {
			terminal++
		}
		sample.Outcomes = append(sample.Outcomes, outcome)
	}
	record.Sample = sample
	record.Finished = (confirmed >= 0 && terminal == 3) || (confirmed < 0 && (sample.State == "invalidated" || sample.State == "expired" || sample.State == "unavailable"))
	return record
}
