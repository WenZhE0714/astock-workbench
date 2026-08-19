package domain

import "time"

// AIChatTurn is one persisted question and answer for a stock consultation.
// It is advisory text only and never represents an executable trade order.
type AIChatTurn struct {
	AskedAt   time.Time          `json:"asked_at"`
	FactsAt   time.Time          `json:"facts_at,omitempty"`
	FactsHash string             `json:"facts_hash,omitempty"`
	Question  string             `json:"question"`
	Answer    string             `json:"answer"`
	Agents    []AgentResearchRun `json:"agents,omitempty"`
}
