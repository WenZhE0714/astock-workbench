package market

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const THSStockScanSource = "同花顺公开快照（大单口径）"

type THSStockScanClient struct {
	HTTPClient *http.Client
	BaseURL    string
	Industries StockIndustryClient
}

func ParseTHSStockScanPayload(raw, symbol string) (domain.MarketStockSnapshot, error) {
	if !supportsTHSFundFlow(symbol) {
		return domain.MarketStockSnapshot{}, fmt.Errorf("不支持的扫描代码 %s", symbol)
	}
	var payload struct {
		Items map[string]json.RawMessage `json:"items"`
	}
	if err := decodeTHSCallback(raw, "quotebridge_v6_realhead_hs_"+symbol[2:]+"_last", &payload); err != nil {
		return domain.MarketStockSnapshot{}, err
	}
	text := func(key string) string {
		var value string
		_ = json.Unmarshal(payload.Items[key], &value)
		return value
	}
	marketID := "33"
	if strings.HasPrefix(symbol, "sh") {
		marketID = "17"
	}
	if text("5") != symbol[2:] || text("marketid") != marketID {
		return domain.MarketStockSnapshot{}, fmt.Errorf("同花顺扫描证券代码不匹配")
	}
	stamp, err := time.ParseInLocation("2006-01-02 15:04", text("updateTime"), time.FixedZone("Asia/Shanghai", 8*60*60))
	if err != nil {
		return domain.MarketStockSnapshot{}, fmt.Errorf("同花顺扫描缺少有效行情时间")
	}
	number := func(key string) float64 { return rawNumber(payload.Items[key]) }
	item := domain.MarketStockSnapshot{
		Symbol: symbol, Source: THSStockScanSource, QuoteTime: stamp.Format("2006-01-02 15:04:05"), Name: text("name"),
		Price: number("10"), Percent: number("199112"), Amount: number("19"), Turnover: number("1968584"),
		VolumeRatio: number("1771976"), High: number("8"), Low: number("9"), Open: number("7"), PreviousClose: number("6"),
		MarketCap: number("3541450"), Speed: math.NaN(), MainNet: math.NaN(), MainRatio: math.NaN(),
	}
	if !finiteTHS(item.Price) || item.Price <= 0 || item.Name == "" {
		return domain.MarketStockSnapshot{}, fmt.Errorf("同花顺扫描未返回有效价格/名称")
	}
	buyLarge, sellLarge, buySuper, sellSuper := number("225"), number("226"), number("223"), number("224")
	if finiteTHS(buyLarge) && finiteTHS(sellLarge) && finiteTHS(buySuper) && finiteTHS(sellSuper) && buyLarge >= 0 && sellLarge >= 0 && buySuper >= 0 && sellSuper >= 0 {
		net := (buyLarge - sellLarge) + (buySuper - sellSuper)
		if finiteTHS(net) {
			item.MainNet = net
		}
		if finiteTHS(item.Amount) && item.Amount > 0 && finiteTHS(net/item.Amount*100) {
			item.MainRatio = net / item.Amount * 100
		}
	}
	return item, nil
}

func (client THSStockScanClient) FetchStocks(ctx context.Context, symbols []string) ([]domain.MarketStockSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	base := firstNonEmpty(client.BaseURL, os.Getenv("ASTOCK_THS_STOCK_SCAN_API_URL"), thsFundIndexURL)
	values := make([]domain.MarketStockSnapshot, len(symbols))
	failures := make([]string, len(symbols))
	jobs := make(chan int, len(symbols))
	for index, symbol := range symbols {
		if supportsTHSFundFlow(symbol) {
			jobs <- index
		} else {
			failures[index] = "未覆盖 " + symbol
		}
	}
	close(jobs)
	var workers sync.WaitGroup
	for range min(6, len(jobs)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				symbol := symbols[index]
				raw, err := (THSFundFlowClient{HTTPClient: client.HTTPClient}).fetchSnapshot(ctx, base, symbol[2:])
				if err == nil {
					values[index], err = ParseTHSStockScanPayload(raw, symbol)
				}
				if err != nil {
					failures[index] = symbol + ": " + err.Error()
					continue
				}
				if client.Industries != nil {
					if industry, industryErr := client.Industries.FetchStockIndustry(ctx, symbol); industryErr == nil {
						values[index].Industry = industry.Name
					}
				}
			}
		}()
	}
	workers.Wait()
	items := make([]domain.MarketStockSnapshot, 0, len(values))
	causes := make([]string, 0)
	for index, item := range values {
		if item.Symbol != "" {
			items = append(items, item)
		}
		if failures[index] != "" {
			causes = append(causes, failures[index])
		}
	}
	if ctx.Err() != nil {
		return items, ctx.Err()
	}
	if len(causes) > 0 {
		return items, fmt.Errorf("同花顺扫描: %s", strings.Join(causes, "；"))
	}
	return items, nil
}
