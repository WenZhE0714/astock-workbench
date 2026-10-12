package market

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSinaAdjustmentFetchKeepsRangeSeedAndExcludesFuture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`var sz002080hfq={"total":4,"data":[{"d":"2027-01-01","f":"4"},{"d":"2024-06-01","f":"2.5"},{"d":"2023-01-01","f":"2"},{"d":"1900-01-01","f":"1"}]}; /* provider signature */`))
	}))
	defer server.Close()
	t.Setenv("ASTOCK_ADJUSTMENT_API_URL", server.URL)
	snapshot, err := (SinaAdjustmentClient{}).FetchAdjustmentFactors(t.Context(), "sz002080", "2024-01-01", "2024-12-31")
	if err != nil || snapshot.Status != "checked" || len(snapshot.Factors) != 2 || snapshot.Factors[0].Date != "2023-01-01" || snapshot.Factors[1].Value != 2.5 || snapshot.FetchedAt.IsZero() || !strings.Contains(snapshot.SourceURL, "sz002080/hfq.js") {
		t.Fatalf("invalid snapshot: %+v %v", snapshot, err)
	}
}

func TestSinaAdjustmentRejectsInvalidFeed(t *testing.T) {
	for _, body := range []string{
		`var sh600519hfq={"total":1,"data":[{"d":"1900-01-01","f":"1"}]}`,
		`var sz002080hfq={"total":2,"data":[{"d":"1900-01-01","f":"1"}]}`,
		`var sz002080hfq={"total":0,"data":[]}`,
		`var sz002080hfq={"total":1,"data":[{"d":"2024-02-30","f":"1"}]}`,
		`var sz002080hfq={"total":1,"data":[{"d":"1900-01-01","f":"NaN"}]}`,
		`var sz002080hfq={"total":1,"data":[{"d":"1900-01-01","f":"0"}]}`,
		`var sz002080hfq={"total":2,"data":[{"d":"1900-01-01","f":"1"},{"d":"1900-01-01","f":"2"}]}`,
	} {
		if _, err := parseSinaAdjustmentFactors(body, "sz002080"); err == nil {
			t.Fatalf("accepted invalid feed: %s", body)
		}
	}
}
