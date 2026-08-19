package app

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

var evidenceReferencePattern = regexp.MustCompile(`\[(E[0-9]{2,})\]`)

func evidenceDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if len(value) >= 10 {
		value = value[:10]
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, shanghaiLocation)
	return parsed, err == nil
}

func withinEvidenceWindow(value string, generatedAt time.Time, days int) bool {
	date, ok := evidenceDate(value)
	if !ok {
		return false
	}
	today := generatedAt.In(shanghaiLocation)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, shanghaiLocation)
	return !date.After(today) && !date.Before(today.AddDate(0, 0, -days))
}

func filterRecentAnnouncements(items []domain.MarketAnnouncement, generatedAt time.Time, limit int) ([]domain.MarketAnnouncement, int) {
	result := make([]domain.MarketAnnouncement, 0, limit)
	seen := make(map[string]bool)
	dropped := 0
	for _, item := range items {
		key := strings.TrimSpace(item.ArtCode)
		if key == "" {
			key = item.Symbol + "|" + strings.Join(strings.Fields(item.Title), " ")
		}
		if !withinEvidenceWindow(item.Date, generatedAt, 30) || seen[key] {
			dropped++
			continue
		}
		seen[key] = true
		result = append(result, item)
		if len(result) == limit {
			break
		}
	}
	return result, dropped
}

func filterRecentAnnouncementsBySymbol(items []domain.MarketAnnouncement, generatedAt time.Time, perSymbol int) ([]domain.MarketAnnouncement, int) {
	result := make([]domain.MarketAnnouncement, 0, len(items))
	seen := make(map[string]bool)
	counts := make(map[string]int)
	dropped := 0
	for _, item := range items {
		key := strings.TrimSpace(item.ArtCode)
		if key == "" {
			key = item.Symbol + "|" + strings.Join(strings.Fields(item.Title), " ")
		}
		if !withinEvidenceWindow(item.Date, generatedAt, 30) || seen[key] || counts[item.Symbol] >= perSymbol {
			dropped++
			continue
		}
		seen[key] = true
		counts[item.Symbol]++
		result = append(result, item)
	}
	return result, dropped
}

func filterRecentNews(items []domain.StockNewsItem, generatedAt time.Time, limit int) ([]domain.StockNewsItem, int) {
	result := make([]domain.StockNewsItem, 0, limit)
	seen := make(map[string]bool)
	dropped := 0
	for _, item := range items {
		key := strings.ToLower(strings.Join(strings.Fields(item.Title), ""))
		if key == "" || !withinEvidenceWindow(item.Date, generatedAt, 7) || seen[key] {
			dropped++
			continue
		}
		seen[key] = true
		result = append(result, item)
		if len(result) == limit {
			break
		}
	}
	return result, dropped
}

func classifyStockNews(
	items []domain.StockNewsItem,
	symbol, name string,
	boards []domain.BoardFlow,
	generatedAt time.Time,
	limit int,
) ([]domain.StockNewsItem, int, int) {
	filtered, dropped := filterRecentNews(items, generatedAt, 0)
	name = strings.TrimSpace(name)
	code := ""
	if len(symbol) == 8 {
		code = symbol[2:]
	}
	direct := make([]domain.StockNewsItem, 0, limit)
	industry := make([]domain.StockNewsItem, 0, 2)
	boardNames := make([]string, 0)
	for _, board := range boards {
		if board.Kind != domain.BoardKindIndustry {
			continue
		}
		boardName := strings.TrimSpace(board.Name)
		if len([]rune(boardName)) >= 2 {
			boardNames = append(boardNames, boardName)
		}
	}
	unrelated := 0
	for _, item := range filtered {
		directMention := (name != "" && strings.Contains(item.Title, name)) || (code != "" && strings.Contains(item.Title, code))
		if directMention {
			item.Relevance = "direct_mention"
			if len(direct) < limit {
				direct = append(direct, item)
			}
			continue
		}
		industryMention := false
		for _, boardName := range boardNames {
			if strings.Contains(item.Title, boardName) {
				industryMention = true
				break
			}
		}
		if industryMention {
			item.Relevance = "industry_context"
			if len(industry) < 2 {
				industry = append(industry, item)
			}
			continue
		}
		unrelated++
	}
	result := append([]domain.StockNewsItem(nil), direct...)
	for _, item := range industry {
		if len(result) >= limit {
			break
		}
		result = append(result, item)
	}
	dropped += len(filtered) - len(result)
	return result, dropped, unrelated
}

func filterRecentResearch(items []domain.BrokerResearchItem, generatedAt time.Time, limit int) ([]domain.BrokerResearchItem, int) {
	result := make([]domain.BrokerResearchItem, 0, limit)
	seen := make(map[string]bool)
	dropped := 0
	for _, item := range items {
		key := strings.TrimSpace(item.SourceID)
		if key == "" {
			key = item.Symbol + "|" + item.Organization + "|" + strings.Join(strings.Fields(item.Title), "")
		}
		if !withinEvidenceWindow(item.PublishedAt, generatedAt, 90) || seen[key] {
			dropped++
			continue
		}
		seen[key] = true
		result = append(result, item)
		if len(result) == limit {
			break
		}
	}
	return result, dropped
}

func officialDisclosureSearchURL(symbol, artCode string) string {
	if len(symbol) != 8 {
		return ""
	}
	code := url.QueryEscape(symbol[2:])
	if strings.TrimSpace(artCode) != "" {
		return "https://data.eastmoney.com/notices/detail/" + code + "/" + url.QueryEscape(strings.TrimSpace(artCode)) + ".html"
	}
	if strings.HasPrefix(symbol, "sh") {
		return "https://www.sse.com.cn/assortment/stock/list/info/announcement/index.shtml?productId=" + code
	}
	return "https://www.szse.cn/disclosure/listed/notice/index.html?stock=" + code
}

func buildEvidenceSnapshot(
	generatedAt time.Time,
	announcements []domain.MarketAnnouncement,
	news []domain.StockNewsItem,
	research []domain.BrokerResearchItem,
) domain.EvidenceSnapshot {
	items := make([]domain.ResearchEvidence, 0, len(announcements)+len(news)+len(research))
	for _, item := range announcements {
		items = append(items, domain.ResearchEvidence{
			Kind: domain.EvidenceDisclosureIndex, Tier: domain.EvidenceTierC,
			Symbol: item.Symbol, Name: item.Name, Title: strings.TrimSpace(item.Title),
			Publisher: "东方财富公告索引", PublishedAt: item.Date, FetchedAt: generatedAt,
			URL: officialDisclosureSearchURL(item.Symbol, item.ArtCode), SourceID: item.ArtCode, VerifiedBody: false,
			Usage: "第三方公告索引；需回到交易所、巨潮或公司正式披露正文核验，不可直接作为公告条款事实",
		})
	}
	for _, item := range research {
		summary := ""
		if item.PreviousRating != "" || item.RatingChange != "" {
			summary = strings.TrimSpace("前次评级 " + item.PreviousRating + "；评级变化 " + item.RatingChange)
		}
		items = append(items, domain.ResearchEvidence{
			Kind: domain.EvidenceBrokerResearch, Tier: domain.EvidenceTierB,
			Symbol: item.Symbol, Name: item.Name, Title: item.Title, Publisher: item.Organization,
			Author: item.Author, PublishedAt: item.PublishedAt, FetchedAt: generatedAt,
			URL: item.URL, SourceID: item.SourceID, Summary: summary, Rating: item.Rating,
			Usage: "券商专业观点；评级和预测不属于公司已披露事实",
		})
	}
	for _, item := range news {
		publisher := strings.TrimSpace(item.Source)
		if publisher == "" {
			publisher = "来源未标明的财经资讯"
		}
		items = append(items, domain.ResearchEvidence{
			Kind: domain.EvidenceAuthoritativeNews, Tier: domain.EvidenceTierC,
			Symbol: item.Symbol, Name: item.Name, Title: strings.TrimSpace(item.Title), Publisher: publisher, PublishedAt: item.Date,
			FetchedAt: generatedAt, URL: item.URL, Usage: mapNewsRelevance(item.Relevance),
		})
	}
	tierOrder := map[domain.EvidenceTier]int{domain.EvidenceTierA: 0, domain.EvidenceTierB: 1, domain.EvidenceTierC: 2, domain.EvidenceTierD: 3}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].PublishedAt != items[j].PublishedAt {
			return items[i].PublishedAt > items[j].PublishedAt
		}
		if tierOrder[items[i].Tier] != tierOrder[items[j].Tier] {
			return tierOrder[items[i].Tier] < tierOrder[items[j].Tier]
		}
		if items[i].Title != items[j].Title {
			return items[i].Title < items[j].Title
		}
		if items[i].Publisher != items[j].Publisher {
			return items[i].Publisher < items[j].Publisher
		}
		if items[i].Symbol != items[j].Symbol {
			return items[i].Symbol < items[j].Symbol
		}
		return items[i].SourceID < items[j].SourceID
	})
	for index := range items {
		items[index].ID = fmt.Sprintf("E%02d", index+1)
	}
	return domain.EvidenceSnapshot{GeneratedAt: generatedAt, Items: items}
}

func mapNewsRelevance(value string) string {
	if value == "direct_mention" {
		return "标题直接提及个股名称或代码；仍需回到原文核验"
	}
	if value == "market_context" {
		return "市场/行业背景线索；标题未直接提及个股，不支撑个股事实"
	}
	if value == "industry_context" {
		return "匹配所属行业的背景线索；标题未直接提及个股，不支撑个股事实"
	}
	return "媒体新闻线索；需回到原文和一手来源交叉核验"
}

func evidenceIDsForSymbol(snapshot domain.EvidenceSnapshot, symbol string) []string {
	ids := make([]string, 0)
	for _, item := range snapshot.Items {
		if item.Symbol == symbol {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func firstEvidenceID(snapshot domain.EvidenceSnapshot, kind domain.EvidenceKind) string {
	for _, item := range snapshot.Items {
		if item.Kind == kind {
			return item.ID
		}
	}
	return ""
}

func renderEvidenceSection(snapshot domain.EvidenceSnapshot) string {
	var builder strings.Builder
	builder.WriteString("\n\n## 信息凭证\n\n")
	if len(snapshot.Items) == 0 {
		builder.WriteString("- 本次未采集到满足时效要求的外部信息凭证；报告仅依据行情、量价和资金快照。\n")
		return builder.String()
	}
	for _, warning := range snapshot.Warnings {
		fmt.Fprintf(&builder, "- 采集说明：%s。\n", strings.TrimSuffix(strings.TrimSpace(warning), "。"))
	}
	for _, item := range snapshot.Items {
		author := ""
		if item.Author != "" {
			author = "，" + item.Author
		}
		verification := ""
		if item.Kind == domain.EvidenceDisclosureIndex && !item.VerifiedBody {
			verification = "，正文未核验"
		}
		link := item.Title
		if item.URL != "" {
			link = "[" + item.Title + "](" + item.URL + ")"
		}
		fmt.Fprintf(&builder, "- [%s] %s级，%s%s，%s：%s%s。用途：%s。\n",
			item.ID, item.Tier, item.Publisher, author, item.PublishedAt, link, verification, item.Usage)
	}
	return builder.String()
}

func attachEvidenceSection(markdown string, snapshot domain.EvidenceSnapshot) string {
	markdown = strings.TrimSpace(markdown)
	if strings.Contains(markdown, "## 信息凭证") {
		return markdown
	}
	return markdown + renderEvidenceSection(snapshot)
}

func validateEvidenceReferences(markdown string, snapshot domain.EvidenceSnapshot) error {
	known := make(map[string]bool, len(snapshot.Items))
	for _, item := range snapshot.Items {
		known[item.ID] = true
	}
	foundKnown := false
	for _, match := range evidenceReferencePattern.FindAllStringSubmatch(markdown, -1) {
		if len(match) < 2 {
			continue
		}
		if !known[match[1]] {
			return fmt.Errorf("AI报告引用了冻结快照中不存在的凭证 %s", match[1])
		}
		foundKnown = true
	}
	if len(snapshot.Items) > 0 && !foundKnown {
		return fmt.Errorf("AI报告未引用已采集的冻结信息凭证")
	}
	return nil
}
