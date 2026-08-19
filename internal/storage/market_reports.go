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

type MarketReportIndexEntry struct {
	ID          string
	GeneratedAt time.Time
	AIUsed      bool
	AIError     string
	Directory   string
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

func (store *MarketReportStore) LoadLatest() (domain.GeneratedMarketReport, error) {
	items, err := store.List(1)
	if err != nil {
		return domain.GeneratedMarketReport{}, err
	}
	if len(items) == 0 {
		return domain.GeneratedMarketReport{}, fmt.Errorf("尚未生成智能市场报告")
	}
	return store.Load(items[0].ID)
}

func validReportID(id string) bool {
	id = strings.TrimSpace(id)
	return id != "" && !strings.Contains(id, string(filepath.Separator)) && !strings.Contains(id, "..")
}

func (store *MarketReportStore) Load(id string) (domain.GeneratedMarketReport, error) {
	if !validReportID(id) {
		return domain.GeneratedMarketReport{}, fmt.Errorf("无效市场报告 ID %q", id)
	}
	directory := filepath.Join(store.root, id)
	return loadMarketReportDirectory(directory)
}

func loadMarketReportDirectory(directory string) (domain.GeneratedMarketReport, error) {
	markdownPath := filepath.Join(directory, "report.md")
	factsPath := filepath.Join(directory, "snapshot.json")
	metadataPath := filepath.Join(directory, "metadata.json")
	agentsPath := filepath.Join(directory, "agents.json")
	evidencePath := filepath.Join(directory, "evidence.json")
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
	var agents []domain.AgentResearchRun
	if agentsData, agentsError := os.ReadFile(agentsPath); agentsError == nil {
		if err := json.Unmarshal(agentsData, &agents); err != nil {
			return domain.GeneratedMarketReport{}, err
		}
	} else if !os.IsNotExist(agentsError) {
		return domain.GeneratedMarketReport{}, agentsError
	}
	if evidenceData, evidenceError := os.ReadFile(evidencePath); evidenceError == nil {
		var evidence domain.EvidenceSnapshot
		if err := json.Unmarshal(evidenceData, &evidence); err != nil {
			return domain.GeneratedMarketReport{}, err
		}
		facts.Evidence = evidence
	} else if !os.IsNotExist(evidenceError) {
		return domain.GeneratedMarketReport{}, evidenceError
	}
	return domain.GeneratedMarketReport{
		GeneratedAt: metadata.GeneratedAt, AIUsed: metadata.AIUsed, AIError: metadata.AIError,
		Markdown: string(markdown), Facts: facts, Directory: directory,
		MarkdownPath: markdownPath, FactsPath: factsPath, AgentsPath: agentsPath, EvidencePath: evidencePath, Agents: agents,
	}, nil
}

func (store *MarketReportStore) List(limit int) ([]MarketReportIndexEntry, error) {
	entries, err := os.ReadDir(store.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]MarketReportIndexEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(store.root, entry.Name())
		metadataData, readError := os.ReadFile(filepath.Join(directory, "metadata.json"))
		if readError != nil {
			continue
		}
		var metadata marketReportMetadata
		if json.Unmarshal(metadataData, &metadata) != nil || metadata.GeneratedAt.IsZero() {
			continue
		}
		items = append(items, MarketReportIndexEntry{
			ID: entry.Name(), GeneratedAt: metadata.GeneratedAt, AIUsed: metadata.AIUsed,
			AIError: metadata.AIError, Directory: directory,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].GeneratedAt.After(items[j].GeneratedAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
