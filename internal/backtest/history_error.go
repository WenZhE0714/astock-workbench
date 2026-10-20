package backtest

import "fmt"

// SymbolHistoryError identifies a failed input without changing the requested universe.
type SymbolHistoryError struct {
	Symbol string
	Err    error
}

func (err *SymbolHistoryError) Error() string { return fmt.Sprintf("%s: %v", err.Symbol, err.Err) }
func (err *SymbolHistoryError) Unwrap() error { return err.Err }
