// Package backtest owns historical simulation contracts. It is intentionally
// isolated from execution so a research run can never submit a live order.
package backtest

import (
	"context"
	"time"
)

type PriceAdjustment string

const (
	AdjustmentNone     PriceAdjustment = "none"
	AdjustmentForward  PriceAdjustment = "forward"
	AdjustmentBackward PriceAdjustment = "backward"
)

type Request struct {
	Strategy        string
	Tickers         []string
	Start           time.Time
	End             time.Time
	InitialCash     float64
	CommissionRate  float64
	SlippageBPS     float64
	Adjustment      PriceAdjustment
	Benchmark       string
	NoFutureData    bool
	PointInTimePool bool
}

type Metrics struct {
	TotalReturn      float64
	AnnualizedReturn float64
	MaxDrawdown      float64
	Sharpe           float64
	Trades           int
}

type Result struct {
	Request  Request
	Metrics  Metrics
	Warnings []string
}

type Engine interface {
	Run(context.Context, Request) (Result, error)
}
