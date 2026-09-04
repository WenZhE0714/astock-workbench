package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type boardRankingStub struct{}

func (boardRankingStub) FetchIndustryFlows(context.Context) (map[string]domain.BoardFlow, error) {
	return map[string]domain.BoardFlow{
		"强势": {Code: "BK001", Name: "强势", Percent: 3.2, MainNet: 2e8, RiseCount: 80, FallCount: 10},
		"弱势": {Code: "BK002", Name: "弱势", Percent: -2.1, MainNet: -1e8, RiseCount: 10, FallCount: 70},
	}, nil
}

func TestBoardsEndpointSortsIndustrySnapshots(t *testing.T) {
	server := NewServer(nil, nil, nil, nil, "", WithIndustryFlows(boardRankingStub{}))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/boards?sort=weak", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response boardRankingResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 2 || response.Items[0].Name != "弱势" || response.Sort != "weak" {
		t.Fatalf("unexpected board ranking: %+v", response)
	}
}
