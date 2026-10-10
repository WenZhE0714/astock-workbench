package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestHeadShouldersPlanPersistsFrozenBoundaries(t *testing.T) {
	root := t.TempDir()
	s := chartPatternServerFixture(root)
	for _, sample := range []struct {
		symbol, kind string
		status       int
	}{
		{"sh600004", "head-shoulders-bottom", http.StatusCreated},
		{"sh600005", "head-shoulders-top", http.StatusBadRequest},
	} {
		t.Run(sample.kind, func(t *testing.T) {
			analysis, err := s.chartAnalysis(context.Background(), sample.symbol, "")
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(map[string]string{"symbol": sample.symbol, "through": analysis.DataDate, "fingerprint": analysis.Fingerprint, "structure_id": sample.kind, "expires_on": "2026-09-25"})
			if err != nil {
				t.Fatal(err)
			}
			recorder := postTradePlan(s, body)
			if recorder.Code != sample.status {
				t.Fatalf("save returned %d: %s", recorder.Code, recorder.Body.String())
			}
			if sample.status != http.StatusCreated {
				return
			}
			var response struct {
				Plan domain.TradePlan `json:"plan"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			plan := response.Plan
			if plan.Structure.ID != sample.kind || len(plan.Structure.Lines) != 7 || plan.MonitorRule == nil {
				t.Fatalf("head-shoulders geometry missing: %+v", plan.Structure)
			}
			if enabled := setMonitor(t, s, plan, true); enabled.Code != http.StatusOK {
				t.Fatalf("monitor enable failed: %s", enabled.Body.String())
			}
			restarted := chartPatternServerFixture(root)
			loaded, err := restarted.tradePlans.Load(plan.Symbol, plan.ID)
			if err != nil || !reflect.DeepEqual(loaded, plan) {
				t.Fatalf("restart changed the frozen plan: %v", err)
			}
			monitor, err := restarted.planMonitors.Load(plan.ID)
			if err != nil || !monitor.Enabled || monitor.Rule.PatternReadyOn != plan.Structure.Pattern.ReadyOn || monitor.Rule.BreakoutPrice != plan.Structure.Pattern.TriggerPrice || monitor.Rule.Levels.Invalidation != plan.Structure.Pattern.InvalidationPrice {
				t.Fatalf("restart changed monitor boundaries: %v %+v", err, monitor)
			}
			if duplicate := postTradePlan(restarted, body); duplicate.Code != http.StatusOK {
				t.Fatal("repeated save created another plan")
			}
			request := httptest.NewRequest(http.MethodGet, "/api/stock?symbol="+sample.symbol+"&limit=21", nil)
			recorder = httptest.NewRecorder()
			s.Handler().ServeHTTP(recorder, request)
			var stock stockResponse
			if recorder.Code != 200 || json.Unmarshal(recorder.Body.Bytes(), &stock) != nil || stock.ChartAnalysis == nil || !reflect.DeepEqual(stock.ChartAnalysis.Structures, analysis.Structures) {
				t.Fatal("stock response changed head-shoulders geometry")
			}
		})
	}
}
