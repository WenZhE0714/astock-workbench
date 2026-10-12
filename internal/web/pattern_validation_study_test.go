package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

func TestPatternStudyAPIArchivesImmutableConfigAndPeriodEvidence(t *testing.T) {
	s := patternValidationServerFixture(t.TempDir())
	history := s.history.(patternHistoryFixture)
	ranges := &validationRangeFixture{patternHistoryFixture: history}
	s.patternHistory = ranges
	boundary := history["sh600004"][85].Date
	body := fmt.Sprintf(`{"symbols":["600004"],"start":"2026-06-01","end":"2026-09-25","validation_start":%q}`, boundary)
	r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-validation", body)
	var result strategy.PatternValidationView
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || result.Report.Study == nil || result.Filter.Period != "validation" || result.Total != 0 || len(result.Periods) != 2 {
		t.Fatalf("study failed: %d %s", r.Code, r.Body.String())
	}
	if len(ranges.starts) != 2 {
		t.Fatal("period replays fetched market data twice")
	}
	id := result.Report.RunID
	archive, err := s.patternValidations.Load(id)
	if err != nil || archive.Request.ValidationStart != boundary || archive.Samples[0].Period != "research" {
		t.Fatal("study archive lost request or membership")
	}
	encoded, _ := json.Marshal(archive)
	s.history, s.patternHistory, s.patternActions, s.patternFactors = nil, nil, nil, nil
	r = validationHTTPRequest(s, http.MethodGet, "/api/pattern-validation?id="+id+"&period=research&horizon=10", "")
	var research strategy.PatternValidationView
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &research) != nil || research.Totals.BoundaryExcluded != 1 || research.Totals.Mature != 0 || research.Report.InputHash != archive.Study.Research.InputHash || research.Report.Cutoff != archive.Study.Config.ResearchEnd {
		t.Fatalf("wrong research replay: %s", r.Body.String())
	}
	reloaded, _ := s.patternValidations.Load(id)
	after, _ := json.Marshal(reloaded)
	if string(encoded) != string(after) {
		t.Fatal("GET rewrote frozen archive")
	}
	r = validationHTTPRequest(s, http.MethodGet, "/api/pattern-validation?id="+id+"&download=1", "")
	var exported domain.PatternValidationReport
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &exported) != nil || exported.Study.ConfigHash != archive.Study.ConfigHash || exported.InputHash != archive.InputHash {
		t.Fatal("export lost frozen config or full inputs")
	}
	archive.Version = "pattern-validation-v6"
	archive.Request.ValidationStart = ""
	archive.Study = nil
	for i := range archive.Samples {
		archive.Samples[i].Period = ""
		for j := range archive.Samples[i].Outcomes {
			archive.Samples[i].Outcomes[j].PeriodCheck = nil
		}
	}
	legacy, err := s.patternValidations.Save(archive)
	if err != nil {
		t.Fatal(err)
	}
	r = validationHTTPRequest(s, http.MethodGet, "/api/pattern-validation?id="+legacy.RunID, "")
	var old strategy.PatternValidationView
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &old) != nil || old.Report.Study != nil || old.Periods != nil || old.Filter.Period != "" {
		t.Fatal("old archive automatically acquired a split")
	}
	if r = validationHTTPRequest(s, http.MethodGet, "/api/pattern-validation?id="+legacy.RunID+"&period=research", ""); r.Code != 400 {
		t.Fatal("legacy archive accepted retroactive split")
	}
}
