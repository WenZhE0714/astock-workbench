// Package execution defines the future paper/live broker boundary. Research
// signals are advisory inputs; every order must pass a deterministic RiskGate.
package execution

import (
	"context"
	"time"
)

type Side string

const (
	Buy  Side = "buy"
	Sell Side = "sell"
)

type Order struct {
	ClientOrderID string
	Ticker        string
	Side          Side
	Quantity      int
	LimitPrice    float64
	CreatedAt     time.Time
	ResearchID    string
}

type RiskDecision struct {
	Allowed bool
	Reasons []string
}

type RiskGate interface {
	Evaluate(context.Context, Order) (RiskDecision, error)
}

type Broker interface {
	Submit(context.Context, Order) (string, error)
	Cancel(context.Context, string) error
}
