package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

func TestTradePlaybookEndpointAggregatesStoredPlansAndReviews(t *testing.T) {
	root := t.TempDir()
	plans := storage.NewTradePlanStore(filepath.Join(root, "plans"))
	reviews := storage.NewTradePlanReviewStore(filepath.Join(root, "reviews"))
	plan := domain.TradePlan{
		ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Version: 1, Symbol: "sh600519",
		CreatedAt: time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local), ExpiresOn: "2026-09-25",
		Analysis:  domain.ChartAnalysis{Symbol: "sh600519", Fingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", DataDate: "2026-09-18"},
		Structure: domain.ChartStructure{ID: "range-breakout", Name: "区间突破", Plan: &domain.ChartPlanLevels{EntryLow: 100, EntryHigh: 101, Invalidation: 98, Target2: 106}},
	}
	if _, _, err := plans.Save(plan); err != nil {
		t.Fatal(err)
	}
	entry, exit := 100.0, 104.0
	if _, err := reviews.Update(plan, domain.TradePlanReviewRevision{
		ExecutionStatus: "followed", Discipline: "followed", Tags: []string{"放量"}, ActualEntry: &entry, ActualExit: &exit,
	}, time.Date(2026, 9, 22, 16, 0, 0, 0, time.Local)); err != nil {
		t.Fatal(err)
	}
	server := NewServer(nil, nil, nil, nil, "", WithTradePlans(plans), WithTradePlanReviews(reviews))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/trade-playbook", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Report domain.TradePlaybookReport `json:"report"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Report.TotalPlans != 1 || response.Report.CompletedTrades != 1 || response.Report.AverageR == nil || *response.Report.AverageR != 2 || len(response.Report.Setups) != 1 || len(response.Report.Tags) != 1 {
		t.Fatalf("unexpected playbook report: %+v", response.Report)
	}
	loaded, err := reviews.Load(plan.Symbol, plan.ID)
	if err != nil || loaded.Sequence != 1 {
		t.Fatalf("GET mutated review: %+v %v", loaded, err)
	}
}

type playbookAllPlanStub struct {
	*storage.TradePlanStore
	items []domain.TradePlan
}

func (stub playbookAllPlanStub) All(limit int) ([]domain.TradePlan, error) {
	if limit != 0 {
		return nil, fmt.Errorf("unexpected bounded plan aggregation: %d", limit)
	}
	return stub.items, nil
}

type playbookAllReviewStub struct {
	*storage.TradePlanReviewStore
	items []domain.TradePlanReview
}

func (stub playbookAllReviewStub) All(limit int) ([]domain.TradePlanReview, error) {
	if limit != 0 {
		return nil, fmt.Errorf("unexpected bounded review aggregation: %d", limit)
	}
	return stub.items, nil
}

func TestTradePlaybookEndpointDoesNotDropOlderPlanReviews(t *testing.T) {
	now := time.Date(2026, 9, 22, 18, 0, 0, 0, time.UTC)
	plans := make([]domain.TradePlan, 0, 2001)
	for index := 0; index < 2001; index++ {
		plans = append(plans, domain.TradePlan{
			ID: fmt.Sprintf("%064x", index), Symbol: "sh600519", CreatedAt: now.Add(-time.Duration(index) * time.Hour),
			Analysis:  domain.ChartAnalysis{Fingerprint: "snapshot"},
			Structure: domain.ChartStructure{ID: "range-breakout", Name: "区间突破", Plan: &domain.ChartPlanLevels{Invalidation: 98}},
		})
	}
	review := domain.TradePlanReview{Version: 1, Symbol: "sh600519", PlanID: plans[2000].ID, Fingerprint: "snapshot", UpdatedAt: now,
		Current: domain.TradePlanReviewRevision{ExecutionStatus: "followed", ActualEntry: playbookTestNumber(100), ActualExit: playbookTestNumber(104)},
	}
	server := NewServer(nil, nil, nil, nil, "", WithTradePlans(playbookAllPlanStub{items: plans}), WithTradePlanReviews(playbookAllReviewStub{items: []domain.TradePlanReview{review}}))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/trade-playbook", nil))
	var result struct {
		Report domain.TradePlaybookReport `json:"report"`
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("request failed: %d %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Report.TotalPlans != 2001 || len(result.Report.Recent) != 2001 || result.Report.CompletedTrades != 1 || *result.Report.AverageR != 2 {
		t.Fatalf("older review was dropped: total=%d rows=%d completed=%d", result.Report.TotalPlans, len(result.Report.Recent), result.Report.CompletedTrades)
	}
}

func TestTradePlaybookEndpointRequiresReadOnlyMethod(t *testing.T) {
	server := NewServer(nil, nil, nil, nil, "")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/trade-playbook", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
}

func TestPlaybookPlanLinkCanLoadOutsideRecentPlanList(t *testing.T) {
	server := chartServerFixture(t.TempDir())
	var oldest domain.TradePlan
	for index := 0; index < 101; index++ {
		plan := domain.TradePlan{
			ID: fmt.Sprintf("%064x", index), Version: 1, Symbol: "sh600519", CreatedAt: server.now().Add(time.Duration(index) * time.Minute), ExpiresOn: "2026-09-25",
			Analysis:  domain.ChartAnalysis{Symbol: "sh600519", Fingerprint: "snapshot"},
			Structure: domain.ChartStructure{ID: "range-breakout", Plan: &domain.ChartPlanLevels{Invalidation: 98}},
		}
		if index == 0 {
			oldest = plan
		}
		if _, _, err := server.tradePlans.Save(plan); err != nil {
			t.Fatal(err)
		}
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/trade-plans?symbol=sh600519&plan_id="+oldest.ID, nil))
	var response struct {
		Items []domain.TradePlan `json:"items"`
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("plan link failed: %d %s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 101 || response.Items[0].ID != oldest.ID {
		t.Fatalf("linked plan missing: %d items", len(response.Items))
	}
}

func TestTradePlaybookBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_PLAYBOOK_TEST_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	root := t.TempDir()
	server := chartServerFixture(filepath.Join(root, "plans"))
	plans := server.tradePlans
	reviews := storage.NewTradePlanReviewStore(filepath.Join(root, "reviews"))
	server.planReviews = reviews
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	now := time.Date(2026, 9, 22, 18, 0, 0, 0, location)
	definitions := []struct {
		symbol, structure, name, status, discipline string
		realized                                    *float64
		tags                                        []string
	}{
		{"sh600519", "range-breakout", "区间突破", "followed", "followed", playbookTestNumber(2), []string{"放量", "主线"}},
		{"sz000001", "range-breakout", "区间突破", "deviated", "deviated", playbookTestNumber(-1), []string{"放量"}},
		{"sh600519", "ma-pullback", "趋势回踩", "followed", "partial", playbookTestNumber(.8), []string{"趋势"}},
		{"sh601318", "ma-pullback", "趋势回踩", "watching", "", nil, []string{"等待确认"}},
		{"sz002594", "assistant-breakout", "AI突破规则", "skipped", "not_applicable", nil, []string{"高开"}},
	}
	server.nameCacheFile = filepath.Join(root, "names.tsv")
	names, err := storage.LoadNameCache(server.nameCacheFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := names.Remember([]domain.Candidate{{Symbol: "sh600519", Name: "贵州茅台"}, {Symbol: "sz000001", Name: "平安银行"}, {Symbol: "sh601318", Name: "中国平安"}, {Symbol: "sz002594", Name: "比亚迪"}}); err != nil {
		t.Fatal(err)
	}
	for index, definition := range definitions {
		idCharacter := string(rune('a' + index))
		fingerprintCharacter := string(rune('f' + index))
		plan := domain.TradePlan{
			ID: strings.Repeat(idCharacter, 64), Version: 1, Symbol: definition.symbol,
			CreatedAt: now.Add(-time.Duration(len(definitions)-index) * 24 * time.Hour), ExpiresOn: "2026-09-30",
			Analysis:  domain.ChartAnalysis{Symbol: definition.symbol, Fingerprint: strings.Repeat(fingerprintCharacter, 64), DataDate: "2026-09-18"},
			Structure: domain.ChartStructure{ID: definition.structure, Name: definition.name, Plan: &domain.ChartPlanLevels{EntryLow: 100, EntryHigh: 101, Invalidation: 98, Target2: 106}},
		}
		if _, _, err := plans.Save(plan); err != nil {
			t.Fatal(err)
		}
		revision := domain.TradePlanReviewRevision{ExecutionStatus: definition.status, Discipline: definition.discipline, Tags: definition.tags, ExitReason: "按剧本复盘"}
		if definition.realized != nil {
			entry := 100.0
			exit := entry + *definition.realized*2
			revision.ActualEntry, revision.ActualExit = &entry, &exit
		}
		if _, err := reviews.Update(plan, revision, now.Add(time.Duration(index-5)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	server.now = func() time.Time { return now }
	if err := http.ListenAndServe(address, server.Handler()); err != nil {
		t.Fatal(err)
	}
}

func playbookTestNumber(value float64) *float64 { return &value }
