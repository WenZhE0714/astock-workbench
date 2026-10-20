package backtest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type unavailableHistory struct{ cause error }

func (provider unavailableHistory) FetchDailyBarsRange(context.Context, string, time.Time, time.Time, PriceAdjustment) ([]domain.DailyBar, error) {
	return nil, provider.cause
}

func TestDailyEngineIdentifiesHistoryFailuresWithoutDroppingStocks(t *testing.T) {
	request := testRequest()
	request.Tickers = []string{"sh688825"}
	request.Technical.SlowMA = 60
	for _, provider := range []DailyBarProvider{unavailableHistory{context.DeadlineExceeded}, historyRangeMock{bars: testBars()[:55]}} {
		result, err := NewDailyEngine(provider).Run(context.Background(), request)
		var failure *SymbolHistoryError
		if !errors.As(err, &failure) || failure.Symbol != "sh688825" {
			t.Fatalf("missing stock identity: %v", err)
		}
		if len(result.Trades) != 0 || result.RunID != "" || len(request.Tickers) != 1 {
			t.Fatal("failed input created a result or changed the universe")
		}
		if _, ok := provider.(unavailableHistory); ok {
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("original timeout lost")
			}
		} else if !strings.Contains(err.Error(), "55 根") || !strings.Contains(err.Error(), "62 根") {
			t.Fatalf("missing minimum history: %v", err)
		}
	}
}
