package paper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/realtime"
)

const PlanExperimentVersion = "plan-pair-v1"

type PlanExperimentSelection struct {
	Plan      domain.TradePlan `json:"plan"`
	Enabled   bool             `json:"enabled"`
	EnabledAt time.Time        `json:"enabled_at"`
}

type PlanExperimentIntent struct {
	At       time.Time `json:"at"`
	QuoteAt  time.Time `json:"quote_at"`
	Price    float64   `json:"price"`
	Sequence uint64    `json:"monitor_sequence"`
	Reason   string    `json:"reason"`
}

type PlanExperimentTrial struct {
	PlanID         string                `json:"plan_id"`
	Symbol         string                `json:"symbol"`
	Status         string                `json:"status"`
	Reason         string                `json:"reason"`
	LastQuoteAt    time.Time             `json:"last_quote_at,omitzero"`
	RetryAfter     time.Time             `json:"retry_after,omitzero"`
	PendingEntry   *PlanExperimentIntent `json:"pending_entry,omitempty"`
	PendingExit    *PlanExperimentIntent `json:"pending_exit,omitempty"`
	EntryTriggerAt time.Time             `json:"entry_trigger_at,omitzero"`
	EntryOrderID   string                `json:"entry_order_id,omitempty"`
	ExitOrderID    string                `json:"exit_order_id,omitempty"`
}

type PlanExperimentArm struct {
	ID          string                         `json:"id"`
	Name        string                         `json:"name"`
	Report      Report                         `json:"report"`
	Trials      map[string]PlanExperimentTrial `json:"trials"`
	PeakEquity  float64                        `json:"peak_equity"`
	MaxDrawdown float64                        `json:"max_drawdown_percent"`
}

type PlanExperimentPoint struct {
	At              time.Time `json:"at"`
	RangeEquity     float64   `json:"range_equity"`
	ConfirmedEquity float64   `json:"confirmed_equity"`
}

type PlanExperimentChange struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	PlanID  string    `json:"plan_id,omitempty"`
	Enabled bool      `json:"enabled"`
}

type PlanExperiment struct {
	Version           string                             `json:"version"`
	CreatedAt         time.Time                          `json:"created_at"`
	UpdatedAt         time.Time                          `json:"updated_at"`
	StartedAt         time.Time                          `json:"started_at,omitzero"`
	EntriesEnabled    bool                               `json:"entries_enabled"`
	EntriesEnabledAt  time.Time                          `json:"entries_enabled_at,omitzero"`
	Status            string                             `json:"status"`
	Message           string                             `json:"message"`
	Selections        map[string]PlanExperimentSelection `json:"selections"`
	Arms              []PlanExperimentArm                `json:"arms"`
	ValuationComplete bool                               `json:"valuation_complete"`
	ValuedAt          time.Time                          `json:"valued_at,omitzero"`
	Equity            []PlanExperimentPoint              `json:"equity"`
	Changes           []PlanExperimentChange             `json:"changes"`
}

// PlanExperimentInput is one already-fetched point-in-time observation. Quote
// qualification is independent of monitoring so risk exits survive a stopped
// monitor or a stale daily-history source.
type PlanExperimentInput struct {
	Symbol        string
	Quote         PositionQuote
	Qualified     bool
	Error         string
	Bars          []domain.DailyBar
	CalendarDates []string
	Monitors      map[string]domain.PlanMonitor
}

func PlanExperimentConfig() Config {
	cfg := DefaultConfig()
	cfg.MaxPositionPercent, cfg.MaxPortfolioPercent, cfg.CashReservePercent = 10, 60, 40
	cfg.MaxDailyDeploymentPercent, cfg.InitialEntryPercent, cfg.MaxEntryTranches = 20, 100, 1
	cfg.MaxPositionRiskPercent, cfg.MaxPortfolioRiskPercent = 1, 4
	cfg.UnlimitedOpenPositions, cfg.MaxOpenPositions = true, 0
	cfg.DisableIntradayT, cfg.EnableIntradayT = true, false
	return canonicalConfig(cfg)
}

func newPlanExperiment(now time.Time) PlanExperiment {
	state := PlanExperiment{Version: PlanExperimentVersion, CreatedAt: now, UpdatedAt: now, Status: "paused", Message: "尚未允许新开仓", Selections: make(map[string]PlanExperimentSelection), Equity: []PlanExperimentPoint{}, Changes: []PlanExperimentChange{}}
	for _, definition := range []struct{ id, name string }{{"range", "区间条件组"}, {"confirmed", "仅确认对照组"}} {
		cfg := PlanExperimentConfig()
		report := Report{EngineVersion: ShadowEngineVersion, ConfigFingerprint: ConfigFingerprint(cfg), Config: cfg, InitialCash: cfg.InitialCash, RemainingCash: cfg.InitialCash, GeneratedAt: now, ExecutionMode: ExecutionModeLive, CheckpointPhase: CheckpointOpen}
		recomputeAccountMetrics(&report)
		state.Arms = append(state.Arms, PlanExperimentArm{ID: definition.id, Name: definition.name, Report: report, Trials: make(map[string]PlanExperimentTrial), PeakEquity: cfg.InitialCash})
	}
	return state
}

func clonePlanExperiment(state PlanExperiment) (PlanExperiment, error) {
	// Keep mutation callbacks isolated from a previously loaded ledger, including
	// frozen plans, lots and pending intents stored in maps.
	data, err := json.Marshal(state)
	if err != nil {
		return PlanExperiment{}, err
	}
	var result PlanExperiment
	err = json.Unmarshal(data, &result)
	return result, err
}

func sameExperimentJSON(left, right any) bool {
	a, first := json.Marshal(left)
	b, second := json.Marshal(right)
	return first == nil && second == nil && bytes.Equal(a, b)
}

func experimentPrefixUnchanged[T any](previous, next []T) bool {
	return len(next) >= len(previous) && (len(previous) == 0 || sameExperimentJSON(previous, next[:len(previous)]))
}

func ConfigurePlanExperiment(current PlanExperiment, enabled bool, now time.Time) (PlanExperiment, error) {
	state, err := clonePlanExperiment(current)
	if err != nil {
		return current, err
	}
	if state.Version == "" {
		state = newPlanExperiment(now)
	}
	if state.Version != PlanExperimentVersion || now.IsZero() || now.Before(state.UpdatedAt) {
		return current, fmt.Errorf("实验版本或操作时间无效")
	}
	if state.EntriesEnabled == enabled {
		return state, nil
	}
	state.EntriesEnabled, state.UpdatedAt = enabled, now
	state.Status, state.Message = "paused", "停止新开仓，已有持仓继续估值和风控"
	if enabled {
		state.EntriesEnabledAt = now
		state.Status, state.Message = "waiting", "等待纳入计划的新报价"
	}
	for index := range state.Arms {
		for id, trial := range state.Arms[index].Trials {
			trial.PendingEntry = nil
			if trial.EntryOrderID == "" {
				trial.Status = "waiting"
				trial.Reason = state.Message
			}
			state.Arms[index].Trials[id] = trial
		}
	}
	state.Changes = append(state.Changes, PlanExperimentChange{At: now, Kind: "entries", Enabled: enabled})
	return state, nil
}

func SelectPlanExperiment(current PlanExperiment, plan domain.TradePlan, enabled bool, now time.Time) (PlanExperiment, error) {
	state, err := clonePlanExperiment(current)
	if err != nil {
		return current, err
	}
	if state.Version == "" {
		state = newPlanExperiment(now)
	}
	if state.Version != PlanExperimentVersion || now.IsZero() || now.Before(state.UpdatedAt) || plan.ID == "" || plan.Structure.Plan == nil || plan.Analysis.Version != "chart-v1" {
		return current, fmt.Errorf("实验或计划快照无效")
	}
	if len(plan.ID) != 64 || plan.CreatedAt.After(now) {
		return current, fmt.Errorf("计划标识或创建时间无效")
	}
	if _, err := time.Parse(time.DateOnly, plan.ExpiresOn); err != nil {
		return current, fmt.Errorf("计划有效期无效")
	}
	levels := plan.Structure.Plan
	if levels.EntryLow <= 0 || levels.Invalidation <= 0 || levels.Invalidation >= levels.EntryLow || levels.EntryHigh < levels.EntryLow || levels.Target2 <= levels.EntryHigh {
		return current, fmt.Errorf("计划价格区间无效")
	}
	if enabled && plan.ExpiresOn < localTradingDate(now) {
		return current, fmt.Errorf("过期计划不能纳入实验")
	}
	selection, exists := state.Selections[plan.ID]
	if exists && !sameExperimentJSON(selection.Plan, plan) {
		return current, fmt.Errorf("已纳入的计划快照不能改写")
	}
	if exists && selection.Enabled == enabled {
		return state, nil
	}
	selection.Plan, selection.Enabled, selection.EnabledAt = plan, enabled, now
	state.Selections[plan.ID] = selection
	state.UpdatedAt = now
	state.Changes = append(state.Changes, PlanExperimentChange{At: now, Kind: "selection", PlanID: plan.ID, Enabled: enabled})
	for index := range state.Arms {
		trial, found := state.Arms[index].Trials[plan.ID]
		if !found {
			trial = PlanExperimentTrial{PlanID: plan.ID, Symbol: plan.Symbol, Status: "waiting", Reason: "等待新报价确认入场条件"}
		}
		trial.PendingEntry = nil
		if trial.EntryOrderID == "" {
			trial.Status = "waiting"
			trial.Reason = "等待实验与监控条件"
		}
		state.Arms[index].Trials[plan.ID] = trial
	}
	return state, nil
}

func PlanExperimentNeedsQuotes(state PlanExperiment) bool {
	for _, arm := range state.Arms {
		if len(arm.Report.Positions) > 0 {
			return true
		}
	}
	if state.EntriesEnabled {
		for _, selection := range state.Selections {
			if selection.Enabled {
				return true
			}
		}
	}
	return false
}

func AdvancePlanExperiment(current PlanExperiment, inputs []PlanExperimentInput, now time.Time, mode string) (PlanExperiment, error) {
	if current.Version == "" {
		return current, nil
	}
	if current.Version != PlanExperimentVersion || now.IsZero() || now.Before(current.UpdatedAt) {
		return current, fmt.Errorf("实验版本或推进时间无效")
	}
	state, err := clonePlanExperiment(current)
	if err != nil {
		return current, err
	}
	state.UpdatedAt = now
	state.Status, state.Message = "paused", "停止新开仓，已有持仓继续估值和风控"
	if state.EntriesEnabled {
		state.Status, state.Message = "waiting", "等待有效报价与监控条件"
	}
	if mode != "trading" && mode != "closing" {
		state.Status, state.Message = "closed", "非交易时段，保留账本并等待新报价"
		return state, nil
	}
	bySymbol := make(map[string]PlanExperimentInput)
	for _, input := range inputs {
		input.Qualified = input.Qualified && input.Symbol == input.Quote.Symbol && validExperimentQuote(input.Quote, now, mode)
		bySymbol[input.Symbol] = input
	}
	ids := make([]string, 0, len(state.Selections))
	for id := range state.Selections {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		left, right := state.Selections[ids[i]].EnabledAt, state.Selections[ids[j]].EnabledAt
		if left.Equal(right) {
			return ids[i] < ids[j]
		}
		return left.Before(right)
	})
	anyQuote, complete := false, true
	for _, input := range bySymbol {
		if input.Qualified {
			anyQuote = true
		}
	}
	if anyQuote && state.StartedAt.IsZero() && state.EntriesEnabled {
		for _, selection := range state.Selections {
			input := bySymbol[selection.Plan.Symbol]
			at, _ := parseRealtimeTimestamp(input.Quote.QuoteTime)
			if selection.Enabled && input.Qualified && !at.Before(state.EntriesEnabledAt) && !at.Before(selection.EnabledAt) {
				state.StartedAt = now
				break
			}
		}
	}
	for armIndex := range state.Arms {
		arm := &state.Arms[armIndex]
		cutoff := parseTimestampOrZero(arm.Report.LastRealtimeAt)
		// Exits free risk budget before the new-entry pass in both arms.
		for _, id := range ids {
			selection := state.Selections[id]
			trial := arm.Trials[id]
			input := bySymbol[selection.Plan.Symbol]
			positionIndex := realtimePositionIndex(arm.Report.Positions, selection.Plan.Symbol)
			if positionIndex >= 0 && arm.Report.Positions[positionIndex].SignalID == id {
				at, _ := parseRealtimeTimestamp(input.Quote.QuoteTime)
				markAt := parseTimestampOrZero(arm.Report.Positions[positionIndex].ValuationTime)
				if !input.Qualified || at.Before(markAt) {
					complete = false
					trial.Reason = "持仓报价无效，保留原估值并暂停撮合"
					arm.Trials[id] = trial
					continue
				}
				if at, _ := parseRealtimeTimestamp(input.Quote.QuoteTime); at.After(cutoff) {
					advanceExperimentExit(arm, &trial, selection, input, now, mode)
				}
				arm.Trials[id] = trial
			}
		}
		for _, id := range ids {
			selection := state.Selections[id]
			trial := arm.Trials[id]
			if trial.EntryOrderID != "" {
				continue
			}
			input := bySymbol[selection.Plan.Symbol]
			if at, _ := parseRealtimeTimestamp(input.Quote.QuoteTime); !input.Qualified || at.After(cutoff) {
				advanceExperimentEntry(&state, arm, &trial, selection, input, now, mode)
			}
			arm.Trials[id] = trial
		}
		quotes := make([]PositionQuote, 0, len(inputs))
		for _, input := range bySymbol {
			if input.Qualified {
				if at, _ := parseRealtimeTimestamp(input.Quote.QuoteTime); !at.Before(cutoff) {
					quotes = append(quotes, input.Quote)
					if input.Quote.QuoteTime > arm.Report.LastRealtimeAt {
						arm.Report.LastRealtimeAt = input.Quote.QuoteTime
						arm.Report.AsOf = localTradingDate(at)
					}
				}
			}
		}
		arm.Report = RevaluePositions(arm.Report, quotes, now)
		arm.Report.OpenPositions = len(arm.Report.Positions)
		recomputeTradeStats(&arm.Report)
	}
	state.ValuationComplete = complete && (anyQuote || !PlanExperimentNeedsQuotes(state))
	if complete && anyQuote {
		for index := range state.Arms {
			arm := &state.Arms[index]
			arm.PeakEquity = math.Max(arm.PeakEquity, arm.Report.TotalEquity)
			if arm.PeakEquity > 0 {
				arm.MaxDrawdown = math.Max(arm.MaxDrawdown, (arm.PeakEquity-arm.Report.TotalEquity)/arm.PeakEquity*100)
			}
		}
		state.ValuedAt = now
		state.Status, state.Message = "running", "两组使用同批报价，费用与风险约束一致"
		if !state.EntriesEnabled {
			state.Message = "新开仓已停止，现有持仓继续风控"
		}
		if !state.StartedAt.IsZero() && (len(state.Equity) == 0 || now.Sub(state.Equity[len(state.Equity)-1].At) >= time.Minute) {
			state.Equity = append(state.Equity, PlanExperimentPoint{At: now, RangeEquity: state.Arms[0].Report.TotalEquity, ConfirmedEquity: state.Arms[1].Report.TotalEquity})
		}
	} else if !complete {
		state.Status, state.Message = "data_wait", "部分持仓缺少有效报价，暂停收益差比较"
	}
	return state, ValidatePlanExperimentTransition(current, state)
}

func experimentSignal(plan domain.TradePlan, quote PositionQuote, at time.Time) realtime.Signal {
	return realtime.Signal{ID: plan.ID, Symbol: plan.Symbol, Industry: "未分类", AsOf: at, Price: quote.Price, TriggerPrice: plan.Structure.Plan.EntryLow, InvalidationPrice: plan.Structure.Plan.Invalidation, Reasons: []string{plan.Structure.Name, plan.Structure.Plan.Confirmation}, QuoteTime: quote.QuoteTime}
}

func experimentEntryEligible(state PlanExperiment, arm PlanExperimentArm, selection PlanExperimentSelection, input PlanExperimentInput, quoteAt time.Time) (bool, string) {
	if !state.EntriesEnabled || !selection.Enabled {
		return false, "新开仓未启用或计划已移出实验"
	}
	if !input.Qualified {
		return false, "报价或交易日无效，等待有效数据"
	}
	if quoteAt.Before(state.EntriesEnabledAt) || quoteAt.Before(selection.EnabledAt) {
		return false, "等待实验启用后的新报价"
	}
	monitor, found := input.Monitors[selection.Plan.ID]
	if !found || monitor.Fingerprint != selection.Plan.Analysis.Fingerprint || !monitor.Enabled || monitor.DataStatus != "healthy" || monitor.ConfirmedOn == "" || monitor.ConfirmedOn >= localTradingDate(quoteAt) || quoteAt.Before(monitor.LastQuoteAt) || quoteAt.Sub(monitor.LastQuoteAt) > 2*time.Minute || (monitor.Phase != "confirmed" && monitor.Phase != "in_zone") {
		return false, "等待有效的监控确认与新报价"
	}
	levels := selection.Plan.Structure.Plan
	if selection.Plan.ExpiresOn < localTradingDate(quoteAt) || input.Quote.Price <= levels.Invalidation || input.Quote.Price >= levels.Target2 {
		return false, "计划过期或报价已越过风险/目标边界"
	}
	if arm.ID == "range" && (input.Quote.Price < levels.EntryLow || input.Quote.Price > levels.EntryHigh) {
		return false, "等待进入冻结的入场区间"
	}
	return true, "日线已确认，等待下一条报价模拟开仓"
}

func advanceExperimentEntry(state *PlanExperiment, arm *PlanExperimentArm, trial *PlanExperimentTrial, selection PlanExperimentSelection, input PlanExperimentInput, now time.Time, mode string) {
	if mode != "trading" {
		return
	}
	quoteAt, valid := parseRealtimeTimestamp(input.Quote.QuoteTime)
	eligible, reason := experimentEntryEligible(*state, *arm, selection, input, quoteAt)
	if !valid || !eligible {
		trial.PendingEntry = nil
		if trial.EntryOrderID == "" {
			trial.Status = "waiting"
			trial.Reason = "等待实验与监控条件"
		}
		trial.Status, trial.Reason = "waiting", reason
		return
	}
	if !quoteAt.After(trial.LastQuoteAt) {
		return
	}
	trial.LastQuoteAt = quoteAt
	if quoteAt.Before(trial.RetryAfter) {
		return
	}
	if trial.PendingEntry != nil && (quoteAt.Sub(trial.PendingEntry.At) > 2*time.Minute || localTradingDate(quoteAt) != localTradingDate(trial.PendingEntry.At)) {
		trial.PendingEntry = nil
	}
	if trial.PendingEntry == nil {
		monitor := input.Monitors[selection.Plan.ID]
		trial.PendingEntry = &PlanExperimentIntent{At: now, QuoteAt: quoteAt, Price: input.Quote.Price, Sequence: monitor.Sequence, Reason: reason}
		trial.Status, trial.Reason = "pending_entry", reason
		return
	}
	if !quoteAt.After(trial.PendingEntry.At) {
		return
	}
	intent := *trial.PendingEntry
	trial.PendingEntry = nil
	trial.EntryTriggerAt = intent.At
	eventID := realtimeEventID("plan-entry", selection.Plan.Symbol, intent.At.Format("2006-01-02 15:04:05"), arm.ID+"-"+selection.Plan.ID)
	if err := executeExperimentBuy(&arm.Report, selection.Plan, input, arm.ID, eventID, intent, quoteAt); err != nil {
		trial.Status, trial.Reason = "rejected", err.Error()
		trial.RetryAfter = quoteAt.Add(5 * time.Minute)
		signal := experimentSignal(selection.Plan, input.Quote, intent.At)
		appendRealtimeRejection(&arm.Report, signal, "buy", localTradingDate(quoteAt), eventID, input.Quote, err.Error())
		if len(arm.Report.Rejections) > 0 {
			arm.Report.Rejections[len(arm.Report.Rejections)-1].PlanID = selection.Plan.ID
		}
		arm.Report.AsOf, arm.Report.LastRealtimeAt, arm.Report.GeneratedAt = localTradingDate(quoteAt), input.Quote.QuoteTime, now
		return
	}
	trial.EntryOrderID = eventID + "-buy"
	trial.Status, trial.Reason = "open", "已模拟开仓；本计划不重复加仓"
}

func executeExperimentBuy(report *Report, plan domain.TradePlan, input PlanExperimentInput, armID, eventID string, intent PlanExperimentIntent, quoteAt time.Time) error {
	quote, cfg := input.Quote, report.Config
	if realtimePositionIndex(report.Positions, plan.Symbol) >= 0 {
		return fmt.Errorf("同股已有其他计划持仓，等待其退出")
	}
	if !tradableExperimentQuote(quote) {
		return fmt.Errorf("缺少有效成交量或涨跌停边界，暂停模拟撮合")
	}
	if realtimeBuyBlocked(quote) {
		return fmt.Errorf("报价触及涨停，模拟买入被拒绝")
	}
	price := quote.Price * (1 + cfg.SlippageBPS/10000)
	if price > quote.LimitUp || price >= plan.Structure.Plan.Target2 || (armID == "range" && price > plan.Structure.Plan.EntryHigh) {
		return fmt.Errorf("滑点后价格超过涨停、目标位或计划区间上沿")
	}
	bars := normalizedBars(input.Bars)
	end := -1
	for index := len(bars) - 1; index >= 0; index-- {
		if bars[index].Date < localTradingDate(quoteAt) {
			end = index
			break
		}
	}
	monitor, found := input.Monitors[plan.ID]
	if end < 19 || !found || bars[end].Date != monitor.HistoryDate || math.Abs(bars[end].Close-quote.PreviousClose) > .011 {
		return fmt.Errorf("成交额样本未对齐监控使用的完整交易日")
	}
	for _, bar := range bars[end-19 : end+1] {
		if !finite(bar.Amount) || bar.Amount <= 0 {
			return fmt.Errorf("前20个完整日K缺少真实成交额，暂停模拟建仓")
		}
	}
	capacity := trailingAverageAmount(bars, end, 20) * cfg.MaxParticipationPercent / 100
	if capacity <= 0 || !finite(capacity) {
		return fmt.Errorf("缺少完整日K成交额，无法校验模拟成交容量")
	}
	signal := experimentSignal(plan, quote, intent.At)
	signal.Price = intent.Price
	budget, _, reason := entryBudgetForTarget(cfg, signal, quote.Price, report.RemainingCash, realtimeDailyDeployment(*report, localTradingDate(quoteAt)), realtimeActivePositions(report.Positions), shadowPosition{}, false, cfg.MaxPositionPercent)
	if budget <= 0 {
		return fmt.Errorf("%s", reason)
	}
	lotCfg := cfg
	if strings.HasPrefix(plan.Symbol, "sh688") || strings.HasPrefix(plan.Symbol, "sh689") {
		lotCfg.LotSize = 200
	}
	quantity := quantityWithinBudget(quote.Price, budget, capacity, lotCfg)
	if quantity < lotCfg.LotSize {
		return fmt.Errorf("资金、风险预算或容量不足最低买入数量")
	}
	buy := makeOrder(signal, "buy", localTradingDate(quoteAt), quote.Price, quantity, cfg, capacity)
	buy.PlanID = plan.ID
	buy.ID, buy.EventID, buy.EventSource = eventID+"-buy", eventID, "plan-experiment-"+armID
	buy.Status, buy.ExecutionTime, buy.PositionAction, buy.PositionSequence = OrderFilled, quote.QuoteTime, "open", 1
	buy.Reason = fmt.Sprintf("计划 %s；观察于 %s（报价 %s），下一报价模拟成交", plan.ID[:8], intent.At.In(shanghaiLocation).Format("15:04:05"), intent.QuoteAt.In(shanghaiLocation).Format("15:04:05"))
	fee := transactionFee(buy.Amount, "buy", cfg)
	if buy.Amount+fee > budget || report.RemainingCash-buy.Amount-fee < cfg.InitialCash*cfg.CashReservePercent/100 {
		return fmt.Errorf("含费用后现金缓冲不足")
	}
	lot := ShadowPositionLot{OrderID: buy.ID, EntryDate: buy.AttemptDate, EntryTime: buy.ExecutionTime, Quantity: quantity, EntryPrice: buy.Price, EntryAmount: buy.Amount, EntryFee: fee, SignalID: plan.ID, Industry: signal.Industry, SignalDate: signalDate(signal), SignalClose: intent.Price, TriggerPrice: plan.Structure.Plan.EntryLow, InvalidationPrice: plan.Structure.Plan.Invalidation, SignalReasons: signal.Reasons, TargetExitDate: plan.ExpiresOn}
	position := ShadowOpenPosition{SignalID: plan.ID, Symbol: plan.Symbol, Industry: signal.Industry, SignalDate: lot.SignalDate, SignalClose: intent.Price, TriggerPrice: lot.TriggerPrice, InvalidationPrice: lot.InvalidationPrice, SignalReasons: signal.Reasons, Lots: []ShadowPositionLot{lot}, TargetPositionPercent: cfg.MaxPositionPercent}
	position = applyRealtimePositionMetrics(position, quote, buy.AttemptDate)
	report.Positions = append(report.Positions, position)
	report.RemainingCash -= buy.Amount + fee
	report.Orders = append(report.Orders, buy)
	report.FilledEntries++
	report.TotalTurnover += buy.Amount
	report.TotalFees += fee
	report.AsOf, report.LastRealtimeAt, report.GeneratedAt = buy.AttemptDate, quote.QuoteTime, quoteAt
	return nil
}

func advanceExperimentExit(arm *PlanExperimentArm, trial *PlanExperimentTrial, selection PlanExperimentSelection, input PlanExperimentInput, now time.Time, mode string) {
	quoteAt, valid := parseRealtimeTimestamp(input.Quote.QuoteTime)
	if !valid || !quoteAt.After(trial.LastQuoteAt) {
		return
	}
	trial.LastQuoteAt = quoteAt
	index := realtimePositionIndex(arm.Report.Positions, selection.Plan.Symbol)
	if index < 0 {
		return
	}
	position := arm.Report.Positions[index]
	position = applyRealtimePositionMetrics(position, input.Quote, localTradingDate(quoteAt))
	arm.Report.Positions[index] = position
	reason := realtimeRiskExitReason(position, input.Quote.Price, arm.Report.Config)
	if monitor, found := input.Monitors[selection.Plan.ID]; found && monitor.Phase == "invalidated" {
		reason = "计划监控已判定结构失效"
	}
	if reason == "" && selection.Plan.ExpiresOn < localTradingDate(quoteAt) {
		reason = "计划到期，退出实验持仓"
	}
	if reason == "" && input.Quote.Price >= selection.Plan.Structure.Plan.Target2 {
		reason = "触及冻结的2R目标测算位"
	}
	if trial.PendingExit == nil && reason != "" {
		trial.PendingExit = &PlanExperimentIntent{At: now, QuoteAt: quoteAt, Price: input.Quote.Price, Reason: reason}
		trial.Status, trial.Reason = "pending_exit", reason+"；等待下一可卖报价"
		return
	}
	if trial.PendingExit == nil || mode != "trading" || !quoteAt.After(trial.PendingExit.At) {
		return
	}
	indexes := realtimeSellableLotIndexes(position.Lots, localTradingDate(quoteAt), false)
	if len(indexes) == 0 {
		trial.Status, trial.Reason = "pending_exit", "T+1锁定，等待下一交易日可卖数量"
		return
	}
	if !tradableExperimentQuote(input.Quote) || realtimeSellBlocked(input.Quote) || input.Quote.Price*(1-arm.Report.Config.SlippageBPS/10000) < input.Quote.LimitDown {
		trial.Reason = "边界/成交量不足或跌停不可成交，保留待退出持仓"
		return
	}
	eventID := realtimeEventID("plan-exit", selection.Plan.Symbol, trial.PendingExit.At.Format("2006-01-02 15:04:05"), arm.ID+"-"+selection.Plan.ID)
	signal := experimentSignal(selection.Plan, input.Quote, trial.PendingExit.At)
	before := len(arm.Report.Orders)
	filled := executeRealtimeSell(&arm.Report, &position, indexes, signal, input.Quote, eventID, "risk_exit", trial.PendingExit.Reason, input.CalendarDates)
	if filled <= 0 {
		return
	}
	for i := before; i < len(arm.Report.Orders); i++ {
		arm.Report.Orders[i].EventSource = "plan-experiment-" + arm.ID
		arm.Report.Orders[i].PlanID = selection.Plan.ID
	}
	for i := len(arm.Report.Trades) - len(indexes); i < len(arm.Report.Trades); i++ {
		if i >= 0 {
			arm.Report.Trades[i].PlanID = selection.Plan.ID
		}
	}
	if position.Quantity == 0 {
		arm.Report.Positions = append(arm.Report.Positions[:index], arm.Report.Positions[index+1:]...)
	} else {
		arm.Report.Positions[index] = position
	}
	trial.ExitOrderID = arm.Report.Orders[len(arm.Report.Orders)-1].ID
	trial.Status, trial.Reason = "closed", trial.PendingExit.Reason
	trial.PendingExit = nil
	arm.Report.AsOf, arm.Report.LastRealtimeAt, arm.Report.GeneratedAt = localTradingDate(quoteAt), input.Quote.QuoteTime, now
}

func ValidatePlanExperimentTransition(previous, next PlanExperiment) error {
	if next.Version != PlanExperimentVersion || len(next.Arms) != 2 || next.Selections == nil {
		return fmt.Errorf("实验账本格式无效")
	}
	for index, arm := range next.Arms {
		if arm.ID != []string{"range", "confirmed"}[index] || arm.Report.ConfigFingerprint != ConfigFingerprint(PlanExperimentConfig()) || !sameExperimentJSON(arm.Report.Config, PlanExperimentConfig()) || arm.Report.InitialCash != PlanExperimentConfig().InitialCash || arm.Trials == nil {
			return fmt.Errorf("实验账户配置无效")
		}
		cash, fees, turnover := arm.Report.InitialCash, 0.0, 0.0
		for _, order := range arm.Report.Orders {
			if order.Status != OrderFilled {
				continue
			}
			fee := transactionFee(order.Amount, order.Side, arm.Report.Config)
			if order.Side == "buy" {
				cash -= order.Amount + fee
			} else if order.Side == "sell" {
				cash += order.Amount - fee
			} else {
				return fmt.Errorf("实验成交方向无效")
			}
			fees += fee
			turnover += order.Amount
		}
		if !finite(cash) || cash < -.001 || math.Abs(cash-arm.Report.RemainingCash) > .001 || math.Abs(fees-arm.Report.TotalFees) > .001 || math.Abs(turnover-arm.Report.TotalTurnover) > .001 {
			return fmt.Errorf("实验现金或费用不能与成交记录对账")
		}
		for _, position := range arm.Report.Positions {
			selection, found := next.Selections[position.SignalID]
			trial, tracked := arm.Trials[position.SignalID]
			if !found || !tracked || selection.Plan.Symbol != position.Symbol || trial.EntryOrderID == "" {
				return fmt.Errorf("实验持仓缺少计划归因")
			}
		}
	}
	if previous.Version == "" {
		return nil
	}
	if !next.CreatedAt.Equal(previous.CreatedAt) || next.UpdatedAt.Before(previous.UpdatedAt) || (!previous.StartedAt.IsZero() && !next.StartedAt.Equal(previous.StartedAt)) || !experimentPrefixUnchanged(previous.Changes, next.Changes) {
		return fmt.Errorf("实验起点或控制历史不能改写")
	}
	for id, selection := range previous.Selections {
		updated, ok := next.Selections[id]
		if !ok || !sameExperimentJSON(selection.Plan, updated.Plan) {
			return fmt.Errorf("已纳入的计划快照不能删除或改写")
		}
	}
	if !experimentPrefixUnchanged(previous.Equity, next.Equity) {
		return fmt.Errorf("实验净值历史不能改写")
	}
	for index, arm := range previous.Arms {
		nextReport := next.Arms[index].Report
		if !experimentPrefixUnchanged(arm.Report.Orders, nextReport.Orders) || !experimentPrefixUnchanged(arm.Report.Trades, nextReport.Trades) || !experimentPrefixUnchanged(arm.Report.Rejections, nextReport.Rejections) {
			return fmt.Errorf("实验成交与拒单历史不能改写")
		}
		if err := ValidateTransition(arm.Report, next.Arms[index].Report); err != nil {
			return err
		}
		for id, trial := range arm.Trials {
			updated, ok := next.Arms[index].Trials[id]
			if !ok || updated.LastQuoteAt.Before(trial.LastQuoteAt) || (trial.EntryOrderID != "" && trial.EntryOrderID != updated.EntryOrderID) || (trial.ExitOrderID != "" && trial.ExitOrderID != updated.ExitOrderID) {
				return fmt.Errorf("实验执行水位或订单归因不能回退")
			}
		}
	}
	return nil
}

func validExperimentQuote(quote PositionQuote, now time.Time, mode string) bool {
	at, ok := parseRealtimeTimestamp(quote.QuoteTime)
	if !ok || quote.Symbol == "" || !finite(quote.Price) || quote.Price <= 0 || at.After(now) || localTradingDate(at) != localTradingDate(now) {
		return false
	}
	if mode == "closing" {
		return at.In(shanghaiLocation).Hour() >= 15
	}
	minute := now.In(shanghaiLocation).Hour()*60 + now.In(shanghaiLocation).Minute()
	return mode == "trading" && ((minute >= 570 && minute < 690) || (minute >= 780 && minute < 900)) && realtimeExecutionWindow(at) && now.Sub(at) <= 2*time.Minute
}

func tradableExperimentQuote(quote PositionQuote) bool {
	return finite(quote.LimitUp) && finite(quote.LimitDown) && quote.LimitDown > 0 && quote.LimitUp > quote.LimitDown && quote.Price >= quote.LimitDown && quote.Price <= quote.LimitUp && finite(quote.Volume) && quote.Volume > 0 && finite(quote.Amount) && quote.Amount > 0
}
