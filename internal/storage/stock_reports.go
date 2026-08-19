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

type StockReportIndexEntry struct {
	ID          string
	GeneratedAt time.Time
	Symbol      string
	Name        string
	AIUsed      bool
	AIError     string
	Directory   string
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
	agentsPath := filepath.Join(directory, "agents.json")
	evidencePath := filepath.Join(directory, "evidence.json")
	markdownPath := filepath.Join(directory, "report.md")
	metadataPath := filepath.Join(directory, "metadata.json")
	facts, err := json.MarshalIndent(report.Facts, "", "  ")
	if err != nil {
		return report, err
	}
	agents, err := json.MarshalIndent(append([]domain.AgentResearchRun{}, report.Agents...), "", "  ")
	if err != nil {
		return report, err
	}
	evidence, err := json.MarshalIndent(report.Facts.Evidence, "", "  ")
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
	if err := atomicWrite(agentsPath, append(agents, '\n'), 0o600); err != nil {
		return report, err
	}
	if err := atomicWrite(evidencePath, append(evidence, '\n'), 0o600); err != nil {
		return report, err
	}
	if err := atomicWrite(metadataPath, append(metadata, '\n'), 0o600); err != nil {
		return report, err
	}
	report.Directory = directory
	report.MarkdownPath = markdownPath
	report.FactsPath = factsPath
	report.AgentsPath = agentsPath
	report.EvidencePath = evidencePath
	return report, nil
}

func (store *StockReportStore) LoadLatest(symbol string) (domain.GeneratedStockReport, error) {
	items, err := store.List(symbol, 1)
	if err != nil {
		return domain.GeneratedStockReport{}, err
	}
	code := stockReportCode(symbol)
	if len(items) == 0 {
		return domain.GeneratedStockReport{}, fmt.Errorf("尚未生成 %s 的个股研判", code)
	}
	return store.Load(symbol, items[0].ID)
}

func (store *StockReportStore) Load(symbol, id string) (domain.GeneratedStockReport, error) {
	code := stockReportCode(symbol)
	if code == "" {
		return domain.GeneratedStockReport{}, fmt.Errorf("无效个股报告代码 %q", symbol)
	}
	if !validReportID(id) {
		return domain.GeneratedStockReport{}, fmt.Errorf("无效个股报告 ID %q", id)
	}
	directory := filepath.Join(store.root, code, id)
	return loadStockReportDirectory(directory)
}

func loadStockReportDirectory(directory string) (domain.GeneratedStockReport, error) {
	markdownPath := filepath.Join(directory, "report.md")
	factsPath := filepath.Join(directory, "snapshot.json")
	metadataPath := filepath.Join(directory, "metadata.json")
	agentsPath := filepath.Join(directory, "agents.json")
	evidencePath := filepath.Join(directory, "evidence.json")
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
	var agents []domain.AgentResearchRun
	if agentsData, agentsError := os.ReadFile(agentsPath); agentsError == nil {
		if err := json.Unmarshal(agentsData, &agents); err != nil {
			return domain.GeneratedStockReport{}, err
		}
	} else if !os.IsNotExist(agentsError) {
		return domain.GeneratedStockReport{}, agentsError
	}
	if evidenceData, evidenceError := os.ReadFile(evidencePath); evidenceError == nil {
		var evidence domain.EvidenceSnapshot
		if err := json.Unmarshal(evidenceData, &evidence); err != nil {
			return domain.GeneratedStockReport{}, err
		}
		facts.Evidence = evidence
	} else if !os.IsNotExist(evidenceError) {
		return domain.GeneratedStockReport{}, evidenceError
	}
	return domain.GeneratedStockReport{
		GeneratedAt: metadata.GeneratedAt, Symbol: metadata.Symbol, Name: metadata.Name,
		AIUsed: metadata.AIUsed, AIError: metadata.AIError, Markdown: string(markdown), Facts: facts,
		Agents: agents, Directory: directory, MarkdownPath: markdownPath, FactsPath: factsPath, AgentsPath: agentsPath,
		EvidencePath: evidencePath,
	}, nil
}

func (store *StockReportStore) List(symbol string, limit int) ([]StockReportIndexEntry, error) {
	code := stockReportCode(symbol)
	if code == "" {
		return nil, fmt.Errorf("无效个股报告代码 %q", symbol)
	}
	root := filepath.Join(store.root, code)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]StockReportIndexEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(root, entry.Name())
		metadataData, readError := os.ReadFile(filepath.Join(directory, "metadata.json"))
		if readError != nil {
			continue
		}
		var metadata stockReportMetadata
		if json.Unmarshal(metadataData, &metadata) != nil || metadata.GeneratedAt.IsZero() {
			continue
		}
		items = append(items, StockReportIndexEntry{
			ID: entry.Name(), GeneratedAt: metadata.GeneratedAt, Symbol: metadata.Symbol,
			Name: metadata.Name, AIUsed: metadata.AIUsed, AIError: metadata.AIError, Directory: directory,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].GeneratedAt.After(items[j].GeneratedAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
