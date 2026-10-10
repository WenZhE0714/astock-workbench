package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestTimeframeEndpointBindsToChartAndRemainsReadOnly(t *testing.T) {
	s := chartServerFixture(t.TempDir())
	base := chartRequest(t, s)
	s.quotes = nil
	path := "/api/chart-timeframes?symbol=600519&through=" + base.DataDate + "&fingerprint=" + base.Fingerprint
	r := httptest.NewRecorder()
	s.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
	var result domain.ChartTimeframeComparison
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || result.BaseFingerprint != base.Fingerprint || result.Daily.Samples != base.BarsUsed {
		t.Fatalf("bad bound comparison: %s", r.Body.String())
	}
	plans, err := s.tradePlans.List(base.Symbol, 100)
	if err != nil || len(plans) != 0 {
		t.Fatal("comparison created a plan")
	}
	for _, sample := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/api/chart-timeframes", 400},
		{http.MethodGet, "/api/chart-timeframes?symbol=600519&through=2099-01-01", 400},
		{http.MethodGet, "/api/chart-timeframes?symbol=600519&fingerprint=" + strings.Repeat("0", 64), 409},
		{http.MethodPost, path, 405},
	} {
		r = httptest.NewRecorder()
		s.Handler().ServeHTTP(r, httptest.NewRequest(sample.method, sample.path, nil))
		if r.Code != sample.status {
			t.Fatalf("%s: %d %s", sample.path, r.Code, r.Body.String())
		}
	}
}
