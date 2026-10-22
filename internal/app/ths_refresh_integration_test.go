package app

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/storage"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

func TestTHSRefreshAndDailyHistoryLive(t *testing.T) {
	if os.Getenv("ASTOCK_THS_AUTH_INTEGRATION") != "1" {
		t.Skip("live credential renewal is opt-in")
	}
	app, err := New(io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err := app.configureMarketSource("ths"); err != nil {
		t.Fatal(err)
	}
	before := app.thsMarket.AccessToken()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bars, err := app.thsMarket.FetchDailyBars(ctx, "sz300033")
	if err != nil {
		t.Logf("primary unavailable: %v", err)
		bars, err = app.history.FetchDailyBars(ctx, "sz300033")
		if err != nil {
			t.Fatal(err)
		}
	}
	signal, err := strategy.AnalyzeTechnical("sz300033", bars)
	if err != nil {
		t.Fatal(err)
	}
	_, saved, _, err := storage.LoadTHSQuantAPIConfig(app.paths.THSQuantAPIConfigFile, app.paths.THSQuantAPITokenFile, app.paths.THSQuantAPIRefreshTokenFile)
	if err != nil || saved != app.thsMarket.AccessToken() {
		t.Fatal("renewed credential was not persisted")
	}
	t.Logf("credential_changed=%t bars=%d latest=%s source=%s close=%.2f", before != app.thsMarket.AccessToken(), len(bars), signal.DataDate, signal.DataSource, signal.Price)
}
