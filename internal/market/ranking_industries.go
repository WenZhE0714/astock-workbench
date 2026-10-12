package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type StockIndustry struct {
	Name   string
	Source string
}

type StockIndustryClient interface {
	FetchStockIndustry(context.Context, string) (StockIndustry, error)
}

type THSStockIndustryClient struct {
	HTTPClient *http.Client
	ProfileURL string
	FundURL    string
}

func validStockIndustry(name string) bool {
	name = strings.TrimSpace(name)
	switch name {
	case "", "--", "-", "null", "暂无", "暂无数据", "未知", "未分类":
		return false
	}
	return !strings.ContainsAny(name, ":：<>|\r\n")
}

func (client THSStockIndustryClient) fetch(ctx context.Context, address string) (string, error) {
	headers := map[string]string{"Referer": "https://basic.10jqka.com.cn/"}
	if client.HTTPClient != nil {
		return fetchDecodedWithClient(ctx, client.HTTPClient, address, nil, headers)
	}
	return fetchTHSDecoded(ctx, address, nil, headers)
}

func (client THSStockIndustryClient) FetchStockIndustry(ctx context.Context, symbol string) (StockIndustry, error) {
	if !supportsTHSFundFlow(symbol) {
		return StockIndustry{}, fmt.Errorf("不支持的行业映射代码 %s", symbol)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	profileBase := firstNonEmpty(client.ProfileURL, os.Getenv("ASTOCK_THS_STOCK_PROFILE_URL"), thsStockProfileURL)
	profileCtx, cancelProfile := fallbackContext(ctx, 2, time.Second)
	raw, err := client.fetch(profileCtx, thsStockProfileAddress(profileBase, symbol))
	cancelProfile()
	if err == nil {
		if industry := ParseTHSStockIndustry(decodeTHSAuto(raw)); validStockIndustry(industry) {
			return StockIndustry{Name: industry, Source: "同花顺F10申万行业"}, nil
		}
	}
	if ctx.Err() != nil {
		return StockIndustry{}, ctx.Err()
	}
	base := firstNonEmpty(client.FundURL, os.Getenv("ASTOCK_THS_FUND_FLOW_API_URL"), thsFundFlowAPIURL)
	raw, err = client.fetch(ctx, strings.ReplaceAll(base, "{code}", symbol[2:]))
	if err != nil {
		return StockIndustry{}, err
	}
	var payload struct {
		Field struct {
			Name string `json:"hyname"`
		} `json:"field"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return StockIndustry{}, err
	}
	name := strings.TrimSpace(payload.Field.Name)
	if !validStockIndustry(name) {
		return StockIndustry{}, fmt.Errorf("同花顺未返回有效行业分类")
	}
	return StockIndustry{Name: name, Source: "同花顺资金页行业"}, nil
}

type cachedStockIndustry struct {
	industry StockIndustry
	at       time.Time
}

// Industry labels change much less frequently than rankings. Keep successful
// classifications for a day and coalesce concurrent requests for one stock.
type IndustryEnrichedRankingClient struct {
	primary    MarketRankingClient
	industries StockIndustryClient
	now        func() time.Time
	mu         sync.Mutex
	cache      map[string]cachedStockIndustry
	inflight   map[string]chan struct{}
}

func NewIndustryEnrichedRankingClient(primary MarketRankingClient, industries StockIndustryClient) *IndustryEnrichedRankingClient {
	return &IndustryEnrichedRankingClient{primary: primary, industries: industries, now: time.Now,
		cache: make(map[string]cachedStockIndustry), inflight: make(map[string]chan struct{})}
}

func (client *IndustryEnrichedRankingClient) lookup(ctx context.Context, symbol string) (StockIndustry, bool) {
	for {
		if ctx.Err() != nil {
			return StockIndustry{}, false
		}
		client.mu.Lock()
		now := client.now()
		if entry, ok := client.cache[symbol]; ok && now.Sub(entry.at) >= 0 && now.Sub(entry.at) < 24*time.Hour {
			client.mu.Unlock()
			return entry.industry, true
		}
		if pending := client.inflight[symbol]; pending != nil {
			client.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return StockIndustry{}, false
			}
		}
		pending := make(chan struct{})
		client.inflight[symbol] = pending
		client.mu.Unlock()
		industry, err := client.industries.FetchStockIndustry(ctx, symbol)
		valid := err == nil && validStockIndustry(industry.Name)
		client.mu.Lock()
		if valid {
			client.cache[symbol] = cachedStockIndustry{industry: industry, at: client.now()}
		}
		delete(client.inflight, symbol)
		close(pending)
		client.mu.Unlock()
		return industry, valid
	}
}

func (client *IndustryEnrichedRankingClient) FetchMarketRanking(ctx context.Context, kind domain.MarketRankingKind, limit int) ([]domain.MarketRankingItem, error) {
	if client == nil || client.primary == nil {
		return nil, fmt.Errorf("榜单数据源未初始化")
	}
	items, err := client.primary.FetchMarketRanking(ctx, kind, limit)
	if err != nil || len(items) == 0 || client.industries == nil {
		return items, err
	}
	items = append([]domain.MarketRankingItem(nil), items...)
	jobs := make(chan int, len(items))
	for index, item := range items {
		if validStockIndustry(item.Industry) {
			source := firstNonEmpty(item.IndustrySource, item.Source)
			client.mu.Lock()
			client.cache[item.Symbol] = cachedStockIndustry{industry: StockIndustry{Name: item.Industry, Source: source}, at: client.now()}
			client.mu.Unlock()
		} else {
			jobs <- index
		}
	}
	close(jobs)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	for range min(4, len(jobs)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				if industry, ok := client.lookup(ctx, items[index].Symbol); ok {
					items[index].Industry, items[index].IndustrySource = industry.Name, industry.Source
				}
			}
		}()
	}
	workers.Wait()
	return items, nil
}
