package domain

import "time"

// AgentResearchRun records one read-only specialist's interpretation of a
// frozen fact snapshot. It is advisory evidence, never an executable signal.
type AgentResearchRun struct {
	Role       string        `json:"role"`
	Label      string        `json:"label"`
	Status     string        `json:"status"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt time.Time     `json:"finished_at"`
	Duration   time.Duration `json:"duration_ns"`
	FactsHash  string        `json:"facts_hash,omitempty"`
	Analysis   string        `json:"analysis,omitempty"`
	Error      string        `json:"error,omitempty"`
}
