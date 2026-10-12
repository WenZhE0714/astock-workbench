package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

func TestPatternBenchmarkAPIArchivesAndPreservesLegacyResults(t *testing.T) {
	s := patternValidationServerFixture(t.TempDir())
	r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-validation", `{"symbols":["600004"],"start":"2026-06-01","end":"2026-09-25"}`)
	var view strategy.PatternValidationView
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &view) != nil || view.Report.Benchmark == nil || view.Totals.BenchmarkPaired != 1 || view.Totals.AverageBenchmarkReturn == nil {
		t.Fatalf("benchmark response missing: %d %s", r.Code, r.Body.String())
	}
	archive, err := s.patternValidations.Load(view.Report.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if archive.Inputs[len(archive.Inputs)-1].Symbol != "sh000300" || archive.Samples[0].Outcomes[0].Benchmark == nil {
		t.Fatal("archive lost benchmark evidence")
	}
	s.history, s.patternHistory, s.patternActions, s.patternFactors = nil, nil, nil, nil
	r = validationHTTPRequest(s, http.MethodGet, "/api/pattern-validation?id="+archive.RunID+"&horizon=20", "")
	var reloaded strategy.PatternValidationView
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &reloaded) != nil || reloaded.Totals.BenchmarkPaired != 1 || reloaded.Samples[0].Outcomes[2].Benchmark.End != reloaded.Samples[0].Outcomes[2].Through {
		t.Fatal("offline archive read lost paired results")
	}
	originalReturn := *reloaded.Totals.AveragePriceReturn
	archive.Version = "pattern-validation-v5"
	archive.Benchmark = nil
	for i := range archive.Samples {
		for j := range archive.Samples[i].Outcomes {
			archive.Samples[i].Outcomes[j].Benchmark = nil
		}
	}
	legacy, err := s.patternValidations.Save(archive)
	if err != nil {
		t.Fatal(err)
	}
	r = validationHTTPRequest(s, http.MethodGet, "/api/pattern-validation?id="+legacy.RunID+"&horizon=20", "")
	var old strategy.PatternValidationView
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &old) != nil || old.Report.Benchmark != nil || old.Totals.BenchmarkPaired != 0 || old.Totals.BenchmarkUnpaired != 1 || old.Totals.AverageBenchmarkReturn != nil || *old.Totals.AveragePriceReturn != originalReturn {
		t.Fatal("legacy results were changed or backfilled")
	}
	r = validationHTTPRequest(s, http.MethodGet, "/api/pattern-validation?id="+legacy.RunID+"&download=1", "")
	var exported domain.PatternValidationReport
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &exported) != nil || exported.Benchmark != nil || exported.Samples[0].Outcomes[0].Benchmark != nil {
		t.Fatal("legacy export unexpectedly gained comparisons")
	}
}
