package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/strategy"
)

type validationFactorFixture struct {
	Date      string
	Err       error
	Calls     *int
	Ratio     float64
	ShareDate string
}

func (fixture validationFactorFixture) FetchAdjustmentFactors(_ context.Context, symbol, start, end string) (domain.AdjustmentSnapshot, error) {
	if fixture.Calls != nil {
		*fixture.Calls++
	}
	factors := []domain.AdjustmentFactor{{Date: "1900-01-01", Value: 2}}
	if symbol == "sh600004" && fixture.Date != "" {
		ratio := fixture.Ratio
		if ratio == 0 {
			ratio = 1.01
		}
		factors = append(factors, domain.AdjustmentFactor{Date: fixture.Date, Value: 2 * ratio})
	}
	if symbol == "sh600007" && fixture.ShareDate != "" {
		factors = append(factors, domain.AdjustmentFactor{Date: fixture.ShareDate, Value: 3})
	}
	return domain.AdjustmentSnapshot{Symbol: symbol, Start: start, End: end, Status: "checked", Source: "隔离复权因子", SourceURL: "https://finance.sina.com.cn/", FetchedAt: time.Date(2026, 10, 9, 16, 0, 0, 0, realtimeWebLocation), Factors: factors}, fixture.Err
}

func TestPatternAdjustmentAPIArchivesRawAndNeverSilentlyFallsBack(t *testing.T) {
	s := patternValidationServerFixture(t.TempDir())
	history := s.history.(patternHistoryFixture)
	date := history["sh600004"][85].Date
	count := 0
	previous := history["sh600004"][84].Close
	s.patternFactors = validationFactorFixture{Date: date, Calls: &count, Ratio: previous / (previous - .25)}
	s.patternActions = validationActionFixture{Events: map[string][]domain.CorporateAction{"sh600004": {{ID: "dividend", Symbol: "sh600004", ExDate: date, Kinds: []string{"cash-dividend"}, Source: "fixture", Terms: validationTerms(2.5, 0, 0)}}}}
	raw := `{"symbols":["600004"],"start":"2026-06-01","end":"2026-09-25"}`
	adjusted := `{"symbols":["600004"],"start":"2026-06-01","end":"2026-09-25","price_basis":"hfq"}`
	if r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-validation", raw); r.Code != 200 || count != 0 {
		t.Fatalf("raw request fetched factors: %d %d", r.Code, count)
	}
	r := validationHTTPRequest(s, http.MethodPost, "/api/pattern-validation", adjusted)
	var view strategy.PatternValidationView
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &view) != nil || len(view.Report.Adjustments) != 1 || view.Report.Adjustments[0].Status != "applied" || count != 1 {
		t.Fatalf("adjusted API failed: %d %s", r.Code, r.Body.String())
	}
	archive, err := s.patternValidations.Load(view.Report.RunID)
	if err != nil || archive.Inputs[0].Adjustment == nil || archive.Inputs[0].Bars[89].Close != history["sh600004"][89].Close {
		t.Fatal("original prices or factor snapshot lost")
	}
	if len(archive.Adjustments[0].Quality) != 1 || archive.Adjustments[0].Quality[0].State != "passed" || archive.Inputs[0].Actions.Events[0].Terms == nil {
		t.Fatal("numeric verification evidence missing from archive")
	}
	s.patternFactors = validationFactorFixture{Err: errors.New("offline"), Calls: &count}
	r = validationHTTPRequest(s, http.MethodGet, "/api/pattern-validation?id="+view.Report.RunID+"&horizon=20&action=adjusted", "")
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &view) != nil || view.Totals.ActionAdjusted < 1 || count != 1 {
		t.Fatalf("archive reread changed/fetched factors: %s", r.Body.String())
	}
	archive.Version = "pattern-validation-v4"
	archive.Adjustments[0].Quality = nil
	archive.Adjustments[0].QualityMethod = ""
	archive.Adjustments[0].QualityStatus = ""
	archive.Request.PriceBasis = ""
	legacy, err := s.patternValidations.Save(archive)
	if err != nil {
		t.Fatal(err)
	}
	if r = validationHTTPRequest(s, http.MethodGet, "/api/pattern-validation?id="+legacy.RunID, ""); r.Code != 200 {
		t.Fatal("v4 archive no longer readable")
	}
	r = validationHTTPRequest(s, http.MethodPost, "/api/pattern-validation", adjusted)
	if r.Code != 502 || !strings.Contains(r.Body.String(), "offline") {
		t.Fatalf("missing factors silently fell back: %d %s", r.Code, r.Body.String())
	}
	r = validationHTTPRequest(s, http.MethodPost, "/api/pattern-validation", strings.Replace(adjusted, `"hfq"`, `"qfq"`, 1))
	if r.Code != 400 {
		t.Fatal("unknown basis accepted")
	}
}

func validationTerms(cash, bonus, transfer float64) *domain.DistributionTerms {
	return &domain.DistributionTerms{CashPer10: &cash, BonusPer10: &bonus, CapitalizationPer10: &transfer}
}
