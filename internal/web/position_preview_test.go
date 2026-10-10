package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPositionPreviewUsesServerPricesAndRejectsChangedSnapshot(t *testing.T) {
	server := chartServerFixture(t.TempDir())
	analysis := chartRequest(t, server)
	input := map[string]any{"symbol": analysis.Symbol, "through": analysis.DataDate, "fingerprint": analysis.Fingerprint, "structure_id": "range-breakout",
		"budget": map[string]any{"equity": 100000, "cash": 100000, "risk_percent": 1, "max_position_percent": 20}}
	post := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(input)
		r := httptest.NewRecorder()
		server.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/position-preview", bytes.NewReader(body)))
		return r
	}
	if response := post(); response.Code != 200 {
		t.Fatalf("preview failed: %s", response.Body.String())
	}
	input["entry_price"] = 1
	if response := post(); response.Code != 400 {
		t.Fatal("client price was accepted")
	}
	delete(input, "entry_price")
	input["fingerprint"] = strings.Repeat("0", 64)
	if response := post(); response.Code != 409 {
		t.Fatal("changed snapshot was accepted")
	}
}
