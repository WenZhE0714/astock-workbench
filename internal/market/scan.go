package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const announcementAPIURL = "https://np-anotice-stock.eastmoney.com/api/security/ann"

type MarketScanClient interface {
	FetchIndustryRanking(context.Context, domain.MarketScanMetric, bool, int) ([]domain.BoardFlow, error)
	FetchStockRanking(context.Context, domain.MarketScanMetric, bool, int) ([]domain.MarketStockSnapshot, error)
	FetchStocks(context.Context, []string) ([]domain.MarketStockSnapshot, error)
	FetchAnnouncements(context.Context, []string, int) ([]domain.MarketAnnouncement, error)
}

type IndustryLeaderClient interface {
	FetchIndustryLeaders(context.Context, string, int) ([]domain.MarketStockSnapshot, error)
}

type scanPayload struct {
	Data *struct {
		Diff []struct {
			Price         json.RawMessage `json:"f2"`
			Percent       json.RawMessage `json:"f3"`
			Amount        json.RawMessage `json:"f6"`
			Turnover      json.RawMessage `json:"f8"`
			VolumeRatio   json.RawMessage `json:"f10"`
			Code          string          `json:"f12"`
			Market        json.RawMessage `json:"f13"`
			Name          string          `json:"f14"`
			High          json.RawMessage `json:"f15"`
			Low           json.RawMessage `json:"f16"`
			Open          json.RawMessage `json:"f17"`
			PreviousClose json.RawMessage `json:"f18"`
			MarketCap     json.RawMessage `json:"f20"`
			Speed         json.RawMessage `json:"f22"`
			MainNet       json.RawMessage `json:"f62"`
			Industry      string          `json:"f100"`
			RiseCount     json.RawMessage `json:"f104"`
			FallCount     json.RawMessage `json:"f105"`
			FlatCount     json.RawMessage `json:"f106"`
			LeaderName    string          `json:"f128"`
			LeaderPercent json.RawMessage `json:"f136"`
			LeaderCode    string          `json:"f140"`
			MainRatio     json.RawMessage `json:"f184"`
		} `json:"diff"`
	} `json:"data"`
}

func ParseIndustryScanPayload(raw string) []domain.BoardFlow {
	var payload scanPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Data == nil {
		return nil
	}
	result := make([]domain.BoardFlow, 0, len(payload.Data.Diff))
	for _, item := range payload.Data.Diff {
		code := normalizeBoardCode(item.Code)
		name := strings.TrimSpace(item.Name)
		if code == "" || name == "" {
			continue
		}
		result = append(result, domain.BoardFlow{
			Code: code, Name: name, Kind: domain.BoardKindIndustry,
			Percent: rawNumber(item.Percent), Turnover: rawNumber(item.Turnover),
			MainNet: rawNumber(item.MainNet), MainRatio: rawNumber(item.MainRatio),
			RiseCount: rawInteger(item.RiseCount), FallCount: rawInteger(item.FallCount), FlatCount: rawInteger(item.FlatCount),
			LeaderName: strings.TrimSpace(item.LeaderName), LeaderCode: strings.TrimSpace(item.LeaderCode),
			LeaderPercent: rawNumber(item.LeaderPercent),
		})
	}
	return result
}

func rawInteger(value json.RawMessage) int {
	number := rawNumber(value)
	if number != number {
		return 0
	}
	return int(number)
}

func ParseMarketStockScanPayload(raw string) []domain.MarketStockSnapshot {
	var payload scanPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Data == nil {
		return nil
	}
	result := make([]domain.MarketStockSnapshot, 0, len(payload.Data.Diff))
	for _, item := range payload.Data.Diff {
		if len(item.Code) != 6 || strings.TrimSpace(item.Name) == "" {
			continue
		}
		prefix := "sz"
		if marketNumber(item.Market, item.Code) == 1 {
			prefix = "sh"
		}
		symbol := prefix + item.Code
		if !ValidPrefixedSymbol(symbol) {
			continue
		}
		result = append(result, domain.MarketStockSnapshot{
			Symbol: symbol, Name: strings.TrimSpace(item.Name), Industry: strings.TrimSpace(item.Industry),
			Price: rawNumber(item.Price), Percent: rawNumber(item.Percent), Amount: rawNumber(item.Amount),
			Turnover: rawNumber(item.Turnover), VolumeRatio: rawNumber(item.VolumeRatio), Speed: rawNumber(item.Speed),
			High: rawNumber(item.High), Low: rawNumber(item.Low), Open: rawNumber(item.Open),
			PreviousClose: rawNumber(item.PreviousClose), MarketCap: rawNumber(item.MarketCap),
			MainNet: rawNumber(item.MainNet), MainRatio: rawNumber(item.MainRatio),
		})
	}
	return result
}

func scanMetricField(metric domain.MarketScanMetric) (string, error) {
	switch metric {
	case domain.MarketScanByPercent:
		return "f3", nil
	case domain.MarketScanByAmount:
		return "f6", nil
	case domain.MarketScanByMainNet:
		return "f62", nil
	default:
		return "", fmt.Errorf("未知市场扫描指标 %q", metric)
	}
}

func scanFields() string {
	return "f2,f3,f6,f8,f10,f12,f13,f14,f15,f16,f17,f18,f20,f22,f62,f100,f104,f105,f106,f128,f136,f140,f184"
}

func scanRankingAddress(base, universe string, metric domain.MarketScanMetric, descending bool, limit int) (string, error) {
	field, err := scanMetricField(metric)
	if err != nil {
		return "", err
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	order := "0"
	if descending {
		order = "1"
	}
	values := url.Values{
		"fields": {scanFields()}, "fid": {field}, "fltt": {"2"}, "fs": {universe},
		"invt": {"2"}, "np": {"1"}, "pn": {"1"}, "po": {order}, "pz": {strconv.Itoa(limit)},
		"ut": {"bd1d9ddb04089700cf9c27f6f7426281"},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode(), nil
}

func fetchScanRanking(ctx context.Context, bases []string, universe string, metric domain.MarketScanMetric, descending bool, limit int, parse func(string) int) (string, error) {
	var lastError error
	for _, base := range bases {
		address, err := scanRankingAddress(base, universe, metric, descending, limit)
		if err != nil {
			return "", err
		}
		requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		raw, fetchError := fetchDecoded(requestContext, address, nil)
		cancel()
		if fetchError != nil {
			lastError = fetchError
			continue
		}
		if parse(raw) == 0 {
			lastError = fmt.Errorf("未解析到市场扫描数据")
			continue
		}
		return raw, nil
	}
	if lastError == nil {
		lastError = fmt.Errorf("市场扫描数据暂不可用")
	}
	return "", lastError
}

func (EastmoneyClient) FetchIndustryRanking(ctx context.Context, metric domain.MarketScanMetric, descending bool, limit int) ([]domain.BoardFlow, error) {
	bases := boardRankBases(domain.BoardKindIndustry)
	if configured := os.Getenv("ASTOCK_INDUSTRY_SCAN_API_URL"); configured != "" {
		bases = []string{configured}
	}
	raw, err := fetchScanRanking(ctx, bases, "m:90+t:2+f:!50", metric, descending, limit, func(value string) int {
		return len(ParseIndustryScanPayload(value))
	})
	if err != nil {
		return nil, err
	}
	return ParseIndustryScanPayload(raw), nil
}

func (EastmoneyClient) FetchStockRanking(ctx context.Context, metric domain.MarketScanMetric, descending bool, limit int) ([]domain.MarketStockSnapshot, error) {
	bases := marketRankingBases()
	if configured := os.Getenv("ASTOCK_MARKET_SCAN_API_URL"); configured != "" {
		bases = []string{configured}
	}
	raw, err := fetchScanRanking(ctx, bases, marketRankingUniverse, metric, descending, limit, func(value string) int {
		return len(ParseMarketStockScanPayload(value))
	})
	if err != nil {
		return nil, err
	}
	return ParseMarketStockScanPayload(raw), nil
}

func (EastmoneyClient) FetchIndustryLeaders(ctx context.Context, boardCode string, limit int) ([]domain.MarketStockSnapshot, error) {
	code := normalizeBoardCode(boardCode)
	if code == "" {
		return nil, fmt.Errorf("无效行业板块代码 %q", boardCode)
	}
	bases := marketRankingBases()
	if configured := os.Getenv("ASTOCK_INDUSTRY_LEADER_API_URL"); configured != "" {
		bases = []string{configured}
	}
	raw, err := fetchScanRanking(ctx, bases, "b:"+code, domain.MarketScanByAmount, true, limit, func(value string) int {
		return len(ParseMarketStockScanPayload(value))
	})
	if err != nil {
		return nil, err
	}
	items := ParseMarketStockScanPayload(raw)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func marketScanUListAddress(base string, symbols []string) string {
	securityIDs := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		if securityID := eastmoneySecurityID(symbol); securityID != "" {
			securityIDs = append(securityIDs, securityID)
		}
	}
	values := url.Values{
		"fltt": {"2"}, "invt": {"2"}, "fields": {scanFields()},
		"secids": {strings.Join(securityIDs, ",")}, "ut": {"b2884a393a59ad64002292a3e90d46a5"},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + values.Encode()
}

func (EastmoneyClient) FetchStocks(ctx context.Context, symbols []string) ([]domain.MarketStockSnapshot, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	bases := []string{fundFlowAPIURL, fundFlowFallbackAPIURL}
	if configured := os.Getenv("ASTOCK_FUND_FLOW_API_URL"); configured != "" {
		bases = []string{configured}
	}
	var lastError error
	for _, base := range bases {
		requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		raw, fetchError := fetchDecoded(requestContext, marketScanUListAddress(base, symbols), nil)
		cancel()
		if fetchError != nil {
			lastError = fetchError
			continue
		}
		items := ParseMarketStockScanPayload(raw)
		if len(items) == 0 {
			lastError = fmt.Errorf("未解析到候选股行情")
			continue
		}
		return items, nil
	}
	return nil, lastError
}

type announcementPayload struct {
	Data *struct {
		List []struct {
			ArtCode    string `json:"art_code"`
			NoticeDate string `json:"notice_date"`
			Title      string `json:"title"`
			Codes      []struct {
				MarketCode string `json:"market_code"`
				ShortName  string `json:"short_name"`
				StockCode  string `json:"stock_code"`
			} `json:"codes"`
		} `json:"list"`
	} `json:"data"`
}

func ParseMarketAnnouncementPayload(raw string) []domain.MarketAnnouncement {
	var payload announcementPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || payload.Data == nil {
		return nil
	}
	result := make([]domain.MarketAnnouncement, 0, len(payload.Data.List))
	for _, item := range payload.Data.List {
		if len(item.Codes) == 0 || len(item.Codes[0].StockCode) != 6 || strings.TrimSpace(item.Title) == "" {
			continue
		}
		prefix := "sz"
		if item.Codes[0].MarketCode == "1" {
			prefix = "sh"
		}
		date := item.NoticeDate
		if len(date) >= 10 {
			date = date[:10]
		}
		result = append(result, domain.MarketAnnouncement{
			Symbol: prefix + item.Codes[0].StockCode, Name: strings.TrimSpace(item.Codes[0].ShortName),
			Date: date, Title: strings.TrimSpace(item.Title), ArtCode: item.ArtCode,
		})
	}
	return result
}

func (EastmoneyClient) FetchAnnouncements(ctx context.Context, symbols []string, limit int) ([]domain.MarketAnnouncement, error) {
	codes := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		if len(symbol) == 8 {
			codes = append(codes, symbol[2:])
		}
	}
	if len(codes) == 0 {
		return nil, nil
	}
	if limit < 1 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	base := os.Getenv("ASTOCK_ANNOUNCEMENT_API_URL")
	if base == "" {
		base = announcementAPIURL
	}
	values := url.Values{
		"sr": {"-1"}, "page_size": {strconv.Itoa(limit)}, "page_index": {"1"},
		"ann_type": {"A"}, "client_source": {"web"}, "stock_list": {strings.Join(codes, ",")},
	}
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	raw, err := fetchDecoded(requestContext, base+separator+values.Encode(), nil)
	cancel()
	if err != nil {
		return nil, err
	}
	items := ParseMarketAnnouncementPayload(raw)
	if len(items) == 0 {
		return nil, fmt.Errorf("未解析到候选股公告索引")
	}
	return items, nil
}
