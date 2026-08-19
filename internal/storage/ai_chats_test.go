package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

func TestAIChatStorePersistsConversationPerStock(t *testing.T) {
	root := t.TempDir()
	store := NewAIChatStore(root)
	turns := []domain.AIChatTurn{{
		AskedAt: time.Date(2026, 8, 12, 10, 30, 0, 0, time.Local), FactsAt: time.Date(2026, 8, 12, 10, 29, 59, 0, time.Local),
		FactsHash: "sha256:test", Agents: []domain.AgentResearchRun{{Role: "technical", Status: "ok"}},
		Question: " 现在能买吗？ ", Answer: " 等待确认。 ",
	}}
	if err := store.Save("sh600519", "贵州茅台", turns); err != nil {
		t.Fatal(err)
	}
	name, loaded, err := store.Load("sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if name != "贵州茅台" || len(loaded) != 1 || loaded[0].Question != "现在能买吗？" || loaded[0].Answer != "等待确认。" || loaded[0].FactsHash != "sha256:test" || len(loaded[0].Agents) != 1 {
		t.Fatalf("unexpected conversation: name=%q turns=%#v", name, loaded)
	}
	info, err := os.Stat(filepath.Join(root, "600519", "conversation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected permissions: %o", info.Mode().Perm())
	}
	_, empty, err := store.Load("sz000001")
	if err != nil || len(empty) != 0 {
		t.Fatalf("missing conversation should be empty: %#v %v", empty, err)
	}
}
