package market

import (
	"context"
	"errors"
	"testing"

	"github.com/wenzhe/astock-workbench/internal/domain"
)

type boardFlowClientStub struct {
	items []domain.BoardFlow
	err   error
	calls int
}

func (client *boardFlowClientStub) FetchBoards(context.Context, string) ([]domain.BoardFlow, error) {
	client.calls++
	return client.items, client.err
}

func TestFallbackBoardFlowClientUsesIndependentFallback(t *testing.T) {
	primary := &boardFlowClientStub{err: errors.New("primary unavailable")}
	fallback := &boardFlowClientStub{items: []domain.BoardFlow{{Code: "th881125", Name: "白酒", Source: "同花顺"}}}
	items, err := NewFallbackBoardFlowClient(primary, fallback).FetchBoards(context.Background(), "sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if primary.calls != 1 || fallback.calls != 1 || len(items) != 1 || items[0].Code != "th881125" {
		t.Fatalf("unexpected fallback result: primary=%d fallback=%d items=%+v", primary.calls, fallback.calls, items)
	}
}

func TestFallbackBoardFlowClientRejectsEmptyPrimaryResponse(t *testing.T) {
	primary := &boardFlowClientStub{}
	fallback := &boardFlowClientStub{items: []domain.BoardFlow{{Code: "th881155", Name: "银行"}}}
	items, err := NewFallbackBoardFlowClient(primary, fallback).FetchBoards(context.Background(), "sh600000")
	if err != nil || len(items) != 1 || fallback.calls != 1 {
		t.Fatalf("expected empty primary to fall back: err=%v items=%+v calls=%d", err, items, fallback.calls)
	}
}
