package domain

import "time"

// AIChatTurn is one in-memory question and answer in the live watch session.
// It is advisory text only and never represents an executable trade order.
type AIChatTurn struct {
	AskedAt  time.Time `json:"asked_at"`
	Question string    `json:"question"`
	Answer   string    `json:"answer"`
}
