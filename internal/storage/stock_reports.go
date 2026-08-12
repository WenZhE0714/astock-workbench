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

type StockReportStore struct {
	root string
}

type stockReportMetadata struct {
	GeneratedAt time.Time `json:"generated_at"`
	Symbol      string    `json:"symbol"`
	Name        string    `json:"name"`
	AIUsed      bool      `json:"ai_used"`
	AIError     string    `json:"ai_error,omitempty"`
}

func NewStockReportStore(root string) *StockReportStore {
	return &StockReportStore{root: root}
}

func stockReportCode(symbol string) string {
	if len(symbol) == 8 && (strings.HasPrefix(symbol, "sh") || strings.HasPrefix(symbol, "sz")) {
		return symbol[2:]
	}
	return ""
}

func (store *StockReportStore) Save(report domain.GeneratedStockReport) (domain.GeneratedStockReport, error) {
	code := stockReportCode(report.Symbol)
	if code == "" {
		return report, fmt.Errorf("无效个股报告代码 %q", report.Symbol)
	}
	if report.GeneratedAt.IsZero() {
		report.GeneratedAt = time.Now()
	}
	if strings.TrimSpace(report.Markdown) == "" {
		return report, fmt.Errorf("个股报告内容为空")
	}
	directory := filepath.Join(store.root, code, report.GeneratedAt.Format("20060102T150405"))
	factsPath := filepath.Join(directory, "snapshot.json")
	markdownPath := filepath.Join(directory, "report.md")
	metadataPath := filepath.Join(directory, "metadata.json")
	facts, err := json.MarshalIndent(report.Facts, "", "  ")
	if err != nil {
		return report, err
	}
	metadata, err := json.MarshalIndent(stockReportMetadata{
		GeneratedAt: report.GeneratedAt, Symbol: report.Symbol, Name: report.Name,
		AIUsed: report.AIUsed, AIError: report.AIError,
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

func (store *StockReportStore) LoadLatest(symbol string) (domain.GeneratedStockReport, error) {
	code := stockReportCode(symbol)
	if code == "" {
		return domain.GeneratedStockReport{}, fmt.Errorf("无效个股报告代码 %q", symbol)
	}
	root := filepath.Join(store.root, code)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return domain.GeneratedStockReport{}, fmt.Errorf("尚未生成 %s 的个股研判", code)
	}
	if err != nil {
		return domain.GeneratedStockReport{}, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return domain.GeneratedStockReport{}, fmt.Errorf("尚未生成 %s 的个股研判", code)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	directory := filepath.Join(root, names[0])
	markdownPath := filepath.Join(directory, "report.md")
	factsPath := filepath.Join(directory, "snapshot.json")
	metadataPath := filepath.Join(directory, "metadata.json")
	markdown, err := os.ReadFile(markdownPath)
	if err != nil {
		return domain.GeneratedStockReport{}, err
	}
	factsData, err := os.ReadFile(factsPath)
	if err != nil {
		return domain.GeneratedStockReport{}, err
	}
	metadataData, err := os.ReadFile(metadataPath)
	if err != nil {
		return domain.GeneratedStockReport{}, err
	}
	var facts domain.StockReportFacts
	if err := json.Unmarshal(factsData, &facts); err != nil {
		return domain.GeneratedStockReport{}, err
	}
	var metadata stockReportMetadata
	if err := json.Unmarshal(metadataData, &metadata); err != nil {
		return domain.GeneratedStockReport{}, err
	}
	return domain.GeneratedStockReport{
		GeneratedAt: metadata.GeneratedAt, Symbol: metadata.Symbol, Name: metadata.Name,
		AIUsed: metadata.AIUsed, AIError: metadata.AIError, Markdown: string(markdown), Facts: facts,
		Directory: directory, MarkdownPath: markdownPath, FactsPath: factsPath,
	}, nil
}
