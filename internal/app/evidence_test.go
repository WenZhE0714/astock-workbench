package app

import (
	"strings"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestEvidenceSnapshotFiltersDatesDeduplicatesAndPreservesSourcePolicy(t *testing.T) {
	generatedAt := time.Date(2026, 8, 13, 12, 0, 0, 0, shanghaiLocation)
	announcements, droppedAnnouncements := filterRecentAnnouncements([]domain.MarketAnnouncement{
		{Symbol: "sh600519", Date: "2026-08-12", Title: "回购公告", ArtCode: "A1"},
		{Symbol: "sh600519", Date: "2026-08-12", Title: "回购公告副本", ArtCode: "A1"},
		{Symbol: "sh600519", Date: "2026-08-14", Title: "未来公告", ArtCode: "A2"},
		{Symbol: "sh600519", Date: "2026-06-01", Title: "过期公告", ArtCode: "A3"},
	}, generatedAt, 5)
	news, droppedNews := filterRecentNews([]domain.StockNewsItem{
		{Symbol: "sh600519", Date: "2026-08-13 09:00:00", Title: " 同一 条 新闻 ", Source: "财经媒体"},
		{Symbol: "sh600519", Date: "2026-08-13 10:00:00", Title: "同一条新闻", Source: "转载媒体"},
		{Symbol: "sh600519", Date: "2026-08-01", Title: "旧新闻", Source: "财经媒体"},
	}, generatedAt, 8)
	research, droppedResearch := filterRecentResearch([]domain.BrokerResearchItem{
		{Symbol: "sh600519", PublishedAt: "2026-07-23", Title: "需求根基稳固", Organization: "中邮证券", SourceID: "R1", Rating: "买入"},
		{Symbol: "sh600519", PublishedAt: "2026-08-14", Title: "未来研报", Organization: "测试证券", SourceID: "R2"},
	}, generatedAt, 5)
	if len(announcements) != 1 || len(news) != 1 || len(research) != 1 || droppedAnnouncements != 3 || droppedNews != 2 || droppedResearch != 1 {
		t.Fatalf("unexpected filtering: announcements=%#v news=%#v research=%#v drops=%d/%d/%d", announcements, news, research, droppedAnnouncements, droppedNews, droppedResearch)
	}
	snapshot := buildEvidenceSnapshot(generatedAt, announcements, news, research)
	if len(snapshot.Items) != 3 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	byKind := make(map[domain.EvidenceKind]domain.ResearchEvidence)
	for index, item := range snapshot.Items {
		if item.ID == "" || item.ID != []string{"E01", "E02", "E03"}[index] {
			t.Fatalf("unstable evidence ID order: %#v", snapshot.Items)
		}
		byKind[item.Kind] = item
	}
	if byKind[domain.EvidenceDisclosureIndex].Tier != domain.EvidenceTierC || byKind[domain.EvidenceDisclosureIndex].VerifiedBody {
		t.Fatalf("announcement index was incorrectly promoted to authority evidence: %#v", byKind[domain.EvidenceDisclosureIndex])
	}
	if byKind[domain.EvidenceBrokerResearch].Tier != domain.EvidenceTierB || !strings.Contains(byKind[domain.EvidenceBrokerResearch].Usage, "专业观点") {
		t.Fatalf("broker opinion policy missing: %#v", byKind[domain.EvidenceBrokerResearch])
	}
	if byKind[domain.EvidenceAuthoritativeNews].Tier != domain.EvidenceTierC {
		t.Fatalf("news was assigned an unsafe tier: %#v", byKind[domain.EvidenceAuthoritativeNews])
	}
}

func TestStockNewsClassificationRejectsGenericListsAndKeepsIndustryContext(t *testing.T) {
	generatedAt := time.Date(2026, 8, 13, 12, 0, 0, 0, shanghaiLocation)
	items, _, unrelated := classifyStockNews([]domain.StockNewsItem{
		{Date: "2026-08-13", Title: "中国巨石发布经营提示"},
		{Date: "2026-08-13", Title: "玻璃玻纤行业资金净流出"},
		{Date: "2026-08-13", Title: "48股特大单净流入超2亿元"},
	}, "sh600176", "中国巨石", []domain.BoardFlow{
		{Name: "玻璃玻纤", Kind: domain.BoardKindIndustry},
		{Name: "DeepSeek概念", Kind: domain.BoardKindConcept},
	}, generatedAt, 5)
	if len(items) != 2 || items[0].Relevance != "direct_mention" || items[1].Relevance != "industry_context" || unrelated != 1 {
		t.Fatalf("unexpected stock-news relevance: items=%#v unrelated=%d", items, unrelated)
	}
}

func TestReportPromptsRequireExistingEvidenceIDs(t *testing.T) {
	facts := domain.StockReportFacts{Evidence: domain.EvidenceSnapshot{Items: []domain.ResearchEvidence{{ID: "E01", Tier: domain.EvidenceTierB}}}}
	stockPrompt, err := stockReportPrompt(facts)
	if err != nil {
		t.Fatal(err)
	}
	marketPrompt, err := marketReportPrompt(domain.MarketScanFacts{Evidence: facts.Evidence})
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{stockPrompt, marketPrompt, researchAgentPrompt(stockReportAgentRoles[0], "个股研判", "fresh", []byte(`{"evidence":{"items":[{"id":"E01"}]}}`))} {
		if !strings.Contains(prompt, "[E##]") || !strings.Contains(prompt, "禁止创造") || !strings.Contains(prompt, "B层") {
			t.Fatalf("prompt does not enforce evidence policy:\n%s", prompt)
		}
	}
}

func TestDeterministicReportAlwaysRendersEvidenceCredentials(t *testing.T) {
	snapshot := domain.EvidenceSnapshot{Items: []domain.ResearchEvidence{{
		ID: "E01", Tier: domain.EvidenceTierB, Kind: domain.EvidenceBrokerResearch,
		Title: "测试研报", Publisher: "测试证券", Author: "分析师甲", PublishedAt: "2026-08-12",
		URL: "https://example.test/report", Usage: "券商专业观点",
	}}}
	markdown := attachEvidenceSection("# 报告", snapshot)
	for _, expected := range []string{"## 信息凭证", "[E01]", "B级", "测试证券", "分析师甲", "https://example.test/report"} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("missing %q in evidence section: %s", expected, markdown)
		}
	}
}

func TestEvidenceReferenceValidationRejectsMissingAndUnknownIDs(t *testing.T) {
	snapshot := domain.EvidenceSnapshot{Items: []domain.ResearchEvidence{{ID: "E01"}}}
	if err := validateEvidenceReferences("仅陈述观点", snapshot); err == nil {
		t.Fatal("expected a report without citations to be rejected")
	}
	if err := validateEvidenceReferences("来源 [E99]", snapshot); err == nil {
		t.Fatal("expected an unknown citation to be rejected")
	}
	if err := validateEvidenceReferences("来源 [E01]", snapshot); err != nil {
		t.Fatalf("known citation was rejected: %v", err)
	}
}
