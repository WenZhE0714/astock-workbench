package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"github.com/wenzhe/astock-workbench/internal/notify"
	"github.com/wenzhe/astock-workbench/internal/storage"
)

type boardBrowserMarket struct{}

func (boardBrowserMarket) FetchIndustryFlows(context.Context) (map[string]domain.BoardFlow, error) {
	return map[string]domain.BoardFlow{"银行": {Code: "BK0475", Name: "银行", Source: "fixture", Percent: 1.8, MainNet: 5e8, MainRatio: 12, RiseCount: 35, FallCount: 4}, "半导体": {Code: "BK1031", Name: "半导体", Source: "fixture", Percent: 3.4, MainNet: 12e8, MainRatio: 8, RiseCount: 58, FallCount: 10}, "白酒": {Code: "BK0896", Name: "白酒", Source: "fixture", Percent: 1.2, MainNet: -3e8, MainRatio: -4}, "电力": {Code: "BK0428", Name: "电力", Source: "fixture", Percent: -2, MainNet: 2e8, MainRatio: 4}, "电池": {Code: "BK1033", Name: "电池", Source: "fixture", Percent: -3.5, MainNet: -8e8, MainRatio: -15}}, nil
}
func (boardBrowserMarket) FetchBoard(context.Context, string) (domain.BoardFlow, []domain.MarketStockSnapshot, error) {
	return domain.BoardFlow{Code: "BK0475", Name: "银行"}, []domain.MarketStockSnapshot{{Symbol: "sh600000", Name: "浦发银行", Percent: 1.8, Price: 12.3}}, nil
}
func (boardBrowserMarket) FetchBoardDailyFlows(context.Context, string) ([]domain.BoardDailyFlow, error) {
	rows := []domain.BoardDailyFlow{}
	for i := 1; i < 16; i++ {
		rows = append(rows, domain.BoardDailyFlow{Date: time.Date(2026, 10, i, 0, 0, 0, 0, realtimeWebLocation).Format(time.DateOnly), MainNet: float64(i%4-1) * 1e8})
	}
	return rows, nil
}

func TestBoardWorkbenchBrowserFixture(t *testing.T) {
	address := os.Getenv("ASTOCK_BOARD_TEST_ADDR")
	if address == "" {
		t.Skip("browser fixture is opt-in")
	}
	root := t.TempDir()
	s := chartServerFixture(filepath.Join(root, "plans"))
	s.now = func() time.Time { return time.Date(2026, 10, 16, 15, 40, 0, 0, realtimeWebLocation) }
	provider := boardBrowserMarket{}
	s.industryFlows = provider
	s.boardDetails = provider
	s.quotes = quoteClientFunc(func(_ context.Context, symbols []string) ([]domain.Quote, error) {
		rows := []domain.Quote{}
		for _, symbol := range symbols {
			rows = append(rows, domain.Quote{Symbol: symbol, Name: "浦发银行", Current: "12.30", QuoteTime: "2026-10-16 15:00:00"})
		}
		return rows, nil
	})
	store := storage.NewBoardSnapshotStore(filepath.Join(root, "boards"))
	WithBoardWorkbench(store, provider)(s)
	for i := 0; i < 8; i++ {
		at := time.Date(2026, 10, 16, 9, 35+i, 0, 0, realtimeWebLocation)
		snapshot := domain.BoardSnapshot{Version: 1, ID: at.Format("1504"), Date: "2026-10-16", CapturedAt: at, CalendarKnown: true, Items: []domain.BoardObservation{}, Signals: []domain.BoardSignal{{Symbol: "sh600000", Name: "浦发银行", Industry: "银行", State: "triggered", Score: 78, Price: 11.5 + float64(i)*.1, AsOf: at, Reason: "资金与趋势共振"}}, Holdings: []domain.BoardHolding{{Profile: "balanced", ProfileName: "均衡型", Symbol: "sh600000", Name: "浦发银行", Industry: "银行", Quantity: 1000, Available: 1000, MarketValue: 12000, AsOf: at}}}
		flows, _ := provider.FetchIndustryFlows(context.Background())
		for _, flow := range flows {
			value := flow.MainNet * (.5 + float64(i)/10)
			pct := flow.Percent
			snapshot.Items = append(snapshot.Items, domain.BoardObservation{Code: flow.Code, Name: flow.Name, Source: flow.Source, MainNet: &value, Percent: &pct, RiseCount: flow.RiseCount, FallCount: flow.FallCount})
		}
		if err := store.Save(snapshot); err != nil {
			t.Fatal(err)
		}
	}
	records := storage.NewNativeNotificationStore(filepath.Join(root, "mac.json"))
	reviews := storage.NewNotificationReviewStore(filepath.Join(root, "reviews.json"))
	WithNotificationCenter(records, reviews)(s)
	if err := records.Save(notify.State{Records: map[string]notify.Record{"fixture-event": {Message: notify.Message{ID: "fixture-event", Symbol: "sh600000", Profile: "balanced", Kind: "buy", Title: "ASTOCK · 模拟买入", Subtitle: "均衡型 · 浦发银行", Body: "开仓 · 12.30 元 × 1000 股", At: s.currentTime()}, Status: "submitted"}}}); err != nil {
		t.Fatal(err)
	}
	t.Logf("board fixture http://%s", address)
	if err := http.ListenAndServe(address, s.Handler()); err != nil {
		t.Fatal(err)
	}
}
