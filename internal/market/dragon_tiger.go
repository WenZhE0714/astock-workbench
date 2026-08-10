package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const (
	dragonTigerAPIURL     = "https://datacenter-web.eastmoney.com/api/data/v1/get"
	dragonTigerWindowDays = 30
)

type DragonTigerClient interface {
	FetchDragonTiger(context.Context, string) (domain.DragonTigerSnapshot, error)
}

type dragonTigerPayload struct {
	Success bool `json:"success"`
	Result  *struct {
		Data []struct {
			Code            string          `json:"SECURITY_CODE"`
			Name            string          `json:"SECURITY_NAME_ABBR"`
			TradeDate       string          `json:"TRADE_DATE"`
			SeatSummary     string          `json:"EXPLAIN"`
			Reason          string          `json:"EXPLANATION"`
			ClosePrice      json.RawMessage `json:"CLOSE_PRICE"`
			ChangePercent   json.RawMessage `json:"CHANGE_RATE"`
			NetAmount       json.RawMessage `json:"BILLBOARD_NET_AMT"`
			BuyAmount       json.RawMessage `json:"BILLBOARD_BUY_AMT"`
			SellAmount      json.RawMessage `json:"BILLBOARD_SELL_AMT"`
			DealAmount      json.RawMessage `json:"BILLBOARD_DEAL_AMT"`
			MarketAmount    json.RawMessage `json:"ACCUM_AMOUNT"`
			NetRatio        json.RawMessage `json:"DEAL_NET_RATIO"`
			DealAmountRatio json.RawMessage `json:"DEAL_AMOUNT_RATIO"`
			Turnover        json.RawMessage `json:"TURNOVERRATE"`
			Next1Percent    json.RawMessage `json:"D1_CLOSE_ADJCHRATE"`
			Next2Percent    json.RawMessage `json:"D2_CLOSE_ADJCHRATE"`
			Next5Percent    json.RawMessage `json:"D5_CLOSE_ADJCHRATE"`
			Next10Percent   json.RawMessage `json:"D10_CLOSE_ADJCHRATE"`
		} `json:"data"`
	} `json:"result"`
}

func dragonTigerSymbol(code string) string {
	if len(code) != 6 {
		return ""
	}
	prefix := "sz"
	if strings.HasPrefix(code, "6") {
		prefix = "sh"
	}
	return prefix + code
}

func normalizeTradeDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 10 {
		return value[:10]
	}
	return value
}

func ParseDragonTigerPayload(raw string) []domain.DragonTigerEntry {
	var payload dragonTigerPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || !payload.Success {
		return nil
	}
	if payload.Result == nil {
		return []domain.DragonTigerEntry{}
	}
	result := make([]domain.DragonTigerEntry, 0, len(payload.Result.Data))
	for _, item := range payload.Result.Data {
		symbol := dragonTigerSymbol(item.Code)
		if symbol == "" || item.TradeDate == "" {
			continue
		}
		result = append(result, domain.DragonTigerEntry{
			Symbol: symbol, Name: item.Name, TradeDate: normalizeTradeDate(item.TradeDate),
			Reason: item.Reason, SeatSummary: item.SeatSummary,
			ClosePrice: rawNumber(item.ClosePrice), ChangePercent: rawNumber(item.ChangePercent),
			NetAmount: rawNumber(item.NetAmount), BuyAmount: rawNumber(item.BuyAmount), SellAmount: rawNumber(item.SellAmount),
			DealAmount: rawNumber(item.DealAmount), MarketAmount: rawNumber(item.MarketAmount),
			NetRatio: rawNumber(item.NetRatio), DealAmountRatio: rawNumber(item.DealAmountRatio), Turnover: rawNumber(item.Turnover),
			Next1Percent: rawNumber(item.Next1Percent), Next2Percent: rawNumber(item.Next2Percent),
			Next5Percent: rawNumber(item.Next5Percent), Next10Percent: rawNumber(item.Next10Percent),
		})
	}
	sort.SliceStable(result, func(left, right int) bool {
		return result[left].TradeDate > result[right].TradeDate
	})
	return result
}

func dragonTigerAddress(base, symbol, startDate, endDate string) string {
	code := ""
	if len(symbol) == 8 {
		code = symbol[2:]
	}
	if strings.Contains(base, "{code}") {
		address := strings.ReplaceAll(base, "{code}", url.QueryEscape(code))
		address = strings.ReplaceAll(address, "{start}", url.QueryEscape(startDate))
		return strings.ReplaceAll(address, "{end}", url.QueryEscape(endDate))
	}
	columns := strings.Join([]string{
		"SECURITY_CODE", "SECURITY_NAME_ABBR", "TRADE_DATE", "EXPLAIN", "EXPLANATION", "CLOSE_PRICE", "CHANGE_RATE",
		"BILLBOARD_NET_AMT", "BILLBOARD_BUY_AMT", "BILLBOARD_SELL_AMT", "BILLBOARD_DEAL_AMT", "ACCUM_AMOUNT",
		"DEAL_NET_RATIO", "DEAL_AMOUNT_RATIO", "TURNOVERRATE", "D1_CLOSE_ADJCHRATE", "D2_CLOSE_ADJCHRATE",
		"D5_CLOSE_ADJCHRATE", "D10_CLOSE_ADJCHRATE",
	}, ",")
	filter := fmt.Sprintf(`(SECURITY_CODE="%s")(TRADE_DATE>='%s')(TRADE_DATE<='%s')`, code, startDate, endDate)
	values := url.Values{
		"client":      {"WEB"},
		"columns":     {columns},
		"filter":      {filter},
		"pageNumber":  {"1"},
		"pageSize":    {"20"},
		"reportName":  {"RPT_DAILYBILLBOARD_DETAILSNEW"},
		"sortColumns": {"TRADE_DATE,SECURITY_CODE"},
		"sortTypes":   {"-1,1"},
		"source":      {"WEB"},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

func (EastmoneyClient) FetchDragonTiger(ctx context.Context, symbol string) (domain.DragonTigerSnapshot, error) {
	if eastmoneySecurityID(symbol) == "" {
		return domain.DragonTigerSnapshot{}, fmt.Errorf("无效股票代码 %q", symbol)
	}
	now := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
	startDate := now.AddDate(0, 0, -dragonTigerWindowDays).Format("2006-01-02")
	endDate := now.Format("2006-01-02")
	base := os.Getenv("ASTOCK_DRAGON_TIGER_API_URL")
	if base == "" {
		base = dragonTigerAPIURL
	}
	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	raw, err := fetchDecoded(requestContext, dragonTigerAddress(base, symbol, startDate, endDate), nil)
	cancel()
	if err != nil {
		return domain.DragonTigerSnapshot{}, err
	}
	entries := ParseDragonTigerPayload(raw)
	if entries == nil {
		return domain.DragonTigerSnapshot{}, fmt.Errorf("未解析到龙虎榜数据")
	}
	return domain.DragonTigerSnapshot{Loaded: true, WindowDays: dragonTigerWindowDays, Entries: entries}, nil
}
