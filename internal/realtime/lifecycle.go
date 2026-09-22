package realtime

import (
	"sort"
	"strings"
	"time"
)

// SignalLifecycleEvent is one meaningful point-in-time change in a signal
// episode. Repeated 30-second snapshots with the same state are collapsed.
type SignalLifecycleEvent struct {
	At       time.Time   `json:"at"`
	Kind     string      `json:"kind"`
	State    SignalState `json:"state"`
	Score    float64     `json:"score"`
	Price    float64     `json:"price"`
	Detail   string      `json:"detail,omitempty"`
	Reasons  []string    `json:"reasons,omitempty"`
	Warnings []string    `json:"warnings,omitempty"`
}

// SignalLifecycle joins intraday observations with forward outcomes. It is a
// research timeline, not an order instruction.
type SignalLifecycle struct {
	Key                 string                 `json:"key"`
	Symbol              string                 `json:"symbol"`
	Name                string                 `json:"name,omitempty"`
	Industry            string                 `json:"industry,omitempty"`
	SignalDate          string                 `json:"signal_date"`
	FirstSeen           time.Time              `json:"first_seen"`
	LastSeen            time.Time              `json:"last_seen"`
	LatestState         SignalState            `json:"latest_state"`
	PeakScore           float64                `json:"peak_score"`
	LatestScore         float64                `json:"latest_score"`
	LatestPrice         float64                `json:"latest_price"`
	TriggerPrice        float64                `json:"trigger_price,omitempty"`
	InvalidationPrice   float64                `json:"invalidation_price,omitempty"`
	EntryShapeLabel     string                 `json:"entry_shape_label,omitempty"`
	MonsterStage        MonsterStage           `json:"monster_stage,omitempty"`
	CandidateSources    []string               `json:"candidate_sources,omitempty"`
	Status              string                 `json:"status"`
	MatureHorizon       int                    `json:"mature_horizon,omitempty"`
	MatureReturnPercent float64                `json:"mature_return_percent,omitempty"`
	Events              []SignalLifecycleEvent `json:"events"`
	Outcomes            []SignalOutcome        `json:"outcomes,omitempty"`
}

func signalLifecycleDate(signal Signal) string {
	if !signal.AsOf.IsZero() {
		return signal.AsOf.In(time.Local).Format("2006-01-02")
	}
	return strings.TrimSpace(signal.DataDate)
}

func lifecycleEvent(signal Signal, kind, detail string) SignalLifecycleEvent {
	return SignalLifecycleEvent{
		At: signal.AsOf, Kind: kind, State: signal.State, Score: signal.Score,
		Price: signal.Price, Detail: detail,
		Reasons: append([]string(nil), signal.Reasons...), Warnings: append([]string(nil), signal.Warnings...),
	}
}

// BuildSignalLifecycles groups observations by symbol and trading date, then
// attaches the latest forward result for each horizon. It deliberately keeps
// the raw signal reasons on lifecycle events so a user can audit why a state
// changed.
func BuildSignalLifecycles(signals []Signal, outcomes []SignalOutcome, limit int) []SignalLifecycle {
	type bucket struct{ items []Signal }
	groups := make(map[string]*bucket)
	for _, signal := range signals {
		if strings.TrimSpace(signal.Symbol) == "" {
			continue
		}
		date := signalLifecycleDate(signal)
		if date == "" {
			continue
		}
		key := signal.Symbol + "|" + date
		if groups[key] == nil {
			groups[key] = &bucket{}
		}
		groups[key].items = append(groups[key].items, signal)
	}
	result := make([]SignalLifecycle, 0, len(groups))
	for key, group := range groups {
		sort.SliceStable(group.items, func(i, j int) bool { return group.items[i].AsOf.Before(group.items[j].AsOf) })
		if len(group.items) == 0 {
			continue
		}
		first, latest := group.items[0], group.items[len(group.items)-1]
		lifecycle := SignalLifecycle{
			Key: key, Symbol: latest.Symbol, Name: latest.Name, Industry: latest.Industry,
			SignalDate: signalLifecycleDate(latest), FirstSeen: first.AsOf, LastSeen: latest.AsOf,
			LatestState: latest.State, PeakScore: latest.Score, LatestScore: latest.Score, LatestPrice: latest.Price,
			TriggerPrice: latest.TriggerPrice, InvalidationPrice: latest.InvalidationPrice,
			EntryShapeLabel: latest.EntryShapeLabel, MonsterStage: latest.Monster.Stage,
			CandidateSources: append([]string(nil), latest.CandidateSources...), Status: string(latest.State),
			Events: make([]SignalLifecycleEvent, 0),
		}
		previous := Signal{}
		for _, signal := range group.items {
			if signal.Score > lifecycle.PeakScore {
				lifecycle.PeakScore = signal.Score
			}
			kind, detail := "observation", ""
			if previous.ID == "" {
				kind, detail = "first-seen", "首次进入信号观察"
			} else if signal.State != previous.State {
				kind, detail = "state-change", string(previous.State)+" → "+string(signal.State)
			} else if abs(signal.Score-previous.Score) >= 4 {
				kind, detail = "score-change", "综合分发生明显变化"
			} else if len(signal.Warnings) > 0 && len(previous.Warnings) == 0 {
				kind, detail = "data-warning", "数据覆盖发生变化"
			}
			if kind != "observation" {
				lifecycle.Events = append(lifecycle.Events, lifecycleEvent(signal, kind, detail))
			}
			previous = signal
		}
		if len(lifecycle.Events) == 0 {
			lifecycle.Events = append(lifecycle.Events, lifecycleEvent(latest, "observation", "持续观察"))
		}
		for _, outcome := range outcomes {
			if outcome.Symbol == latest.Symbol && outcome.SignalDate == lifecycle.SignalDate {
				lifecycle.Outcomes = append(lifecycle.Outcomes, outcome)
				if outcome.Status == OutcomeReady && (lifecycle.MatureHorizon == 0 || outcome.Horizon < lifecycle.MatureHorizon) {
					lifecycle.MatureHorizon, lifecycle.MatureReturnPercent = outcome.Horizon, outcome.ReturnPercent
				}
			}
		}
		sort.SliceStable(lifecycle.Outcomes, func(i, j int) bool { return lifecycle.Outcomes[i].Horizon < lifecycle.Outcomes[j].Horizon })
		if lifecycle.MatureHorizon > 0 {
			lifecycle.Status = "matured"
		} else if latest.State == StateInvalid {
			lifecycle.Status = "data-limited"
		} else {
			lifecycle.Status = "observing"
		}
		result = append(result, lifecycle)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].LastSeen.After(result[j].LastSeen) })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
