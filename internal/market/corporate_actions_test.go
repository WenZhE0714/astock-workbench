package market

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func distributionFixtureRow(date string) distributionRow {
	return distributionRow{Code: "002080", ReportDate: date, ExDate: date + " 00:00:00", Progress: "实施分配", Description: "10派1元", Cash: json.RawMessage(`1`), Bonus: json.RawMessage(`null`), Transfer: json.RawMessage(`0`)}
}

func TestCorporateActionsFetchesAllPagesAndFiltersDates(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		if q.Get("reportName") != "RPT_SHAREBONUS_DET" || q.Get("filter") != `(SECURITY_CODE="002080")` || q.Get("pageSize") != "100" {
			t.Errorf("unexpected query: %v", q)
		}
		page, _ := strconv.Atoi(q.Get("pageNumber"))
		rows := []distributionRow{}
		for i := (page - 1) * 100; i < min(page*100, 101); i++ {
			rows = append(rows, distributionFixtureRow(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format(time.DateOnly)))
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{"pages": 2, "count": 101, "data": rows}})
	}))
	defer server.Close()
	t.Setenv("ASTOCK_CORPORATE_ACTION_API_URL", server.URL)
	snapshot, err := (EastmoneyClient{}).FetchCorporateActions(t.Context(), "sz002080", "2024-02-01", "2024-03-01")
	if err != nil || calls != 2 || snapshot.Status != "checked" || len(snapshot.Events) != 30 || snapshot.Events[0].ExDate != "2024-02-01" || snapshot.Events[29].ExDate != "2024-03-01" || snapshot.FetchedAt.IsZero() {
		t.Fatalf("pagination/filter failed: calls=%d events=%d err=%v", calls, len(snapshot.Events), err)
	}
}

func TestCorporateActionsRejectsAmbiguousResponses(t *testing.T) {
	for _, kind := range []string{"missing-result", "missing-count", "missing-data", "truncated", "wrong-symbol", "missing-ex-date", "invalid-date", "missing-ratio", "invalid-ratio", "duplicate", "conflict"} {
		t.Run(kind, func(t *testing.T) {
			row := distributionFixtureRow("2024-05-01")
			rows := []distributionRow{row}
			result := map[string]any{"pages": 1, "count": 1, "data": rows}
			payload := map[string]any{"success": true, "result": result}
			switch kind {
			case "missing-result":
				payload["result"] = nil
			case "missing-count":
				delete(result, "count")
			case "missing-data":
				delete(result, "data")
			case "truncated":
				result["count"] = 2
			case "wrong-symbol":
				rows[0].Code = "600519"
			case "missing-ex-date":
				rows[0].ExDate = ""
			case "invalid-date":
				rows[0].ExDate = "2024-02-30"
			case "missing-ratio":
				result["data"] = []map[string]any{{"SECURITY_CODE": "002080", "ASSIGN_PROGRESS": "实施分配", "EX_DIVIDEND_DATE": "2024-05-01", "BONUS_RATIO": nil, "IT_RATIO": 0}}
			case "invalid-ratio":
				rows[0].Cash = json.RawMessage(`"NaN"`)
			case "duplicate":
				result["data"], result["count"] = append(rows, row), 2
			case "conflict":
				row.Description = "conflicting plan"
				result["data"], result["count"] = append(rows, row), 2
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(payload) }))
			defer server.Close()
			t.Setenv("ASTOCK_CORPORATE_ACTION_API_URL", server.URL)
			snapshot, err := (EastmoneyClient{}).FetchCorporateActions(t.Context(), "sz002080", "2024-01-01", "2024-12-31")
			if err == nil || snapshot.Status == "checked" {
				t.Fatalf("ambiguous response accepted: %s %+v", kind, snapshot)
			}
		})
	}
}

func TestCorporateActionsDistinguishesProposalsEmptyAndImplemented(t *testing.T) {
	for _, count := range []int{0, 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			rows := []distributionRow{}
			if count == 1 {
				row := distributionFixtureRow("2024-05-01")
				row.Progress = "董事会预案"
				rows = append(rows, row)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"success": true, "result": map[string]any{"pages": 1, "count": count, "data": rows}})
			}))
			defer server.Close()
			t.Setenv("ASTOCK_CORPORATE_ACTION_API_URL", server.URL)
			snapshot, err := (EastmoneyClient{}).FetchCorporateActions(t.Context(), "sz002080", "2024-01-01", "2024-12-31")
			if err != nil || snapshot.Status != "checked" || len(snapshot.Events) != 0 {
				t.Fatalf("empty/proposal handling: %+v %v", snapshot, err)
			}
		})
	}
	row := distributionFixtureRow("2024-05-01")
	row.Bonus, row.Transfer = json.RawMessage(`"1"`), json.RawMessage(`2`)
	event, err := distributionEvent(row, "sz002080", "https://data.eastmoney.com/yjfp/")
	if err != nil || len(event.Kinds) != 3 || event.SourceURL == "" {
		t.Fatalf("mixed distribution lost types: %+v %v", event, err)
	}
	if event.Terms == nil || *event.Terms.CashPer10 != 1 || *event.Terms.BonusPer10 != 1 || *event.Terms.CapitalizationPer10 != 2 {
		t.Fatalf("numeric distribution terms lost: %+v", event.Terms)
	}
}
