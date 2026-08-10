// Package strategy defines provider-neutral strategy contracts. Implementations
// may combine market data, deterministic indicators and TradingAgents research,
// but must not place orders directly.
package strategy

import (
	"context"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type Horizon string

const (
	HorizonIntraday Horizon = "intraday"
	HorizonSwing    Horizon = "swing"
	HorizonValue    Horizon = "value"
)

type Input struct {
	Ticker   string
	AsOf     time.Time
	Horizon  Horizon
	Quote    *domain.Quote
	Research *domain.AnalysisResult
}

// Signal is deliberately richer than Buy/Sell. Triggers and invalidations are
// required before a future paper/live execution layer may consume it.
type Signal struct {
	Strategy      string
	Ticker        string
	AsOf          time.Time
	Horizon       Horizon
	Rating        string
	Confidence    float64
	Thesis        string
	Triggers      []string
	Invalidations []string
	MaxWeight     float64
}

type Strategy interface {
	Name() string
	Evaluate(context.Context, Input) (Signal, error)
}
