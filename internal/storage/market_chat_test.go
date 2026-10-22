package storage

import (
	"github.com/wenzhe/astock-workbench/internal/domain"
	"testing"
)

func TestMarketChatRecordIsExclusiveAndSeparateFromBankConversation(t *testing.T) {
	s := NewAIChatStore(t.TempDir())
	id := "1792570000000000000-abcdef123456"
	record := domain.MarketChatRecord{SchemaVersion: 1, Assessment: domain.MarketChatAssessment{ID: id}, Question: "大盘", Answer: "初始答案"}
	if err := s.SaveMarketRecord(record); err != nil {
		t.Fatal(err)
	}
	record.Answer = "覆盖答案"
	if err := s.SaveMarketRecord(record); err == nil {
		t.Fatal("record overwritten")
	}
	loaded, err := s.LoadMarketRecord(id)
	if err != nil || loaded.Answer != "初始答案" {
		t.Fatal("original lost")
	}
	if _, err := s.LoadMarketRecord("../../config"); err == nil {
		t.Fatal("unsafe path accepted")
	}
	if err := s.Save("market", "大盘", []domain.AIChatTurn{{Question: "大盘", Answer: "答", Assessment: &record.Assessment}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("sz000001", "平安银行", []domain.AIChatTurn{{Question: "银行", Answer: "答"}}); err != nil {
		t.Fatal(err)
	}
	_, turns, err := s.Load("market")
	if err != nil || len(turns) != 1 || turns[0].Assessment == nil || turns[0].Question != "大盘" {
		t.Fatal("market history collided with security")
	}
}
