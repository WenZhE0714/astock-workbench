package market

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type stockIndustryFunc func(context.Context, string) (StockIndustry, error)

func (fn stockIndustryFunc) FetchStockIndustry(ctx context.Context, symbol string) (StockIndustry, error) {
	return fn(ctx, symbol)
}

func TestTHSStockIndustryUsesF10AndIndependentJSONFallback(t *testing.T) {
	for _, profileAvailable := range []bool{true, false} {
		calls := 0
		client := THSStockIndustryClient{ProfileURL: "https://ths.test/profile/{code}", FundURL: "https://ths.test/funds/{code}", HTTPClient: &http.Client{Transport: rankingRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			body := ""
			switch r.URL.Path {
			case "/profile/301052":
				body = `<span>所属申万行业：</span><a>出版</a>`
				if !profileAvailable {
					body = `<span>所属行业：</span><span>所属地域：上海</span>`
				}
				encoded, err := simplifiedchinese.GB18030.NewEncoder().String(body)
				if err != nil {
					t.Fatal(err)
				}
				body = encoded
			case "/funds/301052":
				body = `{"field":{"hyname":"文化传媒"}}`
			default:
				t.Fatalf("unexpected industry request: %s", r.URL.Path)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		industry, err := client.FetchStockIndustry(context.Background(), "sz301052")
		if err != nil {
			t.Fatal(err)
		}
		if profileAvailable && (industry.Name != "出版" || industry.Source != "同花顺F10申万行业" || calls != 1) {
			t.Fatalf("wrong F10 industry: %+v calls=%d", industry, calls)
		}
		if !profileAvailable && (industry.Name != "文化传媒" || industry.Source != "同花顺资金页行业" || calls != 2) {
			t.Fatalf("wrong fallback industry: %+v calls=%d", industry, calls)
		}
	}
}

func TestIndustryEnrichmentPreservesRankingAndCachesSuccessfulLabels(t *testing.T) {
	original := []domain.MarketRankingItem{
		{Symbol: "sh600519", Industry: "白酒", IndustrySource: "已有分类", Percent: 3},
		{Symbol: "sz301052", Industry: "--", Percent: 2, Price: 30.47},
		{Symbol: "sh688004", Industry: "", Percent: 1, Price: 37.25},
	}
	var calls atomic.Int32
	primary := rankingFunc(func(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error) {
		return original, nil
	})
	lookup := stockIndustryFunc(func(_ context.Context, symbol string) (StockIndustry, error) {
		calls.Add(1)
		if symbol == "sh600519" {
			t.Error("existing industry was re-fetched")
		}
		return StockIndustry{Name: "电子", Source: "同花顺F10申万行业"}, nil
	})
	client := NewIndustryEnrichedRankingClient(primary, lookup)
	for range 2 {
		items, err := client.FetchMarketRanking(context.Background(), domain.MarketRankingGainers, 3)
		if err != nil || len(items) != 3 {
			t.Fatalf("ranking lost: %+v %v", items, err)
		}
		for index, item := range items {
			if item.Symbol != original[index].Symbol || item.Percent != original[index].Percent || item.Price != original[index].Price {
				t.Fatalf("ranking changed: %+v", items)
			}
		}
		if items[0].Industry != "白酒" || items[0].IndustrySource != "已有分类" || items[1].Industry != "电子" || items[2].IndustrySource != "同花顺F10申万行业" {
			t.Fatalf("enrichment missing: %+v", items)
		}
		items[1].Industry = "调用方修改"
	}
	if calls.Load() != 2 || original[1].Industry != "--" {
		t.Fatalf("cache/input isolation failed: calls=%d original=%+v", calls.Load(), original)
	}
}

func TestIndustryEnrichmentRetriesFailuresAndExpiresCache(t *testing.T) {
	primary := rankingFunc(func(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error) {
		return []domain.MarketRankingItem{{Symbol: "sz301052", Industry: "--"}}, nil
	})
	calls := 0
	client := NewIndustryEnrichedRankingClient(primary, stockIndustryFunc(func(context.Context, string) (StockIndustry, error) {
		calls++
		if calls == 1 {
			return StockIndustry{}, errors.New("offline")
		}
		return StockIndustry{Name: "出版", Source: "F10"}, nil
	}))
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }
	items, err := client.FetchMarketRanking(context.Background(), domain.MarketRankingGainers, 1)
	if err != nil || len(items) != 1 || items[0].Industry != "--" {
		t.Fatal("optional lookup failed the whole ranking")
	}
	items, err = client.FetchMarketRanking(context.Background(), domain.MarketRankingGainers, 1)
	if err != nil || items[0].Industry != "出版" || calls != 2 {
		t.Fatal("failed lookup poisoned the cache")
	}
	now = now.Add(24 * time.Hour)
	_, _ = client.FetchMarketRanking(context.Background(), domain.MarketRankingGainers, 1)
	if calls != 3 {
		t.Fatalf("expired classification was not refreshed: %d", calls)
	}
}

func TestIndustryEnrichmentCoalescesConcurrentLookups(t *testing.T) {
	primary := rankingFunc(func(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error) {
		return []domain.MarketRankingItem{{Symbol: "sz301052", Industry: "--"}}, nil
	})
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	client := NewIndustryEnrichedRankingClient(primary, stockIndustryFunc(func(context.Context, string) (StockIndustry, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return StockIndustry{Name: "出版", Source: "F10"}, nil
	}))
	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			items, err := client.FetchMarketRanking(context.Background(), domain.MarketRankingGainers, 1)
			if err != nil || items[0].Industry != "出版" {
				t.Errorf("concurrent enrichment failed: %+v %v", items, err)
			}
		}()
	}
	<-started
	close(release)
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("duplicate industry requests: %d", calls.Load())
	}
}

func TestIndustryLookupCancellationKeepsLoadedRanking(t *testing.T) {
	primary := rankingFunc(func(context.Context, domain.MarketRankingKind, int) ([]domain.MarketRankingItem, error) {
		return []domain.MarketRankingItem{{Symbol: "sz301052", Industry: "--", Price: 30.47}}, nil
	})
	client := NewIndustryEnrichedRankingClient(primary, stockIndustryFunc(func(ctx context.Context, _ string) (StockIndustry, error) {
		<-ctx.Done()
		return StockIndustry{}, ctx.Err()
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	items, err := client.FetchMarketRanking(ctx, domain.MarketRankingGainers, 1)
	if err != nil || len(items) != 1 || items[0].Price != 30.47 {
		t.Fatalf("industry deadline erased loaded ranking: %+v %v", items, err)
	}
}
