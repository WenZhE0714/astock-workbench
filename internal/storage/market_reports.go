package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type MarketReportStore struct {
	root string
}

type marketReportMetadata struct {
	GeneratedAt time.Time `json:"generated_at"`
	AIUsed      bool      `json:"ai_used"`
	AIError     string    `json:"ai_error,omitempty"`
}

func NewMarketReportStore(root string) *MarketReportStore {
	return &MarketReportStore{root: root}
}

func (store *MarketReportStore) Save(report domain.GeneratedMarketReport) (domain.GeneratedMarketReport, error) {
	if report.GeneratedAt.IsZero() {
		report.GeneratedAt = time.Now()
	}
	if strings.TrimSpace(report.Markdown) == "" {
		return report, fmt.Errorf("市场报告内容为空")
	}
	directory := filepath.Join(store.root, report.GeneratedAt.Format("20060102T150405"))
	factsPath := filepath.Join(directory, "snapshot.json")
	markdownPath := filepath.Join(directory, "report.md")
	metadataPath := filepath.Join(directory, "metadata.json")
	facts, err := json.MarshalIndent(report.Facts, "", "  ")
	if err != nil {
		return report, err
	}
	metadata, err := json.MarshalIndent(marketReportMetadata{
		GeneratedAt: report.GeneratedAt, AIUsed: report.AIUsed, AIError: report.AIError,
	}, "", "  ")
	if err != nil {
		return report, err
	}
	if err := atomicWrite(factsPath, append(facts, '\n'), 0o600); err != nil {
		return report, err
	}
	if err := atomicWrite(markdownPath, []byte(strings.TrimSpace(report.Markdown)+"\n"), 0o600); err != nil {
		return report, err
	}
	if err := atomicWrite(metadataPath, append(metadata, '\n'), 0o600); err != nil {
		return report, err
	}
	report.Directory = directory
	report.MarkdownPath = markdownPath
	report.FactsPath = factsPath
	return report, nil
}

func (store *MarketReportStore) LoadLatest() (domain.GeneratedMarketReport, error) {
	entries, err := os.ReadDir(store.root)
	if os.IsNotExist(err) {
		return domain.GeneratedMarketReport{}, fmt.Errorf("尚未生成智能市场报告")
	}
	if err != nil {
		return domain.GeneratedMarketReport{}, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return domain.GeneratedMarketReport{}, fmt.Errorf("尚未生成智能市场报告")
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	directory := filepath.Join(store.root, names[0])
	markdownPath := filepath.Join(directory, "report.md")
	factsPath := filepath.Join(directory, "snapshot.json")
	metadataPath := filepath.Join(directory, "metadata.json")
	markdown, err := os.ReadFile(markdownPath)
	if err != nil {
		return domain.GeneratedMarketReport{}, err
	}
	factsData, err := os.ReadFile(factsPath)
	if err != nil {
		return domain.GeneratedMarketReport{}, err
	}
	metadataData, err := os.ReadFile(metadataPath)
	if err != nil {
		return domain.GeneratedMarketReport{}, err
	}
	var facts domain.MarketScanFacts
	if err := json.Unmarshal(factsData, &facts); err != nil {
		return domain.GeneratedMarketReport{}, err
	}
	var metadata marketReportMetadata
	if err := json.Unmarshal(metadataData, &metadata); err != nil {
		return domain.GeneratedMarketReport{}, err
	}
	return domain.GeneratedMarketReport{
		GeneratedAt: metadata.GeneratedAt, AIUsed: metadata.AIUsed, AIError: metadata.AIError,
		Markdown: string(markdown), Facts: facts, Directory: directory,
		MarkdownPath: markdownPath, FactsPath: factsPath,
	}, nil
}
