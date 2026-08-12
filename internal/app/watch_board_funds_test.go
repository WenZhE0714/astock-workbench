package app

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type boardFundScanMock struct {
	mu       sync.Mutex
	requests []bool
	inflows  []domain.BoardFlow
	outflows []domain.BoardFlow
}

func (mock *boardFundScanMock) FetchIndustryRanking(_ context.Context, _ domain.MarketScanMetric, descending bool, _ int) ([]domain.BoardFlow, error) {
	mock.mu.Lock()
	mock.requests = append(mock.requests, descending)
	mock.mu.Unlock()
	if descending {
		return append([]domain.BoardFlow(nil), mock.inflows...), nil
	}
	return append([]domain.BoardFlow(nil), mock.outflows...), nil
}

func (*boardFundScanMock) FetchStockRanking(context.Context, domain.MarketScanMetric, bool, int) ([]domain.MarketStockSnapshot, error) {
	return nil, nil
}

func (*boardFundScanMock) FetchStocks(context.Context, []string) ([]domain.MarketStockSnapshot, error) {
	return nil, nil
}

func (*boardFundScanMock) FetchAnnouncements(context.Context, []string, int) ([]domain.MarketAnnouncement, error) {
	return nil, nil
}

type boardFundLeaderMock struct {
	failCode string
}

func (mock boardFundLeaderMock) FetchIndustryLeaders(_ context.Context, boardCode string, limit int) ([]domain.MarketStockSnapshot, error) {
	if boardCode == mock.failCode {
		return nil, fmt.Errorf("upstream unavailable")
	}
	items := make([]domain.MarketStockSnapshot, limit)
	for index := range items {
		items[index] = domain.MarketStockSnapshot{
			Symbol: fmt.Sprintf("sh60%04d", index), Name: boardCode + fmt.Sprintf("龙头%d", index+1),
			Amount: float64(limit-index) * 1e9, Percent: float64(index), Speed: 0.1, MainNet: 1e8,
		}
	}
	return items, nil
}

func boardFundBoards(prefix string, count int, sign float64) []domain.BoardFlow {
	items := make([]domain.BoardFlow, count)
	for index := range items {
		items[index] = domain.BoardFlow{
			Code: fmt.Sprintf("BK%04d", index+1), Name: fmt.Sprintf("%s%d", prefix, index+1),
			Kind: domain.BoardKindIndustry, MainNet: sign * float64(count-index) * 1e8,
			Percent: sign * float64(index+1), RiseCount: 10, FallCount: 2,
		}
	}
	return items
}

func TestFetchBoardFundDashboardKeepsFiveBoardsAndThreeLeaders(t *testing.T) {
	scan := &boardFundScanMock{
		inflows:  boardFundBoards("流入", 6, 1),
		outflows: boardFundBoards("流出", 6, -1),
	}
	app := &App{marketScan: scan, industryLeaders: boardFundLeaderMock{}}
	dashboard, err := app.fetchBoardFundDashboard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dashboard.Inflows) != 5 || len(dashboard.Outflows) != 5 {
		t.Fatalf("unexpected board counts: %d/%d", len(dashboard.Inflows), len(dashboard.Outflows))
	}
	for _, item := range append(append([]domain.BoardFundRankingItem(nil), dashboard.Inflows...), dashboard.Outflows...) {
		if len(item.Leaders) != 3 {
			t.Fatalf("board %s has %d leaders", item.Board.Name, len(item.Leaders))
		}
	}
	if dashboard.Inflows[0].Board.MainNet <= 0 || dashboard.Outflows[0].Board.MainNet >= 0 {
		t.Fatalf("unexpected ranking directions: %#v %#v", dashboard.Inflows[0], dashboard.Outflows[0])
	}
	scan.mu.Lock()
	defer scan.mu.Unlock()
	if len(scan.requests) != 2 || scan.requests[0] == scan.requests[1] {
		t.Fatalf("expected one ascending and one descending request: %#v", scan.requests)
	}
}

func TestFetchBoardFundDashboardKeepsPartialLeaderFailures(t *testing.T) {
	scan := &boardFundScanMock{
		inflows:  boardFundBoards("流入", 5, 1),
		outflows: boardFundBoards("流出", 5, -1),
	}
	app := &App{marketScan: scan, industryLeaders: boardFundLeaderMock{failCode: "BK0003"}}
	dashboard, err := app.fetchBoardFundDashboard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dashboard.Warnings) != 2 {
		t.Fatalf("same board code in each direction should produce two scoped warnings: %#v", dashboard.Warnings)
	}
	if len(dashboard.Inflows[2].Leaders) != 0 || len(dashboard.Outflows[2].Leaders) != 0 {
		t.Fatalf("failed board should have an unavailable leader list: %#v", dashboard)
	}
	if len(dashboard.Inflows[1].Leaders) != 3 || len(dashboard.Outflows[1].Leaders) != 3 {
		t.Fatalf("successful boards should be retained: %#v", dashboard)
	}
}

func TestWatchBoardFundsFailurePreservesLastSnapshot(t *testing.T) {
	refreshedAt := time.Date(2026, 8, 12, 10, 30, 0, 0, time.Local)
	state := watchBoardFunds{}
	state.complete(domain.BoardFundDashboard{
		RefreshedAt: refreshedAt,
		Inflows:     []domain.BoardFundRankingItem{{Board: domain.BoardFlow{Name: "半导体"}}},
	})
	state.beginRefresh()
	state.fail(fmt.Errorf("network down"))
	if state.refreshedAt != refreshedAt || len(state.inflows) != 1 || state.inflows[0].Board.Name != "半导体" {
		t.Fatalf("failed refresh discarded the last snapshot: %#v", state)
	}
	if status := state.status(false); status == "" {
		t.Fatal("refresh failure should be visible")
	}
}
