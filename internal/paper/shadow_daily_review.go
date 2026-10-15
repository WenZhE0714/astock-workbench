package paper

import (
	"sort"
	"time"

	"github.com/wenzhe/astock-workbench/internal/realtime"
)

type ShadowGateCount struct {
	Code     string   `json:"code"`
	Label    string   `json:"label"`
	Count    int      `json:"count"`
	Examples []string `json:"examples,omitempty"`
}

type ShadowScanReview struct {
	Scans             int               `json:"scans"`
	Observations      int               `json:"observations"`
	Symbols           int               `json:"symbols"`
	Eligible          int               `json:"eligible"`
	EligibleSymbols   int               `json:"eligible_symbols"`
	ExcludedScans     int               `json:"excluded_scans"`
	DuplicateScans    int               `json:"duplicate_scans"`
	BeforeStart       int               `json:"before_start"`
	InvalidSignals    int               `json:"invalid_signals"`
	HighestEntryScore float64           `json:"highest_entry_score"`
	FirstAt           time.Time         `json:"first_at,omitzero"`
	LastAt            time.Time         `json:"last_at,omitzero"`
	Gates             []ShadowGateCount `json:"gates"`
}

type ShadowScanReviewBuilder struct {
	date     string
	config   Config
	result   ShadowScanReview
	seen     map[string]bool
	symbols  map[string]bool
	eligible map[string]bool
	gates    map[string]*ShadowGateCount
}

func NewShadowScanReviewBuilder(date string, cfg Config) *ShadowScanReviewBuilder {
	return &ShadowScanReviewBuilder{date: date, config: cfg, seen: map[string]bool{}, symbols: map[string]bool{}, eligible: map[string]bool{}, gates: map[string]*ShadowGateCount{}}
}

func (b *ShadowScanReviewBuilder) Add(scan realtime.ScanResult) {
	if scan.GeneratedAt.IsZero() || scan.GeneratedAt.In(shanghaiLocation).Format(time.DateOnly) != b.date || scan.MarketState != realtime.MarketStateTrading || scan.TradingDate != "" && (!scan.TradingDay || scan.TradingDate != b.date) {
		b.result.ExcludedScans++
		return
	}
	if !b.config.TradingStartAt.IsZero() && scan.GeneratedAt.Before(b.config.TradingStartAt) {
		b.result.BeforeStart++
		return
	}
	key := scan.GeneratedAt.UTC().Format(time.RFC3339Nano) + "|" + scan.Universe
	if b.seen[key] {
		b.result.DuplicateScans++
		return
	}
	b.seen[key] = true
	b.result.Scans++
	if b.result.FirstAt.IsZero() || scan.GeneratedAt.Before(b.result.FirstAt) {
		b.result.FirstAt = scan.GeneratedAt
	}
	if scan.GeneratedAt.After(b.result.LastAt) {
		b.result.LastAt = scan.GeneratedAt
	}
	seenSymbols := map[string]bool{}
	for _, signal := range scan.Signals {
		if signal.Symbol == "" || seenSymbols[signal.Symbol] || signal.AsOf.IsZero() || signal.AsOf.After(scan.GeneratedAt) || signalDate(signal) != b.date {
			b.result.InvalidSignals++
			continue
		}
		seenSymbols[signal.Symbol] = true
		if !b.config.TradingStartAt.IsZero() && signal.AsOf.Before(b.config.TradingStartAt) {
			b.result.InvalidSignals++
			continue
		}
		b.symbols[signal.Symbol] = true
		b.result.Observations++
		if score := shadowSignalScoreForConfig(signal, b.config); finite(score) && score > b.result.HighestEntryScore {
			b.result.HighestEntryScore = score
		}
		if shadowSignalEntryEligible(signal, b.config) {
			b.result.Eligible++
			b.eligible[signal.Symbol] = true
			continue
		}
		code, label := shadowEntryGateReason(signal, b.config)
		gate := b.gates[code]
		if gate == nil {
			gate = &ShadowGateCount{Code: code, Label: label}
			b.gates[code] = gate
		}
		gate.Count++
		example := signal.Symbol
		if signal.Name != "" {
			example += " " + signal.Name
		}
		found := false
		for _, old := range gate.Examples {
			found = found || old == example
		}
		if !found && len(gate.Examples) < 3 {
			gate.Examples = append(gate.Examples, example)
		}
	}
}

func (b *ShadowScanReviewBuilder) Result() ShadowScanReview {
	result := b.result
	result.Symbols, result.EligibleSymbols = len(b.symbols), len(b.eligible)
	result.Gates = []ShadowGateCount{}
	for _, gate := range b.gates {
		result.Gates = append(result.Gates, *gate)
	}
	sort.Slice(result.Gates, func(i, j int) bool {
		if result.Gates[i].Count == result.Gates[j].Count {
			return result.Gates[i].Code < result.Gates[j].Code
		}
		return result.Gates[i].Count > result.Gates[j].Count
	})
	return result
}

// Diagnostics use the execution engine's eligibility result above. These
// labels explain failed qualification; they do not claim an order was rejected.
func shadowEntryGateReason(signal realtime.Signal, cfg Config) (string, string) {
	if cfg.UseMonsterRadar {
		switch signal.MarketRegime {
		case "熊市":
			return "monster-bear", "熊市不进入抓妖候选"
		case "高波动":
			return "monster-volatility", "高波动环境不进入抓妖候选"
		case "数据不足":
			return "market-data", "市场状态数据不足"
		}
		if signal.Monster.Score < cfg.MinimumScore {
			return "monster-score", "雷达评分低于账户门槛"
		}
		if !signal.Monster.QuoteFresh {
			return "monster-quote", "雷达行情新鲜度未通过"
		}
		return "monster-conditions", "雷达阶段、边界或行业条件未齐备"
	}
	minimum := cfg.MinimumScore
	if cfg.UseCalibratedScore && signal.CalibrationID != "" && signal.CalibrationMinimumScore > 0 {
		minimum = signal.CalibrationMinimumScore
	}
	if shadowSignalStateForConfig(signal, cfg) == realtime.StateInvalid {
		return "signal-data", "关键因子或市场状态不足"
	}
	if shadowSignalScoreForConfig(signal, cfg) < minimum {
		if signal.RiskMultiplier > 0 && signal.RiskMultiplier < 1 {
			return "risk-adjusted-score", "市场风险折算后未达评分门槛"
		}
		return "entry-score", "入场评分未达账户门槛"
	}
	if cfg.UseCalibratedScore && signal.CalibrationID != "" {
		return "calibrated-pool", "校准组合候选筛选未通过"
	}
	return "portfolio-pool", "组合候选状态、排名或集中度筛选未通过"
}

func ShadowDailyReviewForDate(report Report, date string) *ShadowDailyReview {
	// This view uses date-filtered ledger events, not a historical revaluation.
	report.AsOf = date
	review := buildShadowDailyReview(report)
	if review != nil {
		review.CalibrationStatus, review.CalibrationSummary = "", ""
	}
	return review
}
