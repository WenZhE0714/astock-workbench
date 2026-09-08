package web

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type dashboardRankingsStub struct{}

func (dashboardRankingsStub) FetchMarketRanking(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error) {
	return []domain.MarketRankingItem{{Symbol: "sh600000", Name: "浦发银行", Price: 12, Percent: 0, Speed: math.NaN(), Amount: math.Inf(1), Turnover: 0}}, nil
}

func TestRankingsEndpointPreservesMissingFieldsAndZero(t *testing.T) {
	server := NewServer(nil, nil, nil, nil, "", WithMarketRankings(dashboardRankingsStub{}))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/rankings?kind=amount", nil))
	var response marketRankingResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("rankings with missing fields must return valid JSON: %v, body=%s", err, recorder.Body.String())
	}
	if recorder.Code != http.StatusOK || response.Kind != domain.MarketRankingAmount || len(response.Items) != 1 {
		t.Fatalf("unexpected ranking response: %d %+v", recorder.Code, response)
	}
	item := response.Items[0]
	if item.Speed != nil || item.Amount != nil || item.Percent == nil || *item.Percent != 0 || item.Turnover == nil || *item.Turnover != 0 {
		t.Fatalf("missing values and real zero must remain distinct: %+v", item)
	}
}
