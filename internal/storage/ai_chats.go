package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

const maxAIChatTurnsPerStock = 100

type AIChatStore struct {
	root string
}

type aiChatConversation struct {
	SchemaVersion int                 `json:"schema_version"`
	Symbol        string              `json:"symbol"`
	Name          string              `json:"name"`
	UpdatedAt     time.Time           `json:"updated_at"`
	Turns         []domain.AIChatTurn `json:"turns"`
}

func NewAIChatStore(root string) *AIChatStore {
	return &AIChatStore{root: root}
}

func (store *AIChatStore) conversationPath(symbol string) (string, error) {
	code := stockReportCode(symbol)
	if code == "" {
		return "", fmt.Errorf("无效AI咨询股票代码 %q", symbol)
	}
	return filepath.Join(store.root, code, "conversation.json"), nil
}

func (store *AIChatStore) Save(symbol, name string, turns []domain.AIChatTurn) error {
	path, err := store.conversationPath(symbol)
	if err != nil {
		return err
	}
	if len(turns) > maxAIChatTurnsPerStock {
		turns = turns[len(turns)-maxAIChatTurnsPerStock:]
	}
	clean := make([]domain.AIChatTurn, 0, len(turns))
	for _, turn := range turns {
		turn.Question = strings.TrimSpace(turn.Question)
		turn.Answer = strings.TrimSpace(turn.Answer)
		if turn.Question == "" || turn.Answer == "" {
			continue
		}
		clean = append(clean, turn)
	}
	conversation := aiChatConversation{
		SchemaVersion: 1, Symbol: symbol, Name: strings.TrimSpace(name),
		UpdatedAt: time.Now(), Turns: clean,
	}
	data, err := json.MarshalIndent(conversation, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0o600)
}

func (store *AIChatStore) Load(symbol string) (string, []domain.AIChatTurn, error) {
	path, err := store.conversationPath(symbol)
	if err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	var conversation aiChatConversation
	if err := json.Unmarshal(data, &conversation); err != nil {
		return "", nil, err
	}
	if conversation.SchemaVersion != 1 || conversation.Symbol != symbol {
		return "", nil, fmt.Errorf("AI咨询历史格式无效")
	}
	return conversation.Name, append([]domain.AIChatTurn(nil), conversation.Turns...), nil
}
