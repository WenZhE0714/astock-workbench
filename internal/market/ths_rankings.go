package market

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const THSMarketRankingSource = "同花顺公开榜单"
const thsMarketRankingURL = "https://q.10jqka.com.cn/index/index/board/all/field/{metric}/order/{order}/page/{page}/ajax/1/"

var errTHSRankingSnapshot = errors.New("同花顺榜单快照无效")

var thsRankingPagePattern = regexp.MustCompile(`(?is)<span\b[^>]*class=["']page_info["'][^>]*>\s*(\d+)\s*/\s*(\d+)\s*</span>`)

type THSMarketRankingClient struct {
	HTTPClient *http.Client
	BaseURL    string
}

type thsMarketRankingPage struct {
	items       []domain.MarketRankingItem
	page, pages int
}

func thsRankingSort(kind domain.MarketRankingKind) (string, string, error) {
	switch kind {
	case domain.MarketRankingGainers:
		return "zdf", "desc", nil
	case domain.MarketRankingLosers:
		return "zdf", "asc", nil
	case domain.MarketRankingRapidRise:
		return "zs", "desc", nil
	case domain.MarketRankingAmount:
		return "cje", "desc", nil
	case domain.MarketRankingTurnover:
		return "hs", "desc", nil
	default:
		return "", "", fmt.Errorf("未知榜单类型 %q", kind)
	}
}

func parseTHSMarketRankingPage(raw string, kind domain.MarketRankingKind) (thsMarketRankingPage, error) {
	page := thsMarketRankingPage{}
	raw = decodeTHSAuto(raw)
	pagination := thsRankingPagePattern.FindStringSubmatch(raw)
	body := thsIndustryBodyPattern.FindStringSubmatch(raw)
	if len(pagination) != 3 || len(body) != 2 {
		return page, fmt.Errorf("%w: 缺少表格或分页数据", errTHSRankingSnapshot)
	}
	page.page, _ = strconv.Atoi(pagination[1])
	page.pages, _ = strconv.Atoi(pagination[2])
	if page.page < 1 || page.pages < page.page {
		return page, fmt.Errorf("%w: 分页无效", errTHSRankingSnapshot)
	}
	rows := thsIndustryRowPattern.FindAllStringSubmatch(body[1], -1)
	if len(rows) == 0 {
		return page, fmt.Errorf("%w: 榜单为空", errTHSRankingSnapshot)
	}
	for _, row := range rows {
		cells := thsIndustryCellPattern.FindAllStringSubmatch(row[1], -1)
		if len(cells) < 11 {
			continue
		}
		values := make([]string, len(cells))
		for index, cell := range cells {
			values[index] = stripTHSHTML(cell[1])
		}
		code := values[1]
		// The upstream "all" board also contains Beijing listings. Keep the
		// established Shanghai/Shenzhen A-share universe before taking top N.
		if len(code) != 6 || !(strings.HasPrefix(code, "00") || strings.HasPrefix(code, "30") || strings.HasPrefix(code, "60") || strings.HasPrefix(code, "688")) {
			continue
		}
		symbol, status := InspectSymbol(code)
		if status != "ok" || !ValidPrefixedSymbol(symbol) || values[2] == "" {
			continue
		}
		item := domain.MarketRankingItem{
			Symbol: symbol, Name: values[2], Source: THSMarketRankingSource, Industry: "--",
			Price: parseTHSFloat(values[3]), Percent: parseTHSFloat(values[4]), Speed: parseTHSFloat(values[6]),
			Turnover: parseTHSFloat(values[7]), Amount: parseTHSMoney(values[10]),
		}
		if !finiteTHS(item.Price) || item.Price <= 0 || !finiteTHS(rankingMetric(item, kind)) {
			continue
		}
		page.items = append(page.items, item)
	}
	if !validRankingOrder(page.items, kind) {
		return page, fmt.Errorf("%w: 排序与请求不符", errTHSRankingSnapshot)
	}
	return page, nil
}

func (client THSMarketRankingClient) FetchMarketRanking(ctx context.Context, kind domain.MarketRankingKind, limit int) ([]domain.MarketRankingItem, error) {
	if _, _, err := thsRankingSort(kind); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var lastError error
	for attempt := 0; attempt < 2; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		attemptCtx, cancelAttempt := fallbackContext(ctx, 2-attempt, 3*time.Second)
		items, err := client.fetchRankingAttempt(attemptCtx, kind, limit)
		cancelAttempt()
		if err == nil {
			return items, nil
		}
		lastError = err
		if !errors.Is(err, errTHSRankingSnapshot) && !errors.Is(err, context.DeadlineExceeded) && !transientHTTPReadError(err) {
			var status *httpStatusError
			if !errors.As(err, &status) || status.code < 500 {
				break
			}
		}
	}
	return nil, lastError
}

// A retry starts again at page one so rows from different attempts cannot be
// combined into a market-wide ranking that never existed.
func (client THSMarketRankingClient) fetchRankingAttempt(ctx context.Context, kind domain.MarketRankingKind, limit int) ([]domain.MarketRankingItem, error) {
	metric, order, err := thsRankingSort(kind)
	if err != nil {
		return nil, err
	}
	limit = normalizedRankingLimit(limit)
	base := firstNonEmpty(client.BaseURL, os.Getenv("ASTOCK_THS_MARKET_RANK_API_URL"), thsMarketRankingURL)
	items := make([]domain.MarketRankingItem, 0, limit)
	seen := make(map[string]bool)
	for pageNumber := 1; pageNumber <= 10; pageNumber++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		address := strings.NewReplacer("{metric}", metric, "{order}", order, "{page}", strconv.Itoa(pageNumber)).Replace(base)
		requestCtx, cancelRequest := context.WithTimeout(ctx, 3*time.Second)
		headers := map[string]string{"Referer": "https://q.10jqka.com.cn/", "X-Requested-With": "XMLHttpRequest"}
		var raw string
		if client.HTTPClient != nil {
			raw, err = fetchDecodedWithClient(requestCtx, client.HTTPClient, address, nil, headers)
		} else {
			raw, err = fetchTHSDecoded(requestCtx, address, nil, headers)
		}
		cancelRequest()
		if err != nil {
			return nil, fmt.Errorf("同花顺榜单: %w", err)
		}
		page, err := parseTHSMarketRankingPage(raw, kind)
		if err != nil {
			return nil, err
		}
		if page.page != pageNumber {
			return nil, fmt.Errorf("%w: 重复或错误页码 %d（请求%d）", errTHSRankingSnapshot, page.page, pageNumber)
		}
		for _, item := range page.items {
			if !seen[item.Symbol] {
				items = append(items, item)
				seen[item.Symbol] = true
			}
		}
		if !validRankingOrder(items, kind) {
			return nil, fmt.Errorf("%w: 跨页排序变化", errTHSRankingSnapshot)
		}
		if len(items) >= limit {
			return items[:limit], nil
		}
		if pageNumber == page.pages {
			break
		}
	}
	return nil, fmt.Errorf("%w: 沪深A股仅返回%d/%d条有效数据", errTHSRankingSnapshot, len(items), limit)
}
